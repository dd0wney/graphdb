package storage

import (
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// These tests pin Track P item (3) / H2: the HNSW insert is lifted out of
// gs.mu (planned under the lock, applied after release). The behaviour they
// guard is that the lift is (a) functionally transparent — vectors stay
// searchable after create and re-index after update — (b) preserves the
// abort-on-bad-input contract before durability, and (c) race-free under
// concurrent writes to the same vector-bearing node (the reason the insert
// must read a snapshot, not the live node pointer).

func makeVecIndex(t *testing.T, gs *GraphStorage, tenant, prop string, dim int) {
	t.Helper()
	if err := gs.CreateVectorIndexForTenant(tenant, prop, dim, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
}

// TestCreateNode_VectorSearchableAfterOffLockInsert: a node created with a
// vector property is findable via VectorSearchForTenant — i.e. the off-lock
// apply step actually ran and landed the vector in the HNSW index.
func TestCreateNode_VectorSearchableAfterOffLockInsert(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	makeVecIndex(t, gs, "acme", "embedding", 4)
	vec := []float32{1, 0, 0, 0}
	node, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{
		"embedding": VectorValue(vec),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	results, err := gs.VectorSearchForTenant("acme", "embedding", vec, 1, 10)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	if len(results) != 1 || results[0].ID != node.ID {
		t.Fatalf("expected node %d as nearest neighbour, got %+v — off-lock insert did not land the vector", node.ID, results)
	}
}

// TestUpdateNode_VectorReindexedAfterOffLockInsert: updating a node's vector
// re-indexes it (remove+add), so a search for the new vector returns it.
func TestUpdateNode_VectorReindexedAfterOffLockInsert(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	makeVecIndex(t, gs, "acme", "embedding", 4)
	node, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{
		"embedding": VectorValue([]float32{1, 0, 0, 0}),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newVec := []float32{0, 0, 0, 1}
	if err := gs.UpdateNodeForTenant(node.ID, map[string]Value{"embedding": VectorValue(newVec)}, "acme"); err != nil {
		t.Fatalf("update: %v", err)
	}

	results, err := gs.VectorSearchForTenant("acme", "embedding", newVec, 1, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != node.ID {
		t.Fatalf("expected node %d for re-indexed vector, got %+v", node.ID, results)
	}
}

// TestCreateNode_BadVectorDimensionAborts: a vector whose length does not
// match the index dimension must fail the create with an error AND leave no
// node behind — the dimension check is hoisted under gs.mu so the abort
// happens before WAL durability (strictly better than the pre-H2 path, which
// stored the node first).
func TestCreateNode_BadVectorDimensionAborts(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	makeVecIndex(t, gs, "acme", "embedding", 4)
	_, err = gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{
		"embedding": VectorValue([]float32{1, 0, 0}), // dim 3, index wants 4
	})
	if err == nil {
		t.Fatalf("expected dimension-mismatch error, got nil")
	}
	if got := gs.CountNodesForTenant("acme"); got != 0 {
		t.Fatalf("expected 0 nodes after aborted create, got %d — node persisted despite bad vector", got)
	}
}

// TestUpdateNode_ConcurrentSameVectorNode_RaceClean is the load-bearing test
// for the lift: many goroutines UpdateNode the SAME vector-bearing node
// concurrently. The off-lock insert must read a snapshot captured under the
// lock, never the live node.Properties map a concurrent writer is mutating.
// Run under `go test -race` — a naive "move the UpdateNodeVectorIndexes call
// past gs.mu.Unlock" would trip the map read/write detector here.
func TestUpdateNode_ConcurrentSameVectorNode_RaceClean(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	makeVecIndex(t, gs, "acme", "embedding", 4)
	node, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{
		"embedding": VectorValue([]float32{1, 0, 0, 0}),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const goroutines = 8
	const iters = 25
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				// Distinct unit vectors per writer so the index churns.
				vec := []float32{0, 0, 0, 0}
				vec[(g+i)%4] = 1
				if err := gs.UpdateNodeForTenant(node.ID, map[string]Value{
					"embedding": VectorValue(vec),
				}, "acme"); err != nil {
					t.Errorf("concurrent update: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// The node must still be searchable (the index is consistent after the churn).
	results, err := gs.VectorSearchForTenant("acme", "embedding", []float32{1, 0, 0, 0}, 1, 10)
	if err != nil {
		t.Fatalf("search after churn: %v", err)
	}
	if len(results) != 1 || results[0].ID != node.ID {
		t.Fatalf("expected node %d searchable after concurrent updates, got %+v", node.ID, results)
	}
}
