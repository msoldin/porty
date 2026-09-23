package control_test

import (
	"context"
	"errors"
	portycompose "github.com/msoldin/porty/internal/compose"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	"testing"
	"time"
)

func TestControlPlaneRejectsConflictBeforeAcceptAndRecordsDeploymentProvenance(t *testing.T) {
	ctx := context.Background()
	coordinator := portyop.NewCoordinator()
	operations := &countingOperationStore{}
	deploymentStore := &capturingDeploymentStore{saved: make(chan portyop.Deployment, 1)}
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

func (controlLookup) ByID(context.Context, portystack.StackID) (portystack.Stack, error) {
	return portystack.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway"}, nil
}

type controlEnvironmentStore struct{}

func (controlEnvironmentStore) SetEnvironment(context.Context, portystack.StackID, string, string) error {
	return nil
}
func (controlEnvironmentStore) DeleteEnvironment(context.Context, portystack.StackID, string) error {
	return nil
}
func (controlEnvironmentStore) Environment(context.Context, portystack.StackID) (map[string]string, error) {
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

func (*controlRuntime) Validate(context.Context, portycompose.Request) error { return nil }
func (*controlRuntime) Digest(context.Context, portycompose.Request) (string, error) {
	return "sha256:compose", nil
}
func (*controlRuntime) Status(context.Context, portycompose.Request) (string, error) {
	return `[]`, nil
}
func (*controlRuntime) Start(context.Context, portycompose.Request) error        { return nil }
func (*controlRuntime) Stop(context.Context, portycompose.Request) error         { return nil }
func (*controlRuntime) Restart(context.Context, portycompose.Request) error      { return nil }
func (*controlRuntime) Deploy(context.Context, portycompose.Request, bool) error { return nil }
func (*controlRuntime) Pull(context.Context, portycompose.Request) error         { return nil }
func (*controlRuntime) Logs(context.Context, portycompose.Request, int) (string, error) {
	return "", nil
}

type countingOperationStore struct{ created int }

func (s *countingOperationStore) CreateOperation(context.Context, portyop.Operation) error {
	s.created++
	return nil
}
func (*countingOperationStore) UpdateOperation(context.Context, portyop.Operation) error { return nil }

type capturingDeploymentStore struct{ saved chan portyop.Deployment }

func (s *capturingDeploymentStore) SaveDeployment(_ context.Context, deployment portyop.Deployment) error {
	s.saved <- deployment
	return nil
}
