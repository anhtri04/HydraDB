# Phase 2: The Stream Index (Query Layer)

This phase solves the performance problem you just saw - scanning every event to find ones for a specific account. Let's dive into the concepts.

---

## The Problem

In the bank example, `replayAccount("alice")` does this:

```go
records, _ := l.ReadAll()           // Read ALL events
for _, record := range records {
    event := DeserializeEvent(record.Data)
    if event.AccountID == accountID {  // Filter in memory
        account.Apply(event)
    }
}
```

With 1 million events across 10,000 accounts, finding Alice's 100 events requires reading all 1 million. This is **O(n)** where n = total events.

---

## 1. Hash Maps (In-Memory Index)

### What is a Hash Map?

A hash map (also called dictionary, associative array, or map) provides **O(1) average** lookup by key.

```
Key (StreamID)     â†’     Value (Positions)
â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
"alice"            â†’     [0, 132, 281, 427]
"bob"              â†’     [566, 693]
"charlie"          â†’     [814, 920, 1050]
```

### How It Works

1. **Hash Function**: Converts key to integer (e.g., `hash("alice") = 42`)
2. **Bucket Array**: Integer indexes into array of buckets
3. **Lookup**: hash â†’ bucket â†’ find key â†’ return value

```
hash("alice") = 42
buckets[42] = [("alice", [0,132,281,427]), ("xyz", [...])]
                  â†‘
              Found it!
```

### Go's Built-in Map

```go
// Map from StreamID to list of byte positions
index := make(map[string][]int64)

// Add a position
index["alice"] = append(index["alice"], 0)
index["alice"] = append(index["alice"], 132)

// Lookup - O(1) average
positions := index["alice"]  // [0, 132]
```

---

## 2. Building the Index on Startup

### The Bootstrap Problem

When your event store starts, the index is empty. You need to rebuild it by scanning the log once:

```
Log File:
â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”
â”‚ pos=0     â”‚ pos=132   â”‚ pos=281   â”‚ pos=427   â”‚ pos=566     â”‚
â”‚ alice:Op  â”‚ alice:Dep â”‚ alice:Dep â”‚ alice:Wd  â”‚ bob:Open    â”‚
â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜
                            â†“
                     Scan once at startup
                            â†“
â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”
â”‚  Index (in RAM):                                            â”‚
â”‚    "alice" â†’ [0, 132, 281, 427]                             â”‚
â”‚    "bob"   â†’ [566]                                          â”‚
â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜
```

### Process

```go
func buildIndex(log *Log) map[string][]int64 {
    index := make(map[string][]int64)
    
    records, _ := log.ReadAll()
    for _, record := range records {
        event := deserialize(record.Data)
        streamID := event.AccountID  // or whatever identifies the stream
        index[streamID] = append(index[streamID], record.Position)
    }
    
    return index
}
```

**Trade-off**: Startup is O(n), but all subsequent lookups are O(1).

---

## 3. Keeping the Index Updated

When you append a new event, update the index immediately:

```go
func (s *Store) Append(streamID string, data []byte) (int64, error) {
    // Write to disk
    pos, err := s.log.Append(data)
    if err != nil {
        return 0, err
    }
    
    // Update in-memory index
    s.index[streamID] = append(s.index[streamID], pos)
    
    return pos, nil
}
```

The index stays in sync with the log without re-scanning.

---

## 4. Global Position vs Stream Version

This is a crucial concept in event sourcing. Each event has **two different orderings**:

### Global Position

The physical order in the log file - when did this event arrive relative to ALL events?

```
Global:  1        2        3        4        5        6
Event:   alice:Op alice:D  alice:D  alice:W  bob:Op   bob:D
```

**Use cases**:
- "Give me all events since position 1000" (catch-up subscriptions)
- "What happened system-wide in order?"

### Stream Version

The logical order within a specific stream - what version is this entity at?

```
Stream "alice":
  Version 0: AccountOpened
  Version 1: MoneyDeposited (first deposit)
  Version 2: MoneyDeposited (second deposit)
  Version 3: MoneyWithdrawn

Stream "bob":
  Version 0: AccountOpened
  Version 1: MoneyDeposited
```

