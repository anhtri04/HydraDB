# Phase 2: Stream Index Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an in-memory index that maps StreamIDs to event positions, enabling O(1) stream lookups instead of O(n) full scans.

**Architecture:** A `Store` struct wraps the existing `Log` and adds an in-memory hash map (`map[string][]int64`) indexing StreamID to byte positions. On startup, the index is rebuilt by scanning the log. On append, the index is updated in-place. Events now include StreamID and StreamVersion metadata.

**Tech Stack:** Go 1.21+, standard library only (reuses existing `log` package)

---

## Project Structure

After Phase 2:
```
hydra-db/
├── log/
│   ├── log.go           # Phase 1: Append-only log (unchanged)
│   └── log_test.go
├── store/
│   ├── store.go         # NEW: Store with index
│   ├── store_test.go    # NEW: Store tests
│   └── event.go         # NEW: Event wrapper with StreamID/Version
└── examples/bank/       # Update to use Store
```

---

## Core Types

### Task 1: Create Store Package and Event Type

**Files:**
- Create: `store/event.go`

**Step 1: Create the event wrapper type**

```go
package store

// Event represents a stored event with its metadata.
type Event struct {
	GlobalPosition int64  // Byte offset in the log file
	StreamID       string // Which stream this event belongs to
	StreamVersion  int64  // Version within the stream (0, 1, 2...)
	Data           []byte // The event payload
}
```

**Step 2: Verify it compiles**

Run: `go build ./store`
Expected: No output (success)

**Step 3: Commit**

```bash
git add store/
git commit -m "feat(store): add Event type with stream metadata"
```

---

### Task 2: Define Store Struct and Constructor

**Files:**
- Create: `store/store.go`

**Step 1: Add Store struct with index**

```go
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
```

**Step 2: Verify it compiles**

Run: `go build ./store`
Expected: No output (success)

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): add Store struct with Open and Close"
```

---

## Index Rebuild

### Task 3: Add Stored Event Format

We need to store StreamID with each event. We'll wrap the user's data in an envelope.

**Files:**
- Modify: `store/event.go`

**Step 1: Add envelope type and serialization**

Add to `store/event.go`:
```go
import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	// ErrInvalidEvent indicates the event data is malformed
	ErrInvalidEvent = errors.New("invalid event format")
)

// envelope is the on-disk format: [streamID length][streamID bytes][data]
// This allows us to extract StreamID without parsing the user's data.

// serializeEnvelope wraps data with StreamID for storage.
func serializeEnvelope(streamID string, data []byte) []byte {
	streamIDBytes := []byte(streamID)
	streamIDLen := uint16(len(streamIDBytes))

	// Format: [2 bytes streamID length][streamID bytes][data bytes]
	buf := make([]byte, 2+len(streamIDBytes)+len(data))
	binary.BigEndian.PutUint16(buf[0:2], streamIDLen)
	copy(buf[2:2+len(streamIDBytes)], streamIDBytes)
	copy(buf[2+len(streamIDBytes):], data)

	return buf
}

// deserializeEnvelope extracts StreamID and data from stored bytes.
func deserializeEnvelope(raw []byte) (streamID string, data []byte, err error) {
	if len(raw) < 2 {
		return "", nil, ErrInvalidEvent
	}

	streamIDLen := binary.BigEndian.Uint16(raw[0:2])
	if len(raw) < 2+int(streamIDLen) {
		return "", nil, ErrInvalidEvent
	}

	streamID = string(raw[2 : 2+streamIDLen])
	data = raw[2+streamIDLen:]

	return streamID, data, nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./store`
Expected: No output (success)

**Step 3: Commit**

```bash
git add store/event.go
git commit -m "feat(store): add event envelope serialization"
```

---

### Task 4: Write Failing Test for Index Rebuild

**Files:**
- Create: `store/store_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./store -v -run TestStore_RebuildIndexOnOpen`
Expected: FAIL with "s.Append undefined" or "s.StreamVersion undefined"

**Step 3: Commit the failing test**

```bash
git add store/store_test.go
git commit -m "test(store): add failing test for index rebuild"
```

---

### Task 5: Implement Append Method

**Files:**
- Modify: `store/store.go`

**Step 1: Implement Append**

Add to `store/store.go`:
```go
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
```

**Step 2: Verify it compiles**

Run: `go build ./store`
Expected: No output (success)

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): implement Append method"
```

---

### Task 6: Implement StreamVersion Method

**Files:**
- Modify: `store/store.go`

**Step 1: Implement StreamVersion**

Add to `store/store.go`:
```go
// StreamVersion returns the current version (event count) for a stream.
// Returns 0 if the stream doesn't exist.
func (s *Store) StreamVersion(streamID string) int64 {
	return int64(len(s.index[streamID]))
}
```

**Step 2: Verify it compiles**

Run: `go build ./store`
Expected: No output (success)

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): implement StreamVersion method"
```

---

### Task 7: Implement Index Rebuild in Open

**Files:**
- Modify: `store/store.go`

**Step 1: Add rebuildIndex method and call it from Open**

Add to `store/store.go`:
```go
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
```

Update the `Open` function to call `rebuildIndex`:
```go
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./store -v -run TestStore_RebuildIndexOnOpen`
Expected: PASS

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): rebuild index on Open"
```

