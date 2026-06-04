package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/audit"
	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// seedComplianceTestTenants pre-creates the test tenants so withTenant
// doesn't reject them with 403. setupTestServer enables multi-tenancy
// but doesn't seed named tenants; only "default" is auto-created.
func seedComplianceTestTenants(t *testing.T, s *Server) {
	t.Helper()
	if s.tenantStore == nil {
		return
	}
	for _, id := range []string{"tenant-a", "tenant-b"} {
		if err := s.tenantStore.Create(&tenant.Tenant{ID: id, Name: id, Status: tenant.TenantStatusActive}); err != nil {
			t.Fatalf("seed tenant %s: %v", id, err)
		}
	}
}

// seedComplianceAuditEvents writes test events for two tenants directly
// to the in-memory logger. Bypasses the middleware so the test can
// control event content precisely; the middleware-population path is
// already covered by TestAuditCollector_PopulatedAfterAuth (PR-0).
func seedComplianceAuditEvents(t *testing.T, s *Server) {
	t.Helper()
	for _, e := range []*audit.Event{
		{UserID: "u-a1", Username: "alice", TenantID: "tenant-a", Action: audit.ActionCreate, ResourceType: audit.ResourceNode, Status: audit.StatusSuccess},
		{UserID: "u-a2", Username: "alice2", TenantID: "tenant-a", Action: audit.ActionRead, ResourceType: audit.ResourceNode, Status: audit.StatusSuccess},
		{UserID: "u-b1", Username: "bob", TenantID: "tenant-b", Action: audit.ActionCreate, ResourceType: audit.ResourceNode, Status: audit.StatusSuccess},
		{UserID: "u-b2", Username: "bob2", TenantID: "tenant-b", Action: audit.ActionUpdate, ResourceType: audit.ResourceEdge, Status: audit.StatusFailure},
	} {
		if err := s.inMemoryAuditLogger.Log(e); err != nil {
			t.Fatalf("seed Log: %v", err)
		}
	}
}

// callComplianceAuditLog issues a GET to /v1/compliance/audit-log with the
// given token + optional headers/query. Returns the parsed response and
// the recorder's status code.
func callComplianceAuditLog(t *testing.T, s *Server, token, queryAndHeaders string) (int, map[string]any) {
	t.Helper()
	path := "/v1/compliance/audit-log"
	xTenant := ""
	if queryAndHeaders != "" {
		const hdrPrefix = "X-Tenant-ID:"
		if strings.HasPrefix(queryAndHeaders, hdrPrefix) {
			rest := queryAndHeaders[len(hdrPrefix):]
			end := len(rest)
			for i, c := range rest {
				if c == ' ' || c == '?' {
					end = i
					break
				}
			}
			xTenant = rest[:end]
			if end < len(rest) {
				path += rest[end:]
			}
		} else {
			path += "?" + queryAndHeaders
		}
	}

	// Test chain omits auditCollectorMiddleware + auditMiddleware on
	// purpose: the handler only reads from inMemoryAuditLogger, and
	// including the audit middleware would emit an additional event
	// for this very request — distorting the seeded event count the
	// tests assert on. PR-0 already pins the auditCollector→audit
	// integration path (middleware_audit_collector_test.go).
	inner := http.HandlerFunc(s.handleComplianceAuditLog)
	handler := s.requireAuth(s.withTenant(inner))

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if xTenant != "" {
		req.Header.Set(TenantIDHeader, xTenant)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var body map[string]any
	if rr.Body.Len() > 0 {
		_ = json.NewDecoder(rr.Body).Decode(&body)
	}
	return rr.Code, body
}

// TestComplianceAuditLog_TenantIsolation pins the load-bearing F3
// guarantee: a non-admin user querying /v1/compliance/audit-log sees only
// their own tenant's events, even if events for other tenants exist in
// the log. Mirrors the audit-regression-row template in
// docs/F3_COMPLIANCE_API_DESIGN.md §5.
func TestComplianceAuditLog_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)
	seedComplianceAuditEvents(t, server)

	userA, err := server.userStore.CreateUser("alice-a", "AlicePassword123!", auth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser alice-a: %v", err)
	}
	tokenA, err := server.jwtManager.GenerateTokenWithTenant(userA.ID, userA.Username, userA.Role, "tenant-a")
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant tenant-a: %v", err)
	}

	code, body := callComplianceAuditLog(t, server, tokenA, "")
	if code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%v", code, body)
	}
	if body["tenant"] != "tenant-a" {
		t.Errorf("response tenant: want tenant-a, got %v", body["tenant"])
	}
	if total, _ := body["total"].(float64); int(total) != 2 {
		t.Errorf("total: want 2 events for tenant-a, got %v (events=%v)", total, body["events"])
	}
	events, _ := body["events"].([]any)
	for i, ev := range events {
		m, _ := ev.(map[string]any)
		if m["tenant_id"] != "tenant-a" {
			t.Errorf("event[%d] tenant_id: want tenant-a, got %v", i, m["tenant_id"])
		}
	}
}

