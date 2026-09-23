package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func testRepository(t *testing.T) (*gitlib.Repository, string, *Client) {
	t.Helper()
	path := t.TempDir()
	repo, err := gitlib.PlainInit(path, false, gitlib.WithDefaultBranch(plumbing.NewBranchReferenceName("main")))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.User.Name, cfg.User.Email = "Porty Test", "porty@example.invalid"
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	client, err := New(path, "main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, path, client
}

func testFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testCommit(t *testing.T, repo *gitlib.Repository, name, message string) plumbing.Hash {
	t.Helper()
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit(message, &gitlib.CommitOptions{Author: &object.Signature{Name: "Porty Test", Email: "porty@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestRepositoryOperationsWithoutGitExecutable(t *testing.T) {
	repo, root, client := testRepository(t)
	status, err := client.Status(context.Background())
	if err != nil || !status.Configured || status.Branch != "main" || status.Dirty {
		t.Fatalf("unborn status = %+v, %v", status, err)
	}
	history, err := client.History(context.Background(), 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("unborn history = %+v, %v", history, err)
	}
	testFile(t, root, "alpha/docker-compose.yml", "services: {}\n")
	testFile(t, root, "beta/docker-compose.yml", "services: {}\n")
	t.Setenv("PATH", "")
	status, err = client.Status(context.Background())
	if err != nil || !status.Dirty || len(status.Paths) != 2 {
		t.Fatalf("dirty status = %+v, %v", status, err)
	}
	if _, err := client.Commit(context.Background(), "alpha", "add alpha"); err != nil {
		t.Fatal(err)
	}
	diff, err := client.Diff(context.Background(), "beta")
	if err != nil || !strings.Contains(diff, "+services: {}") {
		t.Fatalf("diff = %q, %v", diff, err)
	}
	status, err = client.Status(context.Background())
	if err != nil || len(status.Paths) != 1 || status.Paths[0] != "beta/docker-compose.yml" {
		t.Fatalf("post commit status = %+v, %v", status, err)
	}
	history, err = client.HistoryPage(context.Background(), 10, 0)
	if err != nil || len(history) != 1 || history[0].Subject != "add alpha" {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if _, err := repo.Head(); err != nil {
		t.Fatal(err)
	}
}

func TestCommitKeepsStagedSiblingOutOfCommit(t *testing.T) {
	repo, root, client := testRepository(t)
	testFile(t, root, "initial.txt", "initial\n")
	testCommit(t, repo, "initial.txt", "initial")
	testFile(t, root, "alpha/docker-compose.yml", "services: {}\n")
	testFile(t, root, "beta/docker-compose.yml", "services: {}\n")
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("beta/docker-compose.yml"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(context.Background(), "alpha", "add alpha"); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.File("beta/docker-compose.yml"); err == nil {
		t.Fatal("sibling included in commit")
	}
	index, err := repo.Storer.Index()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Entry("beta/docker-compose.yml"); err != nil {
		t.Fatalf("staged sibling lost: %v", err)
	}
}

func TestFetchPushAndFastForwardWorkWithLocalTransport(t *testing.T) {
	barePath := filepath.Join(t.TempDir(), "remote.git")
	bare, err := gitlib.PlainInit(barePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	local, root, client := testRepository(t)
	if _, err := local.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{barePath}}); err != nil {
		t.Fatal(err)
	}
	testFile(t, root, "alpha/docker-compose.yml", "services: {}\n")
	if _, err := client.Commit(context.Background(), "alpha", "first"); err != nil {
		t.Fatal(err)
	}
	if err := client.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	otherPath := t.TempDir()
	other, err := gitlib.PlainClone(otherPath, &gitlib.CloneOptions{URL: barePath, ReferenceName: plumbing.NewBranchReferenceName("main")})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	testFile(t, otherPath, "beta/docker-compose.yml", "services: {}\n")
	testCommit(t, other, "beta/docker-compose.yml", "second")
	if err := other.Push(&gitlib.PushOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background())
	if err != nil || status.Behind != 1 || status.Ahead != 0 {
		t.Fatalf("divergence = %+v, %v", status, err)
	}
	if err := client.PullFastForward(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "beta", "docker-compose.yml")); err != nil {
		t.Fatal(err)
	}
	if err := client.PullFastForward(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPullRejectsDirtyWorktreeWithoutChangingHead(t *testing.T) {
	repo, root, client := testRepository(t)
	testFile(t, root, "alpha/docker-compose.yml", "services: {}\n")
	before, err := client.Commit(context.Background(), "alpha", "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", "main"), plumbing.NewHash(before))); err != nil {
		t.Fatal(err)
	}
	testFile(t, root, "alpha/docker-compose.yml", "services:\n  app: {}\n")
	if err := client.PullFastForward(context.Background()); err == nil {
		t.Fatal("pull accepted dirty worktree")
	}
	after, err := client.Head(context.Background())
	if err != nil || after != before {
		t.Fatalf("head changed: %q, %v", after, err)
	}
}

func TestDiffBoundsBinaryAndLargeUntrackedFiles(t *testing.T) {
	_, root, client := testRepository(t)
	testFile(t, root, "alpha/binary.dat", "x\x00y")
	testFile(t, root, "alpha/large.dat", strings.Repeat("x", maxUntrackedFileBytes+1))
	diff, err := client.Diff(context.Background(), "alpha")
	if err != nil || !strings.Contains(diff, "Binary files") || len(diff) > maxDiffBytes {
		t.Fatalf("diff length %d, error %v", len(diff), err)
	}
}

func TestRepositoryRejectsUnsafeInputs(t *testing.T) {
	if ValidateBranch("-danger") == nil || ValidateBranch("main..other") == nil {
		t.Fatal("unsafe branch accepted")
	}
	if ValidateRemoteURL("https://user:secret@example.com/repo.git") == nil {
		t.Fatal("credential URL accepted")
	}
	if ValidateRemoteURL("file:///tmp/repo.git") == nil {
		t.Fatal("file remote accepted")
	}
	_, root, client := testRepository(t)
	if _, err := client.Commit(context.Background(), "../escape", "message"); !errors.Is(err, ErrInvalidCommit) {
		t.Fatalf("commit error = %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "stack")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Diff(context.Background(), "stack"); !errors.Is(err, ErrUnsafeRepository) {
		t.Fatalf("symlink diff = %v", err)
	}
}