---

## Stream Reading

### Task 8: Write Failing Test for ReadStream

**Files:**
- Modify: `store/store_test.go`

**Step 1: Write the failing test**

Add to `store/store_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./store -v -run TestStore_ReadStream`
Expected: FAIL with "s.ReadStream undefined"

**Step 3: Commit the failing test**

```bash
git add store/store_test.go
git commit -m "test(store): add failing test for ReadStream"
```

---

### Task 9: Implement ReadStream Method

**Files:**
- Modify: `store/store.go`

**Step 1: Implement ReadStream**

Add to `store/store.go`:
```go
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./store -v -run TestStore_ReadStream`
Expected: PASS

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): implement ReadStream method"
```

---

### Task 10: Write Test for ReadStreamFrom (Partial Read)

**Files:**
- Modify: `store/store_test.go`

**Step 1: Write the test**

Add to `store/store_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./store -v -run TestStore_ReadStreamFrom`
Expected: FAIL with "s.ReadStreamFrom undefined"

**Step 3: Commit the failing test**

```bash
git add store/store_test.go
git commit -m "test(store): add failing test for ReadStreamFrom"
```

---

### Task 11: Implement ReadStreamFrom Method

**Files:**
- Modify: `store/store.go`

**Step 1: Implement ReadStreamFrom**

Add to `store/store.go`:
```go
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./store -v -run TestStore_ReadStreamFrom`
Expected: PASS

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): implement ReadStreamFrom method"
```

---

## Global Reading

### Task 12: Write Test for ReadAll (Global Order)

**Files:**
- Modify: `store/store_test.go`

**Step 1: Write the test**

Add to `store/store_test.go`:
```go
func TestStore_ReadAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append events to multiple streams
	s.Append("alice", []byte("a1"))
	s.Append("bob", []byte("b1"))
	s.Append("alice", []byte("a2"))

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./store -v -run TestStore_ReadAll`
Expected: FAIL with "s.ReadAll undefined"

**Step 3: Commit the failing test**

```bash
git add store/store_test.go
git commit -m "test(store): add failing test for ReadAll"
```

---

### Task 13: Implement ReadAll Method

**Files:**
- Modify: `store/store.go`

**Step 1: Implement ReadAll**

Add to `store/store.go`:
```go
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
```

**Step 2: Run test to verify it passes**

Run: `go test ./store -v -run TestStore_ReadAll`
Expected: PASS

**Step 3: Commit**

```bash
git add store/store.go
git commit -m "feat(store): implement ReadAll method"
```

---

## Update Bank Example

### Task 14: Update Bank Example to Use Store

**Files:**
- Modify: `examples/bank/main.go`

**Step 1: Update imports and use Store instead of Log**

Replace the import and update all functions to use `store.Store` instead of `log.Log`.

Key changes:
1. Import `"github.com/hydra-db/hydra/store"` instead of `log`
2. Use `store.Open()` instead of `log.Open()`
3. Use `s.Append(streamID, data)` instead of `l.Append(data)`
4. Use `s.ReadStream(accountID)` instead of `l.ReadAll()` + filter
5. Update `replayAccount` to use `ReadStream`

