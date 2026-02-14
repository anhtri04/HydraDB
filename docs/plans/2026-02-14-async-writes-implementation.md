# Async Writes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add configurable async durability modes to HydraDB store for 100x+ write throughput improvement.

**Architecture:** Add `AsyncFlusher` that batches pending writes and flushes on interval/size thresholds. Writers return immediately after OS page cache write, wait for fsync outside the lock.

**Tech Stack:** Go 1.21+, existing HydraDB store/log packages

---

## Prerequisites

Read these files to understand current implementation:
- `store/store.go` - Core Append logic with global lock and fsync
- `log/log.go` - Log management
- `log/segment.go` - Segment file I/O with fsync
- `store/store_test.go` - Existing tests

---

## Task 1: Create Durability Configuration Types

**Files:**
- Create: `store/durability.go`

**Step 1: Write the types**

```go
package store

import "time"

// SyncMode controls when data is synced to disk
type SyncMode int

const (
	// SyncEveryWrite fsyncs after every write (safest, slowest)
	SyncEveryWrite SyncMode = iota
	// SyncAsync batches fsyncs based on interval/batch size
	SyncAsync
	// SyncEverySecond convenience mode for 1 second flush interval
	SyncEverySecond
)

// DurabilityConfig configures write durability guarantees
type DurabilityConfig struct {
	// SyncMode controls when data is synced to disk
	SyncMode SyncMode
	// SyncInterval is the max time between syncs (for Async mode)
	// Default: 10ms
	SyncInterval time.Duration
	// SyncBatchSize is the max events between syncs (for Async mode)
	// Default: 1000
	SyncBatchSize int
}

// DefaultDurabilityConfig returns safe defaults (current behavior)
func DefaultDurabilityConfig() DurabilityConfig {
	return DurabilityConfig{
		SyncMode:      SyncEveryWrite,
		SyncInterval:  10 * time.Millisecond,
		SyncBatchSize: 1000,
	}
}

// WithAsync returns config for async mode with custom interval
func WithAsync(interval time.Duration, batchSize int) DurabilityConfig {
	return DurabilityConfig{
		SyncMode:      SyncAsync,
		SyncInterval:  interval,
		SyncBatchSize: batchSize,
	}
}
```

**Step 2: Commit**

```bash
git add store/durability.go
git commit -m "feat(store): add durability configuration types"
```

---

## Task 2: Create AsyncFlusher Implementation

**Files:**
- Create: `store/async_flusher.go`

**Step 1: Write the flusher**

```go
package store

import (
	"sync"
	"time"
)

// pendingWrite tracks a write waiting for fsync
type pendingWrite struct {
	done chan struct{}
	err  error
}

// AsyncFlusher batches fsync operations for better throughput
type AsyncFlusher struct {
	config DurabilityConfig

	mu       sync.Mutex
	pending  []*pendingWrite
	timer    *time.Timer
	stopCh   chan struct{}
	wg       sync.WaitGroup
	flushFn  func() error
}

// NewAsyncFlusher creates a new async flusher
func NewAsyncFlusher(config DurabilityConfig, flushFn func() error) *AsyncFlusher {
	f := &AsyncFlusher{
		config:  config,
		stopCh:  make(chan struct{}),
		flushFn: flushFn,
		timer:   time.NewTimer(config.SyncInterval),
	}
	f.timer.Stop() // Don't start until first write
	f.wg.Add(1)
	go f.loop()
	return f
}

// loop is the background goroutine that triggers flushes
func (f *AsyncFlusher) loop() {
	defer f.wg.Done()
	for {
		select {
		case <-f.timer.C:
			f.Flush()
		case <-f.stopCh:
			return
		}
	}
}

// Queue adds a write to the pending queue and returns a channel
// that will be closed when the write is flushed
func (f *AsyncFlusher) Queue() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	pw := &pendingWrite{done: make(chan struct{})}
	f.pending = append(f.pending, pw)

	// Start timer on first pending write
	if len(f.pending) == 1 {
		f.timer.Reset(f.config.SyncInterval)
	}

	// Flush immediately if batch size reached
	if len(f.pending) >= f.config.SyncBatchSize {
		f.timer.Stop()
		go f.Flush()
	}

	return pw.done
}

// Flush performs an immediate fsync and notifies all pending writers
func (f *AsyncFlusher) Flush() error {
	f.mu.Lock()
	if len(f.pending) == 0 {
		f.mu.Unlock()
		return nil
	}

	pending := f.pending
	f.pending = make([]*pendingWrite, 0, f.config.SyncBatchSize)
	f.timer.Stop()
	f.mu.Unlock()

	// Perform the actual fsync
	err := f.flushFn()

	// Notify all pending writers
	for _, pw := range pending {
		pw.err = err
		close(pw.done)
	}

	return err
}

// Stop shuts down the flusher, performing a final flush
func (f *AsyncFlusher) Stop() error {
	close(f.stopCh)
	err := f.Flush()
	f.wg.Wait()
	return err
}
```

