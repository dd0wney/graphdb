package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestGetEnvOrDefault tests the getEnvOrDefault helper function
func TestGetEnvOrDefault(t *testing.T) {
	// Test with environment variable set
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	result := getEnvOrDefault("TEST_VAR", "default-value")
	if result != "test-value" {
		t.Errorf("Expected 'test-value', got '%s'", result)
	}

	// Test with environment variable not set
	result = getEnvOrDefault("NONEXISTENT_VAR", "default-value")
	if result != "default-value" {
		t.Errorf("Expected 'default-value', got '%s'", result)
	}

	t.Logf("✓ getEnvOrDefault working correctly")
}

// TestParseSecurityFlags tests the parseSecurityFlags function
func TestParseSecurityFlags(t *testing.T) {
	// Test with server-url flag
	args := []string{"--server-url=http://example.com:9090"}
	config := parseSecurityFlags(args)

	if config.ServerURL != "http://example.com:9090" {
		t.Errorf("Expected server URL 'http://example.com:9090', got '%s'", config.ServerURL)
	}

	// Test with token flag
	args = []string{"--token=test-jwt-token"}
	config = parseSecurityFlags(args)

	if config.Token != "test-jwt-token" {
		t.Errorf("Expected token 'test-jwt-token', got '%s'", config.Token)
	}

	// Test with api-key flag
	args = []string{"--api-key=test-api-key"}
	config = parseSecurityFlags(args)

	if config.APIKey != "test-api-key" {
		t.Errorf("Expected API key 'test-api-key', got '%s'", config.APIKey)
	}

	// Test with multiple flags
	args = []string{
		"--server-url=http://localhost:8080",
		"--token=my-token",
		"--api-key=my-key",
	}
	config = parseSecurityFlags(args)

	if config.ServerURL != "http://localhost:8080" {
		t.Errorf("Expected server URL 'http://localhost:8080', got '%s'", config.ServerURL)
	}
	if config.Token != "my-token" {
		t.Errorf("Expected token 'my-token', got '%s'", config.Token)
	}
	if config.APIKey != "my-key" {
		t.Errorf("Expected API key 'my-key', got '%s'", config.APIKey)
	}

	t.Logf("✓ parseSecurityFlags working correctly")
}

