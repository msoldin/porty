package gitcli_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/msoldin/porty/internal/infrastructure/gitcli"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
)

func TestValidationRejectsCommandBearingInputs(t *testing.T) {
	for _, value := range []string{"file:///tmp/repo", "git://example.com/repo", "https://example.com/repo\n--upload-pack=evil", "-oProxyCommand=evil"} {
		if err := gitcli.ValidateRemoteURL(value); !errors.Is(err, gitcli.ErrInvalidRemote) {
			t.Fatalf("ValidateRemoteURL(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"-main", "feature..evil", "refs/heads/main", "main lock", "main~1"} {
		if err := gitcli.ValidateBranch(value); !errors.Is(err, gitcli.ErrInvalidBranch) {
			t.Fatalf("ValidateBranch(%q) = %v", value, err)
		}
	}
	if err := gitcli.ValidateRemoteURL("https://example.com/team/repo.git"); err != nil {
		t.Fatal(err)
	}
	if err := gitcli.ValidateRemoteURL("ssh://git@example.com/team/repo.git"); err != nil {
		t.Fatal(err)
	}
	if err := gitcli.ValidateBranch("release/v1"); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkCommandsUseFixedArgumentsAndHardenedEnvironment(t *testing.T) {
	runner := &recordingRunner{}
	client, err := gitcli.New(runner, "/srv/porty/repository", "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.PullFastForward(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-c", "core.hooksPath=/dev/null", "fetch", "--no-tags", "--prune", "origin", "main"},
		{"-c", "core.hooksPath=/dev/null", "merge", "--ff-only", "origin/main"},
		{"-c", "core.hooksPath=/dev/null", "push", "--", "origin", "HEAD:refs/heads/main"},
	}
	if !reflect.DeepEqual(runner.args(), want) {
		t.Fatalf("commands = %#v, want %#v", runner.args(), want)
	}
	for _, request := range runner.requests {
		if !request.CleanEnv {
			t.Fatal("Git request inherited the ambient environment")
		}
		joined := strings.Join(request.Env, "\n")
		for _, required := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("environment missing %q: %#v", required, request.Env)
			}
		}
	}
}

func TestStatusParsesAheadAndBehindDivergence(t *testing.T) {
	runner := &staticRunner{result: portyprocess.Result{Output: "## main...origin/main [ahead 2, behind 3]\x00 M alpha/docker-compose.yml\x00"}}
	client, err := gitcli.New(runner, "/srv/porty/repository", "main")
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Ahead != 2 || status.Behind != 3 || !status.Dirty {
		t.Fatalf("Status() = %#v", status)
	}
}

func TestCloneInitAndAdoptContracts(t *testing.T) {
	runner := &recordingRunner{}
	ctx := context.Background()
	if err := gitcli.Clone(ctx, runner, "https://example.com/team/repo.git", "/srv/porty/repository", "main"); err != nil {
		t.Fatal(err)
	}
	if err := gitcli.Init(ctx, runner, "/srv/porty/new-repository", "main"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-c", "core.hooksPath=/dev/null", "clone", "--no-tags", "--single-branch", "--branch", "main", "--", "https://example.com/team/repo.git", "repository"},
		{"-c", "core.hooksPath=/dev/null", "init", "-b", "main", "new-repository"},
	}
	if !reflect.DeepEqual(runner.args(), want) {
		t.Fatalf("commands = %#v, want %#v", runner.args(), want)
	}
}

func TestHTTPSCredentialsStayOutOfArgumentsAndAreRedacted(t *testing.T) {
	runner := &recordingRunner{}
	client, err := gitcli.New(runner, "/srv/porty/repository", "main")
	if err != nil {
		t.Fatal(err)
	}
	client, err = client.WithHTTPSCredentials("/usr/libexec/porty-git-askpass", "deploy", "top-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := runner.requests[0]
	if strings.Contains(strings.Join(request.Args, " "), "top-secret") {
		t.Fatal("secret appeared in Git arguments")
	}
	if !reflect.DeepEqual(request.Redact, []string{"top-secret"}) {
		t.Fatalf("redactions = %#v", request.Redact)
	}
	environment := strings.Join(request.Env, "\n")
	for _, expected := range []string{"GIT_ASKPASS=/usr/libexec/porty-git-askpass", "PORTY_GIT_USERNAME=deploy", "PORTY_GIT_PASSWORD=top-secret"} {
		if !strings.Contains(environment, expected) {
			t.Fatalf("environment missing %q", expected)
		}
	}
}

func TestStatusParsingAndStackScopedCommitInRealRepository(t *testing.T) {
	repository := initRepository(t)
	write(t, filepath.Join(repository, "alpha", "docker-compose.yml"), "services: {}\n")
	write(t, filepath.Join(repository, "beta", "docker-compose.yml"), "services: {}\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "initial")
	write(t, filepath.Join(repository, "alpha", "docker-compose.yml"), "services:\n  web: {}\n")
	write(t, filepath.Join(repository, "beta", "docker-compose.yml"), "services:\n  db: {}\n")

	client, err := gitcli.New(portyprocess.NewRunner(), repository, "main")
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Dirty || len(status.Paths) != 2 {
		t.Fatalf("Status() = %#v", status)
	}
	if _, err := client.Commit(context.Background(), "alpha", "update alpha"); err != nil {
		t.Fatal(err)
	}
	changed := strings.Fields(runGit(t, repository, "show", "--pretty=format:", "--name-only", "HEAD"))
	if !reflect.DeepEqual(changed, []string{"alpha/docker-compose.yml"}) {
		t.Fatalf("committed paths = %#v", changed)
	}
	remaining := runGit(t, repository, "status", "--porcelain")
	if !strings.Contains(remaining, "beta/docker-compose.yml") {
		t.Fatalf("other stack change was lost: %q", remaining)
	}
}

func TestAdoptRejectsHostileLocalConfiguration(t *testing.T) {
	repository := initRepository(t)
	runGit(t, repository, "config", "core.fsmonitor", "/tmp/evil")
	client, err := gitcli.New(portyprocess.NewRunner(), repository, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ValidateSafety(context.Background()); !errors.Is(err, gitcli.ErrUnsafeRepository) {
		t.Fatalf("ValidateSafety() = %v, want ErrUnsafeRepository", err)
	}
}

type recordingRunner struct{ requests []portyprocess.Request }

func (r *recordingRunner) Run(_ context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	r.requests = append(r.requests, request)
	return portyprocess.Result{}, nil
}

type staticRunner struct{ result portyprocess.Result }

func (r *staticRunner) Run(context.Context, portyprocess.Request) (portyprocess.Result, error) {
	return r.result, nil
}

func (r *recordingRunner) args() [][]string {
	result := make([][]string, 0, len(r.requests))
	for _, request := range r.requests {
		result = append(result, request.Args)
	}
	return result
}

func initRepository(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	runGit(t, directory, "init", "-b", "main")
	runGit(t, directory, "config", "user.name", "Porty Test")
	runGit(t, directory, "config", "user.email", "porty@example.invalid")
	return directory
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return string(output)
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
