package storage

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// seedForClear puts something into every structure DeleteAllNodes must empty:
// the global and per-tenant counters, the per-tenant ID sets, adjacency, and a
// vector index with vectors in it.
func seedForClear(t *testing.T, gs *GraphStorage) {
	t.Helper()
	if err := gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}
	var ids []uint64
	for i := range 5 {
		n, err := gs.CreateNode([]string{"Doc"}, map[string]Value{
			"embedding": VectorValue([]float32{float32(i + 1), 1, 0}),
		})
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		ids = append(ids, n.ID)
	}
	for i := 0; i+1 < len(ids); i++ {
		if _, err := gs.CreateEdge(ids[i], ids[i+1], "NEXT", nil, 1); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}
	// clearAll walks every tenant, so one index must live outside the default.
	if err := gs.CreateVectorIndexForTenant("tenant-a", "face", 2, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	a, err := gs.CreateNodeWithTenant("tenant-a", []string{"Person"}, map[string]Value{
		"face": VectorValue([]float32{1, 0}),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}
	b, err := gs.CreateNodeWithTenant("tenant-a", []string{"Person"}, nil)
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant("tenant-a", a.ID, b.ID, "KNOWS", nil, 1); err != nil {
		t.Fatalf("CreateEdgeWithTenant: %v", err)
	}
}

func assertNoViolations(t *testing.T, gs *GraphStorage, when string) {
	t.Helper()
	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants %s: %v", when, err)
	}
	for _, v := range violations {
		t.Errorf("invariant violation %s: %s", when, v)
	}
}

// DeleteAllNodes must leave a store that is indistinguishable from an empty
// one: counters, per-tenant ID sets and the vector index included, not only the
// shards. A counter left behind is a silent wrong answer from GetStatistics,
// and a stale tenant ID set or HNSW entry outlives the node it names.
func TestDeleteAllNodes_ResetsDerivedState(t *testing.T) {
	open := map[string]func(t *testing.T) *GraphStorage{
		"json": func(t *testing.T) *GraphStorage {
			gs, err := NewGraphStorageWithConfig(jsonConfig(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			seedForClear(t, gs)
			return gs
		},
		// Reopened, so the data sits in the mmap base, not the shard overlay.
		"mmap-reopened": func(t *testing.T) *GraphStorage {
			dir := t.TempDir()
			gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
			if err != nil {
				t.Fatal(err)
			}
			seedForClear(t, gs)
			if err := gs.Close(); err != nil {
				t.Fatal(err)
			}
			gs, err = NewGraphStorageWithConfig(mmapConfig(dir))
			if err != nil {
				t.Fatal(err)
			}
			return gs
		},
	}

	for name, openStore := range open {
		t.Run(name, func(t *testing.T) {
			gs := openStore(t)
			defer gs.Close()
			assertNoViolations(t, gs, "before DeleteAllNodes")

			if err := gs.DeleteAllNodes(); err != nil {
				t.Fatalf("DeleteAllNodes: %v", err)
			}

			stats := gs.GetStatistics()
			if stats.NodeCount != 0 || stats.EdgeCount != 0 {
				t.Errorf("GetStatistics after DeleteAllNodes = %d nodes, %d edges, want 0, 0",
					stats.NodeCount, stats.EdgeCount)
			}
			assertNoViolations(t, gs, "after DeleteAllNodes")

			// Repopulating is the documented use. A stale counter or ID set
			// shows here as a total that includes the deleted graph.
			if _, err := gs.CreateNodeWithTenant("tenant-a", []string{"Person"}, nil); err != nil {
				t.Fatalf("CreateNodeWithTenant after clear: %v", err)
			}
			// Read directly: CountNodesForTenant reads tenantStats, which the
			// clear already reset, so it cannot see a stale ID set.
			gs.mu.RLock()
			idSet := len(gs.tenantNodeIDs["tenant-a"])
			gs.mu.RUnlock()
			if idSet != 1 {
				t.Errorf("len(tenantNodeIDs[tenant-a]) after repopulate = %d, want 1", idSet)
			}
			if got := gs.GetStatistics().NodeCount; got != 1 {
				t.Errorf("GetStatistics().NodeCount after repopulate = %d, want 1", got)
			}
			assertNoViolations(t, gs, "after repopulate")
		})
	}
}

// DeleteAllNodes keeps vector index definitions, so a reload needs no second
// CreateVectorIndex: the repopulated vectors are searchable at once and only
// they are found. The definition must also outlive a restart, which it does
// only if the empty snapshot DeleteAllNodes writes records it.
func TestDeleteAllNodes_KeepsVectorIndexDefinitions(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	seedForClear(t, gs)
	if err := gs.DeleteAllNodes(); err != nil {
		t.Fatalf("DeleteAllNodes: %v", err)
	}

	n, err := gs.CreateNode([]string{"Doc"}, map[string]Value{
		"embedding": VectorValue([]float32{0, 0, 1}),
	})
	if err != nil {
		t.Fatalf("CreateNode after clear: %v", err)
	}
	results, err := gs.VectorSearch("embedding", []float32{0, 0, 1}, 10, 50)
	if err != nil {
		t.Fatalf("VectorSearch after clear and repopulate: %v", err)
	}
	if len(results) != 1 || results[0].ID != n.ID {
		t.Fatalf("VectorSearch after repopulate = %+v, want only node %d", results, n.ID)
	}

	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	results, err = reopened.VectorSearch("embedding", []float32{0, 0, 1}, 10, 50)
	if err != nil {
		t.Fatalf("VectorSearch after restart: %v (definition lost across restart?)", err)
	}
	if len(results) != 1 || results[0].ID != n.ID {
		t.Fatalf("VectorSearch after restart = %+v, want only node %d", results, n.ID)
	}
}
