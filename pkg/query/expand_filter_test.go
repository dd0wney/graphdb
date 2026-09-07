package query

// A branch that cannot be pruned before it is walked.
//
// getEdges filters on rel.Direction and rel.Type only, so the traversal has no
// way to reject an intermediate node before it expands it. A consumer whose rule
// lives outside the query language — interrogate consults LDAP and POSIX state
// that no query string can express — can only filter the rows after they come
// back, by which time the engine has already read the whole subtree.
//
// PathOptions.Expand is the opt-in predicate that runs at expansion time. This
// file gates it, and the distinction it gates is exact:
//
//	filtering  the rejected node does not appear in the rows
//	pruning    the rejected node's NEIGHBOURS are never read
//
// B-1 asserts the second, which is the one worth having. Assertion (ii) there
// is the whole test.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// gateQuery walks up to four hops from the root, so every node of gateGraph is
// within reach and any absence is the filter's doing.
const gateQuery = "MATCH (a:Root)-[:LINK*1..4]->(b) RETURN b.name"

type gateFixture struct {
	gs      *storage.GraphStorage
	e       *Executor
	ids     map[string]uint64
	cleanup func()
}

// gateGraph builds a root with two branches:
//
//	root -> open                          (depth 1, never filtered)
//	root -> gate -> s1 -> s2 -> s3        (depths 1, 2, 3, 4)
//
// Rejecting "gate" must remove the whole s1..s3 subtree AND must stop the
// traversal ever reading s1's, s2's or s3's adjacency.
func gateGraph(t *testing.T) *gateFixture {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	ids := make(map[string]uint64, 6)
	mk := func(label, name string) uint64 {
		t.Helper()
		n, err := gs.CreateNode([]string{label}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
		if err != nil {
			cleanup()
			t.Fatalf("create %s: %v", name, err)
		}
		ids[name] = n.ID
		return n.ID
	}
	link := func(from, to uint64) {
		t.Helper()
		if _, err := gs.CreateEdge(from, to, "LINK", nil, 1); err != nil {
			cleanup()
			t.Fatalf("create edge: %v", err)
		}
	}

	root := mk("Root", "root")
	open := mk("Open", "open")
	gate := mk("Gate", "gate")
	s1 := mk("Hidden", "s1")
	s2 := mk("Hidden", "s2")
	s3 := mk("Hidden", "s3")

	link(root, open)
	link(root, gate)
	link(gate, s1)
	link(s1, s2)
	link(s2, s3)

	return &gateFixture{gs: gs, e: NewExecutor(gs), ids: ids, cleanup: cleanup}
}

// rowNames collects the "name" column of a result set into a set.
func rowNames(t *testing.T, rs *ResultSet) map[string]bool {
	t.Helper()
	names := make(map[string]bool, len(rs.Rows))
	for _, row := range rs.Rows {
		v, ok := row[rs.Columns[0]]
		if !ok {
			t.Fatalf("row has no column %q: %v", rs.Columns[0], row)
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("the name column came back as %T, not string: %v", v, v)
		}
		names[s] = true
	}
	return names
}

// B-1. Rejecting the gate must prune the subtree, not merely hide it.
func TestExpandFilterPrunesTheSubtree(t *testing.T) {
	f := gateGraph(t)
	defer f.cleanup()

	var offered []uint64
	opts := PathOptions{Expand: func(x Expansion) bool {
		offered = append(offered, x.To.ID)
		return x.To.ID != f.ids["gate"]
	}}

	rs, err := runQueryWithOptions(t, f.e, gateQuery, opts)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// (i) Nothing behind the gate is bound.
	names := rowNames(t, rs)
	for _, hidden := range []string{"gate", "s1", "s2", "s3"} {
		if names[hidden] {
			t.Errorf("%q came back although the gate was rejected", hidden)
		}
	}
	if !names["open"] {
		t.Errorf("the unfiltered sibling %q did not come back, so the filter over-reached "+
			"and this test cannot tell pruning from a broken traversal", "open")
	}

	// (ii) Nothing behind the gate was ever OFFERED to the filter. A rejected
	// node that still reaches the queue gets expanded, and its subtree gets
	// read from storage — that is filtering, not pruning, and it leaves the
	// cost the consumer complained about exactly where it was.
	seen := make(map[uint64]bool, len(offered))
	for _, id := range offered {
		seen[id] = true
	}
	for _, hidden := range []string{"s1", "s2", "s3"} {
		if seen[f.ids[hidden]] {
			t.Errorf("the filter was offered %q, which sits behind the rejected gate. The "+
				"subtree was expanded and read, so the branch was filtered rather than "+
				"pruned.", hidden)
		}
	}

	// Instrument check: if the filter was never offered the gate at all, every
	// assertion above passes for the wrong reason.
	if !seen[f.ids["gate"]] {
		t.Fatalf("the filter was never offered the gate, so this test proves nothing "+
			"about pruning. It saw %d candidates: %v", len(offered), offered)
	}
}

// B-2. The negative control. A nil filter must change nothing.
func TestNilExpandFilterIsUnchanged(t *testing.T) {
	f := gateGraph(t)
	defer f.cleanup()

	rs, err := runQueryWithOptions(t, f.e, gateQuery, PathOptions{})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	names := rowNames(t, rs)
	for _, want := range []string{"open", "gate", "s1", "s2", "s3"} {
		if !names[want] {
			t.Errorf("%q is missing with no filter set; the zero PathOptions is not "+
				"today's behaviour", want)
		}
	}
	if len(rs.Rows) != 5 {
		t.Errorf("got %d rows with no filter, want 5 reachable nodes", len(rs.Rows))
	}
}

// B-3. The two capabilities are orthogonal: the filter prunes under both
// semantics, and neither depends on the other.
func TestExpandFilterAppliesInBothSemantics(t *testing.T) {
	for _, tc := range []struct {
		name string
		sem  PathSemantics
	}{
		{"AllSimplePaths", AllSimplePaths},
		{"DistinctNodes", DistinctNodes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := gateGraph(t)
			defer f.cleanup()

			opts := PathOptions{
				Semantics: tc.sem,
				Expand:    func(x Expansion) bool { return x.To.ID != f.ids["gate"] },
			}
			rs, err := runQueryWithOptions(t, f.e, gateQuery, opts)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}

			names := rowNames(t, rs)
			if names["s3"] {
				t.Errorf("the subtree behind the rejected gate came back, so the filter "+
					"does not apply under %s", tc.name)
			}
			if !names["open"] {
				t.Errorf("the unfiltered sibling is missing under %s", tc.name)
			}
		})
	}
}

