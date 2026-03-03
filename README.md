# Hydra Event Store

Hydra is an embedded event store database written in Go. It provides append-only streams with optimistic concurrency control, real-time subscriptions, and both HTTP and gRPC APIs.

## Features

- **Append-only streams** with automatic indexing for O(k) reads (where k is events in the stream)
- **Optimistic concurrency control** for safe concurrent writes
- **Real-time subscriptions** via Server-Sent Events (SSE) and gRPC streaming
- **Multiple durability modes** for throughput/latency trade-offs
- **Soft delete** with background scavenging
- **Idempotent writes** with event deduplication
- **Snapshot support** for fast aggregate state recovery
- **Connection pooling clients** for HTTP and gRPC

## Architecture

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

## Quick Start

### Build

```bash
# Build the server
go build -o hydra-server ./cmd/hydra

# Build stress test tools
go build -o http_stress ./cmd/stress/http_stress
go build -o grpc_stress ./cmd/stress/grpc_stress

# Generate gRPC code (when proto changes)
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       server/grpc/proto/eventstore.proto
```

### Run the Server

```bash
./hydra-server
```

The server starts:
- HTTP API on port 8080
- gRPC API on port 9090

## Embedded Library Usage

Hydra can be used as an embedded library for event sourcing applications:

```go
package main

import (
    "log"
    "github.com/hydra-db/hydra/store"
)

func main() {
    // Open or create a store
    s, err := store.Open("./data")
    if err != nil {
        log.Fatal(err)
    }
    defer s.Close()

    // Append an event with optimistic concurrency control
    result, err := s.Append(
        "user-123",                    // stream ID
        "event-001",                   // event ID (for idempotency)
        []byte(`{"name":"John"}`),    // event data
        store.ExpectedVersionNoStream, // expected version
    )
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Event written at position %d, version %d", result.Position, result.Version)

    // Read stream events
    events, err := s.ReadStream("user-123")
    if err != nil {
        log.Fatal(err)
    }

    for _, e := range events {
        log.Printf("Event v%d: %s", e.StreamVersion, string(e.Data))
    }
}
```

## Expected Version Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `ExpectedVersionAny` | -1 | Allow append regardless of current version |
| `ExpectedVersionNoStream` | 0 | Require stream to not exist (for creation) |
| `ExpectedVersionStreamExists` | -2 | Require stream to exist (any version) |

## Durability Modes

Configure durability for different throughput/latency trade-offs:

| Mode | Description | Use Case |
|------|-------------|----------|
| `SyncEveryWrite` | fsync after every write (default) | Financial data, critical events |
| `SyncAsync` | Batch fsyncs by interval/size | General event sourcing, logging |
| `SyncEverySecond` | fsync once per second | Metrics, temporary buffers |

### Configuration

```go
// Default (safest, slowest)
s, err := store.Open(dir)

// Async with 10ms flush interval
s, err := store.Open(dir, store.WithDurability(store.WithAsync(10*time.Millisecond, 1000)))

// Explicit sync for critical writes
result, err := s.AppendSync(streamID, eventID, data, expectedVersion)
```

### Performance Expectations

| Mode | Typical Throughput |
|------|-------------------|
| SyncEveryWrite | ~500 events/sec |
| SyncAsync (10ms) | ~50,000 events/sec |
| SyncAsync (100ms) | ~100,000 events/sec |

## HTTP API

### Append Event

```bash
POST /streams/{streamID}
Content-Type: application/json

{
  "data": "base64-encoded-event-data",
  "expected_version": 0
}
```

Response: `201 Created`
```json
{
  "position": 123,
  "version": 0
}
```

### Read Stream

```bash
GET /streams/{streamID}
```

Response: `200 OK` with NDJSON stream
```json
{"position":123,"stream_id":"user-123","version":0,"data":"..."}
{"position":456,"stream_id":"user-123","version":1,"data":"..."}
```

### Read All Events

```bash
GET /all
```

### Subscribe to All Events (SSE)

