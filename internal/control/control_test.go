package control_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	portycompose "github.com/msoldin/porty/internal/compose"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
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

func TestFailedDeploymentReportsComposeError(t *testing.T) {
	coordinator := portyop.NewCoordinator()
	operations := &countingOperationStore{updated: make(chan portyop.Operation, 4)}
	runtime := &controlRuntime{deployErr: errors.New("cannot start Compose service")}
	environment := portystack.NewEnvironmentService(controlEnvironmentStore{})
	service := portyop.NewDeploymentService(runtime, &capturingDeploymentStore{saved: make(chan portyop.Deployment, 1)}, coordinator)
	control := portycontrol.NewControlPlane("/srv/repository", controlLookup{}, environment, portyrepo.NewRepositoryService(controlGit{}), runtime, portyop.NewOperationService(operations, nil, time.Second, 1024), service, coordinator, nil, nil)
	if _, err := control.StartAction(context.Background(), "stk_gateway", "deploy"); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case operation := <-operations.updated:
			if operation.Status != portyop.OperationFailed {
				continue
			}
			if operation.Output != "cannot start Compose service" {
				t.Fatalf("failed deployment output = %q", operation.Output)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("failed operation was not recorded")
		}
	}
}

func TestStackStateReportsPriorSuccessfulDeploymentAfterLatestFailure(t *testing.T) {
	runtime := &controlRuntime{}
	environment := portystack.NewEnvironmentService(controlEnvironmentStore{})
	stateStore := deploymentStateStore{
		latest:        portyop.Deployment{Status: portyop.DeploymentFailed},
		hasSuccessful: true,
	}
	control := portycontrol.NewControlPlane("/srv/repository", controlLookup{}, environment, nil, runtime, nil, nil, nil, stateStore, nil)
	state, err := control.StackState(context.Background(), "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if !state.HasDeployed || state.Freshness != portycontrol.DeploymentUnverifiable {
		t.Fatalf("StackState() = %#v", state)
	}
}

type deploymentStateStore struct {
	latest        portyop.Deployment
	hasSuccessful bool
}

func (s deploymentStateStore) LatestDeployment(context.Context, portystack.StackID) (portyop.Deployment, error) {
	return s.latest, nil
}

func (s deploymentStateStore) HasSuccessfulDeployment(context.Context, portystack.StackID) (bool, error) {
	return s.hasSuccessful, nil
}

type controlLookup struct{}

func (controlLookup) ByID(context.Context, portystack.StackID) (portystack.Stack, error) {
	return portystack.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway"}, nil
}

type controlEnvironmentStore struct{}

func (controlEnvironmentStore) SetEnvironment(context.Context, portystack.StackID, string, string) error {
	return nil
}
func (controlEnvironmentStore) SetEnvironmentWithSecret(context.Context, portystack.StackID, string, string, bool) error {
	return nil
}
func (controlEnvironmentStore) EnvironmentValue(context.Context, portystack.StackID, string) (portystack.EnvironmentValue, error) {
	return portystack.EnvironmentValue{Value: "secret"}, nil
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

type controlRuntime struct{ deployErr error }

func (*controlRuntime) Validate(context.Context, portycompose.Request) error { return nil }
func (*controlRuntime) Digest(context.Context, portycompose.Request) (string, error) {
	return "sha256:compose", nil
}
func (*controlRuntime) Status(context.Context, portycompose.Request) ([]api.ContainerSummary, error) {
	return []api.ContainerSummary{}, nil
}
func (*controlRuntime) Start(context.Context, portycompose.Request) error   { return nil }
func (*controlRuntime) Stop(context.Context, portycompose.Request) error    { return nil }
func (*controlRuntime) Restart(context.Context, portycompose.Request) error { return nil }
func (r *controlRuntime) Deploy(context.Context, portycompose.Request, bool) error {
	return r.deployErr
}
func (*controlRuntime) Pull(context.Context, portycompose.Request) error { return nil }
func (*controlRuntime) Logs(context.Context, portycompose.Request, int) (string, error) {
	return "", nil
}

type countingOperationStore struct {
	created int
	updated chan portyop.Operation
}

func (s *countingOperationStore) CreateOperation(context.Context, portyop.Operation) error {
	s.created++
	return nil
}
func (s *countingOperationStore) UpdateOperation(_ context.Context, operation portyop.Operation) error {
	if s.updated != nil {
		s.updated <- operation
	}
	return nil
}

type capturingDeploymentStore struct{ saved chan portyop.Deployment }

func (s *capturingDeploymentStore) SaveDeployment(_ context.Context, deployment portyop.Deployment) error {
	s.saved <- deployment
	return nil
}
