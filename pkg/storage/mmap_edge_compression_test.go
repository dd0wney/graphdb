package storage

import "testing"

// TestEdgeCompression_ComposesWithMmapOverlay is Task 12: it settles whether
// edge compression and the mmap overlay compose in production, or whether
// compression hides mmap base adjacency after it runs.
//
// The 2026-09-07 04:38 UTC session handoff recorded an observation, not a
// finding: getEdgeIDsForNode (storage_helpers.go, around line 166) checks
// gs.compressedOutgoing / gs.compressedIncoming FIRST when useEdgeCompression
// is set, and returns that entry without ever reaching the overlay-plus-
// mmap-base union below it (storage_helpers.go, around line 194).
// compressAllEdgeLists (storage_helpers.go, around line 122) compresses only
// gs.outgoingEdges / gs.incomingEdges — on an mmap-backed store those hold
// the post-open OVERLAY only, never the base CSR held in gs.mmapSnap. So a
// node with base-CSR edges that also gains an overlay edge, followed by a
// compression run (snapshotWithBoundary, persistence.go, called by both
// Snapshot() and CompactWAL()), risks losing its base edges from every
// future getEdgeIDsForNode read.
//
// This is not a corner case: DefaultStorageConfig turns both features on
// (EnableEdgeCompression: true, UseMmapSnapshot: true — storage.go lines 36
// and 47), so mmapConfig (which starts from DefaultStorageConfig) already
// exercises the default deployment shape.
func TestEdgeCompression_ComposesWithMmapOverlay(t *testing.T) {
	dir := t.TempDir()

	// Step 1: build a store with mmap snapshots and edge compression, both
	// on by default. Create three nodes and one base edge, then close.
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	if !gs.useEdgeCompression {
		t.Fatalf("test setup: useEdgeCompression = false, want true (DefaultStorageConfig enables it)")
	}

	nodeA, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(A): %v", err)
	}
	nodeB, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(B): %v", err)
	}
	nodeC, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(C): %v", err)
	}

	baseEdge, err := gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge(A->B), base edge: %v", err)
	}

	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Step 2: reopen, and assert the store actually took the mmap path (the
	// #474 pattern: gs.mmapSnap is non-nil only when it did).
	reopened, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if reopened.mmapSnap == nil {
		t.Fatalf("reopen did not take the mmap path (mmapSnap is nil); this test proves nothing without it")
	}

	// Step 4: add one overlay edge from nodeA, which already has baseEdge in
	// the mmap CSR.
	overlayEdge, err := reopened.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge(A->C), overlay edge: %v", err)
	}
	want := []uint64{baseEdge.ID, overlayEdge.ID}

	// Step 3 (before): read before the compression trigger, to isolate what
	// the trigger changes. Both the base edge and the overlay edge must be
	// visible through the internal helper and the public tenant-scoped API.
	reopened.mu.RLock()
	beforeInternal := append([]uint64(nil), reopened.getEdgeIDsForNode(nodeA.ID, true)...)
	reopened.mu.RUnlock()
	assertUint64SetEqual(t, "before compaction: getEdgeIDsForNode(A, outgoing)", beforeInternal, want)

	beforePublic, err := reopened.GetOutgoingEdgesForTenant(nodeA.ID, DefaultTenantID)
	if err != nil {
		t.Fatalf("GetOutgoingEdgesForTenant(A) before compaction: %v", err)
	}
	assertUint64SetEqual(t, "before compaction: GetOutgoingEdgesForTenant(A)", edgeIDList(beforePublic), want)

	// Step 3 (trigger): compress the way production compresses. Snapshot
	// calls snapshotWithBoundary, which compresses gs.outgoingEdges /
	// gs.incomingEdges into gs.compressedOutgoing / gs.compressedIncoming
	// and clears the overlay maps, whenever useEdgeCompression is set.
	if err := reopened.Snapshot(); err != nil {
		t.Fatalf("Snapshot (compression trigger): %v", err)
	}

	// Step 5: read again. If compression and the mmap overlay compose, this
	// returns the same set as the before-read.
	reopened.mu.RLock()
	afterInternal := append([]uint64(nil), reopened.getEdgeIDsForNode(nodeA.ID, true)...)
	reopened.mu.RUnlock()
	assertUint64SetEqual(t, "after compaction: getEdgeIDsForNode(A, outgoing)", afterInternal, want)

	afterPublic, err := reopened.GetOutgoingEdgesForTenant(nodeA.ID, DefaultTenantID)
	if err != nil {
		t.Fatalf("GetOutgoingEdgesForTenant(A) after compaction: %v", err)
	}
	assertUint64SetEqual(t, "after compaction: GetOutgoingEdgesForTenant(A)", edgeIDList(afterPublic), want)

	// Step 6: CheckInvariants must find no violation.
	violations, err := CheckInvariants(reopened)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("CheckInvariants violations after compaction: %v", violations)
	}
}