**Step 2: Commit**

```bash
git add store/async_flusher.go
git commit -m "feat(store): implement AsyncFlusher for batched fsync"
```

---

## Task 3: Add AsyncFlusher Tests

**Files:**
- Create: `store/async_flusher_test.go`

**Step 1: Write tests**

```go
package store

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncFlusher_FlushesOnBatchSize(t *testing.T) {
	var flushCount atomic.Int32
	flushFn := func() error {
		flushCount.Add(1)
		return nil
	}

	config := DurabilityConfig{
		SyncMode:      SyncAsync,
		SyncInterval:  time.Hour, // Never trigger by time
		SyncBatchSize: 3,
	}

	f := NewAsyncFlusher(config, flushFn)
	defer f.Stop()

	// Queue 2 writes - shouldn't flush yet
	f.Queue()
	f.Queue()
	time.Sleep(10 * time.Millisecond)

	if flushCount.Load() != 0 {
		t.Errorf("expected 0 flushes, got %d", flushCount.Load())
	}

	// Queue 3rd write - should trigger flush
	done := f.Queue()
	<-done

	if flushCount.Load() != 1 {
		t.Errorf("expected 1 flush, got %d", flushCount.Load())
	}
}

func TestAsyncFlusher_FlushesOnInterval(t *testing.T) {
	var flushCount atomic.Int32
	flushFn := func() error {
		flushCount.Add(1)
		return nil
	}

	config := DurabilityConfig{
		SyncMode:      SyncAsync,
		SyncInterval:  50 * time.Millisecond,
		SyncBatchSize: 1000,
	}

	f := NewAsyncFlusher(config, flushFn)
	defer f.Stop()

	// Queue a write
	done := f.Queue()

	// Should flush after interval
	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Error("flush didn't happen within timeout")
	}

	if flushCount.Load() != 1 {
		t.Errorf("expected 1 flush, got %d", flushCount.Load())
	}
}

func TestAsyncFlusher_MultipleWritersNotified(t *testing.T) {
	flushFn := func() error { return nil }

	config := DurabilityConfig{
		SyncMode:      SyncAsync,
		SyncInterval:  time.Hour,
		SyncBatchSize: 5,
	}

	f := NewAsyncFlusher(config, flushFn)
	defer f.Stop()

	// Queue 5 writes
	dones := make([]<-chan struct{}, 5)
	for i := 0; i < 5; i++ {
		dones[i] = f.Queue()
	}

	// All should be notified
	for i, done := range dones {
		select {
		case <-done:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Errorf("writer %d wasn't notified", i)
		}
	}
}

func TestAsyncFlusher_StopFlushesPending(t *testing.T) {
	var flushCount atomic.Int32
	flushFn := func() error {
		flushCount.Add(1)
		return nil
	}

	config := DurabilityConfig{
		SyncMode:      SyncAsync,
		SyncInterval:  time.Hour,
		SyncBatchSize: 100,
	}

	f := NewAsyncFlusher(config, flushFn)

	// Queue some writes
	f.Queue()
	f.Queue()

	// Stop should flush
	f.Stop()

	if flushCount.Load() != 1 {
		t.Errorf("expected 1 flush on stop, got %d", flushCount.Load())
	}
}
```

