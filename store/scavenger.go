package store

import (
	"encoding/binary"
	"hash/crc32"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	hydralog "github.com/hydra-db/hydra/log"
)

// Scavenger performs background compaction of closed segments
type Scavenger struct {
	mu       sync.Mutex
	store    *Store
	running  bool
	stopCh   chan struct{}
	interval time.Duration
}

// ScavengeResult contains statistics from a scavenge operation
type ScavengeResult struct {
	SegmentID      int
	OriginalSize   int64
	CompactedSize  int64
	EventsRemoved  int
	EventsRetained int
	Duration       time.Duration
}

// NewScavenger creates a new background scavenger
func NewScavenger(s *Store, interval time.Duration) *Scavenger {
	return &Scavenger{
		store:    s,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins background scavenging
func (sc *Scavenger) Start() {
	sc.mu.Lock()
	if sc.running {
		sc.mu.Unlock()
		return
	}
	sc.running = true
	sc.stopCh = make(chan struct{})
	sc.mu.Unlock()

	go sc.run()
}

// Stop stops the background scavenger
func (sc *Scavenger) Stop() {
	sc.mu.Lock()
	if !sc.running {
		sc.mu.Unlock()
		return
	}
	sc.running = false
	sc.mu.Unlock()

	close(sc.stopCh)
}

// IsRunning returns whether the scavenger is running
func (sc *Scavenger) IsRunning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.running
}

func (sc *Scavenger) run() {
	ticker := time.NewTicker(sc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sc.ScavengeClosedSegments()
		case <-sc.stopCh:
			return
		}
	}
}

// ScavengeClosedSegments scavenges all closed segments (can be called manually)
func (sc *Scavenger) ScavengeClosedSegments() []ScavengeResult {
	sc.store.mu.RLock()
	l := sc.store.log
	closedSegments := l.ClosedSegments()
	deletedStreams := make(map[string]bool)
	for k, v := range sc.store.deleted {
		deletedStreams[k] = v
	}
	sc.store.mu.RUnlock()

	var results []ScavengeResult

	for _, segInfo := range closedSegments {
		result, err := sc.scavengeSegment(segInfo, deletedStreams)
		if err != nil {
			log.Printf("scavenge segment %d failed: %v", segInfo.ID, err)
			continue
		}
		if result != nil {
			results = append(results, *result)
		}
	}

	return results
}

// scavengeSegment compacts a single segment
func (sc *Scavenger) scavengeSegment(segInfo hydralog.SegmentInfo, deletedStreams map[string]bool) (*ScavengeResult, error) {
	start := time.Now()

	// Open the segment for reading
	seg, err := hydralog.OpenSegment(filepath.Dir(segInfo.Path), segInfo.ID, segInfo.BasePos)
	if err != nil {
		return nil, err
	}
	defer seg.CloseFile()

	// Read all records
	records, err := seg.ReadAll()
	if err != nil {
		return nil, err
	}

	// Filter out deleted streams
	var retained []hydralog.Record
	removed := 0

	for _, record := range records {
		streamID, _, _, err := deserializeEnvelope(record.Data)
		if err != nil {
			continue // Skip corrupt records
		}

		if deletedStreams[streamID] {
			removed++
			continue
		}

		retained = append(retained, record)
	}

	// If nothing was removed, skip rewriting
	if removed == 0 {
		return nil, nil
	}

	// Write retained records to a new segment file
	newPath := segInfo.Path + ".new"
	newFile, err := os.Create(newPath)
	if err != nil {
		return nil, err
	}

	var newSize int64
	for _, record := range retained {
		n, err := writeRecord(newFile, record.Data)
		if err != nil {
			newFile.Close()
			os.Remove(newPath)
			return nil, err
		}
		newSize += n
	}
	newFile.Sync()
	newFile.Close()

	// Atomic swap: rename new file to old file
	if err := os.Rename(newPath, segInfo.Path); err != nil {
		os.Remove(newPath)
		return nil, err
	}

	return &ScavengeResult{
		SegmentID:      segInfo.ID,
		OriginalSize:   segInfo.Size,
		CompactedSize:  newSize,
		EventsRemoved:  removed,
		EventsRetained: len(retained),
		Duration:       time.Since(start),
	}, nil
}

// writeRecord writes a record in the standard format [4 bytes length][4 bytes checksum][data]
func writeRecord(f *os.File, data []byte) (int64, error) {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(header[4:8], crc32.ChecksumIEEE(data))

	n1, err := f.Write(header)
	if err != nil {
		return 0, err
	}

	n2, err := f.Write(data)
	if err != nil {
		return 0, err
	}

	return int64(n1 + n2), nil
}
