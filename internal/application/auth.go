package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrAlreadyRegistered    = errors.New("administrator already registered")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrRateLimited          = errors.New("authentication rate limited")
	ErrInvalidPassword      = errors.New("password must be between 12 and 128 bytes")
	ErrInvalidUsername      = errors.New("username must be between 1 and 64 characters")
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) bool
}

type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type SessionRecord struct {
	TokenHash         []byte
	CSRFHash          []byte
	UserID            string
	Username          string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

type SessionCredentials struct {
	SessionToken string
	CSRFToken    string
}

type AuthenticatedSession struct {
	SessionRecord
}

type AuthStore interface {
	Registered(ctx context.Context) (bool, error)
	Register(ctx context.Context, user User, session SessionRecord) error
	UserByUsername(ctx context.Context, username string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	OnlyUser(ctx context.Context) (User, error)
	CreateSession(ctx context.Context, session SessionRecord) error
	SessionByTokenHash(ctx context.Context, tokenHash []byte) (SessionRecord, error)
	TouchSession(ctx context.Context, tokenHash []byte, lastSeen, idleExpiry time.Time) error
	RevokeSession(ctx context.Context, tokenHash []byte, at time.Time) error
	UpdatePasswordAndRevokeSessions(ctx context.Context, userID, passwordHash string, at time.Time) error
}

type loginWindow struct {
	Started  time.Time
	Failures int
}

type AuthService struct {
	store     AuthStore
	hasher    PasswordHasher
	now       func() time.Time
	dummyHash string
	mu        sync.Mutex
	attempts  map[string]loginWindow
}

func NewAuthService(store AuthStore, hasher PasswordHasher, now func() time.Time) *AuthService {
	dummy, _ := hasher.Hash("not a real Porty password")
	return &AuthService{store: store, hasher: hasher, now: now, dummyHash: dummy, attempts: make(map[string]loginWindow)}
}

func (s *AuthService) Registered(ctx context.Context) (bool, error) { return s.store.Registered(ctx) }

func (s *AuthService) Register(ctx context.Context, username, password string) (SessionCredentials, error) {
	username = strings.TrimSpace(username)
	if len(username) < 1 || len(username) > 64 {
		return SessionCredentials{}, ErrInvalidUsername
	}
	if err := validatePassword(password); err != nil {
		return SessionCredentials{}, err
	}
	passwordHash, err := s.hasher.Hash(password)
	if err != nil {
		return SessionCredentials{}, err
	}
	now := s.now().UTC()
	credentials, record, err := s.newSession("usr_"+randomToken(18), username, now)
	if err != nil {
		return SessionCredentials{}, err
	}
	user := User{ID: record.UserID, Username: username, PasswordHash: passwordHash, CreatedAt: now}
	if err := s.store.Register(ctx, user, record); err != nil {
		return SessionCredentials{}, err
	}
	return credentials, nil
}

func (s *AuthService) Login(ctx context.Context, username, password, sourceIP string) (SessionCredentials, error) {
	key := strings.ToLower(strings.TrimSpace(username)) + "|" + sourceIP
	if s.limited(key) {
		return SessionCredentials{}, ErrRateLimited
	}
	user, err := s.store.UserByUsername(ctx, username)
	encoded := s.dummyHash
	if err == nil {
		encoded = user.PasswordHash
	}
	if !s.hasher.Verify(encoded, password) || err != nil {
		s.recordFailure(key)
		return SessionCredentials{}, ErrAuthenticationFailed
	}
	s.clearFailures(key)
	credentials, record, err := s.newSession(user.ID, user.Username, s.now().UTC())
	if err != nil {
		return SessionCredentials{}, err
	}
	if err := s.store.CreateSession(ctx, record); err != nil {
		return SessionCredentials{}, err
	}
	return credentials, nil
}

func (s *AuthService) Authenticate(ctx context.Context, rawToken string) (AuthenticatedSession, error) {
	hash := tokenHash(rawToken)
	record, err := s.store.SessionByTokenHash(ctx, hash)
	if err != nil || record.RevokedAt != nil {
		return AuthenticatedSession{}, ErrAuthenticationFailed
	}
	now := s.now().UTC()
	if !now.Before(record.IdleExpiresAt) || !now.Before(record.AbsoluteExpiresAt) {
		_ = s.store.RevokeSession(ctx, hash, now)
		return AuthenticatedSession{}, ErrAuthenticationFailed
	}
	record.LastSeenAt = now
	record.IdleExpiresAt = now.Add(12 * time.Hour)
	if record.IdleExpiresAt.After(record.AbsoluteExpiresAt) {
		record.IdleExpiresAt = record.AbsoluteExpiresAt
	}
	if err := s.store.TouchSession(ctx, hash, record.LastSeenAt, record.IdleExpiresAt); err != nil {
		return AuthenticatedSession{}, err
	}
	return AuthenticatedSession{SessionRecord: record}, nil
}

func (s *AuthService) CheckCSRF(session AuthenticatedSession, rawToken string) bool {
	hash := tokenHash(rawToken)
	return subtle.ConstantTimeCompare(hash, session.CSRFHash) == 1
}

func (s *AuthService) ChangePassword(ctx context.Context, rawToken, currentPassword, newPassword string) error {
	session, err := s.Authenticate(ctx, rawToken)
	if err != nil {
		return err
	}
	user, err := s.store.UserByID(ctx, session.UserID)
	if err != nil || !s.hasher.Verify(user.PasswordHash, currentPassword) {
		return ErrAuthenticationFailed
	}
	return s.setPassword(ctx, user.ID, newPassword)
}

func (s *AuthService) ResetPassword(ctx context.Context, newPassword string) error {
	user, err := s.store.OnlyUser(ctx)
	if err != nil {
		return err
	}
	return s.setPassword(ctx, user.ID, newPassword)
}

func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	return s.store.RevokeSession(ctx, tokenHash(rawToken), s.now().UTC())
}

func (s *AuthService) setPassword(ctx context.Context, userID, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	return s.store.UpdatePasswordAndRevokeSessions(ctx, userID, hash, s.now().UTC())
}

func (s *AuthService) newSession(userID, username string, now time.Time) (SessionCredentials, SessionRecord, error) {
	sessionToken, err := secureToken(32)
	if err != nil {
		return SessionCredentials{}, SessionRecord{}, err
	}
	csrfToken, err := secureToken(32)
	if err != nil {
		return SessionCredentials{}, SessionRecord{}, err
	}
	record := SessionRecord{
		TokenHash: tokenHash(sessionToken), CSRFHash: tokenHash(csrfToken), UserID: userID, Username: username,
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(12 * time.Hour), AbsoluteExpiresAt: now.Add(7 * 24 * time.Hour),
	}
	return SessionCredentials{SessionToken: sessionToken, CSRFToken: csrfToken}, record, nil
}

func validatePassword(password string) error {
	if len([]byte(password)) < 12 || len([]byte(password)) > 128 {
		return ErrInvalidPassword
	}
	return nil
}

func secureToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func randomToken(bytes int) string {
	token, _ := secureToken(bytes)
	return token
}

func tokenHash(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func (s *AuthService) limited(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	window, ok := s.attempts[key]
	if !ok || s.now().Sub(window.Started) >= time.Minute {
		return false
	}
	return window.Failures >= 5
}

func (s *AuthService) recordFailure(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	window, ok := s.attempts[key]
	if !ok || now.Sub(window.Started) >= time.Minute {
		window = loginWindow{Started: now}
	}
	window.Failures++
	s.attempts[key] = window
}

func (s *AuthService) clearFailures(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, key)
}
