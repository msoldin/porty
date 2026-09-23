package sqlite

import (
	"context"
	"database/sql"
	"errors"
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO operations(id,kind,scope_type,scope_id,request_key,status,output_truncated,initiated_by) VALUES(?,?,?,?,?,?,?,?)`,
		operation.ID, operation.Kind, operation.ScopeType, nullableString(operation.ScopeID), nullableString(operation.RequestKey), operation.Status, operation.OutputTruncated, nullableString(operation.InitiatedBy))
	return err
}

func (s *OperationStore) UpdateOperation(ctx context.Context, operation portyop.Operation) error {
	result, err := s.db.ExecContext(ctx, `UPDATE operations SET status=?,started_at=?,completed_at=?,exit_code=?,error_code=?,output_tail=?,output_truncated=? WHERE id=?`,
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
	return operationFromRow(row), nil
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
		result = append(result, operationFromRow(row))
	}
	return result, nil
}

func (s *OperationStore) FailInterrupted(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE operations SET status=?,completed_at=?,error_code=? WHERE status IN (?,?)`,
		portyop.OperationFailed, encodeTime(at), "server_restarted", portyop.OperationQueued, portyop.OperationRunning)
	return err
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
