package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hydra-db/hydra/pubsub"
	"github.com/hydra-db/hydra/store"
	grpcserver "github.com/hydra-db/hydra/server/grpc"
	httpserver "github.com/hydra-db/hydra/server/http"
)

// buildDurabilityConfig creates a DurabilityConfig from CLI flags
func buildDurabilityConfig(mode string, interval time.Duration, batchSize int) store.DurabilityConfig {
	switch mode {
	case "async":
		return store.WithAsync(interval, batchSize)
	case "every-second":
		return store.DurabilityConfig{
			SyncMode:      store.SyncEverySecond,
			SyncInterval:  time.Second,
			SyncBatchSize: batchSize,
		}
	case "every-write":
		return store.DefaultDurabilityConfig()
	default:
		log.Printf("Unknown sync-mode '%s', using default (every-write)\n", mode)
		return store.DefaultDurabilityConfig()
	}
}

func main() {
	// CLI flags for durability configuration
	var (
		syncMode       = flag.String("sync-mode", "every-write", "Durability mode: every-write, async, every-second")
		syncInterval   = flag.Duration("sync-interval", 10*time.Millisecond, "Max time between syncs (for async mode)")
		syncBatchSize  = flag.Int("sync-batch-size", 1000, "Max events between syncs (for async mode)")
	)
	flag.Parse()

	// Build durability config from flags
	config := buildDurabilityConfig(*syncMode, *syncInterval, *syncBatchSize)

	// Open store with durability config
	s, err := store.Open("hydra.log", store.WithDurability(config))
	if err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer s.Close()

	// Create broadcaster for real-time subscriptions
	broadcaster := pubsub.NewBroadcaster()
	s.SetBroadcaster(broadcaster)

	// Create servers
	httpSrv := httpserver.NewServer(s, broadcaster, 8080)
	grpcSrv := grpcserver.NewServer(s, broadcaster, 9090)

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
	log.Printf("  Durability: %s (interval: %v, batch: %d)\n", *syncMode, config.SyncInterval, config.SyncBatchSize)
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
