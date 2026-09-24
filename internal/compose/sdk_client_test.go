package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

type recordingCompose struct {
	api.Compose
	project    *types.Project
	options    api.UpOptions
	calls      []string
	err        error
	logMessage string
}

func (r *recordingCompose) Up(_ context.Context, project *types.Project, options api.UpOptions) error {
	r.project, r.options = project, options
	r.calls = append(r.calls, "up")
	return r.err
}

func (r *recordingCompose) Stop(context.Context, string, api.StopOptions) error {
	r.calls = append(r.calls, "stop")
	return r.err
}
func (r *recordingCompose) Restart(context.Context, string, api.RestartOptions) error {
	r.calls = append(r.calls, "restart")
	return r.err
}
func (r *recordingCompose) Pull(context.Context, *types.Project, api.PullOptions) error {
	r.calls = append(r.calls, "pull")
	return r.err
}
func (r *recordingCompose) Down(context.Context, string, api.DownOptions) error {
	r.calls = append(r.calls, "down")
	return r.err
}
func (r *recordingCompose) Ps(context.Context, string, api.PsOptions) ([]api.ContainerSummary, error) {
	r.calls = append(r.calls, "ps")
	return []api.ContainerSummary{{Service: "app"}}, r.err
}
func (r *recordingCompose) Logs(_ context.Context, _ string, consumer api.LogConsumer, _ api.LogOptions) error {
	r.calls = append(r.calls, "logs")
	consumer.Log("app", r.logMessage)
	return r.err
}

func TestDeployPassesValidatedProjectToCompose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &recordingCompose{}
	client := New(service, time.Minute)
	if err := client.Deploy(context.Background(), Request{StackDir: dir, ProjectName: "sample"}, true); err != nil {
		t.Fatal(err)
	}
	if service.project == nil || service.project.Name != "sample" || service.options.Create.Recreate != api.RecreateForce {
		t.Fatalf("SDK Up call = %+v, %+v", service.project, service.options)
	}
}

func TestInvalidProjectPreventsComposeMutation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("include:\n  - https://example.com/repo.git\nservices: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &recordingCompose{}
	client := New(service, time.Minute)
	if err := client.Deploy(context.Background(), Request{StackDir: dir, ProjectName: "sample"}, false); err == nil {
		t.Fatal("remote include accepted")
	}
	if len(service.calls) != 0 {
		t.Fatalf("daemon calls = %v", service.calls)
	}
}

func TestLifecycleActionsDispatchToSDK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &recordingCompose{logMessage: "ready"}
	client := New(service, time.Minute)
	request := Request{StackDir: dir, ProjectName: "sample"}
	if err := client.Stop(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := client.Restart(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := client.Pull(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := client.Down(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background(), request)
	if err != nil || len(status) != 1 || status[0].Service != "app" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	logs, err := client.Logs(context.Background(), request, 1)
	if err != nil || !strings.Contains(logs, "ready") {
		t.Fatalf("logs = %q, %v", logs, err)
	}
	if got := strings.Join(service.calls, ","); got != "stop,restart,pull,down,ps,logs" {
		t.Fatalf("calls = %s", got)
	}
}

func TestComposeLogsAndErrorsAreBoundedAndRedacted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &recordingCompose{logMessage: "secret" + strings.Repeat("x", maxCommandOutput), err: errors.New("denied for secret")}
	client := New(service, time.Minute)
	logs, err := client.Logs(context.Background(), Request{StackDir: dir, ProjectName: "sample", Environment: map[string]string{"TOKEN": "secret"}}, 1)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(logs, "secret") || len(logs) > maxCommandOutput {
		t.Fatalf("logs length = %d, error = %v", len(logs), err)
	}
}

type recordingContainers struct {
	client.APIClient
	calls []string
	err   error
}

func (r *recordingContainers) ContainerStart(_ context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	r.calls = append(r.calls, "start:"+id)
	return client.ContainerStartResult{}, r.err
}

func (r *recordingContainers) ContainerStop(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
	r.calls = append(r.calls, "stop:"+id)
	return client.ContainerStopResult{}, r.err
}

func (r *recordingContainers) ContainerRestart(_ context.Context, id string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	r.calls = append(r.calls, "restart:"+id)
	return client.ContainerRestartResult{}, r.err
}

func TestContainerActionTargetsOnlySelectedID(t *testing.T) {
	docker := &recordingContainers{}
	runtime := newWithContainerActions(&recordingCompose{}, docker, time.Minute)
	for _, action := range []string{"start", "stop", "restart"} {
		if err := runtime.ContainerAction(context.Background(), Request{}, "full-id-b", action); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(docker.calls, ","); got != "start:full-id-b,stop:full-id-b,restart:full-id-b" {
		t.Fatalf("container actions = %s", got)
	}
}

func TestContainerActionRedactsAndBoundsDockerError(t *testing.T) {
	docker := &recordingContainers{err: errors.New("TOKEN=secret" + strings.Repeat("x", maxCommandOutput))}
	runtime := newWithContainerActions(&recordingCompose{}, docker, time.Minute)
	err := runtime.ContainerAction(context.Background(), Request{Environment: map[string]string{"TOKEN": "secret"}}, "full-id-b", "stop")
	if err == nil {
		t.Fatal("expected Docker error")
	}
	if strings.Contains(err.Error(), "secret") || len(err.Error()) > maxCommandOutput+len("Compose: ") {
		t.Fatalf("unsafe Docker error: length = %d, contains secret = %t", len(err.Error()), strings.Contains(err.Error(), "secret"))
	}
}
