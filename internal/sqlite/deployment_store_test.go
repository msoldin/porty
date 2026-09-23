package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestDeploymentStorePersistsHistoryAndLatestSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stacks := portysqlite.NewStackStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	stack := domain.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway-123", CreatedAt: now, UpdatedAt: now}
	if err := stacks.Create(ctx, stack); err != nil {
		t.Fatal(err)
	}
	store := portysqlite.NewDeploymentStore(db)
	operations := portysqlite.NewOperationStore(db)
	if err := operations.CreateOperation(ctx, domain.Operation{ID: "op_1", Kind: "deploy", ScopeType: "stack", ScopeID: string(stack.ID), Status: domain.OperationRunning}); err != nil {
		t.Fatal(err)
	}
	deployment := domain.Deployment{
		ID: "dep_1", StackID: stack.ID, OperationID: "op_1", GitCommit: "abc123", Dirty: true,
		ComposeDigest: "sha256:desired", Status: domain.DeploymentSucceeded,
		StartedAt: now, CompletedAt: now.Add(time.Second), Duration: time.Second,
	}
	if err := store.SaveDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestDeployment(ctx, stack.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != deployment.ID || latest.ComposeDigest != deployment.ComposeDigest || latest.Duration != time.Second {
		t.Fatalf("LatestDeployment() = %#v", latest)
	}
	history, err := store.Deployments(ctx, stack.ID, 20)
	if err != nil || len(history) != 1 {
		t.Fatalf("Deployments() = %#v, %v", history, err)
	}
	if err := stacks.Archive(ctx, stack.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := stacks.Purge(ctx, stack.ID); err != nil {
		t.Fatalf("Purge() with deployment history = %v", err)
	}
}
