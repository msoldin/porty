package control_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	portycompose "github.com/msoldin/porty/internal/compose"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	"strings"
	"testing"
	"time"
)

type reviewRuntime struct {
	controlRuntime
	source           string
	digestErr        error
	entered, release chan struct{}
}

func (r *reviewRuntime) Digest(_ context.Context, request portycompose.Request) (string, error) {
	if r.digestErr != nil {
		return "", r.digestErr
	}
	sum := sha256.Sum256([]byte(r.source + request.Environment["TOKEN"]))
	return hex.EncodeToString(sum[:]), nil
}
func (r *reviewRuntime) Deploy(context.Context, portycompose.Request, bool) error {
	if r.entered != nil {
		close(r.entered)
		<-r.release
	}
	return nil
}

type reviewEnvironment struct {
	controlEnvironmentStore
	token       string
	coordinator *portyop.Coordinator
	lockedReads int
}

func (e *reviewEnvironment) Environment(_ context.Context, id portystack.StackID) (map[string]string, error) {
	if e.coordinator != nil {
		release, err := e.coordinator.Try(false, string(id))
		if err == nil {
			release()
			return nil, errors.New("environment read without source lock")
		}
		e.lockedReads++
	}
	return map[string]string{"TOKEN": e.token}, nil
}

type reviewGit struct {
	controlGit
	head, diff string
}

func (g *reviewGit) Head(context.Context) (string, error)         { return g.head, nil }
func (g *reviewGit) Diff(context.Context, string) (string, error) { return g.diff, nil }

type reviewLookup struct{ archived bool }

func (l *reviewLookup) ByID(_ context.Context, id portystack.StackID) (portystack.Stack, error) {
	s := portystack.Stack{ID: id, DirectoryName: string(id), ComposeProjectName: string(id)}
	if l.archived {
		now := time.Now()
		s.ArchivedAt = &now
	}
	return s, nil
}

type reviewFixture struct {
	control     *portycontrol.ControlPlane
	runtime     *reviewRuntime
	environment *reviewEnvironment
	git         *reviewGit
	lookup      *reviewLookup
	coordinator *portyop.Coordinator
	operations  *countingOperationStore
}

func newReviewFixture() reviewFixture {
	f := reviewFixture{runtime: &reviewRuntime{source: "compose"}, environment: &reviewEnvironment{token: "sensitive-value"}, git: &reviewGit{head: "abc", diff: "local change"}, lookup: &reviewLookup{}, coordinator: portyop.NewCoordinator(), operations: &countingOperationStore{updated: make(chan portyop.Operation, 8)}}
	deployments := portyop.NewDeploymentService(f.runtime, &capturingDeploymentStore{saved: make(chan portyop.Deployment, 1)}, f.coordinator)
	f.control = portycontrol.NewControlPlane("/srv/repository", f.lookup, portystack.NewEnvironmentService(f.environment), portyrepo.NewRepositoryService(f.git), f.runtime, portyop.NewOperationService(f.operations, nil, time.Second, 1024), deployments, f.coordinator, nil, nil)
	return f
}
func TestReviewedDeploymentRejectsChangedSource(t *testing.T) {
	for _, kind := range []string{"configuration", "environment", "head", "diff", "stack", "restart", "archived", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			f := newReviewFixture()
			ctx := context.Background()
			review, err := f.control.ReviewDeployment(ctx, "one")
			if err != nil {
				t.Fatal(err)
			}
			id := portystack.StackID("one")
			expected := portycontrol.ErrDeploymentReviewChanged
			switch kind {
			case "configuration":
				f.runtime.source = "changed"
			case "environment":
				f.environment.token = "changed"
			case "head":
				f.git.head = "new"
			case "diff":
				f.git.diff = "new"
			case "stack":
				id = "two"
			case "restart":
				f = newReviewFixture()
			case "archived":
				f.lookup.archived = true
				expected = portycontrol.ErrStackRuntimeActionUnavailable
			case "invalid":
				f.runtime.digestErr = errors.New("invalid compose")
				expected = f.runtime.digestErr
			}
			_, err = f.control.StartReviewedDeployment(ctx, id, review.SourceRevision)
			if !errors.Is(err, expected) {
				t.Fatalf("error=%v want %v", err, expected)
			}
			if f.operations.created != 0 {
				t.Fatal("stale review accepted an operation")
			}
			release, err := f.coordinator.Try(false, string(id))
			if err != nil {
				t.Fatalf("admission leaked lock: %v", err)
			}
			release()
		})
	}
}
func TestReviewRevisionDoesNotExposeSecrets(t *testing.T) {
	f := newReviewFixture()
	review, err := f.control.ReviewDeployment(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := f.runtime.Digest(context.Background(), portycompose.Request{Environment: map[string]string{"TOKEN": f.environment.token}})
	if len(review.SourceRevision) != 64 || strings.Contains(review.SourceRevision, f.environment.token) || review.SourceRevision == digest {
		t.Fatalf("unsafe revision %q", review.SourceRevision)
	}
	again, err := f.control.ReviewDeployment(context.Background(), "one")
	if err != nil || review != again {
		t.Fatalf("unstable review: %v", err)
	}
}
func TestReviewedDeploymentKeepsSourceLockedUntilCompletion(t *testing.T) {
	f := newReviewFixture()
	f.environment.coordinator = f.coordinator
	f.runtime.entered = make(chan struct{})
	f.runtime.release = make(chan struct{})
	defer func() {
		if f.runtime.release != nil {
			close(f.runtime.release)
		}
	}()
	review, err := f.control.ReviewDeployment(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.control.StartReviewedDeployment(context.Background(), "one", review.SourceRevision); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.runtime.entered:
	case <-time.After(time.Second):
		t.Fatal("deployment did not start")
	}
	if release, err := f.coordinator.Try(false, "one"); !errors.Is(err, portyop.ErrOperationConflict) {
		if release != nil {
			release()
		}
		t.Fatalf("source unlocked during deploy: %v", err)
	}
	if f.environment.lockedReads != 2 {
		t.Fatalf("locked source reads=%d", f.environment.lockedReads)
	}
	close(f.runtime.release)
	// Wait for operation completion before checking release; deployment completion alone is insufficient.
	deadline := time.After(time.Second)
	for {
		select {
		case operation := <-f.operations.updated:
			if operation.Status != portyop.OperationSucceeded {
				continue
			}
			for attempts := 0; attempts < 100; attempts++ {
				if release, err := f.coordinator.Try(false, "one"); err == nil {
					release()
					f.runtime.release = nil
					return
				}
				time.Sleep(time.Millisecond)
			}
			f.runtime.release = nil
			t.Fatal("source lock not released after completion")
		case <-deadline:
			f.runtime.release = nil
			t.Fatal("operation did not finish")
		}
	}
}
