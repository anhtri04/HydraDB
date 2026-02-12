package store

// Event represents a stored event with its metadata.
type Event struct {
	GlobalPosition int64  // Byte offset in the log file
	StreamID       string // Which stream this event belongs to
	StreamVersion  int64  // Version within the stream (0, 1, 2...)
	Data           []byte // The event payload
}
