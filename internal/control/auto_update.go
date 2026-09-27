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

type UpdateRuntime interface {
	SnapshotUpdate(context.Context, compose.Request) (compose.UpdateSnapshot, error)
	PrepareUpdate(context.Context, compose.UpdateSnapshot) (compose.PreparedUpdate, error)
	ApplyUpdate(context.Context, compose.PreparedUpdate) error
	VerifyUpdate(context.Context, compose.PreparedUpdate) (compose.UpdateResult, error)
	CheckProject(context.Context, string) error
}
type UpdateStore interface {
	autoupdate.Store
	SavePrepared(context.Context, autoupdate.Run, op.Operation, compose.PreparedUpdate, op.Deployment) error
	MarkApplying(context.Context, string, int64) error
	MarkVerifying(context.Context, string) error
	FinishRun(context.Context, string, string, string, []alert.Change) error
	EffectiveImages(context.Context, stack.StackID) (map[string]compose.ImageChange, error)
	InvalidateImages(context.Context, stack.StackID) error
	PruneImages(context.Context, stack.StackID, map[string]string) error
	PendingExecutions(context.Context) ([]autoupdate.Execution, error)
	RecoverExecution(context.Context, autoupdate.Execution, []compose.ServiceUpdateResult) error
}

func (c *ControlPlane) SetUpdateStore(store UpdateStore) { c.updateStore = store }

type updateEvidence struct {
	Stack       stack.Stack
	Snapshot    compose.UpdateSnapshot
	Baseline    op.Deployment
	Head, Diff  string
	Environment map[string]string
	Policy      autoupdate.Policy
}

