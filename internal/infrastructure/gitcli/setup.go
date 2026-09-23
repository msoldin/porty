package gitcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
)

const (
	repositoryDirectoryName  = "repository"
	sshDirectoryName         = "ssh"
	sshIdentityName          = "id"
	sshKnownHostsName        = "known_hosts"
	remoteInspectMaxOutput   = 1 << 20
	remoteInspectMaxBranches = 1000
	remoteInspectTimeout     = 30 * time.Second
	remoteFetchTimeout       = 30 * time.Second
	remoteFetchMaxOutput     = 1 << 20
	remoteTreeTimeout        = 30 * time.Second
	remoteCheckoutTimeout    = 30 * time.Second
	remoteCheckoutMaxOutput  = 1 << 20
	checkoutStagingDirectory = ".git/porty-checkout"
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

func (p *Provisioner) InspectSSHMaterial() domain.SSHMaterialStatus {
	if !safeSSHDirectory(p.sshKeyPath, p.knownHostsPath) {
		return domain.SSHMaterialStatus{}
	}
	keyPresent, keyUsable := sshMaterialFileStatus(p.sshKeyPath, 0o077)
	hostsPresent, hostsUsable := sshMaterialFileStatus(p.knownHostsPath, 0o022)
	return domain.SSHMaterialStatus{
		IdentityAvailable:   keyPresent,
		KnownHostsAvailable: hostsPresent,
		Usable:              keyUsable && hostsUsable,
	}
}

func (p *Provisioner) InspectRemote(ctx context.Context, remoteURL string, authentication domain.RepositoryAuthentication) (domain.RemoteInspection, error) {
	if err := ValidateRemoteURL(remoteURL); err != nil {
		return domain.RemoteInspection{}, application.ErrInvalidRequest
	}
	client, err := p.authenticatedClient("main", remoteURL, authentication)
	if err != nil {
		return domain.RemoteInspection{}, err
	}
	if err := client.validateAuthentication(); err != nil {
		return domain.RemoteInspection{}, application.ErrSSHMaterialUnavailable
	}
	remoteURL = safeRemoteURL(remoteURL)
	inspectCtx, cancel := context.WithTimeout(ctx, remoteInspectTimeout)
	defer cancel()
	result, runErr := runAt(
		inspectCtx,
		boundedRunner{runner: p.runner, maxOutput: remoteInspectMaxOutput},
		p.dataRoot,
		client.redact,
		client.env,
		"ls-remote", "--symref", remoteURL, "HEAD", "refs/heads/*",
	)
	if runErr != nil {
		return domain.RemoteInspection{}, classifyRemoteFailure(result.Output)
	}
	if result.Truncated || len(result.Output) > remoteInspectMaxOutput {
		return domain.RemoteInspection{}, application.ErrRemoteUnavailable
	}
	inspection, err := parseRemoteInspection(result.Output)
	if err != nil {
		return domain.RemoteInspection{}, application.ErrRemoteUnavailable
	}
	inspection.RemoteURL = safeRemoteURL(remoteURL)
	return inspection, nil
}

func (p *Provisioner) Provision(ctx context.Context, request application.RepositoryProvisionRequest) (application.GitRepository, domain.RepositoryConfiguration, error) {
	if err := ValidateBranch(request.Branch); err != nil || !validIdentity(request.Author) {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidRequest
	}
	switch request.Mode {
	case domain.RepositorySetupInit:
		return p.provisionInit(ctx, request)
	case domain.RepositorySetupRemote:
		return p.provisionRemote(ctx, request)
	case domain.RepositorySetupAdopt:
		return p.provisionAdopt(ctx, request)
	default:
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidRequest
	}
}

func (p *Provisioner) provisionRemote(ctx context.Context, request application.RepositoryProvisionRequest) (application.GitRepository, domain.RepositoryConfiguration, error) {
	remoteInspection, err := p.InspectRemote(ctx, request.RemoteURL, request.Authentication)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if !remoteInspection.Empty && !containsBranch(remoteInspection.Branches, request.Branch) {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidRequest
	}

	pathInspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	switch pathInspection.State {
	case domain.RepositoryPathWorktree:
		if pathInspection.Detached || pathInspection.Branch != request.Branch || pathInspection.Author != request.Author || pathInspection.ExistingRemote == nil || pathInspection.ExistingRemote.URL != remoteInspection.RemoteURL {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
		}
		return p.activeRemoteClient(ctx, request, remoteInspection.Empty)
	case domain.RepositoryPathOccupied:
		return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
	case domain.RepositoryPathInvalid:
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	case domain.RepositoryPathEmpty:
	default:
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}

	attempt, err := p.newRepositoryAttempt()
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			attempt.cleanup()
		}
		attempt.close()
	}()

	_, initErr := runAt(ctx, p.runner, p.dataRoot, nil, nil, "init", "-b", request.Branch, p.repositoryRoot)
	if err := attempt.captureInitializedRepository(); err != nil && initErr == nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if initErr != nil {
		return nil, domain.RepositoryConfiguration{}, initErr
	}
	if _, err := p.git(ctx, "remote", "add", "origin", remoteInspection.RemoteURL); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if !remoteInspection.Empty {
		client, err := p.authenticatedClient(request.Branch, remoteInspection.RemoteURL, request.Authentication)
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		if err := client.validateAuthentication(); err != nil {
			return nil, domain.RepositoryConfiguration{}, application.ErrSSHMaterialUnavailable
		}
		fetchCtx, cancel := context.WithTimeout(ctx, remoteFetchTimeout)
		result, err := runAtRepository(
			fetchCtx,
			boundedRunner{runner: p.runner, maxOutput: remoteFetchMaxOutput},
			p.repositoryRoot,
			client.redact,
			client.env,
			"fetch", "--no-tags", "origin", "refs/heads/"+request.Branch+":refs/remotes/origin/"+request.Branch,
		)
		cancel()
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, classifyRemoteFailure(result.Output)
		}
		_, err = p.remoteWorktreePaths(ctx, request.Branch)
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		if err := attempt.repositoryRoot.Mkdir(checkoutStagingDirectory, 0o700); err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		checkoutCtx, cancel := context.WithTimeout(ctx, remoteCheckoutTimeout)
		_, checkoutErr := runAtRepository(checkoutCtx,
			boundedRunner{runner: p.runner, maxOutput: remoteCheckoutMaxOutput}, p.repositoryRoot, nil,
			[]string{"GIT_WORK_TREE=" + filepath.Join(p.repositoryRoot, checkoutStagingDirectory)},
			"checkout", "-B", request.Branch, "--track", "origin/"+request.Branch)
		cancel()
		if checkoutErr != nil {
			return nil, domain.RepositoryConfiguration{}, checkoutErr
		}
		if err := attempt.publishWorktree(); err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
	}
	if err := p.writeIdentity(ctx, request.Author); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	client, configuration, err := p.activeRemoteClient(ctx, request, remoteInspection.Empty)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	cleanupNeeded = false
	return client, configuration, nil
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

