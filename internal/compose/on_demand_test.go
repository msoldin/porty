package compose

import (
	"context"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type demandDockerFake struct {
	client.APIClient
	rows             []container.Summary
	containers       map[string]container.InspectResponse
	driver           string
	started, stopped []string
	stats            string
}

func (d *demandDockerFake) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{Items: d.rows}, nil
}
func (d *demandDockerFake) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: d.containers[id]}, nil
}
func (d *demandDockerFake) NetworkInspect(context.Context, string, client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	return client.NetworkInspectResult{Network: network.Inspect{Driver: d.driver}}, nil
}
func (d *demandDockerFake) ContainerStats(context.Context, string, client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(d.stats))}, nil
}
func (d *demandDockerFake) ContainerStart(_ context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	d.started = append(d.started, id)
	return client.ContainerStartResult{}, nil
}
func (d *demandDockerFake) ContainerStop(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
	d.stopped = append(d.stopped, id)
	return client.ContainerStopResult{}, nil
}
func demandFixture() (*Client, *types.Project, *demandDockerFake) {
	labels := map[string]string{api.ProjectLabel: "sample", api.ServiceLabel: "game", api.ContainerNumberLabel: "1"}
	port := network.MustParsePort("25565/udp")
	bindings := network.PortMap{port: {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "25565"}}}
	d := &demandDockerFake{driver: "bridge", rows: []container.Summary{{ID: "game-id", Labels: labels}}, containers: map[string]container.InspectResponse{"game-id": {ID: "game-id", Config: &container.Config{Labels: labels}, HostConfig: &container.HostConfig{NetworkMode: "bridge", PortBindings: bindings}, State: &container.State{Status: "running", Running: true, StartedAt: time.Now().Format(time.RFC3339Nano)}, NetworkSettings: &container.NetworkSettings{Ports: bindings, Networks: map[string]*network.EndpointSettings{"bridge": {NetworkID: "network-id"}}}}}}
	project := &types.Project{Name: "sample", Services: types.Services{"game": {Name: "game", Image: "game:local", Ports: []types.ServicePortConfig{{Target: 25565, Published: "25565", Protocol: "udp", HostIP: "127.0.0.1"}}}}}
	c := New(nil, time.Minute)
	c.demand = d
	return c, project, d
}
func TestOnDemandSnapshotRejectsAmbiguousRuntime(t *testing.T) {
	for _, mode := range []string{"missing", "scaled", "wrong ownership", "host networking", "overlay", "dynamic port", "remapped UDP", "mixed", "restart", "oneoff", "no published ports"} {
		t.Run(mode, func(t *testing.T) {
			c, p, d := demandFixture()
			v := d.containers["game-id"]
			service := p.Services["game"]
			switch mode {
			case "missing":
				d.rows = nil
			case "scaled":
				d.rows = append(d.rows, d.rows[0])
			case "wrong ownership":
				v.Config.Labels[api.ProjectLabel] = "other"
			case "host networking":
				v.HostConfig.NetworkMode = "host"
			case "overlay":
				d.driver = "overlay"
			case "dynamic port":
				service.Ports[0].Published = ""
			case "remapped UDP":
				service.Ports[0].Published = "25566"
			case "mixed":
				v.State.Running = false
				v.State.Status = "created"
			case "restart":
				v.State.Restarting = true
			case "oneoff":
				v.Config.Labels[api.OneoffLabel] = "True"
			case "no published ports":
				service.Ports = nil
				v.HostConfig.PortBindings = nil
				v.NetworkSettings.Ports = nil
			}
			p.Services["game"] = service
			d.containers["game-id"] = v
			if _, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"}); err == nil {
				t.Fatalf("unsafe %s configuration accepted", mode)
			}
		})
	}
}
func TestOnDemandSnapshotAcceptsExactStoppedContainer(t *testing.T) {
	c, p, d := demandFixture()
	v := d.containers["game-id"]
	v.State.Running = false
	v.State.Status = "exited"
	v.NetworkSettings = &container.NetworkSettings{}
	d.containers["game-id"] = v
	snapshot, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Running || len(snapshot.Containers) != 1 || snapshot.Containers[0].ID != "game-id" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}
func TestOnDemandRejectsOutsideDependentsAndOneShotDependencies(t *testing.T) {
	c, p, _ := demandFixture()
	p.Services["consumer"] = types.ServiceConfig{Name: "consumer", DependsOn: types.DependsOnConfig{"game": {Condition: types.ServiceConditionStarted}}}
	if _, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"}); err == nil {
		t.Fatal("outside dependent permitted group shutdown")
	}
	delete(p.Services, "consumer")
	s := p.Services["game"]
	s.DependsOn = types.DependsOnConfig{"db": {Condition: types.ServiceConditionCompletedSuccessfully}}
	p.Services["game"] = s
	if _, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"}); err == nil {
		t.Fatal("one-shot dependency accepted")
	}
}
func TestOnDemandReadsFreshNetworkCountersOnly(t *testing.T) {
	c, _, d := demandFixture()
	d.stats = `{"id":"game-id","read":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","networks":{"eth0":{"rx_bytes":23,"tx_bytes":45}}}`
	counts, _, err := c.OnDemandCounters(context.Background(), []string{"game-id"})
	if err != nil || counts["game-id"].Received != 23 || counts["game-id"].Sent != 45 {
		t.Fatalf("counters: %+v %v", counts, err)
	}
	for _, payload := range []string{`{}`, `{"id":"other"}`, `{"id":"game-id","read":"2000-01-01T00:00:00Z","networks":{"eth0":{"rx_bytes":1}}}`, strings.Repeat("x", (1<<20)+1)} {
		d.stats = payload
		if _, _, err := c.OnDemandCounters(context.Background(), []string{"game-id"}); err == nil {
			t.Fatal("missing, foreign, stale or oversized stats accepted")
		}
	}
}

func TestOnDemandAcceptsAlreadyRunningExternalStartedDependency(t *testing.T) {
	c, p, d := demandFixture()
	game := p.Services["game"]
	game.DependsOn = types.DependsOnConfig{"db": {Condition: types.ServiceConditionStarted}}
	p.Services["game"] = game
	p.Services["db"] = types.ServiceConfig{Name: "db", Image: "db:local"}
	labels := map[string]string{api.ProjectLabel: "sample", api.ServiceLabel: "db", api.ContainerNumberLabel: "1"}
	d.rows = append(d.rows, container.Summary{ID: "db-id", Labels: labels})
	d.containers["db-id"] = container.InspectResponse{ID: "db-id", Config: &container.Config{Labels: labels}, HostConfig: &container.HostConfig{NetworkMode: "bridge"}, State: &container.State{Running: true, Status: "running"}}
	snapshot, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ExternalDependencies) != 1 || snapshot.ExternalDependencies[0] != "db" {
		t.Fatalf("dependency evidence missing: %+v", snapshot)
	}
}

func TestOnDemandRejectsUnconfiguredPublishedEndpoint(t *testing.T) {
	c, p, d := demandFixture()
	v := d.containers["game-id"]
	v.NetworkSettings.Ports = network.PortMap{network.MustParsePort("25565/udp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "25566"}}}
	d.containers["game-id"] = v
	if _, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"}); err == nil {
		t.Fatal("runtime port outside configured publication accepted")
	}
}
