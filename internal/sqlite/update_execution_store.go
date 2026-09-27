package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/compose"
	op "github.com/msoldin/porty/internal/operation"
	"github.com/msoldin/porty/internal/stack"
	"time"
)

func (s *AutoUpdateStore) SavePrepared(ctx context.Context, run autoupdate.Run, operation op.Operation, prepared compose.PreparedUpdate, baseline op.Deployment) error {
	changes, err := json.Marshal(prepared.Changes)
	if err != nil {
		return err
	}
	provenance, err := json.Marshal(baseline)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE auto_update_runs SET phase='prepared',operation_id=? WHERE id=? AND phase IN ('queued','checking') AND EXISTS(SELECT 1 FROM auto_update_policies WHERE stack_id=? AND revision=? AND enabled=1 AND paused_reason='')`, operation.ID, run.ID, run.StackID, run.PolicyRevision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return autoupdate.ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO update_executions(run_id,operation_id,phase,source_digest,changes_json,baseline_json) VALUES(?,?,'prepared',?,?,?)`, run.ID, operation.ID, prepared.Snapshot.SourceDigest, string(changes), string(provenance))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AutoUpdateStore) MarkApplying(ctx context.Context, id string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE auto_update_runs SET phase='applying' WHERE id=? AND phase='prepared' AND policy_revision=? AND EXISTS(SELECT 1 FROM auto_update_policies p JOIN stacks s ON s.id=p.stack_id WHERE p.stack_id=auto_update_runs.stack_id AND p.revision=? AND p.enabled=1 AND p.paused_reason='' AND s.archived_at IS NULL)`, id, revision, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return autoupdate.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, "UPDATE update_executions SET phase='applying' WHERE run_id=? AND phase='prepared'", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AutoUpdateStore) MarkVerifying(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE auto_update_runs SET phase='verifying' WHERE id=? AND phase='applying'", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE update_executions SET phase='verifying' WHERE run_id=? AND phase='applying'", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AutoUpdateStore) PendingExecutions(ctx context.Context) ([]autoupdate.Execution, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT run_id,operation_id,phase,source_digest,changes_json,baseline_json FROM update_executions WHERE phase!='terminal' ORDER BY run_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []autoupdate.Execution{}
	for rows.Next() {
		var e autoupdate.Execution
		var changes, baseline string
		if err := rows.Scan(&e.RunID, &e.OperationID, &e.Phase, &e.SourceDigest, &changes, &baseline); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(changes), &e.Changes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(baseline), &e.Baseline); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
func (s *AutoUpdateStore) EffectiveImages(ctx context.Context, id stack.StackID) (map[string]compose.ImageChange, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT service,source_reference,target_reference,platform,image_id FROM deployment_images WHERE stack_id=?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	images := map[string]compose.ImageChange{}
	for rows.Next() {
		var image compose.ImageChange
		if err := rows.Scan(&image.Service, &image.SourceReference, &image.TargetReference, &image.Platform, &image.AfterImageID); err != nil {
			return nil, err
		}
		images[image.Service] = image
	}
	return images, rows.Err()
}
func (s *AutoUpdateStore) InvalidateImages(ctx context.Context, id stack.StackID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM deployment_images WHERE stack_id=?", id)
	return err
}
func completeUpdate(ctx context.Context, tx *sql.Tx, operation op.Operation, completion *op.UpdateCompletion) error {
	if completion == nil {
		return nil
	}
	var changesJSON, phase string
	err := tx.QueryRowContext(ctx, "SELECT changes_json,phase FROM update_executions WHERE run_id=? AND operation_id=?", completion.RunID, operation.ID).Scan(&changesJSON, &phase)
	if err != nil {
		return err
	}
	if phase == "terminal" {
		return nil
	}
	if phase != "applying" && phase != "verifying" {
		return errors.New("update has not entered mutation phase")
	}
	d := completion.Deployment
	if d.OperationID != operation.ID || string(d.StackID) != operation.ScopeID {
		return errors.New("update completion ownership mismatch")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO deployments(id,stack_id,operation_id,git_commit,dirty,diff_digest,compose_digest,status,started_at,completed_at,duration_ms,error_code) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, d.StackID, d.OperationID, nullableString(d.GitCommit), d.Dirty, nullableString(d.DiffDigest), nullableString(d.ComposeDigest), d.Status, encodeTime(d.StartedAt), nullableTime(d.CompletedAt), d.Duration.Milliseconds(), nullableString(d.ErrorCode))
	if err != nil {
		return err
	}
	outcome := "failed"
	if operation.Status == op.OperationSucceeded && d.Status == op.DeploymentSucceeded && completion.PauseReason == "" {
		outcome = "updated"
		var changes []compose.ImageChange
		if err := json.Unmarshal([]byte(changesJSON), &changes); err != nil {
			return err
		}
		for _, change := range changes {
			verified := false
			for _, result := range completion.Services {
				if result.Service == change.Service && result.Outcome == "verified" && result.ActualImageID == change.AfterImageID {
					verified = true
				}
			}
			if !verified {
				return errors.New("missing verified image result")
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO deployment_images(stack_id,service,source_reference,target_reference,platform,image_id) VALUES(?,?,?,?,?,?) ON CONFLICT(stack_id,service) DO UPDATE SET source_reference=excluded.source_reference,target_reference=excluded.target_reference,platform=excluded.platform,image_id=excluded.image_id`, d.StackID, change.Service, change.SourceReference, change.TargetReference, change.Platform, change.AfterImageID)
			if err != nil {
				return err
			}
		}
	}
	if completion.PauseReason != "" {
		if _, err := tx.ExecContext(ctx, "UPDATE auto_update_policies SET paused_reason=?,revision=revision+1 WHERE stack_id=?", completion.PauseReason, d.StackID); err != nil {
			return err
		}
	}
	results, err := json.Marshal(completion.Services)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE update_executions SET phase='terminal',results_json=? WHERE run_id=?", string(results), completion.RunID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE auto_update_runs SET phase='terminal',outcome=?,reason=? WHERE id=?", outcome, completion.PauseReason, completion.RunID)
	return err
}

