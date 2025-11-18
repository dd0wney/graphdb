package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/api"
	"github.com/dd0wney/cluso-graphdb/pkg/editions"
	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// loadLicense attempts to load a license from various sources
func loadLicense(logger *slog.Logger) (*licensing.License, error) {
	// 1. Try loading from environment variable (JSON format)
	if licenseJSON := os.Getenv("GRAPHDB_LICENSE"); licenseJSON != "" {
		logger.Info("loading license from GRAPHDB_LICENSE environment variable")
		var license licensing.License
		if err := json.Unmarshal([]byte(licenseJSON), &license); err != nil {
			return nil, fmt.Errorf("failed to parse license JSON: %w", err)
		}
		return &license, nil
	}

	// 2. Try loading from license key (simple format)
	if licenseKey := os.Getenv("GRAPHDB_LICENSE_KEY"); licenseKey != "" {
		logger.Info("loading license from GRAPHDB_LICENSE_KEY environment variable")

		// Simple license object with key only
		// In production, this should call the license server to validate and get full details
		license := &licensing.License{
			Key:    licenseKey,
			Status: "active",
			Type:   licensing.LicenseTypeEnterprise,
		}

		// Basic key format validation
		if !licensing.ValidateLicenseKey(licenseKey) {
			return nil, fmt.Errorf("invalid license key format")
		}

		return license, nil
	}

	// 3. Try loading from license file
	licensePaths := []string{
		os.Getenv("GRAPHDB_LICENSE_PATH"),
		"/etc/graphdb/license.key",
		"./license.key",
		"./config/license.key",
		"./license.json",
	}

	for _, path := range licensePaths {
		if path == "" {
			continue
		}

		if _, err := os.Stat(path); err == nil {
			logger.Info("loading license from file", "path", path)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to read license file %s: %w", path, err)
			}

			// Try parsing as JSON first
			var license licensing.License
			if err := json.Unmarshal(data, &license); err == nil {
				return &license, nil
			}

			// If not JSON, treat as plain license key
			licenseKey := string(data)
			if licensing.ValidateLicenseKey(licenseKey) {
				return &licensing.License{
					Key:    licenseKey,
					Status: "active",
					Type:   licensing.LicenseTypeEnterprise,
				}, nil
			}

			return nil, fmt.Errorf("invalid license format in %s", path)
		}
	}

	return nil, fmt.Errorf("no license found (tried env vars GRAPHDB_LICENSE, GRAPHDB_LICENSE_KEY, and standard paths)")
}

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

	// Initialize edition detection
	editions.Initialize()

	// Structured logging (Railway best practice)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("Cluso GraphDB Server starting",
		"edition", editions.Current.String(),
		"features", len(editions.GetEnabledFeatures()),
	)

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

	// License validation for Enterprise edition
	var license *licensing.License
	var telemetryReporter *licensing.TelemetryReporter

	if editions.IsEnterprise() {
		logger.Info("Enterprise edition detected, validating license")

		// Load license from file or environment variable
		license, err = loadLicense(logger)
		if err != nil {
			logger.Error("failed to load license", "error", err)
			logger.Error("Enterprise edition requires a valid license")
			logger.Info("To use GraphDB without a license, set GRAPHDB_EDITION=community")
			os.Exit(1)
		}

		// Validate license
		if err := license.Validate(); err != nil {
			logger.Error("license validation failed", "error", err)
			logger.Error("Enterprise edition requires a valid license")
			logger.Info("To use GraphDB without a license, set GRAPHDB_EDITION=community")
			os.Exit(1)
		}

		logger.Info("license validated successfully",
			"type", license.Type,
			"email", license.Email,
		)

		// Initialize telemetry (opt-in via GRAPHDB_ENABLE_TELEMETRY=true)
		if os.Getenv("GRAPHDB_ENABLE_TELEMETRY") == "true" {
			telemetryReporter = licensing.NewTelemetryReporter(true, 24*time.Hour)
			telemetryReporter.Start(license, func() (int64, int64) {
				stats := graph.GetStatistics()
				return int64(stats.NodeCount), int64(stats.EdgeCount)
			})
			logger.Info("telemetry reporting enabled")

			// Defer stopping telemetry
			defer func() {
				if telemetryReporter != nil {
					telemetryReporter.Stop()
				}
			}()
		}
	} else {
		logger.Info("Community edition - no license required")
	}

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
