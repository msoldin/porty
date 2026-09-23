package git

import (
	"context"
	"errors"
	"fmt"
	portyrepo "github.com/msoldin/porty/internal/repository"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	portyprocess "github.com/msoldin/porty/internal/process"
)

var (
	ErrInvalidRemote    = errors.New("invalid Git remote")
	ErrInvalidBranch    = errors.New("invalid Git branch")
	ErrInvalidCommit    = errors.New("invalid commit request")
	ErrUnsafeRepository = errors.New("unsafe Git repository configuration")
	branchCharacters    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

type Runner interface {
	Run(context.Context, portyprocess.Request) (portyprocess.Result, error)
}

type Status = portyrepo.GitStatus

type Commit = portyrepo.GitCommit

type Client struct {
	runner         Runner
	repo           string
	branch         string
	redact         []string
	env            []string
	sshKeyPath     string
	knownHostsPath string
}

const (
	maxUntrackedFileBytes = 1 << 20
	maxDiffBytes          = 4 << 20
)

func New(runner Runner, repository, branch string) (*Client, error) {
	if runner == nil || !filepath.IsAbs(repository) {
		return nil, errors.New("absolute repository path and runner are required")
	}
	if err := ValidateBranch(branch); err != nil {
		return nil, err
	}
	return &Client{runner: runner, repo: filepath.Clean(repository), branch: branch}, nil
}

func Clone(ctx context.Context, runner Runner, remote, target, branch string) error {
	if err := ValidateRemoteURL(remote); err != nil {
		return err
	}
	if err := ValidateBranch(branch); err != nil {
		return err
	}
	parent, name, err := repositoryTarget(target)
	if err != nil {
		return err
	}
	_, err = runAt(ctx, runner, parent, nil, nil, "clone", "--no-tags", "--single-branch", "--branch", branch, "--", remote, name)
	return err
}

func Init(ctx context.Context, runner Runner, target, branch string) error {
	if err := ValidateBranch(branch); err != nil {
		return err
	}
	parent, name, err := repositoryTarget(target)
	if err != nil {
		return err
	}
	_, err = runAt(ctx, runner, parent, nil, nil, "init", "-b", branch, name)
	return err
}

func InitExisting(ctx context.Context, runner Runner, target, branch string) error {
	if err := ValidateBranch(branch); err != nil {
		return err
	}
	if !filepath.IsAbs(target) {
		return errors.New("repository target must be absolute")
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeRepository
	}
	_, err = runAt(ctx, runner, filepath.Clean(target), nil, nil, "init", "-b", branch)
	return err
}

func (c *Client) CloneInto(ctx context.Context, remote string) error {
	if err := ValidateRemoteURL(remote); err != nil {
		return err
	}
	entries, err := os.ReadDir(c.repo)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("repository directory must be empty for clone")
	}
	_, err = runAt(ctx, c.runner, c.repo, c.redact, c.env, "clone", "--no-tags", "--single-branch", "--branch", c.branch, "--", remote, ".")
	return err
}

func (c *Client) SetOrigin(ctx context.Context, remote string) error {
	if err := ValidateRemoteURL(remote); err != nil {
		return err
	}
	_, err := c.run(ctx, "remote", "add", "origin", remote)
	return err
}

