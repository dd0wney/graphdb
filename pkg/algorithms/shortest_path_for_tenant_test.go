package algorithms

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Audit A6b (2026-05-08): algorithm-level contract for the
// tenant-scoped bidirectional BFS.
//
// The two cross-tenant-shortcut tests originally in this file were
// removed in the A6a follow-up: after CreateEdgeWithTenant became
// tenant-strict on node verification (see
// pkg/storage/tenant_signatures_test.go's
// TestCreateEdgeWithTenant_CrossTenantNodeRefIsRefused), cross-tenant
// edges can no longer be constructed via public API. The
// "filter-at-expansion vs post-filter" regression they guarded
// against is now prevented at the storage layer — re-running them
// would require an unsafe internal-edge-insertion helper whose cost
// outweighs the defense-in-depth value.
//
// What remains is the basic tenant-scoping smoke test: same-node
// path works, and the function exists with the expected signature
// (the latter is mostly checked at compile time but pinned here for
// future refactor signal).

// TestShortestPathForTenant_SameNode mirrors the tenant-blind
// SameNode test — degenerate case must still work for tenant-scoped
// callers.
func TestShortestPathForTenant_SameNode(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("n: %v", err)
	}

	got, err := ShortestPathForTenant(gs, n.ID, n.ID, "tenant-A")
	if err != nil {
		t.Fatalf("ShortestPathForTenant: %v", err)
	}
	if len(got) != 1 || got[0] != n.ID {
		t.Errorf("want [%d], got %v", n.ID, got)
	}
}

// TestShortestPathForTenant_FindsLegitimateTenantAPath: with the
// gap-closure, the only legitimate cross-tenant scenario is "two
// disjoint subgraphs." Pin that A's BFS finds A's path and isn't
// confused by B's existing-but-unconnected subgraph.
func TestShortestPathForTenant_FindsLegitimateTenantAPath(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a1, _ := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a2, _ := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	b1, _ := gs.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b2, _ := gs.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)

	if _, err := gs.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("a1→a2: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("b1→b2: %v", err)
	}

	got, err := ShortestPathForTenant(gs, a1.ID, a2.ID, "tenant-A")
	if err != nil {
		t.Fatalf("ShortestPathForTenant: %v", err)
	}
	want := []uint64{a1.ID, a2.ID}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("want %v, got %v", want, got)
	}
}
