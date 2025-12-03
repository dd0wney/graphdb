package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
)

// setupSecurityTestServer creates a test server with security features enabled
func setupSecurityTestServer(t *testing.T) (*Server, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	server, err := NewServer(gs, 8080)
	if err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create server: %v", err)
	}

	// Initialize encryption key manager
	masterKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	masterKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		fmt.Sscanf(masterKeyHex[i*2:i*2+2], "%02x", &masterKey[i])
	}

	keyDir := tmpDir + "/keys"
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create key directory: %v", err)
	}

	keyManager, err := encryption.NewKeyManager(encryption.KeyManagerConfig{
		KeyDir:    keyDir,
		MasterKey: masterKey,
	})
	if err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create key manager: %v", err)
	}

	// Generate an initial encryption key
	if _, err := keyManager.GenerateKEK(); err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to generate encryption key: %v", err)
	}

	// Get the active KEK for creating the encryption engine
	activeKey, _, err := keyManager.GetActiveKEK()
	if err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to get active KEK: %v", err)
	}

	// Create encryption engine using the active key
	encryptionEngine, err := encryption.NewEngine(activeKey)
	if err != nil {
		gs.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create encryption engine: %v", err)
	}

	server.keyManager = keyManager
	server.encryptionEngine = encryptionEngine

	// Initialize TLS config for testing
	server.tlsConfig = &tlspkg.Config{
		Enabled: true,
	}

	// Initialize audit logger (already done in NewServer, but verify)
	if server.auditLogger == nil {
		server.auditLogger = audit.NewAuditLogger(1000)
	}

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tmpDir)
	}

	return server, cleanup
}

// TestHandleSecurityKeyRotate tests the POST /api/v1/security/keys/rotate endpoint
func TestHandleSecurityKeyRotate(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Test successful key rotation
	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/keys/rotate", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityKeyRotate(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["message"] != "Key rotated successfully" {
		t.Errorf("Expected success message, got %v", response["message"])
	}

	if _, ok := response["new_version"]; !ok {
		t.Error("Expected new_version in response")
	}

	if _, ok := response["timestamp"]; !ok {
		t.Error("Expected timestamp in response")
	}

	t.Logf("✓ Key rotation successful: new_version=%v", response["new_version"])
}

// TestHandleSecurityKeyRotate_MethodNotAllowed tests invalid HTTP method
func TestHandleSecurityKeyRotate_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/keys/rotate", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityKeyRotate(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}

	t.Logf("✓ Method validation working correctly")
}

// TestHandleSecurityKeyRotate_EncryptionDisabled tests behavior when encryption is disabled
func TestHandleSecurityKeyRotate_EncryptionDisabled(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "security-test-*")
	defer os.RemoveAll(tmpDir)

	gs, _ := storage.NewGraphStorage(tmpDir)
	defer gs.Close()

	server, err := NewServer(gs, 8080)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	// Don't set keyManager - simulate encryption disabled

	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/keys/rotate", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityKeyRotate(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}

	t.Logf("✓ Encryption disabled check working correctly")
}

// TestHandleSecurityKeyInfo tests the GET /api/v1/security/keys/info endpoint
func TestHandleSecurityKeyInfo(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/keys/info", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityKeyInfo(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := response["statistics"]; !ok {
		t.Error("Expected statistics in response")
	}

	if _, ok := response["keys"]; !ok {
		t.Error("Expected keys in response")
	}

	t.Logf("✓ Key info retrieved successfully")
}

// TestHandleSecurityKeyInfo_MethodNotAllowed tests invalid HTTP method
func TestHandleSecurityKeyInfo_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/keys/info", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityKeyInfo(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}

	t.Logf("✓ Method validation working correctly")
}

// TestHandleSecurityKeyInfo_EncryptionDisabled tests behavior when encryption is disabled
func TestHandleSecurityKeyInfo_EncryptionDisabled(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "security-test-*")
	defer os.RemoveAll(tmpDir)

	gs, _ := storage.NewGraphStorage(tmpDir)
	defer gs.Close()

	server, err := NewServer(gs, 8080)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/keys/info", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityKeyInfo(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}

	t.Logf("✓ Encryption disabled check working correctly")
}

