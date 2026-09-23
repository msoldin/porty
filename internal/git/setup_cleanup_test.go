package git

import (
	"errors"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryAttemptCleanupPreservesSubstitutedMetadataDuringOwnershipCapture(t *testing.T) {
	repository := t.TempDir()
	root, err := os.OpenRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	replacingRoot := &metadataReplacingRoot{Root: root, testing: t}
	gitRoot, gitInfo, reserveErr := reserveGitDirectory(replacingRoot)
	if gitRoot != nil {
		defer gitRoot.Close()
	}
	if !replacingRoot.replaced {
		t.Fatal("metadata ownership boundary was not exercised")
	}
	repositoryInfo, err := root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	attempt := repositoryAttempt{repositoryRoot: root, repositoryInfo: repositoryInfo, gitRoot: gitRoot, gitInfo: gitInfo}
	attempt.cleanup()
	content, err := os.ReadFile(filepath.Join(repository, ".git", "external.txt"))
	if err != nil || string(content) != "external metadata\n" {
		t.Fatalf("external metadata = %q, err = %v (reservation error = %v)", content, err, reserveErr)
	}
	if !errors.Is(reserveErr, portyrepo.ErrInvalidWorktree) {
		t.Fatalf("reservation error = %v, want ErrInvalidWorktree", reserveErr)
	}
}

func TestReserveGitDirectoryPreservesConcurrentMetadata(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(".git/external.txt", []byte("external metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRoot, _, err := reserveGitDirectory(root)
	if gitRoot != nil {
		gitRoot.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("reservation error = %v, want ErrExist", err)
	}
	if content, err := root.ReadFile(".git/external.txt"); err != nil || string(content) != "external metadata\n" {
		t.Fatalf("external metadata = %q, err = %v", content, err)
	}
	directory, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".git" {
		t.Fatalf("private staging artifacts remain: entries = %v, err = %v", entries, err)
	}
}

func TestMetadataCleanupUsesCapturedDescriptorAfterReplacement(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	gitRoot, _, err := reserveGitDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer gitRoot.Close()
	if err := gitRoot.WriteFile("owned.txt", []byte("owned metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacingRoot := &metadataReplacingRoot{Root: root, testing: t}
	if _, err := replacingRoot.Lstat(".git"); err != nil {
		t.Fatal(err)
	}
	if err := removeRootContents(gitRoot); err != nil {
		t.Fatal(err)
	}
	if content, err := root.ReadFile(".git/external.txt"); err != nil || string(content) != "external metadata\n" {
		t.Fatalf("external metadata = %q, err = %v", content, err)
	}
	if _, err := root.Lstat("created-metadata/owned.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned metadata was not cleaned: %v", err)
	}
}

type metadataReplacingRoot struct {
	*os.Root
	testing  *testing.T
	replaced bool
}

func (root *metadataReplacingRoot) Lstat(name string) (os.FileInfo, error) {
	if name == ".git" && !root.replaced {
		root.replaced = true
		if err := root.Root.Rename(".git", "created-metadata"); err != nil {
			root.testing.Fatal(err)
		}
		if err := root.Root.Mkdir(".git", 0o700); err != nil {
			root.testing.Fatal(err)
		}
		if err := root.Root.WriteFile(".git/external.txt", []byte("external metadata\n"), 0o600); err != nil {
			root.testing.Fatal(err)
		}
	}
	return root.Root.Lstat(name)
}
