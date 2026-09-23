package git

import (
	"fmt"
	"net/url"
	"os"

	gitclient "github.com/go-git/go-git/v6/plumbing/client"
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
)

func (c *Client) transportOptions(remoteURL string) ([]gitclient.Option, error) {
	if c.secret != "" {
		return []gitclient.Option{gitclient.WithHTTPAuth(&githttp.BasicAuth{
			Username: c.username, Password: c.secret,
		})}, nil
	}
	if c.sshKeyPath == "" {
		return nil, nil
	}
	if err := c.validateAuthentication(); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return nil, ErrInvalidRemote
	}
	username := "git"
	if parsed.User != nil && parsed.User.Username() != "" {
		username = parsed.User.Username()
	}
	key, err := os.ReadFile(c.sshKeyPath)
	if err != nil {
		return nil, fmt.Errorf("SSH material unavailable: %w", err)
	}
	auth, err := gitssh.NewPublicKeys(username, key, "")
	if err != nil {
		return nil, fmt.Errorf("SSH material unavailable: %w", err)
	}
	callback, err := gitssh.NewKnownHostsCallback(c.knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("SSH material unavailable: %w", err)
	}
	auth.HostKeyCallback = callback
	return []gitclient.Option{gitclient.WithSSHAuth(auth)}, nil
}
