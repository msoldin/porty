package stack_test

import (
	"context"
	"errors"
	portystack "github.com/msoldin/porty/internal/stack"
	"os"
	"path/filepath"
	"testing"

	portyfs "github.com/msoldin/porty/internal/filesystem"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestDiscoveringSameDirectoryPreservesStableStackIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gateway"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway", "docker-compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := portyfs.Open(root, portyfs.Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := portystack.NewStackService(files, portysqlite.NewStackStore(db))

	first, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("stack identities first=%#v second=%#v", first, second)
	}
}

func TestRenameMovesDirectoryAndKeepsStableIdentity(t *testing.T) {
	ctx := context.Background()
	files, store, closeAll := stackFixture(t)
	defer closeAll()
	service := portystack.NewStackService(files, store)

	created, err := service.Create(ctx, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := service.Rename(ctx, created, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != created.ID || renamed.DirectoryName != "edge" {
		t.Fatalf("Rename() = %#v, want same identity in edge", renamed)
	}
	if _, err := files.Read("edge", "docker-compose.yml"); err != nil {
		t.Fatalf("renamed stack is unreadable: %v", err)
	}
}

func TestCreateRemovesDirectoryWhenMetadataWriteFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	files, err := portyfs.Open(root, portyfs.Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_stack BEFORE INSERT ON stacks BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	service := portystack.NewStackService(files, portysqlite.NewStackStore(db))
	if _, err := service.Create(ctx, "gateway"); err == nil {
		t.Fatal("Create succeeded despite the failed metadata write")
	}
	if _, err := os.Stat(filepath.Join(root, "gateway")); !os.IsNotExist(err) {
		t.Fatalf("failed stack directory remained: %v", err)
	}
}

func TestEnvironmentServiceReturnsKeysWithoutValues(t *testing.T) {
	ctx := context.Background()
	_, store, closeAll := stackFixture(t)
	defer closeAll()
	stacks := portystack.NewStackService(noopStackFiles{}, store)
	created, err := stacks.Create(ctx, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	environment := portystack.NewEnvironmentService(store)
	if err := environment.Set(ctx, created.ID, "TOKEN", "super-secret"); err != nil {
		t.Fatal(err)
	}
	keys, err := environment.Keys(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "TOKEN" {
		t.Fatalf("Keys() = %#v, want [TOKEN]", keys)
	}
	if err := environment.Set(ctx, created.ID, "BAD-KEY", "value"); !errors.Is(err, portystack.ErrInvalidEnvironment) {
		t.Fatalf("invalid Set() error = %v", err)
	}
}

type noopStackFiles struct{}

func (noopStackFiles) Discover() ([]string, error)      { return nil, nil }
func (noopStackFiles) CreateStack(string) error         { return nil }
func (noopStackFiles) RenameStack(string, string) error { return nil }
func (noopStackFiles) RemoveStack(string) error         { return nil }

func stackFixture(t *testing.T) (*portyfs.Manager, *portysqlite.StackStore, func()) {
	t.Helper()
	root := t.TempDir()
	files, err := portyfs.Open(root, portyfs.Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		files.Close()
		t.Fatal(err)
	}
	return files, portysqlite.NewStackStore(db), func() {
		files.Close()
		db.Close()
	}
}
