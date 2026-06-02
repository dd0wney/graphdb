package storage

import (
	"sort"
	"testing"
)

// makeEdgeFixtureNodes creates two nodes for the given tenant and returns
// their IDs, for use as edge endpoints.
func makeEdgeFixtureNodes(t *testing.T, gs *GraphStorage, tenant string) (uint64, uint64) {
	t.Helper()
	a, err := gs.CreateNodeWithTenant(tenant, nil, nil)
	if err != nil {
		t.Fatalf("create node a: %v", err)
	}
	b, err := gs.CreateNodeWithTenant(tenant, nil, nil)
	if err != nil {
		t.Fatalf("create node b: %v", err)
	}
	return a.ID, b.ID
}

// TestGetAllEdgesForTenant_TenantIsolationAndOrder pins that the edge
// enumeration index returns exactly the caller's edges, in ascending ID
// order, excluding co-located tenants' edges.
func TestGetAllEdgesForTenant_TenantIsolationAndOrder(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a, b := makeEdgeFixtureNodes(t, gs, "acme")
	var acmeEdges []uint64
	for i := 0; i < 5; i++ {
		e, err := gs.CreateEdgeWithTenant("acme", a, b, "LINKS", nil, 1.0)
		if err != nil {
			t.Fatalf("create acme edge: %v", err)
		}
		acmeEdges = append(acmeEdges, e.ID)
	}
	// Interleave another tenant's edges.
	oa, ob := makeEdgeFixtureNodes(t, gs, "other")
	for i := 0; i < 3; i++ {
		if _, err := gs.CreateEdgeWithTenant("other", oa, ob, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("create other edge: %v", err)
		}
	}

	got := gs.GetAllEdgesForTenant("acme")
	if len(got) != 5 {
		t.Fatalf("expected 5 acme edges, got %d (cross-tenant leak or miss)", len(got))
	}

	gotIDs := make([]uint64, len(got))
	for i, e := range got {
		gotIDs[i] = e.ID
		if e.TenantID != "acme" {
			t.Errorf("edge %d has tenant %q, expected acme", e.ID, e.TenantID)
		}
	}
	if !sort.SliceIsSorted(gotIDs, func(i, j int) bool { return gotIDs[i] < gotIDs[j] }) {
		t.Errorf("results not in ascending ID order: %v", gotIDs)
	}
}

// TestGetAllEdgesForTenant_DeleteRemovesFromEnumeration pins that a deleted
// edge leaves the enumeration set and an emptied tenant drops its set.
func TestGetAllEdgesForTenant_DeleteRemovesFromEnumeration(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a, b := makeEdgeFixtureNodes(t, gs, "acme")
	e1, _ := gs.CreateEdgeWithTenant("acme", a, b, "LINKS", nil, 1.0)
	e2, _ := gs.CreateEdgeWithTenant("acme", a, b, "LINKS", nil, 1.0)

	if err := gs.DeleteEdgeForTenant(e1.ID, "acme"); err != nil {
		t.Fatalf("delete edge: %v", err)
	}
	got := gs.GetAllEdgesForTenant("acme")
	if len(got) != 1 || got[0].ID != e2.ID {
		t.Fatalf("expected only edge %d after deleting %d, got %v", e2.ID, e1.ID, got)
	}

	if err := gs.DeleteEdgeForTenant(e2.ID, "acme"); err != nil {
		t.Fatalf("delete edge: %v", err)
	}
	if got := gs.GetAllEdgesForTenant("acme"); len(got) != 0 {
		t.Fatalf("expected 0 edges after deleting all, got %d", len(got))
	}
	gs.mu.RLock()
	_, present := gs.tenantEdgeIDs[effectiveTenantID("acme")]
	gs.mu.RUnlock()
	if present {
		t.Errorf("tenantEdgeIDs[acme] should be deleted once empty")
	}
}

// TestGetAllEdgesForTenant_SurvivesRestart confirms the edge enumeration
// index is rebuilt on snapshot load via addEdgeToTenantIndex. This depends
// on the edge tenant index being wired into snapshot-load + WAL-replay
// (#259) — without that fix the rebuild hook never fires and this returns 0.
func TestGetAllEdgesForTenant_SurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		a, b := makeEdgeFixtureNodes(t, gs, "acme")
		for i := 0; i < 3; i++ {
			if _, err := gs.CreateEdgeWithTenant("acme", a, b, "LINKS", nil, 1.0); err != nil {
				t.Fatalf("create edge: %v", err)
			}
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	if got := gs2.GetAllEdgesForTenant("acme"); len(got) != 3 {
		t.Errorf("expected 3 acme edges after restart, got %d — edge enumeration index not rebuilt from snapshot", len(got))
	}
}
