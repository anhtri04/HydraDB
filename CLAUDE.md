# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Hydra is an embedded event store database written in Go. It provides append-only streams with optimistic concurrency control, real-time subscriptions, and both HTTP and gRPC APIs.

## Common Commands

```bash
# Build the server
go build -o hydra-server ./cmd/hydra

# Run tests
go test ./...
go test -v ./store/...    # Run specific package tests
go test -run TestStore_ReadStream ./store/...  # Run single test

# Run the server
./hydra-server

# Generate gRPC code (when proto changes)
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       server/grpc/proto/eventstore.proto
```

## Architecture

### Layer Structure

```
API Layer (server/)
├── HTTP (port 8080) - REST-like with NDJSON streaming
└── gRPC (port 9090) - Protocol buffers with reflection

Core Layer (store/)
├── Store - Stream indexing, concurrency control, soft deletes
├── SnapshotStore - In-memory state snapshots
└── Scavenger - Background compaction of deleted streams

Storage Layer (log/)
├── Log - Manages segment rotation
└── Segment - Individual log files with checksums

Pub/Sub Layer (pubsub/)
└── Broadcaster - In-memory event distribution to subscribers
```

### Storage Format

**Segment Files:** Named `hydra-00000.log`, `hydra-00001.log`, etc. Each segment defaults to 64MB before rotation.

**Record Format:** `[4 bytes length][4 bytes checksum][data]` - Uses CRC32 for integrity verification.

**Envelope Format:** `[2 bytes streamID len][streamID][2 bytes eventID len][eventID][data]` - Events are wrapped with metadata before storage.

### Key Design Patterns

1. **Stream Indexing:** The store maintains an in-memory index (`map[string][]int64`) mapping stream IDs to log positions. This index is rebuilt by scanning all segments on store open.

2. **Optimistic Concurrency:** Append operations specify an expected version. Special constants: `ExpectedVersionAny` (-1), `ExpectedVersionNoStream` (0), `ExpectedVersionStreamExists` (-2).

3. **Soft Deletes:** Streams are marked deleted via tombstone events (`$stream-deleted`). Data remains in the log but is hidden from reads. The scavenger compacts deleted data from closed segments.

4. **Subscriptions:** The broadcaster maintains subscriber channels with filtering. Slow subscribers drop events rather than blocking. Both catch-up (historical) and live subscriptions are supported.

5. **Idempotency:** Events with the same `eventID` within a stream are deduplicated on append.

### Error Handling

The store package defines specific errors for concurrency control:
- `ErrWrongExpectedVersion` - Optimistic concurrency check failed
- `ErrStreamExists` - Stream already exists when `ExpectedVersionNoStream` specified
- `ErrStreamNotFound` - Stream doesn't exist when `ExpectedVersionStreamExists` specified

## Testing

Tests use `t.TempDir()` for isolation and create fresh store instances per test. The store is safe for concurrent access via a `sync.RWMutex`.

## Durability Modes

HydraDB supports configurable durability modes for different throughput/latency trade-offs:

### Sync Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `SyncEveryWrite` | fsync after every write (default) | Financial data, critical events |
| `SyncAsync` | Batch fsyncs by interval/size | General event sourcing, logging |
| `SyncEverySecond` | fsync once per second | Metrics, temporary buffers |

### Configuration

```go
// Default (safest, slowest)
store, err := store.Open(dir)

// Async with 10ms flush interval
store, err := store.Open(dir, store.WithDurability(store.WithAsync(10*time.Millisecond, 1000)))

// Explicit sync for critical writes
result, err := store.AppendSync(streamID, eventID, data, expectedVersion)
```

### Performance Expectations

| Mode | Typical Throughput |
|------|-------------------|
| SyncEveryWrite | ~500 events/sec |
| SyncAsync (10ms) | ~50,000 events/sec |
| SyncAsync (100ms) | ~100,000 events/sec |

## gRPC Reflection

The gRPC server has reflection enabled, allowing exploration with tools like `grpcurl`:

```bash
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext localhost:9090 describe eventstore.EventStore
```
