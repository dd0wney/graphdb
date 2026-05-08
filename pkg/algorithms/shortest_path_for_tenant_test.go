package algorithms

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Audit A6b (2026-05-08): pin the tenant-scoping contract at the
// algorithm layer. The HTTP-level tests in pkg/api are end-to-end —
// they wouldn't catch a refactor that, say, dedups
// expandFrontierForTenant with expandFrontier via a filter param and
// drops the filter on one branch. This file pins the algorithm
// directly: no Server, no httptest.
//
// Mirrors how pkg/storage/tenant_signatures_test.go pins the storage
// layer's *ForTenant guarantees.

// TestShortestPathForTenant_FiltersCrossTenantShortcut is the
// must-not-regress case: tenant-A path A1→X→A2 (length 3) coexists
// with a tenant-B-stamped shortcut A1→A2 (length 2). The algorithm
// must filter at edge expansion and return the tenant-A path —
// post-filtering would deny a path that exists in A's subgraph.
func TestShortestPathForTenant_FiltersCrossTenantShortcut(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a1, err := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("a1: %v", err)
	}
	x, err := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("x: %v", err)
	}
	a2, err := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("a2: %v", err)
	}

	if _, err := gs.CreateEdgeWithTenant("tenant-A", a1.ID, x.ID, "REL", nil, 0); err != nil {
		t.Fatalf("a1→x: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant("tenant-A", x.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("x→a2: %v", err)
	}
	// Cross-tenant shortcut.
	if _, err := gs.CreateEdgeWithTenant("tenant-B", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("shortcut: %v", err)
	}

	got, err := ShortestPathForTenant(gs, a1.ID, a2.ID, "tenant-A")
	if err != nil {
		t.Fatalf("ShortestPathForTenant: %v", err)
	}
	want := []uint64{a1.ID, x.ID, a2.ID}
	if len(got) != len(want) {
		t.Fatalf("want path %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d]: want %d, got %d (full=%v)", i, want[i], got[i], got)
		}
	}
}

// TestShortestPathForTenant_NoPathWhenOnlyCrossTenantExists pins the
// other side of the contract: if the *only* path between two tenant-A
// nodes goes through tenant-B-stamped edges, the algorithm reports no
// path. (Pre-A6b would have returned a path through cross-tenant
// edges — exfiltrating B's connectivity to A.)
func TestShortestPathForTenant_NoPathWhenOnlyCrossTenantExists(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a1, err := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("a1: %v", err)
	}
	a2, err := gs.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if err != nil {
		t.Fatalf("a2: %v", err)
	}

	// Only edge between them is owned by B.
	if _, err := gs.CreateEdgeWithTenant("tenant-B", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := ShortestPathForTenant(gs, a1.ID, a2.ID, "tenant-A")
	if err != nil {
		t.Fatalf("ShortestPathForTenant: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty path (no tenant-A connectivity), got %v", got)
	}
}

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