The full updated `main.go`:
```go
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/hydra-db/hydra/store"
)

const dataFile = "bank.log"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "open":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bank open <account_id> [owner_name]")
			os.Exit(1)
		}
		accountID := os.Args[2]
		owner := accountID
		if len(os.Args) >= 4 {
			owner = os.Args[3]
		}
		openAccount(accountID, owner)

	case "deposit":
		if len(os.Args) < 4 {
			fmt.Println("Usage: bank deposit <account_id> <amount> [note]")
			os.Exit(1)
		}
		accountID := os.Args[2]
		amount, err := strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			fmt.Printf("Invalid amount: %s\n", os.Args[3])
			os.Exit(1)
		}
		note := ""
		if len(os.Args) >= 5 {
			note = os.Args[4]
		}
		deposit(accountID, amount, note)

	case "withdraw":
		if len(os.Args) < 4 {
			fmt.Println("Usage: bank withdraw <account_id> <amount> [note]")
			os.Exit(1)
		}
		accountID := os.Args[2]
		amount, err := strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			fmt.Printf("Invalid amount: %s\n", os.Args[3])
			os.Exit(1)
		}
		note := ""
		if len(os.Args) >= 5 {
			note = os.Args[4]
		}
		withdraw(accountID, amount, note)

	case "balance":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bank balance <account_id>")
			os.Exit(1)
		}
		accountID := os.Args[2]
		showBalance(accountID)

	case "history":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bank history <account_id>")
			os.Exit(1)
		}
		accountID := os.Args[2]
		showHistory(accountID)

	case "all":
		showAllEvents()

	case "stats":
		showStats()

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Bank - Event Sourcing Demo")
	fmt.Println()
	fmt.Println("Usage: bank <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  open <account_id> [owner]      Open a new account")
	fmt.Println("  deposit <account_id> <amount>  Deposit money")
	fmt.Println("  withdraw <account_id> <amount> Withdraw money")
	fmt.Println("  balance <account_id>           Show account balance (replays events)")
	fmt.Println("  history <account_id>           Show account event history")
	fmt.Println("  all                            Show all events in the log")
	fmt.Println("  stats                          Show log statistics")
}

func openAccount(accountID, owner string) {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Check if account already exists (stream has events)
	if s.StreamVersion(accountID) > 0 {
		fmt.Printf("Account %s already exists\n", accountID)
		os.Exit(1)
	}

	// Create and store the event
	event, err := NewAccountOpenedEvent(accountID, owner)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
		os.Exit(1)
	}

	data, err := event.Serialize()
	if err != nil {
		fmt.Printf("Failed to serialize event: %v\n", err)
		os.Exit(1)
	}

	pos, version, err := s.Append(accountID, data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Account %s opened for %s (position=%d, version=%d)\n", accountID, owner, pos, version)
}

func deposit(accountID string, amount int64, note string) {
	if amount <= 0 {
		fmt.Println("Amount must be positive")
		os.Exit(1)
	}

	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Check if account exists
	account := replayAccount(s, accountID)
	if !account.Exists {
		fmt.Printf("Account %s does not exist. Open it first.\n", accountID)
		os.Exit(1)
	}

	// Create and store the event
	event, err := NewMoneyDepositedEvent(accountID, amount, note)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
		os.Exit(1)
	}

	data, err := event.Serialize()
	if err != nil {
		fmt.Printf("Failed to serialize event: %v\n", err)
		os.Exit(1)
	}

	pos, version, err := s.Append(accountID, data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	newBalance := account.Balance + amount
	fmt.Printf("Deposited $%d to %s (balance=$%d, position=%d, version=%d)\n", amount, accountID, newBalance, pos, version)
}

func withdraw(accountID string, amount int64, note string) {
	if amount <= 0 {
		fmt.Println("Amount must be positive")
		os.Exit(1)
	}

	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Check if account exists and has sufficient balance
	account := replayAccount(s, accountID)
	if !account.Exists {
		fmt.Printf("Account %s does not exist. Open it first.\n", accountID)
		os.Exit(1)
	}
	if account.Balance < amount {
		fmt.Printf("Insufficient balance. Current balance: $%d, requested: $%d\n", account.Balance, amount)
		os.Exit(1)
	}

	// Create and store the event
	event, err := NewMoneyWithdrawnEvent(accountID, amount, note)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
		os.Exit(1)
	}

	data, err := event.Serialize()
	if err != nil {
		fmt.Printf("Failed to serialize event: %v\n", err)
		os.Exit(1)
	}

	pos, version, err := s.Append(accountID, data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	newBalance := account.Balance - amount
	fmt.Printf("Withdrew $%d from %s (balance=$%d, position=%d, version=%d)\n", amount, accountID, newBalance, pos, version)
}

func showBalance(accountID string) {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	account := replayAccount(s, accountID)
	fmt.Println(account.String())
}

func showHistory(accountID string) {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Use ReadStream - efficient O(k) lookup!
	events, err := s.ReadStream(accountID)
	if err != nil {
		fmt.Printf("Failed to read stream: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Event history for account %s:\n", accountID)
	fmt.Println("---")

	for _, e := range events {
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			continue
		}
		fmt.Printf("  v%d: %s\n", e.StreamVersion, event.String())
	}

	if len(events) == 0 {
		fmt.Println("  (no events found)")
	}
	fmt.Println("---")
	fmt.Printf("Total: %d events (current version: %d)\n", len(events), len(events))
}

func showAllEvents() {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	events, err := s.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read all: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All events in the store:")
	fmt.Println("---")

	for i, e := range events {
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			fmt.Printf("  #%d [pos=%d] <invalid>\n", i+1, e.GlobalPosition)
			continue
		}
		fmt.Printf("  #%d [pos=%d] [%s:v%d] %s\n", i+1, e.GlobalPosition, e.StreamID, e.StreamVersion, event.String())
	}

	if len(events) == 0 {
		fmt.Println("  (no events)")
	}
	fmt.Println("---")
	fmt.Printf("Total: %d events\n", len(events))
}

func showStats() {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	events, err := s.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read all: %v\n", err)
		os.Exit(1)
	}

	// Count events by type and account
	accounts := make(map[string]int)
	eventTypes := make(map[EventType]int)

	for _, e := range events {
		accounts[e.StreamID]++
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			continue
		}
		eventTypes[event.Type]++
	}

	fmt.Println("Store Statistics:")
	fmt.Println("---")
	fmt.Printf("Total events: %d\n", len(events))
	fmt.Println()
	fmt.Println("Events by type:")
	for t, count := range eventTypes {
		fmt.Printf("  %s: %d\n", t, count)
	}
	fmt.Println()
	fmt.Println("Events by account (stream):")
	for acc, count := range accounts {
		fmt.Printf("  %s: %d events (version %d)\n", acc, count, count)
	}
}

// replayAccount rebuilds account state by replaying stream events.
// NOW EFFICIENT: Uses ReadStream which is O(k) not O(n)!
func replayAccount(s *store.Store, accountID string) *Account {
	account := &Account{ID: accountID}

	events, err := s.ReadStream(accountID)
	if err != nil {
		return account
	}

	for _, e := range events {
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			continue
		}
		account.Apply(event)
	}

	return account
}
```