// B-4. Expansion must carry real values in all four fields, and a rule that
// reads Depth must prune. A struct whose fields are zero would let a filter
// compile, run, and decide on nothing.
func TestExpandFilterReceivesEdgeAndDepth(t *testing.T) {
	f := gateGraph(t)
	defer f.cleanup()

	// Part 1: the fields carry the real edge, the real endpoints and the depth
	// the candidate would occupy.
	depths := map[uint64]int{
		f.ids["open"]: 1,
		f.ids["gate"]: 1,
		f.ids["s1"]:   2,
		f.ids["s2"]:   3,
		f.ids["s3"]:   4,
	}
	inspect := PathOptions{Expand: func(x Expansion) bool {
		switch {
		case x.Edge == nil:
			t.Errorf("Expansion.Edge is nil")
		case x.Edge.Type != "LINK":
			t.Errorf("Expansion.Edge.Type is %q, want %q", x.Edge.Type, "LINK")
		}
		if x.From == nil || x.To == nil {
			t.Fatalf("Expansion endpoints are nil: from=%v to=%v", x.From, x.To)
		}
		if want, ok := depths[x.To.ID]; ok && x.Depth != want {
			t.Errorf("Expansion.Depth for node %d is %d, want %d", x.To.ID, x.Depth, want)
		}
		return true
	}}
	if _, err := runQueryWithOptions(t, f.e, gateQuery, inspect); err != nil {
		t.Fatalf("inspection query failed: %v", err)
	}

	// Part 2: a depth rule prunes. Nodes at depth 3 and 4 must not come back,
	// and the traversal must stop rather than walk past them.
	shallow := PathOptions{Expand: func(x Expansion) bool { return x.Depth <= 2 }}
	rs, err := runQueryWithOptions(t, f.e, gateQuery, shallow)
	if err != nil {
		t.Fatalf("depth-limited query failed: %v", err)
	}
	names := rowNames(t, rs)
	for _, want := range []string{"open", "gate", "s1"} {
		if !names[want] {
			t.Errorf("%q sits within two hops but is missing under a Depth <= 2 filter", want)
		}
	}
	for _, unwanted := range []string{"s2", "s3"} {
		if names[unwanted] {
			t.Errorf("%q sits deeper than two hops and came back, so Expansion.Depth did "+
				"not decide anything", unwanted)
		}
	}
}