// TestHandleSecurityAuditLogs tests the GET /api/v1/security/audit/logs endpoint
func TestHandleSecurityAuditLogs(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add some test audit events
	server.auditLogger.Log(&audit.Event{
		ID:           "test-1",
		Timestamp:    time.Now(),
		UserID:       "user123",
		Username:     "testuser",
		Action:       audit.ActionRead,
		ResourceType: audit.ResourceNode,
		ResourceID:   "node-1",
		Status:       audit.StatusSuccess,
	})

	server.auditLogger.Log(&audit.Event{
		ID:           "test-2",
		Timestamp:    time.Now(),
		UserID:       "user456",
		Username:     "admin",
		Action:       audit.ActionUpdate,
		ResourceType: audit.ResourceEdge,
		ResourceID:   "edge-1",
		Status:       audit.StatusFailure,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/logs", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	count, ok := response["count"].(float64)
	if !ok {
		t.Fatal("Expected count field in response")
	}

	if count != 2 {
		t.Errorf("Expected 2 events, got %v", count)
	}

	events, ok := response["events"].([]any)
	if !ok {
		t.Fatal("Expected events array in response")
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events in array, got %d", len(events))
	}

	t.Logf("✓ Audit logs retrieved successfully: %d events", len(events))
}

// TestHandleSecurityAuditLogs_WithFilters tests audit log filtering
func TestHandleSecurityAuditLogs_WithFilters(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add test events
	server.auditLogger.Log(&audit.Event{
		ID:           "test-1",
		Timestamp:    time.Now(),
		UserID:       "user123",
		Username:     "testuser",
		Action:       audit.ActionRead,
		ResourceType: audit.ResourceNode,
		Status:       audit.StatusSuccess,
	})

	server.auditLogger.Log(&audit.Event{
		ID:           "test-2",
		Timestamp:    time.Now(),
		UserID:       "user456",
		Username:     "admin",
		Action:       audit.ActionUpdate,
		ResourceType: audit.ResourceEdge,
		Status:       audit.StatusSuccess,
	})

	// Filter by user_id
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/logs?user_id=user123", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]any
	json.Unmarshal(rr.Body.Bytes(), &response)

	events := response["events"].([]any)
	if len(events) != 1 {
		t.Errorf("Expected 1 filtered event, got %d", len(events))
	}

	t.Logf("✓ Audit log filtering working correctly")
}

// TestHandleSecurityAuditLogs_WithLimit tests audit log limit parameter
func TestHandleSecurityAuditLogs_WithLimit(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add 5 test events
	for i := 0; i < 5; i++ {
		server.auditLogger.Log(&audit.Event{
			ID:           fmt.Sprintf("test-%d", i),
			Timestamp:    time.Now(),
			UserID:       fmt.Sprintf("user%d", i),
			Action:       audit.ActionRead,
			ResourceType: audit.ResourceNode,
			Status:       audit.StatusSuccess,
		})
	}

	// Request with limit=3
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/logs?limit=3", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]any
	json.Unmarshal(rr.Body.Bytes(), &response)

	events := response["events"].([]any)
	if len(events) != 3 {
		t.Errorf("Expected 3 limited events, got %d", len(events))
	}

	t.Logf("✓ Audit log limit working correctly")
}

// TestHandleSecurityAuditLogs_MethodNotAllowed tests invalid HTTP method
func TestHandleSecurityAuditLogs_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/audit/logs", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}

	t.Logf("✓ Method validation working correctly")
}

// TestHandleSecurityAuditExport tests the POST /api/v1/security/audit/export endpoint
func TestHandleSecurityAuditExport(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add test audit events
	server.auditLogger.Log(&audit.Event{
		ID:           "export-test-1",
		Timestamp:    time.Now(),
		UserID:       "user123",
		Username:     "testuser",
		Action:       audit.ActionRead,
		ResourceType: audit.ResourceNode,
		Status:       audit.StatusSuccess,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/audit/export", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityAuditExport(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	// Check Content-Type header
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Check Content-Disposition header
	disposition := rr.Header().Get("Content-Disposition")
	if disposition == "" {
		t.Error("Expected Content-Disposition header")
	}

	// Verify response is valid JSON
	var events []any
	if err := json.Unmarshal(rr.Body.Bytes(), &events); err != nil {
		t.Fatalf("Failed to parse exported JSON: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("Expected 1 exported event, got %d", len(events))
	}

	t.Logf("✓ Audit log export successful: %d events", len(events))
}

// TestHandleSecurityAuditExport_MethodNotAllowed tests invalid HTTP method
func TestHandleSecurityAuditExport_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/export", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityAuditExport(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}

	t.Logf("✓ Method validation working correctly")
}

// TestHandleSecurityHealth tests the GET /api/v1/security/health endpoint
func TestHandleSecurityHealth(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/health", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify top-level fields
	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", response["status"])
	}

	if _, ok := response["timestamp"]; !ok {
		t.Error("Expected timestamp in response")
	}

	// Verify components
	components, ok := response["components"].(map[string]any)
	if !ok {
		t.Fatal("Expected components map in response")
	}

	// Check encryption component
	encryption, ok := components["encryption"].(map[string]any)
	if !ok {
		t.Fatal("Expected encryption component")
	}

	if encryption["enabled"] != true {
		t.Error("Expected encryption to be enabled")
	}

	// Check TLS component
	tls, ok := components["tls"].(map[string]any)
	if !ok {
		t.Fatal("Expected tls component")
	}

	if tls["enabled"] != true {
		t.Error("Expected TLS to be enabled")
	}

	// Check audit component
	auditComp, ok := components["audit"].(map[string]any)
	if !ok {
		t.Fatal("Expected audit component")
	}

	if auditComp["enabled"] != true {
		t.Error("Expected audit to be enabled")
	}

	// Check authentication component
	auth, ok := components["authentication"].(map[string]any)
	if !ok {
		t.Fatal("Expected authentication component")
	}

	if auth["jwt_enabled"] != true {
		t.Error("Expected JWT to be enabled")
	}

	if auth["apikey_enabled"] != true {
		t.Error("Expected API key to be enabled")
	}

	t.Logf("✓ Security health check passed: all components healthy")
}

// TestHandleSecurityHealth_MethodNotAllowed tests invalid HTTP method
func TestHandleSecurityHealth_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/health", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityHealth(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}

	t.Logf("✓ Method validation working correctly")
}

// TestHandleSecurityHealth_PartiallyEnabled tests health when some features are disabled
func TestHandleSecurityHealth_PartiallyEnabled(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "security-test-*")
	defer os.RemoveAll(tmpDir)

	gs, _ := storage.NewGraphStorage(tmpDir)
	defer gs.Close()

	server, err := NewServer(gs, 8080)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	// Don't set encryption or TLS - simulate partial security setup

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/health", nil)
	rr := httptest.NewRecorder()

	server.handleSecurityHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]any
	json.Unmarshal(rr.Body.Bytes(), &response)

	components := response["components"].(map[string]any)
	encryption := components["encryption"].(map[string]any)
	tls := components["tls"].(map[string]any)

	if encryption["enabled"] != false {
		t.Error("Expected encryption to be disabled")
	}

	if tls["enabled"] != false {
		t.Error("Expected TLS to be disabled")
	}

	t.Logf("✓ Partial security health check working correctly")
}

