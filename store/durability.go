package store

import "time"

// SyncMode controls when data is synced to disk
type SyncMode int

const (
	// SyncEveryWrite fsyncs after every write (safest, slowest)
	SyncEveryWrite SyncMode = iota
	// SyncAsync batches fsyncs based on interval/size
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
