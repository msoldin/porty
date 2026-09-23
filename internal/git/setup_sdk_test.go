package git

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	portyrepo "github.com/msoldin/porty/internal/repository"
	gossh "golang.org/x/crypto/ssh"
)

type responseWriteCloser struct{ io.Writer }

func (responseWriteCloser) Close() error { return nil }

func TestProvisionerInspectsHTTPSRemoteWithoutGitExecutable(t *testing.T) {
	remoteURL := testHTTPSRemote(t, "", "")
	p, _ := testProvisioner(t)
	t.Setenv("PATH", "")
	inspection, err := p.InspectRemote(context.Background(), remoteURL, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone})
	if err != nil || inspection.Empty || inspection.DefaultBranch != "main" || inspection.Suggested != "main" || len(inspection.Branches) != 1 || inspection.Branches[0] != "main" {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
}

func TestProvisionerImportsHTTPSRemoteWithoutGitExecutable(t *testing.T) {
	remoteURL := testHTTPSRemote(t, "", "")
	p, root := testProvisioner(t)
	t.Setenv("PATH", "")
	author := portyrepo.GitIdentity{Name: "Ada", Email: "ada@example.invalid"}
	client, configuration, err := p.Provision(context.Background(), portyrepo.RepositoryProvisionRequest{
		Mode: portyrepo.RepositorySetupRemote, Branch: "main", Author: author,
		RemoteURL: remoteURL, Authentication: portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Remote == nil || !configuration.Remote.Managed {
		t.Fatalf("configuration = %+v", configuration)
	}
	contents, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil || string(contents) != "hello\n" {
		t.Fatalf("checkout = %q, %v", contents, err)
	}
	if target, err := os.Readlink(filepath.Join(root, "nested", "readme")); err != nil || target != "../README.md" {
		t.Fatalf("relative symlink = %q, %v", target, err)
	}
	status, err := client.Status(context.Background())
	if err != nil || status.Dirty || status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("status = %+v, %v", status, err)
	}
}

func TestProvisionerHTTPSAuthenticationStaysInMemory(t *testing.T) {
	remoteURL := testHTTPSRemote(t, "admin", "very-secret")
	p, _ := testProvisioner(t)
	_, err := p.InspectRemote(context.Background(), remoteURL, portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone})
	if !errors.Is(err, portyrepo.ErrRemoteAuthenticationFailed) {
		t.Fatalf("missing auth error = %v", err)
	}
	inspection, err := p.InspectRemote(context.Background(), remoteURL, portyrepo.RepositoryAuthentication{
		Type: portyrepo.RepositoryAuthHTTPS, Username: "admin", Secret: "very-secret",
	})
	if err != nil || inspection.Empty {
		t.Fatalf("authenticated inspection = %+v, %v", inspection, err)
	}
}

