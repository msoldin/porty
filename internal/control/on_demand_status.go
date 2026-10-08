package control

import (
	"context"
	"slices"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/stack"
)

// Sleeping is a presentation of an intentional stop, not a Docker runtime state.
// Verify the entire group's saved membership before classifying any member.
func (c *ControlPlane) sleepingContainerIDs(ctx context.Context, id stack.StackID, rows []api.ContainerSummary) (map[string]bool, error) {
	if c.onDemand == nil {
		return nil, nil
	}
	groups, err := c.onDemand.store.ListGroups(ctx, id)
	if err != nil {
		return nil, err
	}
	sleeping := map[string]bool{}
	for _, g := range groups {
		if g.StackID != id || !g.Enabled || g.Phase != ondemand.Sleeping || g.HoldReason != "" || g.PausedReason != "" || len(g.Members) == 0 || len(g.Members) != len(g.Evidence.ContainerIDs) {
			continue
		}
		members := map[string]string{}
		valid := true
		for _, row := range rows {
			if !slices.Contains(g.Members, row.Service) {
				continue
			}
			if members[row.Service] != "" || !strings.EqualFold(string(row.State), "exited") || !slices.Contains(g.Evidence.ContainerIDs, row.ID) {
				valid = false
				break
			}
			members[row.Service] = row.ID
		}
		if valid && len(members) == len(g.Members) {
			for _, containerID := range members {
				sleeping[containerID] = true
			}
		}
	}
	return sleeping, nil
}
