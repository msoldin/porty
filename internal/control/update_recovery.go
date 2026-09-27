package control

import (
	"context"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/compose"
	"path/filepath"
)

// ReconcileUpdates only inspects runtime state. A healthy snapshot cannot prove
// that an interrupted verification completed, so mutation phases remain paused.
func (c *ControlPlane) ReconcileUpdates(ctx context.Context) error {
	c.updatesBlocked.Store(true)
	if c.updateStore == nil {
		return autoupdate.ErrUnavailable
	}
	executions, err := c.updateStore.PendingExecutions(ctx)
	if err != nil {
		return err
	}
	runs, err := c.updateStore.PendingRuns(ctx)
	if err != nil {
		return err
	}
	byID := map[string]autoupdate.Run{}
	for _, run := range runs {
		byID[run.ID] = run
	}
	for _, execution := range executions {
		var services []compose.ServiceUpdateResult
		for _, change := range execution.Changes {
			services = append(services, compose.ServiceUpdateResult{Service: change.Service, BeforeImageID: change.BeforeImageID, TargetImageID: change.AfterImageID, Outcome: "interrupted"})
		}
		if runtime, ok := c.runtime.(UpdateRuntime); ok && execution.Phase != "prepared" {
			if run, ok := byID[execution.RunID]; ok {
				s, stackErr := c.lookup.ByID(ctx, run.StackID)
				environment, envErr := c.environment.Values(ctx, run.StackID)
				if stackErr == nil && envErr == nil {
					snapshot, _ := runtime.SnapshotUpdate(ctx, compose.Request{StackDir: filepath.Join(c.root, s.DirectoryName), ProjectName: s.ComposeProjectName, Environment: environment})
					for i := range services {
						for _, row := range snapshot.Containers {
							if row.Service == services[i].Service {
								services[i].ActualImageID = row.ImageID
							}
						}
					}
				}
			}
		}
		if err := c.updateStore.RecoverExecution(ctx, execution, services); err != nil {
			return err
		}
		delete(byID, execution.RunID)
	}
	for _, run := range byID {
		if err := c.updateStore.FinishRun(ctx, run.ID, "interrupted", "restart_before_mutation", nil); err != nil {
			return err
		}
	}
	c.updatesBlocked.Store(false)
	return nil
}
