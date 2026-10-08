package sqlite_test

import (
	"context"
	"testing"

	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/operation"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/traffic"
)

func TestOnDemandCompletionCommitsStateOperationAndAlertsTogether(t *testing.T) {
	ctx := context.Background()
	_, db, now := updateFixture(t)
	s := store.NewOnDemandStore(db)
	ops := store.NewOperationStore(db)
	p := ondemand.DefaultPolicy()
	p.Name = "Game"
	p.Members = []string{"game"}
	p.Enabled = true
	e := ondemand.Evidence{SourceDigest: "source", ContainerIDs: []string{"id"}, Bindings: []traffic.Binding{{Network: "tcp4", Address: "127.0.0.1:25565"}}}
	g, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running)
	if err != nil {
		t.Fatal(err)
	}
	op := operation.Operation{ID: "op_sleep", Kind: "on-demand-sleep", ScopeType: "stack", ScopeID: "s", Status: operation.OperationRunning}
	if err := ops.CreateOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginTransition(ctx, g, ondemand.Sleep, op.ID); err != nil {
		t.Fatal(err)
	}
	op.Status = operation.OperationSucceeded
	op.CompletedAt = now
	result := operation.Result{OnDemand: &operation.OnDemandCompletion{GroupID: g.ID, Phase: string(ondemand.Sleeping)}, Alerts: []alert.Change{{Kind: "invalid"}}}
	if _, err := ops.CompleteOperation(ctx, op, result); err == nil {
		t.Fatal("invalid alert accepted")
	}
	fresh, err := s.GetGroup(ctx, "s", g.ID)
	if err != nil || fresh.Phase != ondemand.Stopping {
		t.Fatalf("partial group commit: %+v %v", fresh, err)
	}
	saved, err := ops.Operation(ctx, op.ID)
	if err != nil || saved.Status != operation.OperationRunning {
		t.Fatalf("partial operation commit: %+v %v", saved, err)
	}
	result.Alerts = nil
	if _, err := ops.CompleteOperation(ctx, op, result); err != nil {
		t.Fatal(err)
	}
	fresh, err = s.GetGroup(ctx, "s", g.ID)
	if err != nil || fresh.Phase != ondemand.Sleeping {
		t.Fatalf("successful sleep not committed: %+v %v", fresh, err)
	}
}
