package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hydra-db/hydra/store"
	httpserver "github.com/hydra-db/hydra/server/http"
)

func main() {
	// Open store
	s, err := store.Open("hydra.log")
	if err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer s.Close()

	// Create and start HTTP server
	httpSrv := httpserver.NewServer(s, 8080)

	// Handle shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	log.Fatal(httpSrv.Start())
}
