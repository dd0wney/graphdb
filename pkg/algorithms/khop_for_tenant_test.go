package algorithms

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Audit A6c-algorithms (2026-05-17): kHop is registered behind the
// /algorithms HTTP endpoint's withTenant middleware but its handler
// historically called the tenant-blind variant — leaking foreign
// tenant node IDs via by_hop / distances during BFS expansion. The
// KHopNeighboursForTenant variant + handler fix close that gap; this
// file pins the algorithm-level contract.
//
// Like shortest_path_for_tenant_test.go, we don't test the
// "constructed cross-tenant edge" case — CreateEdgeWithTenant is
// tenant-strict on node verification, so cross-tenant edges can no
// longer be constructed via public API. Disjoint-subgraphs is the
// remaining surface.

// TestKHopNeighboursForTenant_FindsTenantANeighbours: tenant-A's BFS
// reaches A's reachable nodes and isn't confused by tenant-B's
// existing-but-unconnected subgraph.
func TestKHopNeighboursForTenant_FindsTenantANeighbours(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a1, _ := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a2, _ := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a3, _ := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	b1, _ := gs.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b2, _ := gs.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)

	if _, err := gs.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("a1→a2: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant("tenant-A", a2.ID, a3.ID, "REL", nil, 0); err != nil {
		t.Fatalf("a2→a3: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("b1→b2: %v", err)
	}

	opts := DefaultKHopOptions()
	opts.MaxHops = 3
	opts.Direction = DirectionOut

	got, err := KHopNeighboursForTenant(gs, a1.ID, opts, "tenant-A")
	if err != nil {
		t.Fatalf("KHopNeighboursForTenant: %v", err)
	}

	if got.TotalReachable != 2 {
		t.Errorf("TotalReachable = %d, want 2 (a2 and a3)", got.TotalReachable)
	}
	for _, id := range []uint64{b1.ID, b2.ID} {
		if _, hit := got.Distances[id]; hit {
			t.Errorf("tenant-B node %d appeared in tenant-A BFS distances", id)
		}
	}
}

// TestKHopNeighboursForTenant_ForeignSourceReturnsEmpty: starting BFS
// from a node owned by a different tenant must not surface that
// tenant's neighbours. The expansion calls GetOutgoingEdgesForTenant
// which returns no rows for cross-tenant lookups (the unified
// existence-leak discipline), so the BFS simply has no edges to walk.
func TestKHopNeighboursForTenant_ForeignSourceReturnsEmpty(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	b1, _ := gs.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b2, _ := gs.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	if _, err := gs.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("b1→b2: %v", err)
	}

	opts := DefaultKHopOptions()
	opts.MaxHops = 3
	opts.Direction = DirectionOut

	got, err := KHopNeighboursForTenant(gs, b1.ID, opts, "tenant-A")
	if err != nil {
		t.Fatalf("KHopNeighboursForTenant: %v", err)
	}
	if got.TotalReachable != 0 {
		t.Errorf("TotalReachable = %d, want 0 (no expansion from foreign-tenant source)", got.TotalReachable)
	}
}
