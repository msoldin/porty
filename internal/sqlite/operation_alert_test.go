package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/alert"
	op "github.com/msoldin/porty/internal/operation"
	store "github.com/msoldin/porty/internal/sqlite"
)

func TestOperationCommitsFailureAndAlertTogether(t *testing.T) {
	db, alerts := alertDB(t)
	ops := store.NewOperationStore(db)
	ctx := context.Background()
	o := op.Operation{ID: "op-1", Kind: "deploy", ScopeType: "stack", ScopeID: "s", Status: op.OperationRunning}
	if err := ops.CreateOperation(ctx, o); err != nil {
		t.Fatal(err)
	}
	o.Status = op.OperationFailed
	o.CompletedAt = time.Now().UTC()
	change := alert.Change{Kind: "failure", Key: alert.Key{StackID: "s", Problem: "deployment", Target: "stack"}, OccurrenceID: o.ID, Summary: "Deployment failed", ObservedAt: o.CompletedAt}
	if _, err := db.Exec(`CREATE TRIGGER reject_alert BEFORE INSERT ON alerts BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.CompleteOperation(ctx, o, op.Result{Err: errors.New("failure"), Alerts: []alert.Change{change}}); err == nil {
		t.Fatal("completion succeeded despite alert failure")
	}
	got, err := ops.Operation(ctx, o.ID)
	if err != nil || got.Status != op.OperationRunning {
		t.Fatalf("partial commit: %+v %v", got, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_alert`); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.CompleteOperation(ctx, o, op.Result{Alerts: []alert.Change{change}}); err != nil {
		t.Fatal(err)
	}
	page, err := alerts.List(ctx, alert.Filter{})
	if err != nil || page.Total != 1 {
		t.Fatalf("missing alert: %+v %v", page, err)
	}
	got, err = ops.Operation(ctx, o.ID)
	if err != nil || got.Status != op.OperationFailed {
		t.Fatalf("missing completion: %+v %v", got, err)
	}
}

func TestInterruptedMutationRaisesAlertOnce(t *testing.T) {
	db, alerts := alertDB(t)
	ops := store.NewOperationStore(db)
	ctx := context.Background()
	key := alert.Key{StackID: "s", Problem: "deployment", Target: "stack"}
	o := op.Operation{ID: "op-1", Kind: "deploy", ScopeType: "stack", ScopeID: "s", Status: op.OperationRunning, AlertTargets: []alert.Key{key}, Trigger: "manual", StackName: "Example"}
	if err := ops.CreateOperation(ctx, o); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ops.FailInterrupted(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	page, err := alerts.List(ctx, alert.Filter{})
	if err != nil || page.Total != 1 || page.Items[0].Count != 1 || page.Items[0].StackName != "Example" {
		t.Fatalf("recovery: %+v %v", page, err)
	}
}

func TestReadOnlyFailureDoesNotRaiseAlert(t *testing.T) {
	db, alerts := alertDB(t)
	ops := store.NewOperationStore(db)
	ctx := context.Background()
	if err := ops.CreateOperation(ctx, op.Operation{ID: "read", Kind: "logs", ScopeType: "stack", ScopeID: "s", Status: op.OperationRunning}); err != nil {
		t.Fatal(err)
	}
	if err := ops.FailInterrupted(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	page, err := alerts.List(ctx, alert.Filter{})
	if err != nil || page.Total != 0 {
		t.Fatalf("read-only alert: %+v %v", page, err)
	}
}

func TestOperationCompletesWithoutResolvingNewerAlert(t *testing.T) {
	db, alerts := alertDB(t)
	ctx := context.Background()
	ops := store.NewOperationStore(db)
	a := failAlert(t, alerts, "first")
	newer := failAlert(t, alerts, "second")
	o := op.Operation{ID: "success", Status: op.OperationRunning, Kind: "deploy", ScopeType: "stack"}
	if err := ops.CreateOperation(ctx, o); err != nil {
		t.Fatal(err)
	}
	o.Status = op.OperationSucceeded
	o.CompletedAt = time.Now()
	_, err := ops.CompleteOperation(ctx, o, op.Result{Alerts: []alert.Change{{Kind: "recovery", Key: a.Key, OccurrenceID: o.ID, ExpectedRevision: a.Revision, ObservedAt: time.Now()}}})
	if err != nil {
		t.Fatalf("stale recovery prevented completion: %v", err)
	}
	got, err := alerts.Get(ctx, newer.ID)
	if err != nil || got.ResolvedAt != nil {
		t.Fatalf("new failure resolved: %+v %v", got, err)
	}
	saved, err := ops.Operation(ctx, o.ID)
	if err != nil || saved.Status != op.OperationSucceeded {
		t.Fatalf("completion missing: %+v %v", saved, err)
	}
}
