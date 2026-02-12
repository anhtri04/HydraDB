# Phase 3: The Guardian (Consistency Layer)

This phase protects your data from race conditions and duplicate writes. These are critical for a production event store.

---

## The Problem: Concurrent Writes

Imagine two users trying to withdraw money from Alice's account at the same time:

```
Alice's balance: $100

User A (withdraw $80)              User B (withdraw $50)
1. Read balance: $100              1. Read balance: $100
2. Check: 100 >= 80                2. Check: 100 >= 50 
3. Write: Withdrew $80             3. Write: Withdrew $50
4. New balance: $20                4. New balance: $50

Final balance: $50 (should be -$30, which should have been rejected!)
```

Both transactions saw $100 and both succeeded. This is a **race condition** - the classic "lost update" problem.

---

## 1. Optimistic Concurrency Control (OCC)

### The Concept

Instead of locking (pessimistic), we **detect conflicts at write time** (optimistic):

1. Client reads current version: "Alice is at version 5"
2. Client does work locally
3. Client writes: "Append to Alice, **expecting version 5**"
4. Server checks: Is Alice still at version 5?
   - **Yes** â†’ Write succeeds, version becomes 6
   - **No** â†’ Reject with `WrongExpectedVersionError`

```
User A                              User B
  ──────                              ──────
  Read: alice v5, balance=$100        Read: alice v5, balance=$100

  Append(alice, withdraw $80,         Append(alice, withdraw $50,
         expectedVersion=5)                  expectedVersion=5)
          │                                   │
          ▼                                   ▼
  ┌─────────────────────────────────────────────────────┐
  │                    EVENT STORE                      │
  │  alice current version = 5                          │
  │                                                     │
  │  User A arrives first:                              │
  │    expected=5, current=5 ✓ → Write, version=6       │
  │                                                     │
  │  User B arrives second:                             │
  │    expected=5, current=6 ✗ → REJECT!                │
  └─────────────────────────────────────────────────────┘
```

### Why "Optimistic"?

- **Pessimistic**: "I assume conflicts will happen, so I lock first"
- **Optimistic**: "I assume conflicts are rare, so I just check at the end"

Optimistic is better for event stores because:
- Reads don't block
- No deadlocks
- Conflicts are actually rare in well-designed systems

### Expected Version Options

| Value | Meaning |
|-------|---------|
| `ExpectedVersion.Any` (-1) | Don't check, always append (dangerous but fast) |
| `ExpectedVersion.NoStream` (0) | Stream must not exist (for creation) |
| `ExpectedVersion.StreamExists` (-2) | Stream must exist (any version) |
| Specific number (e.g., 5) | Stream must be exactly at this version |

### The Atomicity Requirement

**Critical**: The check and write must be **atomic** (indivisible).

```go
// WRONG - Race condition between check and write!
if s.StreamVersion(streamID) != expectedVersion {
    return ErrWrongExpectedVersion
}
// â† Another goroutine could write here!
s.log.Append(data)

// RIGHT - Use a mutex to make it atomic
s.mu.Lock()
defer s.mu.Unlock()
if s.StreamVersion(streamID) != expectedVersion {
    return ErrWrongExpectedVersion
}
s.log.Append(data)  // Protected by lock
```

---

## 2. Implementing Concurrency Control in Go

### Mutex Basics

A **mutex** (mutual exclusion) ensures only one goroutine can access a section at a time:

```go
import "sync"

type Store struct {
    mu    sync.Mutex      // Protects index and log writes
    log   *log.Log
    index map[string][]int64
}

func (s *Store) Append(streamID string, data []byte, expectedVersion int64) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Everything here is atomic - no other goroutine can interfere
    currentVersion := int64(len(s.index[streamID]))
    if expectedVersion != ExpectedVersionAny && currentVersion != expectedVersion {
        return ErrWrongExpectedVersion
    }
    
    // Safe to write
    pos, _ := s.log.Append(data)
    s.index[streamID] = append(s.index[streamID], pos)
    return nil
}
```

### RWMutex for Better Read Performance

Since reads don't modify data, multiple readers can proceed simultaneously:

```go
type Store struct {
    mu    sync.RWMutex    // RW = Read-Write mutex
    // ...
}

func (s *Store) ReadStream(streamID string) ([]Event, error) {
    s.mu.RLock()         // Read lock - multiple readers OK
    defer s.mu.RUnlock()
    // ... read operations
}

func (s *Store) Append(...) error {
    s.mu.Lock()          // Write lock - exclusive access
    defer s.mu.Unlock()
    // ... write operations
}
```

