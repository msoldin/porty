package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
	portysqlite "github.com/msoldin/porty/internal/infrastructure/sqlite"
)

func TestOperationStoreTracksLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewOperationStore(db)
	operation := domain.Operation{ID: "op_1", Kind: "pull", ScopeType: "repository", RequestKey: "request-1", Status: domain.OperationQueued}
	if err := store.CreateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	operation.Status = domain.OperationSucceeded
	operation.StartedAt = time.Now().UTC()
	operation.CompletedAt = operation.StartedAt.Add(time.Second)
	operation.Output = "done"
	if err := store.UpdateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Operation(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domain.OperationSucceeded || loaded.Output != "done" || loaded.RequestKey != "request-1" {
		t.Fatalf("Operation() = %#v", loaded)
	}
}
