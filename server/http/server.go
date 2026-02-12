package http

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/hydra-db/hydra/store"
)

// Server is the HTTP server for the event store
type Server struct {
	httpServer *http.Server
	handlers   *Handlers
}

// NewServer creates a new HTTP server
func NewServer(s *store.Store, port int) *Server {
	handlers := NewHandlers(s)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/streams/", handlers.StreamsHandler)
	mux.HandleFunc("/all", handlers.ReadAll)

	return &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		handlers: handlers,
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
