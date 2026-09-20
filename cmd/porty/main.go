package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/config"
	"github.com/msoldin/porty/internal/domain"
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
	if os.Getenv("PORTY_GIT_ASKPASS") == "1" {
		value := os.Getenv("PORTY_GIT_PASSWORD")
		if len(os.Args) > 1 && strings.Contains(strings.ToLower(os.Args[1]), "username") {
			value = os.Getenv("PORTY_GIT_USERNAME")
		}
		_, _ = os.Stdout.WriteString(value)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		if err := resetPassword(os.Args[2:], os.LookupEnv); err != nil {
			slog.Error("reset password", "error", err)
			os.Exit(1)
		}
		slog.Info("administrator password reset")
		return
	}
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

func resetPassword(args []string, lookupEnv func(string) (string, bool)) error {
	cfg, err := config.Load(args, lookupEnv)
	if err != nil {
		return err
	}
	password, ok := lookupEnv("PORTY_RESET_PASSWORD")
	if !ok {
		return errors.New("PORTY_RESET_PASSWORD is required")
	}
	db, err := portysqlite.Open(context.Background(), filepath.Join(cfg.DataDir, "porty.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	authService := application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	return authService.ResetPassword(context.Background(), password)
}

func buildHandler(db *sql.DB, cfg config.Config) http.Handler {
	var repositorySafetyErr error
	authService := application.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	options := httpapi.RouterOptions{
		Readiness: func(ctx context.Context) error {
			if repositorySafetyErr != nil {
				return repositorySafetyErr
			}
			return db.PingContext(ctx)
		},
		Auth:       authService,
		SecureHTTP: cfg.Server.TLSCert != "",
	}
	repositoryRoot := filepath.Join(cfg.DataDir, "repository")
	coordinator := application.NewCoordinator()
	runner := portyprocess.NewRunner()
	compose := composecli.New(runner, 5*time.Minute)
	if err := os.MkdirAll(repositoryRoot, 0o700); err == nil {
		if files, err := portyfs.Open(repositoryRoot, portyfs.Limits{MaxEditableBytes: cfg.MaxEditableFileBytes, MaxDepth: 32, MaxEntries: 10_000}); err == nil {
			stackStore := portysqlite.NewStackStore(db)
			workspace := application.NewCoordinatedWorkspaceService(application.NewStackService(files, stackStore), stackStore, files, application.NewEnvironmentService(stackStore), coordinator, compose, repositoryRoot)
			options.Stacks = workspace
			options.Files = workspace
			options.Environment = workspace
		}
	}
	hub := portyws.NewHub(512)
	options.Stream = portyws.Handler{Hub: hub}
	operationStore := portysqlite.NewOperationStore(db)
	_ = operationStore.FailInterrupted(context.Background(), time.Now().UTC())
	deploymentStore := portysqlite.NewDeploymentStore(db)
	options.Operations = operationStore
	options.Deployments = deploymentStore
	if stackStore := portysqlite.NewStackStore(db); options.Stacks != nil {
		repositoryStore := portysqlite.NewRepositoryStore(db)
		branch, err := repositoryStore.Branch(context.Background())
		if err != nil {
			branch = "main"
		}
		git, err := gitcli.New(runner, repositoryRoot, branch)
		if _, statErr := os.Lstat(filepath.Join(repositoryRoot, ".git")); statErr == nil {
			git, err = gitcli.Adopt(context.Background(), runner, repositoryRoot, branch)
		}
		if err == nil {
			helper, _ := os.Executable()
			if credentials, credentialErr := repositoryStore.Credentials(context.Background()); credentialErr == nil {
				git, err = git.WithHTTPSCredentials(helper, credentials.Username, credentials.Secret)
			}
		}
		if err == nil {
			environment := application.NewEnvironmentService(stackStore)
			repositoryService := application.NewRepositoryService(git)
			operations := application.NewOperationService(operationStore, hub, 10*time.Minute, 256<<10)
			deployments := application.NewDeploymentService(compose, deploymentStore, coordinator)
			control := application.NewControlPlane(repositoryRoot, stackStore, environment, repositoryService, compose, operations, deployments, coordinator, hub)
			control.ConfigureState(deploymentStore)
			options.Repository = control
			options.Actions = control
			options.State = control
			helper, _ := os.Executable()
			options.RepositorySetup = &repositorySetup{runner: runner, root: repositoryRoot, coordinator: coordinator, service: repositoryService, store: repositoryStore, helper: helper}
		} else {
			repositorySafetyErr = err
		}
	}
	options.Audit = portysqlite.NewAuditStore(db)
	api := httpapi.NewRouter(options)
	root := http.NewServeMux()
	root.Handle("/healthz", api)
	root.Handle("/readyz", api)
	root.Handle("/api/", api)
	root.Handle("/", web.Handler())
	return root
}

type repositorySetup struct {
	runner      gitcli.Runner
	root        string
	coordinator *application.Coordinator
	service     *application.RepositoryService
	store       *portysqlite.RepositoryStore
	helper      string
}

func (s *repositorySetup) SetupRepository(ctx context.Context, request domain.RepositorySetupRequest) error {
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return err
	}
	defer release()
	branch := request.Branch
	if branch == "" {
		branch = "main"
	}
	client, err := gitcli.New(s.runner, s.root, branch)
	if err != nil {
		return err
	}
	if request.Username != "" || request.Secret != "" {
		client, err = client.WithHTTPSCredentials(s.helper, request.Username, request.Secret)
		if err != nil {
			return err
		}
	}
	switch request.Mode {
	case "init":
		err = gitcli.InitExisting(ctx, s.runner, s.root, branch)
		if err == nil && request.Remote != "" {
			err = client.SetOrigin(ctx, request.Remote)
		}
	case "clone":
		err = client.CloneInto(ctx, request.Remote)
	case "adopt":
		client, err = gitcli.Adopt(ctx, s.runner, s.root, branch)
	default:
		return errors.New("unsupported repository setup mode")
	}
	if err != nil {
		return err
	}
	if request.Username != "" || request.Secret != "" {
		client, err = client.WithHTTPSCredentials(s.helper, request.Username, request.Secret)
		if err != nil {
			return err
		}
	}
	if err := client.ValidateSafety(ctx); err != nil {
		return err
	}
	if err := s.store.SaveConfiguration(ctx, request.Remote, branch, request.Username, request.Secret); err != nil {
		return err
	}
	s.service.Replace(client)
	return nil
}
