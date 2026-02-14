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
