package store

import (
	"sync"
	"time"

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

	// durability config
	durability DurabilityConfig
	flusher    *AsyncFlusher
}

// Option configures a Store
type Option func(*Store) error

// WithDurability sets the durability configuration
func WithDurability(config DurabilityConfig) Option {
	return func(s *Store) error {
		s.durability = config
		return nil
	}
}

// Open opens or creates a store at the given path.
// It rebuilds the index by scanning the existing log.
func Open(path string, opts ...Option) (*Store, error) {
	l, err := log.Open(path)
	if err != nil {
		return nil, err
	}

	s := &Store{
		log:        l,
		index:      make(map[string][]int64),
		eventIDs:   make(map[string]map[string]struct{}),
		deleted:    make(map[string]bool),
		durability: DefaultDurabilityConfig(),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(s); err != nil {
			l.Close()
			return nil, err
		}
	}

	// Rebuild index from existing data
	if err := s.rebuildIndex(); err != nil {
		l.Close()
		return nil, err
	}

	// Initialize async flusher if needed
	if s.durability.SyncMode == SyncAsync || s.durability.SyncMode == SyncEverySecond {
		if s.durability.SyncMode == SyncEverySecond {
			s.durability.SyncInterval = time.Second
		}
		s.flusher = NewAsyncFlusher(s.durability, func() error {
			return s.log.Flush()
		})
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
	if s.flusher != nil {
		if err := s.flusher.Stop(); err != nil {
			return err
		}
	}
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
	result, flushDone, err := s.appendInternal(streamID, eventID, data, expectedVersion)
	if err != nil {
		return AppendResult{}, err
	}

	// Wait for flush outside the lock (async mode)
	if flushDone != nil {
		<-flushDone
	}

	return result, nil
}

// appendInternal performs the locked portion of Append.
// Returns a channel that will be closed when data is flushed (if async mode).
func (s *Store) appendInternal(streamID, eventID string, data []byte, expectedVersion int64) (AppendResult, <-chan struct{}, error) {
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
				}, nil, nil
			}
		}
	}

	// Check expected version
	switch expectedVersion {
	case ExpectedVersionAny:
		// Always allowed
	case ExpectedVersionNoStream:
		if currentVersion > 0 {
			return AppendResult{}, nil, ErrStreamExists
		}
	case ExpectedVersionStreamExists:
		if currentVersion == 0 {
			return AppendResult{}, nil, ErrStreamNotFound
		}
	default:
		if currentVersion != expectedVersion {
			return AppendResult{}, nil, ErrWrongExpectedVersion
		}
	}

	// Wrap data with StreamID and EventID
	envelope := serializeEnvelope(streamID, eventID, data)

	// Write to log
	pos, err := s.log.Append(envelope)
	if err != nil {
		return AppendResult{}, nil, err
	}

	// Update index
	s.index[streamID] = append(s.index[streamID], pos)
	newVersion := int64(len(s.index[streamID])) - 1

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

	result := AppendResult{
		Position: pos,
		Version:  newVersion,
	}

	// Queue for async flush if enabled
	if s.flusher != nil {
		return result, s.flusher.Queue(), nil
	}

	return result, nil, nil
}

// AppendSync adds an event and forces immediate fsync to disk.
// Use this for critical writes that must survive a crash.
func (s *Store) AppendSync(streamID, eventID string, data []byte, expectedVersion int64) (AppendResult, error) {
	result, _, err := s.appendInternal(streamID, eventID, data, expectedVersion)
	if err != nil {
		return AppendResult{}, err
	}

	// Force immediate sync
	if err := s.log.Flush(); err != nil {
		return result, err
	}

	return result, nil
}

// Flush forces an immediate sync of all pending writes to disk.
// Returns nil if sync mode is SyncEveryWrite (nothing to flush).
func (s *Store) Flush() error {
	if s.flusher != nil {
		return s.flusher.Flush()
	}
	return s.log.Flush()
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
