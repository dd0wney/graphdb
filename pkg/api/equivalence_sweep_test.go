package api

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
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
// A later sweep row adds its own entry here when it exercises a path not yet
// covered. noAuthMuxPatterns lists the same strings, once, so the control
// below can check them without re-parsing this function.
func noAuthMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/nodes/", s.handleNode) // /nodes/{id}
	mux.HandleFunc("/edges/", s.handleEdge) // /edges/{id}
	mux.HandleFunc("/edges", s.handleEdges) // POST /edges
	return mux
}

// noAuthMuxPatterns are the pattern strings registered in noAuthMux above.
var noAuthMuxPatterns = []string{"/nodes/", "/edges/", "/edges"}

// productionPatterns parses pkg/api/server.go and returns the set of path
// patterns registerRoutes passes to mux.HandleFunc. Reading the source, rather
// than hand-listing production's routes a second time, is what makes the
// control below honest: a hand-written list would carry the same copy-paste
// risk it exists to catch. The idiom matches
// pkg/cluster/import_guard_test.go, which parses Go source with go/parser for
// the same reason.
//
// Only sees a pattern given as a string literal. server.go's 41
// mux.HandleFunc( call sites are all string literals today; a future one
// built from a computed value would parse past this function silently and
// the control below would stop covering that pattern without failing.
func productionPatterns(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server.go", nil, 0)
	if err != nil {
		t.Fatalf("parse server.go: %v", err)
	}

	patterns := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		patterns[value] = true
		return true
	})
	return patterns
}

