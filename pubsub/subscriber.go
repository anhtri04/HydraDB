package pubsub

// Event represents an event for pub/sub broadcasting
type Event struct {
	GlobalPosition int64  // Byte offset in the log file
	StreamID       string // Which stream this event belongs to
	StreamVersion  int64  // Version within the stream (0, 1, 2...)
	Data           []byte // The event payload
}

// Subscriber represents a single subscription to events
type Subscriber struct {
	ID           string
	EventChan    chan Event
	StreamFilter *string // nil means all streams
	Done         chan struct{}
}

// NewSubscriber creates a new subscriber
// bufferSize controls how many events can be buffered before blocking
func NewSubscriber(id string, streamFilter *string, bufferSize int) *Subscriber {
	return &Subscriber{
		ID:           id,
		EventChan:    make(chan Event, bufferSize),
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
