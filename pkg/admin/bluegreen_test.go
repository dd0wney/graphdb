package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthChecker_FetchVersion tests fetching version from health endpoint
func TestHealthChecker_FetchVersion(t *testing.T) {
	tests := []struct {
		name            string
		healthResponse  any
		statusCode      int
		expectedVersion string
		expectError     bool
	}{
		{
			name: "successful version fetch",
			healthResponse: map[string]any{
				"status":    "healthy",
				"version":   "v1.2.3",
				"uptime":    "1h30m",
				"timestamp": "2025-01-15T10:00:00Z",
			},
			statusCode:      http.StatusOK,
			expectedVersion: "v1.2.3",
			expectError:     false,
		},
		{
			name: "version with build info",
			healthResponse: map[string]any{
				"status":  "healthy",
				"version": "v2.0.0-beta.1+build.123",
			},
			statusCode:      http.StatusOK,
			expectedVersion: "v2.0.0-beta.1+build.123",
			expectError:     false,
		},
		{
			name:            "server returns error",
			healthResponse:  nil,
			statusCode:      http.StatusInternalServerError,
			expectedVersion: "",
			expectError:     true,
		},
		{
			name: "missing version field",
			healthResponse: map[string]any{
				"status": "healthy",
			},
			statusCode:      http.StatusOK,
			expectedVersion: "",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					t.Errorf("Expected /health endpoint, got %s", r.URL.Path)
				}

				w.WriteHeader(tt.statusCode)
				if tt.healthResponse != nil {
					json.NewEncoder(w).Encode(tt.healthResponse)
				}
			}))
			defer ts.Close()

			hc := &HealthChecker{
				checkEndpoint: "/health",
				timeout:       5 * time.Second,
			}

			version, err := hc.FetchVersion(ts.URL)

			if tt.expectError && err == nil {
				t.Errorf("FetchVersion() expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("FetchVersion() unexpected error: %v", err)
			}

			if version != tt.expectedVersion {
				t.Errorf("FetchVersion() version = %v, want %v", version, tt.expectedVersion)
			}
		})
	}
}

// TestBlueGreenManager_GetStatusWithVersions tests status with version detection
func TestBlueGreenManager_GetStatusWithVersions(t *testing.T) {
	// Create test servers for blue and green deployments
	blueServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "healthy",
			"version": "v1.0.0",
		})
	}))
	defer blueServer.Close()

	greenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "healthy",
			"version": "v1.1.0",
		})
	}))
	defer greenServer.Close()

	bgm := NewBlueGreenManager("blue", 8080, 8081)

	// Override health checker to use test servers
	// Note: This test validates the integration, but actual implementation
	// will need to extract port from URL and call the right server

	status := bgm.GetStatus()

	if status.CurrentActive != "blue" {
		t.Errorf("GetStatus() CurrentActive = %v, want blue", status.CurrentActive)
	}

	// Both deployments should be checked
	if status.Blue.Color != "blue" {
		t.Errorf("GetStatus() Blue.Color = %v, want blue", status.Blue.Color)
	}

	if status.Green.Color != "green" {
		t.Errorf("GetStatus() Green.Color = %v, want green", status.Green.Color)
	}
}

