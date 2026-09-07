package query

// A traversal that costs the caller for routes it did not ask about.
//
// traverseVariablePath clones a per-path visited set for every branch, so it
// returns every distinct simple path. A node reachable by twenty routes costs
// twenty bindings, twenty adjacency reads and twenty visited-set clones, and the
// caller who wanted "which nodes are reachable" pays all of it and then
// deduplicates the answer itself.
//
// PathOptions{Semantics: DistinctNodes} is the opt-in mode that admits each node
// once. This file gates it, and it gates the two things that make an opt-in
// worth having:
//
//	the mode works        A-1, A-6, A-7
//	the default is intact A-2, A-3
//	the cost really moved A-4
//	the sharp edge refuses A-5
//
// A-2 and A-3 are the load-bearing half. "Every distinct simple path" is the
// behaviour today, other consumers may depend on it, and a mode that changed it
// by default would be a silent change of meaning.
//
// Asked for by the interrogate session (oit-cyber/interrogate), which joins a
// POSIX directory tree with LDAP group data: a directory is reachable through
// dozens of group memberships, and it wants one row per employee.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

const (
	// fanRoutes is the number of distinct 2-hop routes to the single sink.
	fanRoutes = 20

	// fanSinkQuery reaches the sink of fanGraph. Under AllSimplePaths it
	// returns one row per route; under DistinctNodes it returns one row.
	fanSinkQuery = "MATCH (a:Root)-[:LINK*1..2]->(b:Sink) RETURN b.name"
)

// fanGraph builds one :Root, `routes` distinct :Mid nodes and one :Sink, with
// Root -[:LINK]-> Mid_i -[:LINK]-> Sink for every i. The sink is reachable by
// exactly `routes` distinct simple paths, every one of length 2.
func fanGraph(t *testing.T, routes int) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	root, err := gs.CreateNode([]string{"Root"}, map[string]storage.Value{
		"name": storage.StringValue("root"),
	})
	if err != nil {
		cleanup()
		t.Fatalf("create root: %v", err)
	}
	sink, err := gs.CreateNode([]string{"Sink"}, map[string]storage.Value{
		"name": storage.StringValue("sink"),
	})
	if err != nil {
		cleanup()
		t.Fatalf("create sink: %v", err)
	}
	for i := range routes {
		mid, err := gs.CreateNode([]string{"Mid"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("mid%d", i)),
		})
		if err != nil {
			cleanup()
			t.Fatalf("create mid%d: %v", i, err)
		}
		if _, err := gs.CreateEdge(root.ID, mid.ID, "LINK", nil, 1); err != nil {
			cleanup()
			t.Fatalf("create root->mid%d: %v", i, err)
		}
		if _, err := gs.CreateEdge(mid.ID, sink.ID, "LINK", nil, 1); err != nil {
			cleanup()
			t.Fatalf("create mid%d->sink: %v", i, err)
		}
	}
	return gs, NewExecutor(gs), cleanup
}

// runQueryWithOptions is runQuery with a PathOptions value. ExecuteWithOptions
// is the only entry point that carries the options, so this cannot go through
// Execute.
func runQueryWithOptions(t *testing.T, e *Executor, q string, opts PathOptions) (*ResultSet, error) {
	t.Helper()
	tokens, err := NewLexer(q).Tokenize()
	if err != nil {
		t.Fatalf("lex %q: %v", q, err)
	}
	parsed, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", q, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.ExecuteWithOptions(ctx, parsed, opts)
}

// A-1. The whole point of the mode.
func TestDistinctNodesModeReturnsOneRowPerNode(t *testing.T) {
	_, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	rs, err := runQueryWithOptions(t, e, fanSinkQuery, PathOptions{Semantics: DistinctNodes})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Errorf("the sink is reachable by %d routes, and DistinctNodes admits each node "+
			"once, so the answer must be 1 row; got %d. A row per route means the mode "+
			"did not take effect and the caller still pays for every route.",
			fanRoutes, len(rs.Rows))
	}
}

// A-2. The negative control, and the reason the zero value matters.
//
// This must pass before the mode exists and after it exists. If it ever starts
// passing only after, the default changed and the opt-in constraint is broken.
func TestAllSimplePathsModeIsUnchanged(t *testing.T) {
	_, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	// Both spellings of "did not opt in" must agree: the plain entry point,
	// which knows nothing about PathOptions, and the explicit zero value.
	plain, err := runQuery(t, e, fanSinkQuery)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	zero, err := runQueryWithOptions(t, e, fanSinkQuery, PathOptions{})
	if err != nil {
		t.Fatalf("ExecuteWithOptions with the zero value: %v", err)
	}

	if len(plain.Rows) != fanRoutes {
		t.Errorf("Execute returned %d rows for %d routes; the default enumerates every "+
			"distinct simple path and other consumers depend on that", len(plain.Rows), fanRoutes)
	}
	if len(zero.Rows) != fanRoutes {
		t.Errorf("PathOptions{} returned %d rows for %d routes, so the zero value is not "+
			"today's behaviour and no caller is safe by construction",
			len(zero.Rows), fanRoutes)
	}
}

