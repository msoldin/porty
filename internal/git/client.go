package git

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	portyrepo "github.com/msoldin/porty/internal/repository"
)

var (
	ErrInvalidRemote    = errors.New("invalid Git remote")
	ErrInvalidBranch    = errors.New("invalid Git branch")
	ErrInvalidCommit    = errors.New("invalid commit request")
	ErrUnsafeRepository = errors.New("unsafe Git repository configuration")
	branchCharacters    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

type Client struct {
	repo           string
	branch         string
	username       string
	secret         string
	sshKeyPath     string
	knownHostsPath string
}

const (
	maxUntrackedFileBytes = 1 << 20
	maxDiffBytes          = 4 << 20
)

func New(repository, branch string) (*Client, error) {
	if !filepath.IsAbs(repository) {
		return nil, errors.New("absolute repository path is required")
	}
	if err := ValidateBranch(branch); err != nil {
		return nil, err
	}
	return &Client{repo: filepath.Clean(repository), branch: branch}, nil
}

func Adopt(ctx context.Context, repository, branch string) (*Client, error) {
	client, err := New(repository, branch)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(filepath.Join(repository, ".git"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafeRepository
	}
	if err := client.ValidateSafety(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) WithHTTPSCredentials(username, secret string) (*Client, error) {
	if username == "" || secret == "" || strings.IndexFunc(username, unicode.IsControl) >= 0 || strings.ContainsRune(secret, 0) {
		return nil, errors.New("invalid HTTPS credentials")
	}
	copy := *c
	copy.username = username
	copy.secret = secret
	return &copy, nil
}

func (c *Client) WithSSHCredentials(keyPath, knownHostsPath string) (*Client, error) {
	if !filepath.IsAbs(keyPath) || !filepath.IsAbs(knownHostsPath) || strings.IndexFunc(keyPath+knownHostsPath, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid SSH credentials")
	}
	if err := validateSSHMaterial(keyPath, knownHostsPath); err != nil {
		return nil, err
	}
	copy := *c
	copy.sshKeyPath = filepath.Clean(keyPath)
	copy.knownHostsPath = filepath.Clean(knownHostsPath)
	return &copy, nil
}

func validateSSHMaterial(keyPath, knownHostsPath string) error {
	if !safeSSHDirectory(keyPath, knownHostsPath) {
		return errors.New("SSH material unavailable")
	}
	_, keyUsable := sshMaterialFileStatus(keyPath, 0o077)
	_, hostsUsable := sshMaterialFileStatus(knownHostsPath, 0o022)
	if !keyUsable || !hostsUsable {
		return errors.New("SSH material unavailable")
	}
	return nil
}

func safeSSHDirectory(keyPath, knownHostsPath string) bool {
	if filepath.Dir(keyPath) != filepath.Dir(knownHostsPath) {
		return false
	}
	info, err := os.Lstat(filepath.Dir(keyPath))
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func sshMaterialFileStatus(path string, forbiddenMode os.FileMode) (bool, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, false
	}
	return true, info.Mode().Perm()&forbiddenMode == 0
}

func ValidateRemoteURL(value string) error {
	if value == "" || strings.HasPrefix(value, "-") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ErrInvalidRemote
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "ssh") || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.RawQuery != "" {
		return ErrInvalidRemote
	}
	if parsed.User != nil {
		if _, present := parsed.User.Password(); present || strings.HasPrefix(parsed.User.Username(), "-") {
			return ErrInvalidRemote
		}
	}
	return nil
}

func ValidateBranch(value string) error {
	if !branchCharacters.MatchString(value) || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "refs/") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.ContainsAny(value, `~^:?*[\`) || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.Contains(value, "//") || strings.Contains(value, "/.") {
		return ErrInvalidBranch
	}
	return nil
}

func (c *Client) ValidateSafety(ctx context.Context) error {
	repo, err := c.openRepository()
	if err != nil {
		return err
	}
	defer repo.Close()
	return validateRepositoryConfig(repo)
}

func unsafeConfigKey(key string) bool {
	if strings.HasPrefix(key, "alias.") || strings.HasPrefix(key, "filter.") || strings.HasPrefix(key, "credential.") || strings.HasPrefix(key, "submodule.") || strings.HasPrefix(key, "url.") || strings.HasPrefix(key, "http.") || strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") {
		return true
	}
	if strings.HasPrefix(key, "diff.") && (strings.HasSuffix(key, ".command") || strings.HasSuffix(key, ".textconv") || strings.HasSuffix(key, ".cachetextconv")) {
		return true
	}
	switch key {
	case "core.fsmonitor", "core.sshcommand", "core.hookspath", "core.attributesfile", "core.excludesfile", "core.editor", "core.worktree", "core.alternaterefscommand", "sequence.editor", "commit.gpgsign", "tag.gpgsign":
		return true
	}
	return (strings.HasPrefix(key, "protocol.") && strings.HasSuffix(key, ".allow")) ||
		(strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver")) ||
		(strings.HasPrefix(key, "gpg.") && strings.HasSuffix(key, ".program"))
}

func (c *Client) Status(ctx context.Context) (portyrepo.GitStatus, error) {
	return c.sdkStatus(ctx)
}

func (c *Client) Diff(ctx context.Context, stack string) (string, error) {
	return c.sdkDiff(ctx, stack)
}

func (c *Client) Head(ctx context.Context) (string, error) {
	return c.sdkHead(ctx)
}

func (c *Client) Commit(ctx context.Context, stack, message string) (string, error) {
	return c.sdkCommit(ctx, stack, message)
}

func (c *Client) History(ctx context.Context, limit int) ([]portyrepo.GitCommit, error) {
	return c.HistoryPage(ctx, limit, 0)
}

func (c *Client) HistoryPage(ctx context.Context, limit, offset int) ([]portyrepo.GitCommit, error) {
	return c.sdkHistoryPage(ctx, limit, offset)
}

func (c *Client) Fetch(ctx context.Context) error {
	return c.sdkFetch(ctx)
}

func (c *Client) PullFastForward(ctx context.Context) error {
	return c.sdkPullFastForward(ctx)
}

func (c *Client) Push(ctx context.Context) error {
	return c.sdkPush(ctx)
}

func (c *Client) validateAuthentication() error {
	if c.sshKeyPath == "" {
		return nil
	}
	return validateSSHMaterial(c.sshKeyPath, c.knownHostsPath)
}

func (c *Client) configured() (bool, error) {
	_, err := os.Lstat(filepath.Join(c.repo, ".git"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func validStackPath(value string) bool {
	return value != "" && value != "." && filepath.IsLocal(value) && filepath.Base(value) == value && !strings.HasPrefix(value, ".")
}
