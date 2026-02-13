package store_test

import (
	"fmt"
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
	_, _ = s.Append("alice", "e1", []byte("event1"), store.ExpectedVersionAny)
	_, _ = s.Append("alice", "e2", []byte("event2"), store.ExpectedVersionAny)
	_, _ = s.Append("bob", "e3", []byte("event3"), store.ExpectedVersionAny)
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
	_, _ = s.Append("alice", "e1", []byte("alice-event-0"), store.ExpectedVersionAny)
	_, _ = s.Append("bob", "e2", []byte("bob-event-0"), store.ExpectedVersionAny)
	_, _ = s.Append("alice", "e3", []byte("alice-event-1"), store.ExpectedVersionAny)
	_, _ = s.Append("alice", "e4", []byte("alice-event-2"), store.ExpectedVersionAny)
	_, _ = s.Append("bob", "e5", []byte("bob-event-1"), store.ExpectedVersionAny)

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
		_, _ = s.Append("alice", fmt.Sprintf("e%d", i), []byte("event"), store.ExpectedVersionAny)
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

func TestStore_ReadAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append events to multiple streams
	_, _ = s.Append("alice", "e1", []byte("a1"), store.ExpectedVersionAny)
	_, _ = s.Append("bob", "e2", []byte("b1"), store.ExpectedVersionAny)
	_, _ = s.Append("alice", "e3", []byte("a2"), store.ExpectedVersionAny)

	// Read all events in global order
	events, err := s.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Verify global order (by position)
	expected := []struct {
		streamID string
		data     string
	}{
		{"alice", "a1"},
		{"bob", "b1"},
		{"alice", "a2"},
	}

	for i, e := range events {
		if e.StreamID != expected[i].streamID {
			t.Errorf("event %d: expected streamID '%s', got '%s'", i, expected[i].streamID, e.StreamID)
		}
		if string(e.Data) != expected[i].data {
			t.Errorf("event %d: expected data '%s', got '%s'", i, expected[i].data, string(e.Data))
		}
	}
}

func TestAppend_RejectsWrongExpectedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append first event (version becomes 1)
	_, err = s.Append("alice", "event-1", []byte("data1"), store.ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// Try to append with wrong expected version (expecting 5, but it's 1)
	_, err = s.Append("alice", "event-2", []byte("data2"), 5)
	if err != store.ErrWrongExpectedVersion {
		t.Errorf("expected ErrWrongExpectedVersion, got %v", err)
	}

	// Append with correct expected version should work
	result, err := s.Append("alice", "event-2", []byte("data2"), 1)
	if err != nil {
		t.Fatalf("failed to append with correct version: %v", err)
	}
	if result.Version != 1 { // 0-indexed, so second event is version 1
		t.Errorf("expected version 1, got %d", result.Version)
	}
}

func TestAppend_ExpectedVersionAnyAlwaysSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append with Any to non-existent stream
	_, err = s.Append("alice", "e1", []byte("data1"), store.ExpectedVersionAny)
	if err != nil {
		t.Fatalf("expected success with Any on new stream, got: %v", err)
	}

	// Append with Any to existing stream
	_, err = s.Append("alice", "e2", []byte("data2"), store.ExpectedVersionAny)
	if err != nil {
		t.Fatalf("expected success with Any on existing stream, got: %v", err)
	}

	if v := s.StreamVersion("alice"); v != 2 {
		t.Errorf("expected version 2, got %d", v)
	}
}

func TestAppend_ExpectedVersionNoStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// NoStream on new stream should succeed
	_, err = s.Append("alice", "e1", []byte("data"), store.ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("expected success on new stream, got: %v", err)
	}

	// NoStream on existing stream should fail
	_, err = s.Append("alice", "e2", []byte("data"), store.ExpectedVersionNoStream)
	if err != store.ErrStreamExists {
		t.Errorf("expected ErrStreamExists, got: %v", err)
	}
}

func TestAppend_ExpectedVersionStreamExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// StreamExists on new stream should fail
	_, err = s.Append("alice", "e1", []byte("data"), store.ExpectedVersionStreamExists)
	if err != store.ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound, got: %v", err)
	}

	// Create the stream
	_, _ = s.Append("alice", "e1", []byte("data"), store.ExpectedVersionAny)

	// StreamExists on existing stream should succeed
	_, err = s.Append("alice", "e2", []byte("data"), store.ExpectedVersionStreamExists)
	if err != nil {
		t.Fatalf("expected success on existing stream, got: %v", err)
	}
}

