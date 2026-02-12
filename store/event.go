package store

import (
	"encoding/binary"
	"errors"
)

var (
	// ErrInvalidEvent indicates the event data is malformed
	ErrInvalidEvent = errors.New("invalid event format")

	// ErrWrongExpectedVersion is returned when optimistic concurrency check fails
	ErrWrongExpectedVersion = errors.New("wrong expected version")

	// ErrStreamExists is returned when creating a stream that already exists
	ErrStreamExists = errors.New("stream already exists")

	// ErrStreamNotFound is returned when stream doesn't exist but should
	ErrStreamNotFound = errors.New("stream not found")
)

// ExpectedVersion constants for optimistic concurrency control
const (
	// ExpectedVersionAny allows append regardless of current version
	ExpectedVersionAny int64 = -1

	// ExpectedVersionNoStream requires the stream to not exist (for creation)
	ExpectedVersionNoStream int64 = 0

	// ExpectedVersionStreamExists requires the stream to exist (any version)
	ExpectedVersionStreamExists int64 = -2
)

// AppendResult contains the result of a successful append operation
type AppendResult struct {
	Position int64 // Byte offset in the log file
	Version  int64 // New stream version after append
}

// Event represents a stored event with its metadata.
type Event struct {
	GlobalPosition int64  // Byte offset in the log file
	StreamID       string // Which stream this event belongs to
	StreamVersion  int64  // Version within the stream (0, 1, 2...)
	Data           []byte // The event payload
}

// Envelope format: [2 bytes streamID len][streamID][2 bytes eventID len][eventID][data]

// serializeEnvelope wraps data with StreamID and EventID for storage.
func serializeEnvelope(streamID, eventID string, data []byte) []byte {
	streamIDBytes := []byte(streamID)
	eventIDBytes := []byte(eventID)
	streamIDLen := uint16(len(streamIDBytes))
	eventIDLen := uint16(len(eventIDBytes))

	// Format: [2 bytes streamID len][streamID][2 bytes eventID len][eventID][data]
	totalLen := 2 + len(streamIDBytes) + 2 + len(eventIDBytes) + len(data)
	buf := make([]byte, totalLen)

	offset := 0
	binary.BigEndian.PutUint16(buf[offset:offset+2], streamIDLen)
	offset += 2
	copy(buf[offset:offset+len(streamIDBytes)], streamIDBytes)
	offset += len(streamIDBytes)
	binary.BigEndian.PutUint16(buf[offset:offset+2], eventIDLen)
	offset += 2
	copy(buf[offset:offset+len(eventIDBytes)], eventIDBytes)
	offset += len(eventIDBytes)
	copy(buf[offset:], data)

	return buf
}

// deserializeEnvelope extracts StreamID, EventID, and data from stored bytes.
func deserializeEnvelope(raw []byte) (streamID, eventID string, data []byte, err error) {
	if len(raw) < 4 { // Minimum: 2 bytes streamID len + 2 bytes eventID len
		return "", "", nil, ErrInvalidEvent
	}

	offset := 0

	// Read streamID
	streamIDLen := binary.BigEndian.Uint16(raw[offset : offset+2])
	offset += 2
	if len(raw) < offset+int(streamIDLen)+2 {
		return "", "", nil, ErrInvalidEvent
	}
	streamID = string(raw[offset : offset+int(streamIDLen)])
	offset += int(streamIDLen)

	// Read eventID
	eventIDLen := binary.BigEndian.Uint16(raw[offset : offset+2])
	offset += 2
	if len(raw) < offset+int(eventIDLen) {
		return "", "", nil, ErrInvalidEvent
	}
	eventID = string(raw[offset : offset+int(eventIDLen)])
	offset += int(eventIDLen)

	// Remaining is data
	data = raw[offset:]

	return streamID, eventID, data, nil
}
