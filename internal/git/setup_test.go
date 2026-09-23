package git_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	gitcli "github.com/msoldin/porty/internal/git"
	portyprocess "github.com/msoldin/porty/internal/process"
)

const (
	testRemoteURL = "https://git@example.com/team/repo.git"
	testObjectID  = "0123456789012345678901234567890123456789"
)

func TestProvisionerReportsActualSSHMaterialAvailability(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	if got := provisioner.InspectSSHMaterial(); got.IdentityAvailable || got.KnownHostsAvailable || got.Usable {
		t.Fatalf("missing SSH material = %+v", got)
	}
	sshRoot := filepath.Join(filepath.Dir(repository), "ssh")
	if err := os.Mkdir(sshRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(sshRoot, "id")
	hosts := filepath.Join(sshRoot, "known_hosts")
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := provisioner.InspectSSHMaterial(); !got.IdentityAvailable || got.KnownHostsAvailable || got.Usable {
		t.Fatalf("key only = %+v", got)
	}
	if err := os.WriteFile(hosts, []byte("hosts"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := provisioner.InspectSSHMaterial(); !got.IdentityAvailable || !got.KnownHostsAvailable || !got.Usable {
		t.Fatalf("safe SSH material = %+v", got)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := provisioner.InspectSSHMaterial(); !got.IdentityAvailable || !got.KnownHostsAvailable || got.Usable {
		t.Fatalf("permissive key = %+v", got)
	}
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hosts, key); err != nil {
		t.Fatal(err)
	}
	if got := provisioner.InspectSSHMaterial(); got.IdentityAvailable || !got.KnownHostsAvailable || got.Usable {
		t.Fatalf("symlink key = %+v", got)
	}
}

func TestProvisionerRejectsSymlinkedSSHDirectory(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "id"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "known_hosts"), []byte("hosts"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(filepath.Dir(repository), "ssh")); err != nil {
		t.Fatal(err)
	}
	if got := provisioner.InspectSSHMaterial(); got.IdentityAvailable || got.KnownHostsAvailable || got.Usable {
		t.Fatalf("symlinked SSH directory = %+v", got)
	}
	client, err := gitcli.New(portyprocess.NewRunner(), repository, "main")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.WithSSHCredentials("/bin/true", filepath.Join(filepath.Dir(repository), "ssh", "id"), filepath.Join(filepath.Dir(repository), "ssh", "known_hosts"))
	if err == nil {
		t.Fatal("WithSSHCredentials accepted symlinked SSH directory")
	}
}

func TestProvisionerInspectRemoteUsesSymbolicHEAD(t *testing.T) {
	runner := &remoteInspectionRunner{result: portyprocess.Result{Output: "ref: refs/heads/trunk\tHEAD\n" + testObjectID + "\tHEAD\n" + testObjectID + "\trefs/heads/trunk\n"}}
	provisioner, _ := newTestProvisioner(t, runner)

	inspection, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.RemoteURL != "https://example.com/team/repo.git" || inspection.DefaultBranch != "trunk" || inspection.Suggested != "trunk" || inspection.Empty {
		t.Fatalf("inspection = %#v", inspection)
	}
	if !reflect.DeepEqual(inspection.Branches, []string{"trunk"}) {
		t.Fatalf("branches = %#v, want trunk", inspection.Branches)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(runner.requests))
	}
	request := runner.requests[0]
	if request.MaxOutput != 1<<20 {
		t.Fatalf("MaxOutput = %d, want %d", request.MaxOutput, 1<<20)
	}
	if !reflect.DeepEqual(request.Args, hardened("ls-remote", "--symref", "https://example.com/team/repo.git", "HEAD", "refs/heads/*")) {
		t.Fatalf("arguments = %#v", request.Args)
	}
	if runner.deadline < 29*time.Second || runner.deadline > 30*time.Second {
		t.Fatalf("deadline = %v, want approximately 30s", runner.deadline)
	}
}

func TestProvisionerInspectRemoteUsesSoleBranchWithoutHEAD(t *testing.T) {
	provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: testObjectID + "\trefs/heads/release\n"}})

	inspection, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.DefaultBranch != "release" || inspection.Suggested != "release" {
		t.Fatalf("inspection = %#v", inspection)
	}
}

func TestProvisionerInspectRemoteRequiresChoiceForMultipleBranchesWithoutHEAD(t *testing.T) {
	output := testObjectID + "\trefs/heads/main\n" + testObjectID + "\trefs/heads/release\n"
	provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: output}})

	inspection, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.DefaultBranch != "" || inspection.Suggested != "" || inspection.Empty {
		t.Fatalf("inspection = %#v", inspection)
	}
}

func TestProvisionerInspectRemoteSuggestsMainForEmptyRemote(t *testing.T) {
	provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{})

	inspection, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Empty || inspection.Suggested != "main" || inspection.DefaultBranch != "" || len(inspection.Branches) != 0 {
		t.Fatalf("inspection = %#v", inspection)
	}
}

func TestProvisionerInspectRemoteRejectsObjectHEADWithoutBranches(t *testing.T) {
	provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: testObjectID + "\tHEAD\n"}})
	_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if !errors.Is(err, application.ErrRemoteUnavailable) {
		t.Fatalf("InspectRemote() error = %v, want ErrRemoteUnavailable", err)
	}
}

func TestProvisionerInspectRemoteRejectsSymbolicHEADWithoutBranches(t *testing.T) {
	provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: "ref: refs/heads/main\tHEAD\n"}})
	_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if !errors.Is(err, application.ErrRemoteUnavailable) {
		t.Fatalf("InspectRemote() error = %v, want ErrRemoteUnavailable", err)
	}
}

func TestProvisionerInspectRemoteSortsAndDeduplicatesBranches(t *testing.T) {
	output := testObjectID + "\trefs/heads/zeta\n" + testObjectID + "\trefs/heads/alpha\n" + testObjectID + "\trefs/heads/zeta\n"
	provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: output}})

	inspection, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspection.Branches, []string{"alpha", "zeta"}) {
		t.Fatalf("branches = %#v", inspection.Branches)
	}
}

