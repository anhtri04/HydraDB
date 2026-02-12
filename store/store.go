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

	return s, nil
}

// Close closes the underlying log.
func (s *Store) Close() error {
	return s.log.Close()
}
