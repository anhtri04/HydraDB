package log

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)

const (
	// LengthSize is the size of the record length field in bytes (uint32)
	LengthSize = 4
	// ChecksumSize is the size of the CRC32 checksum field in bytes
	ChecksumSize = 4
	// HeaderSize is the total size of the record header (length + checksum)
	HeaderSize = LengthSize + ChecksumSize
)

// byteOrder defines the byte order for encoding integers
var byteOrder = binary.BigEndian

// ErrCorruptRecord indicates a record failed checksum validation
var ErrCorruptRecord = errors.New("corrupt record: checksum mismatch")

// Log is an append-only log that stores records in a file.
// Each record is stored as: [4-byte length][4-byte CRC32][data]
type Log struct {
	file *os.File
}

// Open opens or creates an append-only log file at the given path.
func Open(path string) (*Log, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	return &Log{file: f}, nil
}

// Close closes the log file.
func (l *Log) Close() error {
	return l.file.Close()
}

// Append writes a record to the log and returns its byte position.
// The record is stored as: [4-byte length][4-byte CRC32][data]
func (l *Log) Append(data []byte) (pos int64, err error) {
	// Get current position (end of file)
	pos, err = l.file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	// Build the record: [length][checksum][data]
	length := uint32(len(data))
	checksum := crc32.ChecksumIEEE(data)

	// Write length
	if err := binary.Write(l.file, byteOrder, length); err != nil {
		return 0, err
	}

	// Write checksum
	if err := binary.Write(l.file, byteOrder, checksum); err != nil {
		return 0, err
	}

	// Write data
	if _, err := l.file.Write(data); err != nil {
		return 0, err
	}

	// Sync to disk for durability
	if err := l.file.Sync(); err != nil {
		return 0, err
	}

	return pos, nil
}

// ReadAt reads a single record at the given byte position.
// Returns ErrCorruptRecord if the checksum doesn't match.
func (l *Log) ReadAt(pos int64) ([]byte, error) {
	// Seek to position
	if _, err := l.file.Seek(pos, io.SeekStart); err != nil {
		return nil, err
	}

	// Read length
	var length uint32
	if err := binary.Read(l.file, byteOrder, &length); err != nil {
		return nil, err
	}

	// Read stored checksum
	var storedChecksum uint32
	if err := binary.Read(l.file, byteOrder, &storedChecksum); err != nil {
		return nil, err
	}

	// Read data
	data := make([]byte, length)
	if _, err := io.ReadFull(l.file, data); err != nil {
		return nil, err
	}

	// Verify checksum
	computedChecksum := crc32.ChecksumIEEE(data)
	if computedChecksum != storedChecksum {
		return nil, ErrCorruptRecord
	}

	return data, nil
}
