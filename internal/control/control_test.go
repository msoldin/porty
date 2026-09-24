package control_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func TestStackRuntimeActionRejectsStaleOrNeverDeployedState(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rows        []api.ContainerSummary
		hasDeployed bool
		allowed     bool
	}{
		{"stopped", []api.ContainerSummary{{Project: "porty-gateway", State: "exited"}}, true, false},
		{"unhealthy", []api.ContainerSummary{{Project: "porty-gateway", State: "running", Health: "unhealthy"}}, true, false},
		{"never deployed", []api.ContainerSummary{{Project: "porty-gateway", State: "running"}}, false, false},
		{"running and deployed", []api.ContainerSummary{{Project: "porty-gateway", State: "running"}}, true, true},
	} {
		for _, action := range []string{"stop", "restart"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				operations := &countingOperationStore{}
				runtime := &controlRuntime{status: tc.rows}
				control := portycontrol.NewControlPlane("/srv/repository", controlLookup{},
					portystack.NewEnvironmentService(controlEnvironmentStore{}), nil, runtime,
					portyop.NewOperationService(operations, nil, time.Second, 1024), nil,
					portyop.NewCoordinator(), deploymentStateStore{hasSuccessful: tc.hasDeployed}, nil)
				_, err := control.StartAction(context.Background(), "stk_gateway", action)
				if tc.allowed {
					if err != nil || operations.created != 1 {
						t.Fatalf("allowed action err=%v created=%d", err, operations.created)
					}
				} else if !errors.Is(err, portycontrol.ErrStackRuntimeActionUnavailable) || operations.created != 0 {
					t.Fatalf("rejected action err=%v created=%d", err, operations.created)
				}
			})
		}
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

func TestContainersIncludeMetadataForEachReplica(t *testing.T) {
	runtime := &controlRuntime{status: []api.ContainerSummary{
		{
			ID: "id-b", Name: "web-2", Project: "porty-gateway", Service: "web",
			Image: "example/web:2.1", Networks: []string{"front", "back"},
			Publishers: api.PortPublishers{
				{URL: "::1", TargetPort: 443, PublishedPort: 8443, Protocol: "tcp"},
				{URL: "127.0.0.1", TargetPort: 80, PublishedPort: 8080, Protocol: "tcp"},
			},
		},
		{ID: "id-a", Name: "web-1", Project: "porty-gateway", Service: "web", Image: "example/web:2.0"},
		{ID: "foreign", Name: "web-3", Project: "other", Service: "web", Image: "secret"},
	}}
	control, _ := newContainerControl(runtime, controlLookup{}, portyop.NewCoordinator())
	items, err := control.Containers(context.Background(), "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "id-a" || items[1].ID != "id-b" {
		t.Fatalf("replicas = %#v", items)
	}
	if items[0].Image != "example/web:2.0" || items[0].Networks == nil || items[0].Ports == nil {
		t.Fatalf("first replica metadata = %#v", items[0])
	}
	second := items[1]
	if second.Image != "example/web:2.1" || len(second.Networks) != 2 || second.Networks[0] != "back" || second.Networks[1] != "front" {
		t.Fatalf("second replica metadata = %#v", second)
	}
	if len(second.Ports) != 2 ||
		second.Ports[0].Host != "127.0.0.1" || second.Ports[0].TargetPort != 80 ||
		second.Ports[1].Host != "::1" || second.Ports[1].PublishedPort != 8443 {
		t.Fatalf("second replica ports = %#v", second.Ports)
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

func TestContainerBatchRejectsInvalidSelectionBeforeMutation(t *testing.T) {
	rows := []api.ContainerSummary{
		{ID: "full-id-a", Project: "porty-gateway", Service: "web", Name: "web-1", State: "running"},
		{ID: "full-id-b", Project: "porty-gateway", Service: "web", Name: "web-2", State: "exited"},
		{ID: "foreign-id", Project: "other", Service: "web", State: "running"},
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("full-id-%d", i)
	}
	for _, tc := range []struct {
		name     string
		ids      []string
		action   string
		archived bool
		want     error
	}{
		{"empty", nil, "stop", false, portycontrol.ErrInvalidContainerSelection},
		{"duplicate", []string{"full-id-a", "full-id-a"}, "stop", false, portycontrol.ErrInvalidContainerSelection},
		{"too many", many, "stop", false, portycontrol.ErrInvalidContainerSelection},
		{"blank", []string{""}, "stop", false, portycontrol.ErrInvalidContainerSelection},
		{"unsupported", []string{"full-id-a"}, "pull", false, portycontrol.ErrUnsupportedContainerAction},
		{"foreign", []string{"full-id-a", "foreign-id"}, "stop", false, portycontrol.ErrContainerNotFound},
		{"missing", []string{"full-id-a", "missing"}, "stop", false, portycontrol.ErrContainerNotFound},
		{"mixed state", []string{"full-id-a", "full-id-b"}, "stop", false, portycontrol.ErrContainerStateConflict},
		{"archived", []string{"full-id-a"}, "stop", true, portycontrol.ErrContainerArchived},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &controlRuntime{status: rows, called: make(chan string, 2)}
			lookup := controlLookup{}
			if tc.archived {
				now := time.Now()
				lookup.archivedAt = &now
			}
			control, store := newContainerControl(runtime, lookup, portyop.NewCoordinator())
			_, err := control.StartContainerBatchAction(context.Background(), "stk_gateway", tc.ids, tc.action)
			if !errors.Is(err, tc.want) || store.created != 0 || len(runtime.called) != 0 {
				t.Fatalf("err=%v created=%d SDK calls=%d", err, store.created, len(runtime.called))
			}
		})
	}
}