// B-5. A filter is caller code running inside the engine. A panic in it must
// become a query error, not a crashed process.
func TestPanickingExpandFilterBecomesAnError(t *testing.T) {
	f := gateGraph(t)
	defer f.cleanup()

	opts := PathOptions{Expand: func(Expansion) bool {
		panic("the filter blew up")
	}}

	rs, err := runQueryWithOptions(t, f.e, gateQuery, opts)
	if err == nil {
		t.Fatalf("a panicking filter produced no error; the panic escaped the query "+
			"boundary or was swallowed. rows=%v", rs)
	}
	if rs != nil {
		t.Errorf("a recovered panic returned a non-nil result set, which a caller could "+
			"mistake for a partial answer: %v", rs)
	}
}

// B-6. The filter must never be offered a node belonging to another tenant.
//
// The construction: a default-tenant edge whose target node belongs to
// "tenant-b". CreateEdge verifies endpoints tenant-blind, so it accepts this;
// CreateEdgeWithTenant would refuse it. The traversal reads through
// GetNodeForTenant, which returns ErrNodeNotFound for a foreign node, so the
// candidate is dropped before the filter sees it. Substituting the tenant-blind
// GetNode in match_path.go makes this test fail.
func TestExpandFilterNeverSeesAForeignTenantNode(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	root, err := gs.CreateNode([]string{"Root"}, map[string]storage.Value{
		"name": storage.StringValue("root"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	mine, err := gs.CreateNode([]string{"Mine"}, map[string]storage.Value{
		"name": storage.StringValue("mine"),
	})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	foreign, err := gs.CreateNodeWithTenant("tenant-b", []string{"Foreign"}, map[string]storage.Value{
		"name": storage.StringValue("foreign"),
	})
	if err != nil {
		t.Fatalf("create foreign: %v", err)
	}

	if _, err := gs.CreateEdge(root.ID, mine.ID, "LINK", nil, 1); err != nil {
		t.Fatalf("create root->mine: %v", err)
	}
	// The crossing edge. It belongs to the default tenant, and its target does
	// not.
	if _, err := gs.CreateEdge(mine.ID, foreign.ID, "LINK", nil, 1); err != nil {
		t.Fatalf("create mine->foreign: %v", err)
	}

	var offered []*storage.Node
	opts := PathOptions{Expand: func(x Expansion) bool {
		offered = append(offered, x.To)
		return true
	}}

	tokens, err := NewLexer(gateQuery).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	parsed, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx, cancel := context.WithTimeout(tenant.WithTenant(context.Background(), tenant.DefaultTenantID), 30*time.Second)
	defer cancel()
	if _, err := NewExecutor(gs).ExecuteWithOptions(ctx, parsed, opts); err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// Instrument check: the filter must have run at all.
	if len(offered) == 0 {
		t.Fatalf("the filter was never called, so this test proves nothing about tenants")
	}
	for _, n := range offered {
		if n.ID == foreign.ID {
			t.Errorf("the filter was offered node %d, which belongs to tenant %q; the "+
				"expansion path bypassed GetNodeForTenant", n.ID, n.TenantID)
		}
		if !tenant.IsDefaultTenant(n.TenantID) {
			t.Errorf("the filter was offered a node of tenant %q while the query ran as "+
				"the default tenant", n.TenantID)
		}
	}
}

// The depth-cap truncation check must ask the filter, not only the visited set.
//
// At the cap the traversal asks whether any neighbour would have been expanded.
// It consulted the visited set alone, so a candidate the CALLER's own filter
// would prune still raised ErrTraversalTruncated. Nothing was lost, and the
// engine said the answer was incomplete anyway. pkg/api turns that error into
// the incomplete signal, so a consumer that pairs Expand with an unbounded star
// would see "incomplete" on nearly every query, and the signal stops meaning
// anything — the exact outcome the comment above the depth check argues against.
func TestExpandFilterDoesNotCauseAFalseTruncation(t *testing.T) {
	_, e, cleanup := chainGraph(t, MaxAllowedTraversalDepth+2)
	defer cleanup()

	// The caller's own rule stops exactly at the engine cap, so the engine cap
	// takes nothing away from it.
	opts := PathOptions{Expand: func(x Expansion) bool {
		return x.Depth <= MaxAllowedTraversalDepth
	}}

	rs, err := runQueryWithOptions(t, e, "MATCH (a:Root)-[:LINK*]->(b:Node) RETURN b.name", opts)
	if rs == nil || len(rs.Rows) == 0 {
		t.Fatalf("no rows came back, so this test cannot tell a false truncation from a "+
			"broken query: rs=%v err=%v", rs, err)
	}
	if err != nil {
		t.Errorf("the traversal reported an incomplete answer, but the only candidate past "+
			"the cap was one the caller's filter rejects. Nothing was lost: %v", err)
	}

	// Control: with no filter the same query IS incomplete, so the signal still
	// works and this test is not merely asserting that truncation never fires.
	_, bare := runQueryWithOptions(t, e, "MATCH (a:Root)-[:LINK*]->(b:Node) RETURN b.name",
		PathOptions{})
	if !errors.Is(bare, ErrTraversalTruncated) {
		t.Fatalf("the unfiltered control did not report truncation, so the instrument is "+
			"broken and the assertion above proves nothing: %v", bare)
	}
}

// The filter reaches variable-length patterns only. D10 decided this: a single
// hop has no subtree to prune, so a filter there would only repeat a WHERE
// clause. `*1..1` carries star syntax but is a single hop, and it takes the
// fixed path.
//
// This pins the limit that ExpandFilter's doc comment states. It is a fact about
// the engine, not an accident, and a later change to it must be deliberate.
func TestExpandFilterDoesNotReachSingleHopPatterns(t *testing.T) {
	f := gateGraph(t)
	defer f.cleanup()

	for _, q := range []string{
		"MATCH (a:Root)-[:LINK]->(b) RETURN b.name",
		"MATCH (a:Root)-[:LINK*1..1]->(b) RETURN b.name",
	} {
		called := 0
		opts := PathOptions{Expand: func(Expansion) bool {
			called++
			return false
		}}
		rs, err := runQueryWithOptions(t, f.e, q, opts)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if called != 0 {
			t.Errorf("%s offered %d candidates to the filter; a single hop takes the fixed "+
				"path, which has no filter", q, called)
		}
		if len(rs.Rows) != 2 {
			t.Errorf("%s returned %d rows, want 2; the reject-all filter must not have "+
				"applied here", q, len(rs.Rows))
		}
	}
}
