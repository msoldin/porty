package websocket

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Handler struct{ Hub *Hub }

type command struct {
	Type           string `json:"type"`
	SubscriptionID string `json:"subscriptionId"`
	Topic          string `json:"topic"`
	Since          uint64 `json:"since"`
}

type envelope struct {
	Type           string    `json:"type"`
	SubscriptionID string    `json:"subscriptionId"`
	Sequence       uint64    `json:"sequence"`
	Timestamp      time.Time `json:"timestamp"`
	Payload        any       `json:"payload,omitempty"`
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	unregister := h.Hub.RegisterConnection(func() { cancel(); connection.CloseNow() })
	defer unregister()
	go func() { <-ctx.Done(); connection.CloseNow() }()
	outgoing := make(chan envelope, 64)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			var message envelope
			select {
			case <-ctx.Done():
				return
			case message = <-outgoing:
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(writeCtx, connection, message)
			writeCancel()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	var mu sync.Mutex
	var forwarders sync.WaitGroup
	subscriptions := make(map[string]*Subscription)
	defer func() {
		cancel()
		mu.Lock()
		for _, subscription := range subscriptions {
			subscription.Cancel()
		}
		mu.Unlock()
		forwarders.Wait()
		<-writerDone
	}()

	for {
		var input command
		if err := wsjson.Read(ctx, connection, &input); err != nil {
			return
		}
		switch input.Type {
		case "subscribe":
			if input.SubscriptionID == "" || input.Topic == "" {
				_ = connection.Close(websocket.StatusPolicyViolation, "invalid subscription")
				return
			}
			mu.Lock()
			if previous := subscriptions[input.SubscriptionID]; previous != nil {
				previous.Cancel()
			}
			subscription := h.Hub.Subscribe(input.Topic, input.Since, 32)
			subscriptions[input.SubscriptionID] = subscription
			mu.Unlock()
			if subscription.Gap {
				if !sendEnvelope(ctx, outgoing, envelope{Type: "gap", SubscriptionID: input.SubscriptionID, Timestamp: time.Now().UTC(), Payload: map[string]any{"since": input.Since}}) {
					return
				}
			}
			for _, event := range subscription.Replay {
				if !sendEvent(ctx, outgoing, input.SubscriptionID, event) {
					return
				}
			}
			forwarders.Add(1)
			go func() {
				defer forwarders.Done()
				forward(ctx, cancel, outgoing, input.SubscriptionID, subscription)
			}()
		case "unsubscribe":
			mu.Lock()
			if subscription := subscriptions[input.SubscriptionID]; subscription != nil {
				subscription.Cancel()
				delete(subscriptions, input.SubscriptionID)
			}
			mu.Unlock()
		default:
			_ = connection.Close(websocket.StatusPolicyViolation, "unknown command")
			return
		}
	}
}

func forward(ctx context.Context, fail context.CancelFunc, outgoing chan<- envelope, id string, subscription *Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-subscription.Events:
			if !open {
				select {
				case <-subscription.Dropped:
					fail()
				default:
				}
				return
			}
			if !sendEvent(ctx, outgoing, id, event) {
				fail()
				return
			}
		}
	}
}

func sendEvent(ctx context.Context, outgoing chan<- envelope, id string, event Event) bool {
	return sendEnvelope(ctx, outgoing, envelope{Type: event.Type, SubscriptionID: id, Sequence: event.Sequence, Timestamp: event.Timestamp, Payload: event.Payload})
}

func sendEnvelope(ctx context.Context, outgoing chan<- envelope, value envelope) bool {
	select {
	case outgoing <- value:
		return true
	case <-ctx.Done():
		return false
	default:
		return false
	}
}
