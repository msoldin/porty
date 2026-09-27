package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/stack"
	"time"
)

type AutoUpdateStore struct{ db *sql.DB }

func NewAutoUpdateStore(db *sql.DB) *AutoUpdateStore { return &AutoUpdateStore{db: db} }

const policyColumns = "p.stack_id,p.enabled,p.expression,p.revision,p.next_run_at,p.paused_reason"

func scanPolicy(row rowScanner) (autoupdate.Policy, error) {
	var p autoupdate.Policy
	var next int64
	err := row.Scan(&p.StackID, &p.Enabled, &p.Expression, &p.Revision, &next, &p.PausedReason)
	if next != 0 {
		p.NextRunAt = time.Unix(0, next).UTC()
	}
	return p, err
}
func (s *AutoUpdateStore) GetPolicy(ctx context.Context, id stack.StackID) (autoupdate.Policy, error) {
	p, err := scanPolicy(s.db.QueryRowContext(ctx, "SELECT "+policyColumns+" FROM auto_update_policies p WHERE p.stack_id=?", id))
	if !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM stacks WHERE id=?", id).Scan(&exists); err != nil {
		return p, err
	}
	return autoupdate.Policy{StackID: id, Expression: autoupdate.DefaultExpression}, nil
}
func (s *AutoUpdateStore) SavePolicy(ctx context.Context, id stack.StackID, update autoupdate.PolicyUpdate, now time.Time) (autoupdate.Policy, error) {
	next, err := autoupdate.NextRun(update.Expression, now)
	if err != nil {
		return autoupdate.Policy{}, err
	}
	if update.ExpectedRevision < 0 {
		return autoupdate.Policy{}, autoupdate.ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return autoupdate.Policy{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL", id).Scan(&exists); err != nil {
		return autoupdate.Policy{}, err
	}
	var result sql.Result
	if update.ExpectedRevision == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO auto_update_policies(stack_id,enabled,expression,revision,next_run_at) VALUES(?,?,?,1,?) ON CONFLICT DO NOTHING`, id, update.Enabled, update.Expression, next.UnixNano())
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE auto_update_policies SET enabled=?,expression=?,revision=revision+1,next_run_at=? WHERE stack_id=? AND revision=?`, update.Enabled, update.Expression, next.UnixNano(), id, update.ExpectedRevision)
	}
	if err != nil {
		return autoupdate.Policy{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return autoupdate.Policy{}, err
	}
	if n != 1 {
		return autoupdate.Policy{}, autoupdate.ErrConflict
	}
	p, err := scanPolicy(tx.QueryRowContext(ctx, "SELECT "+policyColumns+" FROM auto_update_policies p WHERE p.stack_id=?", id))
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (s *AutoUpdateStore) ListDue(ctx context.Context, now time.Time, limit int) ([]autoupdate.Policy, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+policyColumns+` FROM auto_update_policies p JOIN stacks s ON s.id=p.stack_id WHERE p.enabled=1 AND p.paused_reason='' AND s.archived_at IS NULL AND p.next_run_at<=? ORDER BY p.next_run_at,p.last_started_at,p.stack_id LIMIT ?`, now.UnixNano(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []autoupdate.Policy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
func (s *AutoUpdateStore) Admit(ctx context.Context, p autoupdate.Policy, now time.Time) (autoupdate.Run, bool, error) {
	if !p.Enabled || p.PausedReason != "" || p.NextRunAt.IsZero() || p.NextRunAt.After(now) {
		return autoupdate.Run{}, false, nil
	}
	next, err := autoupdate.NextRun(p.Expression, now)
	if err != nil {
		return autoupdate.Run{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return autoupdate.Run{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE auto_update_policies SET next_run_at=? WHERE stack_id=? AND revision=? AND enabled=1 AND paused_reason='' AND next_run_at=? AND EXISTS(SELECT 1 FROM stacks WHERE id=? AND archived_at IS NULL)`, next.UnixNano(), p.StackID, p.Revision, p.NextRunAt.UnixNano(), p.StackID)
	if err != nil {
		return autoupdate.Run{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return autoupdate.Run{}, false, err
	}
	var name string
	if err := tx.QueryRowContext(ctx, "SELECT directory_name FROM stacks WHERE id=?", p.StackID).Scan(&name); err != nil {
		return autoupdate.Run{}, false, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return autoupdate.Run{}, false, err
	}
	run := autoupdate.Run{ID: "upd_" + hex.EncodeToString(random[:]), StackID: p.StackID, StackName: name, PolicyRevision: p.Revision, ScheduledAt: p.NextRunAt, Phase: "queued"}
	var pending int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM auto_update_runs WHERE stack_id=? AND phase!='terminal'", p.StackID).Scan(&pending); err != nil {
		return run, false, err
	}
	if pending > 0 {
		run.Phase = "terminal"
		run.Outcome = "skipped"
		run.Reason = "overlap"
	}
	result, err = tx.ExecContext(ctx, `INSERT INTO auto_update_runs(id,stack_id,stack_name,policy_revision,scheduled_at,phase,outcome,reason) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(stack_id,scheduled_at) DO NOTHING`, run.ID, run.StackID, run.StackName, run.PolicyRevision, run.ScheduledAt.UnixNano(), run.Phase, run.Outcome, run.Reason)
	if err != nil {
		return run, false, err
	}
	n, err = result.RowsAffected()
	if err != nil {
		return run, false, err
	}
	if err := tx.Commit(); err != nil {
		return run, false, err
	}
	return run, n == 1 && pending == 0, nil
}
func (s *AutoUpdateStore) ResetAfterStartup(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT "+policyColumns+" FROM auto_update_policies p")
	if err != nil {
		return err
	}
	var policies []autoupdate.Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			rows.Close()
			return err
		}
		policies = append(policies, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range policies {
		next, err := autoupdate.NextRun(p.Expression, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE auto_update_policies SET next_run_at=? WHERE stack_id=?", next.UnixNano(), p.StackID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const runColumns = "id,stack_id,stack_name,policy_revision,scheduled_at,phase,outcome,reason,operation_id"

func scanRun(row rowScanner) (autoupdate.Run, error) {
	var r autoupdate.Run
	var at int64
	err := row.Scan(&r.ID, &r.StackID, &r.StackName, &r.PolicyRevision, &at, &r.Phase, &r.Outcome, &r.Reason, &r.OperationID)
	r.ScheduledAt = time.Unix(0, at).UTC()
	return r, err
}
func (s *AutoUpdateStore) PendingRuns(ctx context.Context) ([]autoupdate.Run, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+runColumns+" FROM auto_update_runs WHERE phase!='terminal' ORDER BY scheduled_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []autoupdate.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
func (s *AutoUpdateStore) LatestRun(ctx context.Context, id stack.StackID) (autoupdate.Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, "SELECT "+runColumns+" FROM auto_update_runs WHERE stack_id=? ORDER BY scheduled_at DESC LIMIT 1", id))
}
func (s *AutoUpdateStore) FinishRun(ctx context.Context, id, outcome, reason string, changes []alert.Change) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE auto_update_runs SET phase='terminal',outcome=?,reason=? WHERE id=? AND phase!='terminal'`, outcome, reason, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	changes, err = currentAlertChanges(ctx, tx, changes)
	if err != nil {
		return err
	}
	if _, err := applyAlertChanges(ctx, tx, changes); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE update_executions SET phase='terminal' WHERE run_id=? AND phase='prepared'", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AutoUpdateStore) Pause(ctx context.Context, id stack.StackID, reason string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE auto_update_policies SET paused_reason=?,revision=revision+1 WHERE stack_id=?", reason, id)
	return err
}

func (s *AutoUpdateStore) NextScheduled(ctx context.Context) (time.Time, error) {
	var at sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT min(p.next_run_at) FROM auto_update_policies p JOIN stacks s ON s.id=p.stack_id WHERE p.enabled=1 AND p.paused_reason='' AND s.archived_at IS NULL`).Scan(&at)
	if err != nil || !at.Valid {
		return time.Time{}, err
	}
	return time.Unix(0, at.Int64).UTC(), nil
}
func (s *AutoUpdateStore) MarkChecking(ctx context.Context, run autoupdate.Run, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE auto_update_runs SET phase='checking' WHERE id=? AND phase='queued' AND EXISTS(SELECT 1 FROM auto_update_policies p JOIN stacks s ON s.id=p.stack_id WHERE p.stack_id=? AND p.enabled=1 AND p.paused_reason='' AND p.revision=? AND s.archived_at IS NULL)`, run.ID, run.StackID, run.PolicyRevision)
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
	if _, err := tx.ExecContext(ctx, "UPDATE auto_update_policies SET last_started_at=? WHERE stack_id=?", now.UnixNano(), run.StackID); err != nil {
		return err
	}
	return tx.Commit()
}
