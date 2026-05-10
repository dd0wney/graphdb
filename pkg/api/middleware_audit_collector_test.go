package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

// TestAuditCollector_PopulatedAfterAuth pins the load-bearing fix in PR-0
// (audit-collector middleware): a request through the production chain
// auditCollector → audit → requireAuth → withTenant → handler produces an
// audit event with non-empty UserID, Username, and TenantID.
//
// Before this PR, auditMiddleware sat outside requireAuth/withTenant in
// the chain. Both inner middlewares wrap the request via r.WithContext(ctx),
// which is downstream-only — auditMiddleware's r retained the pre-wrap
// context. So r.Context().Value(claimsContextKey) at the audit site always
// returned ok==false, and emitted events had empty user/tenant fields.
// /api/v1/security/audit/logs filters on user_id / tenant_id silently
// matched zero events as a result.
//
// No prior test caught this. The closest, TestAuditMiddleware_WithAuth at
// middleware_test.go:614, injects claims onto the *outer* request before
// calling auditMiddleware directly, bypassing the production middleware
// order, and never reads back the event to assert population.
//
// Discovery in docs/F3_COMPLIANCE_API_DESIGN.md §2.
func TestAuditCollector_PopulatedAfterAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	user, err := server.userStore.CreateUser("alice", "alicepass123", auth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := server.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Subset of the production chain — only the layers that affect identity
	// propagation. auditCollector → audit → requireAuth → withTenant →
	// handler mirrors what server.go's Start() builds for live traffic.
	handler := server.auditCollectorMiddleware(
		server.auditMiddleware(
			server.requireAuth(
				server.withTenant(inner),
			),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	events := server.inMemoryAuditLogger.GetEvents(nil)
	if len(events) == 0 {
		t.Fatal("no audit events emitted by chain")
	}
	last := events[len(events)-1]

	if last.UserID != user.ID {
		t.Errorf("Event.UserID: want %q, got %q", user.ID, last.UserID)
	}
	if last.Username != "alice" {
		t.Errorf("Event.Username: want %q, got %q", "alice", last.Username)
	}
	if last.TenantID == "" {
		t.Error("Event.TenantID: empty — withTenant did not propagate through audit collector")
	}
}

// TestAuditCollector_EmptyOnUnauthRequest verifies graceful degradation:
// an unauthenticated request still produces an event (the failed-auth log)
// with all collector-sourced fields empty. The auditMiddleware must not
// panic when no inner middleware wrote to the collector.
func TestAuditCollector_EmptyOnUnauthRequest(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be reached — requireAuth returns 401 first.
		t.Error("inner handler reached on unauthenticated request")
	})
	handler := server.auditCollectorMiddleware(
		server.auditMiddleware(
			server.requireAuth(
				server.withTenant(inner),
			),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", rr.Code)
	}
	events := server.inMemoryAuditLogger.GetEvents(nil)
	if len(events) == 0 {
		t.Fatal("no audit event emitted for failed-auth request")
	}
	last := events[len(events)-1]
	if last.UserID != "" || last.Username != "" || last.TenantID != "" {
		t.Errorf("unauth event should have empty identity, got UserID=%q Username=%q TenantID=%q",
			last.UserID, last.Username, last.TenantID)
	}
}

// TestAuditCollector_NoCollectorInContext verifies that setAuditUser /
// setAuditTenant are no-ops when called on a context without an attached
// collector. Pins the safety guarantee that requireAuth and withTenant
// can run outside the audit chain (e.g., a hypothetical future test
// harness that calls them directly) without panicking.
func TestAuditCollector_NoCollectorInContext(t *testing.T) {
	ctx := t.Context()
	// Should not panic — both helpers detect nil collector and return
	// silently.
	setAuditUser(ctx, "u1", "alice")
	setAuditTenant(ctx, "tenant-x")

	if c := getAuditCollector(ctx); c != nil {
		t.Errorf("getAuditCollector on bare context: want nil, got %+v", c)
	}
}
