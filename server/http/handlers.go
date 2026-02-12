package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/hydra-db/hydra/store"
)

// Handlers holds the HTTP handlers and their dependencies
type Handlers struct {
	store *store.Store
}

// NewHandlers creates a new Handlers instance
func NewHandlers(s *store.Store) *Handlers {
	return &Handlers{store: s}
}

// Health handles GET /health
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}

// AppendEvent handles POST /streams/{streamId}
func (h *Handlers) AppendEvent(w http.ResponseWriter, r *http.Request, streamID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AppendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	result, err := h.store.Append(streamID, req.EventID, req.Data, req.ExpectedVersion)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		switch err {
		case store.ErrWrongExpectedVersion:
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error:          "wrong expected version",
				CurrentVersion: h.store.StreamVersion(streamID),
			})
		case store.ErrStreamExists:
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "stream already exists"})
		case store.ErrStreamNotFound:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "stream not found"})
		default:
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AppendResponse{
		Position: result.Position,
		Version:  result.Version,
	})
}

// ReadStream handles GET /streams/{streamId}
// Streams events as NDJSON (newline-delimited JSON)
func (h *Handlers) ReadStream(w http.ResponseWriter, r *http.Request, streamID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check for ?from=N parameter
	var fromVersion int64 = 0
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		var err error
		fromVersion, err = strconv.ParseInt(fromStr, 10, 64)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid 'from' parameter"})
			return
		}
	}

	var events []store.Event
	var err error
	if fromVersion > 0 {
		events, err = h.store.ReadStreamFrom(streamID, fromVersion)
	} else {
		events, err = h.store.ReadStream(streamID)
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	// Stream as NDJSON
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	encoder := json.NewEncoder(w)

	for _, event := range events {
		resp := EventResponse{
			Position:      event.GlobalPosition,
			StreamID:      event.StreamID,
			StreamVersion: event.StreamVersion,
			Data:          event.Data,
		}
		encoder.Encode(resp)
		if canFlush {
			flusher.Flush()
		}
	}
}

// ReadAll handles GET /all
// Streams all events in global order as NDJSON
func (h *Handlers) ReadAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	events, err := h.store.ReadAll()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	// Stream as NDJSON
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	encoder := json.NewEncoder(w)

	for _, event := range events {
		resp := EventResponse{
			Position:      event.GlobalPosition,
			StreamID:      event.StreamID,
			StreamVersion: event.StreamVersion,
			Data:          event.Data,
		}
		encoder.Encode(resp)
		if canFlush {
			flusher.Flush()
		}
	}
}

// StreamsHandler routes /streams/* requests
func (h *Handlers) StreamsHandler(w http.ResponseWriter, r *http.Request) {
	// Extract streamId from path: /streams/alice -> "alice"
	streamID := strings.TrimPrefix(r.URL.Path, "/streams/")
	if streamID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "stream ID required"})
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.AppendEvent(w, r, streamID)
	case http.MethodGet:
		h.ReadStream(w, r, streamID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
