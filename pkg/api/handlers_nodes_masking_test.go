package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/masking"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// seedMaskingNode creates a node with the given properties under
// tenantID directly via the storage layer, returning its ID. Bypasses
// the API so the test can control input precisely.
func seedMaskingNode(t *testing.T, s *Server, tenantID, label string, props map[string]storage.Value) uint64 {
	t.Helper()
	node, err := s.graph.CreateNodeWithTenant(tenantID, []string{label}, props)
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}
	return node.ID
}

// getNodeAsUser issues GET /nodes/{id} via the production middleware
// chain, parsing the response body. Returns status + response.
func getNodeAsUser(t *testing.T, s *Server, token, xTenant string, nodeID uint64) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/nodes/"+strconv.FormatUint(nodeID, 10), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if xTenant != "" {
		req.Header.Set(TenantIDHeader, xTenant)
	}
	rr := httptest.NewRecorder()
	handler := s.requireAuth(s.withTenant(http.HandlerFunc(s.handleNode)))
	handler.ServeHTTP(rr, req)
	var body map[string]any
	if rr.Body.Len() > 0 {
		_ = json.NewDecoder(rr.Body).Decode(&body)
	}
	return rr.Code, body
}

// TestMasking_NodeGet_PolicyAppliesToReadPath is the load-bearing
// test for F3 PR-3: a tenant with a masking policy sees masked
// properties on the standard /nodes/{id} read path.
func TestMasking_NodeGet_PolicyAppliesToReadPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	// Seed: tenant-a has a node with sensitive properties.
	nodeID := seedMaskingNode(t, server, "tenant-a", "Person", map[string]storage.Value{
		"name":  storage.StringValue("alice"),
		"email": storage.StringValue("alice@example.com"),
		"ssn":   storage.StringValue("123-45-6789"),
	})

	// Admin sets tenant-a's policy: full-mask email + ssn, leave name alone.
	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminTokenA, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	_, _ = callMaskingPolicySet(t, server, adminTokenA, "", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{
			"email": masking.StrategyFull,
			"ssn":   masking.StrategyFull,
		},
	})

	// Alice (tenant-a viewer) reads the node.
	alice, _ := server.userStore.CreateUser("alice", "AlicePassword123!", auth.RoleViewer)
	aliceToken, _ := server.jwtManager.GenerateTokenWithTenant(alice.ID, alice.Username, alice.Role, "tenant-a")

	code, body := getNodeAsUser(t, server, aliceToken, "", nodeID)
	if code != http.StatusOK {
		t.Fatalf("Get tenant-a's own node: want 200, got %d body=%v", code, body)
	}
	props, _ := body["properties"].(map[string]any)
	if props["name"] != "alice" {
		t.Errorf("name should be unmasked (not in policy), got %v", props["name"])
	}
	if props["email"] == "alice@example.com" {
		t.Errorf("email should be masked; got verbatim %q", props["email"])
	}
	if props["ssn"] == "123-45-6789" {
		t.Errorf("ssn should be masked; got verbatim %q", props["ssn"])
	}
}

// TestMasking_PolicyFollowsTenant pins the load-bearing F3 invariant
// called out in NEXT_SESSION_PROMPT.md: an admin reading tenant-A's
// node via X-Tenant-ID admin-override sees tenant-A's policy applied,
// NOT the admin's resident tenant policy. The masking-policy resolution
// follows the resolved tenant, not the caller.
func TestMasking_PolicyFollowsTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	// Seed: tenant-A and tenant-B each get a node + a distinct policy.
	nodeA := seedMaskingNode(t, server, "tenant-a", "Person", map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})
	nodeB := seedMaskingNode(t, server, "tenant-b", "Person", map[string]storage.Value{
		"email": storage.StringValue("bob@example.com"),
	})

	// Admin sets distinct policies.
	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminTokenA, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")

	// tenant-A: full-mask email.
	_, _ = callMaskingPolicySet(t, server, adminTokenA, "", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyFull},
	})

	// tenant-B: hash email (different strategy — easy to distinguish in output).
	_, _ = callMaskingPolicySet(t, server, adminTokenA, "tenant-b", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyHash},
	})

	// Admin reads tenant-A's node WITHOUT override (resident tenant = tenant-A).
	code, body := getNodeAsUser(t, server, adminTokenA, "", nodeA)
	if code != http.StatusOK {
		t.Fatalf("admin GET tenant-A's node (own resident): want 200, got %d", code)
	}
	propsA1, _ := body["properties"].(map[string]any)
	maskedFull := propsA1["email"]

	// Admin reads tenant-B's node WITH X-Tenant-ID override.
	code, body = getNodeAsUser(t, server, adminTokenA, "tenant-b", nodeB)
	if code != http.StatusOK {
		t.Fatalf("admin GET tenant-B's node via X-Tenant-ID: want 200, got %d", code)
	}
	propsB, _ := body["properties"].(map[string]any)
	maskedHash := propsB["email"]

	// The two masked outputs must DIFFER — tenant-A's full-mask result
	// is a string of MaskChars matching the email length; tenant-B's
	// hash result is hex.
	if maskedFull == maskedHash {
		t.Errorf("policy did not follow tenant: tenant-A masked = tenant-B masked = %v "+
			"(should differ because policies differ)", maskedFull)
	}
	if maskedFull == "alice@example.com" {
		t.Errorf("tenant-A's email returned unmasked when read with override")
	}
	if maskedHash == "bob@example.com" {
		t.Errorf("tenant-B's email returned unmasked when read via admin override")
	}
}

// TestMasking_NoPolicy_PassthroughPreservesPreF3Behavior pins that a
// tenant with no policy set sees identical output to pre-F3.
func TestMasking_NoPolicy_PassthroughPreservesPreF3Behavior(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	nodeID := seedMaskingNode(t, server, "tenant-a", "Person", map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})

	// No policy set for tenant-a. Read as a tenant-a user.
	user, _ := server.userStore.CreateUser("alice", "AlicePassword123!", auth.RoleViewer)
	userToken, _ := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "tenant-a")

	code, body := getNodeAsUser(t, server, userToken, "", nodeID)
	if code != http.StatusOK {
		t.Fatalf("Get: want 200, got %d", code)
	}
	props, _ := body["properties"].(map[string]any)
	if props["email"] != "alice@example.com" {
		t.Errorf("no-policy tenant should see verbatim; got %v", props["email"])
	}
}

// TestMasking_NodeList_PolicyAppliesAcrossCollection pins that masking
// fires on the list path (/nodes) too, not just single-node GET. The
// universal hook means this should work automatically — the test pins
// against regressions where someone adds a parallel list path that
// bypasses nodeToResponse.
func TestMasking_NodeList_PolicyAppliesAcrossCollection(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	for i := 0; i < 3; i++ {
		seedMaskingNode(t, server, "tenant-a", "Person", map[string]storage.Value{
			"email": storage.StringValue("user" + strconv.Itoa(i) + "@example.com"),
		})
	}

	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminToken, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")
	_, _ = callMaskingPolicySet(t, server, adminToken, "", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyRedact},
	})

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	server.requireAuth(server.withTenant(http.HandlerFunc(server.handleNodes))).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("List: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var arr []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(arr) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(arr))
	}
	for i, node := range arr {
		props, _ := node["properties"].(map[string]any)
		email, _ := props["email"].(string)
		if email != "[REDACTED]" {
			t.Errorf("nodes[%d].email: want [REDACTED], got %q", i, email)
		}
	}
}
