package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

func TestBuildHandlerLeavesRegisteredEmptyInstallInSetupState(t *testing.T) {
	dataDir, db := startupDatabase(t)
	if _, err := db.Exec(`UPDATE app_state SET setup_state='registered' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, buildHandler(db, startupConfig(dataDir)), http.StatusOK)
	configuration, _, err := portysqlite.NewRepositoryStore(db).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if configuration.State != domain.RepositorySetupRegistered {
		t.Fatalf("state = %q", configuration.State)
	}
}

func TestBuildHandlerReconcilesRegisteredExistingRepository(t *testing.T) {
	dataDir, db := startupDatabase(t)
	initializeStartupRepository(t, dataDir, "main", "")
	if _, err := db.Exec(`UPDATE app_state SET setup_state='registered' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, buildHandler(db, startupConfig(dataDir)), http.StatusOK)
	configuration, _, _ := portysqlite.NewRepositoryStore(db).Load(context.Background())
	if configuration.State != domain.RepositorySetupReady || configuration.Branch != "main" {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestBuildHandlerOpensReadyLocalOnlyRepository(t *testing.T) {
	testBuildHandlerOpensReadyRepository(t, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}, "")
}
func TestBuildHandlerRestoresReadyHTTPSRemoteClient(t *testing.T) {
	testBuildHandlerOpensReadyRepository(t, domain.RepositoryAuthentication{Type: domain.RepositoryAuthHTTPS, Username: "git", Secret: "secret"}, "https://example.com/repo.git")
}

func TestBuildHandlerRestoresReadySSHRemoteClient(t *testing.T) {
	dataDir, db := startupDatabase(t)
	remoteURL := "ssh://example.com/repo.git"
	initializeStartupRepository(t, dataDir, "main", remoteURL)
	sshDir := filepath.Join(dataDir, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("host key"), 0o644); err != nil {
		t.Fatal(err)
	}
	authentication := domain.RepositoryAuthentication{Type: domain.RepositoryAuthSSH, SSHKeyPath: filepath.Join(sshDir, "id"), KnownHostsPath: filepath.Join(sshDir, "known_hosts")}
	configuration := readyStartupConfiguration(dataDir, remoteURL, authentication.Type)
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), configuration, authentication); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	provisioner, err := gitcli.NewProvisioner(dataDir, portyprocess.NewRunner(), executable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.Open(context.Background(), configuration, authentication); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	assertReadyStatus(t, buildHandler(db, startupConfig(dataDir)), http.StatusOK)
}

func TestBuildHandlerFailsReadinessForTamperedReadyRepository(t *testing.T) {
	dataDir, db := startupDatabase(t)
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), readyStartupConfiguration(dataDir, "", domain.RepositoryAuthNone), domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, buildHandler(db, startupConfig(dataDir)), http.StatusServiceUnavailable)
}

func testBuildHandlerOpensReadyRepository(t *testing.T, authentication domain.RepositoryAuthentication, remoteURL string) {
	t.Helper()
	dataDir, db := startupDatabase(t)
	initializeStartupRepository(t, dataDir, "main", remoteURL)
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), readyStartupConfiguration(dataDir, remoteURL, authentication.Type), authentication); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, buildHandler(db, startupConfig(dataDir)), http.StatusOK)
}

func startupDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := portysqlite.Open(context.Background(), filepath.Join(dataDir, "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return dataDir, db
}
func startupConfig(dataDir string) config.Config {
	cfg := config.Default()
	cfg.DataDir = dataDir
	return cfg
}
func readyStartupConfiguration(dataDir, remoteURL string, authType domain.RepositoryAuthType) domain.RepositoryConfiguration {
	configuration := domain.RepositoryConfiguration{State: domain.RepositorySetupReady, Root: filepath.Join(dataDir, "repository"), Branch: "main", Author: domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}}
	if remoteURL != "" {
		configuration.Remote = &domain.RepositoryRemoteSummary{Name: "origin", URL: remoteURL, AuthType: authType, Managed: true}
	}
	return configuration
}
func initializeStartupRepository(t *testing.T, dataDir, branch, remoteURL string) {
	t.Helper()
	repository := filepath.Join(dataDir, "repository")
	runStartupGit(t, dataDir, "init", "-b", branch, repository)
	runStartupGit(t, repository, "config", "user.name", "Porty")
	runStartupGit(t, repository, "config", "user.email", "porty@localhost")
	if remoteURL != "" {
		runStartupGit(t, repository, "remote", "add", "origin", remoteURL)
	}
}
func runStartupGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
func assertReadyStatus(t *testing.T, handler http.Handler, want int) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/readyz", nil))
	if response.Code != want {
		t.Fatalf("ready status = %d: %s", response.Code, response.Body.String())
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
