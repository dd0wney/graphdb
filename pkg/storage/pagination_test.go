package storage

import (
	"sort"
	"sync/atomic"
	"testing"
)

// ---------- functional tests ----------

// paginationFixture builds a GraphStorage populated with nNodes nodes (all
// with label "Pager") and nEdges edges (all of type "PLINK") under the given
// tenant. The first two nodes are connected by all edges (src→dst).
func paginationFixture(t *testing.T, tenantID string, nNodes, nEdges int) (*GraphStorage, []uint64, []uint64) {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	nodeIDs := make([]uint64, 0, nNodes)
	for i := 0; i < nNodes; i++ {
		n, nerr := gs.CreateNodeWithTenant(tenantID, []string{"Pager"}, nil)
		if nerr != nil {
			t.Fatalf("CreateNodeWithTenant(%d): %v", i, nerr)
		}
		nodeIDs = append(nodeIDs, n.ID)
	}

	if nEdges > 0 && nNodes < 2 {
		t.Fatal("nNodes must be >= 2 to create edges")
	}

	edgeIDs := make([]uint64, 0, nEdges)
	for i := 0; i < nEdges; i++ {
		e, eerr := gs.CreateEdgeWithTenant(
			tenantID,
			nodeIDs[0], nodeIDs[1],
			"PLINK", nil, 1.0,
		)
		if eerr != nil {
			t.Fatalf("CreateEdgeWithTenant(%d): %v", i, eerr)
		}
		edgeIDs = append(edgeIDs, e.ID)
	}

	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	sort.Slice(edgeIDs, func(i, j int) bool { return edgeIDs[i] < edgeIDs[j] })
	return gs, nodeIDs, edgeIDs
}

