package compose

import (
	"context"
	"fmt"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/platforms"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (c *Client) SnapshotUpdate(ctx context.Context, request Request) (UpdateSnapshot, error) {
	project, err := Load(ctx, request)
	if err != nil {
		return UpdateSnapshot{}, err
	}
	return c.snapshotProject(ctx, project, request)
}
func (c *Client) snapshotProject(ctx context.Context, project *types.Project, request Request) (UpdateSnapshot, error) {
	return c.inspectUpdateProject(ctx, project, request, false)
}
func (c *Client) inspectUpdateProject(ctx context.Context, project *types.Project, request Request, allowStarting bool) (UpdateSnapshot, error) {
	snapshot := UpdateSnapshot{Project: project, Excluded: map[string]string{}}
	reject := func(reason string) (UpdateSnapshot, error) {
		return snapshot, fmt.Errorf("%w: %s", ErrUpdateIneligible, reason)
	}
	if c.updates == nil {
		return reject("Docker inspection unavailable")
	}
	if len(project.DisabledServices) > 0 {
		return reject("profiles are unsupported")
	}
	for _, s := range project.Services {
		for _, dep := range s.DependsOn {
			if dep.Condition == types.ServiceConditionCompletedSuccessfully {
				return reject("completion dependencies are unsupported")
			}
		}
	}
	digestValue, err := Digest(project, request.Environment)
	if err != nil {
		return snapshot, err
	}
	snapshot.SourceDigest = digestValue
	list, err := c.updates.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{}.Add("label", api.ProjectLabel+"="+project.Name)})
	if err != nil {
		return reject("unable to inspect containers")
	}
	counts := map[string]int{}
	identities := map[string]string{}
	replicas := map[string]bool{}
	for _, row := range list.Items {
		if strings.EqualFold(row.Labels[api.OneoffLabel], "true") {
			return reject("one-off containers present")
		}
		inspected, err := c.updates.ContainerInspect(ctx, row.ID, client.ContainerInspectOptions{})
		if err != nil {
			return reject("container changed during inspection")
		}
		actual := inspected.Container
		if actual.ID != row.ID || actual.Config == nil || actual.State == nil {
			return reject("container ownership unavailable")
		}
		labels := actual.Config.Labels
		service, ok := project.Services[labels[api.ServiceLabel]]
		if !ok || labels[api.ProjectLabel] != project.Name || strings.EqualFold(labels[api.OneoffLabel], "true") {
			return reject("unexpected container ownership")
		}
		replica, err := strconv.Atoi(labels[api.ContainerNumberLabel])
		if err != nil || replica < 1 || replicas[service.Name+":"+strconv.Itoa(replica)] {
			return reject("replica identity unavailable")
		}
		replicas[service.Name+":"+strconv.Itoa(replica)] = true
		state := actual.State
		if !state.Running || state.Status != "running" || state.Paused || state.Restarting || state.Dead {
			return reject("stack is not fully running")
		}
		health := ""
		if state.Health != nil {
			health = string(state.Health.Status)
			if health != "healthy" && !(allowStarting && health == "starting") {
				return reject("container is not healthy")
			}
		}
		if service.HealthCheck != nil && !service.HealthCheck.Disable && len(service.HealthCheck.Test) > 0 && service.HealthCheck.Test[0] != "NONE" && health != "healthy" && !(allowStarting && health == "starting") {
			return reject("health result unavailable")
		}
		started, err := time.Parse(time.RFC3339Nano, state.StartedAt)
		if err != nil || started.IsZero() {
			return reject("start identity unavailable")
		}
		if digest.Digest(actual.Image).Validate() != nil {
			return reject("image identity unavailable")
		}
		image, err := c.updates.ImageInspect(ctx, actual.Image)
		if err != nil || image.ID != actual.Image || image.Os == "" || image.Architecture == "" {
			return reject("image platform unavailable")
		}
		platform := platforms.Format(platforms.Normalize(ocispec.Platform{OS: image.Os, Architecture: image.Architecture, Variant: image.Variant}))
		if service.Platform != "" {
			expected, err := platforms.Parse(service.Platform)
			if err != nil || !platforms.OnlyStrict(expected).Match(platforms.Normalize(ocispec.Platform{OS: image.Os, Architecture: image.Architecture, Variant: image.Variant})) {
				return reject("running platform differs from configuration")
			}
		}
		if prior := identities[service.Name]; prior != "" && prior != actual.Image {
			return reject("replicas have mixed images")
		}
		identities[service.Name] = actual.Image
		counts[service.Name]++
		reason := ""
		switch {
		case service.Build != nil:
			reason = "locally built service"
		case strings.Contains(service.Image, "@"):
			reason = "digest pinned"
		case service.PullPolicy == types.PullPolicyNever:
			reason = "pull policy never"
		case service.Image == "" || len(image.RepoDigests) == 0:
			reason = "local image without registry provenance"
		}
		if reason != "" {
			snapshot.Excluded[service.Name] = reason
		}
		snapshot.Containers = append(snapshot.Containers, UpdateContainer{ID: actual.ID, Service: service.Name, ImageID: actual.Image, State: "running", Health: health, Platform: platform, Replica: replica, RestartCount: actual.RestartCount, StartedAt: started})
	}
	for name, s := range project.Services {
		if counts[name] != s.GetScale() {
			return reject("missing or unexpected service replicas")
		}
		if s.GetScale() == 0 {
			snapshot.Excluded[name] = "scaled to zero"
		}
	}
	if len(snapshot.Containers) == 0 {
		return reject("no running services")
	}
	sort.Slice(snapshot.Containers, func(i, j int) bool { return snapshot.Containers[i].ID < snapshot.Containers[j].ID })
	return snapshot, nil
}
