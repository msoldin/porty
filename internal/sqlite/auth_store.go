package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	portyauth "github.com/msoldin/porty/internal/auth"
)

type AuthStore struct{ db *sql.DB }

func NewAuthStore(db *sql.DB) *AuthStore { return &AuthStore{db: db} }
func (s *AuthStore) Registered(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count)
	return count > 0, err
}
func (s *AuthStore) SigningKey(ctx context.Context) ([]byte, error) {
	var key []byte
	if err := s.db.QueryRowContext(ctx, `SELECT signing_key FROM auth_keys WHERE id=1`).Scan(&key); err != nil {
		return nil, fmt.Errorf("load signing key: %w", err)
	}
	if len(key) < 32 {
		return nil, errors.New("invalid signing key")
	}
	return key, nil
}
func (s *AuthStore) Register(ctx context.Context, user portyauth.User, record portyauth.RefreshRecord) error {
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
		return portyauth.ErrAlreadyRegistered
	}
	if err := validKeyTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,created_at,password_changed_at) VALUES(?,?,?,?,?)`, user.ID, user.Username, user.PasswordHash, encodeTime(user.CreatedAt), encodeTime(user.CreatedAt)); err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	if err := insertRefresh(ctx, tx, record); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE app_state SET setup_state='registered',updated_at=? WHERE id=1`, encodeTime(user.CreatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}
func validKeyTx(ctx context.Context, tx *sql.Tx) error {
	var key []byte
	if err := tx.QueryRowContext(ctx, `SELECT signing_key FROM auth_keys WHERE id=1`).Scan(&key); err != nil {
		return err
	}
	if len(key) < 32 {
		return errors.New("invalid signing key")
	}
	return nil
}
func (s *AuthStore) UserByUsername(ctx context.Context, username string) (portyauth.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at FROM users WHERE username=?`, username))
}
func (s *AuthStore) UserByID(ctx context.Context, id string) (portyauth.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at FROM users WHERE id=?`, id))
}
func (s *AuthStore) OnlyUser(ctx context.Context) (portyauth.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at FROM users LIMIT 1`))
}

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (portyauth.User, error) {
	var user portyauth.User
	var created string
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &created); err != nil {
		return portyauth.User{}, err
	}
	user.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return user, nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertRefresh(ctx context.Context, target execer, record portyauth.RefreshRecord) error {
	_, err := target.ExecContext(ctx, `INSERT INTO refresh_tokens(token_hash,user_id,family_id,original_login_at,expires_at,csrf_hash) VALUES(?,?,?,?,?,?)`, record.TokenHash, record.UserID, record.FamilyID, encodeTime(record.OriginalLoginAt), encodeTime(record.ExpiresAt), record.CSRFHash)
	return err
}
func cleanupRefresh(ctx context.Context, target execer, now time.Time) error {
	_, err := target.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at<=?`, encodeTime(now))
	return err
}
func (s *AuthStore) CreateRefresh(ctx context.Context, record portyauth.RefreshRecord, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := cleanupRefresh(ctx, tx, now); err != nil {
		return err
	}
	if err := insertRefresh(ctx, tx, record); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AuthStore) ExchangeRefresh(ctx context.Context, hash, csrfHash []byte, next portyauth.RefreshRecord, now time.Time) (portyauth.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return portyauth.User{}, err
	}
	defer tx.Rollback()
	var userID, family, login, expiry string
	var storedCSRF []byte
	var consumed, revoked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT user_id,family_id,original_login_at,expires_at,csrf_hash,consumed_at,revoked_at FROM refresh_tokens WHERE token_hash=?`, hash).Scan(&userID, &family, &login, &expiry, &storedCSRF, &consumed, &revoked)
	if err == sql.ErrNoRows {
		return portyauth.User{}, portyauth.ErrAuthenticationFailed
	}
	if err != nil {
		return portyauth.User{}, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		return portyauth.User{}, err
	}
	if !now.Before(expires) || revoked.Valid {
		return portyauth.User{}, portyauth.ErrAuthenticationFailed
	}
	if consumed.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=? WHERE family_id=? AND revoked_at IS NULL`, encodeTime(now), family); err != nil {
			return portyauth.User{}, err
		}
		if err := tx.Commit(); err != nil {
			return portyauth.User{}, err
		}
		return portyauth.User{}, portyauth.ErrAuthenticationFailed
	}
	if subtle.ConstantTimeCompare(storedCSRF, csrfHash) != 1 {
		return portyauth.User{}, portyauth.ErrAuthenticationFailed
	}
	result, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET consumed_at=? WHERE token_hash=? AND consumed_at IS NULL AND revoked_at IS NULL`, encodeTime(now), hash)
	if err != nil {
		return portyauth.User{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return portyauth.User{}, err
	}
	if rows != 1 {
		return portyauth.User{}, portyauth.ErrAuthenticationFailed
	}
	original, err := time.Parse(time.RFC3339Nano, login)
	if err != nil {
		return portyauth.User{}, err
	}
	next.UserID = userID
	next.FamilyID = family
	next.OriginalLoginAt = original
	next.ExpiresAt = expires
	if err := insertRefresh(ctx, tx, next); err != nil {
		return portyauth.User{}, err
	}
	user, err := scanUser(tx.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at FROM users WHERE id=?`, userID))
	if err != nil {
		return portyauth.User{}, err
	}
	if err := cleanupRefresh(ctx, tx, now); err != nil {
		return portyauth.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return portyauth.User{}, err
	}
	return user, nil
}
func (s *AuthStore) RevokeRefreshFamily(ctx context.Context, hash []byte, userID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var family string
	err = tx.QueryRowContext(ctx, `SELECT family_id FROM refresh_tokens WHERE token_hash=? AND user_id=?`, hash, userID).Scan(&family)
	if err == sql.ErrNoRows {
		return portyauth.ErrAuthenticationFailed
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=? WHERE family_id=? AND revoked_at IS NULL`, encodeTime(now), family); err != nil {
		return err
	}
	if err := cleanupRefresh(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AuthStore) UpdatePasswordAndRevoke(ctx context.Context, userID, passwordHash string, key []byte, at time.Time) error {
	if len(key) < 32 {
		return errors.New("invalid replacement signing key")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,password_changed_at=? WHERE id=?`, passwordHash, encodeTime(at), userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("administrator not found")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, encodeTime(at), userID); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE auth_keys SET signing_key=?,created_at=? WHERE id=1`, key, encodeTime(at))
	if err != nil {
		return err
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("signing key missing")
	}
	if err := cleanupRefresh(ctx, tx, at); err != nil {
		return err
	}
	return tx.Commit()
}
func initializeAuthKey(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var key []byte
	err = tx.QueryRowContext(ctx, `SELECT signing_key FROM auth_keys WHERE id=1`).Scan(&key)
	if err == nil {
		if len(key) < 32 {
			return errors.New("invalid signing key")
		}
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("signing key missing after registration")
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_keys(id,signing_key,created_at) VALUES(1,?,?)`, key, encodeTime(time.Now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}
func encodeTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
