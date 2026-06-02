package storage

import (
	"sort"
	"testing"
)

// TestGetAllNodesForTenant_IncludesUnlabeledNodes is the core guarantee of
// the per-tenant enumeration index: GetAllNodesForTenant must return a
// tenant's nodes whether or not they carry labels. tenantNodesByLabel skips
// unlabeled nodes entirely (its loop ranges over node.Labels), which is why
// the pre-index implementation had to fall back to a full cross-tenant shard
// scan. tenantNodeIDs closes that gap.
func TestGetAllNodesForTenant_IncludesUnlabeledNodes(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	labeled, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("create labeled: %v", err)
	}
	unlabeled, err := gs.CreateNodeWithTenant("acme", nil, nil)
	if err != nil {
		t.Fatalf("create unlabeled: %v", err)
	}

	got := gs.GetAllNodesForTenant("acme")
	if len(got) != 2 {
		t.Fatalf("expected 2 nodes (1 labeled + 1 unlabeled), got %d", len(got))
	}

	ids := map[uint64]bool{}
	for _, n := range got {
		ids[n.ID] = true
	}
	if !ids[labeled.ID] {
		t.Errorf("labeled node %d missing from enumeration", labeled.ID)
	}
	if !ids[unlabeled.ID] {
		t.Errorf("unlabeled node %d missing — enumeration index does not cover label-absent nodes", unlabeled.ID)
	}
}

// TestGetAllNodesForTenant_TenantIsolationAndOrder pins tenant scoping and
// the ascending-ID ordering the resolvers' offset cursors rely on.
func TestGetAllNodesForTenant_TenantIsolationAndOrder(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	var acmeIDs []uint64
	for i := 0; i < 5; i++ {
		n, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
		if err != nil {
			t.Fatalf("create acme: %v", err)
		}
		acmeIDs = append(acmeIDs, n.ID)
	}
	// Interleave another tenant's nodes to prove they are excluded.
	for i := 0; i < 3; i++ {
		if _, err := gs.CreateNodeWithTenant("other", []string{"Doc"}, nil); err != nil {
			t.Fatalf("create other: %v", err)
		}
	}

	got := gs.GetAllNodesForTenant("acme")
	if len(got) != 5 {
		t.Fatalf("expected 5 acme nodes, got %d (cross-tenant leak or miss)", len(got))
	}

	gotIDs := make([]uint64, len(got))
	for i, n := range got {
		gotIDs[i] = n.ID
		if n.TenantID != "acme" {
			t.Errorf("node %d has tenant %q, expected acme", n.ID, n.TenantID)
		}
	}
	if !sort.SliceIsSorted(gotIDs, func(i, j int) bool { return gotIDs[i] < gotIDs[j] }) {
		t.Errorf("results not in ascending ID order: %v — pagination cursors would be unstable", gotIDs)
	}
}

// TestGetAllNodesForTenant_DeleteRemovesFromEnumeration pins that a deleted
// node leaves the enumeration set, and that emptying a tenant drops its set.
func TestGetAllNodesForTenant_DeleteRemovesFromEnumeration(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n1, _ := gs.CreateNodeWithTenant("acme", nil, nil)
	n2, _ := gs.CreateNodeWithTenant("acme", nil, nil)

	if err := gs.DeleteNodeForTenant(n1.ID, "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got := gs.GetAllNodesForTenant("acme")
	if len(got) != 1 || got[0].ID != n2.ID {
		t.Fatalf("expected only node %d after deleting %d, got %v", n2.ID, n1.ID, got)
	}

	if err := gs.DeleteNodeForTenant(n2.ID, "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := gs.GetAllNodesForTenant("acme"); len(got) != 0 {
		t.Fatalf("expected 0 nodes after deleting all, got %d", len(got))
	}
	// Tenant set should be cleaned up, not left as an empty map.
	gs.mu.RLock()
	_, present := gs.tenantNodeIDs[effectiveTenantID("acme")]
	gs.mu.RUnlock()
	if present {
		t.Errorf("tenantNodeIDs[acme] should be deleted once empty")
	}
}

// TestRemoveNodeFromTenantIndex_UnlabeledCountStaysAccurate pins the
// stats-drift fix: deleting multiple unlabeled nodes must decrement
// NodeCount once each. Before the labelMap guard was made local, the first
// delete GC'd the (empty) label map and every subsequent delete hit the nil
// early return, silently skipping decrementTenantNodeCount.
func TestRemoveNodeFromTenantIndex_UnlabeledCountStaysAccurate(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	var ids []uint64
	for i := 0; i < 3; i++ {
		n, err := gs.CreateNodeWithTenant("acme", nil, nil) // no labels
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, n.ID)
	}
	if got := gs.CountNodesForTenant("acme"); got != 3 {
		t.Fatalf("expected NodeCount=3 after 3 creates, got %d", got)
	}

	for i, id := range ids {
		if err := gs.DeleteNodeForTenant(id, "acme"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		want := uint64(len(ids) - i - 1)
		if got := gs.CountNodesForTenant("acme"); got != want {
			t.Fatalf("after deleting %d unlabeled nodes, NodeCount=%d, want %d — stats drift on unlabeled delete", i+1, got, want)
		}
	}
}

// TestGetAllNodesForTenant_SurvivesRestart confirms the enumeration index is
// rebuilt on snapshot load via addNodeToTenantIndex — including unlabeled
// nodes, which the snapshot's flat node set carries but the global
// nodesByLabel does not.
func TestGetAllNodesForTenant_SurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		if _, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil); err != nil {
			t.Fatalf("create labeled: %v", err)
		}
		if _, err := gs.CreateNodeWithTenant("acme", nil, nil); err != nil {
			t.Fatalf("create unlabeled: %v", err)
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

	if got := gs2.GetAllNodesForTenant("acme"); len(got) != 2 {
		t.Errorf("expected 2 acme nodes after restart, got %d — enumeration index not rebuilt from snapshot", len(got))
	}
}
