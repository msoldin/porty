package sqlite

import (
	"context"
	"database/sql"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepositoryStoreLoadsRegisteredDefaultsWithoutAuthRow(t *testing.T) {
	ctx, db, store := newRepositoryStoreTest(t)
	if _, err := db.ExecContext(ctx, `UPDATE app_state SET setup_state = 'registered' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	configuration, authentication, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantConfiguration := portyrepo.RepositoryConfiguration{State: portyrepo.RepositorySetupRegistered}
	if !reflect.DeepEqual(configuration, wantConfiguration) {
		t.Fatalf("Load() configuration = %#v, want %#v", configuration, wantConfiguration)
	}
	wantAuthentication := portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}
	if !reflect.DeepEqual(authentication, wantAuthentication) {
		t.Fatalf("Load() authentication = %#v, want %#v", authentication, wantAuthentication)
	}
}

func TestRepositoryStoreSavesLocalOnlyReadyConfiguration(t *testing.T) {
	ctx, db, store := newRepositoryStoreTest(t)
	wantConfiguration := localRepositoryConfiguration()
	wantAuthentication := portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}

	if err := store.Save(ctx, wantConfiguration, wantAuthentication); err != nil {
		t.Fatal(err)
	}
	configuration, authentication, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(configuration, wantConfiguration) {
		t.Fatalf("Load() configuration = %#v, want %#v", configuration, wantConfiguration)
	}
	if !reflect.DeepEqual(authentication, wantAuthentication) {
		t.Fatalf("Load() authentication = %#v, want %#v", authentication, wantAuthentication)
	}

	var remoteName, remoteURL sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT remote_name, remote_url_redacted FROM app_state WHERE id = 1`).Scan(&remoteName, &remoteURL); err != nil {
		t.Fatal(err)
	}
	if remoteName.Valid || remoteURL.Valid {
		t.Fatalf("local-only remote columns = (%#v, %#v), want SQL NULL", remoteName, remoteURL)
	}
}

func TestRepositoryStoreRoundTripsHTTPSAuthentication(t *testing.T) {
	ctx, _, store := newRepositoryStoreTest(t)
	wantConfiguration := remoteRepositoryConfiguration(portyrepo.RepositoryAuthHTTPS)
	wantAuthentication := portyrepo.RepositoryAuthentication{
		Type:     portyrepo.RepositoryAuthHTTPS,
		Username: "deploy",
		Secret:   "top-secret",
	}

	if err := store.Save(ctx, wantConfiguration, wantAuthentication); err != nil {
		t.Fatal(err)
	}
	configuration, authentication, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(configuration, wantConfiguration) {
		t.Fatalf("Load() configuration = %#v, want %#v", configuration, wantConfiguration)
	}
	if !reflect.DeepEqual(authentication, wantAuthentication) {
		t.Fatalf("Load() authentication = %#v, want %#v", authentication, wantAuthentication)
	}
}

func TestRepositoryStoreRoundTripsSSHAuthentication(t *testing.T) {
	ctx, _, store := newRepositoryStoreTest(t)
	wantConfiguration := remoteRepositoryConfiguration(portyrepo.RepositoryAuthSSH)
	wantAuthentication := portyrepo.RepositoryAuthentication{
		Type:           portyrepo.RepositoryAuthSSH,
		SSHKeyPath:     "/var/lib/porty/ssh/id",
		KnownHostsPath: "/var/lib/porty/ssh/known_hosts",
	}

	if err := store.Save(ctx, wantConfiguration, wantAuthentication); err != nil {
		t.Fatal(err)
	}
	configuration, authentication, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(configuration, wantConfiguration) {
		t.Fatalf("Load() configuration = %#v, want %#v", configuration, wantConfiguration)
	}
	if !reflect.DeepEqual(authentication, wantAuthentication) {
		t.Fatalf("Load() authentication = %#v, want %#v", authentication, wantAuthentication)
	}
}

func TestRepositoryStoreReplacingAuthClearsOldSecretColumns(t *testing.T) {
	ctx, db, store := newRepositoryStoreTest(t)
	if err := store.Save(ctx, remoteRepositoryConfiguration(portyrepo.RepositoryAuthHTTPS), portyrepo.RepositoryAuthentication{
		Type: portyrepo.RepositoryAuthHTTPS, Username: "deploy", Secret: "top-secret",
	}); err != nil {
		t.Fatal(err)
	}
	wantAuthentication := portyrepo.RepositoryAuthentication{
		Type:           portyrepo.RepositoryAuthSSH,
		SSHKeyPath:     "/var/lib/porty/ssh/id",
		KnownHostsPath: "/var/lib/porty/ssh/known_hosts",
	}
	if err := store.Save(ctx, remoteRepositoryConfiguration(portyrepo.RepositoryAuthSSH), wantAuthentication); err != nil {
		t.Fatal(err)
	}

	var authType string
	var username, sshKeyPath, knownHostsPath sql.NullString
	var secret []byte
	if err := db.QueryRowContext(ctx, `SELECT auth_type, https_username, https_secret, ssh_key_path, known_hosts_path FROM repository_auth WHERE id = 1`).
		Scan(&authType, &username, &secret, &sshKeyPath, &knownHostsPath); err != nil {
		t.Fatal(err)
	}
	if authType != string(portyrepo.RepositoryAuthSSH) || username.Valid || secret != nil {
		t.Fatalf("replaced auth columns = (%q, %#v, %q), want SSH with HTTPS columns NULL", authType, username, secret)
	}
	if sshKeyPath.String != wantAuthentication.SSHKeyPath || knownHostsPath.String != wantAuthentication.KnownHostsPath {
		t.Fatalf("SSH paths = (%q, %q), want (%q, %q)", sshKeyPath.String, knownHostsPath.String, wantAuthentication.SSHKeyPath, wantAuthentication.KnownHostsPath)
	}
}

func TestRepositoryStoreRemovingRemoteKeepsReadyState(t *testing.T) {
	ctx, db, store := newRepositoryStoreTest(t)
	if err := store.Save(ctx, remoteRepositoryConfiguration(portyrepo.RepositoryAuthHTTPS), portyrepo.RepositoryAuthentication{
		Type: portyrepo.RepositoryAuthHTTPS, Username: "deploy", Secret: "top-secret",
	}); err != nil {
		t.Fatal(err)
	}
	wantConfiguration := localRepositoryConfiguration()
	if err := store.Save(ctx, wantConfiguration, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}); err != nil {
		t.Fatal(err)
	}

	configuration, authentication, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(configuration, wantConfiguration) {
		t.Fatalf("Load() configuration = %#v, want %#v", configuration, wantConfiguration)
	}
	if authentication.Type != portyrepo.RepositoryAuthNone {
		t.Fatalf("Load() authentication type = %q, want %q", authentication.Type, portyrepo.RepositoryAuthNone)
	}
	var remoteName, remoteURL sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT remote_name, remote_url_redacted FROM app_state WHERE id = 1`).Scan(&remoteName, &remoteURL); err != nil {
		t.Fatal(err)
	}
	if remoteName.Valid || remoteURL.Valid {
		t.Fatalf("removed remote columns = (%#v, %#v), want SQL NULL", remoteName, remoteURL)
	}
	var authRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM repository_auth`).Scan(&authRows); err != nil {
		t.Fatal(err)
	}
	if authRows != 0 {
		t.Fatalf("repository_auth row count = %d, want 0", authRows)
	}
}

