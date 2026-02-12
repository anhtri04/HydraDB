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

func TestStore_ReadStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append events to multiple streams
	s.Append("alice", []byte("alice-event-0"))
	s.Append("bob", []byte("bob-event-0"))
	s.Append("alice", []byte("alice-event-1"))
	s.Append("alice", []byte("alice-event-2"))
	s.Append("bob", []byte("bob-event-1"))

	// Read alice's stream
	events, err := s.ReadStream("alice")
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Verify events are in order with correct versions
	expected := []struct {
		version int64
		data    string
	}{
		{0, "alice-event-0"},
		{1, "alice-event-1"},
		{2, "alice-event-2"},
	}

	for i, e := range events {
		if e.StreamVersion != expected[i].version {
			t.Errorf("event %d: expected version %d, got %d", i, expected[i].version, e.StreamVersion)
		}
		if string(e.Data) != expected[i].data {
			t.Errorf("event %d: expected data '%s', got '%s'", i, expected[i].data, string(e.Data))
		}
		if e.StreamID != "alice" {
			t.Errorf("event %d: expected streamID 'alice', got '%s'", i, e.StreamID)
		}
	}
}

func TestStore_ReadStreamFrom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append 5 events
	for i := 0; i < 5; i++ {
		s.Append("alice", []byte("event"))
	}

	// Read from version 2 onwards
	events, err := s.ReadStreamFrom("alice", 2)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events (versions 2,3,4), got %d", len(events))
	}

	// Verify versions
	for i, e := range events {
		expectedVersion := int64(2 + i)
		if e.StreamVersion != expectedVersion {
			t.Errorf("event %d: expected version %d, got %d", i, expectedVersion, e.StreamVersion)
		}
	}
}
