package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	portyrepo "github.com/msoldin/porty/internal/repository"
)

func (c *Client) openRepository() (*gitlib.Repository, error) {
	for _, path := range []string{c.repo, filepath.Join(c.repo, ".git")} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafeRepository
		}
	}
	if err := validateMetadataFiles(filepath.Join(c.repo, ".git")); err != nil {
		return nil, err
	}
	return gitlib.PlainOpen(c.repo)
}

func (c *Client) stackDirectory(stack string) error {
	if !validStackPath(stack) {
		return ErrInvalidCommit
	}
	info, err := os.Lstat(filepath.Join(c.repo, stack))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeRepository
	}
	return nil
}

func (c *Client) sdkStatus(ctx context.Context) (portyrepo.GitStatus, error) {
	configured, err := c.configured()
	if err != nil {
		return portyrepo.GitStatus{}, err
	}
	result := portyrepo.GitStatus{Configured: configured, Branch: c.branch}
	if !configured {
		return result, nil
	}
	repo, err := c.openRepository()
	if err != nil {
		return portyrepo.GitStatus{}, err
	}
	defer repo.Close()
	if head, err := repo.Storer.Reference(plumbing.HEAD); err == nil && head.Type() == plumbing.SymbolicReference {
		result.Branch = head.Target().Short()
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return portyrepo.GitStatus{}, err
	}
	status, err := worktree.Status()
	if err != nil {
		return portyrepo.GitStatus{}, err
	}
	for name, file := range status {
		if file.Staging != gitlib.Unmodified || file.Worktree != gitlib.Unmodified {
			result.Paths = append(result.Paths, name)
		}
	}
	sort.Strings(result.Paths)
	result.Dirty = len(result.Paths) != 0
	if err := c.setDivergence(ctx, repo, &result); err != nil {
		return portyrepo.GitStatus{}, err
	}
	return result, nil
}

func (c *Client) sdkHead(context.Context) (string, error) {
	repo, err := c.openRepository()
	if err != nil {
		return "", err
	}
	defer repo.Close()
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

func (c *Client) sdkCommit(_ context.Context, stack, message string) (string, error) {
	message = strings.TrimSpace(message)
	if !validStackPath(stack) || message == "" || len(message) > 4096 || strings.ContainsRune(message, 0) {
		return "", ErrInvalidCommit
	}
	if err := c.stackDirectory(stack); err != nil {
		return "", err
	}
	repo, err := c.openRepository()
	if err != nil {
		return "", err
	}
	defer repo.Close()
	cfg, err := repo.Config()
	if err != nil {
		return "", err
	}
	if cfg.User.Name == "" || cfg.User.Email == "" {
		return "", errors.New("Git author is not configured")
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	originalIndex, err := repo.Storer.Index()
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err == nil {
		if err := worktree.Reset(&gitlib.ResetOptions{Commit: head.Hash(), Mode: gitlib.MixedReset}); err != nil {
			return "", err
		}
	} else if errors.Is(err, plumbing.ErrReferenceNotFound) {
		if err := repo.Storer.SetIndex(&index.Index{Version: 2}); err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = repo.Storer.SetIndex(originalIndex)
		}
	}()
	if err := worktree.AddWithOptions(&gitlib.AddOptions{Path: stack}); err != nil {
		return "", err
	}
	hash, err := worktree.Commit(message, &gitlib.CommitOptions{Author: &object.Signature{
		Name: cfg.User.Name, Email: cfg.User.Email, When: time.Now(),
	}})
	if err != nil {
		return "", err
	}
	committedIndex, err := repo.Storer.Index()
	if err != nil {
		return "", err
	}
	committedIndex.Entries = append(committedIndex.Entries[:0], onlyStackEntries(committedIndex.Entries, stack)...)
	for _, entry := range originalIndex.Entries {
		if !inStack(entry.Name, stack) {
			committedIndex.Entries = append(committedIndex.Entries, entry)
		}
	}
	committedIndex.Cache = nil
	if err := repo.Storer.SetIndex(committedIndex); err != nil {
		return "", err
	}
	committed = true
	return hash.String(), nil
}

func inStack(path, stack string) bool {
	return path == stack || strings.HasPrefix(path, stack+"/")
}

func onlyStackEntries(entries []*index.Entry, stack string) []*index.Entry {
	var selected []*index.Entry
	for _, entry := range entries {
		if inStack(entry.Name, stack) {
			selected = append(selected, entry)
		}
	}
	return selected
}
