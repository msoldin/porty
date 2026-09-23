package compose

import (
	"context"
	"os"
	"testing"
	"time"

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