// A-3. The mode must not leak into traverseFixedPath.
func TestSingleHopPatternUnaffectedByPathOptions(t *testing.T) {
	const singleHop = "MATCH (a:Root)-[:LINK]->(b:Mid) RETURN b.name"

	_, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	for _, tc := range []struct {
		name string
		opts PathOptions
	}{
		{"AllSimplePaths", PathOptions{}},
		{"DistinctNodes", PathOptions{Semantics: DistinctNodes}},
	} {
		rs, err := runQueryWithOptions(t, e, singleHop, tc.opts)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		// The mids are distinct nodes, so both modes owe all of them. A
		// single hop has no route explosion to collapse.
		if len(rs.Rows) != fanRoutes {
			t.Errorf("%s: a star-free single-hop pattern returned %d rows, want %d; "+
				"the semantics reached traverseFixedPath, which it must not",
				tc.name, len(rs.Rows), fanRoutes)
		}
	}
}

// A-4. The mode must move the COST, not only the row count. Deduplicating the
// rows after the fact would satisfy A-1 and change nothing the consumer cares
// about, because every route would still be walked, read and allocated.
//
// The instrument is storage.Statistics.TotalQueries, which counts adjacency
// reads process-wide. The test takes a tight delta around one query and must
// not run in parallel with another test on the same store.
func TestDistinctNodesCostGrowsWithNodesNotRoutes(t *testing.T) {
	gs, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	measure := func(opts PathOptions) uint64 {
		t.Helper()
		before := gs.GetStatistics().TotalQueries
		if _, err := runQueryWithOptions(t, e, fanSinkQuery, opts); err != nil {
			t.Fatalf("query failed: %v", err)
		}
		return gs.GetStatistics().TotalQueries - before
	}

	allSimple := measure(PathOptions{})
	distinct := measure(PathOptions{Semantics: DistinctNodes})

	// Instrument check first: a counter that never moves would make every
	// comparison below trivially true.
	if allSimple == 0 {
		t.Fatalf("TotalQueries did not move across a query that reads adjacency, so this "+
			"test measures nothing; distinct delta was %d", distinct)
	}

	if distinct >= allSimple {
		t.Errorf("DistinctNodes did %d adjacency reads and AllSimplePaths did %d. The "+
			"mode returned fewer rows without doing less work, which is what "+
			"deduplicating after the traversal would also achieve.", distinct, allSimple)
	}

	// Cost must be bounded by the node count. fanGraph holds routes+2 nodes,
	// and a node-visited traversal expands each admitted node at most once.
	// The slack of 2 covers resolving the start node.
	nodes := uint64(fanRoutes + 2)
	if distinct > nodes+2 {
		t.Errorf("DistinctNodes did %d adjacency reads over a %d-node graph reachable by "+
			"%d routes; cost still looks route-shaped, not node-shaped",
			distinct, nodes, fanRoutes)
	}
}

// A-5. The sharp edge. With one shared visited set a node is admitted at its
// minimum depth and never returns, so a [2, n] window silently drops rows that
// AllSimplePaths returns. Refuse rather than answer wrongly.
func TestDistinctNodesRefusesMinHopsAboveOne(t *testing.T) {
	const minTwo = "MATCH (a:Root)-[:LINK*2..3]->(b:Sink) RETURN b.name"
	const minOne = "MATCH (a:Root)-[:LINK*1..3]->(b:Sink) RETURN b.name"

	_, e, cleanup := fanGraph(t, 3)
	defer cleanup()

	_, err := runQueryWithOptions(t, e, minTwo, PathOptions{Semantics: DistinctNodes})
	if err == nil {
		t.Fatalf("*2..3 in DistinctNodes was accepted. The mode cannot answer that window " +
			"without dropping rows, so accepting it degrades an answer in silence.")
	}
	if !errors.Is(err, ErrDistinctNodesMinHops) {
		t.Errorf("the refusal does not wrap ErrDistinctNodesMinHops, so a caller cannot "+
			"tell it from any other failure: %v", err)
	}

	// Control: MinHops of 1 is accepted, so the refusal is about the
	// combination and not about the mode.
	if _, err := runQueryWithOptions(t, e, minOne, PathOptions{Semantics: DistinctNodes}); err != nil {
		t.Errorf("*1..3 in DistinctNodes was refused, so the refusal is too broad: %v", err)
	}
	// Control: the same window is accepted under the default semantics.
	if _, err := runQueryWithOptions(t, e, minTwo, PathOptions{}); err != nil {
		t.Errorf("*2..3 under AllSimplePaths was refused, so the refusal leaked into the "+
			"default: %v", err)
	}
}

