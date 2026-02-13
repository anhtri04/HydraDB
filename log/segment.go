package log

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Segment represents a single log segment file
type Segment struct {
	mu       sync.RWMutex
	file     *os.File
	path     string
	id       int   // Segment number (0, 1, 2, ...)
	size     int64 // Current size in bytes
	closed   bool  // Once closed, no more writes
	basePos  int64 // Global position offset for this segment
}

// SegmentInfo contains metadata about a segment
type SegmentInfo struct {
	ID      int
	Path    string
	Size    int64
	Closed  bool
	BasePos int64
}

// OpenSegment opens or creates a segment file
func OpenSegment(dir string, id int, basePos int64) (*Segment, error) {
	path := filepath.Join(dir, fmt.Sprintf("hydra-%05d.log", id))

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	// Get current size
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	return &Segment{
		file:    file,
		path:    path,
		id:      id,
		size:    info.Size(),
		closed:  false,
		basePos: basePos,
	}, nil
}

// Append writes a record to the segment
// Returns the global position where the record was written
func (s *Segment) Append(data []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, fmt.Errorf("segment %d is closed", s.id)
	}

	// Seek to end
	offset, err := s.file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	// Calculate checksum
	checksum := crc32.ChecksumIEEE(data)

	// Write header: [4 bytes length][4 bytes checksum]
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(header[4:8], checksum)

	if _, err := s.file.Write(header); err != nil {
		return 0, err
	}

	// Write data
	if _, err := s.file.Write(data); err != nil {
		return 0, err
	}

	// Sync to disk
	if err := s.file.Sync(); err != nil {
		return 0, err
	}

	recordSize := int64(8 + len(data))
	s.size += recordSize

	// Return global position
	return s.basePos + offset, nil
}

// ReadAt reads a record at the given local offset within this segment
func (s *Segment) ReadAt(localOffset int64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Read header
	header := make([]byte, 8)
	if _, err := s.file.ReadAt(header, localOffset); err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(header[0:4])
	expectedChecksum := binary.LittleEndian.Uint32(header[4:8])

	// Read data
	data := make([]byte, length)
	if _, err := s.file.ReadAt(data, localOffset+8); err != nil {
		return nil, err
	}

	// Verify checksum
	actualChecksum := crc32.ChecksumIEEE(data)
	if actualChecksum != expectedChecksum {
		return nil, ErrCorruptRecord
	}

	return data, nil
}

// ReadAll reads all records from this segment
func (s *Segment) ReadAll() ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var records []Record
	var offset int64 = 0

	for offset < s.size {
		header := make([]byte, 8)
		if _, err := s.file.ReadAt(header, offset); err != nil {
			if err == io.EOF {
				break
			}
			return records, nil // Return what we have on error
		}

		length := binary.LittleEndian.Uint32(header[0:4])
		expectedChecksum := binary.LittleEndian.Uint32(header[4:8])

		data := make([]byte, length)
		if _, err := s.file.ReadAt(data, offset+8); err != nil {
			return records, nil
		}

		// Verify checksum
		actualChecksum := crc32.ChecksumIEEE(data)
		if actualChecksum != expectedChecksum {
			return records, nil // Stop at corruption
		}

		records = append(records, Record{
			Position: s.basePos + offset,
			Data:     data,
		})

		offset += 8 + int64(length)
	}

	return records, nil
}

// MarkClosed marks the segment as closed (no more writes) but keeps it readable
func (s *Segment) MarkClosed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// IsClosed returns whether the segment is closed for writes
func (s *Segment) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// Size returns the current segment size
func (s *Segment) Size() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size
}

// ID returns the segment ID
func (s *Segment) ID() int {
	return s.id
}

// Info returns segment metadata
func (s *Segment) Info() SegmentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SegmentInfo{
		ID:      s.id,
		Path:    s.path,
		Size:    s.size,
		Closed:  s.closed,
		BasePos: s.basePos,
	}
}

// CloseFile closes the underlying file handle
func (s *Segment) CloseFile() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

// Delete removes the segment file from disk
func (s *Segment) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.Close()
	return os.Remove(s.path)
}
