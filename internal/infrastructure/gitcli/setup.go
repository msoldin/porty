package gitcli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
)

const (
	repositoryDirectoryName = "repository"
	sshDirectoryName        = "ssh"
	sshIdentityName         = "id"
	sshKnownHostsName       = "known_hosts"
)

type Provisioner struct {
	runner         Runner
	dataRoot       string
	repositoryRoot string
	executable     string
	sshKeyPath     string
	knownHostsPath string
}

func NewProvisioner(dataDirectory string, runner Runner, executable string) (*Provisioner, error) {
	if runner == nil || dataDirectory == "" || executable == "" {
		return nil, errors.New("data directory, process runner, and executable are required")
	}
	dataRoot, err := filepath.Abs(dataDirectory)
	if err != nil {
		return nil, fmt.Errorf("clean data directory: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("clean executable path: %w", err)
	}
	dataRoot = filepath.Clean(dataRoot)
	sshRoot := filepath.Join(dataRoot, sshDirectoryName)
	return &Provisioner{
		runner:         runner,
		dataRoot:       dataRoot,
		repositoryRoot: filepath.Join(dataRoot, repositoryDirectoryName),
		executable:     filepath.Clean(executable),
		sshKeyPath:     filepath.Join(sshRoot, sshIdentityName),
		knownHostsPath: filepath.Join(sshRoot, sshKnownHostsName),
	}, nil
}

func (p *Provisioner) InspectPath(ctx context.Context) (domain.RepositoryPathInspection, error) {
	if !p.fixedRootIsSafe() {
		return invalidPath("repository root escapes the data directory"), nil
	}
	dataInfo, err := os.Lstat(p.dataRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.RepositoryPathInspection{State: domain.RepositoryPathEmpty}, nil
		}
		return domain.RepositoryPathInspection{}, err
	}
	if !dataInfo.IsDir() || dataInfo.Mode()&os.ModeSymlink != 0 {
		return invalidPath("data directory is not a safe directory"), nil
	}

	rootInfo, err := os.Lstat(p.repositoryRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.RepositoryPathInspection{State: domain.RepositoryPathEmpty}, nil
		}
		return domain.RepositoryPathInspection{}, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return invalidPath("repository root is not a safe directory"), nil
	}

	gitPath := filepath.Join(p.repositoryRoot, ".git")
	gitInfo, err := os.Lstat(gitPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return domain.RepositoryPathInspection{}, err
		}
		entries, readErr := os.ReadDir(p.repositoryRoot)
		if readErr != nil {
			return domain.RepositoryPathInspection{}, readErr
		}
		if len(entries) == 0 {
			return domain.RepositoryPathInspection{State: domain.RepositoryPathEmpty}, nil
		}
		return domain.RepositoryPathInspection{State: domain.RepositoryPathOccupied}, nil
	}
	if !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 {
		return invalidPath("Git metadata is not a safe directory"), nil
	}

	if err := p.validateSafety(ctx); err != nil {
		if errors.Is(err, ErrUnsafeRepository) {
			return invalidPath("repository has unsafe Git configuration"), nil
		}
		return domain.RepositoryPathInspection{}, err
	}

	inspection := domain.RepositoryPathInspection{State: domain.RepositoryPathWorktree}
	branchResult, branchErr := p.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branchErr != nil {
		if branchResult.ExitCode == 1 {
			inspection.Detached = true
		} else {
			return domain.RepositoryPathInspection{}, branchErr
		}
	} else {
		inspection.Branch = strings.TrimSpace(branchResult.Output)
		if err := ValidateBranch(inspection.Branch); err != nil {
			return invalidPath("repository branch is unsafe"), nil
		}
	}

	name, err := p.localConfig(ctx, "user.name")
	if err != nil {
		return domain.RepositoryPathInspection{}, err
	}
	email, err := p.localConfig(ctx, "user.email")
	if err != nil {
		return domain.RepositoryPathInspection{}, err
	}
	inspection.Author = domain.GitIdentity{Name: name, Email: email}

	remotes, err := p.localConfigValues(ctx, "remote.origin.url")
	if err != nil {
		return domain.RepositoryPathInspection{}, err
	}
	if len(remotes) > 1 {
		return invalidPath("repository has multiple origin URLs"), nil
	}
	if len(remotes) == 1 {
		remote := remotes[0]
		if err := ValidateRemoteURL(remote); err != nil {
			return invalidPath("repository origin is unsafe"), nil
		}
		inspection.ExistingRemote = &domain.RepositoryRemoteSummary{
			Name:     "origin",
			URL:      safeRemoteURL(remote),
			AuthType: domain.RepositoryAuthNone,
			Managed:  false,
		}
	}
	return inspection, nil
}

func (p *Provisioner) Provision(ctx context.Context, request application.RepositoryProvisionRequest) (application.GitRepository, domain.RepositoryConfiguration, error) {
	if err := ValidateBranch(request.Branch); err != nil || !validIdentity(request.Author) {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidRequest
	}
	switch request.Mode {
	case domain.RepositorySetupInit:
		return p.provisionInit(ctx, request)
	case domain.RepositorySetupAdopt:
		return p.provisionAdopt(ctx, request)
	default:
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidRequest
	}
}

