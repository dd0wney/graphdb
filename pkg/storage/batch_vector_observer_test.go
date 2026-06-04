package storage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// (recordingObserver lives in observation_test.go)

// TestBatchCreate_IndexesVectors pins that a node created via the batch path is
// inserted into the HNSW vector index (parity with CreateNodeWithTenant /
// Transaction.Commit). The batch path stamps DefaultTenantID, so the index +
// search use tenant "" (-> default). G1: pre-fix executeCreateNode never plans/
// applies vectors, so the node is silently unsearchable.
func TestBatchCreate_IndexesVectors(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndexForTenant(DefaultTenantID, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}

	b := gs.BeginBatch()
	id, err := b.AddNode([]string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := gs.VectorSearchForTenant(DefaultTenantID, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d vector results, want 1 — batch-created node not indexed into HNSW (G1)", len(results))
	}
	if results[0].ID != id {
		t.Errorf("nearest ID=%d, want batch-created node %d", results[0].ID, id)
	}
}

// TestBatchCreate_IndexesVectors_BulkImportMode is G1 under the exact config
// cmd/import-icij uses (BulkImportMode: true → WAL disabled). The vector/observer
// logic is WAL-independent (only the appendToWAL guard checks hasWAL), so the
// fix must hold here too — this is the real consumer seam for the G1 bug.
func TestBatchCreate_IndexesVectors_BulkImportMode(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(StorageConfig{DataDir: t.TempDir(), BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndexForTenant(DefaultTenantID, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	b := gs.BeginBatch()
	id, err := b.AddNode([]string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := gs.VectorSearchForTenant(DefaultTenantID, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	if len(results) != 1 || results[0].ID != id {
		t.Errorf("got %d results (want node %d) — batch vector indexing broken under BulkImportMode (G1)", len(results), id)
	}
}

// TestBatchUpdate_ReindexesVectors pins that a batch UpdateNode re-indexes the
// node's vector when a vector property changes (parity with UpdateNode). The
// node is created via the canonical path (so it IS indexed), then batch-updated
// to a vector matching the query. G2: pre-fix executeUpdateNode never re-plans
// vectors, so the stale vector stays in the index and the query distance is ~1.
func TestBatchUpdate_ReindexesVectors(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndexForTenant(DefaultTenantID, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	// Canonical create with a vector ORTHOGONAL to the eventual query.
	n, err := gs.CreateNodeWithTenant(DefaultTenantID, []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{0, 1, 0})})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	// Batch-update the vector to MATCH the query [1,0,0].
	b := gs.BeginBatch()
	b.UpdateNode(n.ID, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := gs.VectorSearchForTenant(DefaultTenantID, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	if len(results) != 1 || results[0].ID != n.ID {
		t.Fatalf("got %d results, want node %d", len(results), n.ID)
	}
	// Post-fix the index holds [1,0,0] -> distance ~0; pre-fix it holds the
	// stale [0,1,0] -> cosine distance ~1.
	if results[0].Distance > 0.01 {
		t.Errorf("distance %.4f after batch vector update, want ~0 — stale vector not re-indexed (G2)", results[0].Distance)
	}
}

// TestBatchCommit_DispatchesObservers pins that batch create/update/delete fire
// node observers (parity with the direct paths + Transaction.Commit). G4:
// pre-fix none of the executeX methods dispatch, so auto-embed / event hooks
// silently miss every batch write.
func TestBatchCommit_DispatchesObservers(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	obs := &recordingObserver{}
	gs.AddObserver(obs)

	bc := gs.BeginBatch()
	id, err := bc.AddNode([]string{"Doc"}, map[string]Value{"k": StringValue("v")})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := bc.Commit(); err != nil {
		t.Fatalf("Commit(create): %v", err)
	}

	bu := gs.BeginBatch()
	bu.UpdateNode(id, map[string]Value{"k": StringValue("v2")})
	if err := bu.Commit(); err != nil {
		t.Fatalf("Commit(update): %v", err)
	}

	bd := gs.BeginBatch()
	bd.DeleteNode(id)
	if err := bd.Commit(); err != nil {
		t.Fatalf("Commit(delete): %v", err)
	}

	if len(obs.created) == 0 {
		t.Errorf("OnNodeCreated never fired on batch create (G4)")
	}
	if len(obs.updated) == 0 {
		t.Errorf("OnNodeUpdated never fired on batch update (G4)")
	}
	if len(obs.deleted) == 0 {
		t.Errorf("OnNodeDeleted never fired on batch delete (G4)")
	}
}

// TestBatchDeleteNode_CascadeCleansEdgeIndexes pins that a batch node delete
// also removes its cascaded edges from the global type index AND the opposite
// endpoint's adjacency list (parity with DeleteEdge). G3: pre-fix the cascade
// only deleted the edge shard entry + per-tenant index, leaving stale IDs in
// edgesByType and the surviving node's incoming adjacency. Public reads
// self-heal (lookupEdgeShard skips missing IDs), so this asserts INTERNAL state
// (counts, not list membership — the CC6 discipline).
func TestBatchDeleteNode_CascadeCleansEdgeIndexes(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	b := gs.BeginBatch()
	a, _ := b.AddNode([]string{"Entity"}, nil)
	bID, _ := b.AddNode([]string{"Entity"}, nil)
	edgeID, err := b.AddEdge(a, bID, "linked_to", nil, 1.0)
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit(create): %v", err)
	}

	d := gs.BeginBatch()
	d.DeleteNode(a) // cascades the a->b edge
	if err := d.Commit(); err != nil {
		t.Fatalf("Commit(delete): %v", err)
	}

	// White-box: the dead edge must be gone from the global type index...
	if _, stale := gs.edgesByType["linked_to"][edgeID]; stale {
		t.Errorf("cascaded edge %d still in global edgesByType after batch node delete (G3)", edgeID)
	}
	// ...and from the surviving endpoint's incoming adjacency.
	for _, id := range gs.incomingEdges[bID] {
		if id == edgeID {
			t.Errorf("cascaded edge %d still in incomingEdges[%d] after batch node delete (G3)", edgeID, bID)
		}
	}
}
