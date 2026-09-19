package application_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/msoldin/porty/internal/application"
	portyfs "github.com/msoldin/porty/internal/infrastructure/filesystem"
	portysqlite "github.com/msoldin/porty/internal/infrastructure/sqlite"
)

func TestWorkspaceResolvesOpaqueStackIDForFileAndEnvironmentOperations(t *testing.T) {
	ctx := context.Background()
	files, err := portyfs.Open(t.TempDir(), portyfs.Limits{MaxEditableBytes: 1 << 20, MaxDepth: 32, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewStackStore(db)
	workspace := application.NewWorkspaceService(application.NewStackService(files, store), store, files, application.NewEnvironmentService(store))
	stack, err := workspace.CreateStack(ctx, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	file, err := workspace.ReadFile(ctx, stack.ID, "docker-compose.yml")
	if err != nil || file.Hash == "" {
		t.Fatalf("ReadFile() = %#v, %v", file, err)
	}
	if err := workspace.SetEnvironment(ctx, stack.ID, "TOKEN", "secret"); err != nil {
		t.Fatal(err)
	}
	keys, err := workspace.EnvironmentKeys(ctx, stack.ID)
	if err != nil || len(keys) != 1 || keys[0] != "TOKEN" {
		t.Fatalf("keys = %#v, %v", keys, err)
	}
}
