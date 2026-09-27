package compose

import (
	"context"
	"errors"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"iter"
	"strings"
	"testing"
)

type imageDocker struct {
	pullErr   error
	streamErr error
	client.APIClient
	resolved string
	pulled   []string
	auth     bool
	err      error
	after    string
	attempts int
}

func (d *imageDocker) DistributionInspect(_ context.Context, _ string, o client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
	d.attempts++
	d.auth = d.auth || o.EncodedRegistryAuth != ""
	return client.DistributionInspectResult{DistributionInspect: registry.DistributionInspect{Descriptor: ocispec.Descriptor{Digest: digest.Digest(d.resolved)}}}, d.err
}
func (d *imageDocker) ImagePull(_ context.Context, ref string, o client.ImagePullOptions) (client.ImagePullResponse, error) {
	d.auth = d.auth || o.RegistryAuth != "" || o.PrivilegeFunc != nil
	d.pulled = append(d.pulled, ref)
	d.resolved = "sha256:" + strings.Repeat("c", 64)
	if len(o.Platforms) != 1 || o.Platforms[0].Architecture != "amd64" {
		return nil, errors.New("missing platform")
	}
	return &pullResponse{ReadCloser: io.NopCloser(strings.NewReader("")), err: d.streamErr}, d.pullErr
}
func (d *imageDocker) ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: d.after, Os: "linux", Architecture: "amd64"}}, nil
}

type pullResponse struct {
	io.ReadCloser
	err error
}

func (r *pullResponse) Wait(context.Context) error { return r.err }
func (r *pullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(yield func(jsonstream.Message, error) bool) {}
}
func TestPreparePinsTagBeforePull(t *testing.T) {
	c, p, _ := snapshotFixture()
	before := "sha256:" + strings.Repeat("a", 64)
	target := "sha256:" + strings.Repeat("b", 64)
	d := &imageDocker{resolved: target, after: "sha256:" + strings.Repeat("d", 64)}
	c.images = d
	prepared, err := c.PrepareUpdate(context.Background(), UpdateSnapshot{Project: p, Containers: []UpdateContainer{{Service: "app", ImageID: before, Platform: "linux/amd64"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Changes) != 1 || prepared.Changes[0].TargetReference != "docker.io/library/alpine@"+target || len(d.pulled) != 1 || d.pulled[0] != prepared.Changes[0].TargetReference || d.auth {
		t.Fatalf("preparation=%+v pulls=%v auth=%v", prepared, d.pulled, d.auth)
	}
	if p.Services["app"].Image != "alpine:latest" {
		t.Fatal("source project mutated")
	}
}
func TestPrepareIgnoresOtherPlatformOnlyChange(t *testing.T) {
	c, p, _ := snapshotFixture()
	id := "sha256:" + strings.Repeat("a", 64)
	c.images = &imageDocker{resolved: "sha256:" + strings.Repeat("b", 64), after: id}
	prepared, err := c.PrepareUpdate(context.Background(), UpdateSnapshot{Project: p, Containers: []UpdateContainer{{Service: "app", ImageID: id, Platform: "linux/amd64"}}})
	if err != nil || len(prepared.Changes) != 0 {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
}
func TestPrepareRejectsPrivateAuth(t *testing.T) {
	c, p, _ := snapshotFixture()
	d := &imageDocker{err: errdefs.ErrUnauthenticated}
	c.images = d
	_, err := c.PrepareUpdate(context.Background(), UpdateSnapshot{Project: p, Containers: []UpdateContainer{{Service: "app", Platform: "linux/amd64"}}})
	if !errors.Is(err, ErrPublicImageUnavailable) || d.attempts != 1 || len(d.pulled) != 0 || d.auth {
		t.Fatalf("private auth: %v %+v", err, d)
	}
}

func TestPrepareDoesNotMutateContainersWhenPullFails(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "stream"}[stream], func(t *testing.T) {
			c, p, _ := snapshotFixture()
			service := &recordingCompose{}
			c.service = service
			d := &imageDocker{resolved: "sha256:" + strings.Repeat("b", 64)}
			if stream {
				d.streamErr = errors.New("pull stream failed")
			} else {
				d.pullErr = errors.New("pull failed")
			}
			c.images = d
			_, err := c.PrepareUpdate(context.Background(), UpdateSnapshot{Project: p, Containers: []UpdateContainer{{Service: "app", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/amd64"}}})
			if err == nil || len(service.calls) != 0 {
				t.Fatalf("pull failure mutated containers: %v %v", err, service.calls)
			}
		})
	}
}
func TestRegistryRetriesOnlyTransientFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	err := retryRegistry(ctx, func() error {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return errdefs.ErrUnavailable
	})
	if !errors.Is(err, context.Canceled) || attempts != 2 {
		t.Fatalf("retry cancellation: %v attempts=%d", err, attempts)
	}
	attempts = 0
	err = retryRegistry(context.Background(), func() error { attempts++; return errdefs.ErrResourceExhausted })
	if err == nil || attempts != 1 {
		t.Fatal("retried without Retry-After evidence")
	}
}

func TestPrepareRejectsDependencySideEffectsBeforeMutation(t *testing.T) {
	c, p, _ := snapshotFixture()
	s := p.Services["app"]
	s.DependsOn = types.DependsOnConfig{"db": {Restart: true}}
	p.Services["app"] = s
	c.images = &imageDocker{resolved: "sha256:" + strings.Repeat("b", 64), after: "sha256:" + strings.Repeat("d", 64)}
	_, err := c.PrepareUpdate(context.Background(), UpdateSnapshot{Project: p, Containers: []UpdateContainer{{Service: "app", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/amd64"}}})
	if !errors.Is(err, ErrUpdateIneligible) {
		t.Fatalf("unsupported update reached mutation admission: %v", err)
	}
}
