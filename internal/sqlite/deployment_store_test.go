package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	portyop "github.com/msoldin/porty/internal/operation"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	portystack "github.com/msoldin/porty/internal/stack"
)

func TestDeploymentStoreTracksSuccessfulHistoryAfterFailedDeployments(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stacks := portysqlite.NewStackStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	stack := portystack.Stack{ID: "stk_gateway", DirectoryName: "gateway", ComposeProjectName: "porty-gateway-123", CreatedAt: now, UpdatedAt: now}
	if err := stacks.Create(ctx, stack); err != nil {
		t.Fatal(err)
	}
	store := portysqlite.NewDeploymentStore(db)
	hasDeployed, err := store.HasSuccessfulDeployment(ctx, stack.ID)
	if err != nil || hasDeployed {
		t.Fatalf("new stack has successful deployment = %v, %v", hasDeployed, err)
	}
	operations := portysqlite.NewOperationStore(db)
	if err := operations.CreateOperation(ctx, portyop.Operation{ID: "op_failed_first", Kind: "deploy", ScopeType: "stack", ScopeID: string(stack.ID), Status: portyop.OperationFailed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDeployment(ctx, portyop.Deployment{ID: "dep_failed_first", StackID: stack.ID, OperationID: "op_failed_first", Status: portyop.DeploymentFailed, StartedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	hasDeployed, err = store.HasSuccessfulDeployment(ctx, stack.ID)
	if err != nil || hasDeployed {
		t.Fatalf("failed-only stack has successful deployment = %v, %v", hasDeployed, err)
	}
	if err := operations.CreateOperation(ctx, portyop.Operation{ID: "op_1", Kind: "deploy", ScopeType: "stack", ScopeID: string(stack.ID), Status: portyop.OperationRunning}); err != nil {
		t.Fatal(err)
	}
	deployment := portyop.Deployment{
		ID: "dep_1", StackID: stack.ID, OperationID: "op_1", GitCommit: "abc123", Dirty: true,
		ComposeDigest: "sha256:desired", Status: portyop.DeploymentSucceeded,
		StartedAt: now, CompletedAt: now.Add(time.Second), Duration: time.Second,
	}
	if err := store.SaveDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	hasDeployed, err = store.HasSuccessfulDeployment(ctx, stack.ID)
	if err != nil || !hasDeployed {
		t.Fatalf("deployed stack has successful deployment = %v, %v", hasDeployed, err)
	}
	if err := operations.CreateOperation(ctx, portyop.Operation{ID: "op_failed_later", Kind: "deploy", ScopeType: "stack", ScopeID: string(stack.ID), Status: portyop.OperationFailed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDeployment(ctx, portyop.Deployment{ID: "dep_failed_later", StackID: stack.ID, OperationID: "op_failed_later", Status: portyop.DeploymentFailed, StartedAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	hasDeployed, err = store.HasSuccessfulDeployment(ctx, stack.ID)
	if err != nil || !hasDeployed {
		t.Fatalf("prior success after later failure = %v, %v", hasDeployed, err)
	}
	latest, err := store.LatestDeployment(ctx, stack.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "dep_failed_later" || latest.Status != portyop.DeploymentFailed {
		t.Fatalf("LatestDeployment() = %#v", latest)
	}
	history, err := store.Deployments(ctx, stack.ID, 20)
	if err != nil || len(history) != 3 {
		t.Fatalf("Deployments() = %#v, %v", history, err)
	}
	if err := stacks.Archive(ctx, stack.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := stacks.Purge(ctx, stack.ID); err != nil {
		t.Fatalf("Purge() with deployment history = %v", err)
	}
}

func TestDeploymentStoreKeepsUnfinishedTimestampNull(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	stackID := portystack.StackID("stk_gateway")
	if err := portysqlite.NewStackStore(db).Create(ctx, portystack.Stack{ID: stackID, DirectoryName: "gateway", ComposeProjectName: "porty-gateway", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := portysqlite.NewOperationStore(db).CreateOperation(ctx, portyop.Operation{ID: "op_1", Kind: "deploy", ScopeType: "stack", Status: portyop.OperationRunning}); err != nil {
		t.Fatal(err)
	}
	store := portysqlite.NewDeploymentStore(db)
	if err := store.SaveDeployment(ctx, portyop.Deployment{ID: "dep_1", StackID: stackID, OperationID: "op_1", StartedAt: now, Status: portyop.DeploymentStatus("running")}); err != nil {
		t.Fatal(err)
	}
	var nullCompletion bool
	if err := db.QueryRowContext(ctx, "SELECT completed_at IS NULL FROM deployments WHERE id='dep_1'").Scan(&nullCompletion); err != nil {
		t.Fatal(err)
	}
	if !nullCompletion {
		t.Fatal("unfinished deployment stored a non-NULL completion timestamp")
	}
	latest, err := store.LatestDeployment(ctx, stackID)
	if err != nil || !latest.CompletedAt.IsZero() {
		t.Fatalf("latest = %#v, %v", latest, err)
	}
	if _, err := store.LatestDeployment(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing latest: %v", err)
	}
}
