package storage

import (
	"sort"
	"testing"
)

// idsOfNodes extracts node IDs in the order returned, so a test can assert on
// ordering rather than only set membership.
func idsOfNodes(nodes []*Node) []uint64 {
	ids := make([]uint64, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func idsOfEdges(edges []*Edge) []uint64 {
	ids := make([]uint64, len(edges))
	for i, e := range edges {
		ids[i] = e.ID
	}
	return ids
}

func assertSorted(t *testing.T, label string, ids []uint64) {
	t.Helper()
	if !sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] }) {
		t.Errorf("%s: expected IDs in ascending order, got %v", label, ids)
	}
}

func assertSameSet(t *testing.T, label string, got, want []uint64) {
	t.Helper()
	g := append([]uint64(nil), got...)
	w := append([]uint64(nil), want...)
	sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
	sort.Slice(w, func(i, j int) bool { return w[i] < w[j] })
	if len(g) != len(w) {
		t.Errorf("%s: set size mismatch: got %v want %v", label, got, want)
		return
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("%s: set mismatch: got %v want %v", label, got, want)
			return
		}
	}
}

// TestFindNodesByLabelAcrossTenants_DeterministicOrderAfterReorderingDelete pins that the
// global label index returns IDs in deterministic ascending order even after a
// delete. The legacy slice index used swap-with-last removal, which permutes
// the bucket into a mutation-history-dependent order; Path C (set + sort-on-read)
// makes the result order a function of the surviving set alone.
func TestFindNodesByLabelAcrossTenants_DeterministicOrderAfterReorderingDelete(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	var ids []uint64
	for i := 0; i < 4; i++ {
		n, err := gs.CreateNode([]string{"Person"}, nil)
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		ids = append(ids, n.ID)
	}

	// Delete a middle node — swap-with-last removal would leave the bucket as
	// [ids0, ids3, ids2], i.e. not sorted.
	if err := gs.DeleteNode(ids[1]); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}

	persons, err := gs.FindNodesByLabelAcrossTenants("Person")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants: %v", err)
	}
	got := idsOfNodes(persons)
	assertSameSet(t, "FindNodesByLabelAcrossTenants survivors", got, []uint64{ids[0], ids[2], ids[3]})
	assertSorted(t, "FindNodesByLabelAcrossTenants order", got)
}

// TestLabelIndex_SortedAndCorrectAfterReopen pins that both the global
// (FindNodesByLabelAcrossTenants) and per-tenant (GetNodesByLabelForTenant) label indexes
// survive a Close()->reopen under the DEFAULT config (edge compression on) and
// return the surviving set in deterministic ascending order. The per-tenant
// index is rebuilt by iterating the snapshot's flat node map (random Go map
// order), so without sort-on-read the recovered order is nondeterministic.
func TestLabelIndex_SortedAndCorrectAfterReopen(t *testing.T) {
	dir := t.TempDir()

	var surviving []uint64
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		var ids []uint64
		for i := 0; i < 6; i++ {
			n, err := gs.CreateNodeWithTenant("acme", []string{"Person"}, nil)
			if err != nil {
				t.Fatalf("CreateNodeWithTenant: %v", err)
			}
			ids = append(ids, n.ID)
		}
		// Delete two middle nodes to scramble any insertion-order bucket.
		if err := gs.DeleteNodeForTenant(ids[1], "acme"); err != nil {
			t.Fatalf("DeleteNodeForTenant: %v", err)
		}
		if err := gs.DeleteNodeForTenant(ids[3], "acme"); err != nil {
			t.Fatalf("DeleteNodeForTenant: %v", err)
		}
		surviving = []uint64{ids[0], ids[2], ids[4], ids[5]}
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	globalNodes, err := gs.FindNodesByLabelAcrossTenants("Person")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants after reopen: %v", err)
	}
	gGlobal := idsOfNodes(globalNodes)
	assertSameSet(t, "global survivors after reopen", gGlobal, surviving)
	assertSorted(t, "global order after reopen", gGlobal)

	tenantNodes := gs.GetNodesByLabelForTenant("acme", "Person")
	gTenant := idsOfNodes(tenantNodes)
	assertSameSet(t, "tenant survivors after reopen", gTenant, surviving)
	assertSorted(t, "tenant order after reopen", gTenant)
}

// TestGlobalLabelIndex_StickyLabelSurvivesReopen pins that a label whose last
// node has been deleted stays registered in the global index (GetAllLabels)
// both in-process and across a Close()->reopen. The GraphQL schema is generated
// from GetAllLabels, so a label silently vanishing when its last node is deleted
// (or on the next restart) would drop its query fields and 400 previously-valid
// client queries. Pre-Path-C the slice index left an empty bucket under the key;
// Path C reconstructs that stickiness via the in-memory keep-empty removal plus
// seeding the global keys from the snapshot on load.
func TestGlobalLabelIndex_StickyLabelSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		n, err := gs.CreateNode([]string{"Person"}, nil)
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		if err := gs.DeleteNode(n.ID); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
		if !containsString(gs.GetAllLabels(), "Person") {
			t.Fatalf("in-process: Person dropped from GetAllLabels after deleting its last node: %v", gs.GetAllLabels())
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if !containsString(gs.GetAllLabels(), "Person") {
		t.Errorf("after reopen: Person dropped from GetAllLabels: %v", gs.GetAllLabels())
	}
	persons, err := gs.FindNodesByLabelAcrossTenants("Person")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants after reopen: %v", err)
	}
	if len(persons) != 0 {
		t.Errorf("after reopen: expected 0 Person nodes, got %d", len(persons))
	}
}