func TestRepositoryStoreSaveRollsBackConfigurationWhenAuthWriteFails(t *testing.T) {
	ctx, db, store := newRepositoryStoreTest(t)
	wantConfiguration := localRepositoryConfiguration()
	if err := store.Save(ctx, wantConfiguration, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_repository_auth BEFORE INSERT ON repository_auth BEGIN SELECT RAISE(ABORT, 'rejected auth write'); END`); err != nil {
		t.Fatal(err)
	}

	err := store.Save(ctx, remoteRepositoryConfiguration(portyrepo.RepositoryAuthHTTPS), portyrepo.RepositoryAuthentication{
		Type: portyrepo.RepositoryAuthHTTPS, Username: "deploy", Secret: "top-secret",
	})
	if err == nil {
		t.Fatal("Save() error = nil, want authentication write failure")
	}
	configuration, authentication, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(configuration, wantConfiguration) {
		t.Fatalf("Load() configuration after rollback = %#v, want %#v", configuration, wantConfiguration)
	}
	if authentication.Type != portyrepo.RepositoryAuthNone {
		t.Fatalf("Load() authentication after rollback = %#v, want none", authentication)
	}
}

func TestRepositoryStoreRejectsUnknownPersistedAuthenticationType(t *testing.T) {
	ctx, db, store := newRepositoryStoreTest(t)
	if _, err := db.ExecContext(ctx, `INSERT INTO repository_auth(id, auth_type, updated_at) VALUES(1, 'unexpected', '2026-09-20T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Load(ctx); err == nil {
		t.Fatal("Load() error = nil, want unknown authentication type rejected")
	}
}

func newRepositoryStoreTest(t *testing.T) (context.Context, *sql.DB, *RepositoryStore) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return ctx, db, NewRepositoryStore(db)
}

func localRepositoryConfiguration() portyrepo.RepositoryConfiguration {
	return portyrepo.RepositoryConfiguration{
		State:  portyrepo.RepositorySetupReady,
		Root:   "/var/lib/porty/repository",
		Branch: "main",
		Author: portyrepo.GitIdentity{Name: "Porty", Email: "porty@localhost"},
	}
}

func remoteRepositoryConfiguration(authType portyrepo.RepositoryAuthType) portyrepo.RepositoryConfiguration {
	configuration := localRepositoryConfiguration()
	configuration.Remote = &portyrepo.RepositoryRemoteSummary{
		Name: "origin", URL: "https://example.com/team/repository.git", AuthType: authType, Managed: true,
	}
	return configuration
}
