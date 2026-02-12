package store_test

import (
	"path/filepath"
	"testing"

	"github.com/hydra-db/hydra/store"
)

func TestStore_RebuildIndexOnOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Open store and append some events
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	// Append events to different streams
	s.Append("alice", []byte("event1"))
	s.Append("alice", []byte("event2"))
	s.Append("bob", []byte("event3"))
	s.Close()

	// Reopen - should rebuild index
	s, err = store.Open(path)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer s.Close()

	// Verify index was rebuilt by checking stream versions
	if v := s.StreamVersion("alice"); v != 2 {
		t.Errorf("expected alice version 2, got %d", v)
	}
	if v := s.StreamVersion("bob"); v != 1 {
		t.Errorf("expected bob version 1, got %d", v)
	}
	if v := s.StreamVersion("charlie"); v != 0 {
		t.Errorf("expected charlie version 0, got %d", v)
	}
}
