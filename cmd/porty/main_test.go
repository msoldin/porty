package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/config"
	"github.com/msoldin/porty/internal/domain"
	portyauth "github.com/msoldin/porty/internal/infrastructure/auth"
	"github.com/msoldin/porty/internal/infrastructure/gitcli"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
	portysqlite "github.com/msoldin/porty/internal/infrastructure/sqlite"
)

func TestBuildHandlerExposesSetupAPIAndFrontend(t *testing.T) {
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	handler := buildHandler(db, cfg)

	for _, path := range []string{"/api/v1/setup/status", "/"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d: %s", path, response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/stacks", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("GET stacks status = %d, want authenticated route", response.Code)
	}
}

func TestRepositorySetupInitializesConfiguredBranch(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "repository")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := portysqlite.Open(ctx, filepath.Join(dataDir, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runner := portyprocess.NewRunner()
	client, err := gitcli.New(runner, root, "release/v1")
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewRepositoryService(client)
	setup := &repositorySetup{runner: runner, root: root, coordinator: application.NewCoordinator(), service: service, store: portysqlite.NewRepositoryStore(db), helper: "/proc/self/exe"}
	if err := setup.SetupRepository(ctx, domain.RepositorySetupRequest{Mode: "init", Branch: "release/v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	branch, err := portysqlite.NewRepositoryStore(db).Branch(ctx)
	if err != nil || branch != "release/v1" {
		t.Fatalf("saved branch = %q, %v", branch, err)
	}
}

func TestResetPasswordCommandChangesPasswordAndRevokesSessions(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := portysqlite.Open(ctx, filepath.Join(dataDir, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	auth := application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	credentials, err := auth.Register(ctx, "admin", "old correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	env := map[string]string{"PORTY_DATA_DIR": dataDir, "PORTY_RESET_PASSWORD": "new correct horse battery"}
	if err := resetPassword(nil, func(key string) (string, bool) { value, ok := env[key]; return value, ok }); err != nil {
		t.Fatal(err)
	}
	db, err = portysqlite.Open(ctx, filepath.Join(dataDir, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth = application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	if _, err := auth.Authenticate(ctx, credentials.SessionToken); err == nil {
		t.Fatal("password reset did not revoke existing session")
	}
	if _, err := auth.Login(ctx, "admin", "new correct horse battery", "127.0.0.1"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}
