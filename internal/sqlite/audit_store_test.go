package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
)

func TestAuditStoreRecordsAndPaginatesEvents(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "porty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewAuditStore(db)
	for index, id := range []string{"aud_1", "aud_2"} {
		if err := store.RecordAudit(ctx, domain.AuditEvent{ID: id, Action: "stack.delete", TargetType: "stack", Outcome: "succeeded", RequestID: "req_1", OccurredAt: time.Now().UTC().Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.AuditEvents(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "aud_1" {
		t.Fatalf("AuditEvents() = %#v", events)
	}
	if events[0].ActorUserID != "" || events[0].SourceIP != "" || events[0].OccurredAt.IsZero() {
		t.Fatalf("nullable audit fields = %#v", events[0])
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.AuditEvents(cancelled, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled audit query: %v", err)
	}
}
