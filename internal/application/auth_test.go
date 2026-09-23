package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/application"
	portyauth "github.com/msoldin/porty/internal/infrastructure/auth"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func newAuthService(t *testing.T, now *time.Time) *application.AuthService {
	t.Helper()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), func() time.Time { return *now })
}

func TestInitialRegistrationIsAtomic(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := newAuthService(t, &now)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, username := range []string{"admin-one", "admin-two"} {
		group.Add(1)
		go func(username string) {
			defer group.Done()
			<-start
			_, err := service.Register(context.Background(), username, "correct horse battery staple")
			results <- err
		}(username)
	}
	close(start)
	group.Wait()
	close(results)

	var successes, alreadyRegistered int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, application.ErrAlreadyRegistered):
			alreadyRegistered++
		default:
			t.Fatalf("registration error = %v", err)
		}
	}
	if successes != 1 || alreadyRegistered != 1 {
		t.Fatalf("successes=%d alreadyRegistered=%d, want 1 and 1", successes, alreadyRegistered)
	}
}

func TestExpiredSessionAndPasswordResetRevokeAuthentication(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := newAuthService(t, &now)
	credentials, err := service.Register(context.Background(), "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), credentials.SessionToken); err != nil {
		t.Fatalf("new session rejected: %v", err)
	}

	if err := service.ResetPassword(context.Background(), "new correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), credentials.SessionToken); !errors.Is(err, application.ErrAuthenticationFailed) {
		t.Fatalf("session after reset error = %v, want authentication failed", err)
	}

	newCredentials, err := service.Login(context.Background(), "admin", "new correct horse battery staple", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(13 * time.Hour)
	if _, err := service.Authenticate(context.Background(), newCredentials.SessionToken); !errors.Is(err, application.ErrAuthenticationFailed) {
		t.Fatalf("expired session error = %v, want authentication failed", err)
	}
}

func TestLoginRateLimitRejectsSixthFailure(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := newAuthService(t, &now)
	if _, err := service.Register(context.Background(), "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 5; attempt++ {
		_, err := service.Login(context.Background(), "admin", "wrong password", "192.0.2.1")
		if !errors.Is(err, application.ErrAuthenticationFailed) {
			t.Fatalf("attempt %d error = %v, want authentication failed", attempt, err)
		}
	}
	if _, err := service.Login(context.Background(), "admin", "wrong password", "192.0.2.1"); !errors.Is(err, application.ErrRateLimited) {
		t.Fatalf("sixth attempt error = %v, want rate limited", err)
	}
}
