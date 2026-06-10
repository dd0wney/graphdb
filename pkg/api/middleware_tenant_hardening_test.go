package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// TestWithTenant_SuspendedTenantRejected pins security audit finding H-1
// (AUDIT_security_2026-06-10.md): withTenant must reject a request whose
// tenant has been suspended, not just one whose tenant is absent. Before
// the fix the middleware called tenantStore.Get (status-blind), so a
// suspended tenant kept full access until its JWT expired — suspension
// was cosmetic. This test mints a valid token, suspends the tenant, and
// asserts the next request is 403.
//
// RED against pre-fix code: Get() returns the suspended tenant without
// error, so the request reaches the handler (200).
func TestWithTenant_SuspendedTenantRejected(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const suspendedTenant = "suspended-corp"
	if server.tenantStore == nil {
		t.Skip("tenant store not configured")
	}
	if err := server.tenantStore.Create(&tenant.Tenant{
		ID:     suspendedTenant,
		Name:   "Suspended Corp (test)",
		Status: tenant.TenantStatusActive,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	token := mintTestToken(t, server, auth.RoleEditor, "suspended-user", suspendedTenant)

	reached := false
	probe := func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/_probe", server.requireAuth(server.withTenant(probe)))

	// Active tenant: request must succeed (control case).
	reqActive := httptest.NewRequest(http.MethodGet, "/_probe", nil)
	reqActive.Header.Set("Authorization", "Bearer "+token)
	rrActive := httptest.NewRecorder()
	mux.ServeHTTP(rrActive, reqActive)
	if rrActive.Code != http.StatusOK {
		t.Fatalf("active tenant: want 200, got %d (body: %s)", rrActive.Code, rrActive.Body.String())
	}

	// Suspend the tenant, then re-issue the same authenticated request.
	if err := server.tenantStore.Suspend(suspendedTenant); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	reached = false
	reqSusp := httptest.NewRequest(http.MethodGet, "/_probe", nil)
	reqSusp.Header.Set("Authorization", "Bearer "+token)
	rrSusp := httptest.NewRecorder()
	mux.ServeHTTP(rrSusp, reqSusp)

	if rrSusp.Code != http.StatusForbidden {
		t.Errorf("suspended tenant: want 403, got %d (body: %s)", rrSusp.Code, rrSusp.Body.String())
	}
	if reached {
		t.Error("suspended tenant: handler was reached — suspension is not enforced (H-1)")
	}
}

// TestWithTenant_DefaultTenantStillAutoCreates pins that the H-1 fix does
// not break the default-tenant auto-create path: a request for the
// default tenant against an empty store must still succeed by creating
// the tenant, not 403.
func TestWithTenant_DefaultTenantStillAutoCreates(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	if server.tenantStore == nil {
		t.Skip("tenant store not configured")
	}

	// Token with empty tenantID resolves to the default tenant.
	token := mintTestToken(t, server, auth.RoleEditor, "default-user", "")

	probe := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	mux := http.NewServeMux()
	mux.HandleFunc("/_probe", server.requireAuth(server.withTenant(probe)))

	req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("default tenant auto-create: want 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// TestResolveTenantID_RejectsMalformedHeader pins security audit finding
// M-5: an admin-supplied X-Tenant-ID header must be validated against a
// strict allowlist before it is accepted as a tenant ID. Before the fix
// the raw header value flowed into storage keys and log lines unchecked,
// enabling log injection (CRLF) and arbitrary-length/charset tenant keys.
//
// RED against pre-fix code: malformed headers are accepted verbatim, so
// the request is not rejected.
func TestResolveTenantID_RejectsMalformedHeader(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	adminToken := mintTestToken(t, server, auth.RoleAdmin, "m5-admin", "")

	probe := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	mux := http.NewServeMux()
	mux.HandleFunc("/_probe", server.requireAuth(server.withTenant(probe)))

	malformed := []struct {
		name   string
		header string
	}{
		{"crlf injection", "real-tenant\nFAKE LOG LINE"},
		{"path separator", "../other-tenant"},
		{"null byte", "tenant\x00admin"},
		{"space", "has space"},
		{"over length", string(make([]byte, 65))},
	}

	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			req.Header.Set(TenantIDHeader, tc.header)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("malformed X-Tenant-ID %q: want 400, got %d (body: %s)",
					tc.header, rr.Code, rr.Body.String())
			}
		})
	}

	// A well-formed override must still be accepted (control case).
	if err := server.tenantStore.Create(&tenant.Tenant{
		ID:     "valid-tenant-01",
		Name:   "Valid (test)",
		Status: tenant.TenantStatusActive,
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set(TenantIDHeader, "valid-tenant-01")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("well-formed X-Tenant-ID: want 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}