// TestSecurityHandlers_Integration tests all security handlers in sequence
func TestSecurityHandlers_Integration(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// 1. Check initial security health
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/health", nil)
	rr := httptest.NewRecorder()
	server.handleSecurityHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Security health check failed: %s", rr.Body.String())
	}
	t.Logf("✓ Step 1: Security health check passed")

	// 2. Get initial key info
	req = httptest.NewRequest(http.MethodGet, "/api/v1/security/keys/info", nil)
	rr = httptest.NewRecorder()
	server.handleSecurityKeyInfo(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Key info retrieval failed: %s", rr.Body.String())
	}

	var keyInfo1 map[string]any
	json.Unmarshal(rr.Body.Bytes(), &keyInfo1)
	t.Logf("✓ Step 2: Retrieved initial key info")

	// 3. Rotate the key
	req = httptest.NewRequest(http.MethodPost, "/api/v1/security/keys/rotate", nil)
	rr = httptest.NewRecorder()
	server.handleSecurityKeyRotate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Key rotation failed: %s", rr.Body.String())
	}

	var rotateResp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &rotateResp)
	newVersion := rotateResp["new_version"]
	t.Logf("✓ Step 3: Key rotated to version %v", newVersion)

	// 4. Verify key info updated
	req = httptest.NewRequest(http.MethodGet, "/api/v1/security/keys/info", nil)
	rr = httptest.NewRecorder()
	server.handleSecurityKeyInfo(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Key info retrieval after rotation failed: %s", rr.Body.String())
	}
	t.Logf("✓ Step 4: Verified key info after rotation")

	// 5. Generate some audit events
	server.auditLogger.Log(&audit.Event{
		ID:           "integration-test-1",
		Timestamp:    time.Now(),
		UserID:       "test-user",
		Action:       audit.ActionRead,
		ResourceType: audit.ResourceNode,
		Status:       audit.StatusSuccess,
	})

	// 6. Retrieve audit logs
	req = httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/logs", nil)
	rr = httptest.NewRecorder()
	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Audit log retrieval failed: %s", rr.Body.String())
	}

	var auditResp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &auditResp)
	count := auditResp["count"].(float64)
	t.Logf("✓ Step 5: Retrieved %d audit events", int(count))

	// 7. Export audit logs
	req = httptest.NewRequest(http.MethodPost, "/api/v1/security/audit/export", nil)
	rr = httptest.NewRecorder()
	server.handleSecurityAuditExport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Audit log export failed: %s", rr.Body.String())
	}
	t.Logf("✓ Step 6: Exported audit logs successfully")

	t.Logf("✓ Integration test completed: All security handlers working correctly")
}

