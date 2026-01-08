package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestHandleHealth tests the GET /health endpoint
func TestHandleHealth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Store the start time for uptime validation
	startTime := time.Now()
	time.Sleep(10 * time.Millisecond) // Small delay to ensure non-zero uptime

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	// Validate response fields
	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got %q", response.Status)
	}

	if response.Version == "" {
		t.Error("Expected non-empty version")
	}

	if response.Edition == "" {
		t.Error("Expected non-empty edition")
	}

	if response.Uptime == "" {
		t.Error("Expected non-empty uptime")
	}

	if response.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	if !response.Timestamp.After(startTime) {
		t.Error("Timestamp should be after server start time")
	}

	if len(response.Features) == 0 {
		t.Error("Expected at least one feature in response")
	}

	t.Logf("✓ Health check passed: status=%s, edition=%s, features=%d, uptime=%s",
		response.Status, response.Edition, len(response.Features), response.Uptime)
}

// TestHandleMetrics tests the GET /metrics endpoint
func TestHandleMetrics(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some test data for metrics
	for i := 0; i < 5; i++ {
		server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
	}

	// Create some edges
	server.graph.CreateEdge(1, 2, "LINK", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(2, 3, "LINK", map[string]storage.Value{}, 1.0)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	server.handleMetrics(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response MetricsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse metrics response: %v", err)
	}

	// Validate metrics
	if response.NodeCount < 5 {
		t.Errorf("Expected at least 5 nodes, got %d", response.NodeCount)
	}

	if response.EdgeCount < 2 {
		t.Errorf("Expected at least 2 edges, got %d", response.EdgeCount)
	}

	if response.Uptime == "" {
		t.Error("Expected non-empty uptime")
	}

	// These might be 0 in a fresh test server
	if response.TotalQueries < 0 {
		t.Error("Total queries should be non-negative")
	}

	if response.AvgQueryTime < 0 {
		t.Error("Avg query time should be non-negative")
	}

	t.Logf("✓ Metrics: nodes=%d, edges=%d, queries=%d, avg_time=%.2fms, uptime=%s",
		response.NodeCount, response.EdgeCount, response.TotalQueries,
		response.AvgQueryTime, response.Uptime)
}

// TestHandleHealth_AllMethods tests that health endpoint accepts all methods
func TestHandleHealth_AllMethods(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name   string
		method string
	}{
		{"GET to /health", http.MethodGet},
		{"POST to /health", http.MethodPost},
		{"PUT to /health", http.MethodPut},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/health", nil)
			rr := httptest.NewRecorder()

			server.handleHealth(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status %d for %s, got %d",
					http.StatusOK, tt.method, rr.Code)
			}
		})
	}
}

// TestHandleMetrics_AllMethods tests that metrics endpoint only accepts GET
func TestHandleMetrics_AllMethods(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{"GET to /metrics", http.MethodGet, http.StatusOK},
		{"POST to /metrics", http.MethodPost, http.StatusMethodNotAllowed},
		{"PUT to /metrics", http.MethodPut, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/metrics", nil)
			rr := httptest.NewRecorder()

			server.handleMetrics(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d for %s, got %d",
					tt.expectedStatus, tt.method, rr.Code)
			}
		})
	}
}

// TestMonitoring_ConcurrentAccess tests concurrent access to monitoring endpoints
func TestMonitoring_ConcurrentAccess(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Add some data
	for i := 0; i < 10; i++ {
		server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
	}

	// Launch 50 concurrent health checks
	done := make(chan bool, 50)
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rr := httptest.NewRecorder()
			server.handleHealth(rr, req)

			if rr.Code != http.StatusOK {
				errors <- nil
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}

	close(errors)
	errorCount := len(errors)

	if errorCount > 0 {
		t.Errorf("Expected 0 errors in concurrent access, got %d", errorCount)
	}

	t.Logf("✓ 50 concurrent health checks completed successfully")
}

// TestHandleHealth_EditionInfo tests edition information in health response
func TestHandleHealth_EditionInfo(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	var response HealthResponse
	json.Unmarshal(rr.Body.Bytes(), &response)

	// Check that edition is valid
	validEditions := map[string]bool{
		"Community":  true,
		"Enterprise": true,
	}

	if !validEditions[response.Edition] {
		t.Errorf("Invalid edition %q, expected Community or Enterprise", response.Edition)
	}

	// Check that features list matches edition capabilities
	hasFeatures := len(response.Features) > 0
	if !hasFeatures {
		t.Error("Health response should include feature list")
	}

	t.Logf("✓ Edition info validated: %s with %d features", response.Edition, len(response.Features))
}

// TestHandleMetrics_EmptyDatabase tests metrics on empty database
func TestHandleMetrics_EmptyDatabase(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	server.handleMetrics(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response MetricsResponse
	json.Unmarshal(rr.Body.Bytes(), &response)

	// Empty database should have 0 nodes and edges
	if response.NodeCount != 0 {
		t.Errorf("Expected 0 nodes in empty database, got %d", response.NodeCount)
	}

	if response.EdgeCount != 0 {
		t.Errorf("Expected 0 edges in empty database, got %d", response.EdgeCount)
	}

	t.Logf("✓ Empty database metrics correct: nodes=%d, edges=%d",
		response.NodeCount, response.EdgeCount)
}

// TestMonitoring_Integration tests health and metrics together
func TestMonitoring_Integration(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Check initial health
	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRR := httptest.NewRecorder()
	server.handleHealth(healthRR, healthReq)

	var healthResp HealthResponse
	json.Unmarshal(healthRR.Body.Bytes(), &healthResp)

	if healthResp.Status != "healthy" {
		t.Fatalf("Server not healthy: %s", healthResp.Status)
	}

	t.Logf("✓ Initial health check: %s", healthResp.Status)

	// 2. Check initial metrics (empty)
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	server.handleMetrics(metricsRR, metricsReq)

	var metrics1 MetricsResponse
	json.Unmarshal(metricsRR.Body.Bytes(), &metrics1)

	t.Logf("✓ Initial metrics: nodes=%d, edges=%d", metrics1.NodeCount, metrics1.EdgeCount)

	// 3. Add data
	for i := 0; i < 100; i++ {
		server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
	}

	for i := 0; i < 50; i++ {
		server.graph.CreateEdge(uint64(i+1), uint64(i+2), "LINK", map[string]storage.Value{}, 1.0)
	}

	// 4. Check metrics after data addition
	metricsReq2 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR2 := httptest.NewRecorder()
	server.handleMetrics(metricsRR2, metricsReq2)

	var metrics2 MetricsResponse
	json.Unmarshal(metricsRR2.Body.Bytes(), &metrics2)

	if metrics2.NodeCount < 100 {
		t.Errorf("Expected at least 100 nodes after insertion, got %d", metrics2.NodeCount)
	}

	if metrics2.EdgeCount < 50 {
		t.Errorf("Expected at least 50 edges after insertion, got %d", metrics2.EdgeCount)
	}

	t.Logf("✓ After data insertion: nodes=%d, edges=%d", metrics2.NodeCount, metrics2.EdgeCount)

	// 5. Verify health still good
	healthReq2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRR2 := httptest.NewRecorder()
	server.handleHealth(healthRR2, healthReq2)

	var healthResp2 HealthResponse
	json.Unmarshal(healthRR2.Body.Bytes(), &healthResp2)

	if healthResp2.Status != "healthy" {
		t.Errorf("Server should still be healthy after data insertion")
	}

	t.Logf("✓ Health still good after data insertion")
	t.Logf("✓ Complete monitoring integration test passed")
}
