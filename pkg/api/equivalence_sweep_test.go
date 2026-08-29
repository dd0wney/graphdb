package api

import (
	"context"
	"errors"
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
	"github.com/dd0wney/graphdb/pkg/storage/storagetest"
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
// CONSUMER CONTRACT: CC12-existence-indistinguishable — all REST consumers (PR C)
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

// damagedFixture is a server whose store was closed, damaged on disk, and
// reopened. Every field is an ID the sweep needs to name.
type damagedFixture struct {
	server *Server
	// damagedNode and damagedEdge no longer decode. The snapshot CRC covers
	// the header, the directories and the metadata blob, but not record
	// bodies, so the file still opens and the damage reaches a decoder.
	damagedNode uint64
	damagedEdge uint64
	// ownerPeer is an undamaged node of tenant "owner". It is the fixed peer
	// for the OWNER's POST /edges probes, and the negative control that the
	// reopen worked.
	ownerPeer uint64
	// strangerPeer is an undamaged node of tenant "stranger", the fixed peer
	// for the STRANGER's POST /edges probes. Each principal needs a peer of
	// its own: a peer the prober does not own fails the first endpoint check
	// and the row never reaches the check it exists to exercise.
	strangerPeer uint64
}

// newDamagedFixture builds the store in mmap mode, closes it so the snapshot
// is written, damages one node record and one edge record, reopens, and serves.
//
// setupTestServer cannot be reused: it opens the store and never closes it, and
// the snapshot only exists on disk after a close.
func newDamagedFixture(t *testing.T) damagedFixture {
	t.Helper()

	// t.TempDir registers its removal first, so it runs LAST — after the store
	// below is closed.
	dir := t.TempDir()

	cfg := storage.DefaultStorageConfig(dir)
	cfg.UseMmapSnapshot = true // the JSON path has no record to damage

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	damagedNode, err := gs.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("alpha")})
	if err != nil {
		t.Fatalf("create the node to damage: %v", err)
	}
	ownerPeer, err := gs.CreateNodeWithTenant("owner", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("owner-peer")})
	if err != nil {
		t.Fatalf("create the owner's peer: %v", err)
	}
	// The damaged EDGE joins two undamaged nodes. An edge hanging off the
	// damaged node would leave every edge row with two possible causes.
	edgeTail, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create the edge tail: %v", err)
	}
	damagedEdge, err := gs.CreateEdgeWithTenant("owner", ownerPeer.ID, edgeTail.ID, "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("create the edge to damage: %v", err)
	}
	strangerPeer, err := gs.CreateNodeWithTenant("stranger", []string{"Thing"},
		map[string]storage.Value{"name": storage.StringValue("stranger-own")})
	if err != nil {
		t.Fatalf("create the stranger's peer: %v", err)
	}

	if err := gs.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	storagetest.DamageNodeRecord(t, dir, damagedNode.ID)
	storagetest.DamageEdgeRecord(t, dir, damagedEdge.ID)

	reopened, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	// POSITIVE CONTROL on the fault, taken at the storage layer rather than
	// through a handler. GET collapses every error to 404 (getNode,
	// handlers_nodes.go), so the HTTP surface cannot show that the damage
	// took, and a sweep whose fault injector had quietly stopped working
	// would pass every row.
	if _, err := reopened.GetNodeForTenant(damagedNode.ID, "owner"); !errors.Is(err, storage.ErrRecordUnreadable) {
		t.Fatalf("the node fault did not take: the owner reads node %d as %v, "+
			"want ErrRecordUnreadable", damagedNode.ID, err)
	}
	if _, err := reopened.GetEdgeForTenant(damagedEdge.ID, "owner"); !errors.Is(err, storage.ErrRecordUnreadable) {
		t.Fatalf("the edge fault did not take: the owner reads edge %d as %v, "+
			"want ErrRecordUnreadable", damagedEdge.ID, err)
	}

	// NEGATIVE CONTROLS. Without these a reopen that lost everything, or a
	// fault that damaged the whole file, would look exactly like a clean pass.
	if _, err := reopened.GetNodeForTenant(ownerPeer.ID, "owner"); err != nil {
		t.Fatalf("the owner's undamaged node must still read: %v", err)
	}
	if _, err := reopened.GetNodeForTenant(strangerPeer.ID, "stranger"); err != nil {
		t.Fatalf("the stranger's own node must still read, or the stranger's "+
			"membership run is missing and every refusal below is for the wrong "+
			"reason: %v", err)
	}

	server, err := NewServerWithDataDir(reopened, 8080, dir)
	if err != nil {
		t.Fatalf("build server: %v", err)
	}

	return damagedFixture{
		server:       server,
		damagedNode:  damagedNode.ID,
		damagedEdge:  damagedEdge.ID,
		ownerPeer:    ownerPeer.ID,
		strangerPeer: strangerPeer.ID,
	}
}