// TestLabelIndex_CrashRecoveryDefaultConfig exercises the WAL-replay path under
// the DEFAULT config (edge compression on, regular WAL) rather than the
// UseDiskBackedEdges path the other crash tests use. A crash leaves no snapshot,
// so the label index is reconstructed entirely from WAL replay; the set's
// idempotent add keeps replay safe even when an id is reapplied. After recovery
// the surviving set must be correct and sorted.
func TestLabelIndex_CrashRecoveryDefaultConfig(t *testing.T) {
	dir := t.TempDir()

	var surviving []uint64
	{
		// Default-config storage that is intentionally never Close()d (crash).
		gs := testCrashableStorage(t, dir, StorageConfig{
			DataDir:               dir,
			EnableEdgeCompression: true,
		})
		var ids []uint64
		for i := 0; i < 5; i++ {
			n, err := gs.CreateNodeWithTenant("acme", []string{"Person"}, nil)
			if err != nil {
				t.Fatalf("CreateNodeWithTenant: %v", err)
			}
			ids = append(ids, n.ID)
		}
		if err := gs.DeleteNodeForTenant(ids[1], "acme"); err != nil {
			t.Fatalf("DeleteNodeForTenant: %v", err)
		}
		if err := gs.DeleteNodeForTenant(ids[3], "acme"); err != nil {
			t.Fatalf("DeleteNodeForTenant: %v", err)
		}
		surviving = []uint64{ids[0], ids[2], ids[4]}
		// No Close() — simulate crash; testCrashableStorage cleans up.
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("recover NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	globalNodes, err := gs.FindNodesByLabelAcrossTenants("Person")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants after crash: %v", err)
	}
	gGlobal := idsOfNodes(globalNodes)
	assertSameSet(t, "global survivors after crash", gGlobal, surviving)
	assertSorted(t, "global order after crash", gGlobal)

	tenantNodes := gs.GetNodesByLabelForTenant("acme", "Person")
	gTenant := idsOfNodes(tenantNodes)
	assertSameSet(t, "tenant survivors after crash", gTenant, surviving)
	assertSorted(t, "tenant order after crash", gTenant)
}

// TestTypeIndex_SortedAndCorrectAfterReopen is the edge/type sibling of the
// label test: global (FindEdgesByTypeAcrossTenants) and per-tenant (GetEdgesByTypeForTenant)
// type indexes survive reopen under the default (compression-on) config and
// return survivors in deterministic ascending order.
func TestTypeIndex_SortedAndCorrectAfterReopen(t *testing.T) {
	dir := t.TempDir()

	var surviving []uint64
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		a, err := gs.CreateNodeWithTenant("acme", []string{"Person"}, nil)
		if err != nil {
			t.Fatalf("CreateNodeWithTenant: %v", err)
		}
		b, err := gs.CreateNodeWithTenant("acme", []string{"Person"}, nil)
		if err != nil {
			t.Fatalf("CreateNodeWithTenant: %v", err)
		}

		var ids []uint64
		for i := 0; i < 6; i++ {
			e, err := gs.CreateEdgeWithTenant("acme", a.ID, b.ID, "KNOWS", nil, 1.0)
			if err != nil {
				t.Fatalf("CreateEdgeWithTenant: %v", err)
			}
			ids = append(ids, e.ID)
		}
		if err := gs.DeleteEdgeForTenant(ids[1], "acme"); err != nil {
			t.Fatalf("DeleteEdgeForTenant: %v", err)
		}
		if err := gs.DeleteEdgeForTenant(ids[3], "acme"); err != nil {
			t.Fatalf("DeleteEdgeForTenant: %v", err)
		}
		surviving = []uint64{ids[0], ids[2], ids[4], ids[5]}
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	globalEdges, err := gs.FindEdgesByTypeAcrossTenants("KNOWS")
	if err != nil {
		t.Fatalf("FindEdgesByTypeAcrossTenants after reopen: %v", err)
	}
	gGlobal := idsOfEdges(globalEdges)
	assertSameSet(t, "global type survivors after reopen", gGlobal, surviving)
	assertSorted(t, "global type order after reopen", gGlobal)

	tenantEdges := gs.GetEdgesByTypeForTenant("acme", "KNOWS")
	gTenant := idsOfEdges(tenantEdges)
	assertSameSet(t, "tenant type survivors after reopen", gTenant, surviving)
	assertSorted(t, "tenant type order after reopen", gTenant)
}
