package dashboard

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// EventType identifies the category of a dashboard event.
type EventType string

const (
	EventToolCall   EventType = "tool_call"
	EventAuth       EventType = "auth"
	EventConnection EventType = "connection"
	EventError      EventType = "error"
)

// Event is a dashboard event broadcast to SSE subscribers.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Server    string      `json:"server,omitempty"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
}

// EventBus manages SSE subscribers and broadcasts events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
	nextID      int
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]chan Event),
	}
}

// Subscribe registers a new SSE subscriber and returns a channel and unsubscribe function.
func (b *EventBus) Subscribe() (string, <-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := fmt.Sprintf("sub_%d", b.nextID)
	ch := make(chan Event, 64)
	b.subscribers[id] = ch

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(ch)
		}
	}

	return id, ch, unsubscribe
}

// Publish sends an event to all subscribers (non-blocking).
func (b *EventBus) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Drop event if subscriber is slow
		}
	}
}

// MarshalSSE formats an event as an SSE data line.
func MarshalSSE(event Event) ([]byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return append(append([]byte("data: "), data...), '\n', '\n'), nil
}
