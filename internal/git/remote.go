package git

import (
	"context"
	"errors"
	"io"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	portyrepo "github.com/msoldin/porty/internal/repository"
)

func (c *Client) setDivergence(ctx context.Context, repo *gitlib.Repository, status *portyrepo.GitStatus) error {
	remote, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", status.Branch), true)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if head.Hash() == remote.Hash() {
		return nil
	}
	localCommits, err := reachableCommits(ctx, repo, head.Hash())
	if err != nil {
		return err
	}
	remoteCommits, err := reachableCommits(ctx, repo, remote.Hash())
	if err != nil {
		return err
	}
	for hash := range localCommits {
		if _, found := remoteCommits[hash]; !found {
			status.Ahead++
		}
	}
	for hash := range remoteCommits {
		if _, found := localCommits[hash]; !found {
			status.Behind++
		}
	}
	return nil
}

func reachableCommits(ctx context.Context, repo *gitlib.Repository, from plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	iter, err := repo.Log(&gitlib.LogOptions{From: from})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	result := make(map[plumbing.Hash]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, err := iter.Next()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		result[commit.Hash] = struct{}{}
	}
}

func (c *Client) sdkFetch(ctx context.Context) error {
	repo, err := c.openRepository()
	if err != nil {
		return err
	}
	defer repo.Close()
	remote, err := repo.Remote("origin")
	if err != nil {
		return err
	}
	if len(remote.Config().URLs) != 1 {
		return ErrInvalidRemote
	}
	options, err := c.transportOptions(remote.Config().URLs[0])
	if err != nil {
		return err
	}
	err = repo.FetchContext(ctx, &gitlib.FetchOptions{
		RemoteName: "origin", Tags: gitlib.NoTags, Prune: true,
		ClientOptions: options,
		RefSpecs:      []config.RefSpec{config.RefSpec("+refs/heads/" + c.branch + ":refs/remotes/origin/" + c.branch)},
	})
	if errors.Is(err, gitlib.NoErrAlreadyUpToDate) {
		return nil
	}
	if err != nil {
		return classifyRemoteFailure(err.Error())
	}
	return nil
}

func (c *Client) sdkPullFastForward(ctx context.Context) error {
	repo, err := c.openRepository()
	if err != nil {
		return err
	}
	defer repo.Close()
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}
	status, err := worktree.Status()
	if err != nil {
		return err
	}
	if !status.IsClean() {
		return errors.New("cannot pull with local changes")
	}
	head, err := repo.Head()
	if err != nil {
		return err
	}
	remote, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", c.branch), true)
	if err != nil {
		return err
	}
	if head.Hash() == remote.Hash() {
		return nil
	}
	ancestors, err := reachableCommits(ctx, repo, remote.Hash())
	if err != nil {
		return err
	}
	if _, ok := ancestors[head.Hash()]; !ok {
		return errors.New("non-fast-forward pull")
	}
	return worktree.Reset(&gitlib.ResetOptions{Commit: remote.Hash(), Mode: gitlib.HardReset})
}

func (c *Client) sdkPush(ctx context.Context) error {
	repo, err := c.openRepository()
	if err != nil {
		return err
	}
	defer repo.Close()
	remote, err := repo.Remote("origin")
	if err != nil {
		return err
	}
	if len(remote.Config().URLs) != 1 {
		return ErrInvalidRemote
	}
	options, err := c.transportOptions(remote.Config().URLs[0])
	if err != nil {
		return err
	}
	err = repo.PushContext(ctx, &gitlib.PushOptions{RemoteName: "origin", ClientOptions: options, RefSpecs: []config.RefSpec{
		config.RefSpec("refs/heads/" + c.branch + ":refs/heads/" + c.branch),
	}})
	if errors.Is(err, gitlib.NoErrAlreadyUpToDate) {
		return nil
	}
	if err != nil {
		return classifyRemoteFailure(err.Error())
	}
	return nil
}
