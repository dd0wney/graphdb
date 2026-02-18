package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	dataDir := flag.String("data", "./data/replica1", "Data directory")
	httpPort := flag.Int("http", 8081, "HTTP API port")
	primaryAddr := flag.String("primary", "localhost:9090", "Primary node address")
	replicaID := flag.String("id", "", "Replica ID (auto-generated if empty)")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - Replica Node\n")
	fmt.Printf("===============================\n\n")

	// Initialize storage (read-only mode)
	fmt.Printf("📂 Initializing storage...\n")
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Initialize replica node
	fmt.Printf("🔄 Connecting to primary at %s...\n", *primaryAddr)
	replConfig := replication.DefaultReplicationConfig()
	replConfig.IsPrimary = false
	replConfig.PrimaryAddr = *primaryAddr
	replConfig.ReplicaID = *replicaID

	replica := replication.NewReplicaNode(replConfig, graph)
	if err := replica.Start(); err != nil {
		log.Fatalf("Failed to start replica: %v", err)
	}
	defer replica.Stop()

	// Start HTTP API (read-only)
	fmt.Printf("🌐 Starting HTTP API on port %d (read-only)...\n", *httpPort)
	go startHTTPServer(*httpPort, graph, replica)

	fmt.Printf("\n✅ Replica node started!\n")
	fmt.Printf("  HTTP API: http://localhost:%d\n", *httpPort)
	fmt.Printf("  Primary: %s\n", *primaryAddr)
	fmt.Printf("  Data: %s\n\n", *dataDir)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\n👋 Shutting down...\n")
}

func startHTTPServer(port int, graph *storage.GraphStorage, replica *replication.ReplicaNode) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		state := replica.GetReplicationState()
		connected := "disconnected"
		if state.CurrentLSN > 0 {
			connected = "connected"
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":    "healthy",
			"role":      "replica",
			"connected": connected,
		})
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := graph.GetStatistics()
		json.NewEncoder(w).Encode(stats)
	})

	http.HandleFunc("/replication/status", func(w http.ResponseWriter, r *http.Request) {
		state := replica.GetReplicationState()
		json.NewEncoder(w).Encode(state)
	})

	// Read-only endpoints
	http.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Replica is read-only", http.StatusMethodNotAllowed)
			return
		}

		nodes := graph.GetAllNodes()
		json.NewEncoder(w).Encode(nodes)
	})

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:         addr,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
