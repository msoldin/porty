package sqlite_test

import (
	"context"
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
}
