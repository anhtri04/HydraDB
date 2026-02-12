package store

import (
	"encoding/binary"
	"errors"
)

var (
	// ErrInvalidEvent indicates the event data is malformed
	ErrInvalidEvent = errors.New("invalid event format")
)

// Event represents a stored event with its metadata.
type Event struct {
	GlobalPosition int64  // Byte offset in the log file
	StreamID       string // Which stream this event belongs to
	StreamVersion  int64  // Version within the stream (0, 1, 2...)
	Data           []byte // The event payload
}

// envelope is the on-disk format: [streamID length][streamID bytes][data]
// This allows us to extract StreamID without parsing the user's data.

// serializeEnvelope wraps data with StreamID for storage.
func serializeEnvelope(streamID string, data []byte) []byte {
	streamIDBytes := []byte(streamID)
	streamIDLen := uint16(len(streamIDBytes))

	// Format: [2 bytes streamID length][streamID bytes][data bytes]
	buf := make([]byte, 2+len(streamIDBytes)+len(data))
	binary.BigEndian.PutUint16(buf[0:2], streamIDLen)
	copy(buf[2:2+len(streamIDBytes)], streamIDBytes)
	copy(buf[2+len(streamIDBytes):], data)

	return buf
}

// deserializeEnvelope extracts StreamID and data from stored bytes.
func deserializeEnvelope(raw []byte) (streamID string, data []byte, err error) {
	if len(raw) < 2 {
		return "", nil, ErrInvalidEvent
	}

	streamIDLen := binary.BigEndian.Uint16(raw[0:2])
	if len(raw) < 2+int(streamIDLen) {
		return "", nil, ErrInvalidEvent
	}

	streamID = string(raw[2 : 2+streamIDLen])
	data = raw[2+streamIDLen:]

	return streamID, data, nil
}