// A-6. The truncation contract survives the mode. The depth-cap check must
// consult the SHARED visited set: a neighbour already visited globally is
// already in the answer, so it is not lost work and must not raise the signal.
func TestDistinctNodesStillReportsTruncation(t *testing.T) {
	_, e, cleanup := chainGraph(t, MaxAllowedTraversalDepth+2)
	defer cleanup()

	rs, err := runQueryWithOptions(t, e, "MATCH (a:Root)-[:LINK*]->(b:Node) RETURN b.name",
		PathOptions{Semantics: DistinctNodes})

	if rs == nil || len(rs.Rows) == 0 {
		t.Fatalf("no rows came back, so this test cannot tell a truncation signal from a "+
			"broken query: rs=%v err=%v", rs, err)
	}
	if err == nil {
		t.Errorf("the traversal stopped at the cap of %d on a chain of %d and reported "+
			"success; DistinctNodes lost the honesty the default path has",
			MaxAllowedTraversalDepth, MaxAllowedTraversalDepth+2)
	}
	if err != nil && !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("the error does not wrap ErrTraversalTruncated: %v", err)
	}
}

// shortcutGraph builds a :Root with two routes to one :Sink — a direct edge and
// a three-hop detour. The detour edges are created FIRST, so the root's
// adjacency offers the detour before the shortcut and a depth-first
// implementation would bind the long path.
func shortcutGraph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	mk := func(label, name string) *storage.Node {
		t.Helper()
		n, err := gs.CreateNode([]string{label}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
		if err != nil {
			cleanup()
			t.Fatalf("create %s: %v", name, err)
		}
		return n
	}
	link := func(from, to *storage.Node) {
		t.Helper()
		if _, err := gs.CreateEdge(from.ID, to.ID, "LINK", nil, 1); err != nil {
			cleanup()
			t.Fatalf("create edge: %v", err)
		}
	}

	root := mk("Root", "root")
	x := mk("Detour", "x")
	y := mk("Detour", "y")
	sink := mk("Sink", "sink")

	link(root, x) // the detour is offered first
	link(x, y)
	link(y, sink)
	link(root, sink) // the shortcut

	return gs, NewExecutor(gs), cleanup
}

// A-7. A node admitted once must be admitted at its MINIMUM depth, so the
// relationship variable binds a shortest path in edge count.
func TestDistinctNodesBindsAShortestPath(t *testing.T) {
	_, e, cleanup := shortcutGraph(t)
	defer cleanup()

	rs, err := runQueryWithOptions(t, e, "MATCH (a:Root)-[r:LINK*1..3]->(b:Sink) RETURN r",
		PathOptions{Semantics: DistinctNodes})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("the sink is one node reachable two ways, so DistinctNodes owes 1 row; "+
			"got %d", len(rs.Rows))
	}

	raw := rs.Rows[0][rs.Columns[0]]
	path, ok := raw.([]*storage.Edge)
	if !ok {
		t.Fatalf("the relationship variable came back as %T, so this test cannot read the "+
			"bound path: %v", raw, raw)
	}
	if len(path) != 1 {
		t.Errorf("the bound path has %d edges. A breadth-first traversal admits a node at "+
			"its minimum depth, so the 1-hop shortcut must win over the 3-hop detour.",
			len(path))
	}
}

// The MinHops refusal must not depend on the data.
//
// The check lived inside traverseVariablePath, which matchPath calls once per
// start node. A label that matches nothing meant the traversal never ran, so an
// unanswerable query returned an empty success instead of the refusal. A caller
// cannot use a refusal that only fires when the graph happens to cooperate.
func TestDistinctNodesRefusesMinHopsBeforeItReadsAnyData(t *testing.T) {
	const noSuchStart = "MATCH (a:Missing)-[:LINK*2..3]->(b:Sink) RETURN b.name"

	_, e, cleanup := fanGraph(t, 3)
	defer cleanup()

	rs, err := runQueryWithOptions(t, e, noSuchStart, PathOptions{Semantics: DistinctNodes})
	if err == nil {
		t.Fatalf("a start label that matches nothing turned the refusal into an empty "+
			"success: %d rows, nil error. The same pattern over :Root is refused, so the "+
			"answer depends on the data rather than on the request.", len(rs.Rows))
	}
	if !errors.Is(err, ErrDistinctNodesMinHops) {
		t.Errorf("the error does not wrap ErrDistinctNodesMinHops: %v", err)
	}
}

// A PathSemantics the engine does not know must be refused, not quietly read as
// the default. The project prefers a loud refusal to silent degradation.
func TestUnknownPathSemanticsIsRefused(t *testing.T) {
	_, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	rs, err := runQueryWithOptions(t, e, fanSinkQuery, PathOptions{Semantics: PathSemantics(99)})
	if err == nil {
		t.Fatalf("an unknown PathSemantics ran as AllSimplePaths and returned %d rows with "+
			"a nil error. A caller who passed a wrong value is told nothing.", len(rs.Rows))
	}
	if !errors.Is(err, ErrUnknownPathSemantics) {
		t.Errorf("the error does not wrap ErrUnknownPathSemantics: %v", err)
	}
}
