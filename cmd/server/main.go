package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/api"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	port := flag.Int("port", 0, "HTTP server port (default 8080, or set PORT)")
	dataDir := flag.String("data", "./data/server", "Data directory")
	flag.Parse()

	// Get port from env if not provided
	if *port == 0 {
		if envPort := os.Getenv("PORT"); envPort != "" {
			if p, err := strconv.Atoi(envPort); err == nil {
				*port = p
			} else {
				*port = 8080
			}
		} else {
			*port = 8080
		}
	}

	// Structured logging (Railway best practice)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("Cluso GraphDB Server starting")

	// Create graph storage
	logger.Info("initializing graph storage", "data_dir", *dataDir)
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		logger.Error("failed to create graph storage", "error", err)
		os.Exit(1)
	}
	defer graph.Close()

	stats := graph.GetStatistics()
	logger.Info("graph storage initialized",
		"nodes", stats.NodeCount,
		"edges", stats.EdgeCount,
	)

	// Create and start API server
	server := api.NewServer(graph, *port)

	// Handle graceful shutdown (Railway best practice)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("shutting down server")

		// Give time for graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Wait for shutdown or timeout
		<-ctx.Done()

		// Close graph storage
		graph.Close()
		logger.Info("server exited")
		os.Exit(0)
	}()

	// Start server
	logger.Info("server starting", "port", *port)
	if err := server.Start(); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
