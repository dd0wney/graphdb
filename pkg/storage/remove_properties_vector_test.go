package storage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// TestRemoveNodeProperties_DropsVectorFromIndex pins that removing a
// vector-indexed property also removes that node's vector from the HNSW index.
// RemoveNodeProperties deleted the property from the node and the property
// indexes but never re-planned the vector index, so the stale vector kept being
// returned by VectorSearch even though the property was gone.
func TestRemoveNodeProperties_DropsVectorFromIndex(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tn = "acme"
	if err := gs.CreateVectorIndexForTenant(tn, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	n, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Precondition: the vector is searchable.
	if res, err := gs.VectorSearchForTenant(tn, "embedding", []float32{1, 0, 0}, 1, 50); err != nil || len(res) != 1 {
		t.Fatalf("precondition VectorSearch = %d results, err=%v; want 1", len(res), err)
	}

	// Remove the vector-indexed property.
	if err := gs.RemoveNodePropertiesForTenant(n.ID, []string{"embedding"}, tn); err != nil {
		t.Fatalf("RemoveNodePropertiesForTenant: %v", err)
	}

	// The stale vector must be gone from the index.
	res, err := gs.VectorSearchForTenant(tn, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearch after removal: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("VectorSearch = %d results after removing the vector property, want 0 — stale vector left in the HNSW index", len(res))
	}
}

// TestRemoveNodeProperties_KeepsOtherVectorProp pins the per-key isolation the
// advisor flagged: a node with TWO vector-indexed properties must keep the
// other's vector searchable when only one is removed. A naive fix that called
// RemoveNodeFromVectorIndexes (wholesale, all the tenant's indexes) would
// wrongly drop both.
func TestRemoveNodeProperties_KeepsOtherVectorProp(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tn = "acme"
	for _, prop := range []string{"emb_a", "emb_b"} {
		if err := gs.CreateVectorIndexForTenant(tn, prop, 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndexForTenant(%s): %v", prop, err)
		}
	}
	n, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, map[string]Value{
		"emb_a": VectorValue([]float32{1, 0, 0}),
		"emb_b": VectorValue([]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Remove only emb_a.
	if err := gs.RemoveNodePropertiesForTenant(n.ID, []string{"emb_a"}, tn); err != nil {
		t.Fatalf("RemoveNodePropertiesForTenant: %v", err)
	}

	// emb_a is gone...
	if res, err := gs.VectorSearchForTenant(tn, "emb_a", []float32{1, 0, 0}, 1, 50); err != nil {
		t.Fatalf("VectorSearch emb_a: %v", err)
	} else if len(res) != 0 {
		t.Errorf("emb_a search = %d results after removal, want 0", len(res))
	}
	// ...but emb_b must still be searchable.
	if res, err := gs.VectorSearchForTenant(tn, "emb_b", []float32{0, 1, 0}, 1, 50); err != nil {
		t.Fatalf("VectorSearch emb_b: %v", err)
	} else if len(res) != 1 {
		t.Errorf("emb_b search = %d results after removing emb_a, want 1 — the surviving vector property was wrongly dropped", len(res))
	}
}
