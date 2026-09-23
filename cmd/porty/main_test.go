package main

import (
	"context"
	"database/sql"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	gitlib "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"

	"github.com/msoldin/porty/internal/app"
	portyauth "github.com/msoldin/porty/internal/auth"
	"github.com/msoldin/porty/internal/config"
	gitcli "github.com/msoldin/porty/internal/git"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestBuildHandlerExposesSetupAPIAndFrontend(t *testing.T) {
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	handler := newTestHandler(t, db, cfg)

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

func TestServeShutsDownAfterContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server, func() error { return server.Serve(listener) }) }()
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after cancellation")
	}
}

func TestBuildHandlerLeavesRegisteredEmptyInstallInSetupState(t *testing.T) {
	dataDir, db := startupDatabase(t)
	if _, err := db.Exec(`UPDATE app_state SET setup_state='registered' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, newTestHandler(t, db, startupConfig(dataDir)), http.StatusOK)
	configuration, _, err := portysqlite.NewRepositoryStore(db).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if configuration.State != portyrepo.RepositorySetupRegistered {
		t.Fatalf("state = %q", configuration.State)
	}
}

func TestBuildHandlerReconcilesRegisteredExistingRepository(t *testing.T) {
	dataDir, db := startupDatabase(t)
	initializeStartupRepository(t, dataDir, "main", "")
	if _, err := db.Exec(`UPDATE app_state SET setup_state='registered' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, newTestHandler(t, db, startupConfig(dataDir)), http.StatusOK)
	configuration, _, _ := portysqlite.NewRepositoryStore(db).Load(context.Background())
	if configuration.State != portyrepo.RepositorySetupReady || configuration.Branch != "main" {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestBuildHandlerOpensReadyLocalOnlyRepository(t *testing.T) {
	testBuildHandlerOpensReadyRepository(t, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}, "")
}
func TestBuildHandlerRestoresReadyHTTPSRemoteClient(t *testing.T) {
	testBuildHandlerOpensReadyRepository(t, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthHTTPS, Username: "git", Secret: "secret"}, "https://example.com/repo.git")
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
	authentication := portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthSSH, SSHKeyPath: filepath.Join(sshDir, "id"), KnownHostsPath: filepath.Join(sshDir, "known_hosts")}
	configuration := readyStartupConfiguration(dataDir, remoteURL, authentication.Type)
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), configuration, authentication); err != nil {
		t.Fatal(err)
	}
	provisioner, err := gitcli.NewProvisioner(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.Open(context.Background(), configuration, authentication); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	assertReadyStatus(t, newTestHandler(t, db, startupConfig(dataDir)), http.StatusOK)
}

func TestBuildHandlerFailsReadinessForTamperedReadyRepository(t *testing.T) {
	dataDir, db := startupDatabase(t)
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), readyStartupConfiguration(dataDir, "", portyrepo.RepositoryAuthNone), portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, newTestHandler(t, db, startupConfig(dataDir)), http.StatusServiceUnavailable)
}

func testBuildHandlerOpensReadyRepository(t *testing.T, authentication portyrepo.RepositoryAuthentication, remoteURL string) {
	t.Helper()
	dataDir, db := startupDatabase(t)
	initializeStartupRepository(t, dataDir, "main", remoteURL)
	if err := portysqlite.NewRepositoryStore(db).Save(context.Background(), readyStartupConfiguration(dataDir, remoteURL, authentication.Type), authentication); err != nil {
		t.Fatal(err)
	}
	assertReadyStatus(t, newTestHandler(t, db, startupConfig(dataDir)), http.StatusOK)
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
func readyStartupConfiguration(dataDir, remoteURL string, authType portyrepo.RepositoryAuthType) portyrepo.RepositoryConfiguration {
	configuration := portyrepo.RepositoryConfiguration{State: portyrepo.RepositorySetupReady, Root: filepath.Join(dataDir, "repository"), Branch: "main", Author: portyrepo.GitIdentity{Name: "Porty", Email: "porty@localhost"}}
	if remoteURL != "" {
		configuration.Remote = &portyrepo.RepositoryRemoteSummary{Name: "origin", URL: remoteURL, AuthType: authType, Managed: true}
	}
	return configuration
}
func initializeStartupRepository(t *testing.T, dataDir, branch, remoteURL string) {
	t.Helper()
	repository := filepath.Join(dataDir, "repository")
	repo, err := gitlib.PlainInit(repository, false, gitlib.WithDefaultBranch(plumbing.NewBranchReferenceName(branch)))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.User.Name, cfg.User.Email = "Porty", "porty@localhost"
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if remoteURL != "" {
		if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{remoteURL}}); err != nil {
			t.Fatal(err)
		}
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
	auth := portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
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
	auth = portyauth.NewAuthService(portysqlite.NewAuthStore(db), portyauth.NewPasswordHasher(), time.Now)
	if _, err := auth.Authenticate(ctx, credentials.AccessToken); err == nil {
		t.Fatal("password reset did not revoke existing session")
	}
	if _, err := auth.Login(ctx, "admin", "new correct horse battery", "127.0.0.1"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}

func newTestHandler(t *testing.T, db *sql.DB, cfg config.Config) http.Handler {
	t.Helper()
	handler, err := app.New(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
