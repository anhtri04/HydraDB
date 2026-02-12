# Phase 1: Append-Only Log Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a durable append-only log that persists records to disk with corruption detection and random-access reads.

**Architecture:** A `Log` struct wraps a single file, storing records in length-prefixed binary format with CRC32 checksums. Each record is `[4-byte length][4-byte CRC32][N-byte data]`. The log supports append (returns byte offset), read-at (random access), and read-all (sequential scan with corruption handling).

**Tech Stack:** Go 1.21+, standard library only (`os`, `io`, `encoding/binary`, `hash/crc32`)

---

## Project Setup

### Task 1: Initialize Go Module

**Files:**
- Create: `go.mod`
- Create: `log/log.go` (package declaration only)
- Create: `log/log_test.go` (package declaration only)

**Step 1: Initialize the Go module**

Run:
```bash
go mod init github.com/hydra-db/hydra
```

**Step 2: Create the log package structure**

Create `log/log.go`:
```go
package log
```

Create `log/log_test.go`:
```go
package log_test
```

**Step 3: Verify module compiles**

Run: `go build ./...`
Expected: No output (success)

**Step 4: Commit**

```bash
git init
git add go.mod log/
git commit -m "chore: initialize go module with log package structure"
```

---

## Core Types and Constants

### Task 2: Define Record Header Constants

**Files:**
- Modify: `log/log.go`

**Step 1: Add constants and imports**

```go
package log

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
)

const (
	// LengthSize is the size of the record length field in bytes (uint32)
	LengthSize = 4
	// ChecksumSize is the size of the CRC32 checksum field in bytes
	ChecksumSize = 4
	// HeaderSize is the total size of the record header (length + checksum)
	HeaderSize = LengthSize + ChecksumSize
)

// byteOrder defines the byte order for encoding integers
var byteOrder = binary.BigEndian
```

**Step 2: Verify it compiles**

Run: `go build ./log`
Expected: No output (success)

**Step 3: Commit**

```bash
git add log/log.go
git commit -m "feat(log): add record header constants"
```

---

### Task 3: Define Log Struct

**Files:**
- Modify: `log/log.go`

**Step 1: Add Log struct and constructor**

Add after the constants:
```go
// Log is an append-only log that stores records in a file.
// Each record is stored as: [4-byte length][4-byte CRC32][data]
type Log struct {
	file *os.File
}

// Open opens or creates an append-only log file at the given path.
func Open(path string) (*Log, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	return &Log{file: f}, nil
}

// Close closes the log file.
func (l *Log) Close() error {
	return l.file.Close()
}
```

**Step 2: Verify it compiles**

Run: `go build ./log`
Expected: No output (success)

**Step 3: Commit**

```bash
git add log/log.go
git commit -m "feat(log): add Log struct with Open and Close"
```

---

## Append Operation

