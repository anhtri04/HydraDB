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
