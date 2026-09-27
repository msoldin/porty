package compose

import (
	"context"
	"errors"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"net"
	"sort"
	"time"
)

var ErrPublicImageUnavailable = errors.New("public image unavailable; private registries are unsupported")
var ErrImageCheckFailed = errors.New("unable to verify public image update")

func (c *Client) PrepareUpdate(parent context.Context, snapshot UpdateSnapshot) (PreparedUpdate, error) {
	prepared := PreparedUpdate{Snapshot: snapshot}
	if c.images == nil || snapshot.Project == nil {
		return prepared, ErrImageCheckFailed
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	names := snapshot.Project.ServiceNames()
	sort.Strings(names)
	for _, name := range names {
		if snapshot.Excluded[name] != "" {
			continue
		}
		service := snapshot.Project.Services[name]
		var running *UpdateContainer
		for i := range snapshot.Containers {
			if snapshot.Containers[i].Service == name {
				running = &snapshot.Containers[i]
				break
			}
		}
		if running == nil {
			continue
		}
		parsed, err := reference.ParseNormalizedNamed(service.Image)
		if err != nil {
			return prepared, ErrImageCheckFailed
		}
		if _, ok := parsed.(reference.Digested); ok {
			continue
		}
		parsed = reference.TagNameOnly(parsed)
		platform, err := platforms.Parse(running.Platform)
		if err != nil {
			return prepared, ErrImageCheckFailed
		}
		var descriptor ocispec.Descriptor
		err = retryRegistry(ctx, func() error {
			result, err := c.images.DistributionInspect(ctx, parsed.String(), client.DistributionInspectOptions{})
			if err == nil {
				descriptor = result.Descriptor
			}
			return err
		})
		if err != nil {
			return prepared, safeImageError(err)
		}
		if descriptor.Digest.Validate() != nil {
			return prepared, ErrImageCheckFailed
		}
		immutable, err := reference.WithDigest(reference.TrimNamed(parsed), descriptor.Digest)
		if err != nil {
			return prepared, ErrImageCheckFailed
		}
		err = retryRegistry(ctx, func() error {
			response, err := c.images.ImagePull(ctx, immutable.String(), client.ImagePullOptions{Platforms: []ocispec.Platform{platform}})
			if err != nil {
				return err
			}
			if response == nil {
				return ErrImageCheckFailed
			}
			defer response.Close()
			return response.Wait(ctx)
		})
		if err != nil {
			return prepared, safeImageError(err)
		}
		selected, err := c.images.ImageInspect(ctx, immutable.String(), client.ImageInspectWithPlatform(&platform))
		if err != nil {
			return prepared, ErrImageCheckFailed
		}
		actualPlatform := platforms.Normalize(ocispec.Platform{OS: selected.Os, Architecture: selected.Architecture, Variant: selected.Variant})
		if !platforms.OnlyStrict(platform).Match(actualPlatform) || digest.Digest(selected.ID).Validate() != nil {
			return prepared, ErrImageCheckFailed
		}
		manifest := descriptor.Digest.String()
		// Some stores expose an index as the top-level identity. Resolve its selected
		// manifest explicitly instead of comparing that index to a runnable image ID.
		if selected.Descriptor != nil && selected.ID == selected.Descriptor.Digest.String() {
			all, err := c.images.ImageInspect(ctx, immutable.String(), client.ImageInspectWithManifests(true))
			if err != nil {
				return prepared, ErrImageCheckFailed
			}
			matches := 0
			for _, candidate := range all.Manifests {
				if candidate.Kind != image.ManifestKindImage || !candidate.Available || candidate.ImageData == nil || !platforms.OnlyStrict(platform).Match(candidate.ImageData.Platform) {
					continue
				}
				matches++
				manifest = candidate.Descriptor.Digest.String()
			}
			if matches != 1 || digest.Digest(manifest).Validate() != nil {
				return prepared, ErrImageCheckFailed
			}
			ref, err := reference.WithDigest(reference.TrimNamed(parsed), digest.Digest(manifest))
			if err != nil {
				return prepared, ErrImageCheckFailed
			}
			selected, err = c.images.ImageInspect(ctx, ref.String(), client.ImageInspectWithPlatform(&platform))
			if err != nil || digest.Digest(selected.ID).Validate() != nil || selected.ID == manifest {
				return prepared, ErrImageCheckFailed
			}
		}
		if selected.ID == running.ImageID {
			continue
		}
		prepared.Changes = append(prepared.Changes, ImageChange{Service: name, SourceReference: service.Image, TargetReference: immutable.String(), Platform: running.Platform, BeforeImageID: running.ImageID, AfterImageID: selected.ID, ManifestDigest: manifest})
	}
	selected := make(map[string]bool, len(prepared.Changes))
	for _, change := range prepared.Changes {
		selected[change.Service] = true
	}
	if len(selected) > 0 {
		if err := validateUpdateSelection(snapshot.Project, selected); err != nil {
			return prepared, err
		}
	}
	return prepared, nil
}
func safeImageError(err error) error {
	if errdefs.IsUnauthorized(err) || errdefs.IsPermissionDenied(err) {
		return ErrPublicImageUnavailable
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrImageCheckFailed
}
func retryRegistry(ctx context.Context, run func() error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = run()
		if err == nil || !transientRegistry(err) || attempt == 2 {
			return err
		}
		delay := time.Duration(attempt+1) * time.Second
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
func transientRegistry(err error) bool {
	// The SDK does not expose Retry-After. A rate limit waits for the next cron
	// occurrence rather than risking a retry earlier than the registry permits.
	if errdefs.IsResourceExhausted(err) || errdefs.IsUnauthorized(err) || errdefs.IsPermissionDenied(err) {
		return false
	}
	var stream *jsonstream.Error
	if errors.As(err, &stream) {
		return stream.Code >= 500 && stream.Code < 600
	}
	var network net.Error
	return errors.As(err, &network) || client.IsErrConnectionFailed(err) || errdefs.IsUnavailable(err) || errdefs.IsInternal(err)
}
