# Async Writes Design

## Summary

Add configurable durability modes to HydraDB that allow batching fsync operations, dramatically improving write throughput while providing flexibility for different durability requirements.

## Problem

Current HydraDB performs a blocking fsync() on every Append() operation. This limits throughput to ~500 events/sec regardless of concurrency, as each write waits for disk latency (~1-10ms).

## Solution

Introduce an `AsyncFlusher` that batches writes in memory and performs a single fsync for multiple events. Writers return immediately after writing to the OS page cache.

## Configuration

```go
type DurabilityConfig struct {
    SyncMode      SyncMode      // SyncEveryWrite, SyncAsync, SyncEverySecond
    SyncInterval  time.Duration // Max time between syncs (default: 10ms)
    SyncBatchSize int           // Max events between syncs (default: 1000)
}

type SyncMode int
const (
    SyncEveryWrite SyncMode = iota  // Current behavior (fsync every write)
    SyncAsync                        // Async with configurable flush
    SyncEverySecond                  // Convenience: 1 second flush
)
```

## Durability Guarantees

| Mode                | Data Loss Window | Use Case                          |
|---------------------|------------------|-----------------------------------|
| SyncEveryWrite      | 0ms              | Financial transactions            |
| SyncAsync (10ms)    | ~10ms            | General event sourcing            |
| SyncAsync (100ms)   | ~100ms           | Logging, metrics                  |
| SyncEverySecond     | ~1000ms          | Buffer/temporary data             |

## API Changes

### Store Configuration

```go
store, err := store.Open(dir, store.WithDurability(store.DurabilityConfig{
    SyncMode:      store.SyncAsync,
    SyncInterval:  10 * time.Millisecond,
    SyncBatchSize: 1000,
}))
```

### Explicit Flush

```go
// For critical writes, force immediate sync
result, err := store.AppendSync(streamID, eventID, data, expectedVersion)

// Or flush all pending writes
err := store.Flush()
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Write Path (Before)                                        │
│    1. Serialize event                                       │
│    2. Write to file                                         │
│    3. fsync() ← BLOCKS HERE (1-10ms)                        │
│    4. Return                                                │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  Write Path (After)                                         │
│    1. Serialize event                                       │
│    2. Write to OS page cache                                │
│    3. Add to pending sync queue                             │
│    4. Return immediately                                    │
│                                                             │
│  Background Flusher:                                        │
│    - fsync() all pending writes at once                     │
│    - Notify waiting goroutines                              │
└─────────────────────────────────────────────────────────────┘
```

## Expected Performance

| Scenario                 | Before  | After (10ms)   | Improvement |
|--------------------------|---------|----------------|-------------|
| Single-threaded          | ~500/s  | ~50,000/s      | 100x        |
| 100 concurrent writers   | ~500/s  | ~100,000/s     | 200x        |

## Edge Cases

| Scenario                 | Behavior                                      |
|--------------------------|-----------------------------------------------|
| Process crash            | Events not yet fsync'd are lost (configurable) |
| Disk full during flush   | Error propagates to all pending writers       |
| Graceful shutdown        | Close() flushes all pending writes            |
| Mixed sync/async usage   | AppendSync() bypasses async queue             |

## Backwards Compatibility

- Default mode: `SyncEveryWrite` (current behavior)
- Existing code works unchanged
- New code can opt-in to async with config
- No protocol changes to HTTP/gRPC APIs
