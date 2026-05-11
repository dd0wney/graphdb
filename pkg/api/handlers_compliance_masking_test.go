package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/masking"
)

// callMaskingPolicySet issues a POST to /v1/compliance/masking-policy
// with the given token + body. Returns status + parsed body.
func callMaskingPolicySet(t *testing.T, s *Server, token, xTenant string, body any) (int, map[string]any) {
	t.Helper()
	buf := &bytes.Buffer{}
	if body != nil {
		_ = json.NewEncoder(buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/compliance/masking-policy", buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if xTenant != "" {
		req.Header.Set(TenantIDHeader, xTenant)
	}
	rr := httptest.NewRecorder()
	handler := s.requireAuth(s.withTenant(http.HandlerFunc(s.handleComplianceMaskingPolicy)))
	handler.ServeHTTP(rr, req)

	var resp map[string]any
	if rr.Body.Len() > 0 {
		_ = json.NewDecoder(rr.Body).Decode(&resp)
	}
	return rr.Code, resp
}

// callMaskingPolicyGet issues a GET to /v1/compliance/masking-policy/{tenant}.
func callMaskingPolicyGet(t *testing.T, s *Server, token, pathTenant string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/compliance/masking-policy/"+pathTenant, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler := s.requireAuth(s.withTenant(http.HandlerFunc(s.handleComplianceMaskingPolicy)))
	handler.ServeHTTP(rr, req)

	var resp map[string]any
	if rr.Body.Len() > 0 {
		_ = json.NewDecoder(rr.Body).Decode(&resp)
	}
	return rr.Code, resp
}

// TestComplianceMaskingPolicy_AdminSetThenGet pins the happy path: an
// admin POSTs a policy for tenant-a, GETs it back, observed shape
// matches the input.
func TestComplianceMaskingPolicy_AdminSetThenGet(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	admin, err := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	adminToken, err := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant admin: %v", err)
	}

	body := maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{
			"email": masking.StrategyPartial,
			"ssn":   masking.StrategyFull,
		},
		AutoDetect: true,
	}

	code, resp := callMaskingPolicySet(t, server, adminToken, "", body)
	if code != http.StatusOK {
		t.Fatalf("Set: want 200, got %d resp=%v", code, resp)
	}
	if resp["tenant_id"] != "tenant-a" {
		t.Errorf("Set response tenant_id: want tenant-a, got %v", resp["tenant_id"])
	}

	code, resp = callMaskingPolicyGet(t, server, adminToken, "tenant-a")
	if code != http.StatusOK {
		t.Fatalf("Get: want 200, got %d resp=%v", code, resp)
	}
	props, _ := resp["properties"].(map[string]any)
	if props["email"] != string(masking.StrategyPartial) {
		t.Errorf("email strategy: want %q, got %v", masking.StrategyPartial, props["email"])
	}
	if props["ssn"] != string(masking.StrategyFull) {
		t.Errorf("ssn strategy: want %q, got %v", masking.StrategyFull, props["ssn"])
	}
	if resp["auto_detect"] != true {
		t.Errorf("auto_detect: want true, got %v", resp["auto_detect"])
	}
}

// TestComplianceMaskingPolicy_NonAdminSetForbidden pins admin-only on
// the Set operation, even for the caller's own tenant.
func TestComplianceMaskingPolicy_NonAdminSetForbidden(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	user, err := server.userStore.CreateUser("alice", "AlicePassword123!", auth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	token, err := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "tenant-a")
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant: %v", err)
	}

	body := maskingPolicyRequest{Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyFull}}
	code, _ := callMaskingPolicySet(t, server, token, "", body)
	if code != http.StatusForbidden {
		t.Errorf("non-admin Set: want 403, got %d", code)
	}
}

// TestComplianceMaskingPolicy_NonAdminGetOwnTenant pins read-side
// access rules: non-admin can read their own tenant's policy but not
// another tenant's.
func TestComplianceMaskingPolicy_NonAdminGetOwnTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	// Admin seeds policies for both tenants.
	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminToken, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	_, _ = callMaskingPolicySet(t, server, adminToken, "", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyFull},
	})
	_, _ = callMaskingPolicySet(t, server, adminToken, "tenant-b", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"ssn": masking.StrategyFull},
	})

	// Non-admin Alice (tenant-a) reads.
	user, _ := server.userStore.CreateUser("alice", "AlicePassword123!", auth.RoleViewer)
	userToken, _ := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "tenant-a")

	t.Run("own tenant returns 200", func(t *testing.T) {
		code, resp := callMaskingPolicyGet(t, server, userToken, "tenant-a")
		if code != http.StatusOK {
			t.Errorf("own-tenant Get: want 200, got %d resp=%v", code, resp)
		}
	})

	t.Run("other tenant returns 403", func(t *testing.T) {
		code, _ := callMaskingPolicyGet(t, server, userToken, "tenant-b")
		if code != http.StatusForbidden {
			t.Errorf("other-tenant Get: want 403, got %d", code)
		}
	})
}

// TestComplianceMaskingPolicy_GetMissingReturns404 pins that a tenant
// with no policy set returns 404, not an empty 200.
func TestComplianceMaskingPolicy_GetMissingReturns404(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminToken, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")

	code, _ := callMaskingPolicyGet(t, server, adminToken, "tenant-a")
	if code != http.StatusNotFound {
		t.Errorf("missing policy Get: want 404, got %d", code)
	}
}

// TestComplianceMaskingPolicy_AdminCrossTenantSet pins Decision 1c
// equivalent for masking-policy: admin + X-Tenant-ID can target a
// different tenant's policy.
func TestComplianceMaskingPolicy_AdminCrossTenantSet(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	// Admin logged in as tenant-a, but targets tenant-b via header.
	adminToken, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")

	body := maskingPolicyRequest{Properties: map[string]masking.MaskingStrategy{"ssn": masking.StrategyHash}}
	code, resp := callMaskingPolicySet(t, server, adminToken, "tenant-b", body)
	if code != http.StatusOK {
		t.Fatalf("admin cross-tenant Set: want 200, got %d resp=%v", code, resp)
	}
	if resp["tenant_id"] != "tenant-b" {
		t.Errorf("response tenant_id: want tenant-b, got %v", resp["tenant_id"])
	}

	// Verify by GET on tenant-b
	code, resp = callMaskingPolicyGet(t, server, adminToken, "tenant-b")
	if code != http.StatusOK {
		t.Fatalf("verifying Get: want 200, got %d", code)
	}
	props, _ := resp["properties"].(map[string]any)
	if props["ssn"] != string(masking.StrategyHash) {
		t.Errorf("ssn strategy: want %q, got %v", masking.StrategyHash, props["ssn"])
	}
}

// TestComplianceMaskingPolicy_InvalidStrategyRejected pins the 400
// boundary: unknown strategy names get clear errors rather than
// silently falling back to maskPartial.
func TestComplianceMaskingPolicy_InvalidStrategyRejected(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminToken, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")

	body := maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.MaskingStrategy("scramble")},
	}
	code, _ := callMaskingPolicySet(t, server, adminToken, "", body)
	if code != http.StatusBadRequest {
		t.Errorf("invalid strategy: want 400, got %d", code)
	}
}
