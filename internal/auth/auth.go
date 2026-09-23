package auth

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

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrAlreadyRegistered    = errors.New("administrator already registered")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrRateLimited          = errors.New("authentication rate limited")
	ErrInvalidPassword      = errors.New("password must be between 12 and 128 bytes")
	ErrInvalidUsername      = errors.New("username must be between 1 and 64 characters")
)

const AccessLifetime = 15 * time.Minute
const RefreshLifetime = 7 * 24 * time.Hour

type Hasher interface {
	Hash(string) (string, error)
	Verify(string, string) bool
}
type User struct {
	ID, Username, PasswordHash string
	CreatedAt                  time.Time
}
type RefreshRecord struct {
	TokenHash, CSRFHash        []byte
	UserID, FamilyID           string
	OriginalLoginAt, ExpiresAt time.Time
}
type Credentials struct{ AccessToken, RefreshToken, CSRFToken, Username string }
type Principal struct {
	UserID, Username string
	CSRFHash         []byte
	ExpiresAt        time.Time
}
type AuthStore interface {
	Registered(context.Context) (bool, error)
	SigningKey(context.Context) ([]byte, error)
	Register(context.Context, User, RefreshRecord) error
	UserByUsername(context.Context, string) (User, error)
	UserByID(context.Context, string) (User, error)
	OnlyUser(context.Context) (User, error)
	CreateRefresh(context.Context, RefreshRecord, time.Time) error
	ExchangeRefresh(context.Context, []byte, []byte, RefreshRecord, time.Time) (User, error)
	RevokeRefreshFamily(context.Context, []byte, string, time.Time) error
	UpdatePasswordAndRevoke(context.Context, string, string, []byte, time.Time) error
}
type accessClaims struct {
	Username string `json:"username"`
	CSRF     string `json:"csrf"`
	jwt.RegisteredClaims
}
type loginWindow struct {
	Started  time.Time
	Failures int
}
type AuthService struct {
	store         AuthStore
	hasher        Hasher
	now           func() time.Time
	dummyHash     string
	mu            sync.Mutex
	attempts      map[string]loginWindow
	onKeyRotation func()
}

func NewAuthService(store AuthStore, hasher Hasher, now func() time.Time) *AuthService {
	dummy, _ := hasher.Hash("not a real Porty password")
	return &AuthService{store: store, hasher: hasher, now: now, dummyHash: dummy, attempts: make(map[string]loginWindow)}
}
func (s *AuthService) OnKeyRotation(callback func())                { s.onKeyRotation = callback }
func (s *AuthService) Registered(ctx context.Context) (bool, error) { return s.store.Registered(ctx) }

