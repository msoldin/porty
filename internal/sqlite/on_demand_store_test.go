package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/msoldin/porty/internal/ondemand"
	"github.com/msoldin/porty/internal/operation"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/traffic"
)

func onDemandFixture(t *testing.T) (*store.OnDemandStore, ondemand.Policy, ondemand.Evidence) {
	t.Helper()
	_, db, _ := updateFixture(t)
	p := ondemand.DefaultPolicy()
	p.Name = "Game"
	p.Members = []string{"game"}
	p.Enabled = true
	e := ondemand.Evidence{SourceDigest: "sha256:source", ContainerIDs: []string{"container-game"}, Bindings: []traffic.Binding{{Network: "udp4", Address: "127.0.0.1:25565"}}}
	return store.NewOnDemandStore(db), p, e
}
func TestOnDemandRejectsStalePolicyAndOverlappingMembership(t *testing.T) {
	ctx := context.Background()
	s, p, e := onDemandFixture(t)
	g, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running)
	if err != nil {
		t.Fatal(err)
	}
	if g.Revision != 1 || g.Phase != ondemand.Running {
		t.Fatalf("wrong initial group: %+v", g)
	}
	if _, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running); !errors.Is(err, ondemand.ErrConflict) {
		t.Fatalf("duplicate membership accepted: %v", err)
	}
	if _, err := s.SaveGroup(ctx, "s", g.ID, ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running); !errors.Is(err, ondemand.ErrConflict) {
		t.Fatalf("stale policy accepted: %v", err)
	}
	p.Name = "Renamed"
	g, err = s.SaveGroup(ctx, "s", g.ID, ondemand.PolicyUpdate{Policy: p, ExpectedRevision: 1}, e, ondemand.Running)
	if err != nil || g.Revision != 2 {
		t.Fatalf("update: %+v %v", g, err)
	}
}
func TestOnDemandHoldSurvivesEditAndRequiresExplicitResume(t *testing.T) {
	ctx := context.Background()
	s, p, e := onDemandFixture(t)
	g, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HoldGroup(ctx, "s", g.ID, g.Revision, "Manually held."); err != nil {
		t.Fatal(err)
	}
	g, err = s.GetGroup(ctx, "s", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.HoldReason == "" {
		t.Fatal("hold not persisted")
	}
	g, err = s.SaveGroup(ctx, "s", g.ID, ondemand.PolicyUpdate{Policy: p, ExpectedRevision: g.Revision}, e, ondemand.Running)
	if err != nil {
		t.Fatal(err)
	}
	if g.HoldReason == "" {
		t.Fatal("editing silently cleared manual hold")
	}
	g, err = s.ResumeGroup(ctx, "s", g.ID, g.Revision, e, ondemand.Running)
	if err != nil || g.HoldReason != "" || g.PausedReason != "" {
		t.Fatalf("resume: %+v %v", g, err)
	}
}
func TestOnDemandRejectsForeignOwnershipAndStaleDelete(t *testing.T) {
	ctx := context.Background()
	s, p, e := onDemandFixture(t)
	g, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetGroup(ctx, "other", g.ID); err == nil {
		t.Fatal("foreign group returned")
	}
	if err := s.DeleteGroup(ctx, "s", g.ID, g.Revision+1); !errors.Is(err, ondemand.ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if err := s.DeleteGroup(ctx, "s", g.ID, g.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running); err != nil {
		t.Fatalf("deleted membership retained: %v", err)
	}
}
func TestOnDemandPersistsTransitionAndPausesAfterInterruptedRestart(t *testing.T) {
	ctx := context.Background()
	_, db, _ := updateFixture(t)
	s := store.NewOnDemandStore(db)
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
	if err := store.NewOperationStore(db).CreateOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginTransition(ctx, g, ondemand.Sleep, op.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.GetGroup(ctx, "s", g.ID)
	if err != nil || fresh.Phase != ondemand.Stopping {
		t.Fatalf("intent not durable: %+v %v", fresh, err)
	}
	if err := s.BeginTransition(ctx, g, ondemand.Sleep, op.ID); !errors.Is(err, ondemand.ErrConflict) {
		t.Fatalf("duplicate transition admitted: %v", err)
	}
	if err := s.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	fresh, err = s.GetGroup(ctx, "s", g.ID)
	if err != nil || fresh.Phase != ondemand.Unknown || fresh.PausedReason == "" {
		t.Fatalf("interrupted mutation replayable: %+v %v", fresh, err)
	}
}

func TestOnDemandRejectsPublishedEndpointOverlapTransactionally(t *testing.T) {
	ctx := context.Background()
	s, p, e := onDemandFixture(t)
	if _, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running); err != nil {
		t.Fatal(err)
	}
	p.Members = []string{"other"}
	e.ContainerIDs = []string{"container-other"}
	e.Bindings[0].Address = "0.0.0.0:25565"
	if _, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running); !errors.Is(err, ondemand.ErrConflict) {
		t.Fatalf("wildcard overlap accepted: %v", err)
	}
	groups, err := s.ListGroups(ctx, "s")
	if err != nil || len(groups) != 1 {
		t.Fatalf("failed creation leaked a group: %+v %v", groups, err)
	}
	e.Bindings[0].Address = "127.0.0.1:25566"
	if _, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Running); err != nil {
		t.Fatalf("failed creation leaked membership: %v", err)
	}
}

func TestOnDemandHeldGroupCannotAdmitQueuedWake(t *testing.T) {
	ctx := context.Background()
	s, p, e := onDemandFixture(t)
	g, err := s.SaveGroup(ctx, "s", "", ondemand.PolicyUpdate{Policy: p}, e, ondemand.Sleeping)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HoldGroup(ctx, "s", g.ID, g.Revision, "Manual stop."); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginTransition(ctx, g, ondemand.WakeUp, "unused-operation"); !errors.Is(err, ondemand.ErrConflict) {
		t.Fatalf("stale queued wake admitted after hold: %v", err)
	}
}
