package storage

import (
	"fmt"
	"sync/atomic"
	"testing"
)

// benchEnumSink keeps the enumerated slice live so the compiler can't
// dead-code-eliminate the clone loop.
var benchEnumSink atomic.Pointer[[]*Node]

// legacyGetAllNodesForTenant reproduces the pre-index implementation: a full
// scan of every shard across all tenants, filtered by TenantID under
// gs.mu.RLock. Kept here as the benchmark baseline so the noisy-neighbor
// comparison below is apples-to-apples (same data, same machine) rather than
// relying on remembered numbers. Not used in production.
func (gs *GraphStorage) legacyGetAllNodesForTenant(tenantID string) []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)
	var nodes []*Node
	gs.forEachNodeUnlocked(func(node *Node) bool {
		if effectiveTenantID(node.TenantID) == tid {
			nodes = append(nodes, node.Clone())
		}
		return true
	})
	return nodes
}

// BenchmarkGetAllNodesForTenant_NoisyNeighbor measures the read cost for a
// fixed small tenant (10 nodes) as a co-located "noisy neighbor" tenant
// grows, comparing the per-tenant enumeration index against the legacy full
// scan. The index makes the target read O(its own size); the legacy scan is
// O(total-DB), so its cost climbs with the neighbor's data while the indexed
// read stays in the same band (the residual climb on the indexed path is
// cache/heap effects on the same 10 clones, not extra work — verified by
// len(result) and len(tenantNodeIDs[target]) holding at 10). This is the H4
// cross-tenant read amplification, removed.
func BenchmarkGetAllNodesForTenant_NoisyNeighbor(b *testing.B) {
	const targetTenant = "target"
	const targetSize = 10

	// Bounded so the benchmark's setup stays CI-friendly; the trend is
	// already unambiguous by 10k background nodes.
	for _, bg := range []int{0, 1_000, 10_000} {
		gs, err := NewGraphStorage(b.TempDir())
		if err != nil {
			b.Fatalf("NewGraphStorage: %v", err)
		}

		for i := 0; i < targetSize; i++ {
			if _, err := gs.CreateNodeWithTenant(targetTenant, []string{"Doc"}, nil); err != nil {
				b.Fatalf("create target: %v", err)
			}
		}
		for i := 0; i < bg; i++ {
			if _, err := gs.CreateNodeWithTenant("neighbor", []string{"Doc"}, nil); err != nil {
				b.Fatalf("create neighbor: %v", err)
			}
		}

		b.Run(fmt.Sprintf("indexed/background=%d", bg), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				nodes := gs.GetAllNodesForTenant(targetTenant)
				benchEnumSink.Store(&nodes)
			}
		})
		b.Run(fmt.Sprintf("legacy/background=%d", bg), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				nodes := gs.legacyGetAllNodesForTenant(targetTenant)
				benchEnumSink.Store(&nodes)
			}
		})

		_ = gs.Close()
	}
}
