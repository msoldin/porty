package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/msoldin/porty/internal/application"
)

type AuthStore struct{ db *sql.DB }

func NewAuthStore(db *sql.DB) *AuthStore { return &AuthStore{db: db} }

func (s *AuthStore) Registered(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count)
	return count > 0, err
}

func (s *AuthStore) Register(ctx context.Context, user application.User, session application.SessionRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return application.ErrAlreadyRegistered
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, created_at, password_changed_at) VALUES(?,?,?,?,?)`,
		user.ID, user.Username, user.PasswordHash, encodeTime(user.CreatedAt), encodeTime(user.CreatedAt)); err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	if err := insertSession(ctx, tx, session); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE app_state SET setup_state='registered', updated_at=? WHERE id=1`, encodeTime(user.CreatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AuthStore) UserByUsername(ctx context.Context, username string) (application.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE username=?`, username))
}

func (s *AuthStore) UserByID(ctx context.Context, id string) (application.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE id=?`, id))
}

func (s *AuthStore) OnlyUser(ctx context.Context) (application.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at FROM users LIMIT 1`))
}

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (application.User, error) {
	var user application.User
	var created string
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &created); err != nil {
		return application.User{}, err
	}
	user.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return user, nil
}

func (s *AuthStore) CreateSession(ctx context.Context, session application.SessionRecord) error {
	return insertSession(ctx, s.db, session)
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertSession(ctx context.Context, target execer, session application.SessionRecord) error {
	_, err := target.ExecContext(ctx, `INSERT INTO sessions(token_hash, csrf_hash, user_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at) VALUES(?,?,?,?,?,?,?)`,
		session.TokenHash, session.CSRFHash, session.UserID, encodeTime(session.CreatedAt), encodeTime(session.LastSeenAt), encodeTime(session.IdleExpiresAt), encodeTime(session.AbsoluteExpiresAt))
	return err
}

func (s *AuthStore) SessionByTokenHash(ctx context.Context, hash []byte) (application.SessionRecord, error) {
	var record application.SessionRecord
	var created, seen, idle, absolute string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT s.token_hash, s.csrf_hash, s.user_id, u.username, s.created_at, s.last_seen_at, s.idle_expires_at, s.absolute_expires_at, s.revoked_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, hash).
		Scan(&record.TokenHash, &record.CSRFHash, &record.UserID, &record.Username, &created, &seen, &idle, &absolute, &revoked)
	if err != nil {
		return application.SessionRecord{}, err
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	record.LastSeenAt, _ = time.Parse(time.RFC3339Nano, seen)
	record.IdleExpiresAt, _ = time.Parse(time.RFC3339Nano, idle)
	record.AbsoluteExpiresAt, _ = time.Parse(time.RFC3339Nano, absolute)
	if revoked.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, revoked.String)
		record.RevokedAt = &parsed
	}
	return record, nil
}

func (s *AuthStore) TouchSession(ctx context.Context, hash []byte, lastSeen, idleExpiry time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?, idle_expires_at=? WHERE token_hash=?`, encodeTime(lastSeen), encodeTime(idleExpiry), hash)
	return err
}

func (s *AuthStore) RevokeSession(ctx context.Context, hash []byte, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE token_hash=?`, encodeTime(at), hash)
	return err
}

func (s *AuthStore) UpdatePasswordAndRevokeSessions(ctx context.Context, userID, passwordHash string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?, password_changed_at=? WHERE id=?`, passwordHash, encodeTime(at), userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("administrator not found")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, encodeTime(at), userID); err != nil {
		return err
	}
	return tx.Commit()
}

func encodeTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
