package vector

import (
	"math/rand"
	"strconv"
	"testing"
)

// trueMaxLevel scans every node (the pre-M4 O(N) ground truth) for the
// highest top-level present, or -1 if empty.
func trueMaxLevel(h *HNSWIndex) int {
	max := -1
	for _, n := range h.nodes {
		if n.level > max {
			max = n.level
		}
	}
	return max
}

// assertLevelIndexConsistent verifies nodesByLevel is an exact partition of
// h.nodes by level: every node appears under its own level, no bucket holds a
// node of a different level, no empty buckets linger, and no stale ids remain.
func assertLevelIndexConsistent(t *testing.T, h *HNSWIndex) {
	t.Helper()
	seen := 0
	for level, bucket := range h.nodesByLevel {
		if len(bucket) == 0 {
			t.Fatalf("nodesByLevel[%d] is an empty bucket — should have been deleted", level)
		}
		for id, node := range bucket {
			seen++
			live, ok := h.nodes[id]
			if !ok {
				t.Fatalf("nodesByLevel[%d] holds id %d that is not in h.nodes (stale)", level, id)
			}
			if live != node {
				t.Fatalf("nodesByLevel[%d][%d] is a different pointer than h.nodes[%d]", level, id, id)
			}
			if node.level != level {
				t.Fatalf("node %d (level %d) indexed under level %d", id, node.level, level)
			}
		}
	}
	if seen != len(h.nodes) {
		t.Fatalf("level index covers %d nodes but h.nodes has %d", seen, len(h.nodes))
	}
}

// TestEntryPointLevelIndex_ConsistencyAndReplacement exercises the M4 index
// across inserts and repeated entry-point deletions, checking after each
// mutation that (a) the level index partitions h.nodes exactly, and (b) the
// entry point + maxLayer always reflect the true highest remaining level.
func TestEntryPointLevelIndex_ConsistencyAndReplacement(t *testing.T) {
	idx, err := NewHNSWIndex(16, 8, 100, MetricCosine)
	if err != nil {
		t.Fatalf("NewHNSWIndex: %v", err)
	}
	rng := rand.New(rand.NewSource(99))

	const n = 200
	for i := 0; i < n; i++ {
		vec := make([]float32, 16)
		for j := range vec {
			vec[j] = rng.Float32()
		}
		if err := idx.Insert(uint64(i+1), vec); err != nil {
			t.Fatalf("Insert %d: %v", i+1, err)
		}
	}
	assertLevelIndexConsistent(t, idx)
	if idx.entryPoint == nil || idx.maxLayer != trueMaxLevel(idx) {
		t.Fatalf("after inserts: maxLayer=%d, trueMax=%d, entryPoint=%v", idx.maxLayer, trueMaxLevel(idx), idx.entryPoint)
	}

	// Repeatedly delete the current entry point — the path that calls
	// findNewEntryPoint — and re-check the invariants each time.
	for idx.entryPoint != nil {
		epID := idx.entryPoint.id
		if err := idx.Delete(epID); err != nil {
			t.Fatalf("Delete entry point %d: %v", epID, err)
		}
		assertLevelIndexConsistent(t, idx)

		want := trueMaxLevel(idx)
		if want < 0 {
			if idx.entryPoint != nil {
				t.Fatalf("index empty but entryPoint=%v", idx.entryPoint)
			}
			break
		}
		if idx.entryPoint == nil {
			t.Fatalf("nodes remain (maxLevel=%d) but entryPoint is nil", want)
		}
		if idx.entryPoint.level != want {
			t.Fatalf("new entry point at level %d, but highest remaining level is %d", idx.entryPoint.level, want)
		}
		if idx.maxLayer != want {
			t.Fatalf("maxLayer=%d, want %d", idx.maxLayer, want)
		}
	}

	if len(idx.nodesByLevel) != 0 {
		t.Fatalf("nodesByLevel not empty after deleting all nodes: %d buckets", len(idx.nodesByLevel))
	}
}

// TestEntryPointLevelIndex_DeleteNonEntryPoint confirms deleting a node that
// is NOT the entry point still maintains the level index and leaves the entry
// point untouched.
func TestEntryPointLevelIndex_DeleteNonEntryPoint(t *testing.T) {
	idx, err := NewHNSWIndex(8, 8, 100, MetricCosine)
	if err != nil {
		t.Fatalf("NewHNSWIndex: %v", err)
	}
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 50; i++ {
		vec := make([]float32, 8)
		for j := range vec {
			vec[j] = rng.Float32()
		}
		if err := idx.Insert(uint64(i+1), vec); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	epID := idx.entryPoint.id
	// Delete some non-entry-point node.
	var victim uint64
	for id := range idx.nodes {
		if id != epID {
			victim = id
			break
		}
	}
	if err := idx.Delete(victim); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertLevelIndexConsistent(t, idx)
	if idx.entryPoint == nil || idx.entryPoint.id != epID {
		t.Fatalf("entry point changed after deleting a non-entry-point node")
	}
}

// legacyFindMaxLevelScan reproduces the pre-M4 O(N) scan of all nodes, as the
// benchmark baseline for findNewEntryPoint.
func legacyFindMaxLevelScan(h *HNSWIndex) int {
	max := -1
	for _, n := range h.nodes {
		if n.level > max {
			max = n.level
		}
	}
	return max
}

// BenchmarkFindNewEntryPoint compares the M4 level-index lookup against the
// legacy full-node scan as the index grows. The indexed path is ~flat
// (O(#levels)); the legacy scan rises with N (O(N)) — the cost a delete of the
// entry point used to pay.
func BenchmarkFindNewEntryPoint(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		idx, _ := NewHNSWIndex(32, 16, 100, MetricCosine)
		rng := rand.New(rand.NewSource(int64(n)))
		for i := 0; i < n; i++ {
			vec := make([]float32, 32)
			for j := range vec {
				vec[j] = rng.Float32()
			}
			_ = idx.Insert(uint64(i+1), vec)
		}

		b.Run("indexed/N="+strconv.Itoa(n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = idx.findNewEntryPoint()
			}
		})
		b.Run("legacy/N="+strconv.Itoa(n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = legacyFindMaxLevelScan(idx)
			}
		})
	}
}
