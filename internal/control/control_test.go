package control_test

import (
	"context"
	"errors"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

func TestControlPlaneRejectsConflictBeforeAcceptAndRecordsDeploymentProvenance(t *testing.T) {
	ctx := context.Background()
	coordinator := portyop.NewCoordinator()
	operations := &countingOperationStore{}
	deploymentStore := &capturingDeploymentStore{saved: make(chan domain.Deployment, 1)}
	runtime := &controlRuntime{}
	environment := portystack.NewEnvironmentService(controlEnvironmentStore{})
	service := portyop.NewDeploymentService(runtime, deploymentStore, coordinator)
	control := portycontrol.NewControlPlane("/srv/repository", controlLookup{}, environment, portyrepo.NewRepositoryService(controlGit{}), runtime, portyop.NewOperationService(operations, nil, time.Second, 1024), service, coordinator, nil, nil)

	release, err := coordinator.Try(false, "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.StartAction(ctx, "stk_gateway", "deploy"); !errors.Is(err, portyop.ErrOperationConflict) {
		t.Fatalf("StartAction() conflict = %v", err)
	}
	if operations.created != 0 {
		t.Fatalf("conflicting action created %d operations", operations.created)
	}
	release()

	operation, err := control.StartAction(ctx, "stk_gateway", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case deployment := <-deploymentStore.saved:
		if deployment.OperationID != operation.ID || deployment.GitCommit != "abc123" || !deployment.Dirty || deployment.DiffDigest == "" {
			t.Fatalf("deployment provenance = %#v, operation = %#v", deployment, operation)
		}
	case <-time.After(time.Second):
		t.Fatal("deployment was not recorded")
	}
}

type controlLookup struct{}

func (controlLookup) ByID(context.Context, domain.StackID) (domain.Stack, error) {
	return domain.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway"}, nil
}

type controlEnvironmentStore struct{}

func (controlEnvironmentStore) SetEnvironment(context.Context, domain.StackID, string, string) error {
	return nil
}
func (controlEnvironmentStore) DeleteEnvironment(context.Context, domain.StackID, string) error {
	return nil
}
func (controlEnvironmentStore) Environment(context.Context, domain.StackID) (map[string]string, error) {
	return map[string]string{"TOKEN": "secret"}, nil
}

type controlGit struct{}

func (controlGit) Status(context.Context) (portyrepo.GitStatus, error) {
	return portyrepo.GitStatus{Branch: "main", Dirty: true}, nil
}
func (controlGit) Head(context.Context) (string, error)                        { return "abc123", nil }
func (controlGit) Diff(context.Context, string) (string, error)                { return "diff", nil }
func (controlGit) Commit(context.Context, string, string) (string, error)      { return "abc123", nil }
func (controlGit) History(context.Context, int) ([]portyrepo.GitCommit, error) { return nil, nil }
func (controlGit) Fetch(context.Context) error                                 { return nil }
func (controlGit) PullFastForward(context.Context) error                       { return nil }
func (controlGit) Push(context.Context) error                                  { return nil }

type controlRuntime struct{}

func (*controlRuntime) Validate(context.Context, portyop.ComposeRequest) error { return nil }
func (*controlRuntime) Digest(context.Context, portyop.ComposeRequest) (string, error) {
	return "sha256:compose", nil
}
func (*controlRuntime) Status(context.Context, portyop.ComposeRequest) (string, error) {
	return `[]`, nil
}
func (*controlRuntime) Start(context.Context, portyop.ComposeRequest) error        { return nil }
func (*controlRuntime) Stop(context.Context, portyop.ComposeRequest) error         { return nil }
func (*controlRuntime) Restart(context.Context, portyop.ComposeRequest) error      { return nil }
func (*controlRuntime) Deploy(context.Context, portyop.ComposeRequest, bool) error { return nil }
func (*controlRuntime) Pull(context.Context, portyop.ComposeRequest) error         { return nil }
func (*controlRuntime) Logs(context.Context, portyop.ComposeRequest, int) (string, error) {
	return "", nil
}

type countingOperationStore struct{ created int }

func (s *countingOperationStore) CreateOperation(context.Context, domain.Operation) error {
	s.created++
	return nil
}
func (*countingOperationStore) UpdateOperation(context.Context, domain.Operation) error { return nil }

type capturingDeploymentStore struct{ saved chan domain.Deployment }

func (s *capturingDeploymentStore) SaveDeployment(_ context.Context, deployment domain.Deployment) error {
	s.saved <- deployment
	return nil
}
