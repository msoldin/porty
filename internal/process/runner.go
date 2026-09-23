package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

type Request struct {
	Name      string
	Args      []string
	Dir       string
	Env       []string
	Stdin     []byte
	Redact    []string
	MaxOutput int
	CleanEnv  bool
}

type Result struct {
	Output    string
	ExitCode  int
	Truncated bool
}

type Runner struct{}

func NewRunner() *Runner { return &Runner{} }

func (r *Runner) Run(ctx context.Context, request Request) (Result, error) {
	if request.Name == "" {
		return Result{}, errors.New("process executable is required")
	}
	limit := request.MaxOutput
	if limit <= 0 {
		limit = 256 << 10
	}
	command := exec.CommandContext(ctx, request.Name, request.Args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.Dir = request.Dir
	if request.CleanEnv {
		command.Env = cleanEnvironment(request.Env)
	} else {
		command.Env = append(os.Environ(), request.Env...)
	}
	command.Stdin = bytes.NewReader(request.Stdin)
	capture := &limitedBuffer{limit: limit + 64<<10}
	command.Stdout = capture
	command.Stderr = capture
	err := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Result{}, ctxErr
	}
	output := capture.String()
	for _, secret := range request.Redact {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[REDACTED]")
		}
	}
	truncated := capture.truncated || len(output) > limit
	if len(output) > limit {
		output = output[:limit]
	}
	result := Result{Output: output, ExitCode: 0, Truncated: truncated}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.ExitCode = exitError.ExitCode()
		}
		return result, fmt.Errorf("run %s: %w", request.Name, err)
	}
	return result, nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(value)
	return original, nil
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func cleanEnvironment(extra []string) []string {
	result := make([]string, 0, len(extra)+5)
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, present := os.LookupEnv(key); present {
			result = append(result, key+"="+value)
		}
	}
	return append(result, extra...)
}
