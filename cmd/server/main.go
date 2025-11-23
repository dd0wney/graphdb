package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/api"
	"github.com/dd0wney/cluso-graphdb/pkg/editions"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
	"github.com/dd0wney/cluso-graphdb/pkg/plugins"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
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

// loadTLSConfig loads TLS configuration from environment variables
func loadTLSConfig(logger *slog.Logger) *tlspkg.Config {
	// Check if TLS is enabled
	tlsEnabled := os.Getenv("TLS_ENABLED")
	if tlsEnabled != "true" && tlsEnabled != "1" {
		return nil
	}

	logger.Info("TLS enabled, loading configuration")

	cfg := tlspkg.DefaultConfig()
	cfg.Enabled = true

	// Certificate files
	if certFile := os.Getenv("TLS_CERT_FILE"); certFile != "" {
		cfg.CertFile = certFile
	}
	if keyFile := os.Getenv("TLS_KEY_FILE"); keyFile != "" {
		cfg.KeyFile = keyFile
	}
	if caFile := os.Getenv("TLS_CA_FILE"); caFile != "" {
		cfg.CAFile = caFile
	}

	// Auto-generation settings
	if autoGen := os.Getenv("TLS_AUTO_GENERATE"); autoGen == "false" || autoGen == "0" {
		cfg.AutoGenerate = false
	}

	// Hosts for certificate generation
	if hosts := os.Getenv("TLS_HOSTS"); hosts != "" {
		cfg.Hosts = strings.Split(hosts, ",")
		// Trim whitespace from each host
		for i := range cfg.Hosts {
			cfg.Hosts[i] = strings.TrimSpace(cfg.Hosts[i])
		}
	}

	// Organization for certificate generation
	if org := os.Getenv("TLS_ORGANIZATION"); org != "" {
		cfg.Organization = org
	}

	// Minimum TLS version
	if minVer := os.Getenv("TLS_MIN_VERSION"); minVer != "" {
		switch minVer {
		case "1.2":
			cfg.MinVersion = tls.VersionTLS12
		case "1.3":
			cfg.MinVersion = tls.VersionTLS13
		default:
			logger.Warn("invalid TLS_MIN_VERSION, using default (1.2)", "value", minVer)
		}
	}

	// Client authentication mode
	if clientAuth := os.Getenv("TLS_CLIENT_AUTH"); clientAuth != "" {
		switch strings.ToLower(clientAuth) {
		case "none":
			cfg.ClientAuth = tls.NoClientCert
		case "request":
			cfg.ClientAuth = tls.RequestClientCert
		case "required":
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		case "verify":
			cfg.ClientAuth = tls.VerifyClientCertIfGiven
		default:
			logger.Warn("invalid TLS_CLIENT_AUTH, using default (none)", "value", clientAuth)
		}
	}

	// Insecure skip verify (NOT for production)
	if skipVerify := os.Getenv("TLS_INSECURE_SKIP_VERIFY"); skipVerify == "true" || skipVerify == "1" {
		cfg.InsecureSkipVerify = true
		logger.Warn("TLS certificate verification disabled - NOT FOR PRODUCTION")
	}

	return cfg
}

// encodeHexKey encodes a byte slice to hex string
func encodeHexKey(key []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(key)*2)
	for i, b := range key {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}
	return string(result)
}

// decodeHexKey decodes a hex string to byte slice
func decodeHexKey(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex string must have even length")
	}

	result := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi := hexCharToByte(s[i])
		lo := hexCharToByte(s[i+1])
		if hi == 255 || lo == 255 {
			return nil, fmt.Errorf("invalid hex character in key")
		}
		result[i/2] = hi<<4 | lo
	}
	return result, nil
}

