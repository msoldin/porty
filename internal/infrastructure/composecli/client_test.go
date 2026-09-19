package composecli_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/infrastructure/composecli"
	portyprocess "github.com/msoldin/porty/internal/infrastructure/process"
)

func TestCommandsUseExactArgumentsAndEphemeralEnvironmentFile(t *testing.T) {
	runner := &composeRunner{t: t}
	client := composecli.New(runner, 2*time.Second)
	request := composecli.Request{
		StackDir:    filepath.Join(t.TempDir(), "gateway"),
		ProjectName: "porty-gateway-a1b2c3",
		Environment: map[string]string{"TOKEN": "very-secret"},
	}
	if err := os.MkdirAll(request.StackDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := client.Validate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := client.Deploy(context.Background(), request, false); err != nil {
		t.Fatal(err)
	}
	wantSuffixes := [][]string{
		{"config", "--quiet"},
		{"up", "-d", "--remove-orphans"},
	}
	for index, call := range runner.calls {
		if !reflect.DeepEqual(call.Args[len(call.Args)-len(wantSuffixes[index]):], wantSuffixes[index]) {
			t.Fatalf("call %d args = %#v", index, call.Args)
		}
		if call.Name != "docker" || call.Dir != request.StackDir || !reflect.DeepEqual(call.Redact, []string{"very-secret"}) {
			t.Fatalf("call %d = %#v", index, call)
		}
	}
	if len(runner.envPaths) != 2 || runner.envPaths[0] == runner.envPaths[1] {
		t.Fatalf("environment paths = %#v", runner.envPaths)
	}
	for _, path := range runner.envPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("environment file %q survived command", path)
		}
	}
}

func TestNormalizedConfigDigestDoesNotReturnRenderedSecrets(t *testing.T) {
	runner := &composeRunner{t: t, output: "{\"services\":{\"web\":{\"environment\":[\"TOKEN=very-secret\"]}}}"}
	client := composecli.New(runner, time.Second)
	request := composecli.Request{StackDir: t.TempDir(), ProjectName: "porty-web-123", Environment: map[string]string{"TOKEN": "very-secret"}}
	digest, err := client.Digest(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(digest, "sha256:") || strings.Contains(digest, "very-secret") {
		t.Fatalf("Digest() = %q", digest)
	}
}

type composeRunner struct {
	t        *testing.T
	calls    []portyprocess.Request
	envPaths []string
	output   string
}

func (r *composeRunner) Run(ctx context.Context, request portyprocess.Request) (portyprocess.Result, error) {
	r.calls = append(r.calls, request)
	if _, present := ctx.Deadline(); !present {
		r.t.Fatal("Compose command has no deadline")
	}
	for index, argument := range request.Args {
		if argument != "--env-file" || index+1 >= len(request.Args) {
			continue
		}
		path := request.Args[index+1]
		r.envPaths = append(r.envPaths, path)
		info, err := os.Stat(path)
		if err != nil {
			r.t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			r.t.Fatalf("environment mode = %o", info.Mode().Perm())
		}
		contents, _ := os.ReadFile(path)
		if string(contents) != "TOKEN=\"very-secret\"\n" {
			r.t.Fatalf("environment contents = %q", contents)
		}
	}
	return portyprocess.Result{Output: r.output}, nil
}
