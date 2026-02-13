package log_test

import (
	"os"
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

func TestReadAt_ReadsRecordAtPosition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Append two records
	_, err = l.Append([]byte("first"))
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	pos2, err := l.Append([]byte("second"))
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// Read the second record
	data, err := l.ReadAt(pos2)
	if err != nil {
		t.Fatalf("failed to read at %d: %v", pos2, err)
	}

	if string(data) != "second" {
		t.Errorf("expected 'second', got '%s'", string(data))
	}
}

func TestReadAt_DetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Write a valid record
	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}

	pos, err := l.Append([]byte("hello"))
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	l.Close()

	// Corrupt the data (byte after header) - open the segment file
	segmentFile := filepath.Join(path, "hydra-00000.log")
	f, err := os.OpenFile(segmentFile, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	// Seek past header (8 bytes) and corrupt first data byte
	f.Seek(int64(log.HeaderSize), 0)
	f.Write([]byte{0xFF}) // Corrupt the data
	f.Close()

	// Reopen and try to read
	l, err = log.Open(path)
	if err != nil {
		t.Fatalf("failed to reopen log: %v", err)
	}
	defer l.Close()

	_, err = l.ReadAt(pos)
	if err != log.ErrCorruptRecord {
		t.Errorf("expected ErrCorruptRecord, got %v", err)
	}
}

func TestReadAll_ReturnsAllRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer l.Close()

	// Append multiple records
	expected := []string{"one", "two", "three"}
	for _, s := range expected {
		if _, err := l.Append([]byte(s)); err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	// Read all
	records, err := l.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all: %v", err)
	}

	if len(records) != len(expected) {
		t.Fatalf("expected %d records, got %d", len(expected), len(records))
	}

	for i, r := range records {
		if string(r.Data) != expected[i] {
			t.Errorf("record %d: expected '%s', got '%s'", i, expected[i], string(r.Data))
		}
	}
}

func TestReadAll_StopsAtCorruptRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}

	// Append three records
	l.Append([]byte("valid1"))
	l.Append([]byte("valid2"))
	pos3, _ := l.Append([]byte("valid3"))
	l.Close()

	// Corrupt the third record - open the segment file
	segmentFile := filepath.Join(path, "hydra-00000.log")
	f, err := os.OpenFile(segmentFile, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	f.Seek(pos3+int64(log.HeaderSize), 0)
	f.Write([]byte{0xFF})
	f.Close()

	// Reopen and read all
	l, err = log.Open(path)
	if err != nil {
		t.Fatalf("failed to reopen log: %v", err)
	}
	defer l.Close()

	records, err := l.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only return the first two valid records
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
}

func TestLog_DataSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Write some records
	l, err := log.Open(path)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}

	expected := []string{"event1", "event2", "event3"}
	positions := make([]int64, len(expected))
	for i, s := range expected {
		pos, err := l.Append([]byte(s))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
		positions[i] = pos
	}
	l.Close()

	// Reopen the log (simulating restart)
	l, err = log.Open(path)
	if err != nil {
		t.Fatalf("failed to reopen log: %v", err)
	}
	defer l.Close()

	// Verify all records via ReadAt
	for i, pos := range positions {
		data, err := l.ReadAt(pos)
		if err != nil {
			t.Fatalf("failed to read at %d: %v", pos, err)
		}
		if string(data) != expected[i] {
			t.Errorf("position %d: expected '%s', got '%s'", pos, expected[i], string(data))
		}
	}

	// Verify via ReadAll
	records, err := l.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all: %v", err)
	}

	if len(records) != len(expected) {
		t.Fatalf("expected %d records, got %d", len(expected), len(records))
	}
}
