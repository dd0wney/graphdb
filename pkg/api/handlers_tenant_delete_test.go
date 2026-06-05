package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// TestDeleteTenant_CascadesGraphData pins #223: DELETE /api/v1/tenants/{id}
// must cascade-remove the tenant's nodes/edges (not just the tenant record),
// reject the default tenant (403), and 404 a missing tenant.
func TestDeleteTenant_CascadesGraphData(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	server.tenantStore = tenant.NewTenantStore() // seeds "default"
	if err := server.tenantStore.Create(&tenant.Tenant{ID: "t-del", Name: "t-del", Status: tenant.TenantStatusActive}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Seed graph data owned by t-del.
	a, err := server.graph.CreateNodeWithTenant("t-del", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	b, _ := server.graph.CreateNodeWithTenant("t-del", []string{"N"}, nil)
	if _, err := server.graph.CreateEdgeWithTenant("t-del", a.ID, b.ID, "LINK", nil, 1.0); err != nil {
		t.Fatalf("create edge: %v", err)
	}

	adminTok := mintTestToken(t, server, auth.RoleAdmin, "root-admin", "")
	mux := http.NewServeMux()
	server.registerRoutes(mux)

	del := func(path string) int {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		req.Header.Set("Authorization", "Bearer "+adminTok)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}

	// Delete the tenant → 200, and its graph data must be gone (the bug: it persisted).
	if code := del("/api/v1/tenants/t-del"); code != http.StatusOK {
		t.Fatalf("DELETE /api/v1/tenants/t-del = %d, want 200", code)
	}
	if n := server.graph.CountNodesForTenant("t-del"); n != 0 {
		t.Errorf("after tenant delete: node count = %d, want 0 (cascade didn't run)", n)
	}
	if e := server.graph.CountEdgesForTenant("t-del"); e != 0 {
		t.Errorf("after tenant delete: edge count = %d, want 0", e)
	}

	// Default tenant is undeletable.
	if code := del("/api/v1/tenants/default"); code != http.StatusForbidden {
		t.Errorf("DELETE default = %d, want 403", code)
	}
	// Missing tenant is a 404.
	if code := del("/api/v1/tenants/nope"); code != http.StatusNotFound {
		t.Errorf("DELETE missing = %d, want 404", code)
	}
}
