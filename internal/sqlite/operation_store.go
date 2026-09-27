package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/msoldin/porty/internal/alert"
	"time"

	portyop "github.com/msoldin/porty/internal/operation"
	"github.com/msoldin/porty/internal/sqlite/generated"
)

type OperationStore struct {
	db      *sql.DB
	queries *generated.Queries
}

func NewOperationStore(db *sql.DB) *OperationStore {
	return &OperationStore{db: db, queries: generated.New(db)}
}

func (s *OperationStore) CreateOperation(ctx context.Context, operation portyop.Operation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO operations(id,kind,scope_type,scope_id,request_key,status,output_truncated,initiated_by) VALUES(?,?,?,?,?,?,?,?)`,
		operation.ID, operation.Kind, operation.ScopeType, nullableString(operation.ScopeID), nullableString(operation.RequestKey), operation.Status, operation.OutputTruncated, nullableString(operation.InitiatedBy))
	if err != nil {
		return err
	}
	targets, err := json.Marshal(operation.AlertTargets)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operation_alert_context(operation_id,trigger_kind,stack_name,targets_json) VALUES(?,?,?,?)`, operation.ID, operation.Trigger, operation.StackName, string(targets))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *OperationStore) UpdateOperation(ctx context.Context, operation portyop.Operation) error {
	return updateOperation(ctx, s.db, operation)
}

type operationWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func updateOperation(ctx context.Context, db operationWriter, operation portyop.Operation) error {
	result, err := db.ExecContext(ctx, `UPDATE operations SET status=?,started_at=?,completed_at=?,exit_code=?,error_code=?,output_tail=?,output_truncated=? WHERE id=?`,
		operation.Status, nullableTime(operation.StartedAt), nullableTime(operation.CompletedAt), nullableInt(operation.ExitCode), nullableString(operation.ErrorCode), []byte(operation.Output), operation.OutputTruncated, operation.ID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("operation not found")
	}
	return nil
}

func (s *OperationStore) Operation(ctx context.Context, id string) (portyop.Operation, error) {
	row, err := s.queries.GetOperation(ctx, id)
	if err != nil {
		return portyop.Operation{}, err
	}
	o := operationFromRow(row)
	if err := s.loadOperationDetails(ctx, &o); err != nil {
		return o, err
	}
	return o, nil
}

func (s *OperationStore) Operations(ctx context.Context, limit int) ([]portyop.Operation, error) {
	return s.OperationsPage(ctx, limit, 0)
}

func (s *OperationStore) OperationsPage(ctx context.Context, limit, offset int) ([]portyop.Operation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.queries.ListOperations(ctx, generated.ListOperationsParams{Limit: int64(limit), Offset: int64(offset)})
	if err != nil {
		return nil, err
	}
	result := make([]portyop.Operation, 0, len(rows))
	for _, row := range rows {
		o := operationFromRow(row)
		if err := s.loadOperationDetails(ctx, &o); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, nil
}

func (s *OperationStore) FailInterrupted(ctx context.Context, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT o.id,c.stack_name,c.targets_json FROM operations o JOIN operation_alert_context c ON c.operation_id=o.id WHERE o.status IN (?,?) AND NOT EXISTS(SELECT 1 FROM update_executions e WHERE e.operation_id=o.id AND e.phase!='terminal')`, portyop.OperationQueued, portyop.OperationRunning)
	if err != nil {
		return err
	}
	var changes []alert.Change
	for rows.Next() {
		var id, name, raw string
		if err := rows.Scan(&id, &name, &raw); err != nil {
			rows.Close()
			return err
		}
		var keys []alert.Key
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			rows.Close()
			return err
		}
		for _, key := range keys {
			changes = append(changes, alert.Change{Kind: "failure", Key: key, StackName: name, OccurrenceID: id, OperationID: id, Summary: "Operation interrupted by server restart. Verify the stack before retrying.", ObservedAt: at.UTC(), CanResolveManually: true})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err := applyAlertChanges(ctx, tx, changes); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE operations SET status=?,completed_at=?,error_code=? WHERE status IN (?,?) AND NOT EXISTS(SELECT 1 FROM update_executions e WHERE e.operation_id=operations.id AND e.phase!='terminal')`,
		portyop.OperationFailed, encodeTime(at), "server_restarted", portyop.OperationQueued, portyop.OperationRunning)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *OperationStore) loadAlertContext(ctx context.Context, o *portyop.Operation) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT trigger_kind,stack_name,targets_json FROM operation_alert_context WHERE operation_id=?`, o.ID).Scan(&o.Trigger, &o.StackName, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), &o.AlertTargets)
}

func (s *OperationStore) CompleteOperation(ctx context.Context, o portyop.Operation, result portyop.Result) ([]alert.Alert, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := completeUpdate(ctx, tx, o, result.Update); err != nil {
		return nil, err
	}
	if err := updateOperation(ctx, tx, o); err != nil {
		return nil, err
	}
	// Acknowledgments or newer failures can race with a successful runtime
	// action. A stale recovery must not invalidate that action's completion.
	changes, err := currentAlertChanges(ctx, tx, result.Alerts)
	if err != nil {
		return nil, err
	}
	changed, err := applyAlertChanges(ctx, tx, changes)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return changed, nil
}

func operationFromRow(row generated.Operation) portyop.Operation {
	operation := portyop.Operation{
		ID: row.ID, Kind: row.Kind, ScopeType: row.ScopeType, ScopeID: row.ScopeID.String,
		RequestKey: row.RequestKey.String, Status: portyop.OperationStatus(row.Status),
		ExitCode: int(row.ExitCode.Int64), ErrorCode: row.ErrorCode.String, Output: string(row.OutputTail),
		OutputTruncated: row.OutputTruncated != 0, InitiatedBy: row.InitiatedBy.String,
	}
	if row.StartedAt.Valid {
		operation.StartedAt, _ = time.Parse(time.RFC3339Nano, row.StartedAt.String)
	}
	if row.CompletedAt.Valid {
		operation.CompletedAt, _ = time.Parse(time.RFC3339Nano, row.CompletedAt.String)
	}
	return operation
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return encodeTime(value)
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func (s *OperationStore) loadOperationDetails(ctx context.Context, o *portyop.Operation) error {
	if err := s.loadAlertContext(ctx, o); err != nil {
		return err
	}
	var encoded string
	err := s.db.QueryRowContext(ctx, "SELECT results_json FROM update_executions WHERE operation_id=?", o.ID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(encoded), &o.ServiceUpdates)
}
