package log_test

import (
	"path/filepath"
	"testing"

	"github.com/hydra-db/hydra/log"
)

func TestAppend_ReturnsPositionZeroForFirstRecord(t *testing.T) {
	// Setup: create temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Open log
	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Append a record
	data := []byte("hello")
	pos, err := l.Append(data)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// First record should be at position 0
	if pos != 0 {
		t.Errorf("expected position 0, got %d", pos)
	}
}

func TestAppend_ReturnsCorrectPositionForMultipleRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Append first record: "hello" (5 bytes)
	// Record size: 4 (length) + 4 (checksum) + 5 (data) = 13 bytes
	data1 := []byte("hello")
	pos1, err := l.Append(data1)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// Append second record: "world" (5 bytes)
	data2 := []byte("world")
	pos2, err := l.Append(data2)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// First at 0, second at 13 (8 header + 5 data)
	if pos1 != 0 {
		t.Errorf("expected pos1=0, got %d", pos1)
	}
	expectedPos2 := int64(log.HeaderSize + len(data1)) // 8 + 5 = 13
	if pos2 != expectedPos2 {
		t.Errorf("expected pos2=%d, got %d", expectedPos2, pos2)
	}
}
