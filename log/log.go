package log

import (
	"encoding/binary"
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
