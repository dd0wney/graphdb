package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleHealth_ReportsBuildVersion guards GitHub #766: /health reported a
// hardcoded "1.0.0" whatever release was running, so a client could not tell
// which bugs it had to work around.
func TestHandleHealth_ReportsBuildVersion(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	orig := BuildVersion
	BuildVersion = "v9.8.7-health-test"
	t.Cleanup(func() { BuildVersion = orig })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	server.handleHealth(rr, req)

	var response HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("parse health response: %v (body %s)", err, rr.Body.String())
	}
	if response.Version != "v9.8.7-health-test" {
		t.Fatalf("health version = %q, want the build version %q", response.Version, "v9.8.7-health-test")
	}
}