func TestProvisionerInspectRemoteRejectsInvalidBranchRef(t *testing.T) {
	tests := map[string]string{
		"invalid branch":          testObjectID + "\trefs/heads/../escape\n",
		"invalid object":          "not-an-object\trefs/heads/main\n",
		"malformed record":        testObjectID + " refs/heads/main\n",
		"unexpected ref":          testObjectID + "\trefs/tags/v1\n",
		"duplicate symbolic head": "ref: refs/heads/main\tHEAD\nref: refs/heads/main\tHEAD\n" + testObjectID + "\trefs/heads/main\n",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: output}})
			_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
			if !errors.Is(err, application.ErrRemoteUnavailable) {
				t.Fatalf("InspectRemote() error = %v, want ErrRemoteUnavailable", err)
			}
		})
	}
}

func TestProvisionerInspectRemoteRejectsOversizedOutput(t *testing.T) {
	t.Run("truncated output", func(t *testing.T) {
		provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Truncated: true}})
		_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
		if !errors.Is(err, application.ErrRemoteUnavailable) {
			t.Fatalf("InspectRemote() error = %v, want ErrRemoteUnavailable", err)
		}
	})
	t.Run("too many branches", func(t *testing.T) {
		var output strings.Builder
		for index := 0; index <= 1000; index++ {
			fmt.Fprintf(&output, "%s\trefs/heads/branch-%04d\n", testObjectID, index)
		}
		provisioner, _ := newTestProvisioner(t, &remoteInspectionRunner{result: portyprocess.Result{Output: output.String()}})
		_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
		if !errors.Is(err, application.ErrRemoteUnavailable) {
			t.Fatalf("InspectRemote() error = %v, want ErrRemoteUnavailable", err)
		}
	})
}

func TestProvisionerInspectRemoteMapsAuthenticationFailure(t *testing.T) {
	const secret = "do-not-return-this-token"
	runner := &remoteInspectionRunner{
		result: portyprocess.Result{Output: "fatal: Authentication failed for " + secret, ExitCode: 128},
		err:    errors.New("git failed with " + secret),
	}
	provisioner, _ := newTestProvisioner(t, runner)
	authentication := domain.RepositoryAuthentication{Type: domain.RepositoryAuthHTTPS, Username: "git", Secret: secret}

	_, err := provisioner.InspectRemote(context.Background(), "https://example.com/team/repo.git", authentication)
	if !errors.Is(err, application.ErrRemoteAuthenticationFailed) {
		t.Fatalf("InspectRemote() error = %v, want ErrRemoteAuthenticationFailed", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("InspectRemote() error exposed secret: %v", err)
	}
	if len(runner.requests) != 1 || !containsString(runner.requests[0].Redact, secret) || !containsString(runner.requests[0].Env, "PORTY_GIT_PASSWORD="+secret) {
		t.Fatalf("authentication was not applied safely: %#v", runner.requests)
	}
}

func TestProvisionerInspectRemoteMapsGitHTTPAuthenticationFailures(t *testing.T) {
	for _, status := range []string{"401", "403"} {
		t.Run(status, func(t *testing.T) {
			runner := &remoteInspectionRunner{
				result: portyprocess.Result{Output: "fatal: unable to access remote: The requested URL returned error: " + status, ExitCode: 128},
				err:    errors.New("git failed with private transport details"),
			}
			provisioner, _ := newTestProvisioner(t, runner)

			_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
			if !errors.Is(err, application.ErrRemoteAuthenticationFailed) || err.Error() != application.ErrRemoteAuthenticationFailed.Error() {
				t.Fatalf("InspectRemote() error = %v, want stable ErrRemoteAuthenticationFailed", err)
			}
			if strings.Contains(err.Error(), status) {
				t.Fatalf("InspectRemote() error exposed stderr: %v", err)
			}
		})
	}
}

func TestProvisionerInspectRemoteMapsOtherFailureToUnavailable(t *testing.T) {
	runner := &remoteInspectionRunner{
		result: portyprocess.Result{Output: "fatal: unable to access remote host", ExitCode: 128},
		err:    errors.New("network details must not escape"),
	}
	provisioner, _ := newTestProvisioner(t, runner)

	_, err := provisioner.InspectRemote(context.Background(), testRemoteURL, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone})
	if !errors.Is(err, application.ErrRemoteUnavailable) || err.Error() != application.ErrRemoteUnavailable.Error() {
		t.Fatalf("InspectRemote() error = %v, want stable ErrRemoteUnavailable", err)
	}
}

