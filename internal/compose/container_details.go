package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

var ErrContainerInspectTooLarge = errors.New("container inspect exceeds size limit")

type ContainerLogSnapshot struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

const maxContainerInspect = 2 << 20
const maxContainerLogInput = 8 << 20

func (c *Client) ContainerInspect(parent context.Context, request Request, id string) (json.RawMessage, error) {
	if c.containers == nil {
		return nil, errors.New("Docker client unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	result, err := c.containers.ContainerInspect(ctx, id, client.ContainerInspectOptions{Size: false})
	if err != nil {
		return nil, c.safeError(err, request)
	}
	if len(result.Raw) > maxContainerInspect {
		return nil, ErrContainerInspectTooLarge
	}
	if !json.Valid(result.Raw) {
		return nil, errors.New("Docker returned invalid container inspect JSON")
	}
	return result.Raw, nil
}

func (c *Client) ContainerLogs(parent context.Context, request Request, id string) (ContainerLogSnapshot, error) {
	if c.containers == nil {
		return ContainerLogSnapshot{}, errors.New("Docker client unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	inspect, err := c.containers.ContainerInspect(ctx, id, client.ContainerInspectOptions{Size: false})
	if err != nil {
		return ContainerLogSnapshot{}, c.safeError(err, request)
	}
	stream, err := c.containers.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "500"})
	if err != nil {
		return ContainerLogSnapshot{}, c.safeError(err, request)
	}
	defer stream.Close()
	input := &io.LimitedReader{R: stream, N: maxContainerLogInput + 1}
	output := &boundedContainerOutput{}
	if inspect.Container.Config != nil && inspect.Container.Config.Tty {
		_, err = io.Copy(output, input)
	} else {
		_, err = stdcopy.StdCopy(output, output, input)
	}
	truncated := output.truncated || input.N == 0
	if err != nil && !truncated {
		return ContainerLogSnapshot{}, c.safeError(err, request)
	}
	redacted := redactComposeOutput(output.String(), request.Environment)
	if input.N == 0 && len(redacted) <= maxCommandOutput {
		return ContainerLogSnapshot{Output: "Docker log stream exceeded read limit; output omitted.", Truncated: true}, nil
	}
	if len(redacted) > maxCommandOutput {
		redacted = redacted[:maxCommandOutput]
		truncated = true
	}
	return ContainerLogSnapshot{Output: redacted, Truncated: truncated}, nil
}

type boundedContainerOutput struct {
	data      bytes.Buffer
	truncated bool
}

func (w *boundedContainerOutput) Write(p []byte) (int, error) {
	available := maxContainerLogInput - w.data.Len()
	if available <= 0 {
		if len(p) > 0 {
			w.truncated = true
		}
		return len(p), nil
	}
	if len(p) > available {
		_, _ = w.data.Write(p[:available])
		w.truncated = true
		return len(p), nil
	}
	_, _ = w.data.Write(p)
	return len(p), nil
}

func (w *boundedContainerOutput) String() string { return w.data.String() }
