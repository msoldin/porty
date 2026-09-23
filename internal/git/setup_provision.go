package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6/osfs"
	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
	portyrepo "github.com/msoldin/porty/internal/repository"
)

func newReservedRepository(attempt *repositoryAttempt, branch string) (*gitlib.Repository, error) {
	storer := filesystem.NewStorage(osfs.New(filepath.Join(attempt.repositoryRoot.Name(), ".git")), cache.NewObjectLRUDefault())
	return gitlib.Init(storer, gitlib.WithWorkTree(osfs.New(attempt.repositoryRoot.Name())),
		gitlib.WithDefaultBranch(plumbing.NewBranchReferenceName(branch)))
}

func validateRemoteTree(repo *gitlib.Repository, hash plumbing.Hash) error {
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return portyrepo.ErrInvalidWorktree
	}
	tree, err := commit.Tree()
	if err != nil {
		return portyrepo.ErrInvalidWorktree
	}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil || !fs.ValidPath(name) || strings.Contains(name, "\\") || containsGitMetadataComponent(name) {
			return portyrepo.ErrInvalidWorktree
		}
		switch entry.Mode {
		case filemode.Dir, filemode.Regular, filemode.Executable:
		case filemode.Symlink:
			file, err := tree.File(name)
			if err != nil || file.Size > 4096 {
				return portyrepo.ErrInvalidWorktree
			}
			target, err := file.Contents()
			if err != nil || target == "" || path.IsAbs(target) || strings.Contains(target, "\\") {
				return portyrepo.ErrInvalidWorktree
			}
			resolved := path.Clean(path.Join(path.Dir(name), target))
			if !fs.ValidPath(resolved) || resolved == ".." || strings.HasPrefix(resolved, "../") || containsGitMetadataComponent(resolved) {
				return portyrepo.ErrInvalidWorktree
			}
		default:
			return portyrepo.ErrInvalidWorktree
		}
	}
}

func (p *Provisioner) Provision(ctx context.Context, request portyrepo.RepositoryProvisionRequest) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	if ValidateBranch(request.Branch) != nil || !validIdentity(request.Author) {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidRequest
	}
	switch request.Mode {
	case portyrepo.RepositorySetupInit:
		return p.sdkProvisionInit(ctx, request)
	case portyrepo.RepositorySetupRemote:
		return p.sdkProvisionRemote(ctx, request)
	case portyrepo.RepositorySetupAdopt:
		return p.sdkProvisionAdopt(ctx, request)
	default:
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidRequest
	}
}

func (p *Provisioner) sdkProvisionInit(ctx context.Context, request portyrepo.RepositoryProvisionRequest) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if inspection.State == portyrepo.RepositoryPathWorktree {
		if inspection.Detached || inspection.Branch != request.Branch || inspection.Author != request.Author || inspection.ExistingRemote != nil {
			return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
		}
		repo, err := gitlib.PlainOpen(p.repositoryRoot)
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		defer repo.Close()
		if _, err := repo.Head(); err == nil {
			return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
		} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		return p.sdkActiveClient(request.Branch, request.Author, nil)
	}
	if inspection.State != portyrepo.RepositoryPathEmpty {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
	}
	attempt, err := p.newRepositoryAttempt()
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	success := false
	defer func() {
		if !success {
			attempt.cleanup()
		}
		attempt.close()
	}()
	repo, err := newReservedRepository(attempt, request.Branch)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	defer repo.Close()
	if err := setIdentity(repo, request.Author); err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	client, cfg, err := p.sdkActiveClient(request.Branch, request.Author, nil)
	if err == nil {
		success = true
	}
	return client, cfg, err
}

func (p *Provisioner) sdkProvisionAdopt(ctx context.Context, request portyrepo.RepositoryProvisionRequest) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if inspection.State != portyrepo.RepositoryPathWorktree {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidWorktree
	}
	if inspection.Detached {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrDetachedHead
	}
	if inspection.Branch != request.Branch {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidWorktree
	}
	repo, err := gitlib.PlainOpen(p.repositoryRoot)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	defer repo.Close()
	if err := setIdentity(repo, request.Author); err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	var remote *portyrepo.RepositoryRemoteSummary
	if request.ManageExistingRemote && inspection.ExistingRemote != nil {
		copy := *inspection.ExistingRemote
		copy.Managed = true
		remote = &copy
	}
	return p.sdkActiveClient(request.Branch, request.Author, remote)
}

