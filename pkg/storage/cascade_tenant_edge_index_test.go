package storage

import "testing"

// TestDeleteNode_CascadeRemovesEdgeFromTenantIndex pins that deleting a node
// cascades its edges OUT of the per-tenant edge index — not just the global
// type index + global EdgeCount. The cascade helpers (cascadeDeleteOutgoingEdge
// / cascadeDeleteIncomingEdge) historically skipped removeEdgeFromTenantIndex,
// so CountEdgesForTenant / GetEdgesByTypeForTenant over-counted cascaded edges
// in NORMAL operation (the delete-side, cascade-path sibling of the #288/#298
// tenant-index gaps).
//
// CC6 test discipline (memory feedback_in_memory_index_drift_test_design): this
// is in-memory drift that a reopen would self-heal by rebuilding the tenant
// index from the flat edge set — so assert the live COUNT and do NOT reopen, or
// the bug hides behind rebuild-on-load.
func TestDeleteNode_CascadeRemovesEdgeFromTenantIndex(t *testing.T) {
	const tn = "acme"

	tests := []struct {
		name string
		// deleteFrom: delete the edge's source node (exercises the outgoing
		// cascade on the source + the incoming cascade is irrelevant). When
		// false, delete the target node (exercises the incoming cascade).
		deleteFrom bool
	}{
		{name: "delete source node (outgoing cascade)", deleteFrom: true},
		{name: "delete target node (incoming cascade)", deleteFrom: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs, err := NewGraphStorage(t.TempDir())
			if err != nil {
				t.Fatalf("NewGraphStorage: %v", err)
			}
			defer func() { _ = gs.Close() }()

			src, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, nil)
			if err != nil {
				t.Fatalf("create src: %v", err)
			}
			dst, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, nil)
			if err != nil {
				t.Fatalf("create dst: %v", err)
			}
			if _, err := gs.CreateEdgeWithTenant(tn, src.ID, dst.ID, "LINKS", nil, 1.0); err != nil {
				t.Fatalf("create edge: %v", err)
			}

			if got := gs.CountEdgesForTenant(tn); got != 1 {
				t.Fatalf("precondition: CountEdgesForTenant = %d, want 1", got)
			}
			if got := len(gs.GetEdgesByTypeForTenant(tn, "LINKS")); got != 1 {
				t.Fatalf("precondition: GetEdgesByTypeForTenant(LINKS) = %d, want 1", got)
			}

			victim := dst.ID
			if tt.deleteFrom {
				victim = src.ID
			}
			if err := gs.DeleteNodeForTenant(victim, tn); err != nil {
				t.Fatalf("DeleteNodeForTenant: %v", err)
			}

			// The cascaded edge must leave the per-tenant edge index entirely.
			if got := gs.CountEdgesForTenant(tn); got != 0 {
				t.Errorf("CountEdgesForTenant = %d after cascade delete, want 0 — cascade skipped removeEdgeFromTenantIndex", got)
			}
			if got := len(gs.GetEdgesByTypeForTenant(tn, "LINKS")); got != 0 {
				t.Errorf("GetEdgesByTypeForTenant(LINKS) = %d after cascade delete, want 0 — enumeration set leaks the cascaded edge", got)
			}

			assertGraphInvariants(t, gs)
		})
	}
}