func (p *Provisioner) activeRemoteClient(ctx context.Context, request application.RepositoryProvisionRequest, empty bool) (application.GitRepository, domain.RepositoryConfiguration, error) {
	inspection, err := p.InspectPath(ctx)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	canonicalURL := safeRemoteURL(request.RemoteURL)
	if inspection.State != domain.RepositoryPathWorktree || inspection.Detached || inspection.Branch != request.Branch || inspection.Author != request.Author || inspection.ExistingRemote == nil || inspection.ExistingRemote.URL != canonicalURL {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	hasCommit, err := p.hasCommit(ctx)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if hasCommit == empty {
		return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
	}
	if !empty {
		trackingRemote, err := p.localConfig(ctx, "branch."+request.Branch+".remote")
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		trackingMerge, err := p.localConfig(ctx, "branch."+request.Branch+".merge")
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		if trackingRemote != "origin" || trackingMerge != "refs/heads/"+request.Branch {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
		}
		headCommit, err := p.commitAtRef(ctx, "HEAD")
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		remoteCommit, err := p.commitAtRef(ctx, "refs/remotes/origin/"+request.Branch)
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		if headCommit != remoteCommit {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
		}
	}
	client, err := Adopt(ctx, p.runner, p.repositoryRoot, request.Branch)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	client, err = p.applyAuthentication(client, canonicalURL, request.Authentication)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	configuration := domain.RepositoryConfiguration{
		State:  domain.RepositorySetupReady,
		Root:   p.repositoryRoot,
		Branch: request.Branch,
		Author: request.Author,
		Remote: &domain.RepositoryRemoteSummary{
			Name:     "origin",
			URL:      canonicalURL,
			AuthType: request.Authentication.Type,
			Managed:  true,
		},
	}
	return client, configuration, nil
}

func (p *Provisioner) authenticatedClient(branch, remoteURL string, authentication domain.RepositoryAuthentication) (*Client, error) {
	client, err := New(p.runner, p.repositoryRoot, branch)
	if err != nil {
		return nil, application.ErrInvalidRequest
	}
	return p.applyAuthentication(client, remoteURL, authentication)
}

func (p *Provisioner) applyAuthentication(client *Client, remoteURL string, authentication domain.RepositoryAuthentication) (*Client, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return nil, application.ErrInvalidRequest
	}
	switch authentication.Type {
	case domain.RepositoryAuthNone:
		if authentication.Username != "" || authentication.Secret != "" || authentication.SSHKeyPath != "" || authentication.KnownHostsPath != "" {
			return nil, application.ErrInvalidRequest
		}
		return client, nil
	case domain.RepositoryAuthHTTPS:
		if parsed.Scheme != "https" || authentication.SSHKeyPath != "" || authentication.KnownHostsPath != "" {
			return nil, application.ErrInvalidRequest
		}
		authenticated, err := client.WithHTTPSCredentials(p.executable, authentication.Username, authentication.Secret)
		if err != nil {
			return nil, application.ErrInvalidRequest
		}
		return authenticated, nil
	case domain.RepositoryAuthSSH:
		if parsed.Scheme != "ssh" || authentication.Username != "" || authentication.Secret != "" {
			return nil, application.ErrInvalidRequest
		}
		if filepath.Clean(authentication.SSHKeyPath) != p.sshKeyPath || filepath.Clean(authentication.KnownHostsPath) != p.knownHostsPath {
			return nil, application.ErrSSHMaterialUnavailable
		}
		authenticated, err := client.WithSSHCredentials(p.executable, p.sshKeyPath, p.knownHostsPath)
		if err != nil {
			return nil, application.ErrSSHMaterialUnavailable
		}
		return authenticated, nil
	default:
		return nil, application.ErrInvalidRequest
	}
}

