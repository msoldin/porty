package compose

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// StartOnDemand starts only the inspected IDs, in dependency order. It never
// creates containers or pulls images. A partial failure requires reconciliation.
func (c *Client) StartOnDemand(parent context.Context, s OnDemandSnapshot, timeout time.Duration) error {
	if timeout < 30*time.Second || timeout > 15*time.Minute || s.Running {
		return ErrOnDemandIneligible
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := c.preflightOnDemand(ctx, s); err != nil {
		return err
	}
	for _, member := range s.Containers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		actual, err := c.inspectOnDemandIdentity(ctx, s.ProjectName, member)
		if err != nil {
			return err
		}
		if actual.State.Running || actual.State.Status != "exited" {
			return ErrOnDemandIneligible
		}
		if _, err := c.demand.ContainerStart(ctx, member.ID, client.ContainerStartOptions{}); err != nil {
			return fmt.Errorf("start existing container: %w", err)
		}
		for {
			actual, err := c.inspectOnDemandIdentity(ctx, s.ProjectName, member)
			if err != nil {
				return err
			}
			if onDemandReady(actual, member.Healthcheck) {
				break
			}
			if actual.State.Dead || actual.State.Restarting || actual.State.Status == "exited" || actual.State.Health != nil && actual.State.Health.Status == container.Unhealthy {
				return ErrOnDemandIneligible
			}
			if err := (realUpdateClock{}).Wait(ctx, 250*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

// StopOnDemand uses Docker's normal stop signal and bounded grace period, in
// reverse dependency order. Volumes and container identities are preserved.
func (c *Client) StopOnDemand(ctx context.Context, s OnDemandSnapshot, grace int) error {
	if grace < 10 || grace > 120 || !s.Running {
		return ErrOnDemandIneligible
	}
	if err := c.preflightOnDemand(ctx, s); err != nil {
		return err
	}
	for i := len(s.Containers) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		member := s.Containers[i]
		if _, err := c.inspectOnDemandIdentity(ctx, s.ProjectName, member); err != nil {
			return err
		}
		if _, err := c.demand.ContainerStop(ctx, member.ID, client.ContainerStopOptions{Timeout: &grace}); err != nil {
			return fmt.Errorf("stop existing container: %w", err)
		}
		actual, err := c.inspectOnDemandIdentity(ctx, s.ProjectName, member)
		if err != nil {
			return err
		}
		if actual.State.Running || actual.State.Status != "exited" {
			return ErrOnDemandIneligible
		}
	}
	return nil
}
func (c *Client) preflightOnDemand(ctx context.Context, s OnDemandSnapshot) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.demand == nil || s.ProjectName == "" || len(s.Containers) < 1 || len(s.Containers) > 8 {
		return ErrOnDemandIneligible
	}
	seen := map[string]bool{}
	for _, member := range s.Containers {
		if member.ID == "" || seen[member.ID] {
			return ErrOnDemandIneligible
		}
		seen[member.ID] = true
		actual, err := c.inspectOnDemandIdentity(ctx, s.ProjectName, member)
		if err != nil {
			return err
		}
		if actual.State.Running != s.Running || s.Running && !onDemandReady(actual, member.Healthcheck) || !s.Running && actual.State.Status != "exited" {
			return ErrOnDemandIneligible
		}
		if s.Running && (actual.State.StartedAt != member.StartedAt || actual.RestartCount != member.RestartCount) {
			return ErrOnDemandIneligible
		}
	}
	return nil
}
func (c *Client) inspectOnDemandIdentity(ctx context.Context, project string, member OnDemandContainer) (container.InspectResponse, error) {
	result, err := c.demand.ContainerInspect(ctx, member.ID, client.ContainerInspectOptions{})
	actual := result.Container
	if err != nil {
		return actual, err
	}
	if actual.ID != member.ID || actual.Image != member.ImageID || actual.Config == nil || actual.State == nil || actual.HostConfig == nil {
		return actual, ErrOnDemandIneligible
	}
	labels := actual.Config.Labels
	if labels[api.ProjectLabel] != project || labels[api.ServiceLabel] != member.Service || labels[api.ContainerNumberLabel] != "1" || strings.EqualFold(labels[api.OneoffLabel], "true") || actual.State.Paused || actual.State.Dead {
		return actual, ErrOnDemandIneligible
	}
	return actual, nil
}
