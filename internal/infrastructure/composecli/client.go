package composecli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/domain"
	portyfs "github.com/msoldin/porty/internal/infrastructure/filesystem"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
)

type Request = domain.ComposeRequest

type Runner interface {
	Run(context.Context, portyprocess.Request) (portyprocess.Result, error)
}

type Client struct {
	runner  Runner
	timeout time.Duration
}

const maxCommandOutput = 1 << 20

func New(runner Runner, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Client{runner: runner, timeout: timeout}
}

func (c *Client) Validate(ctx context.Context, request Request) error {
	_, err := c.run(ctx, request, "config", "--quiet")
	return err
}

func (c *Client) Digest(ctx context.Context, request Request) (string, error) {
	result, err := c.run(ctx, request, "config", "--format", "json")
	if err != nil {
		return "", err
	}
	environment, err := portyfs.SerializeEnvironment(request.Environment)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(result.Output))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(environment)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func (c *Client) Status(ctx context.Context, request Request) (string, error) {
	result, err := c.run(ctx, request, "ps", "--format", "json")
	return result.Output, err
}

func (c *Client) Start(ctx context.Context, request Request) error {
	_, err := c.run(ctx, request, "up", "-d")
	return err
}

func (c *Client) Stop(ctx context.Context, request Request) error {
	_, err := c.run(ctx, request, "stop")
	return err
}

func (c *Client) Restart(ctx context.Context, request Request) error {
	_, err := c.run(ctx, request, "restart")
	return err
}

func (c *Client) Deploy(ctx context.Context, request Request, recreate bool) error {
	arguments := []string{"up", "-d"}
	if recreate {
		arguments = append(arguments, "--force-recreate")
	}
	arguments = append(arguments, "--remove-orphans")
	_, err := c.run(ctx, request, arguments...)
	return err
}

func (c *Client) Pull(ctx context.Context, request Request) error {
	_, err := c.run(ctx, request, "pull")
	return err
}

func (c *Client) Down(ctx context.Context, request Request) error {
	_, err := c.run(ctx, request, "down", "--remove-orphans")
	return err
}

func (c *Client) Logs(ctx context.Context, request Request, tail int) (string, error) {
	if tail <= 0 || tail > 10000 {
		tail = 500
	}
	result, err := c.run(ctx, request, "logs", "--no-color", "--tail", strconv.Itoa(tail))
	return result.Output, err
}

func (c *Client) run(parent context.Context, request Request, action ...string) (portyprocess.Result, error) {
	if c.runner == nil || !filepath.IsAbs(request.StackDir) || request.ProjectName == "" {
		return portyprocess.Result{}, errors.New("invalid Compose request")
	}
	contents, err := portyfs.SerializeEnvironment(request.Environment)
	if err != nil {
		return portyprocess.Result{}, err
	}
	file, err := os.CreateTemp("", "porty-compose-env-*")
	if err != nil {
		return portyprocess.Result{}, err
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return portyprocess.Result{}, err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return portyprocess.Result{}, err
	}
	if err := file.Close(); err != nil {
		return portyprocess.Result{}, err
	}
	arguments := []string{"compose", "--project-name", request.ProjectName, "--env-file", path, "-f", "docker-compose.yml"}
	arguments = append(arguments, action...)
	secrets := make([]string, 0, len(request.Environment))
	for _, value := range request.Environment {
		secrets = append(secrets, value)
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	result, err := c.runner.Run(ctx, portyprocess.Request{
		Name: "docker", Args: arguments, Dir: request.StackDir, Redact: secrets,
		MaxOutput: maxCommandOutput, CleanEnv: true, Env: []string{"DOCKER_CLI_HINTS=false"},
	})
	if err != nil {
		detail := result.Output
		for _, secret := range secrets {
			if secret != "" {
				detail = strings.ReplaceAll(detail, secret, "[REDACTED]")
			}
		}
		detail = strings.TrimSpace(detail)
		if len(detail) > maxCommandOutput {
			detail = detail[:maxCommandOutput]
		}
		if detail != "" {
			return result, fmt.Errorf("docker compose %s: %s: %w", action[0], detail, err)
		}
		return result, fmt.Errorf("docker compose %s: %w", action[0], err)
	}
	return result, nil
}