func (p *Provisioner) ConfigureRemote(ctx context.Context, request application.RepositoryRemoteProvisionRequest, configuration domain.RepositoryConfiguration) (application.GitRepository, domain.RepositoryConfiguration, error) {
	if configuration.State != domain.RepositorySetupReady || configuration.Root != p.repositoryRoot || configuration.Branch != request.Branch || ValidateRemoteURL(request.RemoteURL) != nil || ValidateBranch(request.Branch) != nil {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidRequest
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil || inspection.State != domain.RepositoryPathWorktree || inspection.Detached || inspection.Branch != request.Branch {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	if inspection.ExistingRemote != nil && !request.ReplaceExisting &&
		(configuration.Remote == nil || !configuration.Remote.Managed || configuration.Remote.Name != "origin" || configuration.Remote.URL != inspection.ExistingRemote.URL) {
		return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryRemoteConflict
	}
	remotes, err := p.git(ctx, "remote")
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	for _, remote := range strings.Fields(remotes.Output) {
		if remote == "porty-candidate" {
			return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryRemoteConflict
		}
	}
	remoteInspection, err := p.InspectRemote(ctx, request.RemoteURL, request.Authentication)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if _, err := p.git(ctx, "remote", "add", "porty-candidate", remoteInspection.RemoteURL); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	candidatePresent := true
	defer func() {
		if candidatePresent {
			_, _ = p.git(context.Background(), "remote", "remove", "porty-candidate")
		}
	}()
	selectedAdvertised := false
	if !remoteInspection.Empty {
		for _, branch := range remoteInspection.Branches {
			selectedAdvertised = selectedAdvertised || branch == request.Branch
		}
	}
	if selectedAdvertised {
		client, err := p.authenticatedClient(request.Branch, remoteInspection.RemoteURL, request.Authentication)
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		if err := client.validateAuthentication(); err != nil {
			return nil, domain.RepositoryConfiguration{}, application.ErrSSHMaterialUnavailable
		}
		fetchCtx, cancel := context.WithTimeout(ctx, remoteFetchTimeout)
		result, fetchErr := runAtRepository(fetchCtx, boundedRunner{runner: p.runner, maxOutput: remoteFetchMaxOutput}, p.repositoryRoot, client.redact, client.env, "fetch", "--no-tags", "porty-candidate", "refs/heads/"+request.Branch+":refs/remotes/porty-candidate/"+request.Branch)
		cancel()
		if fetchErr != nil {
			return nil, domain.RepositoryConfiguration{}, classifyRemoteFailure(result.Output)
		}
		hasCommit, err := p.hasCommit(ctx)
		if err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
		candidateRef := "refs/remotes/porty-candidate/" + request.Branch
		if hasCommit {
			result, err := p.git(ctx, "merge-base", "HEAD", candidateRef)
			if err != nil {
				if result.ExitCode != 1 {
					return nil, domain.RepositoryConfiguration{}, err
				}
				return nil, domain.RepositoryConfiguration{}, application.ErrUnrelatedHistory
			}
			if strings.TrimSpace(result.Output) == "" {
				return nil, domain.RepositoryConfiguration{}, application.ErrUnrelatedHistory
			}
		} else {
			status, err := p.git(ctx, "status", "--porcelain=v1", "-z")
			if err != nil {
				return nil, domain.RepositoryConfiguration{}, err
			}
			if status.Output != "" {
				return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryPathNotEmpty
			}
			checkoutCtx, cancel := context.WithTimeout(ctx, remoteCheckoutTimeout)
			_, err = runAtRepository(checkoutCtx, boundedRunner{runner: p.runner, maxOutput: remoteCheckoutMaxOutput}, p.repositoryRoot, nil, nil, "checkout", "-B", request.Branch, "--track", "porty-candidate/"+request.Branch)
			cancel()
			if err != nil {
				return nil, domain.RepositoryConfiguration{}, err
			}
		}
	}
	if _, err = p.git(ctx, "remote", "remove", "porty-candidate"); err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	candidatePresent = false
	if inspection.ExistingRemote == nil {
		_, err = p.git(ctx, "remote", "add", "origin", remoteInspection.RemoteURL)
	} else {
		_, err = p.git(ctx, "remote", "set-url", "origin", remoteInspection.RemoteURL)
	}
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	if selectedAdvertised {
		_, err = p.git(ctx, "config", "--local", "branch."+request.Branch+".remote", "origin")
		if err == nil {
			_, err = p.git(ctx, "config", "--local", "branch."+request.Branch+".merge", "refs/heads/"+request.Branch)
		}
	}
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	client, err := p.authenticatedClient(request.Branch, remoteInspection.RemoteURL, request.Authentication)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, err
	}
	updated := configuration
	updated.Remote = &domain.RepositoryRemoteSummary{Name: "origin", URL: remoteInspection.RemoteURL, AuthType: request.Authentication.Type, Managed: true}
	return client, updated, nil
}

func (p *Provisioner) RemoveRemote(ctx context.Context, configuration domain.RepositoryConfiguration) (application.GitRepository, domain.RepositoryConfiguration, error) {
	if configuration.State != domain.RepositorySetupReady || configuration.Root != p.repositoryRoot || configuration.Remote == nil || !configuration.Remote.Managed || configuration.Remote.Name != "origin" {
		return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryRemoteUnavailable
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil || inspection.State != domain.RepositoryPathWorktree {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	if inspection.ExistingRemote != nil && inspection.ExistingRemote.URL != configuration.Remote.URL {
		return nil, domain.RepositoryConfiguration{}, application.ErrRepositoryRemoteConflict
	}
	if inspection.ExistingRemote != nil {
		if _, err := p.git(ctx, "remote", "remove", "origin"); err != nil {
			return nil, domain.RepositoryConfiguration{}, err
		}
	}
	client, err := Adopt(ctx, p.runner, p.repositoryRoot, configuration.Branch)
	if err != nil {
		return nil, domain.RepositoryConfiguration{}, application.ErrInvalidWorktree
	}
	updated := configuration
	updated.Remote = nil
	return client, updated, nil
}

func (p *Provisioner) Open(ctx context.Context, configuration domain.RepositoryConfiguration, authentication domain.RepositoryAuthentication) (application.GitRepository, error) {
	if configuration.State != domain.RepositorySetupReady || configuration.Root != p.repositoryRoot || ValidateBranch(configuration.Branch) != nil {
		return nil, application.ErrInvalidWorktree
	}
	inspection, err := p.InspectPath(ctx)
	if err != nil || inspection.State != domain.RepositoryPathWorktree || inspection.Detached || inspection.Branch != configuration.Branch || inspection.Author != configuration.Author {
		return nil, application.ErrInvalidWorktree
	}
	remoteURL := ""
	if configuration.Remote != nil {
		if !configuration.Remote.Managed || configuration.Remote.Name != "origin" || inspection.ExistingRemote == nil || inspection.ExistingRemote.URL != configuration.Remote.URL || configuration.Remote.AuthType != authentication.Type {
			return nil, application.ErrInvalidWorktree
		}
		remoteURL = configuration.Remote.URL
	} else {
		authentication = domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}
	}
	client, err := Adopt(ctx, p.runner, p.repositoryRoot, configuration.Branch)
	if err != nil {
		return nil, application.ErrInvalidWorktree
	}
	client, err = p.applyAuthentication(client, remoteURL, authentication)
	if err != nil {
		return nil, err
	}
	return client, nil
}

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
		return nil, application.ErrInvalidWorktree
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
		return nil, nil, application.ErrInvalidWorktree
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
		return application.ErrInvalidWorktree
	}
	if a.repositoryInfo != nil && !os.SameFile(a.repositoryInfo, repositoryInfo) {
		return application.ErrInvalidWorktree
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
		return application.ErrInvalidWorktree
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

type boundedRunner struct {
	runner    Runner
	maxOutput int
}

func (r boundedRunner) Run(ctx context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	request.MaxOutput = r.maxOutput
	return r.runner.Run(ctx, request)
}

func parseRemoteInspection(output string) (domain.RemoteInspection, error) {
	inspection := domain.RemoteInspection{Branches: []string{}}
	if output == "" {
		inspection.Empty = true
		inspection.Suggested = "main"
		return inspection, nil
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	branches := make(map[string]struct{}, len(lines))
	symbolicHead := ""
	symbolicHeadSeen := false
	for _, line := range lines {
		if line == "" || strings.Count(line, "\t") != 1 {
			return domain.RemoteInspection{}, application.ErrRemoteUnavailable
		}
		value, reference, _ := strings.Cut(line, "\t")
		if strings.HasPrefix(value, "ref: ") {
			if symbolicHeadSeen || reference != "HEAD" {
				return domain.RemoteInspection{}, application.ErrRemoteUnavailable
			}
			branch, ok := branchFromRef(strings.TrimPrefix(value, "ref: "))
			if !ok {
				return domain.RemoteInspection{}, application.ErrRemoteUnavailable
			}
			symbolicHeadSeen = true
			symbolicHead = branch
			continue
		}
		if !validObjectID(value) {
			return domain.RemoteInspection{}, application.ErrRemoteUnavailable
		}
		if reference == "HEAD" {
			continue
		}
		branch, ok := branchFromRef(reference)
		if !ok {
			return domain.RemoteInspection{}, application.ErrRemoteUnavailable
		}
		branches[branch] = struct{}{}
		if len(branches) > remoteInspectMaxBranches {
			return domain.RemoteInspection{}, application.ErrRemoteUnavailable
		}
	}
	for branch := range branches {
		inspection.Branches = append(inspection.Branches, branch)
	}
	sort.Strings(inspection.Branches)
	if len(inspection.Branches) == 0 {
		return domain.RemoteInspection{}, application.ErrRemoteUnavailable
	}
	if _, advertised := branches[symbolicHead]; advertised {
		inspection.DefaultBranch = symbolicHead
	} else if len(inspection.Branches) == 1 {
		inspection.DefaultBranch = inspection.Branches[0]
	}
	inspection.Suggested = inspection.DefaultBranch
	return inspection, nil
}

func branchFromRef(reference string) (string, bool) {
	const prefix = "refs/heads/"
	if !strings.HasPrefix(reference, prefix) {
		return "", false
	}
	branch := strings.TrimPrefix(reference, prefix)
	return branch, ValidateBranch(branch) == nil
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func containsBranch(branches []string, branch string) bool {
	index := sort.SearchStrings(branches, branch)
	return index < len(branches) && branches[index] == branch
}

func classifyRemoteFailure(output string) error {
	lower := strings.ToLower(output)
	for _, marker := range []string{
		"authentication failed",
		"could not read username",
		"permission denied",
		"access denied",
		"returned error: 401",
		"returned error: 403",
		"http 401",
		"http 403",
	} {
		if strings.Contains(lower, marker) {
			return application.ErrRemoteAuthenticationFailed
		}
	}
	return application.ErrRemoteUnavailable
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

func (p *Provisioner) commitAtRef(ctx context.Context, reference string) (string, error) {
	result, err := p.git(ctx, "rev-parse", "--verify", reference+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(result.Output)
	if !validObjectID(commit) {
		return "", application.ErrInvalidWorktree
	}
	return commit, nil
}

func (p *Provisioner) remoteWorktreePaths(ctx context.Context, branch string) ([]string, error) {
	treeCtx, cancel := context.WithTimeout(ctx, remoteTreeTimeout)
	defer cancel()
	result, err := runAtRepository(
		treeCtx,
		boundedRunner{runner: p.runner, maxOutput: remoteInspectMaxOutput},
		p.repositoryRoot,
		nil,
		nil,
		"ls-tree", "-r", "-z", "--name-only", "refs/remotes/origin/"+branch,
	)
	if err != nil {
		return nil, err
	}
	if result.Truncated {
		return nil, application.ErrInvalidWorktree
	}
	return parseRemoteWorktreePaths(result.Output)
}

func parseRemoteWorktreePaths(output string) ([]string, error) {
	if output == "" {
		return []string{}, nil
	}
	if !strings.HasSuffix(output, "\x00") {
		return nil, application.ErrInvalidWorktree
	}
	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	paths := make([]string, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, name := range records {
		if !fs.ValidPath(name) || strings.Contains(name, "\\") || containsGitMetadataComponent(name) {
			return nil, application.ErrInvalidWorktree
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, application.ErrInvalidWorktree
		}
		seen[name] = struct{}{}
		paths = append(paths, name)
	}
	return paths, nil
}

func containsGitMetadataComponent(name string) bool {
	for _, component := range strings.Split(name, "/") {
		if strings.EqualFold(component, ".git") {
			return true
		}
	}
	return false
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
