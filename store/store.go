package store

import (
	"sync"

	"github.com/hydra-db/hydra/log"
	"github.com/hydra-db/hydra/pubsub"
)

// StreamDeletedType is the event type marker for stream deletion tombstones
const StreamDeletedType = "$stream-deleted"

// Store wraps a Log and adds stream indexing with thread-safe access.
type Store struct {
	mu          sync.RWMutex
	log         *log.Log
	index       map[string][]int64             // StreamID -> list of positions
	eventIDs    map[string]map[string]struct{} // StreamID -> set of eventIDs (for dedup)
	broadcaster *pubsub.Broadcaster
	deleted     map[string]bool // tracks deleted streams
	snapshots   *SnapshotStore
}

// Open opens or creates a store at the given path.
// It rebuilds the index by scanning the existing log.
func Open(path string) (*Store, error) {
	l, err := log.Open(path)
	if err != nil {
		return nil, err
	}

	s := &Store{
		log:      l,
		index:    make(map[string][]int64),
		eventIDs: make(map[string]map[string]struct{}),
		deleted:  make(map[string]bool),
	}

	// Rebuild index from existing data
	if err := s.rebuildIndex(); err != nil {
		l.Close()
		return nil, err
	}

	return s, nil
}

// rebuildIndex scans the log and populates the index and eventIDs.
func (s *Store) rebuildIndex() error {
	records, err := s.log.ReadAll()
	if err != nil {
		return err
	}

	for _, record := range records {
		streamID, eventID, data, err := deserializeEnvelope(record.Data)
		if err != nil {
			// Skip invalid records
			continue
		}

		// Check for tombstone
		if string(data) == StreamDeletedType {
			s.deleted[streamID] = true
			continue
		}

		s.index[streamID] = append(s.index[streamID], record.Position)

		// Track eventID for deduplication
		if eventID != "" {
			if s.eventIDs[streamID] == nil {
				s.eventIDs[streamID] = make(map[string]struct{})
			}
			s.eventIDs[streamID][eventID] = struct{}{}
		}
	}

	return nil
}

// Close closes the underlying log.
func (s *Store) Close() error {
	return s.log.Close()
}

// DeleteStream marks a stream as deleted (soft delete)
// The stream data remains in the log but is hidden from reads
func (s *Store) DeleteStream(streamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deleted[streamID] {
		return nil // Already deleted
	}

	// Write tombstone event
	tombstone := serializeEnvelope(streamID, "", []byte(StreamDeletedType))
	_, err := s.log.Append(tombstone)
	if err != nil {
		return err
	}

	s.deleted[streamID] = true

	// Clean up snapshot if exists
	if s.snapshots != nil {
		s.snapshots.Delete(streamID)
	}

	return nil
}

// IsDeleted returns true if a stream has been deleted
func (s *Store) IsDeleted(streamID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deleted[streamID]
}

// SetBroadcaster sets the broadcaster for publishing events
func (s *Store) SetBroadcaster(b *pubsub.Broadcaster) {
	s.broadcaster = b
}

// SetSnapshotStore sets the snapshot store for this store
func (s *Store) SetSnapshotStore(ss *SnapshotStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = ss
}

// Snapshots returns the snapshot store (may be nil)
func (s *Store) Snapshots() *SnapshotStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshots
}

