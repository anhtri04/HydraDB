# AGENTS.md

Guidelines for AI agents working with the Hydra event store database codebase.

## Project Overview

Hydra is an embedded event store database written in Go. It provides append-only streams with optimistic concurrency control, real-time subscriptions, and both HTTP and gRPC APIs.

## Build Commands

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

## Test Commands

```bash
# Run all tests
go test ./...

# Run tests for specific package
go test -v ./store/...
go test -v ./log/...
go test -v ./pubsub/...

# Run single test
go test -run TestStore_ReadStream ./store/...
go test -run TestLog_Append ./log/...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -bench=. ./store/...
```

## Code Style Guidelines

### Imports

Group imports in this order:
1. Standard library
2. Blank line
3. Project imports (`github.com/hydra-db/hydra/...`)
4. External dependencies

```go
import (
    "errors"
    "sync"
    "time"

    "github.com/hydra-db/hydra/log"
    "github.com/hydra-db/hydra/pubsub"

    "google.golang.org/grpc"
)
```

### Naming Conventions

- **Exported identifiers**: PascalCase (e.g., `Store`, `Open`, `Append`)
- **Unexported identifiers**: camelCase (e.g., `rebuildIndex`, `maxSegmentSize`)
- **Error variables**: Prefix with `Err` (e.g., `ErrCorruptRecord`, `ErrWrongExpectedVersion`)
- **Constants**: ALL_CAPS with underscores (e.g., `DefaultMaxSegmentSize`, `StreamDeletedType`)
- **Test functions**: `Test` + descriptive name (e.g., `TestStore_ReadStream`, `TestLog_Append`)
- **Interface names**: Single method interfaces use `-er` suffix (e.g., `Reader`, `Appender`)

### Comments

Follow Go conventions - all exported identifiers must have a comment starting with the identifier name:

```go
// Store wraps a Log and adds stream indexing with thread-safe access.
type Store struct { ... }

// Open opens or creates a store at the given path.
func Open(path string, opts ...Option) (*Store, error) { ... }
```

### Error Handling

Use standard Go error handling patterns:

```go
if err != nil {
    return nil, err
}
```

Define package-level error variables for specific error types:

```go
var ErrWrongExpectedVersion = errors.New("wrong expected version")
var ErrStreamNotFound = errors.New("stream not found")
```

### Testing

- Use `package xxx_test` (external test package) to test only public APIs
- Use `t.TempDir()` for test isolation
- Test function names should be descriptive: `TestStore_RebuildIndexOnOpen`
- Use `t.Fatalf` for fatal errors, `t.Errorf` for non-fatal assertions
- Always close resources with `defer`

```go
func TestStore_ReadStream(t *testing.T) {
    dir := t.TempDir()
    s, err := store.Open(dir)
    if err != nil {
        t.Fatalf("failed to open store: %v", err)
    }
    defer s.Close()
    // ... test code
}
```

### Struct Tags and Fields

Document exported struct fields when not obvious:

```go
type Record struct {
    Position int64  // byte offset in the log
    Data     []byte // raw record data
}
```

### Concurrency

- Protect shared state with `sync.RWMutex`
- Lock at the smallest scope possible
- Document which methods are safe for concurrent use

### Architecture Patterns

- **Options pattern**: Use functional options for configuration
- **Error wrapping**: Use `fmt.Errorf("...: %w", err)` when adding context
- **Resource cleanup**: Always provide `Close()` methods and use `defer`
- **Testing**: Prefer table-driven tests for multiple test cases
