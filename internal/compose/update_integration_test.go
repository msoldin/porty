package compose

import (
	"archive/tar"
	"context"
	"fmt"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This gate must run against a disposable daemon, with two distinct public
// immutable Alpine-compatible images supplied by the integration environment.
func TestUpdateLivePreservesVolumesAndUnselectedService(t *testing.T) {
	if os.Getenv("PORTY_LIVE_DOCKER_CHECK") != "1" {
		t.Skip("live Docker release gate not requested")
	}
	if os.Getenv("PORTY_DISPOSABLE_DOCKER") != "1" {
		t.Fatal("release gate requires PORTY_DISPOSABLE_DOCKER=1")
	}
	imageA, imageB := os.Getenv("PORTY_TEST_IMAGE_A"), os.Getenv("PORTY_TEST_IMAGE_B")
	if imageA == "" || imageB == "" {
		t.Fatal("provide distinct immutable PORTY_TEST_IMAGE_A and PORTY_TEST_IMAGE_B")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c, resources, err := NewDockerClient(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	name := fmt.Sprintf("porty-update-test-%d", time.Now().UnixNano())
	dir := t.TempDir()
	fixture := `services:
  app:
    image: ${TEST_IMAGE}
    command: ["sh", "-c", "test -f /named/marker || hostname > /named/marker; test -f /anonymous/marker || hostname > /anonymous/marker; sleep 3600"]
    volumes:
      - data:/named
      - /anonymous
  db:
    image: ${TEST_IMAGE}
    command: ["sleep", "3600"]
volumes:
  data: {}
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	request := Request{StackDir: dir, ProjectName: name, Environment: map[string]string{"TEST_IMAGE": imageA}}
	project, err := Load(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := c.service.Down(cleanup, name, api.DownOptions{Project: project, Volumes: true, RemoveOrphans: true}); err != nil {
			t.Errorf("fixture cleanup: %v", err)
		}
	}()
	if err := c.Pull(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Deploy(ctx, request, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := c.SnapshotUpdate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	sdk := c.updates.(client.APIClient)
	markers := map[string]string{}
	original := map[string]string{}
	for _, row := range snapshot.Containers {
		original[row.Service] = row.ID
		if row.Service == "app" {
			for _, path := range []string{"/named/marker", "/anonymous/marker"} {
				markers[path] = liveMarker(t, ctx, sdk, row.ID, path)
			}
		}
	}
	pull, err := c.images.ImagePull(ctx, imageB, client.ImagePullOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer pull.Close()
	if err := pull.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	target, err := c.images.ImageInspect(ctx, imageB)
	if err != nil {
		t.Fatal(err)
	}
	if target.ID == snapshot.Containers[0].ImageID {
		t.Fatal("test images have the same runnable identity")
	}
	prepared := PreparedUpdate{Snapshot: snapshot, Changes: []ImageChange{{Service: "app", SourceReference: imageA, TargetReference: imageB, AfterImageID: target.ID}}}
	if err := c.ApplyUpdate(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := c.VerifyUpdate(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	after, err := c.SnapshotUpdate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range after.Containers {
		if row.Service == "db" && row.ID != original["db"] {
			t.Fatal("unselected service recreated")
		}
		if row.Service == "app" {
			if row.ID == original["app"] {
				t.Fatal("selected service not recreated")
			}
			for path, want := range markers {
				if got := liveMarker(t, ctx, sdk, row.ID, path); got != want {
					t.Fatalf("volume data changed at %s", path)
				}
			}
		}
	}
	if _, err := sdk.ContainerStop(ctx, original["db"], client.ContainerStopOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SnapshotUpdate(ctx, request); err == nil {
		t.Fatal("stopped dependency admitted")
	}
	inspected, err := sdk.ContainerInspect(ctx, original["db"], client.ContainerInspectOptions{})
	if err != nil || inspected.Container.State.Running {
		t.Fatalf("stopped dependency was started: %v", err)
	}
}
func liveMarker(t *testing.T, ctx context.Context, sdk client.APIClient, id, path string) string {
	t.Helper()
	copy, err := sdk.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Content.Close()
	archive := tar.NewReader(io.LimitReader(copy.Content, 8192))
	if _, err := archive.Next(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(io.LimitReader(archive, 256))
	if err != nil || len(data) == 0 {
		t.Fatalf("missing marker: %v", err)
	}
	return string(data)
}
