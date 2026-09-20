package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
)

func TestControlPlaneRejectsConflictBeforeAcceptAndRecordsDeploymentProvenance(t *testing.T) {
	ctx := context.Background()
	coordinator := application.NewCoordinator()
	operations := &countingOperationStore{}
	deploymentStore := &capturingDeploymentStore{saved: make(chan domain.Deployment, 1)}
	runtime := &controlRuntime{}
	environment := application.NewEnvironmentService(controlEnvironmentStore{})
	service := application.NewDeploymentService(runtime, deploymentStore, coordinator)
	control := application.NewControlPlane("/srv/repository", controlLookup{}, environment, application.NewRepositoryService(controlGit{}), runtime, application.NewOperationService(operations, nil, time.Second, 1024), service, coordinator)

	release, err := coordinator.Try(false, "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.StartAction(ctx, "stk_gateway", "deploy"); !errors.Is(err, application.ErrOperationConflict) {
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

func (controlGit) Status(context.Context) (domain.GitStatus, error) {
	return domain.GitStatus{Branch: "main", Dirty: true}, nil
}
func (controlGit) Head(context.Context) (string, error)                     { return "abc123", nil }
func (controlGit) Diff(context.Context, string) (string, error)             { return "diff", nil }
func (controlGit) Commit(context.Context, string, string) (string, error)   { return "abc123", nil }
func (controlGit) History(context.Context, int) ([]domain.GitCommit, error) { return nil, nil }
func (controlGit) Fetch(context.Context) error                              { return nil }
func (controlGit) PullFastForward(context.Context) error                    { return nil }
func (controlGit) Push(context.Context) error                               { return nil }

type controlRuntime struct{}

func (*controlRuntime) Validate(context.Context, application.ComposeRequest) error { return nil }
func (*controlRuntime) Digest(context.Context, application.ComposeRequest) (string, error) {
	return "sha256:compose", nil
}
func (*controlRuntime) Status(context.Context, application.ComposeRequest) (string, error) {
	return `[]`, nil
}
func (*controlRuntime) Start(context.Context, application.ComposeRequest) error        { return nil }
func (*controlRuntime) Stop(context.Context, application.ComposeRequest) error         { return nil }
func (*controlRuntime) Restart(context.Context, application.ComposeRequest) error      { return nil }
func (*controlRuntime) Deploy(context.Context, application.ComposeRequest, bool) error { return nil }
func (*controlRuntime) Pull(context.Context, application.ComposeRequest) error         { return nil }
func (*controlRuntime) Logs(context.Context, application.ComposeRequest, int) (string, error) {
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
