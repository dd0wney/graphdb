package query

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// pageRankNeverConverges builds a hub with `leaves` leaf nodes, each joined
// to the hub both ways. With damping 1 the scores swap between two states on
// every iteration and never settle, so CALL algo.pageRank(1.0, N) runs all N
// iterations. That makes its run time a function of N alone.
func pageRankNeverConverges(t *testing.T, gs *storage.GraphStorage, leaves int) {
	t.Helper()
	hub, err := gs.CreateNode([]string{"Hub"}, nil)
	if err != nil {
		t.Fatalf("CreateNode hub: %v", err)
	}
	for i := 0; i < leaves; i++ {
		leaf, err := gs.CreateNode([]string{"Leaf"}, nil)
		if err != nil {
			t.Fatalf("CreateNode leaf: %v", err)
		}
		if _, err := gs.CreateEdge(hub.ID, leaf.ID, "L", nil, 1.0); err != nil {
			t.Fatalf("CreateEdge out: %v", err)
		}
		if _, err := gs.CreateEdge(leaf.ID, hub.ID, "L", nil, 1.0); err != nil {
			t.Fatalf("CreateEdge back: %v", err)
		}
	}
}

// pageRankCallIterations is chosen so the full run takes about a minute on a
// development machine (62 microseconds an iteration on 201 nodes, measured
// 2026-11-13). The deadline is 50 ms and the bound below is 10 s, so a machine
// would have to be 200 times slower than that to fail a fixed build.
const pageRankCallIterations = 1_000_000

// pageRankStopBound is far above the deadline and far below the full run.
const pageRankStopBound = 10 * time.Second

// TestCallPageRank_StopsAtQueryDeadline pins v1.4-procedures-ignore-context.
// A cancelled context is refused by the executor before the CALL step starts,
// so that cannot show the procedure honours it. A deadline that passes while
// the procedure runs can. The error alone proves nothing: the executor also
// checks the context after each step, so before the fix the CALL ran to its
// last iteration and THEN returned DeadlineExceeded. The elapsed time is the
// assertion that tells the two builds apart.
func TestCallPageRank_StopsAtQueryDeadline(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	pageRankNeverConverges(t, gs, 200)
	ex := NewExecutor(gs)

	tokens, err := NewLexer(fmt.Sprintf("CALL algo.pageRank(1.0, %d)", pageRankCallIterations)).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	rs, err := ex.ExecuteWithContext(ctx, q)
	elapsed := time.Since(start)
	t.Logf("CALL returned after %v", elapsed)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want one wrapping context.DeadlineExceeded", err)
	}
	if rs != nil {
		t.Errorf("a stopped CALL returned a result set with %d rows", len(rs.Rows))
	}
	if elapsed > pageRankStopBound {
		t.Errorf("CALL took %v to stop at a 50ms deadline; the procedure ignores its context", elapsed)
	}
}

// TestProcedures_RefuseCancelledContext calls each registered procedure body
// with a context that is already cancelled. The executor guards the front of a
// query, so this is the place that shows the procedure itself passes its
// context on. Before the fix the six procedures discarded it.
func TestProcedures_RefuseCancelledContext(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tid = "tenant-ctx"
	const n = 30
	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		node, err := gs.CreateNodeWithTenant(tid, []string{"N"}, nil)
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		ids = append(ids, node.ID)
	}
	for i := range ids {
		for _, step := range []int{1, 2, 5} {
			if _, err := gs.CreateEdgeWithTenant(tid, ids[i], ids[(i+step)%n], "E", nil, 1.0); err != nil {
				t.Fatalf("create edge: %v", err)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		args []any
	}{
		{"algo.pageRank", nil},
		{"algo.kHop", []any{ids[0], 3}},
		{"algo.shortestPath", []any{ids[0], ids[n/2]}},
		{"algo.nodeSimilarity", []any{ids[0], ids[1]}},
		{"algo.linkPrediction", []any{ids[0], ids[1]}},
		{"algo.detectCycles", nil},
		{"algo.connectedComponents", nil},
		{"algo.scc", nil},
		{"algo.labelPropagation", nil},
		{"algo.betweennessCentrality", nil},
		{"algo.eMeans", []any{2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proc, ok := procedureRegistry[c.name]
			if !ok {
				t.Fatalf("%s is not registered", c.name)
			}
			rows, err := proc(ctx, gs, tid, c.args)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want one wrapping context.Canceled", err)
			}
			if rows != nil {
				t.Errorf("a cancelled procedure returned %d rows", len(rows))
			}
		})
	}
}
