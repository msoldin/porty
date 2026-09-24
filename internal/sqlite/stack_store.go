package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/msoldin/porty/internal/sqlite/generated"
	portystack "github.com/msoldin/porty/internal/stack"
)

type StackStore struct {
	db      *sql.DB
	queries *generated.Queries
}

func NewStackStore(db *sql.DB) *StackStore { return &StackStore{db: db, queries: generated.New(db)} }

func (s *StackStore) Create(ctx context.Context, stack portystack.Stack) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO stacks(id, directory_name, compose_project_name, created_at, updated_at) VALUES(?,?,?,?,?)`,
		stack.ID, stack.DirectoryName, stack.ComposeProjectName, encodeTime(stack.CreatedAt), encodeTime(stack.CreatedAt))
	return err
}

func (s *StackStore) ByDirectory(ctx context.Context, directory string) (portystack.Stack, error) {
	row, err := s.queries.GetStackByDirectory(ctx, directory)
	if err != nil {
		return portystack.Stack{}, err
	}
	return stackFromRow(row), nil
}

func (s *StackStore) ByID(ctx context.Context, id portystack.StackID) (portystack.Stack, error) {
	row, err := s.queries.GetStackByID(ctx, string(id))
	if err != nil {
		return portystack.Stack{}, err
	}
	return stackFromRow(row), nil
}

func (s *StackStore) Active(ctx context.Context) ([]portystack.Stack, error) {
	rows, err := s.queries.ListActiveStacks(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]portystack.Stack, 0, len(rows))
	for _, row := range rows {
		result = append(result, stackFromRow(row))
	}
	return result, nil
}

func stackFromRow(row generated.Stack) portystack.Stack {
	stack := portystack.Stack{ID: portystack.StackID(row.ID), DirectoryName: row.DirectoryName, ComposeProjectName: row.ComposeProjectName}
	stack.CreatedAt, _ = time.Parse(time.RFC3339Nano, row.CreatedAt)
	stack.UpdatedAt, _ = time.Parse(time.RFC3339Nano, row.UpdatedAt)
	if row.ArchivedAt.Valid {
		value, _ := time.Parse(time.RFC3339Nano, row.ArchivedAt.String)
		stack.ArchivedAt = &value
	}
	return stack
}

func (s *StackStore) Archive(ctx context.Context, id portystack.StackID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stacks SET archived_at=?, updated_at=? WHERE id=?`, encodeTime(at), encodeTime(at), id)
	return err
}

func (s *StackStore) Purge(ctx context.Context, id portystack.StackID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM deployments WHERE stack_id=?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM stacks WHERE id=? AND archived_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("archived stack not found")
	}
	return tx.Commit()
}

func (s *StackStore) Rename(ctx context.Context, id portystack.StackID, directory string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stacks SET directory_name=?, updated_at=? WHERE id=? AND archived_at IS NULL`, directory, encodeTime(at), id)
	return err
}

func (s *StackStore) SetEnvironment(ctx context.Context, id portystack.StackID, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO stack_environment(stack_id,key,value,updated_at) VALUES(?,?,?,?) ON CONFLICT(stack_id,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, id, key, []byte(value), encodeTime(time.Now()))
	return err
}

func (s *StackStore) SetEnvironmentWithSecret(ctx context.Context, id portystack.StackID, key, value string, secret bool) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO stack_environment(stack_id,key,value,secret,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(stack_id,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at WHERE stack_environment.secret=excluded.secret`, id, key, []byte(value), secret, encodeTime(time.Now()))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return portystack.ErrEnvironmentSecretImmutable
	}
	return nil
}

func (s *StackStore) EnvironmentValue(ctx context.Context, id portystack.StackID, key string) (portystack.EnvironmentValue, error) {
	var value []byte
	var secret bool
	err := s.db.QueryRowContext(ctx, `SELECT e.value, e.secret FROM stack_environment e JOIN stacks s ON s.id=e.stack_id WHERE e.stack_id=? AND e.key=?`, id, key).Scan(&value, &secret)
	if err != nil {
		return portystack.EnvironmentValue{}, err
	}
	return portystack.EnvironmentValue{Value: string(value), Secret: secret}, nil
}

func (s *StackStore) DeleteEnvironment(ctx context.Context, id portystack.StackID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM stack_environment WHERE stack_id=? AND key=?`, id, key)
	return err
}

func (s *StackStore) Environment(ctx context.Context, id portystack.StackID) (map[string]string, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM stacks WHERE id=?`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM stack_environment WHERE stack_id=? ORDER BY key`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = string(value)
	}
	return result, rows.Err()
}
