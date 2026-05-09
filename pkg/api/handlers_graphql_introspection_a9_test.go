package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// Audit A9 #4 (2026-05-09): HTTP-level proof that the per-tenant
// GraphQL schema cache (A9 #3, PR #38) actually closes the
// introspection metadata leak surfaced by the A9 spike.
//
// Pre-fix: a single global schema was built at startup from
// gs.GetAllLabels(); any /graphql caller running
// `{ __schema { types { name } } }` could enumerate every label
// registered across every tenant — including labels exclusive to
// other tenants. Cardinal cross-tenant metadata leak.
//
// Post-fix: per-tenant schemas built lazily from
// gs.GetLabelsForTenant(theirID). Cross-tenant labels never enter
// the type registry, so introspection cannot surface them.
//
// This test goes end-to-end through the request path:
//
//	client → withTenant(ctx) → handleGraphQL
//	       → getGraphQLHandlerForTenant → handler.ServeHTTP
//	       → graphql-go introspection
//
// so it catches regressions at any layer (cache key drift, schema
// builder swap, label discovery scope, missing tenant context).

// TestGraphQLIntrospection_DoesNotLeakOtherTenantLabels seeds each
// of two tenants with one *exclusive* label, then runs an
// introspection query as each. Exclusive labels are the load-bearing
// signal: shared labels (e.g. both tenants happen to use "User")
// would correctly appear in both schemas and aren't a leak.
func TestGraphQLIntrospection_DoesNotLeakOtherTenantLabels(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"ASecret"}, nil); err != nil {
		t.Fatalf("seed tenant-A: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"BSecret"}, nil); err != nil {
		t.Fatalf("seed tenant-B: %v", err)
	}

	cases := []struct {
		tenantID string
		wantType string // type the caller's own seed produced
		leakType string // type the OTHER tenant's seed produced — must be absent
	}{
		{"tenant-A", "ASecret", "BSecret"},
		{"tenant-B", "BSecret", "ASecret"},
	}

	for _, tc := range cases {
		t.Run(tc.tenantID, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.handleGraphQL(rr, introspectionRequest(t, tc.tenantID))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
			}
			typeNames := extractIntrospectedTypeNames(t, rr.Body.Bytes())

			if !containsString(typeNames, tc.wantType) {
				t.Errorf("%s: own type %q missing from introspection (got: %v)", tc.tenantID, tc.wantType, typeNames)
			}
			if containsString(typeNames, tc.leakType) {
				t.Errorf("%s: cross-tenant type %q LEAKED into introspection — A9 regression (got: %v)",
					tc.tenantID, tc.leakType, typeNames)
			}
		})
	}
}

// TestGraphQLIntrospection_LabelAddedAfterCacheBuild_LeaksOnRegen
// pins both the cache freshness contract and the security property
// across an invalidate cycle. Two distinct concerns are bundled here
// for readability — the boundary matters for future maintenance:
//
//	[design-pinning] Cache is stale until invalidate, then rebuilds.
//	    Pins the *current* lazy-build design. If the cache strategy
//	    changes (e.g., eager invalidate-on-write), update or relax
//	    these assertions — they are not security-essential.
//
//	[SECURITY] Tenant-B never sees tenant-A's labels, before or
//	    after rebuild. This is the A9 contract; if it fails, the
//	    introspection leak has regressed even with a different cache
//	    strategy.
//
// The combined test guards against a future "rebuild on label
// change" feature accidentally wiring the rebuild path from
// gs.GetAllLabels() — that bug would pass the simpler test 1
// (initial schemas are still tenant-scoped) but fail here.
func TestGraphQLIntrospection_LabelAddedAfterCacheBuild_LeaksOnRegen(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"InitialA"}, nil); err != nil {
		t.Fatalf("seed tenant-A: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"InitialB"}, nil); err != nil {
		t.Fatalf("seed tenant-B: %v", err)
	}

	// Prime tenant-A's cache.
	rr := httptest.NewRecorder()
	server.handleGraphQL(rr, introspectionRequest(t, "tenant-A"))
	if rr.Code != http.StatusOK {
		t.Fatalf("prime: %d body=%s", rr.Code, rr.Body.String())
	}
	primed := extractIntrospectedTypeNames(t, rr.Body.Bytes())
	if !containsString(primed, "InitialA") {
		t.Fatalf("prime: tenant-A missing own initial type 'InitialA': %v", primed)
	}

	// Add a new label *after* the cache is built. The cached schema
	// is frozen, so introspection should not yet see it.
	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"AddedLater"}, nil); err != nil {
		t.Fatalf("add later: %v", err)
	}

	rr = httptest.NewRecorder()
	server.handleGraphQL(rr, introspectionRequest(t, "tenant-A"))
	stale := extractIntrospectedTypeNames(t, rr.Body.Bytes())
	if containsString(stale, "AddedLater") {
		// [design-pinning] If this fails because the cache became
		// eager, that's not a security bug — relax this assertion.
		t.Errorf("cache should be stale; 'AddedLater' visible without invalidate (got: %v)", stale)
	}

	// Invalidate. Next request rebuilds from current labels.
	server.graphqlHandlers.Delete("tenant-A")

	rr = httptest.NewRecorder()
	server.handleGraphQL(rr, introspectionRequest(t, "tenant-A"))
	rebuilt := extractIntrospectedTypeNames(t, rr.Body.Bytes())
	if !containsString(rebuilt, "AddedLater") {
		// [design-pinning] Rebuild path. Not security-essential.
		t.Errorf("after invalidate, 'AddedLater' should be visible (got: %v)", rebuilt)
	}

	// [SECURITY] tenant-B must NOT see tenant-A's labels at any point
	// in this lifecycle — neither InitialA (cached early) nor
	// AddedLater (added after tenant-A's cache invalidation).
	rr = httptest.NewRecorder()
	server.handleGraphQL(rr, introspectionRequest(t, "tenant-B"))
	bView := extractIntrospectedTypeNames(t, rr.Body.Bytes())
	if containsString(bView, "AddedLater") {
		t.Errorf("tenant-B introspection LEAKED tenant-A's 'AddedLater' label — A9 regression (got: %v)", bView)
	}
	if containsString(bView, "InitialA") {
		t.Errorf("tenant-B introspection LEAKED tenant-A's 'InitialA' label — A9 regression (got: %v)", bView)
	}
}

// introspectionRequest builds the canonical introspection POST with
// tenant context wired the way withTenant middleware does. Centralized
// so the body shape stays identical across subtests — accidental
// mismatches would mask leaks as "no data" responses.
func introspectionRequest(t *testing.T, tenantID string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"query": `{ __schema { types { name } } }`,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

// extractIntrospectedTypeNames pulls __schema.types[].name from a
// GraphQL introspection response, failing the test on parse error or
// non-empty errors[]. Returned in registration order; callers should
// use containsString rather than indexing.
func extractIntrospectedTypeNames(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Data struct {
			Schema struct {
				Types []struct {
					Name string `json:"name"`
				} `json:"types"`
			} `json:"__schema"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal introspection: %v body=%s", err, string(body))
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("introspection errors: %v body=%s", resp.Errors, string(body))
	}
	out := make([]string, 0, len(resp.Data.Schema.Types))
	for _, ty := range resp.Data.Schema.Types {
		out = append(out, ty.Name)
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
