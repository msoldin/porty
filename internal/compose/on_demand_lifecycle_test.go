package compose

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

type demandLifecycleDocker struct {
	*demandDockerFake
	fail  string
	calls []string
}

func (d *demandLifecycleDocker) ContainerStart(_ context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	d.calls = append(d.calls, "start:"+id)
	if id == d.fail {
		return client.ContainerStartResult{}, errors.New("start failed")
	}
	v := d.containers[id]
	v.State.Running = true
	v.State.Status = "running"
	d.containers[id] = v
	return client.ContainerStartResult{}, nil
}
func (d *demandLifecycleDocker) ContainerStop(_ context.Context, id string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	d.calls = append(d.calls, "stop:"+id)
	if options.Timeout == nil || *options.Timeout != 30 {
		return client.ContainerStopResult{}, errors.New("wrong grace")
	}
	v := d.containers[id]
	v.State.Running = false
	v.State.Status = "exited"
	d.containers[id] = v
	return client.ContainerStopResult{}, nil
}
func TestOnDemandLifecycleUsesExactContainers(t *testing.T) {
	c, p, d := demandFixture()
	snapshot, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &demandLifecycleDocker{demandDockerFake: d}
	c.demand = runtime
	if err := c.StopOnDemand(context.Background(), snapshot, 30); err != nil {
		t.Fatal(err)
	}
	snapshot.Running = false
	snapshot.Containers[0].Running = false
	if err := c.StartOnDemand(context.Background(), snapshot, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(runtime.calls) != 2 || runtime.calls[0] != "stop:game-id" || runtime.calls[1] != "start:game-id" {
		t.Fatalf("wrong mutations: %v", runtime.calls)
	}
}
func TestOnDemandLifecycleRejectsChangedOwnershipBeforeMutation(t *testing.T) {
	c, p, d := demandFixture()
	snapshot, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &demandLifecycleDocker{demandDockerFake: d}
	c.demand = runtime
	d.containers["game-id"].Config.Labels[api.ProjectLabel] = "other"
	if err := c.StopOnDemand(context.Background(), snapshot, 30); err == nil {
		t.Fatal("changed ownership accepted")
	}
	if len(runtime.calls) != 0 {
		t.Fatalf("mutated foreign container: %v", runtime.calls)
	}
}
func TestOnDemandLifecycleHonorsCanceledContext(t *testing.T) {
	c, p, d := demandFixture()
	snapshot, err := c.inspectOnDemandProject(context.Background(), p, Request{}, []string{"game"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &demandLifecycleDocker{demandDockerFake: d}
	c.demand = runtime
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.StopOnDemand(ctx, snapshot, 30); err == nil {
		t.Fatal("canceled mutation accepted")
	}
	if len(runtime.calls) != 0 {
		t.Fatal("mutated after cancellation")
	}
}