func TestProvisionerRejectsMissingSSHMaterial(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, &remoteInspectionRunner{})
	_, err := provisioner.InspectRemote(context.Background(), "ssh://git@example.com/repo.git", domain.RepositoryAuthentication{Type: domain.RepositoryAuthSSH, SSHKeyPath: filepath.Join(filepath.Dir(repository), "ssh", "id"), KnownHostsPath: filepath.Join(filepath.Dir(repository), "ssh", "known_hosts")})
	if !errors.Is(err, application.ErrSSHMaterialUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerRejectsSymlinkedSSHMaterial(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, &remoteInspectionRunner{})
	sshDirectory := filepath.Join(filepath.Dir(repository), "ssh")
	if err := os.Mkdir(sshDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(target, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(sshDirectory, "id")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDirectory, "known_hosts"), []byte("host key"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := provisioner.InspectRemote(context.Background(), "ssh://git@example.com/repo.git", domain.RepositoryAuthentication{Type: domain.RepositoryAuthSSH, SSHKeyPath: filepath.Join(sshDirectory, "id"), KnownHostsPath: filepath.Join(sshDirectory, "known_hosts")})
	if !errors.Is(err, application.ErrSSHMaterialUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerRejectsPermissivePrivateKey(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, &remoteInspectionRunner{})
	sshDirectory := filepath.Join(filepath.Dir(repository), "ssh")
	if err := os.Mkdir(sshDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDirectory, "id"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDirectory, "known_hosts"), []byte("host key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(sshDirectory, "id"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := provisioner.InspectRemote(context.Background(), "ssh://git@example.com/repo.git", domain.RepositoryAuthentication{Type: domain.RepositoryAuthSSH, SSHKeyPath: filepath.Join(sshDirectory, "id"), KnownHostsPath: filepath.Join(sshDirectory, "known_hosts")})
	if !errors.Is(err, application.ErrSSHMaterialUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerConfigureRemoteAcceptsEmptyRemote(t *testing.T) {
	remote := createLocalRemote(t, "main", false)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	_, updated, err := provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Remote == nil || !updated.Remote.Managed {
		t.Fatalf("configuration = %#v", updated)
	}
	if got := runGit(t, repository, "remote", "get-url", "origin"); got != "https://example.com/team/repo.git\n" {
		t.Fatalf("origin = %q", got)
	}
}

func TestProvisionerConfigureRemoteRejectsUnrelatedHistory(t *testing.T) {
	remote := createLocalRemote(t, "main", true)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repository, "local.txt"), "local\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "local")
	_, _, err = provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if !errors.Is(err, application.ErrUnrelatedHistory) {
		t.Fatalf("error = %v", err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "remote")); got != "" {
		t.Fatalf("remotes = %q", got)
	}
}

func TestProvisionerConfigureRemoteAcceptsRelatedHistory(t *testing.T) {
	dataDirectory := t.TempDir()
	repository := filepath.Join(dataDirectory, "repository")
	initializeRepositoryAt(t, repository)
	write(t, filepath.Join(repository, "base.txt"), "base\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "base")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, filepath.Dir(remote), "init", "--bare", remote)
	runGit(t, repository, "remote", "add", "seed", remote)
	runGit(t, repository, "push", "seed", "main")
	runGit(t, repository, "remote", "remove", "seed")
	write(t, filepath.Join(repository, "local.txt"), "local\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "local")
	executable, _ := os.Executable()
	provisioner, err := gitcli.NewProvisioner(dataDirectory, newLocalRemoteRunner(remote), executable)
	if err != nil {
		t.Fatal(err)
	}
	configuration := domain.RepositoryConfiguration{State: domain.RepositorySetupReady, Root: repository, Branch: "main", Author: domain.GitIdentity{Name: "Existing Author", Email: "existing@example.invalid"}}
	_, updated, err := provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if err != nil || updated.Remote == nil {
		t.Fatalf("ConfigureRemote() = %#v, %v", updated, err)
	}
}

func TestProvisionerConfigureRemoteAcceptsMissingBranch(t *testing.T) {
	remote := createLocalRemote(t, "release", true)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	_, updated, err := provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Remote == nil || strings.TrimSpace(runGit(t, repository, "symbolic-ref", "--short", "HEAD")) != "main" {
		t.Fatalf("configuration = %#v", updated)
	}
}

func TestProvisionerConfigureRemoteAdoptsPopulatedRemoteFromCleanUnbornRepository(t *testing.T) {
	remote := createLocalRemote(t, "main", true)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "show", "HEAD:README.md")); got != "remote repository" {
		t.Fatalf("README = %q", got)
	}
	if got := strings.TrimSpace(runGit(t, repository, "config", "branch.main.remote")); got != "origin" {
		t.Fatalf("tracking remote = %q", got)
	}
}

func TestProvisionerConfigureRemoteRejectsDirtyUnbornRepository(t *testing.T) {
	remote := createLocalRemote(t, "main", true)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, _ := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	write(t, filepath.Join(repository, "local.txt"), "dirty\n")
	_, _, err := provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if !errors.Is(err, application.ErrRepositoryPathNotEmpty) {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerConfigureRemoteRequiresConfirmationForUnmanagedOrigin(t *testing.T) {
	remote := createLocalRemote(t, "main", false)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "remote", "add", "origin", "https://old.example/repo.git")
	configuration := domain.RepositoryConfiguration{State: domain.RepositorySetupReady, Root: repository, Branch: "main", Author: domain.GitIdentity{Name: "Existing Author", Email: "existing@example.invalid"}}
	request := application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}
	if _, _, err := provisioner.ConfigureRemote(context.Background(), request, configuration); !errors.Is(err, application.ErrRepositoryRemoteConflict) {
		t.Fatalf("error = %v", err)
	}
	request.ReplaceExisting = true
	if _, _, err := provisioner.ConfigureRemote(context.Background(), request, configuration); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "remote", "get-url", "origin")); got != "https://example.com/team/repo.git" {
		t.Fatalf("origin = %q", got)
	}
}

func TestProvisionerConfigureRemoteRequiresConfirmationForChangedManagedOrigin(t *testing.T) {
	remote := createLocalRemote(t, "main", false)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	initializeRepositoryAt(t, repository)
	changedURL := "https://changed.example/repo.git"
	runGit(t, repository, "remote", "add", "origin", changedURL)
	configuration := domain.RepositoryConfiguration{
		State: domain.RepositorySetupReady, Root: repository, Branch: "main",
		Author: domain.GitIdentity{Name: "Existing Author", Email: "existing@example.invalid"},
		Remote: &domain.RepositoryRemoteSummary{Name: "origin", URL: "https://expected.example/repo.git", Managed: true},
	}
	request := application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}
	if _, _, err := provisioner.ConfigureRemote(context.Background(), request, configuration); !errors.Is(err, application.ErrRepositoryRemoteConflict) {
		t.Fatalf("error = %v, want remote conflict", err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "remote", "get-url", "origin")); got != changedURL {
		t.Fatalf("origin = %q, want %q", got, changedURL)
	}
	request.ReplaceExisting = true
	if _, _, err := provisioner.ConfigureRemote(context.Background(), request, configuration); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionerConfigureRemoteCleansTemporaryRemoteAfterFailure(t *testing.T) {
	remote := createLocalRemote(t, "main", true)
	localRunner := newLocalRemoteRunner(remote)
	runner := &failingCommandRunner{delegate: localRunner, command: "merge-base"}
	provisioner, repository := newTestProvisioner(t, runner)
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, _ := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	write(t, filepath.Join(repository, "local.txt"), "local\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "local")
	_, _, _ = provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if got := strings.TrimSpace(runGit(t, repository, "remote")); got != "" {
		t.Fatalf("remotes = %q", got)
	}
}

func TestProvisionerConfigureRemoteDoesNotMapOperationalMergeBaseFailureToUnrelated(t *testing.T) {
	remote := createLocalRemote(t, "main", true)
	runner := &failingCommandRunner{delegate: newLocalRemoteRunner(remote), command: "merge-base"}
	provisioner, repository := newTestProvisioner(t, runner)
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, _ := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	write(t, filepath.Join(repository, "local.txt"), "local\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "local")
	_, _, err := provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if err == nil || errors.Is(err, application.ErrUnrelatedHistory) {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerRemoveRemoteRemovesOnlyManagedOrigin(t *testing.T) {
	remote := createLocalRemote(t, "main", false)
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))
	author := domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}
	_, configuration, _ := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: author})
	_, configuration, err := provisioner.ConfigureRemote(context.Background(), application.RepositoryRemoteProvisionRequest{RemoteURL: testRemoteURL, Branch: "main", Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}}, configuration)
	if err != nil {
		t.Fatal(err)
	}
	_, updated, err := provisioner.RemoveRemote(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Remote != nil || strings.TrimSpace(runGit(t, repository, "remote")) != "" {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestProvisionerRemoveRemoteLeavesUnmanagedOriginUntouched(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "remote", "add", "origin", "https://example.com/repo.git")
	configuration := domain.RepositoryConfiguration{State: domain.RepositorySetupReady, Root: repository, Branch: "main", Author: domain.GitIdentity{Name: "Existing Author", Email: "existing@example.invalid"}}
	_, _, err := provisioner.RemoveRemote(context.Background(), configuration)
	if !errors.Is(err, application.ErrRepositoryRemoteUnavailable) || strings.TrimSpace(runGit(t, repository, "remote")) != "origin" {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerRemoveRemoteRejectsChangedManagedOrigin(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "remote", "add", "origin", "https://changed.example/repo.git")
	configuration := domain.RepositoryConfiguration{State: domain.RepositorySetupReady, Root: repository, Branch: "main", Author: domain.GitIdentity{Name: "Existing Author", Email: "existing@example.invalid"}, Remote: &domain.RepositoryRemoteSummary{Name: "origin", URL: "https://expected.example/repo.git", Managed: true}}
	_, _, err := provisioner.RemoveRemote(context.Background(), configuration)
	if !errors.Is(err, application.ErrRepositoryRemoteConflict) || strings.TrimSpace(runGit(t, repository, "remote")) != "origin" {
		t.Fatalf("error = %v", err)
	}
}

func TestProvisionerRemoteImportFetchesSelectedBranch(t *testing.T) {
	localRemote := createLocalRemote(t, "trunk", true)
	runner := newLocalRemoteRunner(localRemote)
	provisioner, repository := newTestProvisioner(t, runner)
	request := remoteProvisionRequest("trunk")
	request.Authentication = domain.RepositoryAuthentication{Type: domain.RepositoryAuthHTTPS, Username: "git", Secret: "import-token"}

	repositoryClient, configuration, err := provisioner.Provision(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositoryClient.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repository, "symbolic-ref", "--short", "HEAD"); got != "trunk\n" {
		t.Fatalf("branch = %q, want trunk", got)
	}
	if got, err := os.ReadFile(filepath.Join(repository, "README.md")); err != nil || string(got) != "remote repository\n" {
		t.Fatalf("fetched worktree contents = %q, err=%v", got, err)
	}
	if configuration.Remote == nil || configuration.Remote.URL != "https://example.com/team/repo.git" || !configuration.Remote.Managed {
		t.Fatalf("remote configuration = %#v", configuration.Remote)
	}
	wantCommands := [][]string{
		hardened("init", "-b", "trunk", repository),
		hardened("-C", repository, "remote", "add", "origin", "https://example.com/team/repo.git"),
		hardened("-C", repository, "fetch", "--no-tags", "origin", "refs/heads/trunk:refs/remotes/origin/trunk"),
		hardened("-C", repository, "checkout", "-B", "trunk", "--track", "origin/trunk"),
	}
	if !containsCommands(runner.args(), wantCommands) {
		t.Fatalf("commands = %#v, want ordered commands %#v", runner.args(), wantCommands)
	}
	for _, recorded := range runner.requests {
		if (containsString(recorded.Args, "ls-remote") || containsString(recorded.Args, "fetch")) && !containsString(recorded.Env, "PORTY_GIT_PASSWORD=import-token") {
			t.Fatalf("remote command is missing authentication environment: %#v", recorded)
		}
	}
}

func TestProvisionerRemoteImportCreatesTrackingBranch(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(createLocalRemote(t, "release", true)))

	_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("release"))
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repository, "config", "--local", "branch.release.remote"); got != "origin\n" {
		t.Fatalf("tracking remote = %q", got)
	}
	if got := runGit(t, repository, "config", "--local", "branch.release.merge"); got != "refs/heads/release\n" {
		t.Fatalf("tracking merge = %q", got)
	}
}

func TestProvisionerRemoteImportBoundsFetch(t *testing.T) {
	runner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
	provisioner, _ := newTestProvisioner(t, runner)
	if _, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main")); err != nil {
		t.Fatal(err)
	}
	for _, request := range runner.requests {
		if containsString(request.Args, "fetch") && request.MaxOutput != 1<<20 {
			t.Errorf("fetch MaxOutput = %d, want %d", request.MaxOutput, 1<<20)
		}
	}
	if runner.fetchDeadline < 29*time.Second || runner.fetchDeadline > 30*time.Second {
		t.Errorf("fetch deadline = %v, want approximately 30s", runner.fetchDeadline)
	}
}