// ============================================================================
// NEGATIVE TEST CASES - Input Validation and Error Paths
// ============================================================================

// TestHandleSecurityAuditLogs_InvalidTimeFormat tests malformed timestamp handling
func TestHandleSecurityAuditLogs_InvalidTimeFormat(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add a test event first
	server.inMemoryAuditLogger.Log(&audit.Event{
		ID:           "test-1",
		Timestamp:    time.Now(),
		UserID:       "user123",
		Action:       audit.ActionRead,
		ResourceType: audit.ResourceNode,
		Status:       audit.StatusSuccess,
	})

	testCases := []struct {
		name       string
		startTime  string
		endTime    string
		wantStatus int
	}{
		{
			name:       "Invalid start_time format",
			startTime:  "not-a-date",
			wantStatus: http.StatusOK, // Currently silently ignores - should still return events
		},
		{
			name:      "Invalid end_time format",
			endTime:   "2024-13-45",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Both times invalid",
			startTime:  "garbage",
			endTime:    "also-garbage",
			wantStatus: http.StatusOK,
		},
		{
			name:       "SQL injection attempt in time",
			startTime:  "2024-01-01%27%3B%20DROP%20TABLE%20audit%3B--", // URL-encoded
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/v1/security/audit/logs?"
			if tc.startTime != "" {
				url += "start_time=" + tc.startTime + "&"
			}
			if tc.endTime != "" {
				url += "end_time=" + tc.endTime
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			rr := httptest.NewRecorder()

			server.handleSecurityAuditLogs(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tc.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}

	t.Logf("✓ Invalid time format handling tested")
}

// TestHandleSecurityAuditLogs_InvalidLimit tests invalid limit parameter handling
func TestHandleSecurityAuditLogs_InvalidLimit(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add test events
	for i := 0; i < 10; i++ {
		server.inMemoryAuditLogger.Log(&audit.Event{
			ID:           fmt.Sprintf("test-%d", i),
			Timestamp:    time.Now(),
			UserID:       "user123",
			Action:       audit.ActionRead,
			ResourceType: audit.ResourceNode,
			Status:       audit.StatusSuccess,
		})
	}

	testCases := []struct {
		name          string
		limit         string
		expectEvents  bool // true if we expect events in response
		expectedCount int  // -1 means just check it's valid JSON
	}{
		{
			name:          "Negative limit",
			limit:         "-5",
			expectEvents:  true,
			expectedCount: -1, // Falls back to default (100)
		},
		{
			name:          "Zero limit",
			limit:         "0",
			expectEvents:  true,
			expectedCount: -1,
		},
		{
			name:          "Non-numeric limit",
			limit:         "abc",
			expectEvents:  true,
			expectedCount: -1,
		},
		{
			name:          "Float limit",
			limit:         "5.5",
			expectEvents:  true,
			expectedCount: -1,
		},
		{
			name:          "Very large limit",
			limit:         "99999999999999999999",
			expectEvents:  true,
			expectedCount: -1,
		},
		{
			name:          "SQL injection in limit",
			limit:         "10%3B%20DROP%20TABLE", // URL-encoded "10; DROP TABLE"
			expectEvents:  true,
			expectedCount: -1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/v1/security/audit/logs?limit=" + tc.limit
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rr := httptest.NewRecorder()

			server.handleSecurityAuditLogs(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d. Body: %s",
					http.StatusOK, rr.Code, rr.Body.String())
				return
			}

			var response map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Errorf("Failed to parse response: %v", err)
				return
			}

			if tc.expectEvents {
				events, ok := response["events"].([]any)
				if !ok {
					t.Error("Expected events array in response")
				} else if tc.expectedCount >= 0 && len(events) != tc.expectedCount {
					t.Errorf("Expected %d events, got %d", tc.expectedCount, len(events))
				}
			}
		})
	}

	t.Logf("✓ Invalid limit parameter handling tested")
}

