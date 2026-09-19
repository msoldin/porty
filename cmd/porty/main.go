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
	"github.com/msoldin/porty/internal/infrastructure/composecli"
	portyfs "github.com/msoldin/porty/internal/infrastructure/filesystem"
	"github.com/msoldin/porty/internal/infrastructure/gitcli"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
	portysqlite "github.com/msoldin/porty/internal/infrastructure/sqlite"
	portyws "github.com/msoldin/porty/internal/websocket"
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
	options := httpapi.RouterOptions{
		Readiness:  func(ctx context.Context) error { return db.PingContext(ctx) },
		Auth:       authService,
		SecureHTTP: cfg.Server.TLSCert != "",
	}
	repositoryRoot := filepath.Join(cfg.DataDir, "repository")
	if err := os.MkdirAll(repositoryRoot, 0o700); err == nil {
		if files, err := portyfs.Open(repositoryRoot, portyfs.Limits{MaxEditableBytes: cfg.MaxEditableFileBytes, MaxDepth: 32, MaxEntries: 10_000}); err == nil {
			stackStore := portysqlite.NewStackStore(db)
			workspace := application.NewWorkspaceService(application.NewStackService(files, stackStore), stackStore, files, application.NewEnvironmentService(stackStore))
			options.Stacks = workspace
			options.Files = workspace
			options.Environment = workspace
		}
	}
	hub := portyws.NewHub(512)
	options.Stream = portyws.Handler{Hub: hub}
	operationStore := portysqlite.NewOperationStore(db)
	deploymentStore := portysqlite.NewDeploymentStore(db)
	options.Operations = operationStore
	options.Deployments = deploymentStore
	if stackStore := portysqlite.NewStackStore(db); options.Stacks != nil {
		runner := portyprocess.NewRunner()
		git, err := gitcli.New(runner, repositoryRoot, "main")
		if err == nil {
			environment := application.NewEnvironmentService(stackStore)
			coordinator := application.NewCoordinator()
			compose := composecli.New(runner, 5*time.Minute)
			operations := application.NewOperationService(operationStore, hub, 10*time.Minute, 256<<10)
			deployments := application.NewDeploymentService(compose, deploymentStore, coordinator)
			control := application.NewControlPlane(repositoryRoot, stackStore, environment, application.NewRepositoryService(git), compose, operations, deployments, coordinator)
			options.Repository = control
			options.Actions = control
		}
	}
	api := httpapi.NewRouter(options)
	root := http.NewServeMux()
	root.Handle("/healthz", api)
	root.Handle("/readyz", api)
	root.Handle("/api/", api)
	root.Handle("/", web.Handler())
	return root
}