func TestProvisionerRemoteImportBoundsTreeAndCheckout(t *testing.T) {
	runner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
	provisioner, _ := newTestProvisioner(t, runner)
	if _, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main")); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"ls-tree", "checkout"} {
		found := false
		for _, request := range runner.requests {
			if containsString(request.Args, command) {
				found = true
				if request.MaxOutput != 1<<20 {
					t.Errorf("%s MaxOutput = %d, want %d", command, request.MaxOutput, 1<<20)
				}
			}
		}
		if !found {
			t.Errorf("%s was not called", command)
		}
		if deadline := runner.commandDeadlines[command]; deadline < 29*time.Second || deadline > 30*time.Second {
			t.Errorf("%s deadline = %v, want approximately 30s", command, deadline)
		}
	}
}

func TestProvisionerRemoteImportPublishesNestedFilesAndSymlinks(t *testing.T) {
	remote := createLocalRemote(t, "main", true)
	source := filepath.Join(filepath.Dir(remote), "source")
	write(t, filepath.Join(source, "nested", "start.sh"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(source, "nested", "start.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../README.md", filepath.Join(source, "nested", "readme")); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "nested")
	runGit(t, source, "commit", "-m", "add nested files")
	runGit(t, source, "push", "origin", "main")
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(remote))

	if _, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main")); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(repository, "nested", "start.sh")); err != nil || string(content) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("nested executable = %q, err=%v", content, err)
	}
	if info, err := os.Stat(filepath.Join(repository, "nested", "start.sh")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable permissions missing: info=%v err=%v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(repository, "nested", "readme")); err != nil || target != "../README.md" {
		t.Fatalf("symlink target = %q, err=%v", target, err)
	}
	if status := runGit(t, repository, "status", "--porcelain"); status != "" {
		t.Fatalf("imported worktree is dirty: %q", status)
	}
	if _, err := os.Lstat(filepath.Join(repository, ".git", "porty-checkout")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkout staging directory remains: %v", err)
	}
}