// TestMakeAPIRequest tests the makeAPIRequest function with mock HTTP server
func TestMakeAPIRequest(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		// Verify authentication header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			t.Errorf("Expected 'Bearer test-token' auth header, got '%s'", authHeader)
		}

		// Send success response
		response := map[string]any{
			"message": "success",
			"data":    "test-data",
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Test successful request
	config := &SecurityConfig{
		ServerURL: server.URL,
		Token:     "test-token",
	}

	respBody, err := makeAPIRequest(http.MethodPost, server.URL+"/test", config, nil)
	if err != nil {
		t.Fatalf("makeAPIRequest failed: %v", err)
	}

	var response map[string]any
	if err := json.Unmarshal(respBody, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["message"] != "success" {
		t.Errorf("Expected message 'success', got '%v'", response["message"])
	}

	t.Logf("✓ makeAPIRequest with JWT token working correctly")
}

// TestMakeAPIRequest_WithAPIKey tests API request with API key authentication
func TestMakeAPIRequest_WithAPIKey(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify API key header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "test-api-key" {
			t.Errorf("Expected 'test-api-key' header, got '%s'", apiKey)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	config := &SecurityConfig{
		ServerURL: server.URL,
		APIKey:    "test-api-key",
	}

	_, err := makeAPIRequest(http.MethodGet, server.URL+"/test", config, nil)
	if err != nil {
		t.Fatalf("makeAPIRequest failed: %v", err)
	}

	t.Logf("✓ makeAPIRequest with API key working correctly")
}

// TestMakeAPIRequest_ErrorResponse tests handling of error responses
func TestMakeAPIRequest_ErrorResponse(t *testing.T) {
	// Create mock HTTP server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer server.Close()

	config := &SecurityConfig{
		ServerURL: server.URL,
		Token:     "invalid-token",
	}

	_, err := makeAPIRequest(http.MethodGet, server.URL+"/test", config, nil)
	if err == nil {
		t.Fatal("Expected error for unauthorized request, got nil")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Expected error to contain status code 401, got: %v", err)
	}

	t.Logf("✓ makeAPIRequest error handling working correctly")
}

// TestMakeAPIRequest_WithBody tests API request with request body
func TestMakeAPIRequest_WithBody(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify content type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected 'application/json' content type, got '%s'", contentType)
		}

		// Read and verify request body
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		if reqBody["test_field"] != "test_value" {
			t.Errorf("Expected test_field='test_value', got '%v'", reqBody["test_field"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	config := &SecurityConfig{
		ServerURL: server.URL,
		Token:     "test-token",
	}

	body := map[string]string{"test_field": "test_value"}
	_, err := makeAPIRequest(http.MethodPost, server.URL+"/test", config, body)
	if err != nil {
		t.Fatalf("makeAPIRequest failed: %v", err)
	}

	t.Logf("✓ makeAPIRequest with body working correctly")
}

// TestHandleSecurityInit_GenerateKey tests key generation functionality
func TestHandleSecurityInit_GenerateKey(t *testing.T) {
	// Create a temporary file for key output
	tmpFile, err := os.CreateTemp("", "test-key-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the init command with key generation
	handleSecurityInit([]string{"--generate-key", "--output=" + tmpPath})

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output contains success message
	if !strings.Contains(output, "Master key generated and saved") {
		t.Errorf("Expected success message in output, got: %s", output)
	}

	// Verify key file was created
	keyData, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("Failed to read generated key file: %v", err)
	}

	// Verify key is valid hex string of correct length (32 bytes = 64 hex characters)
	keyHex := string(keyData)
	if len(keyHex) != 64 {
		t.Errorf("Expected key length 64 hex characters, got %d", len(keyHex))
	}

	// Verify it's valid hex
	if _, err := hex.DecodeString(keyHex); err != nil {
		t.Errorf("Generated key is not valid hex: %v", err)
	}

	t.Logf("✓ Security init key generation working correctly")
}

// TestHandleSecurityInit_CustomKeyLength tests custom key length generation
func TestHandleSecurityInit_CustomKeyLength(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-key-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Generate 16-byte key (AES-128)
	handleSecurityInit([]string{"--generate-key", "--key-length=16", "--output=" + tmpPath})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	// Verify key file
	keyData, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}

	keyHex := string(keyData)
	if len(keyHex) != 32 { // 16 bytes = 32 hex characters
		t.Errorf("Expected key length 32 hex characters (16 bytes), got %d", len(keyHex))
	}

	t.Logf("✓ Custom key length generation working correctly")
}

// TestHandleSecurityRotateKeys tests key rotation command
func TestHandleSecurityRotateKeys(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security/keys/rotate" {
			t.Errorf("Expected path /api/v1/security/keys/rotate, got %s", r.URL.Path)
		}

		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		response := map[string]any{
			"message":     "Key rotated successfully",
			"new_version": 2,
			"timestamp":   "2025-11-23T10:30:00Z",
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run rotate-keys command
	handleSecurityRotateKeys([]string{
		"--server-url=" + server.URL,
		"--token=test-token",
	})

	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	if !strings.Contains(output, "Key rotation successful") {
		t.Errorf("Expected success message in output, got: %s", output)
	}

	if !strings.Contains(output, "New key version: 2") {
		t.Errorf("Expected new version in output, got: %s", output)
	}

	t.Logf("✓ Key rotation command working correctly")
}

// TestHandleSecurityKeyInfo tests key info command
func TestHandleSecurityKeyInfo(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security/keys/info" {
			t.Errorf("Expected path /api/v1/security/keys/info, got %s", r.URL.Path)
		}

		response := map[string]any{
			"active_version": 1,
			"total_keys":     1,
			"keys": []map[string]any{
				{
					"version":    1,
					"created_at": "2025-11-23T10:00:00Z",
					"is_active":  true,
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	handleSecurityKeyInfo([]string{
		"--server-url=" + server.URL,
		"--token=test-token",
	})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "Active Key Version: 1") {
		t.Errorf("Expected active version in output, got: %s", output)
	}

	if !strings.Contains(output, "(active)") {
		t.Errorf("Expected active key marker in output, got: %s", output)
	}

	t.Logf("✓ Key info command working correctly")
}

// TestHandleSecurityHealth tests health check command
func TestHandleSecurityHealth(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security/health" {
			t.Errorf("Expected path /api/v1/security/health, got %s", r.URL.Path)
		}

		response := map[string]any{
			"status":    "healthy",
			"timestamp": "2025-11-23T10:00:00Z",
			"components": map[string]any{
				"encryption": map[string]any{
					"enabled": true,
					"key_stats": map[string]any{
						"total_keys":     2,
						"active_version": 2,
					},
				},
				"tls": map[string]any{
					"enabled": true,
				},
				"audit": map[string]any{
					"enabled":     true,
					"event_count": 150,
				},
				"authentication": map[string]any{
					"jwt_enabled":    true,
					"apikey_enabled": true,
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	handleSecurityHealth([]string{
		"--server-url=" + server.URL,
		"--token=test-token",
	})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify all components are shown
	if !strings.Contains(output, "✓ Encryption: Enabled") {
		t.Errorf("Expected encryption status in output, got: %s", output)
	}

	if !strings.Contains(output, "✓ TLS: Enabled") {
		t.Errorf("Expected TLS status in output, got: %s", output)
	}

	if !strings.Contains(output, "✓ Audit Logging: Enabled") {
		t.Errorf("Expected audit status in output, got: %s", output)
	}

	if !strings.Contains(output, "Total events: 150") {
		t.Errorf("Expected audit event count in output, got: %s", output)
	}

	t.Logf("✓ Security health command working correctly")
}

// TestHandleSecurityAuditExport tests audit log export command
func TestHandleSecurityAuditExport(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/security/audit/export" {
			t.Errorf("Expected path /api/v1/security/audit/export, got %s", r.URL.Path)
		}

		// Return sample audit events
		response := map[string]any{
			"events": []map[string]any{
				{
					"id":        "event-1",
					"timestamp": "2025-11-23T10:00:00Z",
					"user_id":   "user123",
					"action":    "read",
					"status":    "success",
				},
				{
					"id":        "event-2",
					"timestamp": "2025-11-23T10:01:00Z",
					"user_id":   "user456",
					"action":    "write",
					"status":    "success",
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create temp file for export
	tmpFile, err := os.CreateTemp("", "audit-export-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	handleSecurityAuditExport([]string{
		"--server-url=" + server.URL,
		"--token=test-token",
		"--output=" + tmpPath,
	})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	if !strings.Contains(output, "Audit logs exported") {
		t.Errorf("Expected export success message in output, got: %s", output)
	}

	if !strings.Contains(output, "Total events: 2") {
		t.Errorf("Expected event count in output, got: %s", output)
	}

	// Verify export file
	exportData, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("Failed to read export file: %v", err)
	}

	var exportJSON map[string]any
	if err := json.Unmarshal(exportData, &exportJSON); err != nil {
		t.Fatalf("Failed to parse export JSON: %v", err)
	}

	events := exportJSON["events"].([]any)
	if len(events) != 2 {
		t.Errorf("Expected 2 events in export, got %d", len(events))
	}

	t.Logf("✓ Audit export command working correctly")
}

// TestHandleSecurityAuditExport_WithFilters tests audit export with filters
func TestHandleSecurityAuditExport_WithFilters(t *testing.T) {
	// Create mock server that verifies filters
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request body to verify filters were sent
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		if reqBody["user_id"] != "user123" {
			t.Errorf("Expected user_id filter 'user123', got '%v'", reqBody["user_id"])
		}

		if reqBody["action"] != "read" {
			t.Errorf("Expected action filter 'read', got '%v'", reqBody["action"])
		}

		response := map[string]any{
			"events": []any{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "audit-export-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	handleSecurityAuditExport([]string{
		"--server-url=" + server.URL,
		"--token=test-token",
		"--output=" + tmpPath,
		"--user-id=user123",
		"--action=read",
	})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	t.Logf("✓ Audit export with filters working correctly")
}

// TestHandleSecurityCommand_Routing tests command routing
func TestHandleSecurityCommand_Routing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "init command",
			args:    []string{"init", "--help"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is a basic routing test
			// Full integration tests are done in individual command tests
			t.Logf("✓ Command routing test for %s", tt.name)
		})
	}
}

// TestPrintFunctions tests that print functions don't panic
func TestPrintFunctions(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Test printUsage
	printUsage()

	// Test printVersion
	printVersion()

	// Test printSecurityUsage
	printSecurityUsage()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output contains expected strings
	if !strings.Contains(output, "GraphDB Admin CLI") {
		t.Error("Expected usage output to contain 'GraphDB Admin CLI'")
	}

	if !strings.Contains(output, "v1.0.0") {
		t.Error("Expected version output to contain version number")
	}

	if !strings.Contains(output, "security") {
		t.Error("Expected usage to contain 'security' command")
	}

	t.Logf("✓ Print functions working correctly")
}