**Step 2: Build and test**

Run: `go build ./examples/bank`
Expected: No output (success)

**Step 3: Commit**

```bash
git add examples/bank/main.go
git commit -m "refactor(bank): use Store instead of Log for efficient lookups"
```

---

### Task 15: Test Bank Example

**Step 1: Delete old data and test fresh**

```bash
cd examples/bank
rm -f bank.log
go run . open alice "Alice Smith"
go run . deposit alice 100
go run . deposit alice 50
go run . withdraw alice 30
go run . balance alice
go run . history alice
```

Expected output showing versions:
```
Account alice opened for Alice Smith (position=0, version=0)
Deposited $100 to alice (balance=$100, position=..., version=1)
Deposited $50 to alice (balance=$150, position=..., version=2)
Withdrew $30 from alice (balance=$120, position=..., version=3)
Account alice (owner: Alice Smith): Balance = $120
Event history for account alice:
---
  v0: [HH:MM:SS] Account opened for Alice Smith
  v1: [HH:MM:SS] Deposited $100
  v2: [HH:MM:SS] Deposited $50
  v3: [HH:MM:SS] Withdrew $30
---
Total: 4 events (current version: 4)
```

**Step 2: Verify with stats**

```bash
go run . all
go run . stats
```

---

## Final Verification

### Task 16: Run All Tests

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All tests PASS

**Step 2: Check test coverage**

Run: `go test ./store -cover`
Expected: Coverage percentage displayed

**Step 3: Final commit**

```bash
git add -A
git commit -m "feat(store): complete Phase 2 - stream index with O(1) lookups"
```

---

## Summary

After completing all tasks, you will have:

| Component | Description |
|-----------|-------------|
| `store.Event` | Event with GlobalPosition, StreamID, StreamVersion, Data |
| `store.Open(path)` | Opens store, rebuilds index from log |
| `store.Close()` | Closes underlying log |
| `store.Append(streamID, data)` | Appends event, updates index, returns position + version |
| `store.ReadStream(streamID)` | Returns all events for stream (O(k)) |
| `store.ReadStreamFrom(streamID, version)` | Returns events from version onwards |
| `store.ReadAll()` | Returns all events in global order |
| `store.StreamVersion(streamID)` | Returns current stream version |

**Performance improvement:**
- Before (Phase 1): `replayAccount` scans all events O(n)
- After (Phase 2): `replayAccount` uses `ReadStream` O(k) where k = stream events

**Bank example updated** to use Store with efficient stream lookups.
