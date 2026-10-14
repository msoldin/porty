package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintVersionUnderStandaloneFlag(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	defer func() { version, commit, buildDate = oldVersion, oldCommit, oldDate }()
	version, commit, buildDate = "v0.1.0", strings.Repeat("a", 40), "2026-10-14T12:00:00Z"
	var out bytes.Buffer
	handled, err := printVersion([]string{"--version"}, &out)
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	want := "porty v0.1.0 (commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, built 2026-10-14T12:00:00Z)\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestPrintVersionUnderOtherArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"reset-password"}, {"--version", "extra"}} {
		var out bytes.Buffer
		handled, err := printVersion(args, &out)
		if handled || err != nil || out.Len() != 0 {
			t.Fatalf("args=%v handled=%v err=%v output=%q", args, handled, err, out.String())
		}
	}
}

type brokenVersionWriter struct{}

func (brokenVersionWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPrintVersionUnderWriterFailure(t *testing.T) {
	handled, err := printVersion([]string{"--version"}, brokenVersionWriter{})
	if !handled || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestVersionCommandUnderInvalidRuntimeConfiguration(t *testing.T) {
	if os.Getenv("PORTY_VERSION_TEST_HELPER") == "1" {
		os.Args = []string{"porty", "--version"}
		main()
		return
	}
	dataDir := filepath.Join(t.TempDir(), "must-not-be-created")
	cmd := exec.Command(os.Args[0], "-test.run=^TestVersionCommandUnderInvalidRuntimeConfiguration$")
	cmd.Env = append(os.Environ(), "PORTY_VERSION_TEST_HELPER=1", "PORTY_LISTEN=invalid address", "PORTY_DATA_DIR="+dataDir, "DOCKER_HOST=unix:///nonexistent/docker.sock")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v: %s", err, output)
	}
	if !bytes.Contains(output, []byte("porty dev (commit unknown, built unknown)")) {
		t.Fatalf("unexpected output: %s", output)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data directory was touched: %v", err)
	}
}
