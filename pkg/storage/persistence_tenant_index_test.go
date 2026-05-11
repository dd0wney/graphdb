package storage

import (
	"fmt"
	"testing"
)

// TestLoadFromDisk_RebuildsTenantIndex pins H4.3-followup: when a graph
// is restored from a snapshot (no WAL replay involved), the per-tenant
// label index must be rebuilt from the snapshot's flat node set. The
// snapshot struct only persists the global `nodesByLabel`, so without
// the rebuild, `GetNodesByLabelForTenant` returns nil for every loaded
// node — and the per-tenant GraphQL schema generator (which lists
// labels from `tenantNodesByLabel`) sees the tenant as labelless.
//
// Sibling test to TestReplayCreateNode_PopulatesTenantIndex: that one
// exercises the WAL-replay path (no snapshot taken), this one exercises
// the clean-shutdown path (snapshot only, no WAL replay).
func TestLoadFromDisk_RebuildsTenantIndex(t *testing.T) {
	dataDir := t.TempDir()

	// Phase 1: create tenant nodes, Close() cleanly so a snapshot is
	// taken and the WAL is truncated. Phase 2's reopen will load
	// exclusively via loadFromDisk (no replay involved).
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}

		for i := 1; i <= 3; i++ {
			_, err := gs.CreateNodeWithTenant(
				"acme",
				[]string{"Doc"},
				map[string]Value{"id": StringValue(fmt.Sprintf("doc-%d", i))},
			)
			if err != nil {
				t.Fatalf("CreateNodeWithTenant: %v", err)
			}
		}

		// Clean Close() snapshots state — that's exactly the path this
		// test wants to exercise on reopen. Contrast with
		// TestReplayCreateNode_PopulatesTenantIndex which deliberately
		// avoids Close() to force WAL-only replay.
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	// Phase 2: reopen — should restore tenant index from snapshot.
	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	docs := gs2.GetNodesByLabelForTenant("acme", "Doc")
	if len(docs) != 3 {
		t.Errorf("expected 3 acme:Doc nodes after snapshot load, got %d — tenant index not rebuilt from snapshot.Nodes", len(docs))
	}

	// Negative: queries for a different tenant must still return empty.
	// Confirms the rebuild iterates correctly and uses each node's own
	// TenantID — not just dumping everything into the default tenant.
	other := gs2.GetNodesByLabelForTenant("other-tenant", "Doc")
	if len(other) != 0 {
		t.Errorf("expected 0 Doc nodes for other-tenant, got %d — tenant scoping is broken in snapshot load", len(other))
	}

	// Per-tenant stats should also reflect the loaded nodes. addNode-
	// ToTenantIndex increments tenantStats; without the rebuild, stats
	// would be empty after a clean-restart even though nodes exist.
	stats := gs2.GetTenantStats("acme")
	if stats == nil || stats.NodeCount != 3 {
		got := uint64(0)
		if stats != nil {
			got = stats.NodeCount
		}
		t.Errorf("expected acme tenant NodeCount=3 after snapshot load, got %d", got)
	}
}