func TestProvisionerRemoteImportSupportsEmptyRemote(t *testing.T) {
	runner := newLocalRemoteRunner(createLocalRemote(t, "main", false))
	provisioner, repository := newTestProvisioner(t, runner)

	_, configuration, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main"))
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repository, "symbolic-ref", "--short", "HEAD"); got != "main\n" {
		t.Fatalf("branch = %q, want main", got)
	}
	if got := runGit(t, repository, "remote", "get-url", "origin"); got != "https://example.com/team/repo.git\n" {
		t.Fatalf("origin = %q", got)
	}
	if configuration.Remote == nil || !configuration.Remote.Managed {
		t.Fatalf("remote configuration = %#v", configuration.Remote)
	}
	for _, arguments := range runner.args() {
		if containsString(arguments, "fetch") || containsString(arguments, "checkout") {
			t.Fatalf("empty import ran history command: %#v", arguments)
		}
	}
}

func TestProvisionerRemoteImportRejectsUnadvertisedBranch(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(createLocalRemote(t, "main", true)))

	_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("missing"))
	if !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("Provision() error = %v, want ErrInvalidRequest", err)
	}
	if _, statErr := os.Lstat(repository); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("repository was mutated: %v", statErr)
	}
}

func TestProvisionerRemoteImportRetryAcceptsExactPartialRepository(t *testing.T) {
	provisioner, _ := newTestProvisioner(t, newLocalRemoteRunner(createLocalRemote(t, "main", true)))
	request := remoteProvisionRequest("main")

	if _, _, err := provisioner.Provision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, configuration, err := provisioner.Provision(context.Background(), request); err != nil {
		t.Fatalf("retry failed: %v", err)
	} else if configuration.Branch != request.Branch || configuration.Author != request.Author || configuration.Remote == nil || !configuration.Remote.Managed {
		t.Fatalf("retry configuration = %#v", configuration)
	}
}

func TestProvisionerRemoteImportRetryRejectsMismatchedOrigin(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(createLocalRemote(t, "main", true)))
	request := remoteProvisionRequest("main")
	if _, _, err := provisioner.Provision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "remote", "set-url", "origin", "https://example.com/other/repo.git")

	_, _, err := provisioner.Provision(context.Background(), request)
	if !errors.Is(err, application.ErrRepositoryPathNotEmpty) {
		t.Fatalf("Provision() error = %v, want ErrRepositoryPathNotEmpty", err)
	}
}

func TestProvisionerRemoteImportRetryRejectsUnrelatedLocalHistory(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, newLocalRemoteRunner(createLocalRemote(t, "main", true)))
	request := remoteProvisionRequest("main")
	if _, _, err := provisioner.Provision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repository, "local-only.txt"), "unrelated local history\n")
	runGit(t, repository, "add", "local-only.txt")
	runGit(t, repository, "commit", "-m", "unrelated local commit")

	_, _, err := provisioner.Provision(context.Background(), request)
	if !errors.Is(err, application.ErrRepositoryPathNotEmpty) {
		t.Fatalf("Provision() error = %v, want ErrRepositoryPathNotEmpty", err)
	}
}

