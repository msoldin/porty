package app

import (
	"context"
	"database/sql"
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/autoupdate"
	portycontrol "github.com/msoldin/porty/internal/control"
	portyop "github.com/msoldin/porty/internal/operation"
	portyrepo "github.com/msoldin/porty/internal/repository"
	portystack "github.com/msoldin/porty/internal/stack"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	portyauth "github.com/msoldin/porty/internal/auth"
	composecli "github.com/msoldin/porty/internal/compose"
	"github.com/msoldin/porty/internal/config"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	gitcli "github.com/msoldin/porty/internal/git"
	httpapi "github.com/msoldin/porty/internal/http"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	portyws "github.com/msoldin/porty/internal/websocket"
	"github.com/msoldin/porty/web"
)

// New assembles the application and its HTTP routes.
func New(ctx context.Context, db *sql.DB, cfg config.Config) (*Application, error) {
	var repositorySafetyErr error
	var accessHandler slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if cfg.LogFormat == "json" {
		accessHandler = slog.NewJSONHandler(os.Stdout, nil)
	}
	authService := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	options := httpapi.RouterOptions{
		AccessLogger: slog.New(accessHandler),
		Readiness: func(ctx context.Context) error {
			if repositorySafetyErr != nil {
				return repositorySafetyErr
			}
			return db.PingContext(ctx)
		},
		Auth:       authService,
		PublicURL:  cfg.Server.PublicURL,
		SecureHTTP: cfg.Server.TLSCert != "" || len(cfg.Server.PublicURL) >= 8 && cfg.Server.PublicURL[:8] == "https://",
	}
	repositoryRoot := filepath.Join(cfg.DataDir, "repository")
	coordinator := portyop.NewCoordinator()
	compose, dockerClient, err := composecli.NewDockerClient(ctx, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	assembled := false
	defer func() {
		if !assembled {
			_ = dockerClient.Close()
		}
	}()
	application := &Application{docker: dockerClient}
	var updateControl *portycontrol.ControlPlane
	var demandControl *portycontrol.OnDemandService
	if err := os.MkdirAll(repositoryRoot, 0o700); err == nil {
		if files, err := portyfs.Open(repositoryRoot, portyfs.Limits{MaxEditableBytes: cfg.MaxEditableFileBytes, MaxDepth: 32, MaxEntries: 10_000}); err == nil {
			stackStore := portysqlite.NewStackStore(db)
			workspace := portystack.NewCoordinatedWorkspaceService(portystack.NewStackService(files, stackStore), stackStore, files, portystack.NewEnvironmentService(stackStore), coordinator, compose, repositoryRoot)
			options.Stacks = workspace
			options.Files = workspace
			options.Environment = workspace
		}
	}
	hub := portyws.NewHub(512)
	alertStore := portysqlite.NewAlertStore(db)
	options.Alerts = alert.NewService(alertStore, hub)
	authService.OnKeyRotation(hub.CloseConnections)
	options.Stream = portyws.Handler{Hub: hub}
	operationStore := portysqlite.NewOperationStore(db)
	operations := portyop.NewOperationService(operationStore, hub, 10*time.Minute, 256<<10)
	application.operations = operations
	application.hub = hub
	updateStore := portysqlite.NewAutoUpdateStore(db)
	deploymentStore := portysqlite.NewDeploymentStore(db)
	options.Operations = operationStore
	options.Deployments = deploymentStore
	options.StackDeploymentTimes = deploymentStore
	if stackStore := portysqlite.NewStackStore(db); options.Stacks != nil {
		repositoryStore := portysqlite.NewRepositoryStore(db)
		configuration, _, loadErr := repositoryStore.Load(ctx)
		branch := configuration.Branch
		if branch == "" {
			branch = "main"
		}
		git, err := gitcli.New(repositoryRoot, branch)
		if loadErr != nil {
			err = loadErr
		}
		if err == nil {
			environment := portystack.NewEnvironmentService(stackStore)
			repositoryService := portyrepo.NewRepositoryService(git)
			repositoryService.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
			var provisioner *gitcli.Provisioner
			if err == nil {
				provisioner, err = gitcli.NewProvisioner(cfg.DataDir)
			}
			var setupService *portyrepo.RepositorySetupService
			if err == nil {
				setupService = portyrepo.NewRepositorySetupService(repositoryStore, provisioner, repositoryService, coordinator, portyrepo.RepositorySetupOptions{SSHKeyPath: filepath.Join(cfg.DataDir, "ssh", "id"), KnownHostsPath: filepath.Join(cfg.DataDir, "ssh", "known_hosts")})
				if reconcileErr := setupService.Reconcile(ctx); reconcileErr != nil && configuration.State == "ready" {
					err = reconcileErr
				}
			}
			deployments := portyop.NewDeploymentService(compose, deploymentStore, coordinator)
			control := portycontrol.NewControlPlane(repositoryRoot, stackStore, environment, repositoryService, compose, operations, deployments, coordinator, deploymentStore, hub)
			control.SetAlertReader(alertStore)
			control.SetUpdateStore(updateStore)
			demandControl = portycontrol.NewOnDemandService(control, portysqlite.NewOnDemandStore(db))
			options.OnDemand = demandControl
			updateControl = control
			options.AutoUpdates = autoupdate.NewPolicyService(updateStore, control, func() {
				if application.scheduler != nil {
					application.scheduler.Notify()
				}
			})
			options.Repository = control
			options.Actions = control
			options.State = control
			options.Containers = control
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
	if updateControl != nil {
		if err := updateControl.ReconcileUpdates(ctx); err != nil {
			slog.Error("automatic update reconciliation unavailable")
		}
	}
	if err := operationStore.FailInterrupted(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	options.Audit = portysqlite.NewAuditStore(db)
	api := httpapi.NewRouter(options)
	root := http.NewServeMux()
	root.Handle("/healthz", api)
	root.Handle("/readyz", api)
	root.Handle("/api/", api)
	root.Handle("/", web.Handler())
	application.handler = root
	if updateControl != nil && options.RepositorySetup != nil {
		application.startScheduler(ctx, updateStore, updateControl, compose, options.RepositorySetup.Ready)
		application.startOnDemand(ctx, demandControl, options.RepositorySetup.Ready)
	}
	assembled = true
	return application, nil
}