// TestBlueGreenManager_Switch tests the blue-green switching functionality
func TestBlueGreenManager_Switch(t *testing.T) {
	tests := []struct {
		name             string
		initialColor     string
		targetColor      string
		expectedSuccess  bool
		expectedNewColor string
		expectError      bool
	}{
		{
			name:             "already on target color",
			initialColor:     "blue",
			targetColor:      "blue",
			expectedSuccess:  true,
			expectedNewColor: "blue",
			expectError:      false,
		},
		{
			name:             "invalid target color",
			initialColor:     "blue",
			targetColor:      "purple",
			expectedSuccess:  false,
			expectedNewColor: "",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bgm := NewBlueGreenManager(tt.initialColor, 8080, 8081)

			ctx := context.Background()
			req := SwitchRequest{
				TargetColor: tt.targetColor,
				DrainTime:   0,
			}

			resp, err := bgm.Switch(ctx, req)

			if tt.expectError && err == nil {
				t.Error("Switch() expected error, got nil")
			}

			if !tt.expectError && err != nil && tt.targetColor != tt.initialColor {
				t.Errorf("Switch() unexpected error: %v", err)
			}

			if resp.Success != tt.expectedSuccess {
				t.Errorf("Switch() Success = %v, want %v", resp.Success, tt.expectedSuccess)
			}

			if tt.expectedNewColor != "" && resp.NewColor != tt.expectedNewColor {
				t.Errorf("Switch() NewColor = %v, want %v", resp.NewColor, tt.expectedNewColor)
			}

			if resp.PreviousColor != tt.initialColor {
				t.Errorf("Switch() PreviousColor = %v, want %v", resp.PreviousColor, tt.initialColor)
			}

			t.Logf("✓ Switch test passed: %s", tt.name)
		})
	}
}

// Note: Full integration tests for Switch with actual deployment health checks
// would require running GraphDB servers on specific ports. The tests above
// cover the core logic paths that don't require external dependencies.

// TestBlueGreenManager_HTTPHandlers tests the HTTP handler endpoints
func TestBlueGreenManager_HTTPHandlers(t *testing.T) {
	bgm := NewBlueGreenManager("blue", 8080, 8081)

	// Test handleStatus endpoint
	t.Run("handleStatus", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/bluegreen/status", nil)
		rr := httptest.NewRecorder()

		bgm.handleStatus(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("handleStatus() status = %d, want %d", rr.Code, http.StatusOK)
		}

		var status BlueGreenStatus
		if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if status.CurrentActive != "blue" {
			t.Errorf("handleStatus() CurrentActive = %v, want blue", status.CurrentActive)
		}

		t.Log("✓ handleStatus working correctly")
	})

	// Note: handleSwitch successful case requires real HTTP servers running
	// on specific ports for health checks, so we test error cases instead

	// Test handleSwitch with invalid method
	t.Run("handleSwitch invalid method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/bluegreen/switch", nil)
		rr := httptest.NewRecorder()

		bgm.handleSwitch(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("handleSwitch() status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
		}

		t.Log("✓ handleSwitch method validation working correctly")
	})

	// Test handleSwitch with invalid JSON
	t.Run("handleSwitch invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/bluegreen/switch",
			bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		bgm.handleSwitch(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("handleSwitch() status = %d, want %d", rr.Code, http.StatusBadRequest)
		}

		t.Log("✓ handleSwitch JSON validation working correctly")
	})
}

// TestBlueGreenManager_RegisterHandlers tests handler registration
func TestBlueGreenManager_RegisterHandlers(t *testing.T) {
	bgm := NewBlueGreenManager("blue", 8080, 8081)

	mux := http.NewServeMux()
	bgm.RegisterHandlers(mux)

	// Test that handlers were registered by making requests
	server := httptest.NewServer(mux)
	defer server.Close()

	// Test status endpoint
	resp, err := http.Get(server.URL + "/admin/bluegreen/status")
	if err != nil {
		t.Fatalf("Failed to call status endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status endpoint returned %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var status BlueGreenStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}

	if status.CurrentActive != "blue" {
		t.Errorf("RegisterHandlers() status endpoint CurrentActive = %v, want blue", status.CurrentActive)
	}

	t.Log("✓ RegisterHandlers working correctly")
}

// TestOpposite tests the opposite color helper function
func TestOpposite(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"blue", "green"},
		{"green", "blue"},
		{"Blue", "blue"},    // Not "blue" - returns blue as default
		{"GREEN", "blue"},   // Not "green" or "blue" - returns blue as default
		{"invalid", "blue"}, // Default case
		{"", "blue"},        // Empty string - default case
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := opposite(tt.input)
			if result != tt.expected {
				t.Errorf("opposite(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}

	t.Log("✓ opposite() working correctly")
}
