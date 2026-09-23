package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	portyauth "github.com/msoldin/porty/internal/auth"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestAccessExpiresAfterFifteenMinutesAndRefreshRotates(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := newAuthService(t, &now)
	issued, err := service.Register(context.Background(), "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if issued.RefreshToken == "" || issued.AccessToken == "" {
		t.Fatal("missing issued tokens")
	}
	if _, err := service.Authenticate(context.Background(), issued.AccessToken); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Minute)
	if _, err := service.Authenticate(context.Background(), issued.AccessToken); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
		t.Fatalf("expired access: %v", err)
	}
	rotated, err := service.Refresh(context.Background(), issued.RefreshToken, issued.CSRFToken)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == issued.RefreshToken || rotated.AccessToken == issued.AccessToken {
		t.Fatal("refresh did not rotate tokens")
	}
	if _, err := service.Authenticate(context.Background(), rotated.AccessToken); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshReplayRevokesFamily(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := newAuthService(t, &now)
	issued, err := service.Register(context.Background(), "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	issuedSuccess := make(chan portyauth.Credentials, 1)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			credentials, err := service.Refresh(context.Background(), issued.RefreshToken, issued.CSRFToken)
			if err == nil {
				issuedSuccess <- credentials
			}
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	var success, denied int
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, portyauth.ErrAuthenticationFailed) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("success=%d denied=%d", success, denied)
	}
	rotated := <-issuedSuccess
	if _, err := service.Refresh(context.Background(), rotated.RefreshToken, rotated.CSRFToken); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
		t.Fatalf("family successor after replay: %v", err)
	}
}

func TestMissingSigningKeyFailsClosedAfterRegistration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	db, err := portysqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), func() time.Time { return now })
	issued, err := service.Register(ctx, "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM auth_keys"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, issued.AccessToken); err == nil {
		t.Fatal("access accepted without signing key")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := portysqlite.Open(ctx, path); err == nil {
		reopened.Close()
		t.Fatal("recreated signing key after registration")
	}
}

func TestFailedKeyRotationPreservesPasswordAndTokens(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), func() time.Time { return now })
	issued, err := service.Register(ctx, "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TRIGGER block_key_rotation BEFORE UPDATE ON auth_keys BEGIN SELECT RAISE(ABORT, 'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(ctx, "new correct horse battery staple"); err == nil {
		t.Fatal("rotation succeeded despite trigger")
	}
	if _, err := service.Authenticate(ctx, issued.AccessToken); err != nil {
		t.Fatalf("old access invalid after rollback: %v", err)
	}
	if _, err := service.Refresh(ctx, issued.RefreshToken, issued.CSRFToken); err != nil {
		t.Fatalf("old refresh invalid after rollback: %v", err)
	}
	if _, err := service.Login(ctx, "admin", "correct horse battery staple", "127.0.0.1"); err != nil {
		t.Fatalf("old password invalid after rollback: %v", err)
	}
}

func TestAccessRejectsInvalidClaimsAndAlgorithms(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewAuthStore(db)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := portyauth.NewAuthService(store, portyauth.NewPasswordHasher(), func() time.Time { return now })
	issued, err := service.Register(ctx, "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.SigningKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var issuedClaims jwt.MapClaims
	if _, _, err := jwt.NewParser().ParseUnverified(issued.AccessToken, &issuedClaims); err != nil {
		t.Fatal(err)
	}
	if _, ok := issuedClaims["password"]; ok {
		t.Fatal("access JWT contains password")
	}
	base := jwt.MapClaims{
		"iss": "porty", "aud": []string{"porty"}, "sub": issuedClaims["sub"],
		"iat": now.Unix(), "exp": now.Add(portyauth.AccessLifetime).Unix(),
		"username": "admin", "csrf": issuedClaims["csrf"],
	}
	for name, mutate := range map[string]func(jwt.MapClaims){
		"issuer":    func(c jwt.MapClaims) { c["iss"] = "other" },
		"audience":  func(c jwt.MapClaims) { c["aud"] = []string{"other"} },
		"subject":   func(c jwt.MapClaims) { delete(c, "sub") },
		"issued at": func(c jwt.MapClaims) { delete(c, "iat") },
		"expiry":    func(c jwt.MapClaims) { delete(c, "exp") },
		"lifetime":  func(c jwt.MapClaims) { c["exp"] = now.Add(16 * time.Minute).Unix() },
	} {
		t.Run(name, func(t *testing.T) {
			claims := jwt.MapClaims{}
			for key, value := range base {
				claims[key] = value
			}
			mutate(claims)
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Authenticate(ctx, raw); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
				t.Fatalf("invalid JWT accepted: %v", err)
			}
		})
	}
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, base).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, none); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
		t.Fatalf("alg=none accepted: %v", err)
	}
	wrongKey, err := jwt.NewWithClaims(jwt.SigningMethodHS256, base).SignedString([]byte("another-valid-but-incorrect-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, wrongKey); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
		t.Fatalf("incorrect signature accepted: %v", err)
	}
}

func TestLogoutRevokesRefreshButAccessLivesUntilExpiry(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := newAuthService(t, &now)
	issued, err := service.Register(context.Background(), "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.Authenticate(context.Background(), issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background(), issued.RefreshToken, principal.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), issued.RefreshToken, issued.CSRFToken); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
		t.Fatalf("refresh after logout: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), issued.AccessToken); err != nil {
		t.Fatalf("access revoked early: %v", err)
	}
	now = now.Add(portyauth.AccessLifetime)
	if _, err := service.Authenticate(context.Background(), issued.AccessToken); !errors.Is(err, portyauth.ErrAuthenticationFailed) {
		t.Fatalf("access after expiry: %v", err)
	}
}
