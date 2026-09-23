package git

import (
	"errors"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	portyrepo "github.com/msoldin/porty/internal/repository"
)

const (
	repositoryDirectoryName  = "repository"
	sshDirectoryName         = "ssh"
	sshIdentityName          = "id"
	sshKnownHostsName        = "known_hosts"
	remoteInspectMaxBranches = 1000
	remoteInspectMaxOutput   = 1 << 20
	remoteInspectTimeout     = 30 * time.Second
	remoteFetchTimeout       = 30 * time.Second
	checkoutStagingDirectory = ".git/porty-checkout"
)

type Provisioner struct {
	dataRoot       string
	repositoryRoot string
	sshKeyPath     string
	knownHostsPath string
}

func NewProvisioner(dataDirectory string) (*Provisioner, error) {
	if dataDirectory == "" {
		return nil, errors.New("data directory is required")
	}
	dataRoot, err := filepath.Abs(dataDirectory)
	if err != nil {
		return nil, err
	}
	dataRoot = filepath.Clean(dataRoot)
	sshRoot := filepath.Join(dataRoot, sshDirectoryName)
	return &Provisioner{
		dataRoot:       dataRoot,
		repositoryRoot: filepath.Join(dataRoot, repositoryDirectoryName),
		sshKeyPath:     filepath.Join(sshRoot, sshIdentityName),
		knownHostsPath: filepath.Join(sshRoot, sshKnownHostsName),
	}, nil
}

func (p *Provisioner) InspectSSHMaterial() portyrepo.SSHMaterialStatus {
	if !safeSSHDirectory(p.sshKeyPath, p.knownHostsPath) {
		return portyrepo.SSHMaterialStatus{}
	}
	keyPresent, keyUsable := sshMaterialFileStatus(p.sshKeyPath, 0o077)
	hostsPresent, hostsUsable := sshMaterialFileStatus(p.knownHostsPath, 0o022)
	return portyrepo.SSHMaterialStatus{
		IdentityAvailable:   keyPresent,
		KnownHostsAvailable: hostsPresent,
		Usable:              keyUsable && hostsUsable,
	}
}

func (p *Provisioner) authenticatedClient(branch, remoteURL string, authentication portyrepo.RepositoryAuthentication) (*Client, error) {
	client, err := New(p.repositoryRoot, branch)
	if err != nil {
		return nil, portyrepo.ErrInvalidRequest
	}
	return p.applyAuthentication(client, remoteURL, authentication)
}

func (p *Provisioner) applyAuthentication(client *Client, remoteURL string, authentication portyrepo.RepositoryAuthentication) (*Client, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return nil, portyrepo.ErrInvalidRequest
	}
	switch authentication.Type {
	case portyrepo.RepositoryAuthNone:
		if authentication.Username != "" || authentication.Secret != "" || authentication.SSHKeyPath != "" || authentication.KnownHostsPath != "" {
			return nil, portyrepo.ErrInvalidRequest
		}
		return client, nil
	case portyrepo.RepositoryAuthHTTPS:
		if parsed.Scheme != "https" || authentication.SSHKeyPath != "" || authentication.KnownHostsPath != "" {
			return nil, portyrepo.ErrInvalidRequest
		}
		authenticated, err := client.WithHTTPSCredentials(authentication.Username, authentication.Secret)
		if err != nil {
			return nil, portyrepo.ErrInvalidRequest
		}
		return authenticated, nil
	case portyrepo.RepositoryAuthSSH:
		if parsed.Scheme != "ssh" || authentication.Username != "" || authentication.Secret != "" {
			return nil, portyrepo.ErrInvalidRequest
		}
		if filepath.Clean(authentication.SSHKeyPath) != p.sshKeyPath || filepath.Clean(authentication.KnownHostsPath) != p.knownHostsPath {
			return nil, portyrepo.ErrSSHMaterialUnavailable
		}
		authenticated, err := client.WithSSHCredentials(p.sshKeyPath, p.knownHostsPath)
		if err != nil {
			return nil, portyrepo.ErrSSHMaterialUnavailable
		}
		return authenticated, nil
	default:
		return nil, portyrepo.ErrInvalidRequest
	}
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
			return portyrepo.ErrRemoteAuthenticationFailed
		}
	}
	return portyrepo.ErrRemoteUnavailable
}

func (p *Provisioner) fixedRootIsSafe() bool {
	if !filepath.IsAbs(p.dataRoot) || !filepath.IsAbs(p.repositoryRoot) {
		return false
	}
	relative, err := filepath.Rel(p.dataRoot, p.repositoryRoot)
	return err == nil && relative == repositoryDirectoryName && filepath.Join(p.dataRoot, repositoryDirectoryName) == p.repositoryRoot
}

func containsGitMetadataComponent(name string) bool {
	for _, component := range strings.Split(name, "/") {
		if strings.EqualFold(component, ".git") {
			return true
		}
	}
	return false
}

func invalidPath(reason string) portyrepo.RepositoryPathInspection {
	return portyrepo.RepositoryPathInspection{State: portyrepo.RepositoryPathInvalid, Reason: reason}
}

func validIdentity(identity portyrepo.GitIdentity) bool {
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