func TestProvisionerRemoteImportFailureRemovesOnlyCreatedArtifacts(t *testing.T) {
	for _, preexistingRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("preexisting root %t", preexistingRoot), func(t *testing.T) {
			localRunner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
			failingRunner := &failingCommandRunner{delegate: localRunner, command: "fetch"}
			provisioner, repository := newTestProvisioner(t, failingRunner)
			if preexistingRoot {
				if err := os.Mkdir(repository, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main"))
			if !errors.Is(err, application.ErrRemoteUnavailable) {
				t.Fatalf("Provision() error = %v, want ErrRemoteUnavailable", err)
			}
			info, statErr := os.Lstat(repository)
			if preexistingRoot {
				if statErr != nil || !info.IsDir() {
					t.Fatalf("pre-existing root was removed: %v", statErr)
				}
				entries, readErr := os.ReadDir(repository)
				if readErr != nil || len(entries) != 0 {
					t.Fatalf("created artifacts remain: entries=%v err=%v", entries, readErr)
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("created root remains: %v", statErr)
			}
		})
	}
}

func TestProvisionerRemoteImportFailurePreservesFileAddedToPreExistingRoot(t *testing.T) {
	localRunner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
	failingRunner := &failingCommandRunner{delegate: localRunner, command: "fetch"}
	provisioner, repository := newTestProvisioner(t, failingRunner)
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	failingRunner.beforeFail = func() {
		write(t, filepath.Join(repository, "keep.txt"), "must survive cleanup\n")
	}

	_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main"))
	if !errors.Is(err, application.ErrRemoteUnavailable) {
		t.Fatalf("Provision() error = %v, want ErrRemoteUnavailable", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(repository, "keep.txt")); readErr != nil || string(got) != "must survive cleanup\n" {
		t.Fatalf("pre-existing-root file = %q, err=%v", got, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(repository, ".git")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created Git metadata remains: %v", statErr)
	}
}

func TestProvisionerRemoteImportCleanupRejectsParentSymlinkReplacement(t *testing.T) {
	localRunner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
	failingRunner := &failingCommandRunner{delegate: localRunner, command: "fetch"}
	provisioner, repository := newTestProvisioner(t, failingRunner)
	dataDirectory := filepath.Dir(repository)
	movedDataDirectory := dataDirectory + "-moved"
	externalDirectory := t.TempDir()
	externalRepository := filepath.Join(externalDirectory, "repository")
	write(t, filepath.Join(externalRepository, "keep.txt"), "external data\n")
	failingRunner.beforeFail = func() {
		if err := os.Rename(dataDirectory, movedDataDirectory); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(externalDirectory, dataDirectory); err != nil {
			t.Fatal(err)
		}
	}

	_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main"))
	if !errors.Is(err, application.ErrRemoteUnavailable) {
		t.Fatalf("Provision() error = %v, want ErrRemoteUnavailable", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(externalRepository, "keep.txt")); readErr != nil || string(got) != "external data\n" {
		t.Fatalf("external file = %q, err=%v", got, readErr)
	}
}

func TestProvisionerRemoteImportFailurePreservesConcurrentFileCreatedDuringCheckout(t *testing.T) {
	localRunner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
	failingRunner := &failingCommandRunner{
		delegate: localRunner,
		command:  "config",
	}
	provisioner, repository := newTestProvisioner(t, failingRunner)
	failingRunner.afterRun = func(request portyprocess.Request) {
		if containsString(request.Args, "checkout") {
			write(t, filepath.Join(repository, "concurrent.txt"), "must survive cleanup\n")
		}
	}

	_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main"))
	if err == nil {
		t.Fatal("Provision() error = nil, want configuration failure")
	}
	if got, readErr := os.ReadFile(filepath.Join(repository, "concurrent.txt")); readErr != nil || string(got) != "must survive cleanup\n" {
		t.Fatalf("concurrent file = %q, err=%v", got, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(repository, "README.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created checkout artifact remains: %v", statErr)
	}
}

func TestProvisionerRemoteImportFailureRemovesPartialCheckoutArtifacts(t *testing.T) {
	localRunner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
	failingRunner := &failingCommandRunner{delegate: localRunner, command: "checkout", failAfterRun: true}
	provisioner, repository := newTestProvisioner(t, failingRunner)

	_, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main"))
	if err == nil {
		t.Fatal("Provision() error = nil, want checkout failure")
	}
	if _, statErr := os.Lstat(filepath.Join(repository, "README.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial checkout artifact remains: %v", statErr)
	}
	if _, statErr := os.Lstat(repository); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created repository root remains: %v", statErr)
	}
}

func TestProvisionerRemoteImportFailurePreservesConcurrentPlannedFile(t *testing.T) {
	for _, failCheckout := range []bool{true, false} {
		t.Run(fmt.Sprintf("checkout failure %t", failCheckout), func(t *testing.T) {
			localRunner := newLocalRemoteRunner(createLocalRemote(t, "main", true))
			failingRunner := &failingCommandRunner{delegate: localRunner, command: "checkout"}
			if !failCheckout {
				failingRunner.command = "config"
			}
			provisioner, repository := newTestProvisioner(t, failingRunner)
			addConcurrentFile := func() {
				write(t, filepath.Join(repository, "README.md"), "concurrent external data\n")
			}
			if failCheckout {
				failingRunner.beforeFail = addConcurrentFile
			} else {
				failingRunner.afterRun = func(request portyprocess.Request) {
					if containsString(request.Args, "checkout") {
						addConcurrentFile()
					}
				}
			}

			if _, _, err := provisioner.Provision(context.Background(), remoteProvisionRequest("main")); err == nil {
				t.Fatal("Provision() error = nil, want failure")
			}
			if got, err := os.ReadFile(filepath.Join(repository, "README.md")); err != nil || string(got) != "concurrent external data\n" {
				t.Fatalf("concurrent planned file = %q, err=%v", got, err)
			}
			if _, err := os.Lstat(filepath.Join(repository, ".git")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("attempt-created metadata remains: %v", err)
			}
		})
	}
}

func TestProvisionerInspectPathClassifiesEmptyDirectory(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}

	inspection, err := provisioner.InspectPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != domain.RepositoryPathEmpty {
		t.Fatalf("state = %q, want %q", inspection.State, domain.RepositoryPathEmpty)
	}
}

func TestProvisionerInspectPathRejectsOccupiedNonRepository(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	write(t, filepath.Join(repository, "keep.txt"), "occupied\n")

	inspection, err := provisioner.InspectPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != domain.RepositoryPathOccupied {
		t.Fatalf("state = %q, want %q", inspection.State, domain.RepositoryPathOccupied)
	}
}

func TestProvisionerInspectPathRejectsRepositoryRootSymlink(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	outside := t.TempDir()
	if err := os.Symlink(outside, repository); err != nil {
		t.Fatal(err)
	}

	inspection, err := provisioner.InspectPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != domain.RepositoryPathInvalid {
		t.Fatalf("state = %q, want %q", inspection.State, domain.RepositoryPathInvalid)
	}
}

func TestProvisionerInspectPathRejectsHostileGitConfiguration(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "config", "diff.hostile.textconv", "/tmp/evil")

	inspection, err := provisioner.InspectPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != domain.RepositoryPathInvalid {
		t.Fatalf("state = %q, want %q", inspection.State, domain.RepositoryPathInvalid)
	}
}

func TestProvisionerInspectPathRejectsMultipleOriginURLs(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "remote", "add", "origin", "file:///tmp/unsafe.git")
	runGit(t, repository, "config", "--add", "remote.origin.url", "https://example.com/team/repo.git")

	inspection, err := provisioner.InspectPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != domain.RepositoryPathInvalid {
		t.Fatalf("state = %q, want %q", inspection.State, domain.RepositoryPathInvalid)
	}
}

func TestProvisionerProvisionInitCreatesRequestedUnbornBranchAndIdentity(t *testing.T) {
	recorder := &recordingDelegateRunner{delegate: portyprocess.NewRunner()}
	provisioner, repository := newTestProvisioner(t, recorder)
	author := domain.GitIdentity{Name: "Ada Lovelace", Email: "ada@example.invalid"}

	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode: domain.RepositorySetupInit, Branch: "trunk", Author: author,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, repository, "symbolic-ref", "--short", "HEAD"); got != "trunk\n" {
		t.Fatalf("branch = %q, want trunk", got)
	}
	if got := runGit(t, repository, "config", "--local", "user.name"); got != author.Name+"\n" {
		t.Fatalf("user.name = %q", got)
	}
	if got := runGit(t, repository, "config", "--local", "user.email"); got != author.Email+"\n" {
		t.Fatalf("user.email = %q", got)
	}
	if configuration.Root != repository || configuration.Branch != "trunk" || configuration.Author != author {
		t.Fatalf("configuration = %#v", configuration)
	}
	wantCommands := [][]string{
		hardened("init", "-b", "trunk", repository),
		hardened("-C", repository, "config", "--local", "user.name", author.Name),
		hardened("-C", repository, "config", "--local", "user.email", author.Email),
	}
	if !containsCommands(recorder.args(), wantCommands) {
		t.Fatalf("commands = %#v, want ordered commands %#v", recorder.args(), wantCommands)
	}
}