func TestAppend_IdempotentWithSameEventID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// First append
	result1, err := s.Append("alice", "unique-event-id", []byte("data"), store.ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("first append failed: %v", err)
	}

	// Second append with SAME eventID should succeed but not write
	result2, err := s.Append("alice", "unique-event-id", []byte("data"), store.ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("idempotent append should succeed, got: %v", err)
	}

	// Position should be -1 indicating no new write
	if result2.Position != -1 {
		t.Errorf("expected position -1 for idempotent write, got %d", result2.Position)
	}

	// Version should still be 1
	if s.StreamVersion("alice") != 1 {
		t.Errorf("expected version 1, got %d", s.StreamVersion("alice"))
	}

	// Only one event should exist
	events, _ := s.ReadStream("alice")
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}

	// Different eventID should write
	result3, err := s.Append("alice", "different-event-id", []byte("data2"), 1)
	if err != nil {
		t.Fatalf("different eventID should succeed: %v", err)
	}
	if result3.Position == -1 {
		t.Error("expected new position for different eventID")
	}

	// Now should have 2 events
	if s.StreamVersion("alice") != 2 {
		t.Errorf("expected version 2, got %d", s.StreamVersion("alice"))
	}

	// Suppress unused variable warning
	_ = result1
}

func TestAppend_IdempotencySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Open and write
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	_, err = s.Append("alice", "persistent-id", []byte("data"), store.ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("first append failed: %v", err)
	}
	s.Close()

	// Reopen (simulating restart)
	s, err = store.Open(path)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer s.Close()

	// Retry with same eventID should be idempotent
	result, err := s.Append("alice", "persistent-id", []byte("data"), store.ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("idempotent append after restart should succeed, got: %v", err)
	}

	if result.Position != -1 {
		t.Errorf("expected position -1 for idempotent write, got %d", result.Position)
	}

	// Still only 1 event
	if s.StreamVersion("alice") != 1 {
		t.Errorf("expected version 1, got %d", s.StreamVersion("alice"))
	}
}

func TestAppend_ConcurrentWithExpectedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Create initial event
	_, _ = s.Append("alice", "init", []byte("init"), store.ExpectedVersionNoStream)

	// Launch 10 goroutines all trying to append with expectedVersion=1
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			eventID := fmt.Sprintf("event-%d", id)
			_, err := s.Append("alice", eventID, []byte("data"), 1) // All expect version 1
			results <- err
		}(i)
	}

	// Collect results
	successCount := 0
	wrongVersionCount := 0
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else if err == store.ErrWrongExpectedVersion {
			wrongVersionCount++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// Exactly one should succeed (the first one to acquire the lock)
	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}

	// The rest should get wrong version error
	if wrongVersionCount != numGoroutines-1 {
		t.Errorf("expected %d wrong version errors, got %d", numGoroutines-1, wrongVersionCount)
	}

	// Final version should be 2 (init + 1 successful append)
	if v := s.StreamVersion("alice"); v != 2 {
		t.Errorf("expected version 2, got %d", v)
	}
}

func TestStore_DeleteStream(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Create stream with events
	s.Append("user-1", "e1", []byte("event1"), store.ExpectedVersionAny)
	s.Append("user-1", "e2", []byte("event2"), store.ExpectedVersionAny)

	// Verify we can read it
	events, _ := s.ReadStream("user-1")
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	// Delete the stream
	err = s.DeleteStream("user-1")
	if err != nil {
		t.Fatalf("failed to delete stream: %v", err)
	}

	// Should return empty now
	events, _ = s.ReadStream("user-1")
	if len(events) != 0 {
		t.Errorf("expected 0 events after delete, got %d", len(events))
	}

	// Should be marked as deleted
	if !s.IsDeleted("user-1") {
		t.Error("stream should be marked as deleted")
	}
}

func TestStore_DeleteStreamIdempotent(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	s.Append("user-1", "e1", []byte("event1"), store.ExpectedVersionAny)

	// Delete twice should not error
	err = s.DeleteStream("user-1")
	if err != nil {
		t.Fatalf("first delete failed: %v", err)
	}

	err = s.DeleteStream("user-1")
	if err != nil {
		t.Fatalf("second delete should not fail: %v", err)
	}
}

func TestStore_DeleteSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// Create and delete stream
	func() {
		s, _ := store.Open(dir)
		s.Append("user-1", "e1", []byte("event1"), store.ExpectedVersionAny)
		s.DeleteStream("user-1")
		s.Close()
	}()

	// Reopen and verify still deleted
	s, _ := store.Open(dir)
	defer s.Close()

	if !s.IsDeleted("user-1") {
		t.Error("deletion should survive restart")
	}

	events, _ := s.ReadStream("user-1")
	if len(events) != 0 {
		t.Error("deleted stream should return empty")
	}
}

func TestStore_DeletedStreamNotInReadAll(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Create two streams
	s.Append("user-1", "e1", []byte("user1-event"), store.ExpectedVersionAny)
	s.Append("user-2", "e2", []byte("user2-event"), store.ExpectedVersionAny)

	// Delete one stream
	s.DeleteStream("user-1")

	// ReadAll should still show all events (global log is immutable)
	// But ReadStream for deleted stream returns empty
	events, _ := s.ReadStream("user-1")
	if len(events) != 0 {
		t.Error("deleted stream should return empty from ReadStream")
	}

	events, _ = s.ReadStream("user-2")
	if len(events) != 1 {
		t.Error("non-deleted stream should still be readable")
	}
}
