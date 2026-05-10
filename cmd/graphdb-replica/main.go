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
	// Audit A8 (2026-05-09): this binary pre-dates the multi-tenant
	// work and serves /nodes unauthenticated across all tenants.
	// Refuse to start unless explicitly opted in via
	// GRAPHDB_LEGACY_BINARY=1 — same fail-closed pattern as the
	// JWT_SECRET fix in pkg/api/server_init.go:74-77. Production
	// deployments should use cmd/server.
	if os.Getenv("GRAPHDB_LEGACY_BINARY") != "1" {
		log.Fatalf("graphdb-replica: this binary pre-dates the multi-tenant " +
			"work (audit A8) and is not safe for production. Use cmd/server. " +
			"Set GRAPHDB_LEGACY_BINARY=1 to run anyway for development/testing.")
	}

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

// buildHTTPHandler registers the replica's read-only HTTP surface on a
// fresh *http.ServeMux. Used by main and exercised in server_test.go to
// pin the route set (audit A8.2: /nodes must NOT be registered — see
// docs/A8_REPLICATION_TENANCY_DESIGN.md §1.3).
func buildHTTPHandler(replica *replication.ReplicaNode, graph *storage.GraphStorage) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := graph.GetStatistics()
		json.NewEncoder(w).Encode(stats)
	})

	mux.HandleFunc("/replication/status", func(w http.ResponseWriter, r *http.Request) {
		state := replica.GetReplicationState()
		json.NewEncoder(w).Encode(state)
	})

	// A8.2: /nodes was previously registered here returning
	// graph.GetAllNodesAcrossTenants() with no auth — any caller could
	// dump every tenant's node corpus. The route is removed (rather than
	// guarded) because replication uses the WAL stream, not HTTP; this
	// route was inspection-only. Any future read-API on the replica
	// should ride cmd/server's middleware stack (see A8.1).

	return mux
}

func startHTTPServer(port int, graph *storage.GraphStorage, replica *replication.ReplicaNode) {
	mux := buildHTTPHandler(replica, graph)
	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