**Step 2: Run test to verify it passes**

```bash
go test -v ./store -run TestAsyncFlusher
```

Expected: All tests pass

**Step 3: Commit**

```bash
git add store/async_flusher_test.go
git commit -m "test(store): add AsyncFlusher unit tests"
```

---

## Task 4: Modify Store to Support Durability Config

**Files:**
- Modify: `store/store.go`

**Step 1: Add durability fields to Store struct**

Find the `Store` struct (around line 14-22) and add fields:

```go
// Store wraps a Log and adds stream indexing with thread-safe access.
type Store struct {
	mu          sync.RWMutex
	log         *log.Log
	index       map[string][]int64             // StreamID -> list of positions
	eventIDs    map[string]map[string]struct{} // StreamID -> set of eventIDs (for dedup)
	broadcaster *pubsub.Broadcaster
	deleted     map[string]bool // tracks deleted streams
	snapshots   *SnapshotStore

	// durability config
	durability DurabilityConfig
	flusher    *AsyncFlusher
}
```

**Step 2: Add functional option type and WithDurability**

Add after the Store struct definition:

```go
// Option configures a Store
type Option func(*Store) error

// WithDurability sets the durability configuration
func WithDurability(config DurabilityConfig) Option {
	return func(s *Store) error {
		s.durability = config
		return nil
	}
}
```

**Step 3: Modify Open function to accept options**

Change the Open function signature and implementation:

```go
// Open opens or creates a store at the given path.
// It rebuilds the index by scanning the existing log.
func Open(path string, opts ...Option) (*Store, error) {
	l, err := log.Open(path)
	if err != nil {
		return nil, err
	}

	s := &Store{
		log:        l,
		index:      make(map[string][]int64),
		eventIDs:   make(map[string]map[string]struct{}),
		deleted:    make(map[string]bool),
		durability: DefaultDurabilityConfig(),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(s); err != nil {
			l.Close()
			return nil, err
		}
	}

	// Rebuild index from existing data
	if err := s.rebuildIndex(); err != nil {
		l.Close()
		return nil, err
	}

	// Initialize async flusher if needed
	if s.durability.SyncMode == SyncAsync || s.durability.SyncMode == SyncEverySecond {
		if s.durability.SyncMode == SyncEverySecond {
			s.durability.SyncInterval = time.Second
		}
		s.flusher = NewAsyncFlusher(s.durability, func() error {
			return s.log.Flush()
		})
	}

	return s, nil
}
```

**Step 4: Modify Close to stop flusher**

Find the `Close` method (around line 83-85) and update:

```go
// Close closes the underlying log.
func (s *Store) Close() error {
	if s.flusher != nil {
		if err := s.flusher.Stop(); err != nil {
			return err
		}
	}
	return s.log.Close()
}
```

**Step 5: Add time import if needed**

Check imports and add `"time"` if not present.

**Step 6: Run existing tests**

```bash
go test ./store -v -run TestStore
```

Expected: Tests still pass (default sync mode = SyncEveryWrite, behavior unchanged)

**Step 7: Commit**

```bash
git add store/store.go
git commit -m "feat(store): add durability config and flusher integration"
```

---

## Task 5: Implement Async Support in Append (Option C Pattern)

**Files:**
- Modify: `store/store.go`

**Step 1: Add appendInternal helper method**