// hexCharToByte converts a hex character to its byte value
func hexCharToByte(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	default:
		return 255 // invalid
	}
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

	// Initialize license validation with server-based licensing
	licenseKey := os.Getenv("GRAPHDB_LICENSE_KEY")
	licenseServerURL := os.Getenv("LICENSE_SERVER_URL")
	if licenseServerURL == "" {
		licenseServerURL = "https://license.graphdb.com"
	}

	licensing.InitGlobal(licenseKey, licenseServerURL)
	currentLicense := licensing.Global().GetLicense()

	logger.Info("Cluso GraphDB Server starting",
		"edition", editions.Current.String(),
		"license_tier", currentLicense.Tier,
		"license_valid", currentLicense.IsValid(),
		"features", len(editions.GetEnabledFeatures()),
	)

	// Initialize encryption engine and key manager BEFORE storage creation
	// This ensures encrypted snapshots can be decrypted during storage initialization
	var encryptionEngine *encryption.Engine
	var keyManager *encryption.KeyManager

	encryptionEnabled := os.Getenv("ENCRYPTION_ENABLED")
	if encryptionEnabled == "true" || encryptionEnabled == "1" {
		logger.Info("encryption enabled, initializing encryption engine")

		// Get or generate master key
		masterKeyHex := os.Getenv("ENCRYPTION_MASTER_KEY")
		var masterKey []byte
		var err error

		if masterKeyHex != "" {
			// Decode from hex
			masterKey, err = decodeHexKey(masterKeyHex)
			if err != nil {
				logger.Error("failed to decode master key", "error", err)
				os.Exit(1)
			}
			logger.Info("master key loaded from environment")
		} else {
			// Generate new master key
			masterKey, err = encryption.GenerateKey()
			if err != nil {
				logger.Error("failed to generate master key", "error", err)
				os.Exit(1)
			}
			logger.Warn("generated new master key - save ENCRYPTION_MASTER_KEY for production")
			logger.Warn("master key (hex):", "key", encodeHexKey(masterKey))
		}

		// Create encryption engine
		encryptionEngine, err = encryption.NewEngine(masterKey)
		if err != nil {
			logger.Error("failed to create encryption engine", "error", err)
			os.Exit(1)
		}

		// Create key manager
		keyDir := os.Getenv("ENCRYPTION_KEY_DIR")
		if keyDir == "" {
			keyDir = "./data/keys"
		}

		keyManager, err = encryption.NewKeyManager(encryption.KeyManagerConfig{
			KeyDir:    keyDir,
			MasterKey: masterKey,
		})
		if err != nil {
			logger.Error("failed to create key manager", "error", err)
			os.Exit(1)
		}

		// Generate initial KEK if none exists
		if keyManager.GetActiveVersion() == 0 {
			version, err := keyManager.GenerateKEK()
			if err != nil {
				logger.Error("failed to generate initial KEK", "error", err)
				os.Exit(1)
			}
			logger.Info("generated initial encryption key", "version", version)
		}

		stats := keyManager.GetStatistics()
		logger.Info("encryption initialized",
			"total_keys", stats.TotalKeys,
			"active_version", stats.ActiveVersion,
		)
	} else {
		logger.Info("encryption disabled - set ENCRYPTION_ENABLED=true to enable")
	}

	// Create graph storage (will load from disk if snapshot exists)
	logger.Info("initializing graph storage", "data_dir", *dataDir)
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		logger.Error("failed to create graph storage", "error", err)
		os.Exit(1)
	}
	defer graph.Close()

	// Connect encryption to storage layer for transparent encryption
	if encryptionEngine != nil && keyManager != nil {
		graph.SetEncryption(encryptionEngine, keyManager)
		logger.Info("encryption connected to storage layer")
	}

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

		// Load Enterprise plugins
		pluginDir := os.Getenv("GRAPHDB_PLUGIN_DIR")
		if pluginDir == "" {
			pluginDir = "./plugins"
		}

		pluginLoader := plugins.NewPluginLoader(license, logger)
		ctx := context.Background()

		if err := pluginLoader.LoadPluginsFromDir(ctx, pluginDir); err != nil {
			logger.Error("failed to load plugins", "error", err)
			// Continue without plugins - they're optional
		}

		// Start all loaded plugins
		if err := pluginLoader.StartAll(ctx); err != nil {
			logger.Error("failed to start plugins", "error", err)
		}

		// Defer plugin shutdown
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := pluginLoader.StopAll(shutdownCtx); err != nil {
				logger.Error("failed to stop plugins", "error", err)
			}
		}()

		logger.Info("Enterprise plugins loaded", "count", len(pluginLoader.GetAllPlugins()))

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

	// Load TLS configuration
	tlsConfig := loadTLSConfig(logger)
	if tlsConfig != nil {
		logger.Info("TLS configuration loaded",
			"auto_generate", tlsConfig.AutoGenerate,
			"client_auth", tlsConfig.ClientAuth,
		)
	}

	// Create and start API server
	server := api.NewServer(graph, *port)

	// Apply TLS configuration if enabled
	if tlsConfig != nil {
		server.SetTLSConfig(tlsConfig)
	}

	// Apply encryption configuration if enabled
	if encryptionEngine != nil && keyManager != nil {
		server.SetEncryption(encryptionEngine, keyManager)
		logger.Info("encryption configured for API server")
	}

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

		// Stop license manager
		licensing.Global().Stop()

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
