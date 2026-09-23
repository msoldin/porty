package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiscoverReturnsOnlyImmediateVisibleDirectoriesWithExactComposeFile(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "gateway", "docker-compose.yml"), "services: {}\n")
	writeFixture(t, filepath.Join(root, "wrong-name", "compose.yaml"), "services: {}\n")
	writeFixture(t, filepath.Join(root, ".hidden", "docker-compose.yml"), "services: {}\n")
	writeFixture(t, filepath.Join(root, "parent", "nested", "docker-compose.yml"), "services: {}\n")

	manager, err := Open(root, Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	stacks, err := manager.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(stacks) != 1 || stacks[0] != "gateway" {
		t.Fatalf("Discover() = %#v, want [gateway]", stacks)
	}
}

func TestReadRejectsTraversalSymlinkAndSpecialFile(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "gateway", "docker-compose.yml"), "services: {}\n")
	outside := filepath.Join(t.TempDir(), "secret")
	writeFixture(t, outside, "outside")
	if err := os.Symlink(outside, filepath.Join(root, "gateway", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "gateway", "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(root, Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	for _, path := range []string{"../secret", "escape", "pipe"} {
		if _, err := manager.Read("gateway", path); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("Read(%q) error = %v, want ErrInvalidPath", path, err)
		}
	}
}

func TestWriteUsesContentHashToRejectStaleBrowser(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "gateway", "docker-compose.yml"), "services: {}\n")
	manager, err := Open(root, Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	current, err := manager.Read("gateway", "docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Write("gateway", "docker-compose.yml", []byte("services:\n  web: {}\n"), current.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Hash == current.Hash {
		t.Fatal("content hash did not change")
	}
	if _, err := manager.Write("gateway", "docker-compose.yml", []byte("services:\n  stale: {}\n"), current.Hash); !errors.Is(err, ErrStaleFile) {
		t.Fatalf("stale Write() error = %v, want ErrStaleFile", err)
	}
	contents, _ := os.ReadFile(filepath.Join(root, "gateway", "docker-compose.yml"))
	if string(contents) != "services:\n  web: {}\n" {
		t.Fatalf("stale write changed file to %q", contents)
	}
}

func TestCreateMoveAndRemoveStayInsideStack(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "gateway", "docker-compose.yml"), "services: {}\n")
	manager, err := Open(root, Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if err := manager.CreateDirectory("gateway", "config/nested"); err != nil {
		t.Fatal(err)
	}
	created, err := manager.CreateFile("gateway", "config/nested/app.conf", []byte("enabled=true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Hash == "" {
		t.Fatal("created file has empty hash")
	}
	if err := manager.Move("gateway", "config/nested/app.conf", "config/app.conf"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove("gateway", "config/nested"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Read("gateway", "config/app.conf"); err != nil {
		t.Fatalf("moved file cannot be read: %v", err)
	}
	if _, err := manager.CreateFile("gateway", "../outside", []byte("bad")); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("escape CreateFile() error = %v, want ErrInvalidPath", err)
	}
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
