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

	mu      sync.Mutex
	pending []*pendingWrite
	timer   *time.Timer
	stopCh  chan struct{}
	stopOnce sync.Once
	wg      sync.WaitGroup
	flushFn func() error
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
	f.stopOnce.Do(func() {
		close(f.stopCh)
	})
	err := f.Flush()
	f.wg.Wait()
	return err
}
