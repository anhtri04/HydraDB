package http

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hydra-db/hydra/pubsub"
	"github.com/hydra-db/hydra/store"
)

// Server is the HTTP server for the event store
type Server struct {
	httpServer *http.Server
	handlers   *Handlers
	sseHandler *SSEHandler
}

// NewServer creates a new HTTP server
func NewServer(s *store.Store, b *pubsub.Broadcaster, port int) *Server {
	handlers := NewHandlers(s)
	sseHandler := NewSSEHandler(s, b)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/streams/", handlers.StreamsHandler)
	mux.HandleFunc("/all", handlers.ReadAll)

	// SSE subscription endpoints
	mux.HandleFunc("/subscribe/all", sseHandler.SubscribeAll)
	mux.HandleFunc("/subscribe/streams/", func(w http.ResponseWriter, r *http.Request) {
		streamID := strings.TrimPrefix(r.URL.Path, "/subscribe/streams/")
		if streamID == "" {
			http.Error(w, "stream ID required", http.StatusBadRequest)
			return
		}
		sseHandler.SubscribeStream(w, r, streamID)
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 0, // No timeout for SSE (long-lived connections)
		},
		handlers:   handlers,
		sseHandler: sseHandler,
	}
}

// Start starts the HTTP server (blocking)
func (s *Server) Start() error {
	log.Printf("HTTP server starting on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the server address
func (s *Server) Addr() string {
	return s.httpServer.Addr
}
