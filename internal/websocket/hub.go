package websocket

import (
	"sync"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type Event struct {
	Type      string    `json:"type"`
	Topic     string    `json:"topic"`
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Payload   any       `json:"payload"`
}

type Subscription struct {
	Replay []Event
	Gap    bool
	Events <-chan Event
	cancel func()
	once   sync.Once
}

func (s *Subscription) Cancel() {
	if s != nil && s.cancel != nil {
		s.once.Do(s.cancel)
	}
}

type subscriber struct {
	topic  string
	events chan Event
}

type Hub struct {
	mu          sync.Mutex
	capacity    int
	sequence    uint64
	replay      []Event
	nextID      uint64
	subscribers map[uint64]*subscriber
}

func NewHub(capacity int) *Hub {
	if capacity <= 0 {
		capacity = 256
	}
	return &Hub{capacity: capacity, subscribers: make(map[uint64]*subscriber)}
}

func (h *Hub) PublishOperation(operation domain.Operation) {
	h.publish(Event{Type: "operation", Topic: "operations", Timestamp: time.Now().UTC(), Payload: operation})
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
	h.subscribers[id] = &subscriber{topic: topic, events: channel}
	result := &Subscription{Events: channel}
	if len(h.replay) > 0 && since < h.replay[0].Sequence-1 {
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
