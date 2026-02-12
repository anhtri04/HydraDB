package store

import (
	"github.com/hydra-db/hydra/log"
)

// Store wraps a Log and adds stream indexing.
type Store struct {
	log   *log.Log
	index map[string][]int64 // StreamID -> list of positions
}

// Open opens or creates a store at the given path.
// It rebuilds the index by scanning the existing log.
func Open(path string) (*Store, error) {
	l, err := log.Open(path)
	if err != nil {
		return nil, err
	}

	s := &Store{
		log:   l,
		index: make(map[string][]int64),
	}

	// Rebuild index from existing data
	if err := s.rebuildIndex(); err != nil {
		l.Close()
		return nil, err
	}

	return s, nil
}

// rebuildIndex scans the log and populates the index.
func (s *Store) rebuildIndex() error {
	records, err := s.log.ReadAll()
	if err != nil {
		return err
	}

	for _, record := range records {
		streamID, _, err := deserializeEnvelope(record.Data)
		if err != nil {
			// Skip invalid records
			continue
		}
		s.index[streamID] = append(s.index[streamID], record.Position)
	}

	return nil
}

// Close closes the underlying log.
func (s *Store) Close() error {
	return s.log.Close()
}

// Append adds an event to a stream and returns its position and version.
func (s *Store) Append(streamID string, data []byte) (pos int64, version int64, err error) {
	// Wrap data with StreamID
	envelope := serializeEnvelope(streamID, data)

	// Write to log
	pos, err = s.log.Append(envelope)
	if err != nil {
		return 0, 0, err
	}

	// Update index
	s.index[streamID] = append(s.index[streamID], pos)
	version = int64(len(s.index[streamID])) // Version is 1-based count, or use 0-based

	return pos, version - 1, nil // Return 0-based version
}

// StreamVersion returns the current version (event count) for a stream.
// Returns 0 if the stream doesn't exist.
func (s *Store) StreamVersion(streamID string) int64 {
	return int64(len(s.index[streamID]))
}

// ReadStream returns all events for a stream in version order.
func (s *Store) ReadStream(streamID string) ([]Event, error) {
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

		sid, data, err := deserializeEnvelope(raw)
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

		sid, data, err := deserializeEnvelope(raw)
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

// ReadAll returns all events in global (insertion) order.
func (s *Store) ReadAll() ([]Event, error) {
	records, err := s.log.ReadAll()
	if err != nil {
		return nil, err
	}

	// We need to track stream versions as we iterate
	streamVersions := make(map[string]int64)

	events := make([]Event, 0, len(records))
	for _, record := range records {
		sid, data, err := deserializeEnvelope(record.Data)
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