| Lock Type | Multiple Readers | Writers |
|-----------|------------------|---------|
| `RLock()` | Yes | Blocked |
| `Lock()` | Blocked | Exclusive |

---

## 3. The Second Problem: Duplicate Writes

### Network Retries

What if the network fails after the server writes but before the client gets confirmation?

```
Client                              Server
  ──────                              ──────
  Append(alice, deposit $100) ────────► Write succeeds!
                              ←──✗──── Response lost (network error)

  Client thinks it failed, retries:
  Append(alice, deposit $100) ────────► Write succeeds again!

Result: Alice deposited $200 instead of $100!
```

### Idempotency

**Idempotent** = "Can be applied multiple times with the same result"

Solution: Give each event a unique ID. If we've seen it before, don't write it again.

```go
type AppendRequest struct {
    StreamID        string
    ExpectedVersion int64
    EventID         string    // Unique identifier (e.g., UUID)
    Data            []byte
}

func (s *Store) Append(req AppendRequest) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Check for duplicate
    if s.hasEventID(req.StreamID, req.EventID) {
        return nil  // Already written, return success (idempotent)
    }
    
    // ... proceed with version check and write
}
```

### How to Track Event IDs

Option 1: **Store in event data** (simpler)
- Include EventID in the serialized envelope
- On startup, rebuild a set of seen IDs

Option 2: **Separate dedup index** (more efficient)
- `map[string]map[string]bool` - `streamID â†’ set of eventIDs`
- Only keep recent IDs (events older than X can't be retried)

For Phase 3, we'll use Option 1 - store EventID in the envelope.

---

## 4. Updated Envelope Format

Current (Phase 2):
```
[2 bytes: streamID length][streamID bytes][data bytes]
```

New (Phase 3):
```
[2 bytes: streamID length][streamID bytes][16 bytes: eventID (UUID)][data bytes]
```

Or with length-prefixed eventID for flexibility:
```
[2 bytes: streamID len][streamID][2 bytes: eventID len][eventID][data]
```

---

## 5. Error Types

```go
var (
    // ErrWrongExpectedVersion is returned when optimistic concurrency check fails
    ErrWrongExpectedVersion = errors.New("wrong expected version")
    
    // ErrStreamExists is returned when trying to create a stream that exists
    ErrStreamExists = errors.New("stream already exists")
    
    // ErrStreamNotFound is returned when stream doesn't exist but should
    ErrStreamNotFound = errors.New("stream not found")
)
```

---

## 6. API Changes

### Before (Phase 2)
```go
func (s *Store) Append(streamID string, data []byte) (pos int64, version int64, err error)
```

### After (Phase 3)
```go
// ExpectedVersion constants
const (
    ExpectedVersionAny        int64 = -1  // Don't check version
    ExpectedVersionNoStream   int64 = 0   // Stream must not exist
    ExpectedVersionStreamExists int64 = -2  // Stream must exist
)

// AppendResult contains the result of a successful append
type AppendResult struct {
    Position int64  // Byte offset in log
    Version  int64  // New stream version
}

// Append adds an event with optimistic concurrency control
func (s *Store) Append(streamID string, eventID string, data []byte, expectedVersion int64) (AppendResult, error)
```

---

## 7. What We'll Build

| Component | Purpose |
|-----------|---------|
| `sync.RWMutex` | Thread-safe access to index |
| `ExpectedVersion` constants | Version check options |
| `ErrWrongExpectedVersion` | Conflict error |
| Updated `Append()` | Takes expectedVersion, checks atomically |
| `eventID` in envelope | Deduplication support |
| Idempotency check | Skip duplicate eventIDs |

---

## 8. Testing Concurrency

Go has excellent tools for testing race conditions:

```bash
# Run tests with race detector
go test -race ./store
```

We'll write tests that:
1. Launch multiple goroutines appending to same stream
2. Verify only one succeeds with same expectedVersion
3. Verify total events match expected count

---

## Summary

| Concept | Key Point |
|---------|-----------|
| **Race Condition** | Two writers see same state, both write, data corrupted |
| **Optimistic Concurrency** | Check version at write time, reject if changed |
| **Expected Version** | Client declares "I expect stream at version X" |
| **Atomicity** | Check + write must be indivisible (use mutex) |
| **RWMutex** | Multiple readers OR one writer |
| **Idempotency** | Same request twice = same result (use eventID) |
| **Event ID** | Unique identifier to detect duplicates |

---

Ready to create the implementation plan for Phase 3?