```bash
GET /subscribe/all
```

Server-Sent Events stream with catch-up + live events.

### Subscribe to Stream (SSE)

```bash
GET /subscribe/streams/{streamID}
```

### Health Check

```bash
GET /health
```

## gRPC API

The gRPC server has reflection enabled for exploration with tools like `grpcurl`.

### Service Definition

```protobuf
service EventStore {
  rpc Append(AppendRequest) returns (AppendResponse);
  rpc ReadStream(ReadStreamRequest) returns (stream Event);
  rpc ReadAll(ReadAllRequest) returns (stream Event);
  rpc Health(HealthRequest) returns (HealthResponse);
  rpc SubscribeToAll(SubscribeToAllRequest) returns (stream Event);
  rpc SubscribeToStream(SubscribeToStreamRequest) returns (stream Event);
}
```

### Explore with grpcurl

```bash
# List services
grpcurl -plaintext localhost:9090 list

# Describe service
grpcurl -plaintext localhost:9090 describe eventstore.EventStore

# Health check
grpcurl -plaintext localhost:9090 eventstore.EventStore/Health

# Append event
grpcurl -plaintext -d '{
  "stream_id": "user-123",
  "event_id": "event-001",
  "data": "...",
  "expected_version": 0
}' localhost:9090 eventstore.EventStore/Append

# Read stream
grpcurl -plaintext -d '{
  "stream_id": "user-123"
}' localhost:9090 eventstore.EventStore/ReadStream

# Subscribe to all events
grpcurl -plaintext localhost:9090 eventstore.EventStore/SubscribeToAll
```

## Client Library

Connection pooling clients for high-performance access:

```go
import "github.com/hydra-db/hydra/pkg/client"

// Unified client (uses gRPC internally)
client, err := client.NewHydraClient("http://localhost:8080", "localhost:9090")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Append event
err = client.Append(ctx, "my-stream", "event-1", []byte(`{"data":"value"}`))
```

### Performance

| Protocol | Pool Size | 1000 CCU | Throughput |
|----------|-----------|----------|------------|
| HTTP | 20 | < 1% errors | ~5,000 req/s |
| gRPC | 10 | < 0.1% errors | ~50,000 req/s |

See [pkg/client/README.md](pkg/client/README.md) for detailed client documentation.

## Example: Bank Account (Event Sourcing)

A complete event sourcing example is included in `examples/bank/`:

```bash
cd examples/bank

# Open a new account
go run . open acc-001 "Alice"

# Deposit money
go run . deposit acc-001 100 "Opening deposit"

# Withdraw money
go run . withdraw acc-001 30 "ATM withdrawal"

# Check balance (replays events)
go run . balance acc-001

# Show event history
go run . history acc-001

# Show all events in the log
go run . all
```

## Testing

```bash
# Run all tests
go test ./...

# Run tests for specific package
go test -v ./store/...
go test -v ./log/...
go test -v ./pubsub/...

# Run single test
go test -run TestStore_ReadStream ./store/...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -bench=. ./store/...
```

## Design Patterns

### Stream Indexing

The store maintains an in-memory index (`map[string][]int64`) mapping stream IDs to log positions. This index is rebuilt by scanning all segments on store open.

### Soft Deletes

Streams are marked deleted via tombstone events (`$stream-deleted`). Data remains in the log but is hidden from reads. The scavenger compacts deleted data from closed segments.

### Subscriptions

The broadcaster maintains subscriber channels with filtering. Slow subscribers drop events rather than blocking. Both catch-up (historical) and live subscriptions are supported.

### Idempotency

Events with the same `eventID` within a stream are deduplicated on append.

## Error Handling

The store package defines specific errors:

- `ErrWrongExpectedVersion` - Optimistic concurrency check failed
- `ErrStreamExists` - Stream already exists when `ExpectedVersionNoStream` specified
- `ErrStreamNotFound` - Stream doesn't exist when `ExpectedVersionStreamExists` specified

## License

MIT License - See LICENSE file for details.
