package websocket_test

import (
	"context"
	"errors"
	"github.com/msoldin/porty/internal/alert"
	portysqlite "github.com/msoldin/porty/internal/sqlite"
	"path/filepath"
	"testing"
	"time"

	portyop "github.com/msoldin/porty/internal/operation"
	portyws "github.com/msoldin/porty/internal/websocket"
)

func TestHubReplaysEventsAndReportsSequenceGap(t *testing.T) {
	hub := portyws.NewHub(2)
	hub.PublishOperation(portyop.Operation{ID: "op_1", Status: portyop.OperationRunning})
	hub.PublishOperation(portyop.Operation{ID: "op_2", Status: portyop.OperationRunning})
	hub.PublishOperation(portyop.Operation{ID: "op_3", Status: portyop.OperationSucceeded})

	subscription := hub.Subscribe("operations", 0, 4)
	defer subscription.Cancel()
	if !subscription.Gap {
		t.Fatal("Subscribe() did not report replay gap")
	}
	if len(subscription.Replay) != 2 || subscription.Replay[0].Sequence != 2 || subscription.Replay[1].Sequence != 3 {
		t.Fatalf("replay = %#v", subscription.Replay)
	}
}

func TestHubDropsSlowSubscriptionWithoutBlockingPublisher(t *testing.T) {
	hub := portyws.NewHub(4)
	subscription := hub.Subscribe("operations", 0, 1)
	hub.PublishOperation(portyop.Operation{ID: "op_1"})
	done := make(chan struct{})
	go func() {
		hub.PublishOperation(portyop.Operation{ID: "op_2"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("slow subscriber blocked publisher")
	}
	for range subscription.Events {
		// A buffered event may remain, but the subscription must be closed.
	}
}

func TestHubReportsGapWhenClientCursorIsAheadAfterRestart(t *testing.T) {
	hub := portyws.NewHub(4)
	hub.PublishOperation(portyop.Operation{ID: "op_after_restart"})
	subscription := hub.Subscribe("operations", 1000, 1)
	defer subscription.Cancel()
	if !subscription.Gap {
		t.Fatal("Subscribe() did not report cursor-ahead restart gap")
	}
}

func TestAlertPublishesOnlyCommittedRevision(t *testing.T) {
	ctx := context.Background()
	db, err := portysqlite.Open(ctx, filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := portysqlite.NewAlertStore(db)
	items, err := store.Apply(ctx, []alert.Change{{Kind: "failure", Key: alert.Key{StackID: "s1", Problem: "deployment", Target: "stack"}, OccurrenceID: "o1", ObservedAt: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	hub := portyws.NewHub(4)
	service := alert.NewService(store, hub)
	subscription := hub.Subscribe("alerts", 0, 4)
	defer subscription.Cancel()
	mutation := alert.Mutation{ID: items[0].ID, ExpectedRevision: 1, ActorID: "admin"}
	updated, err := service.Acknowledge(ctx, mutation)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events:
		item, ok := event.Payload.(alert.Alert)
		if !ok || item.Revision != updated.Revision || event.Topic != "alerts" || event.Type != "alert" {
			t.Fatalf("event=%+v", event)
		}
		persisted, err := store.Get(ctx, item.ID)
		if err != nil || persisted.Revision != item.Revision {
			t.Fatalf("uncommitted event: %+v %v", persisted, err)
		}
	default:
		t.Fatal("missing committed event")
	}
	_, err = service.Acknowledge(ctx, mutation)
	if !errors.Is(err, alert.ErrConflict) {
		t.Fatalf("stale error=%v", err)
	}
	select {
	case event := <-subscription.Events:
		t.Fatalf("failed mutation published: %+v", event)
	default:
	}
}
