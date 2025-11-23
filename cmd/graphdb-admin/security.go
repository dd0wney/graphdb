package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// SecurityConfig holds configuration for security commands
type SecurityConfig struct {
	ServerURL string
	Token     string
	APIKey    string
}

func handleSecurityCommand(args []string) {
	if len(args) == 0 {
		printSecurityUsage()
		os.Exit(1)
	}

	subcommand := args[0]

	switch subcommand {
	case "init":
		handleSecurityInit(args[1:])
	case "rotate-keys":
		handleSecurityRotateKeys(args[1:])
	case "audit-export":
		handleSecurityAuditExport(args[1:])
	case "health":
		handleSecurityHealth(args[1:])
	case "key-info":
		handleSecurityKeyInfo(args[1:])
	case "--help", "-h":
		printSecurityUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown security subcommand: %s\n\n", subcommand)
		printSecurityUsage()
		os.Exit(1)
	}
}

// parseSecurityFlags parses common security command flags
func parseSecurityFlags(args []string) *SecurityConfig {
	fs := flag.NewFlagSet("security", flag.ExitOnError)
	config := &SecurityConfig{}

	fs.StringVar(&config.ServerURL, "server-url", getEnvOrDefault("GRAPHDB_SERVER_URL", "http://localhost:8080"), "GraphDB server URL")
	fs.StringVar(&config.Token, "token", os.Getenv("GRAPHDB_TOKEN"), "JWT token for authentication")
	fs.StringVar(&config.APIKey, "api-key", os.Getenv("GRAPHDB_API_KEY"), "API key for authentication")

	fs.Parse(args)

	return config
}

// makeAPIRequest makes an HTTP request to the GraphDB API
func makeAPIRequest(method, url string, config *SecurityConfig, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	if config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+config.Token)
	} else if config.APIKey != "" {
		req.Header.Set("X-API-Key", config.APIKey)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API request failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// handleSecurityInit initializes security features
func handleSecurityInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	generateKey := fs.Bool("generate-key", false, "Generate a new master encryption key")
	keyLength := fs.Int("key-length", 32, "Length of master key in bytes (default: 32 for AES-256)")
	output := fs.String("output", "", "Output file for generated key (default: stdout)")

	fs.Parse(args)

	fmt.Println("=== GraphDB Security Initialization ===")
	fmt.Println()

	if *generateKey {
		// Generate a new master key
		masterKey := make([]byte, *keyLength)
		if _, err := rand.Read(masterKey); err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to generate master key: %v\n", err)
			os.Exit(1)
		}

		keyHex := hex.EncodeToString(masterKey)

		if *output != "" {
			// Write to file
			if err := os.WriteFile(*output, []byte(keyHex), 0600); err != nil {
				fmt.Fprintf(os.Stderr, "Error: Failed to write key to file: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Master key generated and saved to: %s\n", *output)
			fmt.Println()
			fmt.Println("IMPORTANT: Keep this key secure! Without it, encrypted data cannot be recovered.")
			fmt.Printf("Set environment variable: ENCRYPTION_MASTER_KEY=%s\n", keyHex)
		} else {
			// Print to stdout
			fmt.Println("Generated Master Encryption Key:")
			fmt.Println(keyHex)
			fmt.Println()
			fmt.Println("IMPORTANT: Save this key securely! Without it, encrypted data cannot be recovered.")
			fmt.Println()
			fmt.Println("To use this key, set the environment variable:")
			fmt.Printf("export ENCRYPTION_MASTER_KEY=%s\n", keyHex)
		}
	} else {
		// Show initialization instructions
		fmt.Println("Security Features:")
		fmt.Println("  ✓ Encryption at Rest")
		fmt.Println("  ✓ TLS/SSL Transport Security")
		fmt.Println("  ✓ JWT Authentication")
		fmt.Println("  ✓ API Key Authentication")
		fmt.Println("  ✓ Audit Logging")
		fmt.Println("  ✓ Input Validation")
		fmt.Println()
		fmt.Println("Quick Start:")
		fmt.Println("  1. Generate master encryption key:")
		fmt.Println("     graphdb-admin security init --generate-key")
		fmt.Println()
		fmt.Println("  2. Set environment variables:")
		fmt.Println("     export ENCRYPTION_ENABLED=true")
		fmt.Println("     export ENCRYPTION_MASTER_KEY=<generated-key>")
		fmt.Println()
		fmt.Println("  3. (Optional) Enable TLS:")
		fmt.Println("     export TLS_ENABLED=true")
		fmt.Println("     export TLS_AUTO_GENERATE=true")
		fmt.Println()
		fmt.Println("  4. Start the server:")
		fmt.Println("     ./bin/server")
		fmt.Println()
		fmt.Println("For more details, see docs/SECURITY-QUICKSTART.md")
	}
}

// handleSecurityRotateKeys rotates encryption keys
func handleSecurityRotateKeys(args []string) {
	config := parseSecurityFlags(args)

	if config.Token == "" && config.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: Authentication required. Provide --token or --api-key")
		os.Exit(1)
	}

	fmt.Println("=== Rotating Encryption Keys ===")
	fmt.Println()

	url := config.ServerURL + "/api/v1/security/keys/rotate"
	respBody, err := makeAPIRequest("POST", url, config, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(respBody, &response); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Key rotation successful")
	fmt.Printf("  New key version: %v\n", response["new_version"])
	fmt.Printf("  Timestamp: %v\n", response["timestamp"])
	fmt.Println()
	fmt.Println("Note: Old keys are retained for decrypting existing data.")
	fmt.Println("New data will be encrypted with the new key version.")
}

