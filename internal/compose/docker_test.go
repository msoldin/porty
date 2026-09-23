package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/compose/v5/pkg/api"
	composeengine "github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/client"
)

func TestDockerBridgeUsesProvidedClient(t *testing.T) {
	api, err := client.New(client.WithHost("unix:///nonexistent/porty-docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	bridge := dockerBridge{api: api}
	if bridge.Client() != api {
		t.Fatal("bridge returned another Docker client")
	}
	if enabled, err := bridge.BuildKitEnabled(); err != nil || enabled {
		t.Fatalf("BuildKitEnabled() = %t, %v", enabled, err)
	}
	if _, err := composeengine.NewComposeService(bridge); err != nil {
		t.Fatal(err)
	}
}

func TestDockerBridgeDisablesExecutableCredentialHelpers(t *testing.T) {
	source := configfile.New("")
	source.CredentialsStore = "secretservice"
	source.CredentialHelpers = map[string]string{"example.com": "pass"}
	source.AuthConfigs["inline.example"] = types.AuthConfig{Username: "admin", Password: "token"}
	safe := safeDockerConfig(source)
	if safe.CredentialsStore != "" || len(safe.CredentialHelpers) != 0 {
		t.Fatalf("helper configuration survived: %+v", safe)
	}
	if safe.AuthConfigs["inline.example"].Password != "token" {
		t.Fatal("inline credentials were lost")
	}
	if source.CredentialsStore != "secretservice" {
		t.Fatal("source configuration was mutated")
	}
}

func TestDockerBridgeQueriesLiveDaemon(t *testing.T) {
	if os.Getenv("PORTY_LIVE_DOCKER_CHECK") != "1" {
		t.Skip("requires a disposable Docker daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service, closer, err := newDockerService(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	if _, err := service.Ps(ctx, "porty-sdk-bridge-check", api.PsOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestDockerBridgeStartsWithoutDaemon(t *testing.T) {
	service, closer, err := newDockerService(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	if service == nil {
		t.Fatal("missing Compose service")
	}
}

func TestDockerServiceLifecycleOnDisposableDaemon(t *testing.T) {
	if os.Getenv("PORTY_LIVE_DOCKER_CHECK") != "1" {
		t.Skip("requires a disposable Docker daemon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client, closer, err := NewDockerClient(ctx, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	dir := t.TempDir()
	request := Request{StackDir: dir, ProjectName: "porty_sdk_lifecycle"}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine:3.22\n    command: [sh, -c, 'echo porty-ready; sleep 60']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer client.Down(context.Background(), request)
	t.Setenv("PATH", "")
	if err := client.Pull(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := client.Deploy(ctx, request, false); err != nil {
		t.Fatal(err)
	}
	containers, err := client.Status(ctx, request)
	if err != nil || len(containers) != 1 {
		t.Fatalf("containers = %+v, %v", containers, err)
	}
	if _, err := client.Logs(ctx, request, 20); err != nil {
		t.Fatal(err)
	}
	if err := client.Stop(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := client.Restart(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := client.Down(ctx, request); err != nil {
		t.Fatal(err)
	}
}
