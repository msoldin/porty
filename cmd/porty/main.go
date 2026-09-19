package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/config"
	"github.com/msoldin/porty/internal/httpapi"
	portyauth "github.com/msoldin/porty/internal/infrastructure/auth"
	portysqlite "github.com/msoldin/porty/internal/infrastructure/sqlite"
	"github.com/msoldin/porty/web"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.LookupEnv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	db, err := portysqlite.Open(context.Background(), filepath.Join(cfg.DataDir, "porty.db"))
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	root := buildHandler(db, cfg)

	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("Porty listening", "address", cfg.Server.Listen)
	if cfg.Server.TLSCert != "" {
		err = server.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func buildHandler(db *sql.DB, cfg config.Config) http.Handler {
	authService := application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	api := httpapi.NewRouter(httpapi.RouterOptions{
		Readiness:  func(ctx context.Context) error { return db.PingContext(ctx) },
		Auth:       authService,
		SecureHTTP: cfg.Server.TLSCert != "",
	})
	root := http.NewServeMux()
	root.Handle("/healthz", api)
	root.Handle("/readyz", api)
	root.Handle("/api/", api)
	root.Handle("/", web.Handler())
	return root
}