func TestProvisionerProvisionInitDoesNotCreateOrigin(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode:   domain.RepositorySetupInit,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Porty", Email: "porty@example.invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Remote != nil {
		t.Fatalf("managed remote = %#v, want nil", configuration.Remote)
	}
	if got := runGit(t, repository, "remote"); got != "" {
		t.Fatalf("git remote = %q, want none", got)
	}
}

func TestProvisionerProvisionInitRejectsNonEmptyDirectory(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	write(t, filepath.Join(repository, "keep.txt"), "do not replace\n")

	_, _, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode:   domain.RepositorySetupInit,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Porty", Email: "porty@example.invalid"},
	})
	if !errors.Is(err, application.ErrRepositoryPathNotEmpty) {
		t.Fatalf("Provision() error = %v, want ErrRepositoryPathNotEmpty", err)
	}
	if got, err := os.ReadFile(filepath.Join(repository, "keep.txt")); err != nil || string(got) != "do not replace\n" {
		t.Fatalf("occupied directory was changed: contents=%q err=%v", got, err)
	}
}

func TestProvisionerProvisionAdoptDetectsBranchIdentityAndOrigin(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "remote", "add", "origin", "https://example.com/team/repo.git")
	author := domain.GitIdentity{Name: "New Author", Email: "new@example.invalid"}

	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode: domain.RepositorySetupAdopt, Branch: "main", Author: author, ManageExistingRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Author != author || configuration.Remote == nil {
		t.Fatalf("configuration = %#v", configuration)
	}
	if configuration.Remote.Name != "origin" || configuration.Remote.URL != "https://example.com/team/repo.git" || !configuration.Remote.Managed {
		t.Fatalf("remote = %#v", configuration.Remote)
	}
}

func TestProvisionerProvisionAdoptRejectsDetachedHead(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "commit", "--allow-empty", "-m", "initial")
	runGit(t, repository, "checkout", "--detach", "HEAD")

	_, _, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode:   domain.RepositorySetupAdopt,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Porty", Email: "porty@example.invalid"},
	})
	if !errors.Is(err, application.ErrDetachedHead) {
		t.Fatalf("Provision() error = %v, want ErrDetachedHead", err)
	}
}

func TestProvisionerProvisionAdoptLeavesIgnoredOriginUntouched(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	const remoteURL = "https://example.com/team/repo.git"
	runGit(t, repository, "remote", "add", "origin", remoteURL)

	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode:   domain.RepositorySetupAdopt,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Porty", Email: "porty@example.invalid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Remote != nil {
		t.Fatalf("managed remote = %#v, want nil", configuration.Remote)
	}
	if got := runGit(t, repository, "remote", "get-url", "origin"); got != remoteURL+"\n" {
		t.Fatalf("origin = %q, want unchanged", got)
	}
}

func TestProvisionerProvisionRetryAcceptsExactInitializedRepository(t *testing.T) {
	provisioner, _ := newTestProvisioner(t, portyprocess.NewRunner())
	request := application.RepositoryProvisionRequest{
		Mode:   domain.RepositorySetupInit,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Porty", Email: "porty@example.invalid"},
	}
	if _, _, err := provisioner.Provision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, configuration, err := provisioner.Provision(context.Background(), request); err != nil {
		t.Fatalf("retry failed: %v", err)
	} else if configuration.Branch != request.Branch || configuration.Author != request.Author || configuration.Remote != nil {
		t.Fatalf("retry configuration = %#v", configuration)
	}
}

func TestProvisionerProvisionRetryRejectsRepositoryWithHistory(t *testing.T) {
	provisioner, repository := newTestProvisioner(t, portyprocess.NewRunner())
	initializeRepositoryAt(t, repository)
	runGit(t, repository, "commit", "--allow-empty", "-m", "existing history")

	_, _, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode:   domain.RepositorySetupInit,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Existing Author", Email: "existing@example.invalid"},
	})
	if !errors.Is(err, application.ErrRepositoryPathNotEmpty) {
		t.Fatalf("Provision() error = %v, want ErrRepositoryPathNotEmpty", err)
	}
}

