package compose

import (
	"context"
	"errors"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	"github.com/docker/compose/v5/pkg/api"
	"strings"
	"time"
)

var ErrUpdateApplyFailed = errors.New("automatic service recreation failed")

func (c *Client) ApplyUpdate(parent context.Context, prepared PreparedUpdate) error {
	if prepared.Snapshot.Project == nil || len(prepared.Changes) == 0 {
		return ErrUpdateIneligible
	}
	original := prepared.Snapshot.Project
	names := make([]string, 0, len(prepared.Changes))
	selected := map[string]bool{}
	for _, change := range prepared.Changes {
		s, ok := original.Services[change.Service]
		if !ok || s.GetScale() == 0 || s.Build != nil || selected[change.Service] {
			return ErrUpdateIneligible
		}
		ref, err := reference.ParseNormalizedNamed(change.TargetReference)
		if err != nil {
			return ErrUpdateIneligible
		}
		if _, ok := ref.(reference.Digested); !ok {
			return ErrUpdateIneligible
		}
		selected[change.Service] = true
		names = append(names, change.Service)
	}
	// Dependency propagation and shared namespaces are excluded in both directions.
	for name, s := range original.Services {
		if selected[name] && (len(s.PostStart) > 0 || len(s.PreStop) > 0 || len(s.Links) > 0 || len(s.ExternalLinks) > 0 || len(s.VolumesFrom) > 0) {
			return ErrUpdateIneligible
		}
		for dependency, d := range s.DependsOn {
			if d.Restart && (selected[name] || selected[dependency]) {
				return ErrUpdateIneligible
			}
		}
		for _, link := range append(append([]string{}, s.Links...), s.VolumesFrom...) {
			if selected[strings.Split(link, ":")[0]] {
				return ErrUpdateIneligible
			}
		}
		for _, mode := range []string{s.NetworkMode, s.Ipc, s.Pid} {
			if strings.HasPrefix(mode, "service:") && (selected[name] || selected[strings.TrimPrefix(mode, "service:")]) {
				return ErrUpdateIneligible
			}
			if strings.HasPrefix(mode, "container:") {
				return ErrUpdateIneligible
			}
		}
	}
	project, err := original.WithSelectedServices(names, types.IgnoreDependencies)
	if err != nil {
		return ErrUpdateIneligible
	}
	for _, change := range prepared.Changes {
		s := project.Services[change.Service]
		s.Image = change.TargetReference
		if change.Platform != "" {
			s.Platform = change.Platform
		}
		s.Build = nil
		s.PullPolicy = types.PullPolicyNever
		project.Services[change.Service] = s
	}
	if err := c.CheckProject(parent, project.Name); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	err = c.service.Up(ctx, project, api.UpOptions{Create: api.CreateOptions{Services: names, Recreate: api.RecreateDiverged, RecreateDependencies: api.RecreateNever, Inherit: true, RemoveOrphans: false, IgnoreOrphans: true}, Start: api.StartOptions{}})
	if err != nil {
		return ErrUpdateApplyFailed
	}
	return nil
}