### Task 4: Write Failing Test for Append

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./log -v -run TestAppend_ReturnsPositionZeroForFirstRecord`
Expected: FAIL with "l.Append undefined"

**Step 3: Commit the failing test**

```bash
git add log/log_test.go
git commit -m "test(log): add failing test for Append position"
```

---

### Task 5: Implement Append Method

**Files:**
- Modify: `log/log.go`

**Step 1: Implement Append**

Add after Close method:
```go
// Append writes a record to the log and returns its byte position.
// The record is stored as: [4-byte length][4-byte CRC32][data]
func (l *Log) Append(data []byte) (pos int64, err error) {
	// Get current position (end of file)
	pos, err = l.file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	// Build the record: [length][checksum][data]
	length := uint32(len(data))
	checksum := crc32.ChecksumIEEE(data)

	// Write length
	if err := binary.Write(l.file, byteOrder, length); err != nil {
		return 0, err
	}

	// Write checksum
	if err := binary.Write(l.file, byteOrder, checksum); err != nil {
		return 0, err
	}

	// Write data
	if _, err := l.file.Write(data); err != nil {
		return 0, err
	}

	// Sync to disk for durability
	if err := l.file.Sync(); err != nil {
		return 0, err
	}

	return pos, nil
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./log -v -run TestAppend_ReturnsPositionZeroForFirstRecord`
Expected: PASS

**Step 3: Commit**

```bash
git add log/log.go
git commit -m "feat(log): implement Append method"
```

---

### Task 6: Test Multiple Appends Return Correct Positions

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write the failing test**

Add to `log/log_test.go`:
```go
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./log -v -run TestAppend_ReturnsCorrectPositionForMultipleRecords`
Expected: PASS (implementation already handles this)

**Step 3: Commit**

```bash
git add log/log_test.go
git commit -m "test(log): verify Append positions for multiple records"
```

---

## ReadAt Operation (Random Access)

### Task 7: Write Failing Test for ReadAt

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write the failing test**

Add to `log/log_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./log -v -run TestReadAt_ReadsRecordAtPosition`
Expected: FAIL with "l.ReadAt undefined"

**Step 3: Commit the failing test**

```bash
git add log/log_test.go
git commit -m "test(log): add failing test for ReadAt"
```

---

### Task 8: Implement ReadAt Method

**Files:**
- Modify: `log/log.go`

**Step 1: Add error variable at package level**

Add after the `byteOrder` variable:
```go
// ErrCorruptRecord indicates a record failed checksum validation
var ErrCorruptRecord = errors.New("corrupt record: checksum mismatch")
```

Update imports to include "errors":
```go
import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)
```

**Step 2: Implement ReadAt**

Add after Append method:
```go
// ReadAt reads a single record at the given byte position.
// Returns ErrCorruptRecord if the checksum doesn't match.
func (l *Log) ReadAt(pos int64) ([]byte, error) {
	// Seek to position
	if _, err := l.file.Seek(pos, io.SeekStart); err != nil {
		return nil, err
	}

	// Read length
	var length uint32
	if err := binary.Read(l.file, byteOrder, &length); err != nil {
		return nil, err
	}

	// Read stored checksum
	var storedChecksum uint32
	if err := binary.Read(l.file, byteOrder, &storedChecksum); err != nil {
		return nil, err
	}

	// Read data
	data := make([]byte, length)
	if _, err := io.ReadFull(l.file, data); err != nil {
		return nil, err
	}

	// Verify checksum
	computedChecksum := crc32.ChecksumIEEE(data)
	if computedChecksum != storedChecksum {
		return nil, ErrCorruptRecord
	}

	return data, nil
}
```

**Step 3: Run test to verify it passes**

Run: `go test ./log -v -run TestReadAt_ReadsRecordAtPosition`
Expected: PASS

**Step 4: Commit**

```bash
git add log/log.go
git commit -m "feat(log): implement ReadAt method with checksum validation"
```

---

### Task 9: Test ReadAt Detects Corruption

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write test for corruption detection**

Add to `log/log_test.go`:
```go
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

	// Corrupt the data (byte after header)
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./log -v -run TestReadAt_DetectsCorruption`
Expected: PASS

**Step 3: Commit**

```bash
git add log/log_test.go
git commit -m "test(log): verify ReadAt detects corruption"
```

---

## ReadAll Operation (Sequential Scan)

### Task 10: Define Record Type for ReadAll

**Files:**
- Modify: `log/log.go`

**Step 1: Add Record struct**

Add after the error variable:
```go
// Record represents a single entry in the log with its position.
type Record struct {
	Position int64  // Byte offset in the file
	Data     []byte // The record payload
}
```

**Step 2: Verify it compiles**

Run: `go build ./log`
Expected: No output (success)

**Step 3: Commit**

```bash
git add log/log.go
git commit -m "feat(log): add Record struct"
```

---

### Task 11: Write Failing Test for ReadAll

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write the failing test**

Add to `log/log_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./log -v -run TestReadAll_ReturnsAllRecords`
Expected: FAIL with "l.ReadAll undefined"

**Step 3: Commit the failing test**

```bash
git add log/log_test.go
git commit -m "test(log): add failing test for ReadAll"
```

---

### Task 12: Implement ReadAll Method

**Files:**
- Modify: `log/log.go`

**Step 1: Implement ReadAll**

Add after ReadAt method:
```go
// ReadAll reads all valid records from the log sequentially.
// Stops at EOF or when encountering a corrupt/incomplete record.
func (l *Log) ReadAll() ([]Record, error) {
	// Seek to start
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var records []Record
	var pos int64 = 0

	for {
		// Read length
		var length uint32
		if err := binary.Read(l.file, byteOrder, &length); err != nil {
			if err == io.EOF {
				break // Normal end of file
			}
			return records, nil // Incomplete header, stop reading
		}

		// Read stored checksum
		var storedChecksum uint32
		if err := binary.Read(l.file, byteOrder, &storedChecksum); err != nil {
			return records, nil // Incomplete header, stop reading
		}

		// Read data
		data := make([]byte, length)
		if _, err := io.ReadFull(l.file, data); err != nil {
			return records, nil // Incomplete data, stop reading
		}

		// Verify checksum
		if crc32.ChecksumIEEE(data) != storedChecksum {
			return records, nil // Corrupt record, stop reading
		}

		records = append(records, Record{
			Position: pos,
			Data:     data,
		})

		pos += int64(HeaderSize + len(data))
	}

	return records, nil
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./log -v -run TestReadAll_ReturnsAllRecords`
Expected: PASS

**Step 3: Commit**

```bash
git add log/log.go
git commit -m "feat(log): implement ReadAll method"
```

---

### Task 13: Test ReadAll Handles Trailing Corruption

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write test for corruption handling**

Add to `log/log_test.go`:
```go
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

	// Corrupt the third record
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./log -v -run TestReadAll_StopsAtCorruptRecord`
Expected: PASS

**Step 3: Commit**

```bash
git add log/log_test.go
git commit -m "test(log): verify ReadAll stops at corrupt records"
```

---

## Persistence Test

### Task 14: Test Data Survives Restart

**Files:**
- Modify: `log/log_test.go`

**Step 1: Write persistence test**

Add to `log/log_test.go`:
```go
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./log -v -run TestLog_DataSurvivesRestart`
Expected: PASS

**Step 3: Commit**

```bash
git add log/log_test.go
git commit -m "test(log): verify data survives restart"
```

---

## Final Verification

### Task 15: Run All Tests and Final Commit

**Step 1: Run all tests**

Run: `go test ./log -v`
Expected: All tests PASS

**Step 2: Check test coverage**

Run: `go test ./log -cover`
Expected: Coverage percentage displayed

**Step 3: Final commit for Phase 1**

```bash
git add -A
git commit -m "feat(log): complete Phase 1 - append-only log with checksums"
```

---

## Summary

After completing all tasks, you will have:

| Component | Status |
|-----------|--------|
| `Log.Open(path)` | Opens or creates log file |
| `Log.Close()` | Closes the file |
| `Log.Append(data)` | Writes record, returns position |
| `Log.ReadAt(pos)` | Random access read with checksum validation |
| `Log.ReadAll()` | Sequential scan, stops at corruption |
| `ErrCorruptRecord` | Error for checksum failures |
| `Record{Position, Data}` | Return type for ReadAll |

**Total records format:** `[4-byte length][4-byte CRC32][N-byte data]`

**Tests cover:**
- Basic append and position tracking
- Multiple record positioning
- Random access reads
- Corruption detection
- Graceful handling of truncated files
- Data persistence across restarts
