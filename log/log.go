package log

import (
	"encoding/binary"
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