func (s *AutoUpdateStore) PruneImages(ctx context.Context, id stack.StackID, sources map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT service,source_reference FROM deployment_images WHERE stack_id=?", id)
	if err != nil {
		return err
	}
	var remove []string
	for rows.Next() {
		var name, source string
		if err := rows.Scan(&name, &source); err != nil {
			rows.Close()
			return err
		}
		if sources[name] != source {
			remove = append(remove, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range remove {
		if _, err := tx.ExecContext(ctx, "DELETE FROM deployment_images WHERE stack_id=? AND service=?", id, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *AutoUpdateStore) RecoverExecution(ctx context.Context, e autoupdate.Execution, services []compose.ServiceUpdateResult) error {
	var phase string
	if err := s.db.QueryRowContext(ctx, "SELECT phase FROM update_executions WHERE run_id=?", e.RunID).Scan(&phase); err != nil {
		return err
	}
	if phase == "terminal" {
		return nil
	}
	if phase == "prepared" {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, "UPDATE update_executions SET phase='terminal' WHERE run_id=? AND phase='prepared'", e.RunID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE auto_update_runs SET phase='terminal',outcome='interrupted',reason='restart_before_mutation' WHERE id=?", e.RunID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET status='cancelled',completed_at=?,error_code='server_restarted' WHERE id=?", encodeTime(time.Now()), e.OperationID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if phase != "applying" && phase != "verifying" {
		return errors.New("unknown update recovery phase")
	}
	operationStore := NewOperationStore(s.db)
	operation, err := operationStore.Operation(ctx, e.OperationID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	operation.Status = op.OperationFailed
	operation.CompletedAt = now
	operation.ErrorCode = "update_interrupted"
	operation.Output = "Update interrupted after mutation began. Inspect and recover the stack before resuming automatic updates."
	deployment := e.Baseline
	deployment.ID = "dep_" + operation.ID
	deployment.OperationID = operation.ID
	deployment.StackID = stack.StackID(operation.ScopeID)
	deployment.ComposeDigest = e.SourceDigest
	deployment.Status = op.DeploymentFailed
	deployment.StartedAt = operation.StartedAt
	if deployment.StartedAt.IsZero() {
		deployment.StartedAt = now
	}
	deployment.CompletedAt = now
	deployment.Duration = now.Sub(deployment.StartedAt)
	deployment.ErrorCode = "update_interrupted"
	result := op.Result{Update: &op.UpdateCompletion{RunID: e.RunID, Deployment: deployment, Services: services, PauseReason: "interrupted_update"}, Alerts: []alert.Change{{Kind: "failure", Key: alert.Key{StackID: operation.ScopeID, Problem: "deployment", Target: "stack"}, StackName: operation.StackName, OccurrenceID: operation.ID, OperationID: operation.ID, Summary: "Automatic update interrupted; manual recovery required.", ObservedAt: now, CanResolveManually: true}}}
	_, err = operationStore.CompleteOperation(ctx, operation, result)
	return err
}
