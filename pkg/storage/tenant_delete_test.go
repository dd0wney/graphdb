package storage

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// seedTenant creates two connected nodes (one carrying a vector) + an edge for a
// tenant that has a vector index on "embedding". Returns nothing — callers
// assert via the count/index APIs.
func seedTenant(t *testing.T, gs *GraphStorage, tenant string) {
	t.Helper()
	if err := gs.CreateVectorIndexForTenant(tenant, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("%s: create vector index: %v", tenant, err)
	}
	a, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("%s: create node a: %v", tenant, err)
	}
	b, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
	if err != nil {
		t.Fatalf("%s: create node b: %v", tenant, err)
	}
	if _, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "LINK", nil, 1.0); err != nil {
		t.Fatalf("%s: create edge: %v", tenant, err)
	}
}

// TestDeleteTenant_CascadeSurvivesReopen is the core #223 teeth: deleting a
// tenant removes all its nodes/edges/vector-index, the removal survives a crash
// (WAL replay), and a bystander tenant is fully untouched (security boundary).
func TestDeleteTenant_CascadeSurvivesReopen(t *testing.T) {
	dir := t.TempDir()

	// Session A: seed two tenants, delete one, then crash (no Close).
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		seedTenant(t, gs, "victim")
		seedTenant(t, gs, "bystander")

		n, e, err := gs.DeleteTenant("victim")
		if err != nil {
			t.Fatalf("DeleteTenant: %v", err)
		}
		if n != 2 {
			t.Errorf("nodesDeleted = %d, want 2", n)
		}
		// 1 edge; whether it's counted by the node-cascade or the defensive
		// sweep, the tenant must end with zero edges (asserted below).
		_ = e

		// In-memory: victim fully gone.
		if c := gs.CountNodesForTenant("victim"); c != 0 {
			t.Errorf("victim node count = %d, want 0", c)
		}
		if c := gs.CountEdgesForTenant("victim"); c != 0 {
			t.Errorf("victim edge count = %d, want 0", c)
		}
		if gs.HasVectorIndexForTenant("victim", "embedding") {
			t.Errorf("victim vector index survived delete")
		}
		// Bystander untouched.
		if c := gs.CountNodesForTenant("bystander"); c != 2 {
			t.Errorf("bystander node count = %d, want 2 (cross-tenant blast radius)", c)
		}
		if c := gs.CountEdgesForTenant("bystander"); c != 1 {
			t.Errorf("bystander edge count = %d, want 1", c)
		}
		if !gs.HasVectorIndexForTenant("bystander", "embedding") {
			t.Errorf("bystander vector index wrongly dropped")
		}
		// no Close — simulate crash.
	}

	// Session B: recover from the WAL and assert the delete is durable.
	gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if c := gs.CountNodesForTenant("victim"); c != 0 {
		t.Errorf("after reopen: victim node count = %d, want 0 — delete not WAL-durable", c)
	}
	if c := gs.CountEdgesForTenant("victim"); c != 0 {
		t.Errorf("after reopen: victim edge count = %d, want 0", c)
	}
	if gs.HasVectorIndexForTenant("victim", "embedding") {
		t.Errorf("after reopen: victim vector index resurrected — drop not WAL-durable")
	}
	// Bystander still fully intact after recovery.
	if c := gs.CountNodesForTenant("bystander"); c != 2 {
		t.Errorf("after reopen: bystander node count = %d, want 2", c)
	}
	if c := gs.CountEdgesForTenant("bystander"); c != 1 {
		t.Errorf("after reopen: bystander edge count = %d, want 1", c)
	}
	if !gs.HasVectorIndexForTenant("bystander", "embedding") {
		t.Errorf("after reopen: bystander vector index lost")
	}
}

func TestDeleteTenant_RefusesDefault(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	for _, tid := range []string{"default", ""} {
		if _, _, err := gs.DeleteTenant(tid); err == nil {
			t.Errorf("DeleteTenant(%q) = nil error, want refusal", tid)
		}
	}
}

func TestDeleteTenant_Idempotent(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	seedTenant(t, gs, "t1")
	if _, _, err := gs.DeleteTenant("t1"); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	// Second delete on an already-emptied tenant is a clean no-op.
	n, e, err := gs.DeleteTenant("t1")
	if err != nil {
		t.Errorf("second delete: %v, want nil", err)
	}
	if n != 0 || e != 0 {
		t.Errorf("second delete removed (%d nodes, %d edges), want (0, 0)", n, e)
	}
}
