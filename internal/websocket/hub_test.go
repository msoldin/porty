package websocket_test

import (
	"testing"
	"time"

	"github.com/msoldin/porty/internal/domain"
	portyws "github.com/msoldin/porty/internal/websocket"
)

func TestHubReplaysEventsAndReportsSequenceGap(t *testing.T) {
	hub := portyws.NewHub(2)
	hub.PublishOperation(domain.Operation{ID: "op_1", Status: domain.OperationRunning})
	hub.PublishOperation(domain.Operation{ID: "op_2", Status: domain.OperationRunning})
	hub.PublishOperation(domain.Operation{ID: "op_3", Status: domain.OperationSucceeded})

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
	hub.PublishOperation(domain.Operation{ID: "op_1"})
	done := make(chan struct{})
	go func() {
		hub.PublishOperation(domain.Operation{ID: "op_2"})
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
	hub.PublishOperation(domain.Operation{ID: "op_after_restart"})
	subscription := hub.Subscribe("operations", 1000, 1)
	defer subscription.Cancel()
	if !subscription.Gap {
		t.Fatal("Subscribe() did not report cursor-ahead restart gap")
	}
}
