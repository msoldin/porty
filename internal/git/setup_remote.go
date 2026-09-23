package git

import (
	"context"
	"errors"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	portyrepo "github.com/msoldin/porty/internal/repository"
)

func fetchRemoteBranch(ctx context.Context, repo *gitlib.Repository, client *Client, remoteName, branch string) error {
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return err
	}
	options, err := client.transportOptions(remote.Config().URLs[0])
	if err != nil {
		return portyrepo.ErrSSHMaterialUnavailable
	}
	fetchCtx, cancel := context.WithTimeout(ctx, remoteFetchTimeout)
	defer cancel()
	err = repo.FetchContext(fetchCtx, &gitlib.FetchOptions{RemoteName: remoteName, Tags: gitlib.NoTags,
		ClientOptions: options, RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/" + branch + ":refs/remotes/" + remoteName + "/" + branch)}})
	if errors.Is(err, gitlib.NoErrAlreadyUpToDate) {
		return nil
	}
	if err != nil {
		return classifyRemoteFailure(err.Error())
	}
	return nil
}

func (p *Provisioner) ConfigureRemote(ctx context.Context, request portyrepo.RepositoryRemoteProvisionRequest, configuration portyrepo.RepositoryConfiguration) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	if configuration.State != portyrepo.RepositorySetupReady || configuration.Root != p.repositoryRoot || configuration.Branch != request.Branch || ValidateRemoteURL(request.RemoteURL) != nil || ValidateBranch(request.Branch) != nil {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidRequest
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil || inspection.State != portyrepo.RepositoryPathWorktree || inspection.Detached || inspection.Branch != request.Branch {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidWorktree
	}
	if inspection.ExistingRemote != nil && !request.ReplaceExisting &&
		(configuration.Remote == nil || !configuration.Remote.Managed || configuration.Remote.URL != inspection.ExistingRemote.URL) {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryRemoteConflict
	}
	remoteInspection, err := p.InspectRemote(ctx, request.RemoteURL, request.Authentication)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	repo, err := gitlib.PlainOpen(p.repositoryRoot)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	defer repo.Close()
	cfg, err := repo.Config()
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if _, present := cfg.Remotes["porty-candidate"]; present {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryRemoteConflict
	}
	selected := containsBranch(remoteInspection.Branches, request.Branch)
	if selected {
		_, err := repo.CreateRemote(&config.RemoteConfig{Name: "porty-candidate", URLs: []string{remoteInspection.RemoteURL}})
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		defer repo.DeleteRemote("porty-candidate")
		client, err := p.authenticatedClient(request.Branch, request.RemoteURL, request.Authentication)
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := fetchRemoteBranch(ctx, repo, client, "porty-candidate", request.Branch); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		candidate, err := repo.Reference(plumbing.NewRemoteReferenceName("porty-candidate", request.Branch), true)
		if err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		if err := validateRemoteTree(repo, candidate.Hash()); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
		head, err := repo.Head()
		if err == nil {
			ancestors, err := reachableCommits(ctx, repo, candidate.Hash())
			if err != nil {
				return nil, portyrepo.RepositoryConfiguration{}, err
			}
			if _, related := ancestors[head.Hash()]; !related {
				other, err := reachableCommits(ctx, repo, head.Hash())
				if err != nil {
					return nil, portyrepo.RepositoryConfiguration{}, err
				}
				related = false
				for hash := range other {
					if _, ok := ancestors[hash]; ok {
						related = true
						break
					}
				}
				if !related {
					return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrUnrelatedHistory
				}
			}
		} else if errors.Is(err, plumbing.ErrReferenceNotFound) {
			worktree, err := repo.Worktree()
			if err != nil {
				return nil, portyrepo.RepositoryConfiguration{}, err
			}
			status, err := worktree.Status()
			if err != nil {
				return nil, portyrepo.RepositoryConfiguration{}, err
			}
			if !status.IsClean() {
				return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryPathNotEmpty
			}
			if err := worktree.Checkout(&gitlib.CheckoutOptions{Branch: candidate.Name()}); err != nil {
				return nil, portyrepo.RepositoryConfiguration{}, err
			}
			if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(request.Branch), candidate.Hash())); err != nil {
				return nil, portyrepo.RepositoryConfiguration{}, err
			}
			if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(request.Branch))); err != nil {
				return nil, portyrepo.RepositoryConfiguration{}, err
			}
		} else {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
	}
	cfg, err = repo.Config()
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	cfg.Remotes["origin"] = &config.RemoteConfig{Name: "origin", URLs: []string{remoteInspection.RemoteURL}}
	if selected {
		cfg.Branches[request.Branch] = &config.Branch{Name: request.Branch, Remote: "origin", Merge: plumbing.NewBranchReferenceName(request.Branch)}
	}
	if err := repo.SetConfig(cfg); err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	if selected {
		candidate, err := repo.Reference(plumbing.NewRemoteReferenceName("porty-candidate", request.Branch), true)
		if err == nil {
			_ = repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", request.Branch), candidate.Hash()))
		}
	}
	client, err := p.authenticatedClient(request.Branch, request.RemoteURL, request.Authentication)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	updated := configuration
	updated.Remote = &portyrepo.RepositoryRemoteSummary{Name: "origin", URL: remoteInspection.RemoteURL, AuthType: request.Authentication.Type, Managed: true}
	return client, updated, nil
}

