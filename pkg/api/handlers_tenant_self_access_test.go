package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// TestTenantSelfAccess_WithTenantScopesCorrectly pins that GET
// /api/v1/tenants/{id} scopes a non-admin to their OWN tenant. The route was
// registered without withTenant, so handleGetTenant's self-check compared the
// path tenant against getTenantFromContext(r), which fell back to
// DefaultTenantID — denying a non-admin their own tenant AND letting them read
// the "default" tenant's metadata. withTenant resolves the caller's tenant from
// their claims, making the self-check correct. (Tenant-isolation sweep F2.)
//
// Routed through the real registerRoutes table so the route's middleware
// wrapping is what is asserted.
func TestTenantSelfAccess_WithTenantScopesCorrectly(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Enable multi-tenancy so /api/v1/tenants/ registers and withTenant can
	// validate the caller's tenant against the store. NewTenantStore seeds the
	// "default" tenant already (the foreign tenant the cross-tenant assertion
	// reads), so only tenant-a needs creating.
	server.tenantStore = tenant.NewTenantStore()
	if err := server.tenantStore.Create(&tenant.Tenant{
		ID:     "tenant-a",
		Name:   "tenant-a",
		Status: tenant.TenantStatusActive,
	}); err != nil {
		t.Fatalf("create tenant-a: %v", err)
	}

	// A non-admin scoped to tenant-a.
	token := mintTestToken(t, server, auth.RoleViewer, "tenant-a-viewer", "tenant-a")

	mux := http.NewServeMux()
	server.registerRoutes(mux)

	get := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}

	// Self-access must succeed (403 without withTenant — fail-closed bug).
	if code := get("/api/v1/tenants/tenant-a"); code != http.StatusOK {
		t.Errorf("self GET /api/v1/tenants/tenant-a = %d, want 200 — non-admin denied own tenant without withTenant", code)
	}
	// Cross-tenant read of the default tenant must be forbidden
	// (200 without withTenant — the leak: getTenantFromContext fell back to "default").
	if code := get("/api/v1/tenants/default"); code != http.StatusForbidden {
		t.Errorf("cross-tenant GET /api/v1/tenants/default = %d, want 403 — non-admin read foreign 'default' tenant without withTenant", code)
	}

	// Admin cross-tenant access must still work now that withTenant wraps the
	// whole /api/v1/tenants/ dispatch: withTenant validates the CALLER's tenant
	// (admin -> "default", auto-created) and handleGetTenant skips the self-check
	// for admins, so an admin reaches any path tenant. Guards against withTenant
	// regressing admin tenant management.
	adminTok := mintTestToken(t, server, auth.RoleAdmin, "root-admin", "")
	adminGet := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+adminTok)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr.Code
	}
	if code := adminGet("/api/v1/tenants/tenant-a"); code != http.StatusOK {
		t.Errorf("admin GET /api/v1/tenants/tenant-a = %d, want 200 — withTenant must not block admin cross-tenant ops", code)
	}
	if code := adminGet("/api/v1/tenants/default"); code != http.StatusOK {
		t.Errorf("admin GET /api/v1/tenants/default = %d, want 200", code)
	}
}