// TestNoAuthMuxMatchesProductionRoutes is a control on the driver, not on the
// server. A typo in a pattern string registered in noAuthMux would make
// ServeMux answer 404 for every request to it — and 404 is precisely what the
// sweep expects a stranger to see, so every row would pass while exercising
// nothing.
//
// The patterns this driver serves must be a subset of the ones production
// registers, matched by string.
func TestNoAuthMuxMatchesProductionRoutes(t *testing.T) {
	production := productionPatterns(t)
	for _, p := range noAuthMuxPatterns {
		if !production[p] {
			t.Errorf("noAuthMux serves %q, which production does not register: "+
				"a request to it returns 404 from the mux, which the sweep cannot "+
				"tell from a real not-found", p)
		}
	}
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

// sweepRow is one operation that names a caller-supplied resource ID.
//
// pathFmt takes the varying ID once, through %d, in the path. bodyFmt takes
// zero, one, or two %d verbs: zero for a body with no ID (PUT's fixed
// property update), one for a body naming only the varying ID, two for a
// POST /edges row, whose body names TWO node IDs — the varying one and a
// fixed peer that must stay valid so the varying check is the one reached.
//
// A row with two %d verbs always fills the FIRST from the caller's id and the
// SECOND from fixedID, regardless of which JSON key each verb sits under —
// object key order does not matter to the decoder, so a row is free to put
// the varying key first in its own literal to keep that convention uniform.
type sweepRow struct {
	name    string
	method  string
	pathFmt string
	bodyFmt string
	// fixedID is the OTHER node id a POST /edges row's body needs — the one
	// held constant across both the "owned" and "missing" probes of the SAME
	// row so it keeps passing its own tenant check and execution reaches the
	// check the row exists to exercise. Set by the test once the fixture that
	// owns it exists; the zero value is unused by every row whose bodyFmt has
	// fewer than two %d verbs.
	fixedID uint64
}

func (r sweepRow) request(id uint64) (path, body string) {
	path = r.pathFmt
	if strings.Contains(path, "%d") {
		path = fmt.Sprintf(path, id)
	}
	switch strings.Count(r.bodyFmt, "%d") {
	case 0:
		body = r.bodyFmt
	case 1:
		body = fmt.Sprintf(r.bodyFmt, id)
	default:
		body = fmt.Sprintf(r.bodyFmt, id, r.fixedID)
	}
	return path, body
}

// Every endpoint that names a resource the caller may not own.
//
// Rows 7 and 8 are POST /edges in each endpoint position. They are separate
// because CreateEdgeWithTenant verifies the source and the target in separate
// calls (edge_operations.go:61 and :64), and a guard on one is not a guard on
// the other. Row 7 is also the row a hand-written suite forgets, and it is the
// one that leaked on 2026-08-29.
//
// The fixed peer in each POST row's body is a %d, not a hand-picked constant.
// A first draft of this table fixed it to the literal 1 — which is exactly
// the ID a fresh store gives its first node, so row 8's "fixed" source and
// its "varying" target both named the same node, both probes failed the
// SOURCE check first (edge_operations.go:61, which returns early), and the
// target check (:64) was never reached. The row passed while silently
// re-testing row 7. Fixed by making the fixed peer a real, probing-tenant-
// owned node (TestCrossPrincipalEquivalence_IntactStore's strangerNode, via
// sweepRow.fixedID) instead of a guessed number, for both rows — row 7's
// fixed target had the same latent problem in reverse, saved only by
// evaluation order.
var equivalenceRows = []sweepRow{
	{name: "GET node", method: http.MethodGet, pathFmt: "/nodes/%d"},
	{name: "PUT node", method: http.MethodPut, pathFmt: "/nodes/%d",
		bodyFmt: `{"properties":{"x":"y"}}`},
	{name: "DELETE node", method: http.MethodDelete, pathFmt: "/nodes/%d"},
	{name: "GET edge", method: http.MethodGet, pathFmt: "/edges/%d"},
	{name: "PUT edge", method: http.MethodPut, pathFmt: "/edges/%d",
		bodyFmt: `{"properties":{"x":"y"}}`},
	{name: "DELETE edge", method: http.MethodDelete, pathFmt: "/edges/%d"},
	// Varying source first, so the %d order is (id, fixedID): source varies,
	// target stays a probing-tenant-owned node.
	{name: "POST edge, source position", method: http.MethodPost, pathFmt: "/edges",
		bodyFmt: `{"from_node_id":%d,"to_node_id":%d,"type":"LINKS"}`},
	// Varying target first, so the %d order is still (id, fixedID) even
	// though "to_node_id" now leads the JSON — object key order does not
	// matter to the decoder, and keeping the verb order uniform keeps
	// sweepRow.request simple.
	{name: "POST edge, target position", method: http.MethodPost, pathFmt: "/edges",
		bodyFmt: `{"to_node_id":%d,"from_node_id":%d,"type":"LINKS"}`},
}

// TestCrossPrincipalEquivalence_IntactStore asserts that for every row, a
// stranger asking about a resource owned by another tenant sees exactly what a
// stranger asking about an ID that never existed sees.
func TestCrossPrincipalEquivalence_IntactStore(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	ownedNode, err := s.graph.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("alpha")})
	if err != nil {
		t.Fatalf("create owner node: %v", err)
	}
	otherNode, err := s.graph.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("beta")})
	if err != nil {
		t.Fatalf("create second owner node: %v", err)
	}
	ownedEdge, err := s.graph.CreateEdgeWithTenant("owner", ownedNode.ID, otherNode.ID,
		"LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("create owner edge: %v", err)
	}

	// Owned by the PROBING tenant ("stranger"), not by "owner". A POST /edges
	// row's fixed peer must pass its own tenant check so execution reaches the
	// check the row exists to test — see the comment on equivalenceRows.
	strangerNode, err := s.graph.CreateNodeWithTenant("stranger", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("stranger-own")})
	if err != nil {
		t.Fatalf("create stranger node: %v", err)
	}

	// An ID far beyond anything created. Both probes must agree that it is
	// absent, which is the reference the comparison is made against.
	const neverExisted uint64 = 999999

	for _, row := range equivalenceRows {
		row.fixedID = strangerNode.ID
		t.Run(row.name, func(t *testing.T) {
			existing := ownedNode.ID
			if strings.Contains(row.pathFmt, "/edges/") {
				existing = ownedEdge.ID
			}

			ownedPath, ownedBody := row.request(existing)
			missingPath, missingBody := row.request(neverExisted)

			got := probe(t, s, "stranger", row.method, ownedPath, ownedBody)
			want := probe(t, s, "stranger", row.method, missingPath, missingBody)

			if got != want {
				t.Errorf("existence leak: a resource owned by another tenant is "+
					"distinguishable from one that never existed\n"+
					"  owned by \"owner\": %s\n"+
					"  never existed:    %s", got, want)
			}
		})
	}
}
