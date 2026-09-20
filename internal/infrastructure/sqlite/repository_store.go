package sqlite

import (
	"context"
	"database/sql"
	"time"
)

type RepositoryCredentials struct{ Username, Secret string }

type RepositoryStore struct{ db *sql.DB }

func NewRepositoryStore(db *sql.DB) *RepositoryStore { return &RepositoryStore{db: db} }

func (s *RepositoryStore) SaveConfiguration(ctx context.Context, remote, branch, username, secret string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE app_state SET remote_name=?,remote_url_redacted=?,tracking_branch=?,updated_at=? WHERE id=1`, nullableString("origin"), nullableString(remote), branch, encodeTime(time.Now().UTC())); err != nil {
		return err
	}
	if username == "" && secret == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM repository_auth WHERE id=1`); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO repository_auth(id,auth_type,https_username,https_secret,updated_at) VALUES(1,'https',?,?,?) ON CONFLICT(id) DO UPDATE SET auth_type='https',https_username=excluded.https_username,https_secret=excluded.https_secret,updated_at=excluded.updated_at`, username, []byte(secret), encodeTime(time.Now().UTC())); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *RepositoryStore) Credentials(ctx context.Context) (RepositoryCredentials, error) {
	var result RepositoryCredentials
	var secret []byte
	err := s.db.QueryRowContext(ctx, `SELECT https_username,https_secret FROM repository_auth WHERE id=1 AND auth_type='https'`).Scan(&result.Username, &secret)
	result.Secret = string(secret)
	return result, err
}

func (s *RepositoryStore) Branch(ctx context.Context) (string, error) {
	var branch sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT tracking_branch FROM app_state WHERE id=1`).Scan(&branch)
	if err != nil {
		return "", err
	}
	if branch.String == "" {
		return "main", nil
	}
	return branch.String, nil
}
