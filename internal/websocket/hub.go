package websocket

import (
	"context"
	"sync"
	"time"

	portyop "github.com/msoldin/porty/internal/operation"
)

type Event struct {
	Type      string    `json:"type"`
	Topic     string    `json:"topic"`
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Payload   any       `json:"payload"`
}

type Subscription struct {
	Replay  []Event
	Gap     bool
	Events  <-chan Event
	Dropped <-chan struct{}
	cancel  func()
	once    sync.Once
}

func (s *Subscription) Cancel() {
	if s != nil && s.cancel != nil {
		s.once.Do(s.cancel)
	}
}

type subscriber struct {
	topic   string
	events  chan Event
	dropped chan struct{}
}

type Hub struct {
	mu          sync.Mutex
	capacity    int
	sequence    uint64
	replay      []Event
	nextID      uint64
	subscribers map[uint64]*subscriber
	connections map[uint64]context.CancelFunc
}

func NewHub(capacity int) *Hub {
	if capacity <= 0 {
		capacity = 256
	}
	return &Hub{capacity: capacity, subscribers: make(map[uint64]*subscriber), connections: make(map[uint64]context.CancelFunc)}
}

func (h *Hub) RegisterConnection(cancel context.CancelFunc) func() {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.connections[id] = cancel
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.connections, id)
		h.mu.Unlock()
	}
}

func (h *Hub) CloseConnections() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, cancel := range h.connections {
		cancel()
	}
}

func (h *Hub) PublishOperation(operation portyop.Operation) {
	h.publish(Event{Type: "operation", Topic: "operations", Timestamp: time.Now().UTC(), Payload: operation})
}

func (h *Hub) PublishLog(stackID, output string) {
	h.publish(Event{Type: "log", Topic: "logs:" + stackID, Timestamp: time.Now().UTC(), Payload: map[string]string{"output": output}})
}

func (h *Hub) publish(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sequence++
	event.Sequence = h.sequence
	h.replay = append(h.replay, event)
	if len(h.replay) > h.capacity {
		h.replay = append([]Event(nil), h.replay[len(h.replay)-h.capacity:]...)
	}
	for id, subscription := range h.subscribers {
		if subscription.topic != event.Topic {
			continue
		}
		select {
		case subscription.events <- event:
		default:
			delete(h.subscribers, id)
			close(subscription.dropped)
			close(subscription.events)
		}
	}
}

func (h *Hub) Subscribe(topic string, since uint64, buffer int) *Subscription {
	if buffer <= 0 {
		buffer = 32
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	channel := make(chan Event, buffer)
	dropped := make(chan struct{})
	h.subscribers[id] = &subscriber{topic: topic, events: channel, dropped: dropped}
	result := &Subscription{Events: channel, Dropped: dropped}
	if since > h.sequence || (len(h.replay) > 0 && since < h.replay[0].Sequence-1) {
		result.Gap = true
	}
	for _, event := range h.replay {
		if event.Topic == topic && event.Sequence > since {
			result.Replay = append(result.Replay, event)
		}
	}
	result.cancel = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, exists := h.subscribers[id]; exists {
			delete(h.subscribers, id)
			close(channel)
		}
	}
	return result
}
