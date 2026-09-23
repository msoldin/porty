package git

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	portyrepo "github.com/msoldin/porty/internal/repository"
)

type repositoryArtifact struct {
	name string
	info os.FileInfo
}

type repositoryAttempt struct {
	dataRoot       *os.Root
	repositoryRoot *os.Root
	gitRoot        *os.Root
	repositoryInfo os.FileInfo
	gitInfo        os.FileInfo
	rootCreated    bool
	worktree       []repositoryArtifact
}

func (p *Provisioner) newRepositoryAttempt() (*repositoryAttempt, error) {
	dataRoot, err := os.OpenRoot(p.dataRoot)
	if err != nil {
		return nil, err
	}
	attempt := &repositoryAttempt{dataRoot: dataRoot}
	prepared := false
	defer func() {
		if !prepared {
			attempt.cleanup()
			attempt.close()
		}
	}()
	repositoryInfo, err := dataRoot.Lstat(repositoryDirectoryName)
	if errors.Is(err, os.ErrNotExist) {
		if err := dataRoot.Mkdir(repositoryDirectoryName, 0o700); err != nil {
			return nil, err
		}
		attempt.rootCreated = true
		repositoryInfo, err = dataRoot.Lstat(repositoryDirectoryName)
	}
	if err != nil {
		return nil, err
	}
	if !repositoryInfo.IsDir() || repositoryInfo.Mode()&os.ModeSymlink != 0 {
		return nil, portyrepo.ErrInvalidWorktree
	}
	repositoryRoot, err := dataRoot.OpenRoot(repositoryDirectoryName)
	if err != nil {
		return nil, err
	}
	attempt.repositoryRoot = repositoryRoot
	attempt.repositoryInfo = repositoryInfo
	attempt.gitRoot, attempt.gitInfo, err = reserveGitDirectory(repositoryRoot)
	if err != nil {
		return nil, err
	}
	prepared = true
	return attempt, nil
}

type gitDirectoryRoot interface {
	Mkdir(string, os.FileMode) error
	Lstat(string) (os.FileInfo, error)
	OpenRoot(string) (*os.Root, error)
	Open(string) (*os.File, error)
	Remove(string) error
}

func reserveGitDirectory(root gitDirectoryRoot) (*os.Root, os.FileInfo, error) {
	stagingName := ".porty-metadata-" + rand.Text()
	if err := root.Mkdir(stagingName, 0o700); err != nil {
		return nil, nil, err
	}
	defer root.Remove(stagingName)
	stagingRoot, err := root.OpenRoot(stagingName)
	if err != nil {
		return nil, nil, err
	}
	defer stagingRoot.Close()
	if err := stagingRoot.Mkdir("metadata", 0o700); err != nil {
		return nil, nil, err
	}
	defer stagingRoot.Remove("metadata")
	gitRoot, err := stagingRoot.OpenRoot("metadata")
	if err != nil {
		return nil, nil, err
	}
	prepared := false
	defer func() {
		if !prepared {
			gitRoot.Close()
		}
	}()
	info, err := gitRoot.Stat(".")
	if err != nil {
		return nil, nil, err
	}
	source, err := stagingRoot.Open(".")
	if err != nil {
		return nil, nil, err
	}
	defer source.Close()
	destination, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	defer destination.Close()
	if err := publishGitMetadata(source, destination); err != nil {
		return nil, nil, err
	}
	publishedInfo, err := root.Lstat(".git")
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, publishedInfo) {
		return nil, nil, portyrepo.ErrInvalidWorktree
	}
	prepared = true
	return gitRoot, info, nil
}

func (a *repositoryAttempt) captureInitializedRepository() error {
	repositoryInfo, err := a.dataRoot.Lstat(repositoryDirectoryName)
	if err != nil {
		return err
	}
	if !repositoryInfo.IsDir() || repositoryInfo.Mode()&os.ModeSymlink != 0 {
		return portyrepo.ErrInvalidWorktree
	}
	if a.repositoryInfo != nil && !os.SameFile(a.repositoryInfo, repositoryInfo) {
		return portyrepo.ErrInvalidWorktree
	}
	if a.repositoryRoot == nil {
		a.repositoryRoot, err = a.dataRoot.OpenRoot(repositoryDirectoryName)
		if err != nil {
			return err
		}
	}
	a.repositoryInfo = repositoryInfo
	gitInfo, err := a.repositoryRoot.Lstat(".git")
	if err != nil {
		return err
	}
	if !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(a.gitInfo, gitInfo) {
		return portyrepo.ErrInvalidWorktree
	}
	return nil
}

func (a *repositoryAttempt) publishWorktree() error {
	stagingRoot, err := a.repositoryRoot.OpenRoot(checkoutStagingDirectory)
	if err != nil {
		return err
	}
	defer stagingRoot.Close()
	err = fs.WalkDir(stagingRoot.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name = filepath.FromSlash(name)
		if entry.IsDir() {
			if err := a.repositoryRoot.Mkdir(name, info.Mode().Perm()); err != nil {
				return err
			}
			info, err = a.repositoryRoot.Lstat(name)
			if err != nil {
				return err
			}
		} else if err := a.repositoryRoot.Link(filepath.Join(checkoutStagingDirectory, name), name); err != nil {
			return err
		}
		a.worktree = append(a.worktree, repositoryArtifact{name: name, info: info})
		return nil
	})
	if err != nil {
		return err
	}
	if err := removeRootContents(stagingRoot); err != nil {
		return err
	}
	return a.gitRoot.Remove("porty-checkout")
}

func removeRootContents(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			child, err := root.OpenRoot(entry.Name())
			if err != nil {
				return err
			}
			err = removeRootContents(child)
			child.Close()
			if err != nil {
				return err
			}
		}
		if err := root.Remove(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (a *repositoryAttempt) cleanup() {
	if a.repositoryRoot == nil || a.repositoryInfo == nil {
		return
	}
	sort.Slice(a.worktree, func(left, right int) bool {
		return strings.Count(a.worktree[left].name, "/") > strings.Count(a.worktree[right].name, "/")
	})
	for _, artifact := range a.worktree {
		current, err := a.repositoryRoot.Lstat(artifact.name)
		if err == nil && os.SameFile(current, artifact.info) {
			_ = a.repositoryRoot.Remove(artifact.name)
		}
	}
	if a.gitInfo != nil && a.gitRoot != nil {
		current, err := a.repositoryRoot.Lstat(".git")
		if err == nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, a.gitInfo) {
			if removeRootContents(a.gitRoot) == nil {
				_ = a.repositoryRoot.Remove(".git")
			}
		}
	}
	if a.rootCreated {
		current, err := a.dataRoot.Lstat(repositoryDirectoryName)
		if err == nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, a.repositoryInfo) {
			_ = a.dataRoot.Remove(repositoryDirectoryName)
		}
	}
}

func (a *repositoryAttempt) close() {
	if a.gitRoot != nil {
		_ = a.gitRoot.Close()
	}
	if a.repositoryRoot != nil {
		_ = a.repositoryRoot.Close()
	}
	_ = a.dataRoot.Close()
}