**Use cases**:
- "Load alice's events starting from version 2"
- Optimistic concurrency (Phase 3): "Only append if alice is at version 3"

### How to Track Both

```go
type StoredEvent struct {
    GlobalPosition int64   // Position in log file (byte offset)
    StreamID       string  // Which stream (e.g., "alice")
    StreamVersion  int64   // Version within that stream (0, 1, 2...)
    Data           []byte  // The event payload
}
```

The index maps `StreamID â†’ []GlobalPosition`, and the version is just the array index:

```go
positions := index["alice"]  // [0, 132, 281, 427]
// Version 0 is at position 0
// Version 1 is at position 132
// Version 2 is at position 281
// Version 3 is at position 427
```

---

## 5. Reading Events by Stream

With the index, reading a stream's events becomes efficient:

```go
func (s *Store) ReadStream(streamID string) ([]Event, error) {
    positions, exists := s.index[streamID]
    if !exists {
        return nil, nil  // Stream doesn't exist
    }
    
    events := make([]Event, 0, len(positions))
    for _, pos := range positions {
        data, err := s.log.ReadAt(pos)
        if err != nil {
            return nil, err
        }
        events = append(events, deserialize(data))
    }
    
    return events, nil
}
```

**Performance**: O(k) where k = events in this stream, not n = total events.

---

## 6. The Index Durability Problem

### The Issue

The index lives in RAM. If the process crashes:
- Log file: **Safe** (on disk)
- Index: **Lost** (was in RAM)

On restart, you rebuild by scanning the log. For small logs, this is fine. For millions of events, startup becomes slow.

### Solutions (We'll implement the simple one for now)

| Approach | Description | Trade-off |
|----------|-------------|-----------|
| **Rebuild on startup** | Scan log, rebuild index | Simple but slow startup |
| **Persist index to file** | Save index to separate file | Fast startup, complexity |
| **Checkpoint + replay** | Save index periodically, replay only recent | Balanced approach |
| **LSM/SSTable** | Log-structured merge tree | Production-grade, complex |

For Phase 2, we'll use **rebuild on startup** - it's simple and correct. Phase 6 discusses optimization.

---

## 7. Architecture After Phase 2

```
  ┌─────────────────────────────────────────────────────────────┐
  │                        Store                                │
  │  ┌─────────────────────────────────────────────────────┐    │
  │  │  Index (RAM)                                        │    │
  │  │  map[string][]int64                                 │    │
  │  │    "alice" → [0, 132, 281, 427]                     │    │
  │  │    "bob"   → [566, 693]                             │    │
  │  └─────────────────────────────────────────────────────┘    │
  │                          │                                  │
  │                          ▼                                  │
  │  ┌─────────────────────────────────────────────────────┐    │
  │  │  Log (Disk)                                         │    │
  │  │  [event][event][event][event][event][event]         │    │
  │  └─────────────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────────────┘

Append(streamID, data):
  1. Write to Log â†’ get position
  2. Update Index[streamID] with position

ReadStream(streamID):
  1. Lookup Index[streamID] â†’ positions
  2. For each position, Log.ReadAt(pos)
```

---

## 8. What We'll Build

| Component | Purpose |
|-----------|---------|
| `Store` struct | Wraps Log + Index, provides stream operations |
| `Store.Open()` | Opens log, rebuilds index |
| `Store.Append(streamID, data)` | Appends event, updates index, returns position + version |
| `Store.ReadStream(streamID)` | Returns all events for a stream |
| `Store.ReadStreamFrom(streamID, fromVersion)` | Returns events starting from version |
| `Store.StreamVersion(streamID)` | Returns current version of a stream |

---

## Summary

| Concept | Key Point |
|---------|-----------|
| **Hash Map** | O(1) lookup by StreamID |
| **Index** | Maps StreamID â†’ list of byte positions |
| **Rebuild on startup** | Scan log once to populate index |
| **Keep in sync** | Update index on every append |
| **Global Position** | Order in log file (physical) |
| **Stream Version** | Order within stream (logical) |

---

Ready to create the implementation plan for Phase 2?