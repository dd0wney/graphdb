package algorithms

import (
	"context"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func newCommunityTenantGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("new graph storage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

// triangle creates three nodes in tenantID joined a->b->c->a and returns
// their IDs in ascending order.
func triangle(t *testing.T, gs *storage.GraphStorage, tenantID string) []uint64 {
	t.Helper()
	ids := make([]uint64, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := gs.CreateNodeWithTenant(tenantID, []string{"Character"}, map[string]storage.Value{})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		ids = append(ids, n.ID)
	}
	for i := 0; i < 3; i++ {
		if _, err := gs.CreateEdgeWithTenant(tenantID, ids[i], ids[(i+1)%3], "KNOWS", nil, 1); err != nil {
			t.Fatalf("create edge: %v", err)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func communitySets(res *CommunityDetectionResult) [][]uint64 {
	out := make([][]uint64, 0, len(res.Communities))
	for _, c := range res.Communities {
		nodes := append([]uint64(nil), c.Nodes...)
		sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
		out = append(out, nodes)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func assertTwoTriangles(t *testing.T, res *CommunityDetectionResult, a, b []uint64) {
	t.Helper()
	got := communitySets(res)
	if len(got) != 2 {
		t.Fatalf("want 2 communities, got %d: %v", len(got), got)
	}
	for i, want := range [][]uint64{a, b} {
		if len(got[i]) != 3 || got[i][0] != want[0] || got[i][1] != want[1] || got[i][2] != want[2] {
			t.Errorf("community %d = %v, want %v", i, got[i], want)
		}
	}
	if len(res.NodeCommunity) != 6 {
		t.Errorf("NodeCommunity has %d entries, want 6 (only the caller's tenant)", len(res.NodeCommunity))
	}
}

func TestLabelPropagationForTenant_TwoClustersStayApartAndOtherTenantIsHidden(t *testing.T) {
	gs := newCommunityTenantGraph(t)
	a := triangle(t, gs, "t1")
	b := triangle(t, gs, "t1")
	_ = triangle(t, gs, "t2") // must not appear in a t1 result

	res, err := LabelPropagationForTenant(context.Background(), gs, "t1", 20)
	if err != nil {
		t.Fatalf("LabelPropagationForTenant: %v", err)
	}
	assertTwoTriangles(t, res, a, b)
}

func TestConnectedComponentsForTenant_TwoComponentsAndOtherTenantIsHidden(t *testing.T) {
	gs := newCommunityTenantGraph(t)
	a := triangle(t, gs, "t1")
	b := triangle(t, gs, "t1")
	_ = triangle(t, gs, "t2")

	res, err := ConnectedComponentsForTenant(context.Background(), gs, "t1")
	if err != nil {
		t.Fatalf("ConnectedComponentsForTenant: %v", err)
	}
	assertTwoTriangles(t, res, a, b)
}

func TestLabelPropagationForTenant_HonoursCancellation(t *testing.T) {
	gs := newCommunityTenantGraph(t)
	_ = triangle(t, gs, "t1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LabelPropagationForTenant(ctx, gs, "t1", 20); err == nil {
		t.Fatal("want an error from a cancelled context, got nil")
	}
}
