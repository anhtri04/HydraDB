package http

// AppendRequest is the JSON body for appending an event
type AppendRequest struct {
	EventID         string `json:"eventId"`
	EventType       string `json:"eventType,omitempty"`
	Data            []byte `json:"data"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

// AppendResponse is returned after successful append
type AppendResponse struct {
	Position int64 `json:"position"`
	Version  int64 `json:"version"`
}

// EventResponse represents an event in API responses
type EventResponse struct {
	Position      int64  `json:"position"`
	StreamID      string `json:"streamId"`
	StreamVersion int64  `json:"version"`
	Data          []byte `json:"data"`
}

// ErrorResponse is returned for errors
type ErrorResponse struct {
	Error          string `json:"error"`
	CurrentVersion int64  `json:"currentVersion,omitempty"`
}

// HealthResponse is returned by health check
type HealthResponse struct {
	Status string `json:"status"`
}
