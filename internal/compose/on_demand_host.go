package compose

import (
	"context"
	"runtime"
	"strings"

	"github.com/moby/moby/client"
)

func (c *Client) CheckOnDemandHost(ctx context.Context, project string) error {
	guard, ok := c.guard.(interface {
		CheckOnDemandHost(context.Context, string) error
	})
	if !ok {
		return ErrProtectionUnavailable
	}
	return guard.CheckOnDemandHost(ctx, project)
}

// CheckOnDemandHost extends the private-marker proof used for self protection.
// A hostname or image name is never proof that a container is this process.
func (g *RuntimeGuard) CheckOnDemandHost(ctx context.Context, project string) error {
	if runtime.GOOS != "linux" || project == "" {
		return ErrOnDemandIneligible
	}
	if err := g.checkProject(ctx, project, true); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	endpoint, ok := g.docker.(interface{ DaemonHost() string })
	if !ok || !strings.HasPrefix(endpoint.DaemonHost(), "unix://") {
		return ErrOnDemandIneligible
	}
	info, err := g.docker.Info(ctx, client.InfoOptions{})
	if err != nil || info.Info.ID != g.daemon || info.Info.OSType != "linux" {
		return ErrProtectionUnavailable
	}
	for _, option := range info.Info.SecurityOptions {
		if strings.Contains(option, "rootless") {
			return ErrOnDemandIneligible
		}
	}
	if len(g.matchedContainers) > 1 {
		return ErrProtectionUnavailable
	}
	if len(g.matchedContainers) == 1 {
		id := g.matchedContainers[0]
		result, err := g.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		c := result.Container
		if err != nil || c.ID != id || c.State == nil || !c.State.Running || c.HostConfig == nil || c.HostConfig.NetworkMode != "host" {
			return ErrOnDemandIneligible
		}
		return nil
	}
	return verifyNativeDockerNetwork(ctx, strings.TrimPrefix(endpoint.DaemonHost(), "unix://"))
}
