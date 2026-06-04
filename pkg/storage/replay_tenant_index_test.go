package storage

import "testing"

// crashRecoveryConfig matches NewGraphStorage's defaults so all three phases of
// a crash-recovery test share one WAL/snapshot format (EnableEdgeCompression is
// on by default; edges stay in-memory).
func crashRecoveryConfig(dir string) StorageConfig {
	return StorageConfig{DataDir: dir, EnableEdgeCompression: true}
}

// TestReplayDeleteNode_RemovesNodeFromTenantIndex pins that a node deleted
// post-snapshot — so the delete is recovered through replayDeleteNode, not the
// live DeleteNode path — leaves the per-tenant node index/count consistent.
//
// replayDeleteNode removed the node from the shard map, the global label/
// property indexes and the global NodeCount, but never called
// removeNodeFromTenantIndex. The per-tenant index is rebuilt in loadFromDisk
// over the SNAPSHOT node set (which still contains the node) BEFORE replay, and
// nothing heals it afterward — so CountNodesForTenant over-counted the deleted
// node after every crash recovery. The replay-path sibling of the #288/#298
// gaps.
//
// Isolation (advisor): the node carries NO edges, so this fails only for the
// node-tenant-index gap, never the cascade-edge gap. Crash-recovery discipline:
// the drift is only observable through replay, so this MUST reopen (inverting
// the in-memory CC6 "do not reopen" rule, which applies when rebuild-on-load
// self-heals — here the load path itself is what drifts).
func TestReplayDeleteNode_RemovesNodeFromTenantIndex(t *testing.T) {
	dir := t.TempDir()
	const tn = "acme"

	var victim uint64

	// Phase 1: create the node, clean close -> snapshot has it (no edges).
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("phase1 NewGraphStorage: %v", err)
		}
		n, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, nil)
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		victim = n.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("phase1 Close: %v", err)
		}
	}

	// Phase 2: reopen, delete the node (WAL gets OpDeleteNode), crash (no Close,
	// so the WAL is NOT truncated and the snapshot still holds the node).
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		if err := gs.DeleteNodeForTenant(victim, tn); err != nil {
			t.Fatalf("DeleteNodeForTenant: %v", err)
		}
		// no Close — simulate crash.
	}

	// Phase 3: recover. Snapshot rebuilds the tenant index WITH the node;
	// replayDeleteNode must remove it again.
	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("recovery NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if got := gs.CountNodesForTenant(tn); got != 0 {
		t.Errorf("CountNodesForTenant = %d after crash recovery, want 0 — replayDeleteNode skipped removeNodeFromTenantIndex", got)
	}
	if got := len(gs.GetNodesByLabelForTenant(tn, "Doc")); got != 0 {
		t.Errorf("GetNodesByLabelForTenant(Doc) = %d after crash recovery, want 0 — tenant label index leaks the replayed-deleted node", got)
	}
}

// TestReplayDeleteEdge_RemovesEdgeFromTenantIndex is the standalone-edge sibling
// of the node test: a DeleteEdge recovered through replayDeleteEdge must leave
// the per-tenant edge index/count consistent. replayDeleteEdge skipped
// removeEdgeFromTenantIndex.
//
// Isolation (advisor): the edge is deleted directly (DeleteEdge), NOT by
// cascading a node delete — so this exercises replayDeleteEdge alone, never the
// cascade path. The two endpoint nodes survive, so their tenant node count is
// also asserted (must stay 2).
func TestReplayDeleteEdge_RemovesEdgeFromTenantIndex(t *testing.T) {
	dir := t.TempDir()
	const tn = "acme"

	var edgeID uint64

	// Phase 1: two nodes + one edge, clean close -> snapshot holds all three.
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("phase1 NewGraphStorage: %v", err)
		}
		a, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, nil)
		if err != nil {
			t.Fatalf("create a: %v", err)
		}
		b, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, nil)
		if err != nil {
			t.Fatalf("create b: %v", err)
		}
		e, err := gs.CreateEdgeWithTenant(tn, a.ID, b.ID, "LINKS", nil, 1.0)
		if err != nil {
			t.Fatalf("create edge: %v", err)
		}
		edgeID = e.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("phase1 Close: %v", err)
		}
	}

	// Phase 2: reopen, delete the edge directly (WAL gets OpDeleteEdge), crash.
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		if err := gs.DeleteEdge(edgeID); err != nil {
			t.Fatalf("DeleteEdge: %v", err)
		}
		// no Close — simulate crash.
	}

	// Phase 3: recover. The tenant edge index is rebuilt WITH the edge from the
	// snapshot; replayDeleteEdge must remove it again. Nodes are untouched.
	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("recovery NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if got := gs.CountEdgesForTenant(tn); got != 0 {
		t.Errorf("CountEdgesForTenant = %d after crash recovery, want 0 — replayDeleteEdge skipped removeEdgeFromTenantIndex", got)
	}
	if got := len(gs.GetEdgesByTypeForTenant(tn, "LINKS")); got != 0 {
		t.Errorf("GetEdgesByTypeForTenant(LINKS) = %d after crash recovery, want 0 — tenant type index leaks the replayed-deleted edge", got)
	}
	if got := gs.CountNodesForTenant(tn); got != 2 {
		t.Errorf("CountNodesForTenant = %d after crash recovery, want 2 — endpoint nodes must survive a standalone edge delete", got)
	}
}
