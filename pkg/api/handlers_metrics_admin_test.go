package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
)

// TestMetricsEndpoint_AdminOnly pins that GET /api/metrics is admin-gated.
// The endpoint returns GLOBAL GetStatistics() (cross-tenant NodeCount/EdgeCount/
// TotalQueries) plus operator system stats, so exposing it to any authenticated
// tenant user is a cross-tenant volume-signal leak. The request is routed
// through the real registerRoutes() table so the route's middleware wrapping is
// what is asserted (not a hand-composed wrapper).
func TestMetricsEndpoint_AdminOnly(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	admin, err := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	viewer, err := server.userStore.CreateUser("viewer", "ViewerPassword123!", auth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser viewer: %v", err)
	}
	adminToken, err := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	if err != nil {
		t.Fatalf("token(admin): %v", err)
	}
	viewerToken, err := server.jwtManager.GenerateTokenWithTenant(viewer.ID, viewer.Username, viewer.Role, "tenant-a")
	if err != nil {
		t.Fatalf("token(viewer): %v", err)
	}

	mux := http.NewServeMux()
	server.registerRoutes(mux)

	get := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}

	if code := get(viewerToken); code != http.StatusForbidden {
		t.Errorf("non-admin GET /api/metrics = %d, want 403 — endpoint leaks cross-tenant global stats", code)
	}
	if code := get(adminToken); code != http.StatusOK {
		t.Errorf("admin GET /api/metrics = %d, want 200", code)
	}
}
