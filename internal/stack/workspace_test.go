package stack_test

import (
	"context"
	"errors"
	portyop "github.com/msoldin/porty/internal/operation"
	portystack "github.com/msoldin/porty/internal/stack"
	"os"
	"path/filepath"
	"testing"

	portycompose "github.com/msoldin/porty/internal/compose"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
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
	workspace := portystack.NewCoordinatedWorkspaceService(portystack.NewStackService(files, store), store, files, portystack.NewEnvironmentService(store), nil, nil, "")
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
	value, err := workspace.EnvironmentValue(ctx, stack.ID, "TOKEN")
	if err != nil || value.Value != "secret" || value.Secret {
		t.Fatalf("EnvironmentValue() = %#v, %v", value, err)
	}
}

func TestWorkspaceCoordinatesMutationsAndStopsComposeBeforeDelete(t *testing.T) {
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
	store := portysqlite.NewStackStore(db)
	coordinator := portyop.NewCoordinator()
	runtime := &deletionRuntime{t: t}
	workspace := portystack.NewCoordinatedWorkspaceService(portystack.NewStackService(files, store), store, files, portystack.NewEnvironmentService(store), coordinator, runtime, root)
	stack, err := workspace.CreateStack(ctx, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	release, err := coordinator.Try(false, string(stack.ID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = workspace.WriteFile(ctx, stack.ID, "docker-compose.yml", []byte("services: {}\n"), "wrong")
	release()
	if !errors.Is(err, portyop.ErrOperationConflict) {
		t.Fatalf("WriteFile() during operation = %v, want conflict", err)
	}
	runtime.wantCompose = filepath.Join(root, "gateway", "docker-compose.yml")
	if err := workspace.DeleteStack(ctx, stack.ID); err != nil {
		t.Fatal(err)
	}
	if !runtime.downCalled {
		t.Fatal("DeleteStack() did not stop Compose")
	}
}

type deletionRuntime struct {
	t           *testing.T
	wantCompose string
	downCalled  bool
}

func (r *deletionRuntime) Down(_ context.Context, request portycompose.Request) error {
	r.downCalled = true
	if request.StackDir != filepath.Dir(r.wantCompose) {
		r.t.Fatalf("Down() stack dir = %q", request.StackDir)
	}
	if _, err := os.Stat(r.wantCompose); err != nil {
		r.t.Fatalf("compose file was removed before Down(): %v", err)
	}
	return nil
}
