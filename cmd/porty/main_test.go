package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/msoldin/porty/internal/config"
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
