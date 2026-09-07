package query

// Task 13 (docs/internals/design, plan-model-tiers): benchmark the admissible
// load at the depth-cap frontier.
//
// #563 added PathOptions to the variable-length pattern path. admissible
// (match_path.go:240) is the one place that decides whether the traversal may
// enter a candidate node, and the depth-cap truncation check (match_path.go
// :384-391) calls it once per edge on every frontier node reached by an
// unbounded pattern, regardless of whether a PathOptions.Expand filter is set.
// That call always runs GetNodeForTenant. This file measures the cost of that
// load at the cap frontier, and whether adding an Expand filter that accepts
// every candidate changes it. It changes no production code.
//
// Graph shape:
//
//	root(:Start) --T--> n1 --T--> n2 --T--> ... --T--> n99   (99 edges, one path, depths 0..99)
//	n99 --T--> f_0 .. f_999                                   (1000 edges, depth 100 -- the cap frontier)
//	f_i --T--> g_i for each i in [0, 1000)                    (1000 edges, depth 101 -- past the cap)
//
// Node count: 100 (root..n99) + 1000 (f_i) + 1000 (g_i) = 2100.
// Edge count: 99 + 1000 + 1000 = 2099.
// Depth: 101 (the deepest node in the graph).
//
// MaxAllowedTraversalDepth (traversal_types.go:15) is 100. The query
// "MATCH (a:Start)-[:T*]->(b) RETURN b.name" is unbounded (rel.MaxHops == -1,
// match_path.go:300), so the traversal admits nodes through depth 100 and then
// hits match_path.go:374's "entry.depth >= maxHops" branch at the 1000 f_i
// nodes. Each f_i has exactly one outgoing edge (to its g_i), so each one's
// truncation-check loop (match_path.go:384-391) calls admissible exactly once
// before finding an admissible candidate and breaking -- 1000 extra
// GetNodeForTenant loads per query, one per edge at the cap frontier, on top of
// the loads the ordinary expansion below the cap already does.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

const (
	// admissibleFrontierChainDepth is one less than MaxAllowedTraversalDepth,
	// so the fan-out at its far end lands exactly on the cap.
	admissibleFrontierChainDepth = MaxAllowedTraversalDepth - 1

	// admissibleFrontierFanWidth is the number of nodes admitted at the cap
	// (depth 100), each carrying one edge past the cap for admissible to load
	// during the truncation check.
	admissibleFrontierFanWidth = 1000

	// admissibleFrontierWantRows is the row count both variants must return:
	// one row per admitted node at depth 1..100, which is the 99 chain nodes
	// plus the fanWidth frontier nodes. The g_i nodes past the cap are loaded
	// by admissible but never admitted, so they contribute no rows.
	admissibleFrontierWantRows = admissibleFrontierChainDepth + admissibleFrontierFanWidth

	admissibleFrontierQuery = "MATCH (a:Start)-[:T*]->(b) RETURN b.name"
)

// benchAdmissibleFrontierSink stops the compiler eliding ExecuteWithOptions's
// result. Same pattern as pkg/storage/bench_concurrent_read_test.go's
// benchSink: an unconsumed result in the same package can be proven dead by
// escape analysis, and eliding one variant's work but not the other's would
// compare two different things.
var benchAdmissibleFrontierSink atomic.Int64

// buildAdmissibleFrontierGraph builds the graph described in this file's
// doc comment and returns it with a cleanup function.
func buildAdmissibleFrontierGraph(b *testing.B) (*storage.GraphStorage, func()) {
	b.Helper()

	tmpDir, err := os.MkdirTemp("", "admissible-frontier-bench-*")
	if err != nil {
		b.Fatalf("create temp dir: %v", err)
	}
	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		b.Fatalf("create graph storage: %v", err)
	}
	cleanup := func() {
		_ = gs.Close()
		_ = os.RemoveAll(tmpDir)
	}

	mk := func(label, name string) *storage.Node {
		n, cerr := gs.CreateNode([]string{label}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
		if cerr != nil {
			cleanup()
			b.Fatalf("create node %s: %v", name, cerr)
		}
		return n
	}
	link := func(from, to uint64) {
		if _, cerr := gs.CreateEdge(from, to, "T", nil, 1); cerr != nil {
			cleanup()
			b.Fatalf("create edge %d->%d: %v", from, to, cerr)
		}
	}

	root := mk("Start", "n0")
	prev := root
	for i := 1; i <= admissibleFrontierChainDepth; i++ {
		n := mk("Node", fmt.Sprintf("n%d", i))
		link(prev.ID, n.ID)
		prev = n
	}
	for i := 0; i < admissibleFrontierFanWidth; i++ {
		f := mk("Frontier", fmt.Sprintf("f%d", i))
		link(prev.ID, f.ID)
		g := mk("PastCap", fmt.Sprintf("g%d", i))
		link(f.ID, g.ID)
	}

	return gs, cleanup
}

