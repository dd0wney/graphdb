package storage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// TestVectorSearchForTenant_KnownAnswerOrdering closes the storage half of
// Track Q / Q1: the existing storage vector tests pin k=1 identity and k>1
// *count*, but not k>1 nearest-neighbour *ordering*. A ranking regression that
// returns the right count of the wrong nodes — or the correct nodes in the
// wrong order — slips through a count test. This asserts the k=2 result is the
// two actually-nearest nodes, the exact match first, distances ascending.
//
// Well-separated planted clusters → unambiguous known answer (not the
// synthetic-uniform concentration regime; see memory
// reference_hnsw_construction_cost_data_dependent).
// CONSUMER CONTRACT: CC2-vector-nn-identity — understand-graphdb (#283)
func TestVectorSearchForTenant_KnownAnswerOrdering(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndexForTenant("acme", "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}

	mk := func(vec []float32) uint64 {
		t.Helper()
		n, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{"embedding": VectorValue(vec)})
		if err != nil {
			t.Fatalf("CreateNodeWithTenant: %v", err)
		}
		return n.ID
	}
	exact := mk([]float32{1, 0, 0})    // exact match for the query
	near := mk([]float32{0.9, 0.1, 0}) // second-nearest
	_ = mk([]float32{0, 1, 0})         // far
	_ = mk([]float32{0, 0, 1})         // far

	results, err := gs.VectorSearchForTenant("acme", "embedding", []float32{1, 0, 0}, 2, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].ID != exact {
		t.Errorf("nearest result ID=%d, want exact match %d (ranking regression)", results[0].ID, exact)
	}
	if results[1].ID != near {
		t.Errorf("second result ID=%d, want %d — wrong nodes or wrong order", results[1].ID, near)
	}
	if results[0].Distance > results[1].Distance {
		t.Errorf("results not ascending by distance: %v > %v", results[0].Distance, results[1].Distance)
	}
}
