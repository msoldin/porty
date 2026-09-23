package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
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

func TestOperationStorePreservesNullsPaginationAndErrors(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewOperationStore(db)
	for _, id := range []string{"op_1", "op_2"} {
		if err := store.CreateOperation(ctx, domain.Operation{ID: id, Kind: "pull", ScopeType: "repository", Status: domain.OperationQueued}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.OperationsPage(ctx, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.OperationsPage(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != "op_2" || second[0].ID != "op_1" || !first[0].StartedAt.IsZero() || !first[0].CompletedAt.IsZero() {
		t.Fatalf("pages %#v %#v", first, second)
	}
	if _, err := store.Operation(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing row: %v", err)
	}
	if err := store.UpdateOperation(ctx, domain.Operation{ID: "missing"}); err == nil {
		t.Fatal("missing update accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.OperationsPage(cancelled, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query: %v", err)
	}
}

func TestOperationStoreFailsInterruptedWorkOnStartup(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewOperationStore(db)
	for _, operation := range []domain.Operation{
		{ID: "op_queued", Kind: "pull", ScopeType: "repository", Status: domain.OperationQueued},
		{ID: "op_running", Kind: "deploy", ScopeType: "stack", ScopeID: "stk_1", Status: domain.OperationRunning},
	} {
		if err := store.CreateOperation(ctx, operation); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.FailInterrupted(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"op_queued", "op_running"} {
		operation, err := store.Operation(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if operation.Status != domain.OperationFailed || operation.ErrorCode != "server_restarted" || operation.CompletedAt.IsZero() {
			t.Fatalf("recovered %s = %#v", id, operation)
		}
	}
}