func Adopt(ctx context.Context, runner Runner, repository, branch string) (*Client, error) {
	client, err := New(runner, repository, branch)
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

func (c *Client) WithSecrets(values ...string) *Client {
	copy := *c
	copy.redact = append([]string(nil), values...)
	return &copy
}

func (c *Client) WithHTTPSCredentials(helper, username, secret string) (*Client, error) {
	if !filepath.IsAbs(helper) || strings.IndexFunc(helper, unicode.IsControl) >= 0 || username == "" || secret == "" || strings.IndexFunc(username, unicode.IsControl) >= 0 || strings.ContainsRune(secret, 0) {
		return nil, errors.New("invalid HTTPS credentials")
	}
	copy := *c
	copy.redact = append(append([]string(nil), c.redact...), secret, username)
	copy.env = append(append([]string(nil), c.env...),
		"PORTY_GIT_ASKPASS=1",
		"GIT_ASKPASS="+filepath.Clean(helper),
		"GIT_ASKPASS_REQUIRE=force",
		"PORTY_GIT_USERNAME="+username,
		"PORTY_GIT_PASSWORD="+secret,
	)
	return &copy, nil
}

func (c *Client) WithSSHCredentials(helper, keyPath, knownHostsPath string) (*Client, error) {
	if !filepath.IsAbs(helper) || !filepath.IsAbs(keyPath) || !filepath.IsAbs(knownHostsPath) || strings.IndexFunc(helper+keyPath+knownHostsPath, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid SSH credentials")
	}
	if err := validateSSHMaterial(keyPath, knownHostsPath); err != nil {
		return nil, err
	}
	copy := *c
	copy.sshKeyPath = filepath.Clean(keyPath)
	copy.knownHostsPath = filepath.Clean(knownHostsPath)
	copy.env = append(append([]string(nil), c.env...),
		"GIT_SSH="+filepath.Clean(helper), "GIT_SSH_VARIANT=ssh", "PORTY_GIT_SSH=1",
		"PORTY_GIT_SSH_KEY="+copy.sshKeyPath, "PORTY_GIT_KNOWN_HOSTS="+copy.knownHostsPath,
	)
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
	result, err := c.run(ctx, "config", "--local", "--list", "-z")
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

func unsafeConfigKey(key string) bool {
	if strings.HasPrefix(key, "alias.") || strings.HasPrefix(key, "filter.") || strings.HasPrefix(key, "credential.") || strings.HasPrefix(key, "submodule.") || strings.HasPrefix(key, "url.") || strings.HasPrefix(key, "http.") {
		return true
	}
	if strings.HasPrefix(key, "diff.") && (strings.HasSuffix(key, ".command") || strings.HasSuffix(key, ".textconv") || strings.HasSuffix(key, ".cachetextconv")) {
		return true
	}
	switch key {
	case "core.fsmonitor", "core.sshcommand", "core.hookspath", "core.attributesfile", "core.excludesfile", "core.editor", "sequence.editor", "commit.gpgsign", "tag.gpgsign":
		return true
	}
	return (strings.HasPrefix(key, "protocol.") && strings.HasSuffix(key, ".allow")) ||
		(strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver")) ||
		(strings.HasPrefix(key, "gpg.") && strings.HasSuffix(key, ".program"))
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	configured, err := c.configured()
	if err != nil {
		return Status{}, err
	}
	status := Status{Configured: configured, Branch: c.branch}
	if !configured {
		return status, nil
	}
	result, err := c.run(ctx, "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	items := strings.Split(result.Output, "\x00")
	for index := 0; index < len(items); index++ {
		item := items[index]
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "## ") {
			parseBranchHeader(&status, strings.TrimPrefix(item, "## "))
			continue
		}
		if len(item) < 4 {
			continue
		}
		path := item[3:]
		if item[0] == 'R' || item[1] == 'R' {
			if index+1 < len(items) {
				path = items[index+1]
				index++
			}
		}
		status.Paths = append(status.Paths, path)
	}
	sort.Strings(status.Paths)
	status.Dirty = len(status.Paths) > 0
	return status, nil
}

func parseBranchHeader(status *Status, header string) {
	if branch, found := strings.CutPrefix(header, "No commits yet on "); found {
		status.Branch = strings.TrimSpace(branch)
	} else if branch, _, found := strings.Cut(header, "..."); found {
		status.Branch = strings.Fields(branch)[0]
	} else if fields := strings.Fields(header); len(fields) > 0 {
		status.Branch = fields[0]
	}
	if start := strings.Index(header, "["); start >= 0 {
		end := strings.Index(header[start:], "]")
		if end > 0 {
			for _, part := range strings.Split(header[start+1:start+end], ",") {
				fields := strings.Fields(strings.TrimSpace(part))
				if len(fields) != 2 {
					continue
				}
				value, _ := strconv.Atoi(fields[1])
				if fields[0] == "ahead" {
					status.Ahead = value
				} else if fields[0] == "behind" {
					status.Behind = value
				}
			}
		}
	}
}

func (c *Client) Diff(ctx context.Context, stack string) (string, error) {
	if !validStackPath(stack) {
		return "", ErrInvalidCommit
	}
	result, err := c.run(ctx, "diff", "--no-ext-diff", "--", stack)
	if err != nil {
		return result.Output, err
	}
	untracked, err := c.run(ctx, "ls-files", "--others", "--exclude-standard", "-z", "--", stack)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	output.WriteString(result.Output)
	if output.Len() > maxDiffBytes {
		return output.String()[:maxDiffBytes], nil
	}
	for _, name := range strings.Split(strings.TrimSuffix(untracked.Output, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		path := filepath.Join(c.repo, filepath.FromSlash(name))
		relative, relErr := filepath.Rel(filepath.Join(c.repo, stack), path)
		info, statErr := os.Lstat(path)
		if relErr != nil || !filepath.IsLocal(relative) || statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		mode := 100644
		if info.Mode().Perm()&0o111 != 0 {
			mode = 100755
		}
		fmt.Fprintf(&output, "diff --git a/%s b/%s\nnew file mode %06d\n--- /dev/null\n+++ b/%s\n", name, name, mode, name)
		if info.Size() > maxUntrackedFileBytes {
			fmt.Fprintf(&output, "Binary files /dev/null and b/%s differ\n", name)
			if output.Len() >= maxDiffBytes {
				break
			}
			continue
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		if strings.IndexByte(string(contents), 0) >= 0 {
			fmt.Fprintf(&output, "Binary files /dev/null and b/%s differ\n", name)
			continue
		}
		lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
		if len(contents) == 0 {
			lines = nil
		}
		fmt.Fprintf(&output, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, line := range lines {
			if output.Len()+len(line)+2 > maxDiffBytes {
				output.WriteString("... diff truncated ...\n")
				return output.String(), nil
			}
			output.WriteString("+")
			output.WriteString(line)
			output.WriteString("\n")
		}
	}
	return output.String(), nil
}

func (c *Client) Head(ctx context.Context) (string, error) {
	result, err := c.run(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(result.Output), err
}

func (c *Client) Commit(ctx context.Context, stack, message string) (string, error) {
	message = strings.TrimSpace(message)
	if !validStackPath(stack) || message == "" || len(message) > 4096 || strings.ContainsRune(message, 0) {
		return "", ErrInvalidCommit
	}
	if _, err := c.run(ctx, "add", "--", stack); err != nil {
		return "", err
	}
	if _, err := c.run(ctx, "commit", "--only", "-m", message, "--", stack); err != nil {
		return "", err
	}
	result, err := c.run(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(result.Output), err
}

func (c *Client) History(ctx context.Context, limit int) ([]Commit, error) {
	return c.HistoryPage(ctx, limit, 0)
}

func (c *Client) HistoryPage(ctx context.Context, limit, offset int) ([]Commit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	configured, err := c.configured()
	if err != nil {
		return nil, err
	}
	if !configured {
		return []Commit{}, nil
	}
	head, err := c.run(ctx, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if head.ExitCode == 1 {
			return []Commit{}, nil
		}
		return nil, err
	}
	result, err := c.run(ctx, "log", "--date=iso-strict", "--format=%H%x00%s%x00%an%x00%aI%x00", "--skip", strconv.Itoa(offset), "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(result.Output, "\x00\n"), "\x00")
	history := make([]Commit, 0, len(parts)/4)
	for index := 0; index+3 < len(parts); index += 4 {
		history = append(history, Commit{SHA: parts[index], Subject: parts[index+1], Author: parts[index+2], Time: strings.TrimSpace(parts[index+3])})
	}
	return history, nil
}

func (c *Client) Fetch(ctx context.Context) error {
	_, err := c.run(ctx, "fetch", "--no-tags", "--prune", "origin", c.branch)
	return err
}

func (c *Client) PullFastForward(ctx context.Context) error {
	_, err := c.run(ctx, "merge", "--ff-only", "origin/"+c.branch)
	return err
}

func (c *Client) Push(ctx context.Context) error {
	_, err := c.run(ctx, "push", "--", "origin", "HEAD:refs/heads/"+c.branch)
	return err
}

func (c *Client) run(ctx context.Context, arguments ...string) (portyprocess.Result, error) {
	if err := c.validateAuthentication(); err != nil {
		return portyprocess.Result{}, err
	}
	return runAt(ctx, c.runner, c.repo, c.redact, c.env, arguments...)
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

func runAt(ctx context.Context, runner Runner, directory string, redact, extraEnv []string, arguments ...string) (portyprocess.Result, error) {
	hardened := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "diff.external=", "-c", "commit.gpgSign=false"}, arguments...)
	environment := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0"}
	environment = append(environment, extraEnv...)
	return runner.Run(ctx, portyprocess.Request{
		Name: "git", Args: hardened, Dir: directory, Redact: redact,
		Env: environment, CleanEnv: true,
	})
}

func runAtRepository(ctx context.Context, runner Runner, repository string, redact, extraEnv []string, arguments ...string) (portyprocess.Result, error) {
	arguments = append([]string{"-C", filepath.Clean(repository)}, arguments...)
	return runAt(ctx, runner, filepath.Dir(filepath.Clean(repository)), redact, extraEnv, arguments...)
}

func repositoryTarget(target string) (string, string, error) {
	if !filepath.IsAbs(target) {
		return "", "", errors.New("repository target must be absolute")
	}
	target = filepath.Clean(target)
	name := filepath.Base(target)
	if name == "." || name == string(filepath.Separator) || strings.HasPrefix(name, ".") {
		return "", "", errors.New("invalid repository target")
	}
	return filepath.Dir(target), name, nil
}

func validStackPath(value string) bool {
	return value != "" && value != "." && filepath.IsLocal(value) && filepath.Base(value) == value && !strings.HasPrefix(value, ".")
}
