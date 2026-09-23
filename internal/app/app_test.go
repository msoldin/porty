package app_test

import (
	"context"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/msoldin/porty/internal/app"
	"github.com/msoldin/porty/internal/config"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestNewServesFreshInstallation(t *testing.T) {
	dataDir := t.TempDir()
	db, err := portysqlite.Open(context.Background(), filepath.Join(dataDir, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Default()
	cfg.DataDir = dataDir
	handler, err := app.New(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz", "/api/v1/setup/status", "/"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
	}
}

func TestNewKeepsTamperedReadyRepositoryUnready(t *testing.T) {
	dataDir := t.TempDir()
	db, err := portysqlite.Open(context.Background(), filepath.Join(dataDir, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	configuration := portyrepo.RepositoryConfiguration{
		State:  portyrepo.RepositorySetupReady,
		Root:   filepath.Join(dataDir, "repository"),
		Branch: "main",
		Author: portyrepo.GitIdentity{Name: "Porty", Email: "porty@localhost"},
	}
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), configuration, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = dataDir
	handler, err := app.New(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz = %d: %s", response.Code, response.Body.String())
	}
}
