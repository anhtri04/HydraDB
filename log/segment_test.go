package log_test

import (
	"testing"

	"github.com/hydra-db/hydra/log"
)

func TestSegment_AppendAndRead(t *testing.T) {
	dir := t.TempDir()

	seg, err := log.OpenSegment(dir, 0, 0)
	if err != nil {
		t.Fatalf("failed to open segment: %v", err)
	}
	defer seg.CloseFile()

	// Append
	data := []byte("hello world")
	pos, err := seg.Append(data)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if pos != 0 {
		t.Errorf("expected position 0, got %d", pos)
	}

	// Read back
	got, err := seg.ReadAt(0)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(got))
	}
}

func TestSegment_ReadAll(t *testing.T) {
	dir := t.TempDir()

	seg, err := log.OpenSegment(dir, 0, 0)
	if err != nil {
		t.Fatalf("failed to open segment: %v", err)
	}
	defer seg.CloseFile()

	// Append multiple records
	seg.Append([]byte("one"))
	seg.Append([]byte("two"))
	seg.Append([]byte("three"))

	records, err := seg.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all: %v", err)
	}

	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
}

func TestSegment_Closed(t *testing.T) {
	dir := t.TempDir()

	seg, err := log.OpenSegment(dir, 0, 0)
	if err != nil {
		t.Fatalf("failed to open segment: %v", err)
	}
	defer seg.CloseFile()

	seg.Append([]byte("before close"))
	seg.MarkClosed()

	// Should fail to append after close
	_, err = seg.Append([]byte("after close"))
	if err == nil {
		t.Error("expected error appending to closed segment")
	}

	// Should still be able to read
	records, _ := seg.ReadAll()
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
}

func TestLog_SegmentRotation(t *testing.T) {
	dir := t.TempDir()

	// Use small segment size for testing (1KB)
	l, err := log.OpenWithOptions(dir, 1024)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Write enough data to trigger rotation
	data := make([]byte, 500) // 500 bytes + 8 byte header = 508 bytes
	for i := 0; i < 5; i++ {
		_, err := l.Append(data)
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	// Should have multiple segments
	segments := l.Segments()
	if len(segments) < 2 {
		t.Errorf("expected at least 2 segments, got %d", len(segments))
	}
}

func TestLog_ReadAcrossSegments(t *testing.T) {
	dir := t.TempDir()

	l, err := log.OpenWithOptions(dir, 100) // Tiny segments
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Write data to create multiple segments
	var positions []int64
	for i := 0; i < 10; i++ {
		pos, _ := l.Append([]byte("test data"))
		positions = append(positions, pos)
	}

	// Read all records
	records, err := l.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all: %v", err)
	}

	if len(records) != 10 {
		t.Errorf("expected 10 records, got %d", len(records))
	}

	// Read individual records by position
	for i, pos := range positions {
		data, err := l.ReadAt(pos)
		if err != nil {
			t.Errorf("record %d: failed to read at %d: %v", i, pos, err)
		}
		if string(data) != "test data" {
			t.Errorf("record %d: wrong data", i)
		}
	}
}

func TestLog_SegmentSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// Write some data
	func() {
		l, _ := log.OpenWithOptions(dir, 100)
		l.Append([]byte("one"))
		l.Append([]byte("two"))
		l.Append([]byte("three"))
		l.Close()
	}()

	// Reopen and verify
	l, err := log.OpenWithOptions(dir, 100)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer l.Close()

	records, _ := l.ReadAll()
	if len(records) != 3 {
		t.Errorf("expected 3 records after restart, got %d", len(records))
	}
}
