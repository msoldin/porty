package app

import (
	"context"
	"database/sql"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/msoldin/porty/internal/application"
	portyauth "github.com/msoldin/porty/internal/auth"
	composecli "github.com/msoldin/porty/internal/compose"
	"github.com/msoldin/porty/internal/config"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	gitcli "github.com/msoldin/porty/internal/git"
	"github.com/msoldin/porty/internal/httpapi"
	portyprocess "github.com/msoldin/porty/internal/process"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	portyws "github.com/msoldin/porty/internal/websocket"
	"github.com/msoldin/porty/web"
)

// New assembles the application and its HTTP routes.
func New(ctx context.Context, db *sql.DB, cfg config.Config) (http.Handler, error) {
	var repositorySafetyErr error
	authService := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
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
	_ = operationStore.FailInterrupted(ctx, time.Now().UTC())
	deploymentStore := portysqlite.NewDeploymentStore(db)
	options.Operations = operationStore
	options.Deployments = deploymentStore
	if stackStore := portysqlite.NewStackStore(db); options.Stacks != nil {
		repositoryStore := portysqlite.NewRepositoryStore(db)
		configuration, _, loadErr := repositoryStore.Load(ctx)
		branch := configuration.Branch
		if branch == "" {
			branch = "main"
		}
		git, err := gitcli.New(runner, repositoryRoot, branch)
		if loadErr != nil {
			err = loadErr
		}
		if err == nil {
			environment := application.NewEnvironmentService(stackStore)
			repositoryService := portyrepo.NewRepositoryService(git)
			repositoryService.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
			helper, helperErr := os.Executable()
			if helperErr != nil {
				err = helperErr
			}
			var provisioner *gitcli.Provisioner
			if err == nil {
				provisioner, err = gitcli.NewProvisioner(cfg.DataDir, runner, helper)
			}
			var setupService *portyrepo.RepositorySetupService
			if err == nil {
				setupService = portyrepo.NewRepositorySetupService(repositoryStore, provisioner, repositoryService, coordinator, portyrepo.RepositorySetupOptions{SSHKeyPath: filepath.Join(cfg.DataDir, "ssh", "id"), KnownHostsPath: filepath.Join(cfg.DataDir, "ssh", "known_hosts")})
				if reconcileErr := setupService.Reconcile(ctx); reconcileErr != nil && configuration.State == "ready" {
					err = reconcileErr
				}
			}
			operations := application.NewOperationService(operationStore, hub, 10*time.Minute, 256<<10)
			deployments := application.NewDeploymentService(compose, deploymentStore, coordinator)
			control := application.NewControlPlane(repositoryRoot, stackStore, environment, repositoryService, compose, operations, deployments, coordinator, hub)
			control.ConfigureState(deploymentStore)
			options.Repository = control
			options.Actions = control
			options.State = control
			if setupService != nil {
				options.RepositorySetup = setupService
			}
			if err != nil {
				repositorySafetyErr = err
			}
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
	return root, nil
}
