package storage

import (
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenantid"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// validGraph builds a small healthy graph (one tenant, two labelled nodes, one
// edge, a vector index + one vector node) and returns it with the node IDs. It
// asserts the graph is invariant-clean to start, so the teeth-test below proves
// each corruption is what flips the checker — not a pre-existing violation.
func validGraph(t *testing.T) (gs *GraphStorage, a, b uint64) {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	if err := gs.CreateVectorIndexForTenant("acme", "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	// A property index too, so the property-index invariants are exercised (node
	// a carries "kind", node b does not — covering the not-indexed-node case).
	if err := gs.CreatePropertyIndex("kind", TypeString); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	na, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{
		"embedding": VectorValue([]float32{1, 0, 0}),
		"kind":      StringValue("alpha"),
	})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	nb, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant("acme", na.ID, nb.ID, "LINKS", nil, 1.0); err != nil {
		t.Fatalf("create edge: %v", err)
	}
	if v := checkGraphInvariants(gs); len(v) != 0 {
		t.Fatalf("baseline graph is not invariant-clean: %v", v)
	}
	return gs, na.ID, nb.ID
}

// propIndexFor fetches a property index by key under gs.mu (the map itself is
// guarded by gs.mu; the returned *PropertyIndex has its own mu for its buckets).
func propIndexFor(t *testing.T, gs *GraphStorage, key string) *PropertyIndex {
	t.Helper()
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	idx, ok := gs.propertyIndexes[key]
	if !ok {
		t.Fatalf("no property index for %q", key)
	}
	return idx
}

// TestGraphInvariants_DetectsDrift proves the checker has TEETH: each case
// corrupts exactly one derived structure (the way a forgotten index update
// would) and asserts checkGraphInvariants reports it. A checker that only ever
// passes green tests would be worthless against silent drift — this is the
// evidence it catches the bug classes #288/#298/#305/#307/#308 came from.
func TestGraphInvariants_DetectsDrift(t *testing.T) {
	const acme = tenantid.TenantID("acme")

	tests := []struct {
		name    string
		corrupt func(gs *GraphStorage, a, b uint64)
		want    string // substring the violation report must contain
	}{
		{
			name: "tenant node count drift",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				gs.tenantStats[acme].NodeCount++
				gs.mu.Unlock()
			},
			want: "tenantStats.NodeCount",
		},
		{
			name: "global stats node count drift",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				gs.stats.NodeCount++
				gs.mu.Unlock()
			},
			want: "stats.NodeCount",
		},
		{
			name: "stale tenant enumeration-set entry",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				gs.tenantNodeIDs[acme][999999] = struct{}{}
				gs.mu.Unlock()
			},
			want: "tenantNodeIDs",
		},
		{
			name: "node dropped from tenant label index (forward)",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				delete(gs.tenantNodesByLabel[acme]["Doc"], a)
				gs.mu.Unlock()
			},
			want: "missing from tenant",
		},
		{
			name: "empty per-tenant bucket (must be GC'd)",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				gs.tenantEdgesByType[acme]["ORPHAN"] = map[uint64]struct{}{}
				gs.mu.Unlock()
			},
			want: "empty bucket",
		},
		{
			name: "phantom id in global label bucket (reverse)",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				gs.nodesByLabel["Doc"][888888] = struct{}{}
				gs.mu.Unlock()
			},
			want: "no live entity carries",
		},
		{
			name: "dangling outgoing adjacency entry",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				gs.mu.Lock()
				gs.outgoingEdges[a] = append(gs.outgoingEdges[a], 777777)
				gs.mu.Unlock()
			},
			want: "dangling",
		},
		{
			name: "vector index over-count",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				// Insert a vector for a node that carries no embedding property.
				if err := gs.vectorIndex.AddVectorForTenant(acme, "embedding", b, []float32{0, 0, 1}); err != nil {
					t.Fatalf("AddVectorForTenant: %v", err)
				}
			},
			want: "vector: index",
		},
		{
			name: "phantom id in property bucket (reverse)",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				idx := propIndexFor(t, gs, "kind")
				idx.mu.Lock()
				idx.index["alpha"] = append(idx.index["alpha"], 888888)
				idx.mu.Unlock()
			},
			want: "not backed by a live node",
		},
		{
			name: "node dropped from property bucket (forward)",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				// Drop the whole "alpha" bucket: node a still carries kind="alpha"
				// in the shard, so the forward check must report it missing.
				idx := propIndexFor(t, gs, "kind")
				idx.mu.Lock()
				delete(idx.index, "alpha")
				idx.mu.Unlock()
			},
			want: "missing from bucket",
		},
		{
			name: "empty property bucket (must be GC'd)",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				idx := propIndexFor(t, gs, "kind")
				idx.mu.Lock()
				idx.index["ghost"] = []uint64{}
				idx.mu.Unlock()
			},
			want: "empty bucket",
		},
		{
			name: "duplicate id in property bucket",
			corrupt: func(gs *GraphStorage, a, b uint64) {
				idx := propIndexFor(t, gs, "kind")
				idx.mu.Lock()
				idx.index["alpha"] = append(idx.index["alpha"], a) // a listed twice
				idx.mu.Unlock()
			},
			want: "appears more than once",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs, a, b := validGraph(t)
			defer func() { _ = gs.Close() }()

			tt.corrupt(gs, a, b)

			violations := checkGraphInvariants(gs)
			if len(violations) == 0 {
				t.Fatalf("checker reported NO violation after corruption %q — it would miss this drift class", tt.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v, tt.want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no violation contained %q; got: %v", tt.want, violations)
			}
		})
	}
}
