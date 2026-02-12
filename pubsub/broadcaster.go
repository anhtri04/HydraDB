package pubsub

import (
	"fmt"
	"sync"

	"github.com/hydra-db/hydra/store"
)

// Broadcaster manages subscriptions and broadcasts events
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	nextID      int
}

// NewBroadcaster creates a new broadcaster
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string]*Subscriber),
	}
}

// Subscribe creates a new subscription and returns the subscriber
// streamFilter: nil for all streams, or pointer to specific streamID
// bufferSize: how many events to buffer (prevents slow consumers from blocking)
func (b *Broadcaster) Subscribe(streamFilter *string, bufferSize int) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := fmt.Sprintf("sub-%d", b.nextID)

	sub := NewSubscriber(id, streamFilter, bufferSize)
	b.subscribers[id] = sub

	return sub
}

// Unsubscribe removes a subscriber
func (b *Broadcaster) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sub, exists := b.subscribers[id]; exists {
		sub.Close()
		delete(b.subscribers, id)
	}
}

// Publish sends an event to all matching subscribers
// Non-blocking: if a subscriber's buffer is full, the event is dropped for that subscriber
func (b *Broadcaster) Publish(event store.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		if sub.Matches(event.StreamID) {
			select {
			case sub.EventChan <- event:
				// Sent successfully
			default:
				// Buffer full, drop event for this slow subscriber
			}
		}
	}
}

// SubscriberCount returns the number of active subscribers
func (b *Broadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
