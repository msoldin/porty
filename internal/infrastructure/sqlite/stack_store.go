package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type StackStore struct{ db *sql.DB }

func NewStackStore(db *sql.DB) *StackStore { return &StackStore{db: db} }

func (s *StackStore) Create(ctx context.Context, stack domain.Stack) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO stacks(id, directory_name, compose_project_name, created_at, updated_at) VALUES(?,?,?,?,?)`,
		stack.ID, stack.DirectoryName, stack.ComposeProjectName, encodeTime(stack.CreatedAt), encodeTime(stack.CreatedAt))
	return err
}

func (s *StackStore) ByDirectory(ctx context.Context, directory string) (domain.Stack, error) {
	return scanStack(s.db.QueryRowContext(ctx, `SELECT id, directory_name, compose_project_name, archived_at, created_at, updated_at FROM stacks WHERE directory_name=?`, directory))
}

func (s *StackStore) Active(ctx context.Context) ([]domain.Stack, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, directory_name, compose_project_name, archived_at, created_at, updated_at FROM stacks WHERE archived_at IS NULL ORDER BY directory_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Stack
	for rows.Next() {
		stack, err := scanStack(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, stack)
	}
	return result, rows.Err()
}

func scanStack(row rowScanner) (domain.Stack, error) {
	var stack domain.Stack
	var archived sql.NullString
	var created, updated string
	if err := row.Scan(&stack.ID, &stack.DirectoryName, &stack.ComposeProjectName, &archived, &created, &updated); err != nil {
		return domain.Stack{}, err
	}
	stack.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	stack.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if archived.Valid {
		value, _ := time.Parse(time.RFC3339Nano, archived.String)
		stack.ArchivedAt = &value
	}
	return stack, nil
}

func (s *StackStore) Archive(ctx context.Context, id domain.StackID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stacks SET archived_at=?, updated_at=? WHERE id=?`, encodeTime(at), encodeTime(at), id)
	return err
}

func (s *StackStore) Purge(ctx context.Context, id domain.StackID) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM stacks WHERE id=? AND archived_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("archived stack not found")
	}
	return nil
}

func (s *StackStore) Rename(ctx context.Context, id domain.StackID, directory string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stacks SET directory_name=?, updated_at=? WHERE id=? AND archived_at IS NULL`, directory, encodeTime(at), id)
	return err
}

func (s *StackStore) SetEnvironment(ctx context.Context, id domain.StackID, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO stack_environment(stack_id,key,value,updated_at) VALUES(?,?,?,?) ON CONFLICT(stack_id,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, id, key, []byte(value), encodeTime(time.Now()))
	return err
}

func (s *StackStore) DeleteEnvironment(ctx context.Context, id domain.StackID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM stack_environment WHERE stack_id=? AND key=?`, id, key)
	return err
}

func (s *StackStore) Environment(ctx context.Context, id domain.StackID) (map[string]string, error) {
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
