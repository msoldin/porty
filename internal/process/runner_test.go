package process_test

import (
	"context"
	"errors"
	"testing"
	"time"

	portyprocess "github.com/msoldin/porty/internal/process"
)

func TestRunnerBoundsAndRedactsOutput(t *testing.T) {
	runner := portyprocess.NewRunner()
	result, err := runner.Run(context.Background(), portyprocess.Request{
		Name:      "sh",
		Args:      []string{"-c", `printf 'token=very-secret-value-and-more'`},
		Redact:    []string{"very-secret-value"},
		MaxOutput: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "token=[REDACTED]-and" || !result.Truncated {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunnerTerminatesCancelledProcess(t *testing.T) {
	runner := portyprocess.NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runner.Run(ctx, portyprocess.Request{Name: "sh", Args: []string{"-c", "sleep 5"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled process took %s to terminate", elapsed)
	}
}
