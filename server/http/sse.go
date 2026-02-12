package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hydra-db/hydra/pubsub"
	"github.com/hydra-db/hydra/store"
)

// SSEHandler handles Server-Sent Events subscriptions
type SSEHandler struct {
	store       *store.Store
	broadcaster *pubsub.Broadcaster
}

// NewSSEHandler creates a new SSE handler
func NewSSEHandler(s *store.Store, b *pubsub.Broadcaster) *SSEHandler {
	return &SSEHandler{store: s, broadcaster: b}
}

// SubscribeAll handles GET /subscribe/all?from=N
// Implements catch-up subscription: reads historical events then switches to live
func (h *SSEHandler) SubscribeAll(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Parse from parameter
	var fromPosition int64 = 0
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		var err error
		fromPosition, err = strconv.ParseInt(fromStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid 'from' parameter", http.StatusBadRequest)
			return
		}
	}

	// Subscribe to live events first (to buffer during catch-up)
	sub := h.broadcaster.Subscribe(nil, 1000)
	defer h.broadcaster.Unsubscribe(sub.ID)

	// Phase 1: Catch-up - send historical events
	events, err := h.store.ReadAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var lastPosition int64 = -1
	for _, event := range events {
		if event.GlobalPosition < fromPosition {
			continue
		}
		h.sendSSEEvent(w, event)
		flusher.Flush()
		lastPosition = event.GlobalPosition
	}

	// Phase 2: Live - stream new events
	for {
		select {
		case event, ok := <-sub.EventChan:
			if !ok {
				return // Channel closed
			}
			// Skip events we already sent during catch-up
			if event.GlobalPosition <= lastPosition {
				continue
			}
			h.sendSSEEventFromPubsub(w, event)
			flusher.Flush()
			lastPosition = event.GlobalPosition

		case <-r.Context().Done():
			return // Client disconnected
		}
	}
}

// SubscribeStream handles GET /subscribe/streams/{streamId}?from=N
func (h *SSEHandler) SubscribeStream(w http.ResponseWriter, r *http.Request, streamID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Parse from parameter (stream version)
	var fromVersion int64 = 0
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		var err error
		fromVersion, err = strconv.ParseInt(fromStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid 'from' parameter", http.StatusBadRequest)
			return
		}
	}

	// Subscribe to live events for this stream
	sub := h.broadcaster.Subscribe(&streamID, 1000)
	defer h.broadcaster.Unsubscribe(sub.ID)

	// Phase 1: Catch-up
	var events []store.Event
	var err error
	if fromVersion > 0 {
		events, err = h.store.ReadStreamFrom(streamID, fromVersion)
	} else {
		events, err = h.store.ReadStream(streamID)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var lastVersion int64 = fromVersion - 1
	for _, event := range events {
		h.sendSSEEvent(w, event)
		flusher.Flush()
		lastVersion = event.StreamVersion
	}

	// Phase 2: Live
	for {
		select {
		case event, ok := <-sub.EventChan:
			if !ok {
				return
			}
			if event.StreamVersion <= lastVersion {
				continue
			}
			h.sendSSEEventFromPubsub(w, event)
			flusher.Flush()
			lastVersion = event.StreamVersion

		case <-r.Context().Done():
			return
		}
	}
}

func (h *SSEHandler) sendSSEEvent(w http.ResponseWriter, event store.Event) {
	resp := EventResponse{
		Position:      event.GlobalPosition,
		StreamID:      event.StreamID,
		StreamVersion: event.StreamVersion,
		Data:          event.Data,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (h *SSEHandler) sendSSEEventFromPubsub(w http.ResponseWriter, event pubsub.Event) {
	resp := EventResponse{
		Position:      event.GlobalPosition,
		StreamID:      event.StreamID,
		StreamVersion: event.StreamVersion,
		Data:          event.Data,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "data: %s\n\n", data)
}