// handleSecurityAuditExport exports audit logs
func handleSecurityAuditExport(args []string) {
	fs := flag.NewFlagSet("audit-export", flag.ExitOnError)

	// Define all flags first
	config := &SecurityConfig{}
	fs.StringVar(&config.ServerURL, "server-url", getEnvOrDefault("GRAPHDB_SERVER_URL", "http://localhost:8080"), "GraphDB server URL")
	fs.StringVar(&config.Token, "token", os.Getenv("GRAPHDB_TOKEN"), "JWT token for authentication")
	fs.StringVar(&config.APIKey, "api-key", os.Getenv("GRAPHDB_API_KEY"), "API key for authentication")

	output := fs.String("output", "audit-export.json", "Output file for audit logs")
	userID := fs.String("user-id", "", "Filter by user ID")
	action := fs.String("action", "", "Filter by action")
	startTime := fs.String("start-time", "", "Filter by start time (RFC3339 format)")
	endTime := fs.String("end-time", "", "Filter by end time (RFC3339 format)")

	fs.Parse(args)

	if config.Token == "" && config.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: Authentication required. Provide --token or --api-key")
		os.Exit(1)
	}

	fmt.Println("=== Exporting Audit Logs ===")
	fmt.Println()

	// Build export request
	exportReq := map[string]interface{}{
		"format": "json",
	}

	if *userID != "" {
		exportReq["user_id"] = *userID
	}
	if *action != "" {
		exportReq["action"] = *action
	}
	if *startTime != "" {
		exportReq["start_time"] = *startTime
	}
	if *endTime != "" {
		exportReq["end_time"] = *endTime
	}

	url := config.ServerURL + "/api/v1/security/audit/export"
	respBody, err := makeAPIRequest("POST", url, config, exportReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Write to file
	if err := os.WriteFile(*output, respBody, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to write to file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Audit logs exported to: %s\n", *output)

	// Parse and show summary
	var exportData map[string]interface{}
	if err := json.Unmarshal(respBody, &exportData); err == nil {
		if events, ok := exportData["events"].([]interface{}); ok {
			fmt.Printf("  Total events: %d\n", len(events))
		}
	}
}

// handleSecurityHealth checks security health status
func handleSecurityHealth(args []string) {
	config := parseSecurityFlags(args)

	if config.Token == "" && config.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: Authentication required. Provide --token or --api-key")
		os.Exit(1)
	}

	fmt.Println("=== Security Health Check ===")
	fmt.Println()

	url := config.ServerURL + "/api/v1/security/health"
	respBody, err := makeAPIRequest("GET", url, config, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var health map[string]interface{}
	if err := json.Unmarshal(respBody, &health); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Status: %v\n", health["status"])
	fmt.Printf("Timestamp: %v\n", health["timestamp"])
	fmt.Println()

	if components, ok := health["components"].(map[string]interface{}); ok {
		fmt.Println("Security Components:")

		// Encryption
		if enc, ok := components["encryption"].(map[string]interface{}); ok {
			enabled := enc["enabled"].(bool)
			if enabled {
				fmt.Println("  ✓ Encryption: Enabled")
				if keyStats, ok := enc["key_stats"].(map[string]interface{}); ok {
					fmt.Printf("    - Total keys: %v\n", keyStats["total_keys"])
					fmt.Printf("    - Active version: %v\n", keyStats["active_version"])
				}
			} else {
				fmt.Println("  ✗ Encryption: Disabled")
			}
		}

		// TLS
		if tls, ok := components["tls"].(map[string]interface{}); ok {
			enabled := tls["enabled"].(bool)
			if enabled {
				fmt.Println("  ✓ TLS: Enabled")
			} else {
				fmt.Println("  ✗ TLS: Disabled")
			}
		}

		// Audit
		if audit, ok := components["audit"].(map[string]interface{}); ok {
			enabled := audit["enabled"].(bool)
			if enabled {
				fmt.Println("  ✓ Audit Logging: Enabled")
				fmt.Printf("    - Total events: %v\n", audit["event_count"])
			} else {
				fmt.Println("  ✗ Audit Logging: Disabled")
			}
		}

		// Authentication
		if auth, ok := components["authentication"].(map[string]interface{}); ok {
			jwtEnabled := auth["jwt_enabled"].(bool)
			apikeyEnabled := auth["apikey_enabled"].(bool)
			if jwtEnabled {
				fmt.Println("  ✓ JWT Authentication: Enabled")
			}
			if apikeyEnabled {
				fmt.Println("  ✓ API Key Authentication: Enabled")
			}
		}
	}
}

// handleSecurityKeyInfo displays encryption key information
func handleSecurityKeyInfo(args []string) {
	config := parseSecurityFlags(args)

	if config.Token == "" && config.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: Authentication required. Provide --token or --api-key")
		os.Exit(1)
	}

	fmt.Println("=== Encryption Key Information ===")
	fmt.Println()

	url := config.ServerURL + "/api/v1/security/keys/info"
	respBody, err := makeAPIRequest("GET", url, config, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var keyInfo map[string]interface{}
	if err := json.Unmarshal(respBody, &keyInfo); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Active Key Version: %v\n", keyInfo["active_version"])
	fmt.Printf("Total Keys: %v\n", keyInfo["total_keys"])
	fmt.Println()

	if keys, ok := keyInfo["keys"].([]interface{}); ok {
		fmt.Println("Key History:")
		for _, k := range keys {
			if key, ok := k.(map[string]interface{}); ok {
				version := key["version"]
				createdAt := key["created_at"]
				isActive := key["is_active"]

				status := ""
				if isActive == true {
					status = " (active)"
				}

				fmt.Printf("  Version %v: Created %v%s\n", version, createdAt, status)
			}
		}
	}

	fmt.Println()
	fmt.Println("Note: Key rotation is recommended every 90 days for compliance.")
}

// getEnvOrDefault returns environment variable value or default
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