func (s *AuthService) Register(ctx context.Context, username, password string) (Credentials, error) {
	username = strings.TrimSpace(username)
	if len(username) < 1 || len(username) > 64 {
		return Credentials{}, ErrInvalidUsername
	}
	if err := validatePassword(password); err != nil {
		return Credentials{}, err
	}
	passwordHash, err := s.hasher.Hash(password)
	if err != nil {
		return Credentials{}, err
	}
	key, err := s.store.SigningKey(ctx)
	if err != nil {
		return Credentials{}, err
	}
	now := s.now().UTC()
	user := User{ID: "usr_" + randomToken(18), Username: username, PasswordHash: passwordHash, CreatedAt: now}
	credentials, record, err := newCredentials(user, now, now.Add(RefreshLifetime), key)
	if err != nil {
		return Credentials{}, err
	}
	if err := s.store.Register(ctx, user, record); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func (s *AuthService) Login(ctx context.Context, username, password, sourceIP string) (Credentials, error) {
	attemptKey := strings.ToLower(strings.TrimSpace(username)) + "|" + sourceIP
	if s.limited(attemptKey) {
		return Credentials{}, ErrRateLimited
	}
	user, err := s.store.UserByUsername(ctx, username)
	encoded := s.dummyHash
	if err == nil {
		encoded = user.PasswordHash
	}
	if !s.hasher.Verify(encoded, password) || err != nil {
		s.recordFailure(attemptKey)
		return Credentials{}, ErrAuthenticationFailed
	}
	s.clearFailures(attemptKey)
	key, err := s.store.SigningKey(ctx)
	if err != nil {
		return Credentials{}, err
	}
	now := s.now().UTC()
	credentials, record, err := newCredentials(user, now, now.Add(RefreshLifetime), key)
	if err != nil {
		return Credentials{}, err
	}
	if err := s.store.CreateRefresh(ctx, record, now); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func (s *AuthService) Authenticate(ctx context.Context, rawToken string) (Principal, error) {
	key, err := s.store.SigningKey(ctx)
	if err != nil {
		return Principal{}, err
	}
	claims := &accessClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("porty"), jwt.WithAudience("porty"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	parsed, err := parser.ParseWithClaims(rawToken, claims, func(*jwt.Token) (any, error) { return key, nil })
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.Username == "" || claims.IssuedAt == nil || claims.ExpiresAt == nil || claims.CSRF == "" {
		return Principal{}, ErrAuthenticationFailed
	}
	csrfHash, err := base64.RawURLEncoding.DecodeString(claims.CSRF)
	if err != nil || len(csrfHash) != sha256.Size || claims.ExpiresAt.Sub(claims.IssuedAt.Time) != AccessLifetime {
		return Principal{}, ErrAuthenticationFailed
	}
	return Principal{UserID: claims.Subject, Username: claims.Username, CSRFHash: csrfHash, ExpiresAt: claims.ExpiresAt.Time}, nil
}
func (s *AuthService) CheckCSRF(principal Principal, rawToken string) bool {
	return rawToken != "" && subtle.ConstantTimeCompare(tokenHash(rawToken), principal.CSRFHash) == 1
}
func (s *AuthService) Refresh(ctx context.Context, rawRefresh, rawCSRF string) (Credentials, error) {
	if rawRefresh == "" || rawCSRF == "" {
		return Credentials{}, ErrAuthenticationFailed
	}
	key, err := s.store.SigningKey(ctx)
	if err != nil {
		return Credentials{}, err
	}
	now := s.now().UTC()
	refresh, err := secureToken(32)
	if err != nil {
		return Credentials{}, err
	}
	csrf, err := secureToken(32)
	if err != nil {
		return Credentials{}, err
	}
	successor := RefreshRecord{TokenHash: tokenHash(refresh), CSRFHash: tokenHash(csrf)}
	user, err := s.store.ExchangeRefresh(ctx, tokenHash(rawRefresh), tokenHash(rawCSRF), successor, now)
	if err != nil {
		return Credentials{}, err
	}
	access, err := signAccess(user, successor.CSRFHash, now, key)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{AccessToken: access, RefreshToken: refresh, CSRFToken: csrf, Username: user.Username}, nil
}
func (s *AuthService) ChangePassword(ctx context.Context, rawAccess, currentPassword, newPassword string) error {
	principal, err := s.Authenticate(ctx, rawAccess)
	if err != nil {
		return err
	}
	user, err := s.store.UserByID(ctx, principal.UserID)
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
func (s *AuthService) Logout(ctx context.Context, rawRefresh, userID string) error {
	return s.store.RevokeRefreshFamily(ctx, tokenHash(rawRefresh), userID, s.now().UTC())
}
func (s *AuthService) setPassword(ctx context.Context, userID, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := s.store.UpdatePasswordAndRevoke(ctx, userID, hash, key, s.now().UTC()); err != nil {
		return err
	}
	if s.onKeyRotation != nil {
		s.onKeyRotation()
	}
	return nil
}
func newCredentials(user User, now, expiry time.Time, key []byte) (Credentials, RefreshRecord, error) {
	refresh, err := secureToken(32)
	if err != nil {
		return Credentials{}, RefreshRecord{}, err
	}
	csrf, err := secureToken(32)
	if err != nil {
		return Credentials{}, RefreshRecord{}, err
	}
	family, err := secureToken(18)
	if err != nil {
		return Credentials{}, RefreshRecord{}, err
	}
	csrfHash := tokenHash(csrf)
	access, err := signAccess(user, csrfHash, now, key)
	if err != nil {
		return Credentials{}, RefreshRecord{}, err
	}
	return Credentials{AccessToken: access, RefreshToken: refresh, CSRFToken: csrf, Username: user.Username}, RefreshRecord{TokenHash: tokenHash(refresh), CSRFHash: csrfHash, UserID: user.ID, FamilyID: family, OriginalLoginAt: now, ExpiresAt: expiry}, nil
}
func signAccess(user User, csrfHash []byte, now time.Time, key []byte) (string, error) {
	claims := accessClaims{Username: user.Username, CSRF: base64.RawURLEncoding.EncodeToString(csrfHash), RegisteredClaims: jwt.RegisteredClaims{Issuer: "porty", Audience: jwt.ClaimStrings{"porty"}, Subject: user.ID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(AccessLifetime))}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
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
func randomToken(bytes int) string  { token, _ := secureToken(bytes); return token }
func tokenHash(token string) []byte { digest := sha256.Sum256([]byte(token)); return digest[:] }
func (s *AuthService) limited(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	window, ok := s.attempts[key]
	return ok && s.now().Sub(window.Started) < time.Minute && window.Failures >= 5
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
