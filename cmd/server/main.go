package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dd0wney/cluso-graphdb/pkg/api"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server port")
	dataDir := flag.String("data", "./data/server", "Data directory")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB Server\n")
	fmt.Printf("=======================\n\n")

	// Create graph storage
	fmt.Printf("📂 Initializing graph storage at %s...\n", *dataDir)
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create graph storage: %v", err)
	}
	defer graph.Close()

	fmt.Printf("✅ Graph storage initialized\n")
	fmt.Printf("   Nodes: %d\n", graph.GetStatistics().NodeCount)
	fmt.Printf("   Edges: %d\n\n", graph.GetStatistics().EdgeCount)

	// Create and start API server
	server := api.NewServer(graph, *port)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Printf("\n\n🛑 Shutting down server...\n")
		graph.Close()
		os.Exit(0)
	}()

	// Start server
	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
