package compose

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrUpdateVerification = errors.New("automatic update verification failed")

type updateClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}
type realUpdateClock struct{}

func (realUpdateClock) Now() time.Time { return time.Now() }
func (realUpdateClock) Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (c *Client) VerifyUpdate(parent context.Context, prepared PreparedUpdate) (UpdateResult, error) {
	result := UpdateResult{RecoveryRequired: true}
	selected := map[string]ImageChange{}
	for _, change := range prepared.Changes {
		selected[change.Service] = change
		result.Services = append(result.Services, ServiceUpdateResult{Service: change.Service, BeforeImageID: change.BeforeImageID, TargetImageID: change.AfterImageID, Outcome: "unverified"})
	}
	if prepared.Snapshot.Project == nil || len(selected) == 0 {
		return result, ErrUpdateVerification
	}
	clock := c.updateClock
	if clock == nil {
		clock = realUpdateClock{}
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	deadline := clock.Now().Add(5 * time.Minute)
	stableSince := clock.Now()
	fingerprint := ""
	unchanged := map[string]bool{}
	for _, row := range prepared.Snapshot.Containers {
		if _, ok := selected[row.Service]; !ok {
			unchanged[row.ID] = true
		}
	}
	for {
		if ctx.Err() != nil || !clock.Now().Before(deadline) {
			return result, ErrUpdateVerification
		}
		snapshot, err := c.inspectUpdateProject(ctx, prepared.Snapshot.Project, Request{}, true)
		for i := range result.Services {
			for _, row := range snapshot.Containers {
				if row.Service == result.Services[i].Service {
					result.Services[i].ActualImageID = row.ImageID
				}
			}
		}
		if err != nil {
			return result, ErrUpdateVerification
		}
		seen := map[string]bool{}
		var identity []string
		needsStability := false
		healthPending := false
		for _, row := range snapshot.Containers {
			change, updated := selected[row.Service]
			if !updated {
				if !unchanged[row.ID] {
					return result, ErrUpdateVerification
				}
				seen[row.ID] = true
				continue
			}
			if row.ImageID != change.AfterImageID {
				return result, ErrUpdateVerification
			}
			if row.Health == "starting" {
				healthPending = true
			}
			if row.Health == "" {
				needsStability = true
				identity = append(identity, fmt.Sprintf("%s:%d:%s", row.ID, row.RestartCount, row.StartedAt.UTC().Format(time.RFC3339Nano)))
			}
		}
		if len(seen) != len(unchanged) {
			return result, ErrUpdateVerification
		}
		sort.Strings(identity)
		current := strings.Join(identity, "|")
		if current != fingerprint {
			fingerprint = current
			stableSince = clock.Now()
		}
		if !healthPending && (!needsStability || clock.Now().Sub(stableSince) >= 30*time.Second) {
			for i := range result.Services {
				result.Services[i].Outcome = "verified"
			}
			result.RecoveryRequired = false
			return result, nil
		}
		if err := clock.Wait(ctx, time.Second); err != nil {
			return result, ErrUpdateVerification
		}
	}
}
