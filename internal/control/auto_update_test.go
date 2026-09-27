package control_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/compose"
	ctl "github.com/msoldin/porty/internal/control"
	op "github.com/msoldin/porty/internal/operation"
	repo "github.com/msoldin/porty/internal/repository"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/stack"
	"path/filepath"
	"testing"
	"time"
)

type autoRuntime struct {
	alerts    *alert.Service
	guardErr  error
	verifyErr error
	controlRuntime
	snapshot compose.UpdateSnapshot
	prepare  func()
	apply    func()
	applyErr error
	applied  int
}

func (r *autoRuntime) CheckProject(context.Context, string) error { return r.guardErr }
func (r *autoRuntime) SnapshotUpdate(context.Context, compose.Request) (compose.UpdateSnapshot, error) {
	return r.snapshot, nil
}
func (r *autoRuntime) PrepareUpdate(_ context.Context, s compose.UpdateSnapshot) (compose.PreparedUpdate, error) {
	if r.prepare != nil {
		r.prepare()
	}
	return compose.PreparedUpdate{Snapshot: s, Changes: []compose.ImageChange{{Service: "app", SourceReference: "alpine:latest", TargetReference: "alpine@sha256:target", BeforeImageID: "before", AfterImageID: "after"}}}, nil
}
func (r *autoRuntime) ApplyUpdate(context.Context, compose.PreparedUpdate) error {
	r.applied++
	if r.apply != nil {
		r.apply()
	}
	return r.applyErr
}
func (r *autoRuntime) VerifyUpdate(context.Context, compose.PreparedUpdate) (compose.UpdateResult, error) {
	if r.applyErr != nil || r.verifyErr != nil {
		return compose.UpdateResult{RecoveryRequired: true}, errors.Join(r.applyErr, r.verifyErr)
	}
	return compose.UpdateResult{Services: []compose.ServiceUpdateResult{{Service: "app", TargetImageID: "after", ActualImageID: "after", Outcome: "verified"}}}, nil
}
func autoFixture(t *testing.T) (*ctl.ControlPlane, *store.AutoUpdateStore, *autoRuntime, autoupdate.Run, *op.Coordinator) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ss := store.NewStackStore(db)
	now := time.Now().UTC()
	if err := ss.Create(ctx, stack.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	os := store.NewOperationStore(db)
	if err := os.CreateOperation(ctx, op.Operation{ID: "baseline", Kind: "deploy", ScopeType: "stack", ScopeID: "stk_gateway", Status: op.OperationSucceeded}); err != nil {
		t.Fatal(err)
	}
	ds := store.NewDeploymentStore(db)
	diff := sha256.Sum256([]byte("diff"))
	if err := ds.SaveDeployment(ctx, op.Deployment{ID: "baseline", OperationID: "baseline", StackID: "stk_gateway", Status: op.DeploymentSucceeded, ComposeDigest: "source", GitCommit: "abc123", Dirty: true, DiffDigest: fmt.Sprintf("sha256:%x", diff), StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	updates := store.NewAutoUpdateStore(db)
	p, err := updates.SavePolicy(ctx, "stk_gateway", autoupdate.PolicyUpdate{Enabled: true, Expression: "* * * * *"}, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	run, ok, err := updates.Admit(ctx, p, now)
	if err != nil || !ok {
		t.Fatalf("run=%v %v", ok, err)
	}
	runtime := &autoRuntime{snapshot: compose.UpdateSnapshot{SourceDigest: "source", Project: &types.Project{Name: "porty-gateway", Services: types.Services{"app": {Name: "app", Image: "alpine:latest"}}}, Containers: []compose.UpdateContainer{{ID: "one", Service: "app", ImageID: "before", State: "running"}}}}
	co := op.NewCoordinator()
	control := ctl.NewControlPlane(t.TempDir(), ss, stack.NewEnvironmentService(ss), repo.NewRepositoryService(controlGit{}), runtime, op.NewOperationService(os, nil, time.Minute, 1024), op.NewDeploymentService(runtime, ds, co), co, ds, nil)
	runtime.alerts = alert.NewService(store.NewAlertStore(db), nil)
	control.SetAlertReader(store.NewAlertStore(db))
	control.SetUpdateStore(updates)
	return control, updates, runtime, run, co
}
func TestAutoUpdateSkipsPendingConfiguration(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.snapshot.SourceDigest = "changed"
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	last, _ := s.LatestRun(context.Background(), run.StackID)
	if r.applied != 0 || last.Outcome != "skipped" {
		t.Fatalf("unsafe run: %+v applies=%d", last, r.applied)
	}
}
func TestAutoUpdateRevalidatesAfterPrepare(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.prepare = func() {
		r.snapshot.Containers = []compose.UpdateContainer{{ID: "manually-recreated", Service: "app", ImageID: "before", State: "running"}}
	}
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	last, _ := s.LatestRun(context.Background(), run.StackID)
	if r.applied != 0 || last.Outcome != "skipped" {
		t.Fatalf("stale plan applied: %+v", last)
	}
}
func TestAutoUpdateDefersBusyStack(t *testing.T) {
	c, s, r, run, co := autoFixture(t)
	release, err := co.Try(false, string(run.StackID))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	last, _ := s.LatestRun(context.Background(), run.StackID)
	if r.applied != 0 || last.Reason != "busy" {
		t.Fatalf("busy run=%+v", last)
	}
}
func TestAutoUpdatePausesOnPartialFailure(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.applyErr = errors.New("partial recreation")
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	policy, _ := s.GetPolicy(context.Background(), run.StackID)
	if r.applied != 1 || policy.PausedReason == "" {
		t.Fatalf("not paused: %+v applies=%d", policy, r.applied)
	}
}
func TestAutoUpdateDisableDoesNotAbandonApply(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.apply = func() {
		_, err := s.SavePolicy(context.Background(), run.StackID, autoupdate.PolicyUpdate{Enabled: false, Expression: "* * * * *", ExpectedRevision: 1}, time.Now())
		if err != nil {
			t.Error(err)
		}
	}
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	last, _ := s.LatestRun(context.Background(), run.StackID)
	p, _ := s.GetPolicy(context.Background(), run.StackID)
	if last.Outcome != "updated" || p.Enabled {
		t.Fatalf("disabled applying=%+v %+v", last, p)
	}
}

func TestAutoUpdatePersistsIntentAndHoldsLockBeforeMutation(t *testing.T) {
	c, s, r, run, co := autoFixture(t)
	r.apply = func() {
		pending, err := s.PendingExecutions(context.Background())
		if err != nil || len(pending) != 1 || pending[0].Phase != "applying" {
			t.Errorf("mutation lacks durable intent: %+v %v", pending, err)
		}
		release, err := co.Try(false, string(run.StackID))
		if err == nil {
			release()
			t.Error("manual operation admitted during update")
		}
	}
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	images, err := s.EffectiveImages(context.Background(), run.StackID)
	if err != nil || images["app"].AfterImageID != "after" {
		t.Fatalf("effective images=%+v %v", images, err)
	}
}
func TestAutoUpdateDiscardsDisabledPolicyAfterPrepare(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.prepare = func() {
		_, err := s.SavePolicy(context.Background(), run.StackID, autoupdate.PolicyUpdate{Expression: "* * * * *", ExpectedRevision: 1}, time.Now())
		if err != nil {
			t.Error(err)
		}
	}
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if r.applied != 0 {
		t.Fatal("disabled update mutated runtime")
	}
}

func TestAutoUpdateCannotEnableHostingStack(t *testing.T) {
	c, _, r, _, _ := autoFixture(t)
	r.guardErr = compose.ErrSelfProtected
	if !errors.Is(c.EnableAutoUpdate(context.Background(), "stk_gateway"), compose.ErrSelfProtected) {
		t.Fatal("hosting stack enabled")
	}
}
func TestResumeRequiresVerifiedRecoveryAndKeepsDisabledPolicyDisabled(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.applyErr = errors.New("apply failed")
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetPolicy(context.Background(), run.StackID)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.SavePolicy(context.Background(), run.StackID, autoupdate.PolicyUpdate{Enabled: false, Expression: p.Expression, ExpectedRevision: p.Revision}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ResumeAutoUpdate(context.Background(), run.StackID, p.Revision, ""); err == nil {
		t.Fatal("unverified recovery accepted")
	}
	r.applyErr = nil
	r.verifyErr = nil
	if err := c.ResumeAutoUpdate(context.Background(), run.StackID, p.Revision, ""); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.GetPolicy(context.Background(), run.StackID)
	if err != nil || resumed.Enabled || resumed.PausedReason != "" || r.applied != 1 {
		t.Fatalf("resume mutated runtime or enabled policy: %+v %v applies=%d", resumed, err, r.applied)
	}
}

func TestAlertResolutionCannotResumeUpdate(t *testing.T) {
	c, s, r, run, _ := autoFixture(t)
	r.applyErr = errors.New("partial failure")
	if err := c.CheckAndUpdate(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	page, err := r.alerts.List(context.Background(), alert.Filter{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("alerts=%+v %v", page, err)
	}
	item, err := r.alerts.Acknowledge(context.Background(), alert.Mutation{ID: page.Items[0].ID, ExpectedRevision: page.Items[0].Revision, ActorID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.alerts.Resolve(context.Background(), alert.Mutation{ID: item.ID, ExpectedRevision: item.Revision, ActorID: "reviewer"}); err != nil {
		t.Fatal(err)
	}
	policy, err := s.GetPolicy(context.Background(), run.StackID)
	if err != nil || policy.PausedReason == "" || r.applied != 1 {
		t.Fatalf("alert resumed updates: %+v %v", policy, err)
	}
}
