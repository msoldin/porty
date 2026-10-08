package compose

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type demandHostDocker struct {
	*guardDocker
	mode     container.NetworkMode
	security []string
}

func (d *demandHostDocker) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: system.Info{ID: d.daemon, OSType: "linux", SecurityOptions: d.security}}, nil
}
func (d *demandHostDocker) DaemonHost() string { return "unix:///var/run/docker.sock" }
func (d *demandHostDocker) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: container.InspectResponse{ID: id, State: &container.State{Running: true}, HostConfig: &container.HostConfig{NetworkMode: d.mode}}}, nil
}
func TestOnDemandHostRequiresProvenHostNetworking(t *testing.T) {
	for _, mode := range []container.NetworkMode{"host", "bridge"} {
		t.Run(string(mode), func(t *testing.T) {
			g, d := newGuardFixture(t)
			d.matches = []string{"porty"}
			g.docker = &demandHostDocker{guardDocker: d, mode: mode}
			err := g.CheckOnDemandHost(context.Background(), "game")
			if (err == nil) != (mode == "host") {
				t.Fatalf("mode %s: %v", mode, err)
			}
		})
	}
}
func TestOnDemandHostRejectsRootlessAndSelf(t *testing.T) {
	g, d := newGuardFixture(t)
	d.matches = []string{"porty"}
	g.docker = &demandHostDocker{guardDocker: d, mode: "host", security: []string{"name=rootless"}}
	if err := g.CheckOnDemandHost(context.Background(), "game"); err == nil {
		t.Fatal("rootless accepted")
	}
	if err := g.CheckOnDemandHost(context.Background(), "porty"); err == nil {
		t.Fatal("self stop permitted")
	}
}

func TestOnDemandHostAcceptsProvenStandalonePortyContainer(t *testing.T) {
	g, d := newGuardFixture(t)
	d.matches = []string{""}
	g.docker = &demandHostDocker{guardDocker: d, mode: "host"}
	if err := g.CheckOnDemandHost(context.Background(), "game"); err != nil {
		t.Fatalf("standalone host-network Porty rejected: %v", err)
	}
}