func TestProvisionerConfiguresPopulatedRemoteOnCleanUnbornRepository(t *testing.T) {
	remoteURL := testHTTPSRemote(t, "", "")
	p, root := testProvisioner(t)
	author := portyrepo.GitIdentity{Name: "Ada", Email: "ada@example.invalid"}
	_, cfg, err := p.Provision(context.Background(), portyrepo.RepositoryProvisionRequest{Mode: portyrepo.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	_, updated, err := p.ConfigureRemote(context.Background(), portyrepo.RepositoryRemoteProvisionRequest{
		RemoteURL: remoteURL, Branch: "main", Authentication: portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Remote == nil || !updated.Remote.Managed {
		t.Fatalf("configuration = %+v", updated)
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionerRejectsUnrelatedRemoteWithoutChangingOrigin(t *testing.T) {
	remoteURL := testHTTPSRemote(t, "", "")
	p, root := testProvisioner(t)
	author := portyrepo.GitIdentity{Name: "Ada", Email: "ada@example.invalid"}
	client, cfg, err := p.Provision(context.Background(), portyrepo.RepositoryProvisionRequest{Mode: portyrepo.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	testFile(t, root, "local/docker-compose.yml", "services: {}\n")
	if _, err := client.Commit(context.Background(), "local", "local history"); err != nil {
		t.Fatal(err)
	}
	_, _, err = p.ConfigureRemote(context.Background(), portyrepo.RepositoryRemoteProvisionRequest{
		RemoteURL: remoteURL, Branch: "main", Authentication: portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone},
	}, cfg)
	if !errors.Is(err, portyrepo.ErrUnrelatedHistory) {
		t.Fatalf("ConfigureRemote error = %v", err)
	}
	inspection, err := p.InspectPath(context.Background())
	if err != nil || inspection.ExistingRemote != nil {
		t.Fatalf("origin changed: %+v, %v", inspection, err)
	}
}

func testHTTPSRemote(t *testing.T, expectedUser, expectedSecret string) string {
	t.Helper()
	repo, root, _ := testRepository(t)
	testFile(t, root, "README.md", "hello\n")
	testCommit(t, repo, "README.md", "initial")
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../README.md", filepath.Join(root, "nested", "readme")); err != nil {
		t.Fatal(err)
	}
	hash := testCommit(t, repo, "nested/readme", "relative symlink")
	if err := validateRemoteTree(repo, hash); err != nil {
		t.Fatalf("safe remote tree: %v", err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if expectedUser != "" {
			user, secret, ok := request.BasicAuth()
			if !ok || user != expectedUser || secret != expectedSecret {
				writer.Header().Set("WWW-Authenticate", `Basic realm="Porty test"`)
				http.Error(writer, "authentication failed", http.StatusUnauthorized)
				return
			}
		}
		if request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		} else {
			writer.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		}
		options := &transport.UploadPackRequest{GitProtocol: request.Header.Get("Git-Protocol"), AdvertiseRefs: request.Method == http.MethodGet, StatelessRPC: true}
		if err := transport.UploadPack(request.Context(), repo.Storer, request.Body, responseWriteCloser{writer}, options); err != nil {
			t.Errorf("serve Git: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	certificatePath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certificatePath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", certificatePath)
	return server.URL + "/repository.git"
}

func TestRemoteTreeRejectsEscapingSymlink(t *testing.T) {
	repo, root, _ := testRepository(t)
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../outside", filepath.Join(root, "nested", "escape")); err != nil {
		t.Fatal(err)
	}
	hash := testCommit(t, repo, "nested/escape", "unsafe symlink")
	if err := validateRemoteTree(repo, hash); !errors.Is(err, portyrepo.ErrInvalidWorktree) {
		t.Fatalf("tree validation = %v", err)
	}
}

func testProvisioner(t *testing.T) (*Provisioner, string) {
	t.Helper()
	root := t.TempDir()
	p, err := NewProvisioner(root)
	if err != nil {
		t.Fatal(err)
	}
	return p, filepath.Join(root, "repository")
}

func TestProvisionerInitializesWithoutGitExecutable(t *testing.T) {
	p, root := testProvisioner(t)
	author := portyrepo.GitIdentity{Name: "Ada", Email: "ada@example.invalid"}
	t.Setenv("PATH", "")
	client, cfg, err := p.Provision(context.Background(), portyrepo.RepositoryProvisionRequest{Mode: portyrepo.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != root || cfg.Author != author || cfg.Branch != "main" {
		t.Fatalf("configuration = %+v", cfg)
	}
	status, err := client.Status(context.Background())
	if err != nil || !status.Configured || status.Branch != "main" || status.Dirty {
		t.Fatalf("status = %+v, %v", status, err)
	}
	inspection, err := p.InspectPath(context.Background())
	if err != nil || inspection.State != portyrepo.RepositoryPathWorktree || inspection.Author != author {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
}

func TestProvisionerAdoptsExistingRepositoryAndRejectsUnsafeConfiguration(t *testing.T) {
	p, root := testProvisioner(t)
	repo, err := gitlib.PlainInit(root, false, gitlib.WithDefaultBranch(plumbing.NewBranchReferenceName("main")))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{"https://example.com/team/repo.git"}}); err != nil {
		t.Fatal(err)
	}
	author := portyrepo.GitIdentity{Name: "Ada", Email: "ada@example.invalid"}
	client, cfg, err := p.Provision(context.Background(), portyrepo.RepositoryProvisionRequest{Mode: portyrepo.RepositorySetupAdopt, Branch: "main", Author: author, ManageExistingRemote: true})
	if err != nil || cfg.Remote == nil || !cfg.Remote.Managed {
		t.Fatalf("adopt = %+v, %v", cfg, err)
	}
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\n  repositoryformatversion = 0\n[filter \"evil\"]\n  process = /bin/true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err := p.InspectPath(context.Background())
	if err != nil || inspection.State != portyrepo.RepositoryPathInvalid {
		t.Fatalf("unsafe inspection = %+v, %v", inspection, err)
	}
}

func TestProvisionerRejectsSymlinkedMetadataAndSSHMaterial(t *testing.T) {
	p, root := testProvisioner(t)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	inspection, err := p.InspectPath(context.Background())
	if err != nil || inspection.State != portyrepo.RepositoryPathInvalid {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
	sshDir := filepath.Join(filepath.Dir(root), "ssh")
	if err := os.Mkdir(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(sshDir, "id")
	hosts := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(key, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hosts, []byte("hosts"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p.InspectSSHMaterial().Usable {
		t.Fatal("permissive key accepted")
	}
	client, err := New(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.WithSSHCredentials(key, hosts); err == nil {
		t.Fatal("permissive key accepted by client")
	}
}

func TestProvisionerRejectsSymlinkedGitConfiguration(t *testing.T) {
	p, root := testProvisioner(t)
	repo, err := gitlib.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	configPath := filepath.Join(root, ".git", "config")
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(outside, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, configPath); err != nil {
		t.Fatal(err)
	}
	inspection, err := p.InspectPath(context.Background())
	if err != nil || inspection.State != portyrepo.RepositoryPathInvalid {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
}

func TestProvisionerRejectsRepositoryConfigIncludes(t *testing.T) {
	p, root := testProvisioner(t)
	repo, err := gitlib.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	configPath := filepath.Join(root, ".git", "config")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n[include]\n  path = /tmp/external-git-config\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	inspection, err := p.InspectPath(context.Background())
	if err != nil || inspection.State != portyrepo.RepositoryPathInvalid {
		t.Fatalf("inspection = %+v, %v", inspection, err)
	}
}

func TestSSHAuthenticationRechecksPinnedMaterial(t *testing.T) {
	_, root := testProvisioner(t)
	sshDir := filepath.Join(filepath.Dir(root), "ssh")
	if err := os.Mkdir(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(sshDir, "id")
	hosts := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPublic, err := gossh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hosts, append([]byte("example.com "), gossh.MarshalAuthorizedKey(sshPublic)...), 0o644); err != nil {
		t.Fatal(err)
	}
	client, err := New(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	client, err = client.WithSSHCredentials(key, hosts)
	if err != nil {
		t.Fatal(err)
	}
	options, err := client.transportOptions("ssh://git@example.com/team/repo.git")
	if err != nil || len(options) != 1 {
		t.Fatalf("transport options = %d, %v", len(options), err)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.transportOptions("ssh://git@example.com/team/repo.git"); err == nil {
		t.Fatal("stale permissive key accepted")
	}
}

func TestProvisionerRetryRejectsHistoryInUnbornMode(t *testing.T) {
	p, root := testProvisioner(t)
	author := portyrepo.GitIdentity{Name: "Ada", Email: "ada@example.invalid"}
	request := portyrepo.RepositoryProvisionRequest{Mode: portyrepo.RepositorySetupInit, Branch: "main", Author: author}
	client, _, err := p.Provision(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Provision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	testFile(t, root, "alpha/docker-compose.yml", "services: {}\n")
	if _, err := client.Commit(context.Background(), "alpha", "first"); err != nil {
		t.Fatal(err)
	}
	_, _, err = p.Provision(context.Background(), request)
	if !errors.Is(err, portyrepo.ErrRepositoryPathNotEmpty) {
		t.Fatalf("retry error = %v", err)
	}
}
