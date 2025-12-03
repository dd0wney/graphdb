//go:build zmq
// +build zmq

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

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	dataDir := flag.String("data", "./data/zmq-replica1", "Data directory")
	httpPort := flag.Int("http", 8081, "HTTP API port")
	primaryAddr := flag.String("primary", "localhost:9090", "Primary node address")
	replicaID := flag.String("id", "", "Replica ID (auto-generated if empty)")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - ZeroMQ Replica Node\n")
	fmt.Printf("======================================\n\n")

	// Initialize storage (read-only mode)
	fmt.Printf("📂 Initializing storage...\n")
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Initialize ZeroMQ replica node
	fmt.Printf("🔄 Connecting to ZeroMQ primary at %s...\n", *primaryAddr)
	replConfig := replication.DefaultReplicationConfig()
	replConfig.IsPrimary = false
	replConfig.PrimaryAddr = *primaryAddr
	replConfig.ReplicaID = *replicaID

	zmqReplica, err := replication.NewZMQReplicaNode(replConfig, graph)
	if err != nil {
		log.Fatalf("Failed to create replica: %v", err)
	}

	if err := zmqReplica.Start(); err != nil {
		log.Fatalf("Failed to start replica: %v", err)
	}
	defer zmqReplica.Stop()

	// Start HTTP API (read-only)
	fmt.Printf("🌐 Starting HTTP API on port %d (read-only)...\n", *httpPort)
	go startHTTPServer(*httpPort, graph, zmqReplica)

	fmt.Printf("\n✅ ZeroMQ Replica node started!\n")
	fmt.Printf("  HTTP API: http://localhost:%d\n", *httpPort)
	fmt.Printf("  Primary: %s\n", *primaryAddr)
	fmt.Printf("  Data: %s\n\n", *dataDir)

	fmt.Printf("📊 ZeroMQ Subscriptions:\n")
	fmt.Printf("  SUB: WAL stream from tcp://%s\n", *primaryAddr)
	fmt.Printf("  DEALER: Health checks to tcp://%s:9091\n", extractHost(*primaryAddr))
	fmt.Printf("  PUSH: Write forwarding to tcp://%s:9092\n\n", extractHost(*primaryAddr))

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\n👋 Shutting down...\n")
}

func startHTTPServer(port int, graph *storage.GraphStorage, replica *replication.ZMQReplicaNode) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		state := replica.GetReplicationState()
		connected := "disconnected"
		if state.CurrentLSN > 0 {
			connected = "connected"
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":    "healthy",
			"role":      "replica",
			"transport": "zeromq",
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

		json.NewEncoder(w).Encode([]any{})
	})

	addr := fmt.Sprintf(":%d", port)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func extractHost(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