// TestComplianceAuditLog_AdminCrossTenantOverride pins Decision 1c: an
// admin can scope to a different tenant via X-Tenant-ID, OR widen to
// all tenants via ?tenant=*. Non-admin paths are covered above.
func TestComplianceAuditLog_AdminCrossTenantOverride(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)
	seedComplianceAuditEvents(t, server)

	admin, err := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser root: %v", err)
	}
	adminToken, err := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant root: %v", err)
	}

	t.Run("X-Tenant-ID override targets named tenant", func(t *testing.T) {
		code, body := callComplianceAuditLog(t, server, adminToken, "X-Tenant-ID:tenant-b")
		if code != http.StatusOK {
			t.Fatalf("status: want 200, got %d", code)
		}
		if body["tenant"] != "tenant-b" {
			t.Errorf("tenant: want tenant-b, got %v", body["tenant"])
		}
		if total, _ := body["total"].(float64); int(total) != 2 {
			t.Errorf("total: want 2 (tenant-b events), got %v", total)
		}
	})

	t.Run("tenant=* widens to cross-tenant", func(t *testing.T) {
		code, body := callComplianceAuditLog(t, server, adminToken, "tenant=*")
		if code != http.StatusOK {
			t.Fatalf("status: want 200, got %d", code)
		}
		if body["cross_tenant"] != true {
			t.Errorf("cross_tenant: want true, got %v", body["cross_tenant"])
		}
		if _, ok := body["tenant"]; ok {
			t.Errorf("tenant field should be absent when cross_tenant=true, got %v", body["tenant"])
		}
		if total, _ := body["total"].(float64); int(total) != 4 {
			t.Errorf("total: want 4 (all seeded events), got %v", total)
		}
	})
}

// TestComplianceAuditLog_NonAdminCannotWidenCrossTenant pins the safety
// invariant: a non-admin caller passing ?tenant=* is silently scoped to
// their own tenant (the admin gate inside the handler rejects the
// widening; the rest of the response uses callerTenant).
func TestComplianceAuditLog_NonAdminCannotWidenCrossTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)
	seedComplianceAuditEvents(t, server)

	user, err := server.userStore.CreateUser("bob-b", "BobPassword123!", auth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser bob-b: %v", err)
	}
	token, err := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "tenant-b")
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant tenant-b: %v", err)
	}

	code, body := callComplianceAuditLog(t, server, token, "tenant=*")
	if code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", code)
	}
	if body["cross_tenant"] == true {
		t.Errorf("non-admin must not get cross_tenant=true (received %v)", body)
	}
	if body["tenant"] != "tenant-b" {
		t.Errorf("tenant: want tenant-b (caller's own), got %v", body["tenant"])
	}
	if total, _ := body["total"].(float64); int(total) != 2 {
		t.Errorf("total: want 2 (tenant-b only), got %v", total)
	}
}

// TestComplianceAuditLog_Pagination verifies offset+limit slice over
// the append-only events. Sets limit=1 and walks the cursor.
func TestComplianceAuditLog_Pagination(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)
	seedComplianceAuditEvents(t, server)

	admin, err := server.userStore.CreateUser("root2", "RootPassword123!", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, err := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant: %v", err)
	}

	// Page 1: limit=1, expect count=1, has_more=true, total=4 (cross-tenant view).
	code, body := callComplianceAuditLog(t, server, tok, "tenant=*&limit=1&offset=0")
	if code != http.StatusOK {
		t.Fatalf("status: %d", code)
	}
	if c, _ := body["count"].(float64); int(c) != 1 {
		t.Errorf("page 1 count: want 1, got %v", c)
	}
	if t1, _ := body["total"].(float64); int(t1) != 4 {
		t.Errorf("page 1 total: want 4, got %v", t1)
	}
	if body["has_more"] != true {
		t.Errorf("page 1 has_more: want true, got %v", body["has_more"])
	}

	// Past-end offset: empty events, has_more=false.
	code, body = callComplianceAuditLog(t, server, tok, "tenant=*&limit=10&offset=100")
	if code != http.StatusOK {
		t.Fatalf("past-end status: %d", code)
	}
	if c, _ := body["count"].(float64); int(c) != 0 {
		t.Errorf("past-end count: want 0, got %v", c)
	}
	if body["has_more"] != false {
		t.Errorf("past-end has_more: want false, got %v", body["has_more"])
	}
}

// TestComplianceAuditLog_LimitCap verifies the 1000 cap is enforced.
func TestComplianceAuditLog_LimitCap(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	admin, _ := server.userStore.CreateUser("root3", "RootPassword123!", auth.RoleAdmin)
	tok, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")

	_, body := callComplianceAuditLog(t, server, tok, "limit=99999")
	if l, _ := body["limit"].(float64); int(l) != complianceAuditLogMaxLimit {
		t.Errorf("limit cap: want %d, got %v", complianceAuditLogMaxLimit, l)
	}
}

// TestComplianceAuditLog_MethodNotAllowed pins POST/PUT/DELETE return 405.
func TestComplianceAuditLog_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	user, _ := server.userStore.CreateUser("alice-405", "AlicePassword123!", auth.RoleViewer)
	token, _ := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "tenant-a")

	inner := http.HandlerFunc(server.handleComplianceAuditLog)
	handler := server.requireAuth(server.withTenant(inner))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/v1/compliance/audit-log", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rr.Code)
		}
	}
}
