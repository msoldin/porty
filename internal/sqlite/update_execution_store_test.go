package sqlite_test

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/autoupdate"
	"github.com/msoldin/porty/internal/compose"
	op "github.com/msoldin/porty/internal/operation"
	store "github.com/msoldin/porty/internal/sqlite"
	"strings"
	"testing"
	"time"
)

func TestAutoUpdatePersistsIntentBeforeMutation(t *testing.T) {
	s, db, now := updateFixture(t)
	ctx := context.Background()
	p, err := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := s.Admit(ctx, p, p.NextRunAt)
	if err != nil {
		t.Fatal(err)
	}
	operation := op.Operation{ID: "op-update", Kind: "auto_update", ScopeType: "stack", ScopeID: "s", Status: op.OperationRunning}
	ops := store.NewOperationStore(db)
	if err := ops.CreateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	prepared := compose.PreparedUpdate{Snapshot: compose.UpdateSnapshot{SourceDigest: "source"}, Changes: []compose.ImageChange{{Service: "app", SourceReference: "alpine:latest", TargetReference: "alpine@sha256:" + strings.Repeat("b", 64), AfterImageID: "new-image"}}}
	if err := s.SavePrepared(ctx, run, operation, prepared, op.Deployment{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: false, Expression: autoupdate.DefaultExpression, ExpectedRevision: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkApplying(ctx, run.ID, 1); !errors.Is(err, autoupdate.ErrConflict) {
		t.Fatalf("disabled policy applied: %v", err)
	}
	pending, err := s.PendingExecutions(ctx)
	if err != nil || len(pending) != 1 || pending[0].Phase != "prepared" {
		t.Fatalf("intent=%+v %v", pending, err)
	}
}
func TestAutoUpdateCompletionCommitsDeploymentImagesAndPauseAtomically(t *testing.T) {
	s, db, now := updateFixture(t)
	ctx := context.Background()
	p, _ := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now)
	run, _, _ := s.Admit(ctx, p, p.NextRunAt)
	operation := op.Operation{ID: "op-update", Kind: "auto_update", ScopeType: "stack", ScopeID: "s", Status: op.OperationRunning}
	ops := store.NewOperationStore(db)
	if err := ops.CreateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
	change := compose.ImageChange{Service: "app", SourceReference: "alpine:latest", TargetReference: "alpine@sha256:" + strings.Repeat("b", 64), AfterImageID: "new-image"}
	if err := s.SavePrepared(ctx, run, operation, compose.PreparedUpdate{Snapshot: compose.UpdateSnapshot{SourceDigest: "source"}, Changes: []compose.ImageChange{change}}, op.Deployment{}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkApplying(ctx, run.ID, 1); err != nil {
		t.Fatal(err)
	}
	operation.Status = op.OperationSucceeded
	operation.CompletedAt = now.Add(time.Minute)
	result := op.Result{Update: &op.UpdateCompletion{RunID: run.ID, Deployment: op.Deployment{ID: "dep-update", StackID: "s", OperationID: operation.ID, Status: op.DeploymentSucceeded, ComposeDigest: "source", StartedAt: now}, Services: []compose.ServiceUpdateResult{{Service: "app", TargetImageID: "new-image", ActualImageID: "new-image", Outcome: "verified"}}}}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_images BEFORE INSERT ON deployment_images BEGIN SELECT RAISE(ABORT,'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.CompleteOperation(ctx, operation, result); err == nil {
		t.Fatal("injected write failure ignored")
	}
	saved, err := ops.Operation(ctx, operation.ID)
	if err != nil || saved.Status != op.OperationRunning {
		t.Fatalf("partial completion=%+v %v", saved, err)
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER fail_images"); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.CompleteOperation(ctx, operation, result); err != nil {
		t.Fatal(err)
	}
	images, err := s.EffectiveImages(ctx, "s")
	if err != nil || images["app"].TargetReference != change.TargetReference {
		t.Fatalf("images=%+v %v", images, err)
	}
	pending, err := s.PendingRuns(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("run incomplete: %+v %v", pending, err)
	}
}
