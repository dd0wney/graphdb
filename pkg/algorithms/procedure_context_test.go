package algorithms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// ringWithChords builds a connected, cyclic graph with common neighbours, so
// every algorithm's loops have work to do.
func ringWithChords(t *testing.T, tid string, n int) (*storage.GraphStorage, []uint64) {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { gs.Close() })

	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		node, cErr := gs.CreateNodeWithTenant(tid, []string{"N"}, nil)
		if cErr != nil {
			t.Fatalf("create node: %v", cErr)
		}
		ids = append(ids, node.ID)
	}
	for i := range ids {
		for _, step := range []int{1, 2, 5} {
			j := (i + step) % n
			if _, eErr := gs.CreateEdgeWithTenant(tid, ids[i], ids[j], "LINKS", nil, 1.0); eErr != nil {
				t.Fatalf("create edge: %v", eErr)
			}
		}
	}
	return gs, ids
}

// TestContextVariants_RefuseCancelledContext pins the v1.4 fix for the
// algorithms behind the CALL procedures. They took no context, so a CALL could
// not stop at the query timeout. Each ...Context variant must return an error
// that wraps context.Canceled when its context is already done.
func TestContextVariants_RefuseCancelledContext(t *testing.T) {
	const tid = "t-ctx"
	const n = 40
	gs, ids := ringWithChords(t, tid, n)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	khop := DefaultKHopOptions()
	khop.MaxHops = 3
	adamic := DefaultLinkPredictionOptions()
	adamic.Method = LinkPredAdamicAdar
	adamic.Direction = DirectionBoth
	sim := DefaultNodeSimilarityOptions()
	sim.Direction = DirectionBoth

	checks := []struct {
		name string
		run  func() error
	}{
		{"pagerank", func() error {
			_, e := PageRankForTenantContext(ctx, gs, DefaultPageRankOptions(), tid)
			return e
		}},
		{"shortest_path", func() error {
			_, e := ShortestPathForTenantContext(ctx, gs, ids[0], ids[n/2], tid)
			return e
		}},
		{"khop", func() error {
			_, e := KHopNeighboursForTenantContext(ctx, gs, ids[0], khop, tid)
			return e
		}},
		{"node_similarity_pair", func() error {
			_, e := NodeSimilarityPairForTenantContext(ctx, gs, ids[0], ids[1], sim, tid)
			return e
		}},
		{"link_prediction", func() error {
			_, e := PredictLinkScoreForTenantContext(ctx, gs, ids[0], ids[1], adamic, tid)
			return e
		}},
		{"detect_cycles", func() error {
			_, e := DetectCyclesForTenantContext(ctx, gs, tid)
			return e
		}},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want one wrapping context.Canceled", err)
			}
		})
	}
}

// TestContextVariants_DeadlineWrapsDeadlineExceeded covers the other half of
// the contract: an expired deadline must satisfy errors.Is(DeadlineExceeded),
// which is what handleQuery maps to HTTP 408.
func TestContextVariants_DeadlineWrapsDeadlineExceeded(t *testing.T) {
	const tid = "t-dl"
	gs, _ := ringWithChords(t, tid, 40)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := PageRankForTenantContext(ctx, gs, DefaultPageRankOptions(), tid)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want one wrapping context.DeadlineExceeded", err)
	}
}

// TestContextVariants_LiveContextMatchesPlainFunction guards the delegation:
// with a live context the variant answers exactly as the plain function.
func TestContextVariants_LiveContextMatchesPlainFunction(t *testing.T) {
	const tid = "t-live"
	gs, ids := ringWithChords(t, tid, 20)
	bg := context.Background()

	p1, e1 := ShortestPathForTenant(gs, ids[0], ids[10], tid)
	p2, e2 := ShortestPathForTenantContext(bg, gs, ids[0], ids[10], tid)
	if e1 != nil || e2 != nil || len(p1) == 0 || len(p1) != len(p2) {
		t.Fatalf("shortest path differs: %v %v / %v %v", p1, e1, p2, e2)
	}

	r1, e1 := PageRankForTenant(gs, DefaultPageRankOptions(), tid)
	r2, e2 := PageRankForTenantContext(bg, gs, DefaultPageRankOptions(), tid)
	if e1 != nil || e2 != nil || r1.Iterations != r2.Iterations || len(r1.Scores) != len(r2.Scores) {
		t.Fatalf("pagerank differs: %+v %v / %+v %v", r1, e1, r2, e2)
	}
	for id, s := range r1.Scores {
		if r2.Scores[id] != s {
			t.Fatalf("score for %d differs: %v vs %v", id, s, r2.Scores[id])
		}
	}

	c1, e1 := DetectCyclesForTenant(gs, tid)
	c2, e2 := DetectCyclesForTenantContext(bg, gs, tid)
	if e1 != nil || e2 != nil || len(c1) != len(c2) || len(c1) == 0 {
		t.Fatalf("cycles differ: %d %v / %d %v", len(c1), e1, len(c2), e2)
	}
}