// TestHandleSecurityAuditLogs_InvalidFilterValues tests invalid filter values
func TestHandleSecurityAuditLogs_InvalidFilterValues(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Add a test event
	server.inMemoryAuditLogger.Log(&audit.Event{
		ID:           "test-1",
		Timestamp:    time.Now(),
		UserID:       "user123",
		Username:     "testuser",
		Action:       audit.ActionRead,
		ResourceType: audit.ResourceNode,
		Status:       audit.StatusSuccess,
	})

	testCases := []struct {
		name         string
		queryParams  string
		expectStatus int
	}{
		{
			name:         "Invalid action filter",
			queryParams:  "action=INVALID_ACTION",
			expectStatus: http.StatusOK, // Returns empty results
		},
		{
			name:         "Invalid resource_type filter",
			queryParams:  "resource_type=INVALID_TYPE",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Invalid status filter",
			queryParams:  "status=INVALID_STATUS",
			expectStatus: http.StatusOK,
		},
		{
			name:         "XSS attempt in user_id",
			queryParams:  "user_id=%3Cscript%3Ealert%28%27xss%27%29%3C%2Fscript%3E", // URL-encoded
			expectStatus: http.StatusOK,
		},
		{
			name:         "Path traversal in user_id",
			queryParams:  "user_id=..%2F..%2F..%2Fetc%2Fpasswd", // URL-encoded
			expectStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/v1/security/audit/logs?" + tc.queryParams
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rr := httptest.NewRecorder()

			server.handleSecurityAuditLogs(rr, req)

			if rr.Code != tc.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tc.expectStatus, rr.Code, rr.Body.String())
			}

			// Verify response is valid JSON (no server errors)
			var response map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Errorf("Invalid JSON response: %v", err)
			}
		})
	}

	t.Logf("✓ Invalid filter values handling tested")
}

// TestHandleSecurityKeyRotate_ConcurrentRequests tests concurrent key rotation safety
func TestHandleSecurityKeyRotate_ConcurrentRequests(t *testing.T) {
	server, cleanup := setupSecurityTestServer(t)
	defer cleanup()

	// Fire multiple concurrent rotation requests
	var wg sync.WaitGroup
	results := make(chan int, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/security/keys/rotate", nil)
			rr := httptest.NewRecorder()
			server.handleSecurityKeyRotate(rr, req)
			results <- rr.Code
		}()
	}

	wg.Wait()
	close(results)

	// All requests should complete (either success or serialized failure)
	successCount := 0
	for code := range results {
		if code == http.StatusOK {
			successCount++
		} else if code != http.StatusInternalServerError {
			t.Errorf("Unexpected status code: %d", code)
		}
	}

	t.Logf("✓ Concurrent key rotation tested: %d/10 succeeded", successCount)
}

// TestSecurityHandlers_EmptyAuditLog tests handlers with no audit events
func TestSecurityHandlers_EmptyAuditLog(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "security-test-empty-*")
	defer os.RemoveAll(tmpDir)

	gs, _ := storage.NewGraphStorage(tmpDir)
	defer gs.Close()

	server, err := NewServer(gs, 8080)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test audit logs with no events
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/audit/logs", nil)
	rr := httptest.NewRecorder()
	server.handleSecurityAuditLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d for empty audit log, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]any
	json.Unmarshal(rr.Body.Bytes(), &response)

	count := response["count"].(float64)
	if count != 0 {
		t.Errorf("Expected 0 events, got %v", count)
	}

	// Test export with no events
	req = httptest.NewRequest(http.MethodPost, "/api/v1/security/audit/export", nil)
	rr = httptest.NewRecorder()
	server.handleSecurityAuditExport(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d for empty export, got %d", http.StatusOK, rr.Code)
	}

	var events []any
	if err := json.Unmarshal(rr.Body.Bytes(), &events); err != nil {
		t.Errorf("Invalid JSON in empty export: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected empty array, got %d events", len(events))
	}

	t.Logf("✓ Empty audit log handling tested")
}
