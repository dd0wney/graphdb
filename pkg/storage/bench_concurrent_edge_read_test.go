package storage

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Audit task A4-edges (2026-05-10): edge-side concurrent-read
// benchmark mirroring bench_concurrent_read_test.go's A4-T6 harness.
// Same access-pattern axes (Uniform, Zipfian, Mixed), same contention
// axes (PureReads, ReadsWithTrickle), same Legacy global-RLock
// baseline isolating the lock-grain delta.
//
// Empirical finding (M1, Apple Silicon, 8 cores, Go 1.25):
//
//	pattern              post-A4-edges  legacy-global   ratio
//	-----------------------------------------------------------
//	Uniform PureReads_4   275 ns         268 ns          0.97x
//	Uniform PureReads_16  258 ns         274 ns          1.06x
//	Uniform PureReads_32  263 ns         277 ns          1.05x
//	Zipfian PureReads_4   250 ns         261 ns          1.04x
//
// Same shape as A4 nodes — pure-reader workloads don't contend on
// Go's RWMutex (concurrent RLockers don't block each other), so the
// lock-grain delta is small. At 4 readers the post-A4-edges path is
// slightly slower than legacy due to method-call overhead (one extra
// shardLocks[idx] dereference + the helper indirection); at 16/32
// readers the partition's reader-side scaling pulls ahead by ~5-6%.
// Both far below the 2x the audit projected for nodes — the same
// physics applies here.
//
// What A4-edges actually delivered:
//
//   - Race-cleanness: race-detector clean under -count=3. The latent
//     gs.edges shared-map race (GetEdge under shard.RLock vs
//     CreateEdge/UpdateEdge/DeleteEdge under gs.mu.Lock) is closed.
//   - Symmetry with the node-side partition: helper shape, lock-grain
//     pattern, and bench-harness shape all mirror A4 directly. Future
//     similar partitions (if needed) have a clear template.
//   - The Legacy baseline stays in the file as a regression catch.
//
// One difference from A4's harness: the "Mixed" axis here uses
// GetOutgoingEdges (still gs.mu.RLock — the global adjacency map
// path) rather than FindNodesByLabel. Same role: surfaces the next
// global-state-reader bottleneck after edge point lookups become fast.

// edgeBenchCorpusEdgeFanout is the number of outgoing edges per node
// in the bench corpus. 5 keeps total edge count manageable (100K
// nodes * 5 = 500K edges) while still giving GetOutgoingEdges
// meaningful work to do.
const edgeBenchCorpusEdgeFanout = 5

// setupEdgeBenchCorpus reuses setupBenchCorpus to build the node
// corpus, then adds edgeBenchCorpusEdgeFanout outgoing edges per
// node (target = next node in ID order, wrapping around). Returns
// the GraphStorage, the node ID slice (for source-node access
// patterns), and the edge ID slice (for direct edge-ID access).
func setupEdgeBenchCorpus(b *testing.B) (*GraphStorage, []uint64, []uint64) {
	b.Helper()
	gs, nodeIDs := setupBenchCorpus(b)

	edgeIDs := make([]uint64, 0, len(nodeIDs)*edgeBenchCorpusEdgeFanout)
	for i, fromID := range nodeIDs {
		for j := 0; j < edgeBenchCorpusEdgeFanout; j++ {
			toIdx := (i + 1 + j) % len(nodeIDs)
			toID := nodeIDs[toIdx]
			edge, err := gs.CreateEdge(fromID, toID, "BENCH_EDGE", map[string]Value{
				"weight": IntValue(int64(j)),
			}, 1.0)
			if err != nil {
				b.Fatalf("CreateEdge from=%d to=%d: %v", fromID, toID, err)
			}
			edgeIDs = append(edgeIDs, edge.ID)
		}
	}
	return gs, nodeIDs, edgeIDs
}

// pickEdgeReadID returns the next edge ID to read under the given
// access pattern. Same shape as pickReadID but on the edge ID space.
func pickEdgeReadID(pattern accessPattern, ids []uint64, rng *rand.Rand) uint64 {
	switch pattern {
	case accessZipfian:
		hotSize := int(float64(len(ids)) * benchZipfianHotFrac)
		if hotSize == 0 {
			hotSize = 1
		}
		if rng.Float64() < benchZipfianHotReadShr {
			return ids[rng.IntN(hotSize)]
		}
		return ids[rng.IntN(len(ids))]
	default:
		return ids[rng.IntN(len(ids))]
	}
}

// doEdgeRead performs one read under the given access pattern. For
// accessMixed, 1-in-10 iterations swaps the GetEdge for a
// GetOutgoingEdges scan (the global adjacency-map path that still
// takes gs.mu.RLock).
func doEdgeRead(pattern accessPattern, gs *GraphStorage, nodeIDs, edgeIDs []uint64, rng *rand.Rand) {
	if pattern == accessMixed && rng.IntN(10) == 0 {
		// Pick a random source node and read its outgoing edges. This
		// hits gs.outgoingEdges[nodeID] under gs.mu.RLock plus a
		// per-edge lookupEdgeShard for each edge in the adjacency list.
		nodeID := nodeIDs[rng.IntN(len(nodeIDs))]
		edges, _ := gs.GetOutgoingEdges(nodeID)
		if len(edges) > 0 {
			benchSinkEdge.Store(edges[0])
		}
		return
	}
	id := pickEdgeReadID(pattern, edgeIDs, rng)
	if e, err := gs.GetEdge(id); err == nil {
		benchSinkEdge.Store(e)
	}
}

// benchSinkEdge is the edge analogue of benchSink in the node bench
// file. Same purpose: prevent dead-code elimination of Clone() so the
// Legacy baseline measures the same work as production GetEdge.
var benchSinkEdge atomic.Pointer[Edge]