// ownerSeesTheDamage records, per row, whether the OWNING tenant can still
// tell a damaged record from an ID that never existed. It is measured
// behaviour, not endorsed behaviour.
//
// Six rows answer 500 for the damaged record and 404 for the missing one. The
// two GET rows answer 404 for both, because getNode and getEdge map EVERY
// error to 404 (handlers_nodes.go, handlers_edges.go). That is not a leak —
// the stranger's two answers still match, which is what the row above asserts
// — but it does cost the owner the diagnostic that pkg/storage went to some
// trouble to produce. It is recorded here rather than hidden, and the
// assertion runs in both directions: a row that starts to differ fails too, so
// the change has to be deliberate.
var ownerSeesTheDamage = map[string]bool{
	"GET node":                   false,
	"PUT node":                   true,
	"DELETE node":                true,
	"GET edge":                   false,
	"PUT edge":                   true,
	"DELETE edge":                true,
	"POST edge, source position": true,
	"POST edge, target position": true,
}

// TestCrossPrincipalEquivalence_UnderFault repeats every row with the target
// record damaged on disk so it will not decode.
//
// This is the composition that matters. On 2026-08-29 a damaged record returned
// a different error class from a missing one, so a stranger saw 500 where a
// stranger asking for a nonexistent ID saw 404. A cross-tenant test on an intact
// store cannot see it, because the error paths do not diverge. A fault test with
// one principal cannot see it, because it never compares.
//
// Two assertions run for every row, and the second is what stops the fix from
// becoming a silent regression:
//
//   - the STRANGER's two answers must match, or the damaged record is an
//     existence oracle;
//   - the OWNER must still see what ownerSeesTheDamage records, or a handler
//     that answered 404 for everything would pass the first assertion while
//     destroying the diagnostic.
//
// Each row gets its OWN store. A shared one would let an earlier row's write
// change what a later row reads, and DELETE is a row.
// CONSUMER CONTRACT: CC12-existence-indistinguishable — all REST consumers (PR C)
func TestCrossPrincipalEquivalence_UnderFault(t *testing.T) {
	// An ID far beyond anything created, and beyond the snapshot's own ID
	// range, so both probes agree that it is absent.
	const neverExisted uint64 = 999999

	if len(ownerSeesTheDamage) != len(equivalenceRows) {
		t.Fatalf("ownerSeesTheDamage has %d entries for %d rows: a row was added "+
			"without recording what its owner sees", len(ownerSeesTheDamage), len(equivalenceRows))
	}

	for _, row := range equivalenceRows {
		t.Run(row.name, func(t *testing.T) {
			ownerDiffers, known := ownerSeesTheDamage[row.name]
			if !known {
				t.Fatalf("row %q has no entry in ownerSeesTheDamage", row.name)
			}

			f := newDamagedFixture(t)

			damaged := f.damagedNode
			if strings.Contains(row.pathFmt, "/edges/") {
				damaged = f.damagedEdge
			}

			// The STRANGER's view. Its fixed peer is its own node, so a POST
			// row reaches the check on the varying ID.
			row.fixedID = f.strangerPeer
			damagedPath, damagedBody := row.request(damaged)
			missingPath, missingBody := row.request(neverExisted)

			strangerDamaged := probe(t, f.server, "stranger", row.method, damagedPath, damagedBody)
			strangerMissing := probe(t, f.server, "stranger", row.method, missingPath, missingBody)

			if strangerDamaged != strangerMissing {
				t.Errorf("existence leak under fault: a DAMAGED record owned by another "+
					"tenant is distinguishable from one that never existed\n"+
					"  damaged, owned by \"owner\": %s\n"+
					"  never existed:             %s", strangerDamaged, strangerMissing)
			}

			// The OWNER's view. Its fixed peer must be its own node for the
			// same reason.
			row.fixedID = f.ownerPeer
			ownerDamagedPath, ownerDamagedBody := row.request(damaged)
			ownerMissingPath, ownerMissingBody := row.request(neverExisted)

			ownerDamaged := probe(t, f.server, "owner", row.method, ownerDamagedPath, ownerDamagedBody)
			ownerMissing := probe(t, f.server, "owner", row.method, ownerMissingPath, ownerMissingBody)

			switch {
			case ownerDiffers && ownerDamaged == ownerMissing:
				t.Errorf("the owner lost the diagnostic: a damaged record and an ID that "+
					"never existed both answer %s. Absence and damage must stay apart for "+
					"the tenant that owns the record", ownerDamaged)
			case !ownerDiffers && ownerDamaged != ownerMissing:
				t.Errorf("this row now tells the owner the two apart:\n"+
					"  damaged:       %s\n"+
					"  never existed: %s\n"+
					"If a handler was just taught to report an unreadable record, that is "+
					"an improvement: set %q to true in ownerSeesTheDamage",
					ownerDamaged, ownerMissing, row.name)
			}
		})
	}
}