// Append adds an event to a stream with optimistic concurrency control.
// Returns ErrWrongExpectedVersion if the expected version doesn't match.
// If eventID already exists in the stream, returns success without writing (idempotent).
func (s *Store) Append(streamID, eventID string, data []byte, expectedVersion int64) (AppendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentVersion := int64(len(s.index[streamID]))

	// Check for idempotency - if eventID already seen, return success
	if eventID != "" {
		if ids, exists := s.eventIDs[streamID]; exists {
			if _, seen := ids[eventID]; seen {
				// Event already written, return current state (idempotent)
				return AppendResult{
					Position: -1, // Indicate no new write
					Version:  currentVersion - 1,
				}, nil
			}
		}
	}

	// Check expected version
	switch expectedVersion {
	case ExpectedVersionAny:
		// Always allowed
	case ExpectedVersionNoStream:
		if currentVersion > 0 {
			return AppendResult{}, ErrStreamExists
		}
	case ExpectedVersionStreamExists:
		if currentVersion == 0 {
			return AppendResult{}, ErrStreamNotFound
		}
	default:
		if currentVersion != expectedVersion {
			return AppendResult{}, ErrWrongExpectedVersion
		}
	}

	// Wrap data with StreamID and EventID
	envelope := serializeEnvelope(streamID, eventID, data)

	// Write to log
	pos, err := s.log.Append(envelope)
	if err != nil {
		return AppendResult{}, err
	}

	// Update index
	s.index[streamID] = append(s.index[streamID], pos)
	newVersion := int64(len(s.index[streamID])) - 1 // 0-based version

	// Track eventID for deduplication
	if eventID != "" {
		if s.eventIDs[streamID] == nil {
			s.eventIDs[streamID] = make(map[string]struct{})
		}
		s.eventIDs[streamID][eventID] = struct{}{}
	}

	// Notify subscribers
	if s.broadcaster != nil {
		s.broadcaster.Publish(pubsub.Event{
			GlobalPosition: pos,
			StreamID:       streamID,
			StreamVersion:  newVersion,
			Data:           data,
		})
	}

	return AppendResult{
		Position: pos,
		Version:  newVersion,
	}, nil
}

// StreamVersion returns the current version (event count) for a stream.
// Returns 0 if the stream doesn't exist.
func (s *Store) StreamVersion(streamID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.index[streamID]))
}

// ReadStream returns all events for a stream in version order.
func (s *Store) ReadStream(streamID string) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if stream is deleted
	if s.deleted[streamID] {
		return nil, nil // Return empty for deleted streams
	}

	positions, exists := s.index[streamID]
	if !exists {
		return nil, nil // Stream doesn't exist, return empty
	}

	events := make([]Event, 0, len(positions))
	for i, pos := range positions {
		raw, err := s.log.ReadAt(pos)
		if err != nil {
			return nil, err
		}

		sid, _, data, err := deserializeEnvelope(raw)
		if err != nil {
			return nil, err
		}

		events = append(events, Event{
			GlobalPosition: pos,
			StreamID:       sid,
			StreamVersion:  int64(i),
			Data:           data,
		})
	}

	return events, nil
}

// ReadStreamFrom returns events for a stream starting from the given version.
func (s *Store) ReadStreamFrom(streamID string, fromVersion int64) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if stream is deleted
	if s.deleted[streamID] {
		return nil, nil // Return empty for deleted streams
	}

	positions, exists := s.index[streamID]
	if !exists {
		return nil, nil
	}

	if fromVersion < 0 {
		fromVersion = 0
	}
	if fromVersion >= int64(len(positions)) {
		return nil, nil // No events from this version
	}

	events := make([]Event, 0, len(positions)-int(fromVersion))
	for i := int(fromVersion); i < len(positions); i++ {
		pos := positions[i]
		raw, err := s.log.ReadAt(pos)
		if err != nil {
			return nil, err
		}

		sid, _, data, err := deserializeEnvelope(raw)
		if err != nil {
			return nil, err
		}

		events = append(events, Event{
			GlobalPosition: pos,
			StreamID:       sid,
			StreamVersion:  int64(i),
			Data:           data,
		})
	}

	return events, nil
}

// GetLog returns the underlying log (for scavenging)
func (s *Store) GetLog() *log.Log {
	return s.log
}

// ReadAll returns all events in global (insertion) order.
func (s *Store) ReadAll() ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records, err := s.log.ReadAll()
	if err != nil {
		return nil, err
	}

	// We need to track stream versions as we iterate
	streamVersions := make(map[string]int64)

	events := make([]Event, 0, len(records))
	for _, record := range records {
		sid, _, data, err := deserializeEnvelope(record.Data)
		if err != nil {
			continue // Skip invalid records
		}

		version := streamVersions[sid]
		streamVersions[sid]++

		events = append(events, Event{
			GlobalPosition: record.Position,
			StreamID:       sid,
			StreamVersion:  version,
			Data:           data,
		})
	}

	return events, nil
}
