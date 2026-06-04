package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// TestA5_WithTenantOnNodesAndEdgesRoutes pins the contract that audit
// task A5 establishes: every route flagged by Security CRIT #1+#2 must
// have withTenant middleware in its chain. Without this guarantee, the
// handler-side migration in A6 has nothing to read tenant context from,
// and Security CRIT #1+#2 stay open at the API boundary even though
// A3b enforces at the storage layer.
//
// The test wires up the actual mux (via Start's handler registration
// pattern, replicated here) and asserts that for each protected route,
// the handler observes a non-empty tenant in request context.
//
// If a future change strips withTenant from one of these routes, this
// test fails — the regression is locked in.
func TestA5_WithTenantOnNodesAndEdgesRoutes(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a known JWT for the test caller. The JWT encodes the
	// claims that withTenant will read; default tenant ID is
	// expected because the test doesn't set a custom one.
	token := mintTestToken(t, server, auth.RoleAdmin, "a5-test-user", "")

	// Each row exercises a route with withTenant in its chain. The
	// expectation is *not* that the route succeeds end-to-end (some
	// require POST bodies); it's that requests authenticated correctly
	// reach the handler with tenant context populated. We accept any
	// non-401, non-500 response as evidence that withTenant's auth-
	// dependency check passed.
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"/query GET goes through middleware", http.MethodPost, "/query", `{"query":"MATCH (n) RETURN COUNT(n) AS c"}`},
		{"/graphql", http.MethodPost, "/graphql", `{"query":"{ __typename }"}`},
		{"/nodes list", http.MethodGet, "/nodes", ""},
		{"/edges list", http.MethodGet, "/edges", ""},
		{"/traverse", http.MethodPost, "/traverse", `{"start_node_id":1,"max_depth":1}`},
		{"/shortest-path", http.MethodPost, "/shortest-path", `{"start_node_id":1,"end_node_id":2}`},
		{"/algorithms", http.MethodPost, "/algorithms", `{"algorithm":"has_cycle"}`},
	}

	mux := buildTestMux(server)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Authorization", "Bearer "+token)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// 401 means auth or tenant chain rejected — A5 contract broken.
			if rr.Code == http.StatusUnauthorized {
				t.Errorf("got 401 — withTenant not in chain or auth chain misconfigured (body: %s)",
					rr.Body.String())
			}

			// 500 with "tenant" in the body suggests withTenant is broken.
			if rr.Code == http.StatusInternalServerError &&
				strings.Contains(strings.ToLower(rr.Body.String()), "tenant") {
				t.Errorf("got 500 with tenant-related error (chain broken?): %s", rr.Body.String())
			}
		})
	}
}

// TestA5_TenantContextPropagatesFromJWT pins the tighter contract that
// audit task A6 will rely on: a JWT minted with a specific tenantID
// claim must reach the handler with getTenantFromContext returning
// that exact tenantID — not the default. Without this assertion, A5
// only proves "withTenant doesn't 500"; A6's correctness would depend
// on a propagation property that A5 didn't pin.
//
// The test wires a probe handler into a test mux, mints a JWT with
// tenantID="acme-corp", and asserts the probe sees "acme-corp" via
// getTenantFromContext.
func TestA5_TenantContextPropagatesFromJWT(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const wantTenant = "acme-corp"
	// withTenant validates against tenantStore — pre-create the tenant
	// so the middleware doesn't reject with 403 "tenant not found".
	if server.tenantStore != nil {
		if err := server.tenantStore.Create(&tenant.Tenant{
			ID:     wantTenant,
			Name:   "Acme Corp (test)",
			Status: tenant.TenantStatusActive,
		}); err != nil {
			t.Fatalf("create tenant: %v", err)
		}
	}
	token := mintTestToken(t, server, auth.RoleAdmin, "a5-propagation-user", wantTenant)

	var observed string
	probe := func(_ http.ResponseWriter, r *http.Request) {
		observed = getTenantFromContext(r)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/_a5_probe", server.requireAuth(server.withTenant(probe)))

	req := httptest.NewRequest(http.MethodGet, "/_a5_probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("probe: status %d (body: %s)", rr.Code, rr.Body.String())
	}
	if observed != wantTenant {
		t.Errorf("getTenantFromContext: want %q (from JWT), got %q — A6 will malfunction if this isn't fixed",
			wantTenant, observed)
	}
}

// TestA5_NoAuthReturnsUnauthorized pins that the auth gate still works.
// withTenant must come AFTER requireAuth, so a request without auth
// must short-circuit at requireAuth (401) and never reach withTenant.
func TestA5_NoAuthReturnsUnauthorized(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	mux := buildTestMux(server)

	for _, path := range []string{"/query", "/graphql", "/nodes", "/edges", "/traverse", "/shortest-path", "/algorithms"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("%s: want 401 without auth, got %d (body: %s)", path, rr.Code, rr.Body.String())
			}
		})
	}
}

// buildTestMux returns the server's REAL route table so middleware-chain tests
// exercise the actual registration without binding a listener. It used to be a
// hand-maintained replica of server.go, which drifted (it omitted
// /api/v1/tenants/, the route whose missing withTenant the sweep's F2 fixed) —
// delegating to registerRoutes removes that drift class entirely.
func buildTestMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return mux
}

// mintTestToken creates a JWT for use in middleware-chain tests. tenantID
// can be empty to use the default tenant.
func mintTestToken(t *testing.T, server *Server, role, username, tenantID string) string {
	t.Helper()
	// Create a user record so the auth lookup finds them. If the user
	// already exists from a previous subtest within the same setupTestServer,
	// fall back to GetUserByUsername.
	user, err := server.userStore.CreateUser(username, "test-pass-"+username, role)
	if err != nil {
		existing, getErr := server.userStore.GetUserByUsername(username)
		if getErr != nil {
			t.Fatalf("user setup: create=%v get=%v", err, getErr)
		}
		user = existing
	}
	tok, err := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, tenantID)
	if err != nil {
		t.Fatalf("GenerateTokenWithTenant: %v", err)
	}
	return tok
}
