package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/masking"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// gqlPropertiesAsMap parses the JSON-encoded "properties" string returned
// by the GraphQL node-properties resolver. The schema declares properties
// as a Float-or-String scalar — it serializes the masked map as a JSON
// object string. This helper unwraps that one level of stringification.
func gqlPropertiesAsMap(t *testing.T, propsField any) map[string]any {
	t.Helper()
	s, ok := propsField.(string)
	if !ok {
		t.Fatalf("properties field is %T, want string (JSON-encoded map)", propsField)
	}
	// The resolver emits a JSON-like representation but with bare keys; try
	// strict JSON first, fall back to permissive parsing only if needed.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(s), &parsed); err == nil {
		return parsed
	}
	// Resolver currently emits {"k": "v"} form (see schema.go's resolver).
	// If that ever changes to a non-JSON shape this will fail loudly.
	t.Fatalf("properties field not parseable as JSON: %q", s)
	return nil
}

// graphqlQueryAs issues a POST /graphql via the production middleware
// chain (requireAuth → withTenant → handleGraphQL). Returns status +
// decoded body. Tenant comes from the JWT in token.
func graphqlQueryAs(t *testing.T, s *Server, token, query string) (int, map[string]any) {
	t.Helper()
	reqBody := map[string]any{"query": query}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()
	handler := s.requireAuth(s.withTenant(http.HandlerFunc(s.handleGraphQL)))
	handler.ServeHTTP(rr, req)

	var resp map[string]any
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	}
	return rr.Code, resp
}

// TestGraphQL_Masking_PolicyFollowsTenant is the GraphQL twin of REST's
// TestMasking_PolicyFollowsTenant. It pins that the production GraphQL
// path (handleGraphQL → getGraphQLHandlerForTenant →
// GenerateSchemaWithLimitsForTenant → createNodeType's properties
// resolver → applyMaskingPolicyForGraphQL → Policy.ApplyToStorageValues)
// applies the resolved tenant's masking policy to query results, not
// the caller's.
func TestGraphQL_Masking_PolicyFollowsTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	// Seed: tenant-A and tenant-B each get a node with a distinct email.
	_ = seedMaskingNode(t, server, "tenant-a", "Person", map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})
	_ = seedMaskingNode(t, server, "tenant-b", "Person", map[string]storage.Value{
		"email": storage.StringValue("bob@example.com"),
	})

	// Admin sets distinct policies via the REST masking-policy endpoint
	// (already covered by PR-3a's tests). Reusing the REST surface here
	// keeps the test focused on whether GraphQL respects the policy, not
	// on how the policy is set.
	admin, _ := server.userStore.CreateUser("root", "RootPassword123!", auth.RoleAdmin)
	adminTokenA, _ := server.jwtManager.GenerateTokenWithTenant(admin.ID, admin.Username, admin.Role, "tenant-a")

	_, _ = callMaskingPolicySet(t, server, adminTokenA, "", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyFull},
	})
	_, _ = callMaskingPolicySet(t, server, adminTokenA, "tenant-b", maskingPolicyRequest{
		Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyHash},
	})

	// Each tenant gets a viewer user; queries go via the user's JWT so
	// the GraphQL handler resolves the tenant via withTenant middleware
	// from the JWT claim — the same path production uses.
	userA, _ := server.userStore.CreateUser("alice", "AlicePassword123!", auth.RoleViewer)
	userATok, _ := server.jwtManager.GenerateTokenWithTenant(userA.ID, userA.Username, userA.Role, "tenant-a")
	userB, _ := server.userStore.CreateUser("bob", "BobPassword123!", auth.RoleViewer)
	userBTok, _ := server.jwtManager.GenerateTokenWithTenant(userB.ID, userB.Username, userB.Role, "tenant-b")

	// Regenerate per-tenant schemas so each tenant's Person label is
	// visible in introspection. Audit A9 makes schemas tenant-scoped;
	// the cache is keyed on tenantID. Seeding via storage doesn't
	// invalidate the cache, so we hit the regenerate endpoint via the
	// admin token — same pattern other GraphQL tests use.
	regen := func(token string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/schema/regenerate", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		handler := server.requireAuth(server.withTenant(http.HandlerFunc(server.handleSchemaRegenerate)))
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("schema regenerate: %d %s", rr.Code, rr.Body.String())
		}
	}
	regen(userATok)
	regen(userBTok)

	// Tenant-A: full-mask email. The viewer queries their own tenant.
	codeA, respA := graphqlQueryAs(t, server, userATok, `{ persons { id properties } }`)
	if codeA != http.StatusOK {
		t.Fatalf("tenant-A GraphQL: want 200, got %d body=%v", codeA, respA)
	}
	if errs, ok := respA["errors"]; ok && errs != nil {
		t.Fatalf("tenant-A GraphQL errors: %v", errs)
	}
	maskedFull := extractFirstPersonEmail(t, respA)

	// Tenant-B: hash email.
	codeB, respB := graphqlQueryAs(t, server, userBTok, `{ persons { id properties } }`)
	if codeB != http.StatusOK {
		t.Fatalf("tenant-B GraphQL: want 200, got %d body=%v", codeB, respB)
	}
	if errs, ok := respB["errors"]; ok && errs != nil {
		t.Fatalf("tenant-B GraphQL errors: %v", errs)
	}
	maskedHash := extractFirstPersonEmail(t, respB)

	// Load-bearing assertion: masked outputs must differ (distinct
	// policies) and neither should be the verbatim email.
	if maskedFull == maskedHash {
		t.Errorf("policy did not follow tenant: tenant-A masked = tenant-B masked = %q "+
			"(should differ because policies differ)", maskedFull)
	}
	if strings.Contains(maskedFull, "alice@example.com") {
		t.Errorf("tenant-A email leaked unmasked via GraphQL: %q", maskedFull)
	}
	if strings.Contains(maskedHash, "bob@example.com") {
		t.Errorf("tenant-B email leaked unmasked via GraphQL: %q", maskedHash)
	}
}

