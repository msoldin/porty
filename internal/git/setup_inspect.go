package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	gitclient "github.com/go-git/go-git/v6/plumbing/client"
	portyrepo "github.com/msoldin/porty/internal/repository"
)

type limitedGitTransport struct{ base http.RoundTripper }

func (t limitedGitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &limitedGitBody{ReadCloser: response.Body, remaining: remoteInspectMaxOutput}
	return response, nil
}

type limitedGitBody struct {
	io.ReadCloser
	remaining int
}

func (b *limitedGitBody) Read(data []byte) (int, error) {
	if b.remaining == 0 {
		return 0, errors.New("Git remote advertisement too large")
	}
	if len(data) > b.remaining {
		data = data[:b.remaining]
	}
	n, err := b.ReadCloser.Read(data)
	b.remaining -= n
	return n, err
}

func (p *Provisioner) InspectPath(_ context.Context) (portyrepo.RepositoryPathInspection, error) {
	if !p.fixedRootIsSafe() {
		return invalidPath("repository root escapes the data directory"), nil
	}
	for _, path := range []string{p.dataRoot, p.repositoryRoot} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return portyrepo.RepositoryPathInspection{State: portyrepo.RepositoryPathEmpty}, nil
		}
		if err != nil {
			return portyrepo.RepositoryPathInspection{}, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return invalidPath("repository path is not a safe directory"), nil
		}
	}
	gitPath := filepath.Join(p.repositoryRoot, ".git")
	info, err := os.Lstat(gitPath)
	if errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(p.repositoryRoot)
		if err != nil {
			return portyrepo.RepositoryPathInspection{}, err
		}
		state := portyrepo.RepositoryPathEmpty
		if len(entries) != 0 {
			state = portyrepo.RepositoryPathOccupied
		}
		return portyrepo.RepositoryPathInspection{State: state}, nil
	}
	if err != nil {
		return portyrepo.RepositoryPathInspection{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return invalidPath("Git metadata is not a safe directory"), nil
	}
	if err := validateMetadataFiles(gitPath); err != nil {
		return invalidPath("Git metadata contains unsafe files"), nil
	}
	repo, err := gitlib.PlainOpen(p.repositoryRoot)
	if err != nil {
		return invalidPath("Git metadata is invalid"), nil
	}
	defer repo.Close()
	if err := validateRepositoryConfig(repo); err != nil {
		return invalidPath("repository has unsafe Git configuration"), nil
	}
	cfg, err := repo.Config()
	if err != nil {
		return portyrepo.RepositoryPathInspection{}, err
	}
	result := portyrepo.RepositoryPathInspection{
		State:  portyrepo.RepositoryPathWorktree,
		Author: portyrepo.GitIdentity{Name: cfg.User.Name, Email: cfg.User.Email},
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return portyrepo.RepositoryPathInspection{}, err
	}
	if head.Type() == plumbing.SymbolicReference && head.Target().IsBranch() {
		result.Branch = head.Target().Short()
		if ValidateBranch(result.Branch) != nil {
			return invalidPath("repository branch is unsafe"), nil
		}
	} else {
		result.Detached = true
	}
	if remote, ok := cfg.Remotes["origin"]; ok {
		if len(remote.URLs) != 1 || ValidateRemoteURL(remote.URLs[0]) != nil {
			return invalidPath("repository origin is unsafe"), nil
		}
		result.ExistingRemote = &portyrepo.RepositoryRemoteSummary{
			Name: "origin", URL: safeRemoteURL(remote.URLs[0]), AuthType: portyrepo.RepositoryAuthNone,
		}
	}
	return result, nil
}

func validateMetadataFiles(gitPath string) error {
	for _, name := range []string{"config", "HEAD", "objects", "refs", "index", "packed-refs"} {
		info, err := os.Lstat(filepath.Join(gitPath, name))
		if errors.Is(err, os.ErrNotExist) && (name == "index" || name == "packed-refs") {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: metadata %s is a symlink", ErrUnsafeRepository, name)
		}
		if name == "objects" || name == "refs" {
			if !info.IsDir() {
				return fmt.Errorf("%w: metadata %s is not a directory", ErrUnsafeRepository, name)
			}
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: metadata %s is not regular", ErrUnsafeRepository, name)
		}
	}
	return nil
}

