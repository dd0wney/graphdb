package storage

import (
	"fmt"
	"testing"
)

// edgeIndexFixture creates two acme nodes plus `count` LINKS edges between
// them in dataDir and returns the storage handle. Shared by the snapshot
// and replay tests below.
func edgeIndexFixture(t *testing.T, gs *GraphStorage, count int) {
	t.Helper()

	a, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("CreateNodeWithTenant(a): %v", err)
	}
	b, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("CreateNodeWithTenant(b): %v", err)
	}
	for i := 0; i < count; i++ {
		if _, err := gs.CreateEdgeWithTenant(
			"acme", a.ID, b.ID, "LINKS",
			map[string]Value{"id": StringValue(fmt.Sprintf("e-%d", i))}, 1.0,
		); err != nil {
			t.Fatalf("CreateEdgeWithTenant: %v", err)
		}
	}
}

// TestLoadFromDisk_RebuildsTenantEdgeIndex is the edge sibling of
// TestLoadFromDisk_RebuildsTenantIndex. The node tenant index is rebuilt
// from the snapshot's flat node set (persistence.go), but the edge tenant
// index (tenantEdgesByType) was never rebuilt on load. Result:
// GetEdgesByTypeForTenant returned nil for every edge after a clean
// restart — the per-tenant GraphQL edge schema saw the tenant as
// edge-typeless until the next write reseeded the index.
func TestLoadFromDisk_RebuildsTenantEdgeIndex(t *testing.T) {
	dataDir := t.TempDir()

	// Phase 1: clean Close() so a snapshot is taken and the WAL truncated.
	// Phase 2's reopen loads exclusively via loadFromDisk (no replay).
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		edgeIndexFixture(t, gs, 3)
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	links := gs2.GetEdgesByTypeForTenant("acme", "LINKS")
	if len(links) != 3 {
		t.Errorf("expected 3 acme:LINKS edges after snapshot load, got %d — tenant edge index not rebuilt from snapshot.Edges", len(links))
	}

	// Negative: a different tenant must still see no edges. Confirms the
	// rebuild uses each edge's own TenantID, not the default slot.
	other := gs2.GetEdgesByTypeForTenant("other-tenant", "LINKS")
	if len(other) != 0 {
		t.Errorf("expected 0 LINKS edges for other-tenant, got %d — tenant scoping broken in snapshot load", len(other))
	}

	// Per-tenant edge stats must also reflect the loaded edges.
	stats := gs2.GetTenantStats("acme")
	if stats == nil || stats.EdgeCount != 3 {
		got := uint64(0)
		if stats != nil {
			got = stats.EdgeCount
		}
		t.Errorf("expected acme tenant EdgeCount=3 after snapshot load, got %d", got)
	}
}

// TestReplayCreateEdge_PopulatesTenantEdgeIndex is the edge sibling of
// TestReplayCreateNode_PopulatesTenantIndex. WAL replay rebuilt the global
// edgesByType but never the per-tenant tenantEdgesByType, so a
// crash-and-restart where edges existed only in the WAL left
// GetEdgesByTypeForTenant returning nil.
func TestReplayCreateEdge_PopulatesTenantEdgeIndex(t *testing.T) {
	dataDir := t.TempDir()

	var crashedStorage *GraphStorage

	// Phase 1: create edges, "crash" without snapshot so recovery goes
	// through replayCreateEdge for every edge.
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		crashedStorage = gs
		edgeIndexFixture(t, gs, 3)
		// Deliberately no Close()/Snapshot() — forces WAL-only replay.
	}

	t.Cleanup(func() {
		if crashedStorage != nil {
			_ = crashedStorage.Close()
		}
	})

	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	links := gs2.GetEdgesByTypeForTenant("acme", "LINKS")
	if len(links) != 3 {
		t.Errorf("expected 3 acme:LINKS edges after replay, got %d — tenant edge index not populated by replayCreateEdge", len(links))
	}

	other := gs2.GetEdgesByTypeForTenant("other-tenant", "LINKS")
	if len(other) != 0 {
		t.Errorf("expected 0 LINKS edges for other-tenant, got %d — tenant scoping broken in replay", len(other))
	}
}