// TestGraphQL_Masking_NoPolicy_PassthroughPreservesPreF3Behavior pins
// that a tenant with no masking policy sees identical GraphQL output to
// pre-F3 — the hook is structurally inert when there's no policy to
// apply, not implicitly masking-by-default.
func TestGraphQL_Masking_NoPolicy_PassthroughPreservesPreF3Behavior(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedComplianceTestTenants(t, server)

	_ = seedMaskingNode(t, server, "tenant-a", "Person", map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})

	user, _ := server.userStore.CreateUser("alice", "AlicePassword123!", auth.RoleViewer)
	userTok, _ := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "tenant-a")

	// Regenerate schema so the Person label is visible.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schema/regenerate", nil)
	req.Header.Set("Authorization", "Bearer "+userTok)
	rr := httptest.NewRecorder()
	handler := server.requireAuth(server.withTenant(http.HandlerFunc(server.handleSchemaRegenerate)))
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("schema regenerate: %d %s", rr.Code, rr.Body.String())
	}

	code, resp := graphqlQueryAs(t, server, userTok, `{ persons { id properties } }`)
	if code != http.StatusOK {
		t.Fatalf("GraphQL: %d body=%v", code, resp)
	}
	email := extractFirstPersonEmail(t, resp)
	if !strings.Contains(email, "alice@example.com") {
		t.Errorf("no-policy tenant should see verbatim email; got %q", email)
	}
}

// extractFirstPersonEmail walks a GraphQL response shape:
//
//	{ "data": { "persons": [ { "id": "...", "properties": "{\"email\":\"...\"}" } ] } }
//
// returning the "email" field from the first person's properties string.
// Fails the test if the shape doesn't match.
func extractFirstPersonEmail(t *testing.T, resp map[string]any) string {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("response missing data: %v", resp)
	}
	persons, ok := data["persons"].([]any)
	if !ok || len(persons) == 0 {
		t.Fatalf("response missing or empty persons: %v", data)
	}
	first, ok := persons[0].(map[string]any)
	if !ok {
		t.Fatalf("first person not a map: %T", persons[0])
	}
	props := gqlPropertiesAsMap(t, first["properties"])
	email, _ := props["email"].(string)
	return email
}
