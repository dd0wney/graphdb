package algorithms

import (
	"context"
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestAlgorithms_HonorContextCancellation pins security audit finding H-6:
// the per-tenant graph algorithms must observe the request context, so a
// request that exceeds its deadline (or whose client disconnects) stops
// promptly instead of running the O(V·E)/O(V²) computation to completion
// in a goroutine no one is waiting on.
//
// Each algorithm is invoked with an already-cancelled context over a
// non-trivial graph and must return the context error rather than a
// result. Pre-fix these functions took no context and ran unconditionally.
func TestAlgorithms_HonorContextCancellation(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer gs.Close()

	const tid = "t-cancel"
	// A small connected graph — enough that every algorithm's outer loop
	// has work to do, so the first cancellation check is reached.
	ids := make([]uint64, 0, 6)
	for i := 0; i < 6; i++ {
		n, cErr := gs.CreateNodeWithTenant(tid, []string{"N"}, nil)
		if cErr != nil {
			t.Fatalf("create node: %v", cErr)
		}
		ids = append(ids, n.ID)
	}
	for i := 0; i < len(ids); i++ {
		for j := 0; j < len(ids); j++ {
			if i != j {
				if _, eErr := gs.CreateEdgeWithTenant(tid, ids[i], ids[j], "LINKS", nil, 1.0); eErr != nil {
					t.Fatalf("create edge: %v", eErr)
				}
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	checks := []struct {
		name string
		run  func() error
	}{
		{"betweenness", func() error {
			_, e := BetweennessCentralityForTenant(ctx, gs, tid)
			return e
		}},
		{"edge_betweenness", func() error {
			_, e := EdgeBetweennessCentralityForTenant(ctx, gs, tid)
			return e
		}},
		{"scc", func() error {
			_, e := StronglyConnectedComponentsForTenant(ctx, gs, tid)
			return e
		}},
		{"triangles", func() error {
			_, e := CountTrianglesForTenant(ctx, gs, tid)
			return e
		}},
		{"node_similarity_all", func() error {
			_, e := NodeSimilarityAllForTenant(ctx, gs, DefaultNodeSimilarityOptions(), tid)
			return e
		}},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			err := c.run()
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s under cancelled context: got err %v, want context.Canceled", c.name, err)
			}
		})
	}
}
