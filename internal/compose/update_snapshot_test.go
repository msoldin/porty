package compose

import (
	"context"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"strings"
	"testing"
	"time"
)

type snapshotDocker struct {
	client.APIClient
	rows      []container.Summary
	inspected container.InspectResponse
	image     image.InspectResponse
}

func (d *snapshotDocker) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{Items: d.rows}, nil
}
func (d *snapshotDocker) ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: d.inspected}, nil
}
func (d *snapshotDocker) ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return client.ImageInspectResult{InspectResponse: d.image}, nil
}
func snapshotFixture() (*Client, *types.Project, *snapshotDocker) {
	labels := map[string]string{api.ProjectLabel: "sample", api.ServiceLabel: "app", api.ContainerNumberLabel: "1"}
	d := &snapshotDocker{rows: []container.Summary{{ID: "c1", Labels: labels}}, inspected: container.InspectResponse{ID: "c1", Image: "sha256:" + strings.Repeat("a", 64), Config: &container.Config{Labels: labels}, State: &container.State{Status: "running", Running: true, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}}, image: image.InspectResponse{ID: "sha256:" + strings.Repeat("a", 64), RepoDigests: []string{"alpine@sha256:" + strings.Repeat("b", 64)}, Os: "linux", Architecture: "amd64"}}
	c := New(nil, time.Minute)
	c.updates = d
	return c, &types.Project{Name: "sample", Services: types.Services{"app": {Name: "app", Image: "alpine:latest"}}}, d
}
func TestSnapshotRejectsIncompleteRuntime(t *testing.T) {
	for _, name := range []string{"missing", "stopped", "restarting", "unhealthy", "starting", "paused", "wrong project", "missing replica", "invalid image"} {
		t.Run(name, func(t *testing.T) {
			c, p, d := snapshotFixture()
			switch name {
			case "missing":
				d.rows = nil
			case "stopped":
				d.inspected.State.Running = false
				d.inspected.State.Status = "exited"
			case "restarting":
				d.inspected.State.Restarting = true
			case "paused":
				d.inspected.State.Paused = true
			case "unhealthy", "starting":
				d.inspected.State.Health = &container.Health{Status: container.HealthStatus(name)}
			case "wrong project":
				d.inspected.Config.Labels[api.ProjectLabel] = "other"
			case "missing replica":
				n := 2
				s := p.Services["app"]
				s.Scale = &n
				p.Services["app"] = s
			case "invalid image":
				d.inspected.Image = ""
			}
			if _, err := c.snapshotProject(context.Background(), p, Request{}); err == nil {
				t.Fatal("unsafe runtime accepted")
			}
		})
	}
}
func TestSnapshotExcludesUnsupportedImages(t *testing.T) {
	for _, name := range []string{"build", "local", "pinned", "never"} {
		t.Run(name, func(t *testing.T) {
			c, p, d := snapshotFixture()
			s := p.Services["app"]
			switch name {
			case "build":
				s.Build = &types.BuildConfig{}
			case "local":
				d.image.RepoDigests = nil
			case "pinned":
				s.Image = "alpine@sha256:" + strings.Repeat("a", 64)
			case "never":
				s.PullPolicy = types.PullPolicyNever
			}
			p.Services["app"] = s
			snapshot, err := c.snapshotProject(context.Background(), p, Request{})
			if err != nil || snapshot.Excluded["app"] == "" {
				t.Fatalf("snapshot=%+v err=%v", snapshot, err)
			}
		})
	}
}
