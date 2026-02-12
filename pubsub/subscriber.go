package pubsub

import "github.com/hydra-db/hydra/store"

// Subscriber represents a single subscription to events
type Subscriber struct {
	ID           string
	EventChan    chan store.Event
	StreamFilter *string // nil means all streams
	Done         chan struct{}
}

// NewSubscriber creates a new subscriber
// bufferSize controls how many events can be buffered before blocking
func NewSubscriber(id string, streamFilter *string, bufferSize int) *Subscriber {
	return &Subscriber{
		ID:           id,
		EventChan:    make(chan store.Event, bufferSize),
		StreamFilter: streamFilter,
		Done:         make(chan struct{}),
	}
}

// Matches returns true if this subscriber wants events from the given stream
func (s *Subscriber) Matches(streamID string) bool {
	if s.StreamFilter == nil {
		return true // Subscribe to all
	}
	return *s.StreamFilter == streamID
}

// Close closes the subscriber channels
func (s *Subscriber) Close() {
	close(s.Done)
}