func validateRepositoryConfig(repo *gitlib.Repository) error {
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	if cfg.Core.IsBare {
		return ErrUnsafeRepository
	}
	if cfg.Raw == nil {
		return nil
	}
	for _, section := range cfg.Raw.Sections {
		for _, option := range section.Options {
			if unsafeConfigKey(strings.ToLower(section.Name + "." + option.Key)) {
				return fmt.Errorf("%w: %s.%s", ErrUnsafeRepository, section.Name, option.Key)
			}
		}
		for _, subsection := range section.Subsections {
			for _, option := range subsection.Options {
				if unsafeConfigKey(strings.ToLower(section.Name + "." + subsection.Name + "." + option.Key)) {
					return fmt.Errorf("%w: %s.%s.%s", ErrUnsafeRepository, section.Name, subsection.Name, option.Key)
				}
			}
		}
	}
	return nil
}

func (p *Provisioner) InspectRemote(ctx context.Context, remoteURL string, authentication portyrepo.RepositoryAuthentication) (portyrepo.RemoteInspection, error) {
	if ValidateRemoteURL(remoteURL) != nil {
		return portyrepo.RemoteInspection{}, portyrepo.ErrInvalidRequest
	}
	client, err := p.authenticatedClient("main", remoteURL, authentication)
	if err != nil {
		return portyrepo.RemoteInspection{}, err
	}
	options, err := client.transportOptions(remoteURL)
	if err != nil {
		return portyrepo.RemoteInspection{}, portyrepo.ErrSSHMaterialUnavailable
	}
	if strings.HasPrefix(remoteURL, "https://") {
		options = append(options, gitclient.WithHTTPClient(&http.Client{
			Transport: limitedGitTransport{base: http.DefaultTransport},
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if request.URL.Scheme != "https" || len(via) == 0 || request.URL.Hostname() != via[0].URL.Hostname() {
					return ErrInvalidRemote
				}
				return nil
			},
		}))
	}
	inspectCtx, cancel := context.WithTimeout(ctx, remoteInspectTimeout)
	defer cancel()
	remote := gitlib.NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{safeRemoteURL(remoteURL)}})
	refs, err := remote.ListContext(inspectCtx, &gitlib.ListOptions{ClientOptions: options})
	if err != nil {
		return portyrepo.RemoteInspection{}, classifyRemoteFailure(err.Error())
	}
	inspection := portyrepo.RemoteInspection{RemoteURL: safeRemoteURL(remoteURL), Branches: []string{}}
	branches := make(map[string]bool)
	var symbolicHead string
	for _, ref := range refs {
		if len(branches) >= remoteInspectMaxBranches {
			return portyrepo.RemoteInspection{}, portyrepo.ErrRemoteUnavailable
		}
		if ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference {
			symbolicHead = ref.Target().Short()
		}
		if ref.Name().IsBranch() {
			branch := ref.Name().Short()
			if ValidateBranch(branch) != nil {
				return portyrepo.RemoteInspection{}, portyrepo.ErrRemoteUnavailable
			}
			branches[branch] = true
		}
	}
	for branch := range branches {
		inspection.Branches = append(inspection.Branches, branch)
	}
	sort.Strings(inspection.Branches)
	inspection.Empty = len(inspection.Branches) == 0
	if inspection.Empty {
		inspection.Suggested = "main"
	} else if symbolicHead != "" && branches[symbolicHead] {
		inspection.DefaultBranch = symbolicHead
		inspection.Suggested = symbolicHead
	} else if len(inspection.Branches) == 1 {
		inspection.Suggested = inspection.Branches[0]
	}
	return inspection, nil
}