func (p *Provisioner) provisionInit(ctx context.Context, request application.RepositoryProvisionRequest) (application.GitRepository, domain.RepositoryConfiguration, error) {
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	switch inspection.State {
	case domain.RepositoryPathWorktree:
		if inspection.Detached || inspection.Branch != request.Branch || inspection.Author != request.Author || inspection.ExistingRemote != nil {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
		}
		return p.activeClient(ctx, request.Branch, request.Author, false, true)
	case domain.RepositoryPathOccupied:
		return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
	case domain.RepositoryPathInvalid:
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	case domain.RepositoryPathEmpty:
	default:
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}

	if _, err := runAt(ctx, p.runner, p.dataRoot, nil, nil, "init", "-b", request.Branch, p.repositoryRoot); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if err := p.writeIdentity(ctx, request.Author); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	return p.activeClient(ctx, request.Branch, request.Author, false, true)
}

func (p *Provisioner) provisionAdopt(ctx context.Context, request application.RepositoryProvisionRequest) (application.GitRepository, domain.RepositoryConfiguration, error) {
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if inspection.State != domain.RepositoryPathWorktree {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	if inspection.Detached {
		return nil, domain.RepositoryConfiguration{}, application.ErrDetachedHead
	}
	if inspection.Branch != request.Branch {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	if err := p.writeIdentity(ctx, request.Author); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	return p.activeClient(ctx, request.Branch, request.Author, request.ManageExistingRemote, false)
}

func (p *Provisioner) activeClient(ctx context.Context, branch string, author domain.GitIdentity, manageExistingRemote, requireUnborn bool) (application.GitRepository, domain.RepositoryConfiguration, error) {
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if inspection.State != domain.RepositoryPathWorktree || inspection.Detached || inspection.Branch != branch || inspection.Author != author {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	if requireUnborn {
		if inspection.ExistingRemote != nil {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
		}
		hasCommit, err := p.hasCommit(ctx)
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		if hasCommit {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
		}
	}
	client, err := Adopt(ctx, p.runner, p.repositoryRoot, branch)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	var remote *domain.RepositoryRemoteSummary
	if manageExistingRemote && inspection.ExistingRemote != nil {
		copy := *inspection.ExistingRemote
		copy.Managed = true
		remote = &copy
	}
	configuration := domain.RepositoryConfiguration{
		State:  domain.RepositorySetupReady,
		Root:   p.repositoryRoot,
		Branch: branch,
		Author: author,
		Remote: remote,
	}
	return client, configuration, nil
}

func (p *Provisioner) fixedRootIsSafe() bool {
	if !filepath.IsAbs(p.dataRoot) || !filepath.IsAbs(p.repositoryRoot) {
		return false
	}
	relative, err := filepath.Rel(p.dataRoot, p.repositoryRoot)
	return err == nil && relative == repositoryDirectoryName && filepath.Join(p.dataRoot, repositoryDirectoryName) == p.repositoryRoot
}

func (p *Provisioner) validateSafety(ctx context.Context) error {
	result, err := p.git(ctx, "config", "--local", "--list", "-z")
	if err != nil {
		return err
	}
	for _, item := range strings.Split(result.Output, "\x00") {
		key := strings.ToLower(strings.SplitN(item, "\n", 2)[0])
		if unsafeConfigKey(key) {
			return fmt.Errorf("%w: %s", ErrUnsafeRepository, key)
		}
	}
	return nil
}

func (p *Provisioner) writeIdentity(ctx context.Context, author domain.GitIdentity) error {
	if _, err := p.git(ctx, "config", "--local", "user.name", author.Name); err != nil {
		return err
	}
	_, err := p.git(ctx, "config", "--local", "user.email", author.Email)
	return err
}

func (p *Provisioner) localConfig(ctx context.Context, key string) (string, error) {
	result, err := p.git(ctx, "config", "--local", "--get", key)
	if err != nil {
		if result.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(result.Output), nil
}

func (p *Provisioner) localConfigValues(ctx context.Context, key string) ([]string, error) {
	result, err := p.git(ctx, "config", "--local", "--get-all", key)
	if err != nil {
		if result.ExitCode == 1 {
			return nil, nil
		}
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(result.Output, "\n"), "\n"), nil
}

func (p *Provisioner) hasCommit(ctx context.Context) (bool, error) {
	result, err := p.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		if result.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (p *Provisioner) git(ctx context.Context, arguments ...string) (portyprocess.Result, error) {
	return runAtRepository(ctx, p.runner, p.repositoryRoot, nil, nil, arguments...)
}

func invalidPath(reason string) domain.RepositoryPathInspection {
	return domain.RepositoryPathInspection{State: domain.RepositoryPathInvalid, Reason: reason}
}

func validIdentity(identity domain.GitIdentity) bool {
	if strings.TrimSpace(identity.Name) == "" || strings.TrimSpace(identity.Email) == "" {
		return false
	}
	return strings.IndexFunc(identity.Name, unicode.IsControl) < 0 && strings.IndexFunc(identity.Email, unicode.IsControl) < 0
}

func safeRemoteURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parsed.User = nil
	return parsed.String()
}