// getEdgeViaGlobalRLock is the edge analogue of getNodeViaGlobalRLock.
// Mirrors GetEdge's shape (defer query timing, shard read, Clone) but
// uses gs.mu.RLock instead of the per-shard read lock. Defined here
// in the test file; visible only during testing.
func (gs *GraphStorage) getEdgeViaGlobalRLock(edgeID uint64) (*Edge, error) {
	defer gs.startQueryTiming()()

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	edge, exists := gs.lookupEdgeShard(edgeID)
	if !exists {
		return nil, ErrEdgeNotFound
	}
	return edge.Clone(), nil
}

func doEdgeReadLegacy(pattern accessPattern, gs *GraphStorage, nodeIDs, edgeIDs []uint64, rng *rand.Rand) {
	if pattern == accessMixed && rng.IntN(10) == 0 {
		nodeID := nodeIDs[rng.IntN(len(nodeIDs))]
		edges, _ := gs.GetOutgoingEdges(nodeID)
		if len(edges) > 0 {
			benchSinkEdge.Store(edges[0])
		}
		return
	}
	id := pickEdgeReadID(pattern, edgeIDs, rng)
	if e, err := gs.getEdgeViaGlobalRLock(id); err == nil {
		benchSinkEdge.Store(e)
	}
}

func runEdgePureReads(b *testing.B, pattern accessPattern, readers int) {
	b.Helper()
	gs, nodeIDs, edgeIDs := setupEdgeBenchCorpus(b)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetParallelism(readers)

	var seedCounter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		rng := freshRng(seedCounter.Add(1))
		for pb.Next() {
			doEdgeRead(pattern, gs, nodeIDs, edgeIDs, rng)
		}
	})
}

func runEdgeLegacyPureReads(b *testing.B, pattern accessPattern, readers int) {
	b.Helper()
	gs, nodeIDs, edgeIDs := setupEdgeBenchCorpus(b)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetParallelism(readers)

	var seedCounter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		rng := freshRng(seedCounter.Add(1))
		for pb.Next() {
			doEdgeReadLegacy(pattern, gs, nodeIDs, edgeIDs, rng)
		}
	})
}

func runEdgeReadsWithTrickle(b *testing.B, pattern accessPattern, readers int) {
	b.Helper()
	gs, nodeIDs, edgeIDs := setupEdgeBenchCorpus(b)
	b.ResetTimer()
	b.ReportAllocs()

	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(benchTrickleInterval)
		defer ticker.Stop()
		wRng := freshRng(0xCAFEBABEDEADBEEF)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				id := edgeIDs[wRng.IntN(len(edgeIDs))]
				w := wRng.Float64()
				_ = gs.UpdateEdge(id, map[string]Value{
					"touched_at": IntValue(time.Now().UnixNano()),
				}, &w)
			}
		}
	}()

	var wg sync.WaitGroup
	perReader := b.N / readers
	remainder := b.N % readers
	var seedCounter atomic.Uint64
	for r := 0; r < readers; r++ {
		work := perReader
		if r < remainder {
			work++
		}
		wg.Add(1)
		go func(work int) {
			defer wg.Done()
			rng := freshRng(seedCounter.Add(1))
			for i := 0; i < work; i++ {
				doEdgeRead(pattern, gs, nodeIDs, edgeIDs, rng)
			}
		}(work)
	}
	wg.Wait()

	close(stop)
	<-writerDone
}

// ---------- Benchmark functions ----------

func BenchmarkGetEdge_Uniform_PureReads_4(b *testing.B) { runEdgePureReads(b, accessUniform, 4) }
func BenchmarkGetEdge_Zipfian_PureReads_4(b *testing.B) { runEdgePureReads(b, accessZipfian, 4) }
func BenchmarkGetEdge_Mixed_PureReads_4(b *testing.B)   { runEdgePureReads(b, accessMixed, 4) }

func BenchmarkGetEdge_Uniform_ReadsWithTrickle_4(b *testing.B) {
	runEdgeReadsWithTrickle(b, accessUniform, 4)
}
func BenchmarkGetEdge_Zipfian_ReadsWithTrickle_4(b *testing.B) {
	runEdgeReadsWithTrickle(b, accessZipfian, 4)
}
func BenchmarkGetEdge_Mixed_ReadsWithTrickle_4(b *testing.B) {
	runEdgeReadsWithTrickle(b, accessMixed, 4)
}

// Legacy-lock baseline. ratio = post / pre = the lock-grain throughput
// improvement attributable to A4-edges' per-shard RLock migration.

func BenchmarkGetEdge_Legacy_Uniform_PureReads_4(b *testing.B) {
	runEdgeLegacyPureReads(b, accessUniform, 4)
}
func BenchmarkGetEdge_Legacy_Zipfian_PureReads_4(b *testing.B) {
	runEdgeLegacyPureReads(b, accessZipfian, 4)
}

// Higher-concurrency variants — same scaling-curve story as A4 nodes.
// At 4 readers on 8-core M1 the global RLock barely contends; 16/32
// readers oversubscribe the cores and the RWMutex reader queue starts
// to manifest.

func BenchmarkGetEdge_Uniform_PureReads_16(b *testing.B) { runEdgePureReads(b, accessUniform, 16) }
func BenchmarkGetEdge_Uniform_PureReads_32(b *testing.B) { runEdgePureReads(b, accessUniform, 32) }

func BenchmarkGetEdge_Legacy_Uniform_PureReads_16(b *testing.B) {
	runEdgeLegacyPureReads(b, accessUniform, 16)
}
func BenchmarkGetEdge_Legacy_Uniform_PureReads_32(b *testing.B) {
	runEdgeLegacyPureReads(b, accessUniform, 32)
}
