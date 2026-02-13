package store

import (
	"sync"
)

// Snapshot represents a saved state at a specific version
type Snapshot struct {
	StreamID string
	Version  int64
	State    []byte // Serialized state (client defines format)
}

// SnapshotStore manages in-memory snapshots
type SnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]*Snapshot // streamID -> latest snapshot
	strategy  SnapshotStrategy
}

// NewSnapshotStore creates a new snapshot store
func NewSnapshotStore(strategy SnapshotStrategy) *SnapshotStore {
	if strategy == nil {
		strategy = &Never{}
	}
	return &SnapshotStore{
		snapshots: make(map[string]*Snapshot),
		strategy:  strategy,
	}
}

// Save stores a snapshot for a stream
func (ss *SnapshotStore) Save(streamID string, version int64, state []byte) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.snapshots[streamID] = &Snapshot{
		StreamID: streamID,
		Version:  version,
		State:    state,
	}
}

// Load retrieves the latest snapshot for a stream
// Returns nil if no snapshot exists
func (ss *SnapshotStore) Load(streamID string) *Snapshot {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	return ss.snapshots[streamID]
}

// Delete removes a snapshot (e.g., when stream is deleted)
func (ss *SnapshotStore) Delete(streamID string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	delete(ss.snapshots, streamID)
}

// ShouldSnapshot checks if a snapshot should be created based on the strategy
func (ss *SnapshotStore) ShouldSnapshot(streamID string, version int64, eventType string) bool {
	return ss.strategy.ShouldSnapshot(streamID, version, eventType)
}

// Strategy returns the current snapshot strategy
func (ss *SnapshotStore) Strategy() SnapshotStrategy {
	return ss.strategy
}

// SetStrategy changes the snapshot strategy
func (ss *SnapshotStore) SetStrategy(strategy SnapshotStrategy) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.strategy = strategy
}

// Count returns the number of stored snapshots
func (ss *SnapshotStore) Count() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return len(ss.snapshots)
}

// Clear removes all snapshots
func (ss *SnapshotStore) Clear() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.snapshots = make(map[string]*Snapshot)
}
