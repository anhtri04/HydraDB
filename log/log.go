package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// LengthSize is the size of the record length field in bytes (uint32)
	LengthSize = 4
	// ChecksumSize is the size of the CRC32 checksum field in bytes
	ChecksumSize = 4
	// HeaderSize is the total size of the record header (length + checksum)
	HeaderSize = LengthSize + ChecksumSize

	// DefaultMaxSegmentSize is the default max size before rotating (64MB)
	DefaultMaxSegmentSize = 64 * 1024 * 1024
)

// ErrCorruptRecord indicates a record failed checksum validation
var ErrCorruptRecord = errors.New("corrupt record: checksum mismatch")

// Record represents a single log entry with its position
type Record struct {
	Position int64
	Data     []byte
}

// Log manages a segmented append-only log
type Log struct {
	mu             sync.RWMutex
	dir            string
	segments       []*Segment
	activeSegment  *Segment
	maxSegmentSize int64
}

// Open opens or creates a segmented log in the given directory
func Open(path string) (*Log, error) {
	return OpenWithOptions(path, DefaultMaxSegmentSize)
}

// OpenWithOptions opens a log with custom segment size
func OpenWithOptions(path string, maxSegmentSize int64) (*Log, error) {
	// Use path as directory for segment files
	dir := path

	// For backwards compatibility: if path is a file, use its directory
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	l := &Log{
		dir:            dir,
		segments:       make([]*Segment, 0),
		maxSegmentSize: maxSegmentSize,
	}

	// Load existing segments
	if err := l.loadSegments(); err != nil {
		return nil, err
	}

	// Create initial segment if none exist
	if len(l.segments) == 0 {
		seg, err := OpenSegment(dir, 0, 0)
		if err != nil {
			return nil, err
		}
		l.segments = append(l.segments, seg)
		l.activeSegment = seg
	} else {
		// Last segment is active
		l.activeSegment = l.segments[len(l.segments)-1]
	}

	return l, nil
}

// loadSegments discovers and opens existing segment files
func (l *Log) loadSegments() error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Find all segment files
	var segmentFiles []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "hydra-") && strings.HasSuffix(entry.Name(), ".log") {
			segmentFiles = append(segmentFiles, entry.Name())
		}
	}

	// Sort by segment ID
	sort.Strings(segmentFiles)

	// Open each segment
	var basePos int64 = 0
	for i, name := range segmentFiles {
		// Parse segment ID from filename
		idStr := strings.TrimPrefix(name, "hydra-")
		idStr = strings.TrimSuffix(idStr, ".log")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue // Skip invalid files
		}

		seg, err := OpenSegment(l.dir, id, basePos)
		if err != nil {
			return err
		}

		// Mark all but last as closed
		if i < len(segmentFiles)-1 {
			seg.MarkClosed()
		}

		l.segments = append(l.segments, seg)
		basePos += seg.Size()
	}

	return nil
}

// Append writes data to the log and returns the global position
func (l *Log) Append(data []byte) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if we need to rotate
	if l.activeSegment.Size() >= l.maxSegmentSize {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}

	return l.activeSegment.Append(data)
}

// rotate closes the current segment and creates a new one
func (l *Log) rotate() error {
	// Close current segment
	l.activeSegment.MarkClosed()

	// Create new segment
	newID := l.activeSegment.ID() + 1
	newBasePos := l.activeSegment.Info().BasePos + l.activeSegment.Size()

	seg, err := OpenSegment(l.dir, newID, newBasePos)
	if err != nil {
		return err
	}

	l.segments = append(l.segments, seg)
	l.activeSegment = seg

	return nil
}

// ReadAt reads a record at the given global position
func (l *Log) ReadAt(position int64) ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Find the segment containing this position
	seg := l.findSegment(position)
	if seg == nil {
		return nil, fmt.Errorf("position %d not found", position)
	}

	// Calculate local offset within segment
	localOffset := position - seg.Info().BasePos
	return seg.ReadAt(localOffset)
}

// findSegment finds the segment containing the given global position
func (l *Log) findSegment(position int64) *Segment {
	for _, seg := range l.segments {
		info := seg.Info()
		if position >= info.BasePos && position < info.BasePos+info.Size {
			return seg
		}
	}
	// Check if position is at the end of the last segment (valid for append position)
	if len(l.segments) > 0 {
		last := l.segments[len(l.segments)-1]
		info := last.Info()
		if position == info.BasePos+info.Size {
			return last
		}
	}
	return nil
}

// ReadAll reads all records from all segments
func (l *Log) ReadAll() ([]Record, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var allRecords []Record
	for _, seg := range l.segments {
		records, err := seg.ReadAll()
		if err != nil {
			return allRecords, err
		}
		allRecords = append(allRecords, records...)
	}

	return allRecords, nil
}

// Close closes all segment files
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, seg := range l.segments {
		if err := seg.CloseFile(); err != nil {
			return err
		}
	}
	return nil
}

// Segments returns info about all segments
func (l *Log) Segments() []SegmentInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()

	infos := make([]SegmentInfo, len(l.segments))
	for i, seg := range l.segments {
		infos[i] = seg.Info()
	}
	return infos
}

// ClosedSegments returns only closed (scavengable) segments
func (l *Log) ClosedSegments() []SegmentInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var closed []SegmentInfo
	for _, seg := range l.segments {
		info := seg.Info()
		if info.Closed {
			closed = append(closed, info)
		}
	}
	return closed
}

// Dir returns the log directory
func (l *Log) Dir() string {
	return l.dir
}