func TestProvisionerProvisionAdoptUsesFinalOriginSnapshot(t *testing.T) {
	dataDirectory := t.TempDir()
	repository := filepath.Join(dataDirectory, "repository")
	initializeRepositoryAt(t, repository)
	const initialURL = "https://example.com/team/initial.git"
	const finalURL = "https://example.com/team/final.git"
	runGit(t, repository, "remote", "add", "origin", initialURL)
	author := domain.GitIdentity{Name: "New Author", Email: "new@example.invalid"}
	mutated := false
	runner := &recordingDelegateRunner{delegate: portyprocess.NewRunner()}
	runner.after = func(request portyprocess.Request) {
		if mutated || !reflect.DeepEqual(request.Args, hardened("-C", repository, "config", "--local", "user.email", author.Email)) {
			return
		}
		mutated = true
		runGit(t, repository, "remote", "set-url", "origin", finalURL)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := gitcli.NewProvisioner(dataDirectory, runner, executable)
	if err != nil {
		t.Fatal(err)
	}

	_, configuration, err := provisioner.Provision(context.Background(), application.RepositoryProvisionRequest{
		Mode: domain.RepositorySetupAdopt, Branch: "main", Author: author, ManageExistingRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !mutated {
		t.Fatal("origin was not mutated before final activation")
	}
	if configuration.Remote == nil || configuration.Remote.URL != finalURL || !configuration.Remote.Managed {
		t.Fatalf("remote = %#v, want managed final origin %q", configuration.Remote, finalURL)
	}
}

type remoteInspectionRunner struct {
	result   portyprocess.Result
	err      error
	requests []portyprocess.Request
	deadline time.Duration
}

func (r *remoteInspectionRunner) Run(ctx context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	requestCopy := request
	requestCopy.Args = append([]string(nil), request.Args...)
	requestCopy.Env = append([]string(nil), request.Env...)
	requestCopy.Redact = append([]string(nil), request.Redact...)
	r.requests = append(r.requests, requestCopy)
	if deadline, ok := ctx.Deadline(); ok {
		r.deadline = time.Until(deadline)
	}
	return r.result, r.err
}

type localRemoteRunner struct {
	delegate         Runner
	localPath        string
	remoteURL        string
	requests         []portyprocess.Request
	fetchDeadline    time.Duration
	commandDeadlines map[string]time.Duration
}

type failingCommandRunner struct {
	delegate     Runner
	command      string
	beforeFail   func()
	afterRun     func(portyprocess.Request)
	failAfterRun bool
}

func (r *failingCommandRunner) Run(ctx context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	if containsString(request.Args, r.command) && !r.failAfterRun {
		if r.beforeFail != nil {
			r.beforeFail()
		}
		return portyprocess.Result{Output: "fatal: remote transport unavailable", ExitCode: 128}, errors.New("remote command failed")
	}
	result, err := r.delegate.Run(ctx, request)
	if r.afterRun != nil {
		r.afterRun(request)
	}
	if containsString(request.Args, r.command) && r.failAfterRun {
		return portyprocess.Result{Output: "fatal: remote transport unavailable", ExitCode: 128}, errors.New("remote command failed")
	}
	return result, err
}

func newLocalRemoteRunner(localPath string) *localRemoteRunner {
	return &localRemoteRunner{
		delegate:  portyprocess.NewRunner(),
		localPath: localPath,
		remoteURL: "https://example.com/team/repo.git",
	}
}

func (r *localRemoteRunner) Run(ctx context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	if r.commandDeadlines == nil {
		r.commandDeadlines = make(map[string]time.Duration)
	}
	for _, command := range []string{"ls-tree", "checkout"} {
		if containsString(request.Args, command) {
			if deadline, ok := ctx.Deadline(); ok {
				r.commandDeadlines[command] = time.Until(deadline)
			}
		}
	}
	if containsString(request.Args, "fetch") {
		if deadline, ok := ctx.Deadline(); ok {
			r.fetchDeadline = time.Until(deadline)
		}
	}
	requestCopy := request
	requestCopy.Args = append([]string(nil), request.Args...)
	requestCopy.Env = append([]string(nil), request.Env...)
	requestCopy.Redact = append([]string(nil), request.Redact...)
	r.requests = append(r.requests, requestCopy)

	rewritten := request
	rewritten.Args = append([]string(nil), request.Args...)
	if containsString(rewritten.Args, "ls-remote") {
		for index, argument := range rewritten.Args {
			if argument == testRemoteURL || argument == r.remoteURL {
				rewritten.Args[index] = r.localPath
			}
		}
	}
	if containsString(rewritten.Args, "fetch") {
		for index, argument := range rewritten.Args {
			if argument == "origin" || argument == "porty-candidate" {
				rewritten.Args[index] = r.localPath
			}
		}
	}
	return r.delegate.Run(ctx, rewritten)
}

func (r *localRemoteRunner) args() [][]string {
	result := make([][]string, 0, len(r.requests))
	for _, request := range r.requests {
		result = append(result, request.Args)
	}
	return result
}

func remoteProvisionRequest(branch string) application.RepositoryProvisionRequest {
	return application.RepositoryProvisionRequest{
		Mode:           domain.RepositorySetupRemote,
		Branch:         branch,
		Author:         domain.GitIdentity{Name: "Porty", Email: "porty@example.invalid"},
		RemoteURL:      testRemoteURL,
		Authentication: domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone},
	}
}

func createLocalRemote(t *testing.T, branch string, populated bool) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", remote)
	if !populated {
		return remote
	}
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "init", "-b", branch)
	runGit(t, source, "config", "user.name", "Remote Author")
	runGit(t, source, "config", "user.email", "remote@example.invalid")
	write(t, filepath.Join(source, "README.md"), "remote repository\n")
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "initial")
	runGit(t, source, "remote", "add", "origin", remote)
	runGit(t, source, "push", "origin", branch)
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return remote
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type recordingDelegateRunner struct {
	delegate Runner
	requests []portyprocess.Request
	after    func(portyprocess.Request)
}

type Runner interface {
	Run(context.Context, portyprocess.Request) (portyprocess.Result, error)
}

func (r *recordingDelegateRunner) Run(ctx context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	copy := request
	copy.Args = append([]string(nil), request.Args...)
	copy.Env = append([]string(nil), request.Env...)
	r.requests = append(r.requests, copy)
	result, err := r.delegate.Run(ctx, request)
	if err == nil && r.after != nil {
		r.after(copy)
	}
	return result, err
}

func (r *recordingDelegateRunner) args() [][]string {
	result := make([][]string, 0, len(r.requests))
	for _, request := range r.requests {
		result = append(result, request.Args)
	}
	return result
}

func newTestProvisioner(t *testing.T, runner Runner) (*gitcli.Provisioner, string) {
	t.Helper()
	dataDirectory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := gitcli.NewProvisioner(dataDirectory, runner, executable)
	if err != nil {
		t.Fatal(err)
	}
	return provisioner, filepath.Join(dataDirectory, "repository")
}

func initializeRepositoryAt(t *testing.T, repository string) {
	t.Helper()
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-b", "main")
	runGit(t, repository, "config", "user.name", "Existing Author")
	runGit(t, repository, "config", "user.email", "existing@example.invalid")
}

func containsCommands(got, want [][]string) bool {
	wantIndex := 0
	for _, command := range got {
		if wantIndex < len(want) && reflect.DeepEqual(command, want[wantIndex]) {
			wantIndex++
		}
	}
	return wantIndex == len(want)
}