// parseAdmissibleFrontierQuery parses admissibleFrontierQuery once so both
// sub-benchmarks and the pre-flight checks run the exact same *Query value.
func parseAdmissibleFrontierQuery(b *testing.B) *Query {
	b.Helper()
	tokens, err := NewLexer(admissibleFrontierQuery).Tokenize()
	if err != nil {
		b.Fatalf("lex %q: %v", admissibleFrontierQuery, err)
	}
	parsed, err := NewParser(tokens).Parse()
	if err != nil {
		b.Fatalf("parse %q: %v", admissibleFrontierQuery, err)
	}
	return parsed
}

// runAdmissibleFrontierOnce runs the query once, outside any timed loop, and
// checks three things a benchmark must not assume: the traversal actually
// reached the engine's depth cap (ErrTraversalTruncated), the result set is
// present, and its row count is the one the graph shape predicts. It returns
// the row count so the caller can compare it against the other variant.
func runAdmissibleFrontierOnce(b *testing.B, e *Executor, ctx context.Context, q *Query, opts PathOptions) int {
	b.Helper()
	rs, err := e.ExecuteWithOptions(ctx, q, opts)
	if err == nil {
		b.Fatalf("query reported no truncation; the traversal did not reach the depth cap, " +
			"so this benchmark would not measure the cap-frontier load at all")
	}
	if !errors.Is(err, ErrTraversalTruncated) {
		b.Fatalf("query failed: %v", err)
	}
	if rs == nil {
		b.Fatalf("a truncated query returned a nil result set")
	}
	if len(rs.Rows) != admissibleFrontierWantRows {
		b.Fatalf("got %d rows, want %d (%d chain nodes + %d frontier nodes)",
			len(rs.Rows), admissibleFrontierWantRows, admissibleFrontierChainDepth, admissibleFrontierFanWidth)
	}
	return len(rs.Rows)
}

// BenchmarkAdmissibleFrontier measures ExecuteWithOptions on the graph
// described in this file's doc comment, over the same unbounded query, with
// and without a PathOptions.Expand filter that accepts every candidate.
//
//	NoFilter       PathOptions{} -- zero value, no Expand filter.
//	PassAllFilter  PathOptions{Expand: func(Expansion) bool { return true }}.
//
// Both variants must reach the depth cap and return the same number of rows;
// runAdmissibleFrontierOnce checks this once, before b.ResetTimer, not inside
// either timed loop.
func BenchmarkAdmissibleFrontier(b *testing.B) {
	gs, cleanup := buildAdmissibleFrontierGraph(b)
	defer cleanup()

	e := NewExecutor(gs)
	q := parseAdmissibleFrontierQuery(b)
	ctx := context.Background()

	passAll := PathOptions{Expand: func(Expansion) bool { return true }}

	noFilterRows := runAdmissibleFrontierOnce(b, e, ctx, q, PathOptions{})
	passAllRows := runAdmissibleFrontierOnce(b, e, ctx, q, passAll)
	if noFilterRows != passAllRows {
		b.Fatalf("NoFilter returned %d rows, PassAllFilter returned %d; a filter that accepts "+
			"everything must not change the row count", noFilterRows, passAllRows)
	}

	b.Run("NoFilter", func(b *testing.B) {
		opts := PathOptions{}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rs, err := e.ExecuteWithOptions(ctx, q, opts)
			if err != nil && !errors.Is(err, ErrTraversalTruncated) {
				b.Fatalf("query failed: %v", err)
			}
			benchAdmissibleFrontierSink.Store(int64(len(rs.Rows)))
		}
	})

	b.Run("PassAllFilter", func(b *testing.B) {
		opts := passAll
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rs, err := e.ExecuteWithOptions(ctx, q, opts)
			if err != nil && !errors.Is(err, ErrTraversalTruncated) {
				b.Fatalf("query failed: %v", err)
			}
			benchAdmissibleFrontierSink.Store(int64(len(rs.Rows)))
		}
	})
}
