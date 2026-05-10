package storage

import (
	"fmt"
	"testing"
)

// TestReplayCreateNode_PopulatesTenantIndex pins H4.3: WAL replay must
// rebuild the per-tenant label index, not just the global one.
//
// Before the fix, replayCreateNode called gs.nodesByLabel[label] = ...
// but never gs.addNodeToTenantIndex(&node). After a crash-and-restart
// where nodes existed only in the WAL (not yet snapshotted),
// GetNodesByLabelForTenant returned nil for those nodes — the
// per-tenant GraphQL schema generator (which lists labels from
// tenantNodesByLabel) saw the tenant as labelless until the next
// write, surfacing as `Cannot query field "tasks" on type "Query"`.
func TestReplayCreateNode_PopulatesTenantIndex(t *testing.T) {
	dataDir := t.TempDir()

	var crashedStorage *GraphStorage

	// Phase 1: create tenant nodes, "crash" without snapshot so the
	// recovery path goes through replayCreateNode for every node.
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		crashedStorage = gs

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
		// Note: deliberately no Close() and no Snapshot() — that's what
		// makes this a WAL-only replay test. Without it, the snapshot
		// path would already populate tenantNodesByLabel via the live
		// in-memory state and the regression wouldn't fire.
	}

	t.Cleanup(func() {
		if crashedStorage != nil {
			_ = crashedStorage.Close()
		}
	})

	// Phase 2: reopen — exercises replayCreateNode for every node.
	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	docs := gs2.GetNodesByLabelForTenant("acme", "Doc")
	if len(docs) != 3 {
		t.Errorf("expected 3 acme:Doc nodes after replay, got %d — tenant index not populated by replayCreateNode", len(docs))
	}

	// Negative: queries for a different tenant must still return empty.
	// Confirms replayCreateNode populates the *correct* tenant slot, not
	// just the default one.
	other := gs2.GetNodesByLabelForTenant("other-tenant", "Doc")
	if len(other) != 0 {
		t.Errorf("expected 0 Doc nodes for other-tenant, got %d — tenant scoping is broken in replay", len(other))
	}

	// Sanity: global nodesByLabel was already populated correctly before
	// the H4.3 fix; this just confirms the fix didn't regress it.
	all, err := gs2.FindNodesByLabel("Doc")
	if err != nil {
		t.Fatalf("FindNodesByLabel: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 global Doc nodes after replay, got %d", len(all))
	}
}
