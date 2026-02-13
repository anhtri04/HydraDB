package store

import "time"

// SnapshotStrategy determines when to create snapshots
type SnapshotStrategy interface {
	// ShouldSnapshot returns true if a snapshot should be created
	ShouldSnapshot(streamID string, version int64, eventType string) bool

	// Name returns the strategy name for logging
	Name() string
}

// EveryNEvents creates a snapshot every N events
type EveryNEvents struct {
	N int64
}

func (s *EveryNEvents) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	return version > 0 && version%s.N == 0
}

func (s *EveryNEvents) Name() string {
	return "every-n-events"
}

// OnEventTypes creates a snapshot when specific event types are stored
type OnEventTypes struct {
	Types map[string]bool
}

func NewOnEventTypes(types ...string) *OnEventTypes {
	m := make(map[string]bool)
	for _, t := range types {
		m[t] = true
	}
	return &OnEventTypes{Types: m}
}

func (s *OnEventTypes) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	return s.Types[eventType]
}

func (s *OnEventTypes) Name() string {
	return "on-event-types"
}

// TimeBased creates a snapshot if enough time has passed since the last one
type TimeBased struct {
	Interval     time.Duration
	lastSnapshot map[string]time.Time
}

func NewTimeBased(interval time.Duration) *TimeBased {
	return &TimeBased{
		Interval:     interval,
		lastSnapshot: make(map[string]time.Time),
	}
}

func (s *TimeBased) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	last, exists := s.lastSnapshot[streamID]
	if !exists || time.Since(last) >= s.Interval {
		s.lastSnapshot[streamID] = time.Now()
		return true
	}
	return false
}

func (s *TimeBased) Name() string {
	return "time-based"
}

// AfterEachEvent always returns true (snapshot every event)
type AfterEachEvent struct{}

func (s *AfterEachEvent) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	return true
}

func (s *AfterEachEvent) Name() string {
	return "after-each-event"
}

// Never never creates snapshots
type Never struct{}

func (s *Never) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	return false
}

func (s *Never) Name() string {
	return "never"
}

// Composite combines multiple strategies (OR logic)
type Composite struct {
	Strategies []SnapshotStrategy
}

func (s *Composite) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	for _, strategy := range s.Strategies {
		if strategy.ShouldSnapshot(streamID, version, eventType) {
			return true
		}
	}
	return false
}

func (s *Composite) Name() string {
	return "composite"
}
