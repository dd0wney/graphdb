package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// A cross-principal equivalence sweep.
//
// graphdb's tenant rule is that a cross-tenant lookup returns the same error as
// a missing one, so a stranger cannot learn that an ID exists. Six
// *TenantIsolation tests assert that a stranger cannot READ another tenant's
// data. None asserts that a stranger cannot DISTINGUISH it from data that never
// existed. Those are different properties and the second is the one that leaks.
//
// On 2026-08-29 a change made a damaged record return a different error class,
// so a stranger got 500 where a stranger asking for a nonexistent ID got 404 —
// an existence oracle. No test caught it. A security review did, and proving the
// fix complete took a reviewer enumerating every entry point by hand.
//
// This file replaces that enumeration with a table.

// observable is what a caller can see. The comparison is byte for byte.
//
// Response time is deliberately NOT here. A cross-tenant lookup that returns
// faster than a genuine miss is a leak this sweep cannot see, and the spec says
// so rather than leaving it to be discovered.
type observable struct {
	status int
	body   string
}

func (o observable) String() string {
	return fmt.Sprintf("%d %q", o.status, o.body)
}

// noAuthMux dispatches to the server's raw handlers, with neither requireAuth
// nor withTenant in the chain.
//
// registerRoutes (server.go) wraps every protected route in requireAuth, and
// withTenant itself reads the auth claims that requireAuth puts in context —
// so driving the real route table would 401 every probe regardless of
// tenantID, adding a second failure mode to every row this sweep checks.
// getTenantFromContext (pkg/api/middleware_tenant.go:113) reads tenant out of
// the context directly; it does not care whether withTenant put it there or a
// test did, so calling the raw handler is transparent to the code under test.
//
// The route table starts with only what this task's control needs. A later
// sweep row adds its own entry here when it exercises a path not yet covered.
func noAuthMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/", s.handleNode) // /nodes/{id}
	return mux
}

// probe issues one request as one tenant and returns what the caller sees.
//
// The tenant goes into the request context directly rather than through a
// token, because this sweep tests the error-to-status mapping and not
// authentication. Driving the auth stack would add a second failure mode to
// every row.
func probe(t *testing.T, s *Server, tenantID, method, path, body string) observable {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r = r.WithContext(tenant.WithTenant(context.Background(), tenantID))
	w := httptest.NewRecorder()
	noAuthMux(s).ServeHTTP(w, r)
	return observable{status: w.Code, body: strings.TrimSpace(w.Body.String())}
}

// TestProbeSeesTheTenant is a control on the driver itself. A probe that
// silently ignored its tenantID would make every later row pass by returning
// identical observables for the wrong reason.
func TestProbeSeesTheTenant(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	owned, err := s.graph.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	path := fmt.Sprintf("/nodes/%d", owned.ID)

	asOwner := probe(t, s, "owner", http.MethodGet, path, "")
	asStranger := probe(t, s, "stranger", http.MethodGet, path, "")

	if asOwner.status != http.StatusOK {
		t.Fatalf("the owner must be able to read its own node, got %s", asOwner)
	}
	if asOwner == asStranger {
		t.Fatalf("the probe ignores its tenant: owner and stranger both got %s", asOwner)
	}
}
