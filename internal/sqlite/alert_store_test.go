package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/alert"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func alertDB(t *testing.T) (*sql.DB, *portysqlite.AlertStore) {
	t.Helper()
	db, err := portysqlite.Open(context.Background(), filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, portysqlite.NewAlertStore(db)
}

func failAlert(t *testing.T, store *portysqlite.AlertStore, occurrence string) alert.Alert {
	t.Helper()
	values, err := store.Apply(context.Background(), []alert.Change{{Kind: "failure", Key: alert.Key{StackID: "stack-1", Problem: "deployment", Target: "stack"}, StackName: "My stack", OccurrenceID: occurrence, Summary: "Deployment failed", CanResolveManually: true, ObservedAt: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)}})
	if err != nil || len(values) != 1 {
		t.Fatalf("Apply: %v %v", values, err)
	}
	return values[0]
}

func TestAlertReopensUnacknowledgedAfterRecurrence(t *testing.T) {
	_, s := alertDB(t)
	ctx := context.Background()
	a := failAlert(t, s, "one")
	ack, err := s.Acknowledge(ctx, alert.Mutation{ID: a.ID, ExpectedRevision: a.Revision, ActorID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := s.Resolve(ctx, alert.Mutation{ID: a.ID, ExpectedRevision: ack.Revision, ActorID: "user-1", Note: "Recovered manually"})
	if err != nil || resolved.ResolvedAt == nil {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	reopened := failAlert(t, s, "two")
	if reopened.ID != a.ID || reopened.Episode != 2 || reopened.Count != 1 || reopened.AcknowledgedAt != nil || reopened.ResolvedAt != nil {
		t.Fatalf("reopened: %+v", reopened)
	}
	events, err := s.History(ctx, a.ID, 50, 0)
	if err != nil || len(events) != 4 {
		t.Fatalf("history: %+v %v", events, err)
	}
}

func TestAlertPreservesAcknowledgmentWhileOpen(t *testing.T) {
	_, s := alertDB(t)
	ctx := context.Background()
	a := failAlert(t, s, "one")
	ack, err := s.Acknowledge(ctx, alert.Mutation{ID: a.ID, ExpectedRevision: a.Revision, ActorID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	a = failAlert(t, s, "two")
	if a.Count != 2 || a.AcknowledgedAt == nil || a.AcknowledgedBy != "user-1" || a.Revision <= ack.Revision {
		t.Fatalf("repeat: %+v", a)
	}
	page, err := s.List(ctx, alert.Filter{})
	if err != nil || page.Total != 1 || page.UnacknowledgedCount != 0 {
		t.Fatalf("page: %+v %v", page, err)
	}
}

func TestRecoveryRejectsStaleRevision(t *testing.T) {
	_, s := alertDB(t)
	ctx := context.Background()
	old := failAlert(t, s, "one")
	a := failAlert(t, s, "two")
	_, err := s.Apply(ctx, []alert.Change{{Kind: "recovery", Key: a.Key, OccurrenceID: "late-success", ExpectedRevision: old.Revision, ObservedAt: time.Now()}})
	if !errors.Is(err, alert.ErrConflict) {
		t.Fatalf("stale recovery: %v", err)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil || got.ResolvedAt != nil {
		t.Fatalf("unexpected recovery: %+v %v", got, err)
	}
	_, err = s.Apply(ctx, []alert.Change{{Kind: "recovery", Key: a.Key, OccurrenceID: "success", ExpectedRevision: a.Revision, ObservedAt: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, alert.Filter{})
	if err != nil || page.Total != 1 || page.UnacknowledgedCount != 1 || page.Items[0].ResolvedAt == nil {
		t.Fatalf("recovered incident disappeared: %+v %v", page, err)
	}
}

func TestAlertIgnoresReplayedOccurrence(t *testing.T) {
	_, s := alertDB(t)
	ctx := context.Background()
	a := failAlert(t, s, "one")
	replay := failAlert(t, s, "one")
	if replay.Count != 1 || replay.Revision != a.Revision {
		t.Fatalf("replay changed alert: %+v", replay)
	}
	_, err := s.Acknowledge(ctx, alert.Mutation{ID: a.ID, ExpectedRevision: a.Revision, ActorID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Acknowledge(ctx, alert.Mutation{ID: a.ID, ExpectedRevision: a.Revision, ActorID: "user-1"})
	if !errors.Is(err, alert.ErrConflict) {
		t.Fatalf("stale ack: %v", err)
	}
}

func TestAlertSurvivesStackRemoval(t *testing.T) {
	db, s := alertDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO stacks(id,directory_name,compose_project_name,created_at,updated_at) VALUES('stack-1','My stack','my-stack','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	a := failAlert(t, s, "one")
	_, err = db.Exec(`DELETE FROM stacks WHERE id='stack-1'`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil || got.StackName != "My stack" {
		t.Fatalf("lost history: %+v %v", got, err)
	}
}

func TestAlertBatchRollsBackOnInvalidChange(t *testing.T) {
	_, s := alertDB(t)
	ctx := context.Background()
	_, err := s.Apply(ctx, []alert.Change{{Kind: "failure", Key: alert.Key{StackID: "s", Problem: "pull", Target: "app"}, OccurrenceID: "one", Summary: "Pull failed", ObservedAt: time.Now()}, {Kind: "invalid"}})
	if err == nil {
		t.Fatal("invalid change accepted")
	}
	page, err := s.List(ctx, alert.Filter{})
	if err != nil || page.Total != 0 || page.Items == nil {
		t.Fatalf("non-atomic batch: %+v %v", page, err)
	}
}

func TestAlertBadgeCountIgnoresPagination(t *testing.T) {
	_, s := alertDB(t)
	ctx := context.Background()
	failAlert(t, s, "one")
	page, err := s.List(ctx, alert.Filter{StackID: "another", Limit: 1, Offset: 100})
	if err != nil || len(page.Items) != 0 || page.Total != 0 || page.UnacknowledgedCount != 1 {
		t.Fatalf("count: %+v %v", page, err)
	}
}
