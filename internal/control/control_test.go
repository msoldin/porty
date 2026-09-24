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

func TestContainersIncludeStoppedAndSeparateReplicas(t *testing.T) {
	runtime := &controlRuntime{status: []api.ContainerSummary{
		{ID: "id-b", Name: "app-2", Project: "porty-gateway", Service: "app", State: "exited"},
		{ID: "id-c", Name: "other-1", Project: "other", Service: "app", State: "running"},
		{ID: "id-a", Name: "app-1", Project: "porty-gateway", Service: "app", State: "running", Health: "unhealthy"},
	}}
	control, _ := newContainerControl(runtime, controlLookup{}, portyop.NewCoordinator())
	items, err := control.Containers(context.Background(), "stk_gateway")
	if err != nil || len(items) != 2 {
		t.Fatalf("containers = %#v, %v", items, err)
	}
	if items[0].ID != "id-a" || items[0].Health != "unhealthy" || items[1].ID != "id-b" || items[1].State != "exited" {
		t.Fatalf("containers = %#v", items)
	}
}

func TestContainerActionRejectsWrongProjectAndMissingID(t *testing.T) {
	runtime := &controlRuntime{status: []api.ContainerSummary{
		{ID: "id-a", Project: "porty-gateway", State: "running"},
		{ID: "id-b", Project: "other", State: "running"},
	}}
	control, store := newContainerControl(runtime, controlLookup{}, portyop.NewCoordinator())
	for _, id := range []string{"id-b", "missing"} {
		if _, err := control.StartContainerAction(context.Background(), "stk_gateway", id, "stop"); !errors.Is(err, portycontrol.ErrContainerNotFound) {
			t.Fatalf("action for %s = %v", id, err)
		}
	}
	if store.created != 0 {
		t.Fatalf("invalid actions created %d operations", store.created)
	}
}

func TestContainerActionRejectsIncompatibleStateAndArchivedStack(t *testing.T) {
	runtime := &controlRuntime{status: []api.ContainerSummary{
		{ID: "id-a", Project: "porty-gateway", State: "running", Health: "unhealthy"},
		{ID: "id-b", Project: "porty-gateway", State: "exited"},
		{ID: "id-c", Project: "porty-gateway", State: "paused"},
	}}
	control, store := newContainerControl(runtime, controlLookup{}, portyop.NewCoordinator())
	for _, tc := range []struct{ id, action string }{{"id-a", "start"}, {"id-b", "stop"}, {"id-c", "restart"}} {
		if _, err := control.StartContainerAction(context.Background(), "stk_gateway", tc.id, tc.action); !errors.Is(err, portycontrol.ErrContainerStateConflict) {
			t.Fatalf("%s %s = %v", tc.action, tc.id, err)
		}
	}
	if _, err := control.StartContainerAction(context.Background(), "stk_gateway", "id-a", "pull"); !errors.Is(err, portycontrol.ErrUnsupportedContainerAction) {
		t.Fatalf("unsupported action = %v", err)
	}
	if store.created != 0 {
		t.Fatalf("rejected actions created %d operations", store.created)
	}
	now := time.Now()
	archived, _ := newContainerControl(runtime, controlLookup{archivedAt: &now}, portyop.NewCoordinator())
	if _, err := archived.StartContainerAction(context.Background(), "stk_gateway", "id-a", "stop"); !errors.Is(err, portycontrol.ErrContainerArchived) {
		t.Fatalf("archived action = %v", err)
	}
}

func TestContainerActionTargetsOnlySelectedReplicaAndRespectsLock(t *testing.T) {
	runtime := &controlRuntime{status: []api.ContainerSummary{
		{ID: "id-a", Project: "porty-gateway", Service: "app", State: "running"},
		{ID: "id-b", Project: "porty-gateway", Service: "app", State: "exited"},
	}, called: make(chan string, 1)}
	coordinator := portyop.NewCoordinator()
	control, store := newContainerControl(runtime, controlLookup{}, coordinator)
	release, err := coordinator.Try(false, "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.StartContainerAction(context.Background(), "stk_gateway", "id-b", "start"); !errors.Is(err, portyop.ErrOperationConflict) {
		t.Fatalf("locked action = %v", err)
	}
	release()
	if _, err := control.StartContainerAction(context.Background(), "stk_gateway", "id-b", "start"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-runtime.called:
		if got != "start:id-b" {
			t.Fatalf("SDK action = %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("container action did not run")
	}
	if store.created != 1 {
		t.Fatalf("created operations = %d", store.created)
	}
}

func newContainerControl(runtime *controlRuntime, lookup controlLookup, coordinator *portyop.Coordinator) (*portycontrol.ControlPlane, *countingOperationStore) {
	store := &countingOperationStore{}
	control := portycontrol.NewControlPlane("/srv/repository", lookup, portystack.NewEnvironmentService(controlEnvironmentStore{}), nil, runtime, portyop.NewOperationService(store, nil, time.Second, 1024), nil, coordinator, nil, nil)
	return control, store
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

type controlLookup struct{ archivedAt *time.Time }

func (l controlLookup) ByID(context.Context, portystack.StackID) (portystack.Stack, error) {
	return portystack.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway", ArchivedAt: l.archivedAt}, nil
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

type controlRuntime struct {
	deployErr error
	status    []api.ContainerSummary
	called    chan string
}

func (*controlRuntime) Validate(context.Context, portycompose.Request) error { return nil }
func (*controlRuntime) Digest(context.Context, portycompose.Request) (string, error) {
	return "sha256:compose", nil
}
func (r *controlRuntime) Status(context.Context, portycompose.Request) ([]api.ContainerSummary, error) {
	return r.status, nil
}
func (r *controlRuntime) ContainerAction(_ context.Context, _ portycompose.Request, id, action string) error {
	if r.called != nil {
		r.called <- action + ":" + id
	}
	return nil
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