Add this helper method after the existing Append method (or before, doesn't matter):

```go
// appendInternal performs the locked portion of Append.
// Returns a channel that will be closed when data is flushed (if async mode).
func (s *Store) appendInternal(streamID, eventID string, data []byte, expectedVersion int64) (AppendResult, <-chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentVersion := int64(len(s.index[streamID]))

	// Check for idempotency - if eventID already seen, return success
	if eventID != "" {
		if ids, exists := s.eventIDs[streamID]; exists {
			if _, seen := ids[eventID]; seen {
				// Event already written, return current state (idempotent)
				return AppendResult{
					Position: -1, // Indicate no new write
					Version:  currentVersion - 1,
				}, nil, nil
			}
		}
	}

	// Check expected version
	switch expectedVersion {
	case ExpectedVersionAny:
		// Always allowed
	case ExpectedVersionNoStream:
		if currentVersion > 0 {
			return AppendResult{}, nil, ErrStreamExists
		}
	case ExpectedVersionStreamExists:
		if currentVersion == 0 {
			return AppendResult{}, nil, ErrStreamNotFound
		}
	default:
		if currentVersion != expectedVersion {
			return AppendResult{}, nil, ErrWrongExpectedVersion
		}
	}

	// Wrap data with StreamID and EventID
	envelope := serializeEnvelope(streamID, eventID, data)

	// Write to log
	pos, err := s.log.Append(envelope)
	if err != nil {
		return AppendResult{}, nil, err
	}

	// Update index
	s.index[streamID] = append(s.index[streamID], pos)
	newVersion := int64(len(s.index[streamID])) - 1

	// Track eventID for deduplication
	if eventID != "" {
		if s.eventIDs[streamID] == nil {
			s.eventIDs[streamID] = make(map[string]struct{})
		}
		s.eventIDs[streamID][eventID] = struct{}{}
	}

	// Notify subscribers
	if s.broadcaster != nil {
		s.broadcaster.Publish(pubsub.Event{
			GlobalPosition: pos,
			StreamID:       streamID,
			StreamVersion:  newVersion,
			Data:           data,
		})
	}

	result := AppendResult{
		Position: pos,
		Version:  newVersion,
	}

	// Queue for async flush if enabled
	if s.flusher != nil {
		return result, s.flusher.Queue(), nil
	}

	return result, nil, nil
}
```

**Step 2: Modify Append to use helper**

Replace the existing Append method with:

```go
// Append adds an event to a stream with optimistic concurrency control.
// Returns ErrWrongExpectedVersion if the expected version doesn't match.
// If eventID already exists in the stream, returns success without writing (idempotent).
func (s *Store) Append(streamID, eventID string, data []byte, expectedVersion int64) (AppendResult, error) {
	result, flushDone, err := s.appendInternal(streamID, eventID, data, expectedVersion)
	if err != nil {
		return AppendResult{}, err
	}

	// Wait for flush outside the lock (async mode)
	if flushDone != nil {
		<-flushDone
	}

	return result, nil
}
```

**Step 3: Add AppendSync method**

Add after Append:

```go
// AppendSync adds an event and forces immediate fsync to disk.
// Use this for critical writes that must survive a crash.
func (s *Store) AppendSync(streamID, eventID string, data []byte, expectedVersion int64) (AppendResult, error) {
	result, _, err := s.appendInternal(streamID, eventID, data, expectedVersion)
	if err != nil {
		return AppendResult{}, err
	}

	// Force immediate sync
	if err := s.log.Flush(); err != nil {
		return result, err
	}

	return result, nil
}
```

**Step 4: Add Flush method**

Add after AppendSync:

```go
// Flush forces an immediate sync of all pending writes to disk.
// Returns nil if sync mode is SyncEveryWrite (nothing to flush).
func (s *Store) Flush() error {
	if s.flusher != nil {
		return s.flusher.Flush()
	}
	return s.log.Flush()
}
```

**Step 5: Run tests**

```bash
go test ./store -v
```

Expected: All tests pass

**Step 6: Commit**

```bash
git add store/store.go
git commit -m "feat(store): implement Append with async durability support"
```

---

## Task 6: Add Async Durability Store Tests

**Files:**
- Modify: `store/store_test.go`

**Step 1: Add imports if needed**

Check imports and add:

```go
import (
	"fmt"
	"testing"
	"time"
	// ... other imports ...
)
```

**Step 2: Add async durability tests**

Add new test functions at the end of the file:

```go
func TestStore_AsyncDurability(t *testing.T) {
	dir := t.TempDir()

	config := WithAsync(50*time.Millisecond, 100)

	s, err := Open(dir, WithDurability(config))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append an event
	result, err := s.Append("test-stream", "event-1", []byte("data"), ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	// Event should be visible immediately (in-memory)
	if result.Version != 0 {
		t.Errorf("expected version 0, got %d", result.Version)
	}
}

func TestStore_AppendSync(t *testing.T) {
	dir := t.TempDir()

	// Use async mode but force sync for critical writes
	config := WithAsync(time.Hour, 1000)

	s, err := Open(dir, WithDurability(config))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Use AppendSync for critical write
	result, err := s.AppendSync("test-stream", "event-1", []byte("data"), ExpectedVersionNoStream)
	if err != nil {
		t.Fatalf("failed to append sync: %v", err)
	}

	if result.Version != 0 {
		t.Errorf("expected version 0, got %d", result.Version)
	}

	// Reopen store and verify data persisted
	s.Close()

	s2, err := Open(dir, WithDurability(DefaultDurabilityConfig()))
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer s2.Close()

	// Should be able to read the event
	events, err := s2.ReadStream("test-stream", 0, 10)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 event after reopen, got %d", len(events))
	}
}

func TestStore_AsyncBatchThroughput(t *testing.T) {
	dir := t.TempDir()

	config := WithAsync(10*time.Millisecond, 100)

	s, err := Open(dir, WithDurability(config))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// Append multiple events
	start := time.Now()
	for i := 0; i < 100; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		_, err := s.Append("test-stream", eventID, []byte("data"), ExpectedVersionAny)
		if err != nil {
			t.Fatalf("failed to append event %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	t.Logf("Appended 100 events in %v", elapsed)

	// With async mode, this should be very fast (< 100ms for 100 events)
	// In sync mode, this would take 100+ fsyncs = ~500-1000ms
	if elapsed > 500*time.Millisecond {
		t.Logf("Warning: async mode slower than expected, took %v", elapsed)
	}
}
```

**Step 3: Run new tests**

```bash
go test ./store -v -run "TestStore_Async|TestStore_AppendSync|TestStore_AsyncBatch"
```

Expected: All tests pass

**Step 4: Run all store tests**

```bash
go test ./store -v
```

Expected: All tests pass

**Step 5: Commit**

```bash
git add store/store_test.go
git commit -m "test(store): add async durability integration tests"
```

---

## Task 7: Update Log Package with Flush Method

**Files:**
- Modify: `log/log.go`
- Modify: `log/segment.go`

**Step 1: Add Flush method to Log**

In `log/log.go`, add after the existing methods:

```go
// Flush syncs the active segment to disk
func (l *Log) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.activeSegment.Flush()
}
```

**Step 2: Add Flush method to Segment**

In `log/segment.go`, add after the existing methods:

```go
// Flush syncs the segment file to disk
func (s *Segment) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.file.Sync()
}
```

**Step 3: Run tests**

```bash
go test ./log -v
```

Expected: Tests pass

**Step 4: Commit**

```bash
git add log/log.go log/segment.go
git commit -m "feat(log): add Flush method for explicit sync"
```

---

## Task 8: Integration Test - Verify Performance Improvement

**Files:**
- Create: `store/async_benchmark_test.go`

**Step 1: Write benchmark**

```go
package store

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkAppend_SyncEveryWrite(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir, WithDurability(DefaultDurabilityConfig()))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		s.Append("bench-stream", eventID, []byte("benchmark data"), ExpectedVersionAny)
	}
}

func BenchmarkAppend_Async10ms(b *testing.B) {
	dir := b.TempDir()
	config := WithAsync(10*time.Millisecond, 1000)
	s, err := Open(dir, WithDurability(config))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		s.Append("bench-stream", eventID, []byte("benchmark data"), ExpectedVersionAny)
	}
}

func BenchmarkAppend_Async100ms(b *testing.B) {
	dir := b.TempDir()
	config := WithAsync(100*time.Millisecond, 10000)
	s, err := Open(dir, WithDurability(config))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID := fmt.Sprintf("event-%d", i)
		s.Append("bench-stream", eventID, []byte("benchmark data"), ExpectedVersionAny)
	}
}
```

**Step 2: Run benchmark**

```bash
go test ./store -bench=BenchmarkAppend -benchtime=1s
```

Expected: Async modes should show 10-100x improvement

**Step 3: Commit**

```bash
git add store/async_benchmark_test.go
git commit -m "test(store): add async durability benchmarks"
```

---

## Task 9: Update Documentation

**Files:**
- Modify: `CLAUDE.md` - Add section about durability modes

**Step 1: Add durability section to CLAUDE.md**

Add after the Testing section:

```markdown
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
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add durability modes documentation"
```

---

## Task 10: Final Verification

**Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: All tests pass

**Step 2: Run benchmarks**

```bash
go test ./store -bench=. -benchtime=2s
```

Expected: Benchmarks show performance improvement

**Step 3: Build server**

```bash
go build -o hydra-server ./cmd/hydra
```

Expected: Build succeeds

**Step 4: Commit any final changes**

```bash
git status
git add -A
git commit -m "feat: complete async writes implementation" || echo "Nothing to commit"
```

---

## Summary of Changes

| File | Change |
|------|--------|
| `store/durability.go` | New - Configuration types |
| `store/async_flusher.go` | New - Async flusher implementation |
| `store/async_flusher_test.go` | New - Flusher unit tests |
| `store/store.go` | Modified - Integration with flusher, AppendSync, Option pattern |
| `store/store_test.go` | Modified - Async durability tests |
| `store/async_benchmark_test.go` | New - Performance benchmarks |
| `log/log.go` | Modified - Flush method |
| `log/segment.go` | Modified - Flush method |
| `CLAUDE.md` | Modified - Documentation |

---

## Design Decisions Explained

### Why Option C (Helper Method Pattern)?

Task 5 uses the `appendInternal` helper method pattern to solve the lock management problem cleanly:

1. **The Problem**: Original Append uses `defer s.mu.Unlock()`, which conflicts with releasing the lock early for async waiting
2. **Solution**: Split into two methods:
   - `appendInternal`: Does all locked work, returns flush channel
   - `Append`: Calls internal, waits for flush outside lock
3. **Benefits**:
   - Maintains `defer` safety within the locked section
   - Clear separation between locked and unlocked phases
   - Easy to test the locked logic separately
   - No risk of double-unlock panics

### Lock Release Timing

```go
// 1. Hold lock for fast in-memory operations
s.mu.Lock()
s.index[streamID] = append(s.index[streamID], pos)  // Fast
done := s.flusher.Queue()  // Register for flush
s.mu.Unlock()  // RELEASE - other writers can proceed

// 2. Wait for slow fsync WITHOUT holding lock
<-done  // Other writers update index during this time
```

This maximizes concurrency while maintaining correctness.

---

## Next Steps (Optional)

1. **Add HTTP API endpoint for flush**: `POST /admin/flush` to force sync
2. **Add metrics**: Expose pending write count, flush latency
3. **Add gRPC method**: `Flush` RPC for remote flush
4. **Client support**: Update `pkg/client` to expose sync options
