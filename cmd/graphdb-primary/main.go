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
	// work and routes writes to the default tenant. Refuse to start
	// unless explicitly opted in via GRAPHDB_LEGACY_BINARY=1 — same
	// fail-closed pattern as the JWT_SECRET fix in
	// pkg/api/server_init.go:74-77. Production deployments should
	// use cmd/server.
	if os.Getenv("GRAPHDB_LEGACY_BINARY") != "1" {
		log.Fatalf("graphdb-primary: this binary pre-dates the multi-tenant " +
			"work (audit A8) and is not safe for production. Use cmd/server. " +
			"Set GRAPHDB_LEGACY_BINARY=1 to run anyway for development/testing.")
	}

	dataDir := flag.String("data", "./data/primary", "Data directory")
	httpPort := flag.Int("http", 8080, "HTTP API port")
	replPort := flag.String("repl", ":9090", "Replication port")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - Primary Node\n")
	fmt.Printf("==============================\n\n")

	// Initialize storage with batched WAL
	fmt.Printf("📂 Initializing storage...\n")
	graph, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:        *dataDir,
		EnableBatching: true,
		BatchSize:      100,
		FlushInterval:  100 * time.Microsecond,
	})
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Initialize replication manager
	fmt.Printf("🔄 Starting replication manager...\n")
	replConfig := replication.DefaultReplicationConfig()
	replConfig.IsPrimary = true
	replConfig.ListenAddr = *replPort

	replMgr := replication.NewReplicationManager(replConfig, graph)
	if err := replMgr.Start(); err != nil {
		log.Fatalf("Failed to start replication: %v", err)
	}
	defer replMgr.Stop()

	// Start HTTP API
	fmt.Printf("🌐 Starting HTTP API on port %d...\n", *httpPort)
	go startHTTPServer(*httpPort, graph, replMgr)

	fmt.Printf("\n✅ Primary node started!\n")
	fmt.Printf("  HTTP API: http://localhost:%d\n", *httpPort)
	fmt.Printf("  Replication: %s\n", *replPort)
	fmt.Printf("  Data: %s\n\n", *dataDir)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\n👋 Shutting down...\n")
}

func startHTTPServer(port int, graph *storage.GraphStorage, replMgr *replication.ReplicationManager) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "role": "primary"})
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := graph.GetStatistics()
		json.NewEncoder(w).Encode(stats)
	})

	http.HandleFunc("/replication/status", func(w http.ResponseWriter, r *http.Request) {
		state := replMgr.GetReplicationState()
		json.NewEncoder(w).Encode(state)
	})

	http.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Labels     []string                 `json:"labels"`
				Properties map[string]storage.Value `json:"properties"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			node, err := graph.CreateNode(req.Labels, req.Properties)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			json.NewEncoder(w).Encode(node)
		}
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
