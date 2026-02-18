//go:build nng
// +build nng

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	dataDir := flag.String("data", "./data/nng-primary", "Data directory")
	httpPort := flag.Int("http", 8080, "HTTP API port")
	addDatacenter := flag.String("add-dc", "", "Add datacenter link (format: dc-id:endpoint)")
	flag.Parse()

	fmt.Printf("Cluso GraphDB - NNG Primary Node\n")
	fmt.Printf("=================================\n\n")

	// Initialize storage with batched WAL
	fmt.Printf("Initializing storage...\n")
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

	// Initialize NNG replication manager
	fmt.Printf("Starting NNG replication manager...\n")
	replConfig := replication.DefaultReplicationConfig()
	replConfig.IsPrimary = true

	nngMgr, err := replication.NewNNGReplicationManager(replConfig, graph)
	if err != nil {
		log.Fatalf("Failed to create replication manager: %v", err)
	}

	if err := nngMgr.Start(); err != nil {
		log.Fatalf("Failed to start replication: %v", err)
	}
	defer nngMgr.Stop()

	// Add datacenter link if specified
	if *addDatacenter != "" {
		fmt.Printf("Adding datacenter link: %s\n", *addDatacenter)
		// Parse dc-id:endpoint
		parts := strings.SplitN(*addDatacenter, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			log.Fatalf("Invalid datacenter link format. Expected dc-id:endpoint, got: %s", *addDatacenter)
		}
		dcID, endpoint := parts[0], parts[1]

		// Reconstruct endpoint with port if it was split
		// Handle case like "dc1:tcp://192.168.1.1:9090"
		if strings.HasPrefix(endpoint, "//") {
			// Already has protocol stripped, find next colon for port
			endpoint = parts[1]
		} else if !strings.Contains(endpoint, "://") {
			// No protocol, assume tcp://
			endpoint = "tcp://" + endpoint
		}

		if err := nngMgr.AddDatacenterLink(dcID, endpoint); err != nil {
			log.Fatalf("Failed to add datacenter link: %v", err)
		}
		fmt.Printf("  Datacenter %s -> %s\n", dcID, endpoint)
	}

	// Start HTTP API
	fmt.Printf("Starting HTTP API on port %d...\n", *httpPort)
	go startHTTPServer(*httpPort, graph, nngMgr)

	fmt.Printf("\nNNG Primary node started!\n")
	fmt.Printf("  HTTP API: http://localhost:%d\n", *httpPort)
	fmt.Printf("  WAL Publisher: tcp://*:9090 (PUB/SUB)\n")
	fmt.Printf("  Health Surveyor: tcp://*:9091 (SURVEYOR/RESPONDENT)\n")
	fmt.Printf("  Write Buffer: tcp://*:9092 (PUSH/PULL)\n")
	fmt.Printf("  Data: %s\n\n", *dataDir)

	fmt.Printf("NNG Architecture (pure Go - no CGO):\n")
	fmt.Printf("  PUB/SUB: WAL streaming to N replicas (fan-out)\n")
	fmt.Printf("  SURVEYOR/RESPONDENT: Broadcast health checks to all replicas\n")
	fmt.Printf("  PUSH/PULL: Load-balanced write buffering\n\n")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\nShutting down...\n")
}

func startHTTPServer(port int, graph *storage.GraphStorage, nngMgr *replication.NNGReplicationManager) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "healthy",
			"role":      "primary",
			"transport": "nng",
		})
	})

	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := graph.GetStatistics()
		json.NewEncoder(w).Encode(stats)
	})

	http.HandleFunc("/replication/status", func(w http.ResponseWriter, r *http.Request) {
		state := nngMgr.GetReplicationState()
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
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
