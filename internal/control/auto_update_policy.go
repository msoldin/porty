package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/compose"
	op "github.com/msoldin/porty/internal/operation"
	"github.com/msoldin/porty/internal/stack"
	"path/filepath"
	"reflect"
	"time"
)

func (c *ControlPlane) EnableAutoUpdate(parent context.Context, id stack.StackID) error {
	if !c.UpdatesAvailable() {
		return autoupdate.ErrUnavailable
	}
	s, err := c.lookup.ByID(parent, id)
	if err != nil {
		return err
	}
	if s.ArchivedAt != nil {
		return sql.ErrNoRows
	}
	runtime, ok := c.runtime.(UpdateRuntime)
	if !ok {
		return autoupdate.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	return runtime.CheckProject(ctx, s.ComposeProjectName)
}
func (c *ControlPlane) InspectAutoUpdate(ctx context.Context, id stack.StackID) (autoupdate.Eligibility, error) {
	status := autoupdate.Eligibility{Excluded: map[string]string{}}
	s, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return status, err
	}
	if err := c.EnableAutoUpdate(ctx, id); err != nil {
		status.AvailabilityReason = eligibilityReason(err)
		return status, nil
	}
	status.Available = true
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return status, err
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	snapshot, err := c.runtime.(UpdateRuntime).SnapshotUpdate(inspectCtx, compose.Request{StackDir: filepath.Join(c.root, s.DirectoryName), ProjectName: s.ComposeProjectName, Environment: values})
	if err != nil {
		status.EligibilityReason = "Requires a fully running, healthy stack with supported Compose configuration."
		return status, nil
	}
	status.Excluded = snapshot.Excluded
	if c.stateStore == nil {
		status.EligibilityReason = "No successful deployment baseline."
		return status, nil
	}
	baseline, err := c.stateStore.LatestDeployment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		status.EligibilityReason = "No successful deployment baseline."
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Eligible = baseline.Status == op.DeploymentSucceeded && baseline.ComposeDigest == snapshot.SourceDigest
	if !status.Eligible {
		status.EligibilityReason = "Deploy pending configuration and recover failed deployments before updating."
	}
	return status, nil
}
func (c *ControlPlane) ResumeAutoUpdate(parent context.Context, id stack.StackID, revision int64, actor string) error {
	if err := c.EnableAutoUpdate(parent, id); err != nil {
		return err
	}
	release, err := c.coordinator.Try(false, string(id))
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	policy, err := c.updateStore.GetPolicy(ctx, id)
	if err != nil {
		return err
	}
	if policy.Revision != revision || policy.PausedReason == "" {
		return autoupdate.ErrConflict
	}
	baselineStore, ok := c.stateStore.(interface {
		LatestSuccessfulDeployment(context.Context, stack.StackID) (op.Deployment, error)
	})
	if !ok {
		return autoupdate.ErrUnavailable
	}
	baseline, err := baselineStore.LatestSuccessfulDeployment(ctx, id)
	if err != nil {
		return err
	}
	s, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return err
	}
	runtime := c.runtime.(UpdateRuntime)
	snapshot, err := runtime.SnapshotUpdate(ctx, compose.Request{StackDir: filepath.Join(c.root, s.DirectoryName), ProjectName: s.ComposeProjectName, Environment: values})
	if err != nil {
		return err
	}
	if snapshot.SourceDigest != baseline.ComposeDigest {
		return compose.ErrUpdateIneligible
	}
	head, err := c.repository.Head(ctx)
	if err != nil {
		return err
	}
	status, err := c.repository.Status(ctx)
	if err != nil {
		return err
	}
	diffDigest := ""
	if status.Dirty {
		diff, err := c.repository.Diff(ctx, s.DirectoryName)
		if err != nil {
			return err
		}
		diffDigest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(diff)))
	}
	if head != baseline.GitCommit || diffDigest != baseline.DiffDigest {
		return compose.ErrUpdateIneligible
	}

	// Verify the current image of every service. This observes health/stability but
	// never starts or recreates anything, even after a partially applied update.
	var verification []compose.ImageChange
	var selections []compose.ImageChange
	seen := map[string]bool{}
	for _, row := range snapshot.Containers {
		if seen[row.Service] {
			continue
		}
		seen[row.Service] = true
		change := compose.ImageChange{Service: row.Service, SourceReference: snapshot.Project.Services[row.Service].Image, TargetReference: row.ImageID, AfterImageID: row.ImageID, Platform: row.Platform}
		verification = append(verification, change)
		if snapshot.Excluded[row.Service] == "" {
			selections = append(selections, change)
		}
	}
	if _, err := runtime.VerifyUpdate(ctx, compose.PreparedUpdate{Snapshot: snapshot, Changes: verification}); err != nil {
		return err
	}
	freshValues, err := c.environment.Values(ctx, id)
	if err != nil {
		return err
	}
	fresh, err := runtime.SnapshotUpdate(ctx, compose.Request{StackDir: filepath.Join(c.root, s.DirectoryName), ProjectName: s.ComposeProjectName, Environment: freshValues})
	if err != nil {
		return err
	}
	if fresh.SourceDigest != snapshot.SourceDigest || !reflect.DeepEqual(fresh.Containers, snapshot.Containers) || !reflect.DeepEqual(freshValues, values) {
		return compose.ErrUpdateIneligible
	}
	key := alert.Key{StackID: string(id), Problem: "deployment", Target: "stack"}
	revisions, err := c.previousAlerts(ctx, []alert.Key{key})
	if err != nil {
		return err
	}
	baseline.ID = "dep_" + op.NewOperationID()
	baseline.OperationID = op.NewOperationID()
	baseline.StartedAt = time.Now().UTC()
	baseline.CompletedAt = baseline.StartedAt
	baseline.Duration = 0
	baseline.Status = op.DeploymentSucceeded
	baseline.ErrorCode = ""
	changes := observedChange(op.OperationRequest{ID: baseline.OperationID, StackName: s.DirectoryName}, key, revisions[key], nil, true)
	store, ok := c.updateStore.(interface {
		ResumePolicy(context.Context, stack.StackID, int64, time.Time, string, op.Deployment, []compose.ImageChange, []alert.Change) error
	})
	if !ok {
		return autoupdate.ErrUnavailable
	}
	if err := store.ResumePolicy(ctx, id, revision, time.Now().UTC(), actor, baseline, selections, changes); err != nil {
		return err
	}
	if publisher, ok := c.logs.(alert.Publisher); ok && c.alerts != nil {
		if item, err := c.alerts.Find(ctx, key); err == nil {
			publisher.PublishAlert(item)
		}
	}
	return nil
}
