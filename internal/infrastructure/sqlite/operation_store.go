package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type OperationStore struct{ db *sql.DB }

func NewOperationStore(db *sql.DB) *OperationStore { return &OperationStore{db: db} }

func (s *OperationStore) CreateOperation(ctx context.Context, operation domain.Operation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO operations(id,kind,scope_type,scope_id,request_key,status,output_truncated,initiated_by) VALUES(?,?,?,?,?,?,?,?)`,
		operation.ID, operation.Kind, operation.ScopeType, nullableString(operation.ScopeID), nullableString(operation.RequestKey), operation.Status, operation.OutputTruncated, nullableString(operation.InitiatedBy))
	return err
}

func (s *OperationStore) UpdateOperation(ctx context.Context, operation domain.Operation) error {
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

func (s *OperationStore) Operation(ctx context.Context, id string) (domain.Operation, error) {
	return scanOperation(s.db.QueryRowContext(ctx, operationSelect+` WHERE id=?`, id))
}

func (s *OperationStore) Operations(ctx context.Context, limit int) ([]domain.Operation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, operationSelect+` ORDER BY COALESCE(started_at,'') DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Operation, 0)
	for rows.Next() {
		operation, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, operation)
	}
	return result, rows.Err()
}

const operationSelect = `SELECT id,kind,scope_type,scope_id,request_key,status,started_at,completed_at,exit_code,error_code,output_tail,output_truncated,initiated_by FROM operations`

func scanOperation(row rowScanner) (domain.Operation, error) {
	var operation domain.Operation
	var scopeID, requestKey, startedAt, completedAt, errorCode, initiatedBy sql.NullString
	var exitCode sql.NullInt64
	var output []byte
	if err := row.Scan(&operation.ID, &operation.Kind, &operation.ScopeType, &scopeID, &requestKey, &operation.Status, &startedAt, &completedAt, &exitCode, &errorCode, &output, &operation.OutputTruncated, &initiatedBy); err != nil {
		return domain.Operation{}, err
	}
	operation.ScopeID = scopeID.String
	operation.RequestKey = requestKey.String
	operation.ErrorCode = errorCode.String
	operation.InitiatedBy = initiatedBy.String
	operation.ExitCode = int(exitCode.Int64)
	operation.Output = string(output)
	if startedAt.Valid {
		operation.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt.String)
	}
	if completedAt.Valid {
		operation.CompletedAt, _ = time.Parse(time.RFC3339Nano, completedAt.String)
	}
	return operation, nil
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