// edgeIDList extracts IDs from a slice of edges, for comparison against a
// plain []uint64 want-set.
func edgeIDList(edges []*Edge) []uint64 {
	ids := make([]uint64, len(edges))
	for i, e := range edges {
		ids[i] = e.ID
	}
	return ids
}

// assertUint64SetEqual fails the test naming label when got and want do not
// hold the same uint64 values. Order-independent: adjacency lists make no
// ordering promise.
func assertUint64SetEqual(t *testing.T, label string, got, want []uint64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: got %v (len %d), want %v (len %d)", label, got, len(got), want, len(want))
		return
	}
	seen := make(map[uint64]int, len(want))
	for _, id := range want {
		seen[id]++
	}
	for _, id := range got {
		seen[id]--
	}
	for id, count := range seen {
		if count != 0 {
			t.Errorf("%s: got %v, want %v (mismatch at id %d)", label, got, want, id)
			return
		}
	}
}

// TestCompressEdgeLists_RefusesOnMmapBackedStore is Fix round 1, step 6: the
// public manual entry point must refuse loudly on an mmap-backed store,
// rather than silently no-op into the corruption
// TestEdgeCompression_ComposesWithMmapOverlay proved above. Before the
// guard, CompressEdgeLists returned nil here and still compressed the
// overlay.
func TestCompressEdgeLists_RefusesOnMmapBackedStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	nodeA, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(A): %v", err)
	}
	nodeB, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(B): %v", err)
	}
	if _, err := gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0); err != nil {
		t.Fatalf("CreateEdge(A->B): %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reopened.mmapSnap == nil {
		t.Fatalf("reopen did not take the mmap path (mmapSnap is nil); test cannot proceed")
	}

	if err := reopened.CompressEdgeLists(); err == nil {
		t.Errorf("CompressEdgeLists on an mmap-backed store: got nil error, want a refusal")
	}
}

// TestCompressEdgeLists_SucceedsOnJSONBackedStore is the negative control
// for the guard above: a JSON-backed store with edge compression on must
// still compress successfully, and the compressed edge must still read
// back correctly.
func TestCompressEdgeLists_SucceedsOnJSONBackedStore(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(jsonConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	if !gs.useEdgeCompression {
		t.Fatalf("test setup: useEdgeCompression = false, want true")
	}

	nodeA, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(A): %v", err)
	}
	nodeB, err := gs.CreateNode([]string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("CreateNode(B): %v", err)
	}
	edge, err := gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge(A->B): %v", err)
	}

	if err := gs.CompressEdgeLists(); err != nil {
		t.Fatalf("CompressEdgeLists on a JSON-backed store: %v", err)
	}

	gs.mu.RLock()
	ids := append([]uint64(nil), gs.getEdgeIDsForNode(nodeA.ID, true)...)
	gs.mu.RUnlock()
	assertUint64SetEqual(t, "after CompressEdgeLists: getEdgeIDsForNode(A, outgoing)", ids, []uint64{edge.ID})
}
