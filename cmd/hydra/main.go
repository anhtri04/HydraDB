package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hydra-db/hydra/store"
	grpcserver "github.com/hydra-db/hydra/server/grpc"
	httpserver "github.com/hydra-db/hydra/server/http"
)

func main() {
	// Open store
	s, err := store.Open("hydra.log")
	if err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer s.Close()

	// Create servers
	httpSrv := httpserver.NewServer(s, 8080)
	grpcSrv := grpcserver.NewServer(s, 9090)

	// Start HTTP server in goroutine
	go func() {
		if err := httpSrv.Start(); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Start gRPC server in goroutine
	go func() {
		if err := grpcSrv.Start(); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	log.Println("Hydra Event Store started")
	log.Println("  HTTP: http://localhost:8080")
	log.Println("  gRPC: localhost:9090")

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	httpSrv.Shutdown(ctx)
	grpcSrv.Stop()

	log.Println("Shutdown complete")
}