func (p *Provisioner) sdkProvisionRemote(ctx context.Context, request portyrepo.RepositoryProvisionRequest) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	remoteInspection, err := p.InspectRemote(ctx, request.RemoteURL, request.Authentication)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if !remoteInspection.Empty && !containsBranch(remoteInspection.Branches, request.Branch) {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidRequest
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	remoteSummary := &portyrepo.RepositoryRemoteSummary{
		Name: "origin", URL: remoteInspection.RemoteURL, AuthType: request.Authentication.Type, Managed: true,
	}
	if inspection.State == portyrepo.RepositoryPathWorktree {
		if inspection.Detached || inspection.Branch != request.Branch || inspection.Author != request.Author || inspection.ExistingRemote == nil || inspection.ExistingRemote.URL != remoteSummary.URL {
			return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
		}
		return p.sdkActiveRemoteClient(request, remoteInspection.Empty, remoteSummary)
	}
	if inspection.State != portyrepo.RepositoryPathEmpty {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
	}
	attempt, err := p.newRepositoryAttempt()
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	success := false
	defer func() {
		if !success {
			attempt.cleanup()
		}
		attempt.close()
	}()
	repo, err := newReservedRepository(attempt, request.Branch)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	defer repo.Close()
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteSummary.URL}}); err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if !remoteInspection.Empty {
		client, err := p.authenticatedClient(request.Branch, remoteSummary.URL, request.Authentication)
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := fetchRemoteBranch(ctx, repo, client, "origin", request.Branch); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", request.Branch), true)
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := validateRemoteTree(repo, remoteRef.Hash()); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := attempt.repositoryRoot.Mkdir(checkoutStagingDirectory, 0o700); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		staged, err := gitlib.Open(repo.Storer, osfs.New(filepath.Join(p.repositoryRoot, checkoutStagingDirectory)))
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		worktree, err := staged.Worktree()
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := worktree.Checkout(&gitlib.CheckoutOptions{Branch: remoteRef.Name()}); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(request.Branch), remoteRef.Hash())); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(request.Branch))); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := attempt.publishWorktree(); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, fmt.Errorf("publish Git worktree: %w", err)
		}
		if err := setTracking(repo, request.Branch, "origin"); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
	}
	if err := setIdentity(repo, request.Author); err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	client, cfg, err := p.sdkActiveRemoteClient(request, remoteInspection.Empty, remoteSummary)
	if err != nil {
		err = fmt.Errorf("activate remote repository: %w", err)
	}
	if err == nil {
		success = true
	}
	return client, cfg, err
}

func setIdentity(repo *gitlib.Repository, identity portyrepo.GitIdentity) error {
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	cfg.User.Name, cfg.User.Email = identity.Name, identity.Email
	return repo.SetConfig(cfg)
}

func setTracking(repo *gitlib.Repository, branch, remote string) error {
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	cfg.Branches[branch] = &config.Branch{Name: branch, Remote: remote, Merge: plumbing.NewBranchReferenceName(branch)}
	return repo.SetConfig(cfg)
}

func (p *Provisioner) sdkActiveClient(branch string, author portyrepo.GitIdentity, remote *portyrepo.RepositoryRemoteSummary) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	client, err := New(p.repositoryRoot, branch)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if _, err := Adopt(context.Background(), p.repositoryRoot, branch); err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, fmt.Errorf("%w: %v", portyrepo.ErrInvalidWorktree, err)
	}
	return client, portyrepo.RepositoryConfiguration{State: portyrepo.RepositorySetupReady,
		Root: p.repositoryRoot, Branch: branch, Author: author, Remote: remote}, nil
}

func (p *Provisioner) sdkActiveRemoteClient(request portyrepo.RepositoryProvisionRequest, empty bool, remote *portyrepo.RepositoryRemoteSummary) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	repo, err := gitlib.PlainOpen(p.repositoryRoot)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	defer repo.Close()
	head, err := repo.Head()
	if empty && !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
	}
	if !empty {
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
		}
		remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", request.Branch), true)
		if err != nil || remoteRef.Hash() != head.Hash() {
			return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
		}
	}
	client, cfg, err := p.sdkActiveClient(request.Branch, request.Author, remote)
	if err != nil {
		return nil, cfg, err
	}
	client, err = p.applyAuthentication(client.(*Client), remote.URL, request.Authentication)
	return client, cfg, err
}