func (p *Provisioner) RemoveRemote(ctx context.Context, configuration portyrepo.RepositoryConfiguration) (portyrepo.GitRepository, portyrepo.RepositoryConfiguration, error) {
	if configuration.State != portyrepo.RepositorySetupReady || configuration.Root != p.repositoryRoot || configuration.Remote == nil || !configuration.Remote.Managed || configuration.Remote.Name != "origin" {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryRemoteUnavailable
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil || inspection.State != portyrepo.RepositoryPathWorktree {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrInvalidWorktree
	}
	if inspection.ExistingRemote != nil && inspection.ExistingRemote.URL != configuration.Remote.URL {
		return nil, portyrepo.RepositoryConfiguration{}, portyrepo.ErrRepositoryRemoteConflict
	}
	repo, err := gitlib.PlainOpen(p.repositoryRoot)
	if err != nil {
		return nil, portyrepo.RepositoryConfiguration{}, err
	}
	defer repo.Close()
	if inspection.ExistingRemote != nil {
		if err := repo.DeleteRemote("origin"); err != nil {
			return nil, portyrepo.RepositoryConfiguration{}, err
		}
	}
	updated := configuration
	updated.Remote = nil
	client, err := New(p.repositoryRoot, configuration.Branch)
	return client, updated, err
}

func (p *Provisioner) Open(ctx context.Context, configuration portyrepo.RepositoryConfiguration, authentication portyrepo.RepositoryAuthentication) (portyrepo.GitRepository, error) {
	if configuration.State != portyrepo.RepositorySetupReady || configuration.Root != p.repositoryRoot || ValidateBranch(configuration.Branch) != nil {
		return nil, portyrepo.ErrInvalidWorktree
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil || inspection.State != portyrepo.RepositoryPathWorktree || inspection.Detached || inspection.Branch != configuration.Branch || inspection.Author != configuration.Author {
		return nil, portyrepo.ErrInvalidWorktree
	}
	remoteURL := ""
	if configuration.Remote != nil {
		if !configuration.Remote.Managed || configuration.Remote.Name != "origin" || inspection.ExistingRemote == nil || inspection.ExistingRemote.URL != configuration.Remote.URL || configuration.Remote.AuthType != authentication.Type {
			return nil, portyrepo.ErrInvalidWorktree
		}
		remoteURL = configuration.Remote.URL
	} else {
		authentication = portyrepo.RepositoryAuthentication{Type: portyrepo.RepositoryAuthNone}
	}
	client, err := Adopt(ctx, p.repositoryRoot, configuration.Branch)
	if err != nil {
		return nil, portyrepo.ErrInvalidWorktree
	}
	return p.applyAuthentication(client, remoteURL, authentication)
}
