package gitcli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	"github.com/msoldin/porty/internal/infrastructure/gitcli"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
)

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