func (c *ControlPlane) updateEvidence(ctx context.Context, run autoupdate.Run, runtime UpdateRuntime) (updateEvidence, error) {
	var e updateEvidence
	var err error
	e.Policy, err = c.updateStore.GetPolicy(ctx, run.StackID)
	if err != nil {
		return e, err
	}
	if !e.Policy.Enabled || e.Policy.PausedReason != "" || e.Policy.Revision != run.PolicyRevision {
		return e, autoupdate.ErrConflict
	}
	e.Stack, err = c.lookup.ByID(ctx, run.StackID)
	if err != nil {
		return e, err
	}
	if e.Stack.ArchivedAt != nil {
		return e, compose.ErrUpdateIneligible
	}
	if err := runtime.CheckProject(ctx, e.Stack.ComposeProjectName); err != nil {
		return e, err
	}
	if c.stateStore == nil {
		return e, compose.ErrUpdateIneligible
	}
	e.Baseline, err = c.stateStore.LatestDeployment(ctx, run.StackID)
	if err != nil {
		return e, err
	}
	if e.Baseline.Status != op.DeploymentSucceeded {
		return e, compose.ErrUpdateIneligible
	}
	e.Environment, err = c.environment.Values(ctx, run.StackID)
	if err != nil {
		return e, err
	}
	request := compose.Request{StackDir: filepath.Join(c.root, e.Stack.DirectoryName), ProjectName: e.Stack.ComposeProjectName, Environment: e.Environment}
	e.Snapshot, err = runtime.SnapshotUpdate(ctx, request)
	if err != nil {
		return e, err
	}
	if e.Snapshot.SourceDigest != e.Baseline.ComposeDigest {
		return e, compose.ErrUpdateIneligible
	}
	e.Head, err = c.repository.Head(ctx)
	if err != nil {
		return e, err
	}
	status, err := c.repository.Status(ctx)
	if err != nil {
		return e, err
	}
	if status.Dirty {
		diff, err := c.repository.Diff(ctx, e.Stack.DirectoryName)
		if err != nil {
			return e, err
		}
		e.Diff = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(diff)))
	}
	if e.Head != e.Baseline.GitCommit || e.Diff != e.Baseline.DiffDigest {
		return e, compose.ErrUpdateIneligible
	}
	return e, nil
}
func sameUpdateEvidence(a, b updateEvidence) bool {
	return reflect.DeepEqual(a.Stack, b.Stack) && a.Policy.Revision == b.Policy.Revision && a.Snapshot.SourceDigest == b.Snapshot.SourceDigest && reflect.DeepEqual(a.Snapshot.Containers, b.Snapshot.Containers) && reflect.DeepEqual(a.Environment, b.Environment) && a.Baseline.ID == b.Baseline.ID && a.Head == b.Head && a.Diff == b.Diff
}
func (c *ControlPlane) CheckAndUpdate(parent context.Context, run autoupdate.Run) error {
	if c.updateStore == nil || c.updatesBlocked.Load() {
		return autoupdate.ErrUnavailable
	}
	runtime, ok := c.runtime.(UpdateRuntime)
	if !ok {
		return autoupdate.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	release, err := c.coordinator.Try(false, string(run.StackID))
	if err != nil {
		return c.finishUpdateCheck(run, "skipped", "busy", nil)
	}
	evidence, err := c.updateEvidence(ctx, run, runtime)
	release()
	if err != nil {
		return c.finishUpdateCheck(run, "skipped", eligibilityReason(err), nil)
	}
	checkKey := alert.Key{StackID: string(run.StackID), Problem: "update_check", Target: "stack"}
	deploymentKey := alert.Key{StackID: string(run.StackID), Problem: "deployment", Target: "stack"}
	revisions, err := c.previousAlerts(ctx, []alert.Key{checkKey, deploymentKey})
	if err != nil {
		return err
	}
	request := op.OperationRequest{ID: op.NewOperationID(), Kind: "auto_update", ScopeType: "stack", ScopeID: string(run.StackID), Trigger: "scheduled", StackName: run.StackName, Secrets: mapValues(evidence.Environment)}
	prepared, err := runtime.PrepareUpdate(ctx, evidence.Snapshot)
	if errors.Is(err, compose.ErrUpdateIneligible) {
		return c.finishUpdateCheck(run, "skipped", "unsupported_configuration", nil)
	}
	if err != nil {
		return c.finishUpdateCheck(run, "failed", "image_check_failed", observedChange(op.OperationRequest{ID: run.ID, StackName: run.StackName}, checkKey, 0, compose.ErrImageCheckFailed, false))
	}
	release, err = c.coordinator.Try(false, string(run.StackID))
	if err != nil {
		return c.finishUpdateCheck(run, "skipped", "busy", nil)
	}
	fresh, err := c.updateEvidence(ctx, run, runtime)
	if err != nil || !sameUpdateEvidence(evidence, fresh) {
		release()
		return c.finishUpdateCheck(run, "skipped", "changed_during_check", nil)
	}
	recovery := observedChange(op.OperationRequest{ID: run.ID, StackName: run.StackName}, checkKey, revisions[checkKey], nil, true)
	if len(prepared.Changes) == 0 {
		defer release()
		return c.finishUpdateCheck(run, "unchanged", "", recovery)
	}
	if deadline, ok := ctx.Deadline(); ok {
		request.Timeout = time.Until(deadline)
	}
	if request.Timeout <= 0 {
		release()
		return c.finishUpdateCheck(run, "skipped", "expired", nil)
	}
	operation, err := c.operations.StartTracked(ctx, request, func(jobCtx context.Context) op.Result {
		intentOp := op.Operation{ID: request.ID, ScopeID: string(run.StackID)}
		if err := c.updateStore.SavePrepared(jobCtx, run, intentOp, prepared, evidence.Baseline); err != nil {
			if !errors.Is(err, autoupdate.ErrConflict) {
				c.updatesBlocked.Store(true)
			}
			finishErr := c.finishUpdateCheck(run, "skipped", "intent_not_saved", nil)
			return op.Result{Err: errors.Join(err, finishErr)}
		}
		if err := c.updateStore.MarkApplying(jobCtx, run.ID, run.PolicyRevision); err != nil {
			if !errors.Is(err, autoupdate.ErrConflict) {
				c.updatesBlocked.Store(true)
			}
			return op.Result{Output: "Update discarded before mutation.", Err: c.finishUpdateCheck(run, "skipped", "policy_changed", nil)}
		}
		started := time.Now().UTC()
		applyErr := runtime.ApplyUpdate(jobCtx, prepared)
		if applyErr == nil {
			if err := c.updateStore.MarkVerifying(jobCtx, run.ID); err != nil {
				c.updatesBlocked.Store(true)
				applyErr = err
			}
		}
		verified, verifyErr := runtime.VerifyUpdate(jobCtx, prepared)
		mutationErr := errors.Join(applyErr, verifyErr)
		if verified.RecoveryRequired && mutationErr == nil {
			mutationErr = compose.ErrUpdateVerification
		}
		completed := time.Now().UTC()
		d := op.Deployment{ID: "dep_" + request.ID, StackID: run.StackID, OperationID: request.ID, GitCommit: evidence.Head, Dirty: evidence.Baseline.Dirty, DiffDigest: evidence.Diff, ComposeDigest: evidence.Snapshot.SourceDigest, Status: op.DeploymentSucceeded, StartedAt: started, CompletedAt: completed, Duration: completed.Sub(started)}
		completion := &op.UpdateCompletion{RunID: run.ID, Deployment: d, Services: verified.Services}
		result := op.Result{Update: completion, Output: "Automatic image update verified.", Alerts: recovery}
		if mutationErr != nil {
			completion.PauseReason = "recovery_required"
			completion.Deployment.Status = op.DeploymentFailed
			completion.Deployment.ErrorCode = "auto_update_failed"
			result.Err = compose.ErrUpdateVerification
			result.Output = "Automatic update failed after recreation began. Verify runtime state and recover manually before resuming."
		}
		result.Alerts = append(result.Alerts, observedChange(request, deploymentKey, revisions[deploymentKey], mutationErr, mutationErr == nil)...)
		return result
	}, release)
	if err != nil {
		release()
		return c.finishUpdateCheck(run, "failed", "operation_not_accepted", nil)
	}
	// Accepted mutation finishes independently of the caller, including shutdown.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), request.Timeout+10*time.Second)
	defer waitCancel()
	_, err = c.operations.Wait(waitCtx, operation.ID)
	if err != nil {
		c.updatesBlocked.Store(true)
	}
	return err
}
func eligibilityReason(err error) string {
	switch {
	case errors.Is(err, compose.ErrSelfProtected):
		return "hosting_stack"
	case errors.Is(err, compose.ErrProtectionUnavailable):
		return "protection_unavailable"
	case errors.Is(err, autoupdate.ErrConflict):
		return "policy_changed"
	case errors.Is(err, sql.ErrNoRows):
		return "no_deployed_baseline"
	default:
		return "stack_not_eligible"
	}
}
func (c *ControlPlane) finishUpdateCheck(run autoupdate.Run, outcome, reason string, changes []alert.Change) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.updateStore.FinishRun(ctx, run.ID, outcome, reason, changes); err != nil {
		c.updatesBlocked.Store(true)
		return err
	}
	if publisher, ok := c.logs.(alert.Publisher); ok && c.alerts != nil {
		for _, change := range changes {
			if item, err := c.alerts.Find(ctx, change.Key); err == nil {
				publisher.PublishAlert(item)
			}
		}
	}
	return nil
}

func (c *ControlPlane) manualImageOverrides(ctx context.Context, id stack.StackID, request compose.Request) (map[string]string, map[string]string, map[string]string, error) {
	if c.updateStore == nil {
		return nil, nil, nil, nil
	}
	images, err := c.updateStore.EffectiveImages(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	project, err := compose.Load(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}
	sources := map[string]string{}
	overrides := map[string]string{}
	platforms := map[string]string{}
	for name, service := range project.Services {
		if service.Build != nil {
			continue
		}
		sources[name] = service.Image
		if prior, ok := images[name]; ok && service.Platform != "" && service.Platform != prior.Platform {
			sources[name] = ""
		}
		if image, ok := images[name]; ok && image.SourceReference == service.Image && (service.Platform == "" || service.Platform == image.Platform) {
			overrides[name] = image.TargetReference
			platforms[name] = image.Platform
		}
	}
	return sources, overrides, platforms, nil
}

func (c *ControlPlane) SuspendUpdates() { c.updatesBlocked.Store(true) }
func (c *ControlPlane) UpdatesAvailable() bool {
	return c.updateStore != nil && !c.updatesBlocked.Load()
}
