package storage

import "testing"

// TestReplayUpdateEdge_SurvivesCrashRecovery pins that an edge property/weight
// update made AFTER the last snapshot survives a crash. UpdateEdge writes
// wal.OpUpdateEdge, but replayEntry had no case for it, so a post-snapshot edge
// update was silently lost on recovery (the edge reverted to its snapshot
// state). The replay sibling of replayUpdateNode.
//
// Must reopen (the drift is only observable through WAL replay): session 1
// snapshots the edge, session 2 updates it WAL-only then crashes, session 3
// recovers and must see the updated properties + weight.
func TestReplayUpdateEdge_SurvivesCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	const tenant = "acme"
	var edgeID uint64

	// Session 1: two nodes + an edge (weight 1.0, prop v1), clean close → the
	// snapshot captures the edge in its pre-update state.
	{
		gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		a, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
		if err != nil {
			t.Fatalf("create a: %v", err)
		}
		b, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
		if err != nil {
			t.Fatalf("create b: %v", err)
		}
		e, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "LINK",
			map[string]Value{"k": StringValue("v1")}, 1.0)
		if err != nil {
			t.Fatalf("create edge: %v", err)
		}
		edgeID = e.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	// Session 2: update the edge (prop v2, weight 2.0) — WAL-only (OpUpdateEdge) —
	// then crash (no Close).
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		newWeight := 2.0
		if err := gs.UpdateEdgeForTenant(edgeID, map[string]Value{"k": StringValue("v2")}, &newWeight, tenant); err != nil {
			t.Fatalf("session2 update edge: %v", err)
		}
		// no Close — simulate crash.
	}

	// Session 3: recovery. The update must be replayed, not reverted to snapshot.
	gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	e, err := gs.GetEdgeForTenant(edgeID, tenant)
	if err != nil {
		t.Fatalf("get edge after recovery: %v", err)
	}
	got, _ := e.Properties["k"].AsString()
	if got != "v2" {
		t.Errorf("edge property k = %q after crash recovery, want \"v2\" — OpUpdateEdge not replayed", got)
	}
	if e.Weight != 2.0 {
		t.Errorf("edge weight = %v after crash recovery, want 2.0 — OpUpdateEdge not replayed", e.Weight)
	}
}
