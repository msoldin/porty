package operation_test

import (
	"context"
	"errors"
	portycompose "github.com/msoldin/porty/internal/compose"
	portyop "github.com/msoldin/porty/internal/operation"
	"reflect"
	"testing"
)

func TestDeployValidatesBeforeApplyingAndRecordsDigest(t *testing.T) {
	runtime := &fakeComposeRuntime{}
	store := &fakeDeploymentStore{}
	service := portyop.NewDeploymentService(runtime, store, portyop.NewCoordinator())
	request := portyop.DeployRequest{StackID: "stk_gateway", OperationID: "op_request", StackDir: "/srv/stacks/gateway", ProjectName: "porty-gateway-123", GitCommit: "abc123", Dirty: true}
	deployment, err := service.Deploy(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtime.calls, []string{"validate", "digest", "deploy"}) {
		t.Fatalf("runtime calls = %#v", runtime.calls)
	}
	if deployment.ComposeDigest != "sha256:desired" || deployment.Status != portyop.DeploymentSucceeded || len(store.saved) != 1 {
		t.Fatalf("deployment = %#v saved = %#v", deployment, store.saved)
	}
	if deployment.OperationID != "op_request" || deployment.GitCommit != "abc123" || !deployment.Dirty {
		t.Fatalf("deployment provenance = %#v", deployment)
	}
}

func TestCoordinatorRejectsConflictingStackOperation(t *testing.T) {
	coordinator := portyop.NewCoordinator()
	release, err := coordinator.Try(false, "stk_gateway")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := coordinator.Try(false, "stk_gateway"); !errors.Is(err, portyop.ErrOperationConflict) {
		t.Fatalf("second Try() = %v", err)
	}
}

type fakeComposeRuntime struct{ calls []string }

func (f *fakeComposeRuntime) Validate(context.Context, portycompose.Request) error {
	f.calls = append(f.calls, "validate")
	return nil
}
func (f *fakeComposeRuntime) Digest(context.Context, portycompose.Request) (string, error) {
	f.calls = append(f.calls, "digest")
	return "sha256:desired", nil
}
func (f *fakeComposeRuntime) Deploy(context.Context, portycompose.Request, bool) error {
	f.calls = append(f.calls, "deploy")
	return nil
}

type fakeDeploymentStore struct{ saved []portyop.Deployment }

func (f *fakeDeploymentStore) SaveDeployment(_ context.Context, deployment portyop.Deployment) error {
	f.saved = append(f.saved, deployment)
	return nil
}
