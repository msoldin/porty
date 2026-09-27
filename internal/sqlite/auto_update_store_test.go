package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/msoldin/porty/internal/alert"
	"github.com/msoldin/porty/internal/autoupdate"
	store "github.com/msoldin/porty/internal/sqlite"
	"github.com/msoldin/porty/internal/stack"
	"path/filepath"
	"testing"
	"time"
)

func updateFixture(t *testing.T) (*store.AutoUpdateStore, *sql.DB, time.Time) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if err := store.NewStackStore(db).Create(ctx, stack.Stack{ID: "s", DirectoryName: "app", ComposeProjectName: "app", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return store.NewAutoUpdateStore(db), db, now
}
func TestPolicyRejectsStaleWrite(t *testing.T) {
	s, _, now := updateFixture(t)
	ctx := context.Background()
	p, err := s.GetPolicy(ctx, "s")
	if err != nil || p.Enabled || p.Expression != autoupdate.DefaultExpression || p.Revision != 0 {
		t.Fatalf("default=%+v %v", p, err)
	}
	p, err = s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now)
	if err != nil || p.Revision != 1 {
		t.Fatalf("save=%+v %v", p, err)
	}
	_, err = s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: false, Expression: autoupdate.DefaultExpression}, now)
	if !errors.Is(err, autoupdate.ErrConflict) {
		t.Fatalf("stale=%v", err)
	}
}
func TestAdmissionIsUniquePerStackAndOccurrence(t *testing.T) {
	s, _, now := updateFixture(t)
	ctx := context.Background()
	p, err := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now)
	if err != nil {
		t.Fatal(err)
	}
	run, ok, err := s.Admit(ctx, p, p.NextRunAt)
	if err != nil || !ok || run.Phase != "queued" {
		t.Fatalf("admit=%+v %v %v", run, ok, err)
	}
	_, ok, err = s.Admit(ctx, p, p.NextRunAt)
	if err != nil || ok {
		t.Fatalf("duplicate=%v %v", ok, err)
	}
	runs, err := s.PendingRuns(ctx)
	if err != nil || len(runs) != 1 {
		t.Fatalf("pending=%+v %v", runs, err)
	}
}
func TestStartupSkipsMissedRuns(t *testing.T) {
	s, _, now := updateFixture(t)
	ctx := context.Background()
	_, err := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now)
	if err != nil {
		t.Fatal(err)
	}
	restart := now.Add(72 * time.Hour)
	if err := s.ResetAfterStartup(ctx, restart); err != nil {
		t.Fatal(err)
	}
	due, err := s.ListDue(ctx, restart, 20)
	if err != nil || len(due) != 0 {
		t.Fatalf("due=%+v %v", due, err)
	}
	p, err := s.GetPolicy(ctx, "s")
	if err != nil || !p.NextRunAt.After(restart) {
		t.Fatalf("next=%+v %v", p, err)
	}
}

func TestRunAndCheckAlertsCommitTogether(t *testing.T) {
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
	err = s.FinishRun(ctx, run.ID, "failed", "registry unavailable", []alert.Change{{Kind: "invalid"}})
	if err == nil {
		t.Fatal("invalid alert accepted")
	}
	pending, err := s.PendingRuns(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("transaction partially committed: %+v %v", pending, err)
	}
	changes := []alert.Change{{Kind: "failure", Key: alert.Key{StackID: "s", Problem: "update_check", Target: "stack"}, OccurrenceID: run.ID, ObservedAt: now, Summary: "Registry unavailable"}}
	for range 2 {
		if err := s.FinishRun(ctx, run.ID, "failed", "registry unavailable", changes); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.NewAlertStore(db).List(ctx, alert.Filter{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Count != 1 {
		t.Fatalf("duplicate check alert: %+v %v", page, err)
	}
}
func TestPolicyEditsCannotClearRecoveryPause(t *testing.T) {
	s, _, now := updateFixture(t)
	ctx := context.Background()
	_, err := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: autoupdate.DefaultExpression}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Pause(ctx, "s", "recovery_required"); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: false, Expression: autoupdate.DefaultExpression, ExpectedRevision: 2}, now)
	if err != nil || p.PausedReason != "recovery_required" {
		t.Fatalf("pause lost: %+v %v", p, err)
	}
}

func TestSchedulerSkipsOverlapAndRejectsStalePolicyRevision(t *testing.T) {
	s, _, now := updateFixture(t)
	ctx := context.Background()
	p, _ := s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: true, Expression: "* * * * *"}, now)
	_, accepted, err := s.Admit(ctx, p, p.NextRunAt)
	if err != nil || !accepted {
		t.Fatalf("first=%v %v", accepted, err)
	}
	p, _ = s.GetPolicy(ctx, "s")
	run, accepted, err := s.Admit(ctx, p, p.NextRunAt)
	if err != nil || accepted || run.Reason != "overlap" {
		t.Fatalf("overlap=%+v %v %v", run, accepted, err)
	}
	p, _ = s.GetPolicy(ctx, "s")
	_, err = s.SavePolicy(ctx, "s", autoupdate.PolicyUpdate{Enabled: false, Expression: p.Expression, ExpectedRevision: p.Revision}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, accepted, err = s.Admit(ctx, p, p.NextRunAt)
	if err != nil || accepted {
		t.Fatalf("stale admitted=%v %v", accepted, err)
	}
}
