package compose

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	"io"
	"os"
	"testing"
)

// Embed only to catch unexpected SDK calls; the exercised boundary is implemented below.
type guardDocker struct {
	client.APIClient
	guard   *RuntimeGuard
	matches []string
	copyErr error
	daemon  string
}

func (d *guardDocker) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: system.Info{ID: d.daemon}}, nil
}
func (d *guardDocker) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	rows := []container.Summary{}
	for _, project := range d.matches {
		rows = append(rows, container.Summary{ID: project, State: "running", Labels: map[string]string{api.ProjectLabel: project}})
	}
	return client.ContainerListResult{Items: rows}, nil
}
func (d *guardDocker) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: container.InspectResponse{ID: id, State: &container.State{Running: true, Status: "running"}}}, nil
}
func (d *guardDocker) CopyFromContainer(_ context.Context, _ string, options client.CopyFromContainerOptions) (client.CopyFromContainerResult, error) {
	if d.copyErr != nil {
		return client.CopyFromContainerResult{}, d.copyErr
	}
	if options.SourcePath != d.guard.path {
		return client.CopyFromContainerResult{}, errors.New("unexpected marker path")
	}
	data, err := os.ReadFile(d.guard.path)
	if err != nil {
		return client.CopyFromContainerResult{}, err
	}
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: "marker", Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg})
	tw.Write(data)
	tw.Close()
	return client.CopyFromContainerResult{Content: io.NopCloser(&b)}, nil
}
func newGuardFixture(t *testing.T) (*RuntimeGuard, *guardDocker) {
	t.Helper()
	d := &guardDocker{daemon: "daemon"}
	g, err := NewRuntimeGuard(d)
	if err != nil {
		t.Fatal(err)
	}
	d.guard = g
	t.Cleanup(func() { g.Close() })
	return g, d
}
func TestSelfGuardRejectsHostingProject(t *testing.T) {
	g, d := newGuardFixture(t)
	d.matches = []string{"custom-image-custom-hostname"}
	if !errors.Is(g.CheckProject(context.Background(), d.matches[0]), ErrSelfProtected) {
		t.Fatal("hosting project not protected")
	}
}
func TestSelfGuardProtectsAllMatchingProjects(t *testing.T) {
	g, d := newGuardFixture(t)
	d.matches = []string{"a", "b"}
	for _, name := range d.matches {
		if !errors.Is(g.CheckProject(context.Background(), name), ErrSelfProtected) {
			t.Fatal(name)
		}
	}
}
func TestSelfGuardFailsClosedWhenUnverifiable(t *testing.T) {
	for _, name := range []string{"unreadable", "missing marker", "changed daemon", "missing ownership"} {
		t.Run(name, func(t *testing.T) {
			g, d := newGuardFixture(t)
			if err := g.CheckProject(context.Background(), "app"); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "unreadable":
				d.matches = []string{"a"}
				d.copyErr = errors.New("denied")
			case "missing marker":
				os.Remove(g.path)
			case "changed daemon":
				d.daemon = "other"
			case "missing ownership":
				d.matches = []string{""}
			}
			if !errors.Is(g.CheckProject(context.Background(), "app"), ErrProtectionUnavailable) {
				t.Fatal("unsafe proof accepted")
			}
		})
	}
}

func TestSelfGuardDoesNotTrustImageOrHostname(t *testing.T) {
	g, d := newGuardFixture(t)
	d.matches = []string{"porty-lookalike"}
	d.copyErr = errdefs.ErrNotFound
	if err := g.CheckProject(context.Background(), "porty-lookalike"); err != nil {
		t.Fatalf("host process with no marker matches: %v", err)
	}
	// A later membership proof must be refreshed rather than caching absence.
	d.copyErr = nil
	if !errors.Is(g.CheckProject(context.Background(), "porty-lookalike"), ErrSelfProtected) {
		t.Fatal("cached absence bypassed proof")
	}
}
