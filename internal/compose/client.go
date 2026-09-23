package compose

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/compose/v5/pkg/api"
)

type Client struct {
	service api.Compose
	timeout time.Duration
}

const maxCommandOutput = 1 << 20

func New(service api.Compose, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Client{service: service, timeout: timeout}
}

func (c *Client) Validate(ctx context.Context, request Request) error {
	_, err := Load(ctx, request)
	return err
}

func (c *Client) Digest(ctx context.Context, request Request) (string, error) {
	project, err := Load(ctx, request)
	if err != nil {
		return "", err
	}
	return Digest(project, request.Environment)
}

func (c *Client) Status(parent context.Context, request Request) ([]api.ContainerSummary, error) {
	project, err := Load(parent, request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	status, err := c.service.Ps(ctx, project.Name, api.PsOptions{Project: project, All: true})
	if err != nil {
		return nil, c.safeError(err, request)
	}
	return status, nil
}

func (c *Client) Start(parent context.Context, request Request) error {
	return c.up(parent, request, false, false)
}

func (c *Client) Deploy(parent context.Context, request Request, recreate bool) error {
	return c.up(parent, request, recreate, true)
}

func (c *Client) up(parent context.Context, request Request, recreate, removeOrphans bool) error {
	project, err := Load(parent, request)
	if err != nil {
		return err
	}
	mode := api.RecreateDiverged
	if recreate {
		mode = api.RecreateForce
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	err = c.service.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{Build: &api.BuildOptions{}, Recreate: mode, RemoveOrphans: removeOrphans},
		Start:  api.StartOptions{},
	})
	return c.safeError(err, request)
}

func (c *Client) Stop(parent context.Context, request Request) error {
	project, err := Load(parent, request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	return c.safeError(c.service.Stop(ctx, project.Name, api.StopOptions{Project: project}), request)
}

func (c *Client) Restart(parent context.Context, request Request) error {
	project, err := Load(parent, request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	return c.safeError(c.service.Restart(ctx, project.Name, api.RestartOptions{Project: project}), request)
}

func (c *Client) Pull(parent context.Context, request Request) error {
	project, err := Load(parent, request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	return c.safeError(c.service.Pull(ctx, project, api.PullOptions{}), request)
}

func (c *Client) Down(parent context.Context, request Request) error {
	project, err := Load(parent, request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	return c.safeError(c.service.Down(ctx, project.Name, api.DownOptions{Project: project, RemoveOrphans: true}), request)
}

func (c *Client) Logs(parent context.Context, request Request, tail int) (string, error) {
	project, err := Load(parent, request)
	if err != nil {
		return "", err
	}
	if tail <= 0 || tail > 10000 {
		tail = 500
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	consumer := &boundedLogs{}
	err = c.service.Logs(ctx, project.Name, consumer, api.LogOptions{Project: project, Tail: strconv.Itoa(tail)})
	if err != nil {
		return boundedComposeOutput(consumer.String(), request.Environment), c.safeError(err, request)
	}
	return boundedComposeOutput(consumer.String(), request.Environment), nil
}

func (c *Client) safeError(err error, request Request) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	message := redactComposeOutput(err.Error(), request.Environment)
	if len(message) > maxCommandOutput {
		message = message[:maxCommandOutput]
	}
	return fmt.Errorf("Compose: %s", message)
}

func redactComposeOutput(message string, environment map[string]string) string {
	for _, secret := range environment {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return message
}

func boundedComposeOutput(message string, environment map[string]string) string {
	message = redactComposeOutput(message, environment)
	if len(message) > maxCommandOutput {
		message = message[:maxCommandOutput]
	}
	return message
}

type boundedLogs struct{ data strings.Builder }

func (l *boundedLogs) append(container, message string) {
	if l.data.Len() >= maxCommandOutput {
		return
	}
	line := container + "  | " + message + "\n"
	if len(line) > maxCommandOutput-l.data.Len() {
		line = line[:maxCommandOutput-l.data.Len()]
	}
	l.data.WriteString(line)
}

func (l *boundedLogs) Log(container, message string)    { l.append(container, message) }
func (l *boundedLogs) Err(container, message string)    { l.append(container, message) }
func (l *boundedLogs) Status(container, message string) { l.append(container, message) }
func (l *boundedLogs) String() string                   { return l.data.String() }
