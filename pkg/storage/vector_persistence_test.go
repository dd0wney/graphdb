package storage

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// TestVectorIndex_SurvivesReopen_MultiTenant pins that the HNSW vector index
// survives a clean Close()->reopen — both the index DEFINITIONS and the vectors,
// per tenant. Pre-fix the index was neither serialized nor rebuilt on load, so
// VectorSearchForTenant returned ErrNodeNotFound (no index) after any restart.
// Two tenants verify the rebuild routes by node.TenantID and keeps them isolated.
func TestVectorIndex_SurvivesReopen_MultiTenant(t *testing.T) {
	dir := t.TempDir()
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("NewGraphStorage: %v", err)
		}
		for _, tn := range []string{"acme", "globex"} {
			if err := gs.CreateVectorIndexForTenant(tn, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
				t.Fatalf("CreateVectorIndexForTenant(%s): %v", tn, err)
			}
		}
		// acme's node points +X; globex's points +Y.
		if _, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})}); err != nil {
			t.Fatalf("create acme node: %v", err)
		}
		if _, err := gs.CreateNodeWithTenant("globex", []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{0, 1, 0})}); err != nil {
			t.Fatalf("create globex node: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs.Close() }()

	acme, err := gs.VectorSearchForTenant("acme", "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("search acme after reopen: %v — vector index not rebuilt", err)
	}
	if len(acme) != 1 {
		t.Errorf("acme search = %d results after reopen, want 1 (vectors lost on restart)", len(acme))
	}
	globex, err := gs.VectorSearchForTenant("globex", "embedding", []float32{0, 1, 0}, 1, 50)
	if err != nil {
		t.Fatalf("search globex after reopen: %v", err)
	}
	if len(globex) != 1 {
		t.Errorf("globex search = %d results after reopen, want 1", len(globex))
	}
	// Isolation: acme and globex must have found DIFFERENT nodes.
	if len(acme) == 1 && len(globex) == 1 && acme[0].ID == globex[0].ID {
		t.Errorf("cross-tenant: acme and globex resolved to the same node %d after rebuild", acme[0].ID)
	}
}

// TestVectorIndex_SurvivesCrashRecovery_PostSnapshotWrite is the case that a
// clean-reopen test misses: a node created AFTER the last snapshot exists only
// in the WAL, not snapshot.Nodes. The rebuild MUST run over the final
// post-replay node set, not the snapshot, or post-checkpoint vectors stay lost.
func TestVectorIndex_SurvivesCrashRecovery_PostSnapshotWrite(t *testing.T) {
	dir := t.TempDir()

	// Session 1: index + node1, clean close (snapshot has the def + node1).
	var id1 uint64
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("session1 NewGraphStorage: %v", err)
		}
		if err := gs.CreateVectorIndexForTenant("acme", "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndexForTenant: %v", err)
		}
		n1, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
		if err != nil {
			t.Fatalf("create node1: %v", err)
		}
		id1 = n1.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 Close: %v", err)
		}
	}

	// Session 2: reopen, create node2 (WAL-only, NOT snapshotted), then crash
	// (no Close). The default WAL fsyncs per write, so node2 is durable.
	var id2 uint64
	{
		gs := testCrashableStorage(t, dir, StorageConfig{DataDir: dir, EnableEdgeCompression: true})
		n2, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{0, 1, 0})})
		if err != nil {
			t.Fatalf("create node2: %v", err)
		}
		id2 = n2.ID
		// no Close — simulate crash.
	}

	// Session 3: recovery — snapshot (def + node1) + WAL replay (node2). The
	// post-replay rebuild must index BOTH.
	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("recovery NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	r1, err := gs.VectorSearchForTenant("acme", "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil || len(r1) != 1 || r1[0].ID != id1 {
		t.Errorf("snapshot node1 not searchable after crash recovery: results=%v err=%v", r1, err)
	}
	r2, err := gs.VectorSearchForTenant("acme", "embedding", []float32{0, 1, 0}, 1, 50)
	if err != nil || len(r2) != 1 || r2[0].ID != id2 {
		t.Errorf("WAL-only node2 not searchable after crash recovery: results=%v err=%v — rebuild ran over snapshot.Nodes, not the final post-replay set", r2, err)
	}
}
