package storage

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// TestBatchDeleteNode_RemovesNodeVectors pins that deleting a node through the
// batch path removes its vectors from the HNSW index — parity with the direct
// DeleteNode (which calls RemoveNodeFromVectorIndexes). executeDeleteNode did
// all the index cleanup #304 added but never removed the node's vectors, so a
// batch-deleted node kept being returned by VectorSearch. A second node stays
// to prove the batch removes only the deleted node's vector.
func TestBatchDeleteNode_RemovesNodeVectors(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}

	// Two nodes: victim points +X, keeper points +Y.
	victim, err := gs.CreateNode([]string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("create victim: %v", err)
	}
	keeper, err := gs.CreateNode([]string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{0, 1, 0})})
	if err != nil {
		t.Fatalf("create keeper: %v", err)
	}

	if res, err := gs.VectorSearch("embedding", []float32{1, 0, 0}, 1, 50); err != nil || len(res) != 1 {
		t.Fatalf("precondition victim search = %d results, err=%v; want 1", len(res), err)
	}

	// Batch-delete the victim.
	b := gs.BeginBatch()
	b.DeleteNode(victim.ID)
	if err := b.Commit(); err != nil {
		t.Fatalf("batch Commit: %v", err)
	}

	// The victim's vector must be gone — searching its exact vector must not
	// return the deleted node.
	res, err := gs.VectorSearch("embedding", []float32{1, 0, 0}, 2, 50)
	if err != nil {
		t.Fatalf("VectorSearch after batch delete: %v", err)
	}
	for _, n := range res {
		if n.ID == victim.ID {
			t.Errorf("VectorSearch returned batch-deleted node %d — its vector was left in the HNSW index", victim.ID)
		}
	}

	// The keeper's vector must still be searchable.
	kres, err := gs.VectorSearch("embedding", []float32{0, 1, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearch keeper: %v", err)
	}
	if len(kres) != 1 || kres[0].ID != keeper.ID {
		t.Errorf("keeper search = %v after batch delete, want exactly node %d", kres, keeper.ID)
	}

	assertGraphInvariants(t, gs)
}