// idsFromNodes extracts and sorts IDs from a node slice.
func idsFromNodes(nodes []*Node) []uint64 {
	out := make([]uint64, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// idsFromEdges extracts and sorts IDs from an edge slice.
func idsFromEdges(edges []*Edge) []uint64 {
	out := make([]uint64, len(edges))
	for i, e := range edges {
		out[i] = e.ID
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// walkNodes does a full cursor walk via NodesPageForTenant and returns all
// collected node IDs in order.
func walkNodes(t *testing.T, gs *GraphStorage, tenantID string, limit int) []uint64 {
	t.Helper()
	var all []uint64
	var afterID uint64
	for {
		page, next := gs.NodesPageForTenant(tenantID, afterID, limit)
		for _, n := range page {
			all = append(all, n.ID)
		}
		if next == 0 {
			break
		}
		afterID = next
	}
	return all
}

// walkNodesByLabel does a full cursor walk via NodesByLabelPageForTenant.
func walkNodesByLabel(t *testing.T, gs *GraphStorage, tenantID, label string, limit int) []uint64 {
	t.Helper()
	var all []uint64
	var afterID uint64
	for {
		page, next := gs.NodesByLabelPageForTenant(tenantID, label, afterID, limit)
		for _, n := range page {
			all = append(all, n.ID)
		}
		if next == 0 {
			break
		}
		afterID = next
	}
	return all
}

// walkEdges does a full cursor walk via EdgesPageForTenant.
func walkEdges(t *testing.T, gs *GraphStorage, tenantID string, limit int) []uint64 {
	t.Helper()
	var all []uint64
	var afterID uint64
	for {
		page, next := gs.EdgesPageForTenant(tenantID, afterID, limit)
		for _, e := range page {
			all = append(all, e.ID)
		}
		if next == 0 {
			break
		}
		afterID = next
	}
	return all
}

// walkEdgesByType does a full cursor walk via EdgesByTypePageForTenant.
func walkEdgesByType(t *testing.T, gs *GraphStorage, tenantID, edgeType string, limit int) []uint64 {
	t.Helper()
	var all []uint64
	var afterID uint64
	for {
		page, next := gs.EdgesByTypePageForTenant(tenantID, edgeType, afterID, limit)
		for _, e := range page {
			all = append(all, e.ID)
		}
		if next == 0 {
			break
		}
		afterID = next
	}
	return all
}

// slicesEqual reports whether two uint64 slices are identical.
func slicesEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------- TestNodesPageForTenant ----------

func TestNodesPageForTenant(t *testing.T) {
	const (
		tenant = "acme"
		total  = 25
		limit  = 10
	)
	gs, nodeIDs, _ := paginationFixture(t, tenant, total, 0)

	t.Run("seek skips IDs <= afterID", func(t *testing.T) {
		// afterID = nodeIDs[4] → should return at most `limit` items all > nodeIDs[4]
		afterID := nodeIDs[4]
		page, _ := gs.NodesPageForTenant(tenant, afterID, limit)
		for _, n := range page {
			if n.ID <= afterID {
				t.Errorf("got ID %d <= afterID %d; seek failed", n.ID, afterID)
			}
		}
	})

	t.Run("afterID=0 starts from beginning", func(t *testing.T) {
		page, _ := gs.NodesPageForTenant(tenant, 0, limit)
		if len(page) != limit {
			t.Errorf("first page len = %d, want %d", len(page), limit)
		}
		if page[0].ID != nodeIDs[0] {
			t.Errorf("first item ID = %d, want %d", page[0].ID, nodeIDs[0])
		}
	})

	t.Run("returns at most limit", func(t *testing.T) {
		page, _ := gs.NodesPageForTenant(tenant, 0, limit)
		if len(page) > limit {
			t.Errorf("page len %d exceeds limit %d", len(page), limit)
		}
	})

	t.Run("next = last item ID when more exist", func(t *testing.T) {
		page, next := gs.NodesPageForTenant(tenant, 0, limit)
		if len(page) < limit {
			t.Skip("not enough items to test has-more cursor")
		}
		if next != page[len(page)-1].ID {
			t.Errorf("next = %d, want last page ID %d", next, page[len(page)-1].ID)
		}
	})

	t.Run("next = 0 on last page", func(t *testing.T) {
		// Seek past all but the last item.
		afterID := nodeIDs[total-2]
		_, next := gs.NodesPageForTenant(tenant, afterID, limit)
		if next != 0 {
			t.Errorf("last page next = %d, want 0", next)
		}
	})

	t.Run("liveness probe: next = 0 when all post-page items deleted", func(t *testing.T) {
		// Build a fresh fixture with exactly limit+2 nodes so the first page
		// fills completely and the two trailing nodes can be deleted. After
		// deletion, the probe must find no live items beyond the page and
		// return next=0 rather than a stale cursor pointing at a dead ID.
		gs3, ids3, _ := paginationFixture(t, "probe-tenant", limit+2, 0)

		// Delete both trailing nodes (ids3[limit] and ids3[limit+1]).
		for _, id := range ids3[limit:] {
			if err := gs3.DeleteNodeForTenant(id, "probe-tenant"); err != nil {
				t.Fatalf("DeleteNodeForTenant(%d): %v", id, err)
			}
		}

		// First page should be full (limit items), next must be 0 because no
		// live items remain beyond the page.
		page, next := gs3.NodesPageForTenant("probe-tenant", 0, limit)
		if len(page) != limit {
			t.Errorf("page len = %d, want %d", len(page), limit)
		}
		if next != 0 {
			t.Errorf("next = %d, want 0 — liveness probe failed: stale cursor to deleted items", next)
		}
	})

	t.Run("full cursor-walk yields every node exactly once in ascending order", func(t *testing.T) {
		walked := walkNodes(t, gs, tenant, limit)
		if !slicesEqual(walked, nodeIDs) {
			t.Errorf("walk yielded %v, want %v", walked, nodeIDs)
		}
	})

	t.Run("equivalence: walk == GetAllNodesForTenant sorted", func(t *testing.T) {
		walked := walkNodes(t, gs, tenant, limit)
		all := idsFromNodes(gs.GetAllNodesForTenant(tenant))
		if !slicesEqual(walked, all) {
			t.Errorf("walk %v != GetAll %v", walked, all)
		}
	})

	t.Run("empty tenant returns empty + 0", func(t *testing.T) {
		page, next := gs.NodesPageForTenant("nonexistent-tenant", 0, limit)
		if len(page) != 0 || next != 0 {
			t.Errorf("empty tenant: got len=%d next=%d, want 0, 0", len(page), next)
		}
	})

	t.Run("deleted-mid-walk node is omitted and walk still terminates", func(t *testing.T) {
		gs2, ids2, _ := paginationFixture(t, "del-tenant", 15, 0)

		// Walk page 1 (limit=5), then delete an ID from page 2, then walk the rest.
		page1, next1 := gs2.NodesPageForTenant("del-tenant", 0, 5)
		if next1 == 0 {
			t.Skip("fixture too small for delete-mid-walk test")
		}
		_ = page1

		// Delete the first node that would appear on page 2.
		victim := ids2[5]
		if err := gs2.DeleteNodeForTenant(victim, "del-tenant"); err != nil {
			t.Fatalf("DeleteNodeForTenant: %v", err)
		}

		// Continue the walk from next1; victim must not appear.
		var rest []uint64
		afterID := next1
		for {
			page, next := gs2.NodesPageForTenant("del-tenant", afterID, 5)
			for _, n := range page {
				if n.ID == victim {
					t.Errorf("deleted node %d appeared in walk", victim)
				}
				rest = append(rest, n.ID)
			}
			if next == 0 {
				break
			}
			afterID = next
		}
		// Expect 14 - 5 (page1) = 9 items in rest; but victim was in pos 5 so rest = 14-5-1 = 9 without it
		if len(rest) != 9 {
			t.Errorf("rest len = %d, want 9 (14 total - 5 page1 - 1 deleted)", len(rest))
		}
	})
}

// ---------- TestNodesByLabelPageForTenant ----------

func TestNodesByLabelPageForTenant(t *testing.T) {
	const (
		tenant = "beta"
		total  = 25
		limit  = 10
	)
	gs, nodeIDs, _ := paginationFixture(t, tenant, total, 0)

	t.Run("seek skips IDs <= afterID", func(t *testing.T) {
		afterID := nodeIDs[4]
		page, _ := gs.NodesByLabelPageForTenant(tenant, "Pager", afterID, limit)
		for _, n := range page {
			if n.ID <= afterID {
				t.Errorf("got ID %d <= afterID %d", n.ID, afterID)
			}
		}
	})

	t.Run("full cursor-walk yields every node exactly once in ascending order", func(t *testing.T) {
		walked := walkNodesByLabel(t, gs, tenant, "Pager", limit)
		if !slicesEqual(walked, nodeIDs) {
			t.Errorf("walk %v != expected %v", walked, nodeIDs)
		}
	})

	t.Run("equivalence: walk == GetNodesByLabelForTenant sorted", func(t *testing.T) {
		walked := walkNodesByLabel(t, gs, tenant, "Pager", limit)
		all := idsFromNodes(gs.GetNodesByLabelForTenant(tenant, "Pager"))
		if !slicesEqual(walked, all) {
			t.Errorf("walk %v != GetAll %v", walked, all)
		}
	})

	t.Run("unknown label returns empty + 0", func(t *testing.T) {
		page, next := gs.NodesByLabelPageForTenant(tenant, "NoSuchLabel", 0, limit)
		if len(page) != 0 || next != 0 {
			t.Errorf("unknown label: len=%d next=%d, want 0, 0", len(page), next)
		}
	})

	t.Run("unknown tenant returns empty + 0", func(t *testing.T) {
		page, next := gs.NodesByLabelPageForTenant("ghost-tenant", "Pager", 0, limit)
		if len(page) != 0 || next != 0 {
			t.Errorf("unknown tenant: len=%d next=%d, want 0, 0", len(page), next)
		}
	})
}

// ---------- TestEdgesPageForTenant ----------

func TestEdgesPageForTenant(t *testing.T) {
	const (
		tenant = "gamma"
		nNodes = 2
		total  = 25
		limit  = 10
	)
	gs, _, edgeIDs := paginationFixture(t, tenant, nNodes, total)

	t.Run("seek skips IDs <= afterID", func(t *testing.T) {
		afterID := edgeIDs[4]
		page, _ := gs.EdgesPageForTenant(tenant, afterID, limit)
		for _, e := range page {
			if e.ID <= afterID {
				t.Errorf("got ID %d <= afterID %d", e.ID, afterID)
			}
		}
	})

	t.Run("returns at most limit", func(t *testing.T) {
		page, _ := gs.EdgesPageForTenant(tenant, 0, limit)
		if len(page) > limit {
			t.Errorf("page len %d exceeds limit %d", len(page), limit)
		}
	})

	t.Run("next = last item ID when more exist", func(t *testing.T) {
		page, next := gs.EdgesPageForTenant(tenant, 0, limit)
		if len(page) < limit {
			t.Skip("not enough items to test has-more cursor")
		}
		if next != page[len(page)-1].ID {
			t.Errorf("next = %d, want %d", next, page[len(page)-1].ID)
		}
	})

	t.Run("next = 0 on last page", func(t *testing.T) {
		afterID := edgeIDs[total-2]
		_, next := gs.EdgesPageForTenant(tenant, afterID, limit)
		if next != 0 {
			t.Errorf("last page next = %d, want 0", next)
		}
	})

	t.Run("full cursor-walk yields every edge exactly once in ascending order", func(t *testing.T) {
		walked := walkEdges(t, gs, tenant, limit)
		if !slicesEqual(walked, edgeIDs) {
			t.Errorf("walk %v != expected %v", walked, edgeIDs)
		}
	})

	t.Run("equivalence: walk == GetAllEdgesForTenant sorted", func(t *testing.T) {
		walked := walkEdges(t, gs, tenant, limit)
		all := idsFromEdges(gs.GetAllEdgesForTenant(tenant))
		if !slicesEqual(walked, all) {
			t.Errorf("walk %v != GetAll %v", walked, all)
		}
	})

	t.Run("empty tenant returns empty + 0", func(t *testing.T) {
		page, next := gs.EdgesPageForTenant("nonexistent-tenant", 0, limit)
		if len(page) != 0 || next != 0 {
			t.Errorf("empty tenant: len=%d next=%d, want 0, 0", len(page), next)
		}
	})

	t.Run("deleted-mid-walk edge is omitted and walk still terminates", func(t *testing.T) {
		gs2, _, eids2 := paginationFixture(t, "del-edge-tenant", 2, 15)

		page1, next1 := gs2.EdgesPageForTenant("del-edge-tenant", 0, 5)
		if next1 == 0 {
			t.Skip("fixture too small for delete-mid-walk test")
		}
		_ = page1

		victim := eids2[5]
		if err := gs2.DeleteEdgeForTenant(victim, "del-edge-tenant"); err != nil {
			t.Fatalf("DeleteEdgeForTenant: %v", err)
		}

		var rest []uint64
		afterID := next1
		for {
			page, next := gs2.EdgesPageForTenant("del-edge-tenant", afterID, 5)
			for _, e := range page {
				if e.ID == victim {
					t.Errorf("deleted edge %d appeared in walk", victim)
				}
				rest = append(rest, e.ID)
			}
			if next == 0 {
				break
			}
			afterID = next
		}
		if len(rest) != 9 {
			t.Errorf("rest len = %d, want 9 (14 total - 5 page1 - 1 deleted)", len(rest))
		}
	})
}

// ---------- TestEdgesByTypePageForTenant ----------

func TestEdgesByTypePageForTenant(t *testing.T) {
	const (
		tenant = "delta"
		nNodes = 2
		total  = 25
		limit  = 10
	)
	gs, _, edgeIDs := paginationFixture(t, tenant, nNodes, total)

	t.Run("seek skips IDs <= afterID", func(t *testing.T) {
		afterID := edgeIDs[4]
		page, _ := gs.EdgesByTypePageForTenant(tenant, "PLINK", afterID, limit)
		for _, e := range page {
			if e.ID <= afterID {
				t.Errorf("got ID %d <= afterID %d", e.ID, afterID)
			}
		}
	})

	t.Run("full cursor-walk yields every edge exactly once in ascending order", func(t *testing.T) {
		walked := walkEdgesByType(t, gs, tenant, "PLINK", limit)
		if !slicesEqual(walked, edgeIDs) {
			t.Errorf("walk %v != expected %v", walked, edgeIDs)
		}
	})

	t.Run("equivalence: walk == GetEdgesByTypeForTenant sorted", func(t *testing.T) {
		walked := walkEdgesByType(t, gs, tenant, "PLINK", limit)
		all := idsFromEdges(gs.GetEdgesByTypeForTenant(tenant, "PLINK"))
		if !slicesEqual(walked, all) {
			t.Errorf("walk %v != GetAll %v", walked, all)
		}
	})

	t.Run("unknown edge type returns empty + 0", func(t *testing.T) {
		page, next := gs.EdgesByTypePageForTenant(tenant, "NO_SUCH_TYPE", 0, limit)
		if len(page) != 0 || next != 0 {
			t.Errorf("unknown type: len=%d next=%d, want 0, 0", len(page), next)
		}
	})

	t.Run("unknown tenant returns empty + 0", func(t *testing.T) {
		page, next := gs.EdgesByTypePageForTenant("ghost-tenant", "PLINK", 0, limit)
		if len(page) != 0 || next != 0 {
			t.Errorf("unknown tenant: len=%d next=%d, want 0, 0", len(page), next)
		}
	})
}

// ---------- benchmarks ----------

// benchPageFixture builds a storage with 10000 nodes in the default tenant for
// pagination benchmarks. Built once outside b.N; uses BulkImportMode to skip
// WAL for fast corpus construction (same discipline as setupBenchCorpus in
// bench_concurrent_read_test.go). CreateNode is used (not BeginBatch) so the
// default-tenant index is populated — BulkImportMode only skips WAL writes,
// not in-memory index maintenance.
func benchPageFixture(b *testing.B) *GraphStorage {
	b.Helper()
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:        b.TempDir(),
		BulkImportMode: true,
	})
	if err != nil {
		b.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	b.Cleanup(func() { _ = gs.Close() })
	for i := 0; i < 10_000; i++ {
		if _, err := gs.CreateNode([]string{"Bench"}, nil); err != nil {
			b.Fatalf("CreateNode %d: %v", i, err)
		}
	}
	return gs
}

var benchNodePageSink atomic.Pointer[Node]

// BenchmarkNodesPageForTenant measures fetching one page of 100 from a 10k
// corpus — expects ~100 Clone calls.
func BenchmarkNodesPageForTenant(b *testing.B) {
	gs := benchPageFixture(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		page, _ := gs.NodesPageForTenant("", 0, 100)
		if len(page) > 0 {
			benchNodePageSink.Store(page[0])
		}
	}
}

// BenchmarkNodesGetAllThenPaginate is the "clone everything, then take 100"
// baseline. Expects ~10000 Clone calls — the index-level win is the allocs
// delta between this and BenchmarkNodesPageForTenant.
func BenchmarkNodesGetAllThenPaginate(b *testing.B) {
	gs := benchPageFixture(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		all := gs.GetAllNodesForTenant("")
		// Mirror the old GetAll+paginate baseline: take the first 100 items with ID > 0.
		var page []*Node
		for _, n := range all {
			if n.ID > 0 {
				page = append(page, n)
				if len(page) == 100 {
					break
				}
			}
		}
		if len(page) > 0 {
			benchNodePageSink.Store(page[0])
		}
	}
}
