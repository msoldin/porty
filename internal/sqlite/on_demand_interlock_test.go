package sqlite_test

import (
	"context"
	"testing"

	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/operation"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/traffic"
)

func TestManualOperationAtomicallyHoldsAffectedOnDemandGroup(t *testing.T) {
	ctx := context.Background()
	_, db, _ := updateFixture(t)
	s := store.NewOnDemandStore(db)
	p := ondemand.DefaultPolicy()
	p.Name = "Game"
	p.Members = []string{"game"}
	p.Enabled = true
	e := ondemand.Evidence{SourceDigest: "source", ContainerIDs: []string{"one"}, Bindings: []traffic.Binding{{Network: "udp4", Address: "127.0.0.1:25565"}}}
	g, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running)
	if err != nil {
		t.Fatal(err)
	}
	operations := store.NewOperationStore(db)
	action := operation.Operation{ID: "manual-stop", Kind: "stop", ScopeType: "stack", ScopeID: "s", Status: operation.OperationQueued, Trigger: "manual", AffectedServices: []string{"other"}}
	if err := operations.CreateOperation(ctx, action); err != nil {
		t.Fatal(err)
	}
	current, _ := s.GetGroup(ctx, "s", g.ID)
	if current.HoldReason != "" {
		t.Fatal("unrelated service held")
	}
	action.ID = "selected-stop"
	action.AffectedServices = []string{"game"}
	if err := operations.CreateOperation(ctx, action); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetGroup(ctx, "s", g.ID)
	if current.HoldReason == "" || current.Revision != g.Revision+1 {
		t.Fatal("manual stop admitted without durable hold")
	}
	revision := current.Revision
	if err := operations.CreateOperation(ctx, action); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	current, _ = s.GetGroup(ctx, "s", g.ID)
	if current.Revision != revision {
		t.Fatal("failed admission changed policy")
	}
	action.ID = "manual-start"
	action.Kind = "start"
	if err := operations.CreateOperation(ctx, action); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetGroup(ctx, "s", g.ID)
	if current.HoldReason == "" {
		t.Fatal("manual start cleared hold")
	}
}