func TestContainerBatchTargetsSelectedReplicasAndReportsPartialFailureSafely(t *testing.T) {
	runtime := &controlRuntime{
		status: []api.ContainerSummary{
			{ID: "full-id-a", Project: "porty-gateway", Service: "web", Name: "web-1", State: "running"},
			{ID: "full-id-b", Project: "porty-gateway", Service: "web", Name: "web-2", State: "running"},
			{ID: "full-id-c", Project: "porty-gateway", Service: "web", Name: "web-3", State: "running"},
		},
		called:       make(chan string, 3),
		actionErrors: map[string]error{"full-id-b": errors.New("secret " + strings.Repeat("x", 256<<10))},
	}
	coordinator := portyop.NewCoordinator()
	control, store := newContainerControl(runtime, controlLookup{}, coordinator)
	store.updated = make(chan portyop.Operation, 4)
	release, err := coordinator.Try(false, "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.StartContainerBatchAction(context.Background(), "stk_gateway", []string{"full-id-a"}, "stop"); !errors.Is(err, portyop.ErrOperationConflict) {
		t.Fatalf("conflicting action = %v", err)
	}
	release()
	accepted, err := control.StartContainerBatchAction(context.Background(), "stk_gateway", []string{"full-id-a", "full-id-b"}, "stop")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Kind != "container_batch_stop" || store.created != 1 {
		t.Fatalf("accepted=%#v created=%d", accepted, store.created)
	}
	var completed portyop.Operation
	for {
		select {
		case updated := <-store.updated:
			if updated.Status == portyop.OperationFailed {
				completed = updated
				goto finished
			}
		case <-time.After(time.Second):
			t.Fatal("batch did not complete")
		}
	}
finished:
	if got := <-runtime.called; got != "stop:full-id-a" {
		t.Fatalf("first SDK action = %q", got)
	}
	if got := <-runtime.called; got != "stop:full-id-b" {
		t.Fatalf("second SDK action = %q", got)
	}
	if len(runtime.called) != 0 {
		t.Fatalf("unexpected SDK actions = %d", len(runtime.called))
	}
	if !strings.Contains(completed.Output, "full-id-a: succeeded") || !strings.Contains(completed.Output, "full-id-b: failed") ||
		strings.Contains(completed.Output, "secret") || strings.Contains(completed.Output, strings.Repeat("x", 100)) || len(completed.Output) > 1024 {
		t.Fatalf("unsafe batch output = %q", completed.Output)
	}
	if _, err := control.StartContainerBatchAction(context.Background(), "stk_gateway", []string{"full-id-c"}, "stop"); err != nil {
		t.Fatalf("lock remained held: %v", err)
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
	deployErr    error
	status       []api.ContainerSummary
	called       chan string
	actionErrors map[string]error
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
	return r.actionErrors[id]
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
