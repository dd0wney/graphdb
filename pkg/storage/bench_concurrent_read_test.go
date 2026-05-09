package storage

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Audit task A4 (2026-05-10): concurrent-read benchmark for the
// shard-locking + partitioning change.
//
// Empirical finding (M1, Apple Silicon, 8 cores, Go 1.25):
//
//	pattern              post-A4    legacy-global   ratio
//	--------------------------------------------------------
//	Uniform PureReads_4   379 ns    388 ns          1.02×
//	Uniform PureReads_16  371 ns    402 ns          1.08×
//	Uniform PureReads_32  380 ns    437 ns          1.15×
//
// The audit's "2-4× throughput gain at 4 reader goroutines" projection
// (docs/AUDIT_performance_2026-05-06.md HIGH-2) does NOT hold under a
// pure-reader workload on this hardware. Reason: Go's RWMutex.RLock
// only contends with writer Lock — concurrent RLockers don't block
// each other. Pure readers measure cache-line / atomic-op cost only,
// not lock-wait fraction. The ratio scales with concurrency
// (1.02 → 1.08 → 1.15) but stays far below 2× even at 4× CPU
// oversubscription.
//
// What A4 actually delivered:
//
//   - Race-cleanness: race-detector clean under -count=3. The latent
//     shared-map race that gs.mu.RLock-vs-shard.Lock created (the
//     transaction layer was already taking shard.Lock without
//     coordinating with global readers) is closed.
//   - Clone-elision: handlers_vectors.go vector post-filter drops
//     allocations from O(ef) to O(survivors) for filter-heavy queries.
//   - The throughput goal was reframed from "≥2× at 4 readers" to
//     "structural correctness; throughput-neutral on M1."
//
// Three access-pattern axes (per user choice in the A4-T6 design call):
//
//   - Uniform: every iteration picks any node ID with equal probability.
//     Spreads load evenly across all 256 shards — partition's best case.
//   - Zipfian (hot-set): 80% of reads target 20% of nodes. Surfaces
//     intra-shard contention that the partition can't fix; if A4 still
//     clears ≥2× here, that's a much stronger claim than uniform alone.
//   - Mixed: 90% GetNode (now shard-RLock) + 10% FindNodesByLabel
//     (still gs.mu.RLock). Surfaces whether the global-state-reader
//     path becomes the new bottleneck once GetNode is fast.
//
// Two contention axes per pattern:
//
//   - PureReads: N reader goroutines, no writes. Measures the
//     reader-side cache-line ping-pong cost of RLock/RUnlock.
//   - ReadsWithTrickle: N readers + 1 writer doing UpdateNode at
//     ~10ms cadence (advisor's suggestion: realistic light-write load).
//
// Each pattern × contention is run at N=4 readers (the acceptance
// criterion goalpost). Add N=8/16 variants by hand if the scaling
// curve is interesting.

const (
	// benchCorpusSize is the number of pre-populated nodes. 100K
	// matches the audit's "vector search post-filter" sizing math
	// (ef=100 candidates × K=50 results, against a ~100K-node corpus).
	benchCorpusSize = 100_000

	// benchTrickleInterval gates the writer in the ReadsWithTrickle
	// variant. ~10ms = ~100 writes/sec — enough to surface contention
	// without dominating the bench signal.
	benchTrickleInterval = 10 * time.Millisecond

	// benchZipfianHotFrac is the fraction of node IDs that count as
	// "hot" in the Zipfian variant. 0.2 + 0.8 read-share is the
	// classic 80/20 hot-set sizing.
	benchZipfianHotFrac    = 0.2
	benchZipfianHotReadShr = 0.8
)

// accessPattern selects the read-mix the bench body uses.
type accessPattern int

const (
	accessUniform accessPattern = iota
	accessZipfian
	accessMixed
)

// setupBenchCorpus pre-populates a GraphStorage with benchCorpusSize
// nodes. Done once per Benchmark function (outside the b.N loop) so
// the corpus build cost is paid exactly once.
func setupBenchCorpus(b *testing.B) (*GraphStorage, []uint64) {
	b.Helper()
	dir := b.TempDir()
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:        dir,
		BulkImportMode: true, // skip WAL for fast corpus build
	})
	if err != nil {
		b.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	b.Cleanup(func() { _ = gs.Close() })

	ids := make([]uint64, 0, benchCorpusSize)
	for i := 0; i < benchCorpusSize; i++ {
		props := map[string]Value{
			"name": StringValue(fmt.Sprintf("node-%d", i)),
			"idx":  IntValue(int64(i)),
		}
		node, err := gs.CreateNode([]string{"BenchNode"}, props)
		if err != nil {
			b.Fatalf("CreateNode %d: %v", i, err)
		}
		ids = append(ids, node.ID)
	}
	return gs, ids
}

// pickReadID returns the next ID to read under the given access pattern.
func pickReadID(pattern accessPattern, ids []uint64, rng *rand.Rand) uint64 {
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

// benchSink consumes Clone results so the compiler can't dead-code-
// eliminate them. Without this, the Legacy benchmark would drop the
// Clone allocation entirely (it's in the same package, the result is
// unused, escape analysis can prove no consumer) while production
// GetNode keeps the clone (function boundary makes the call opaque) —
// the comparison would then measure different work.
var benchSink atomic.Pointer[Node]

// doRead performs one read under the given access pattern. For
// accessMixed, 1-in-10 iterations swaps the GetNode for a
// FindNodesByLabel scan (the global-state path that still takes
// gs.mu.RLock).
func doRead(pattern accessPattern, gs *GraphStorage, ids []uint64, rng *rand.Rand) {
	if pattern == accessMixed && rng.IntN(10) == 0 {
		nodes, _ := gs.FindNodesByLabel("BenchNode")
		if len(nodes) > 0 {
			benchSink.Store(nodes[0])
		}
		return
	}
	id := pickReadID(pattern, ids, rng)
	if n, err := gs.GetNode(id); err == nil {
		benchSink.Store(n)
	}
}

// freshRng builds a per-goroutine PCG generator from a hash of the
// current goroutine's identity, so concurrent readers don't share or
// stride a single global RNG state (which would itself become a
// contention point and undermine the bench).
func freshRng(seedNonce uint64) *rand.Rand {
	seed1 := uint64(time.Now().UnixNano()) ^ seedNonce
	seed2 := seedNonce*0x9E3779B97F4A7C15 + 1
	return rand.New(rand.NewPCG(seed1, seed2))
}

// runPureReads is the N-reader pure-read harness. b.RunParallel +
// SetParallelism is the idiomatic Go bench shape: setup once, b.N
// auto-tunes to fill the bench window.
func runPureReads(b *testing.B, pattern accessPattern, readers int) {
	b.Helper()
	gs, ids := setupBenchCorpus(b)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetParallelism(readers)

	var seedCounter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		rng := freshRng(seedCounter.Add(1))
		for pb.Next() {
			doRead(pattern, gs, ids, rng)
		}
	})
}

// runReadsWithTrickle is the N-reader + 1-writer harness. We can't use
// b.RunParallel cleanly here because the writer has a different lifecycle,
// so use explicit goroutines. Each pb.Next() call across readers
// counts toward b.N.
func runReadsWithTrickle(b *testing.B, pattern accessPattern, readers int) {
	b.Helper()
	gs, ids := setupBenchCorpus(b)
	b.ResetTimer()
	b.ReportAllocs()

	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(benchTrickleInterval)
		defer ticker.Stop()
		wRng := freshRng(0xDEADBEEFCAFEBABE)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				id := ids[wRng.IntN(len(ids))]
				_ = gs.UpdateNode(id, map[string]Value{
					"touched_at": IntValue(time.Now().UnixNano()),
				})
			}
		}
	}()

	// Distribute b.N work across `readers` goroutines.
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
				doRead(pattern, gs, ids, rng)
			}
		}(work)
	}
	wg.Wait()

	close(stop)
	<-writerDone
}

// getNodeViaGlobalRLock is a benchmark-only method that mirrors
// GetNode's shape (same overhead: time.Now, checkClosed, query timing,
// metrics, deferred unlock, Clone) but uses gs.mu.RLock instead of the
// per-shard read lock. Defined here in the test file so it doesn't
// pollute production code; visible only during testing.
//
// Compare BenchmarkGetNode_Uniform_PureReads_4 (post-A4: per-shard
// RLock) vs BenchmarkGetNode_Legacy_Uniform_PureReads_4 (uses this
// method: global RLock). The only difference is the lock acquisition,
// so the ratio isolates the lock-grain delta from every other piece of
// GetNode overhead. Acceptance criterion (A4-T6): ratio ≥ 2× at 4
// reader goroutines.
func (gs *GraphStorage) getNodeViaGlobalRLock(nodeID uint64) (*Node, error) {
	start := time.Now()
	defer gs.startQueryTiming()()

	if err := gs.checkClosed(); err != nil {
		gs.recordOperation("get_node", "error", start)
		return nil, err
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		gs.recordOperation("get_node", "error", start)
		return nil, ErrNodeNotFound
	}

	gs.recordOperation("get_node", "success", start)
	return node.Clone(), nil
}

func doReadLegacy(pattern accessPattern, gs *GraphStorage, ids []uint64, rng *rand.Rand) {
	if pattern == accessMixed && rng.IntN(10) == 0 {
		nodes, _ := gs.FindNodesByLabel("BenchNode")
		if len(nodes) > 0 {
			benchSink.Store(nodes[0])
		}
		return
	}
	id := pickReadID(pattern, ids, rng)
	if n, err := gs.getNodeViaGlobalRLock(id); err == nil {
		benchSink.Store(n)
	}
}

func runLegacyPureReads(b *testing.B, pattern accessPattern, readers int) {
	b.Helper()
	gs, ids := setupBenchCorpus(b)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetParallelism(readers)

	var seedCounter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		rng := freshRng(seedCounter.Add(1))
		for pb.Next() {
			doReadLegacy(pattern, gs, ids, rng)
		}
	})
}

// ---------- Benchmark functions ----------
//
// Naming: Benchmark<Op>_<Pattern>_<Variant>_<Readers>
//
// Pure-read variants (no writers) — measure the reader-side lock-grain
// win directly.

func BenchmarkGetNode_Uniform_PureReads_4(b *testing.B) { runPureReads(b, accessUniform, 4) }
func BenchmarkGetNode_Zipfian_PureReads_4(b *testing.B) { runPureReads(b, accessZipfian, 4) }
func BenchmarkGetNode_Mixed_PureReads_4(b *testing.B)   { runPureReads(b, accessMixed, 4) }

// Reads-with-trickle variants (N readers + 1 light writer) — measure
// realistic light-write contention against the same N-reader load.

func BenchmarkGetNode_Uniform_ReadsWithTrickle_4(b *testing.B) {
	runReadsWithTrickle(b, accessUniform, 4)
}

func BenchmarkGetNode_Zipfian_ReadsWithTrickle_4(b *testing.B) {
	runReadsWithTrickle(b, accessZipfian, 4)
}

func BenchmarkGetNode_Mixed_ReadsWithTrickle_4(b *testing.B) {
	runReadsWithTrickle(b, accessMixed, 4)
}

// Legacy-lock baseline. ratio = post / pre = the lock-grain throughput
// improvement attributable to A4's per-shard RLock migration.

func BenchmarkGetNode_Legacy_Uniform_PureReads_4(b *testing.B) {
	runLegacyPureReads(b, accessUniform, 4)
}

func BenchmarkGetNode_Legacy_Zipfian_PureReads_4(b *testing.B) {
	runLegacyPureReads(b, accessZipfian, 4)
}

// Higher-concurrency variants: at 4 readers on 8-core M1 the global
// RLock barely contends, so the lock-grain win is small. At 16 / 32
// readers the OS scheduler oversubscribes the cores and the RWMutex
// reader queue starts to manifest — that's where the partition's
// per-shard scaling earns its keep.

func BenchmarkGetNode_Uniform_PureReads_16(b *testing.B) { runPureReads(b, accessUniform, 16) }
func BenchmarkGetNode_Uniform_PureReads_32(b *testing.B) { runPureReads(b, accessUniform, 32) }

func BenchmarkGetNode_Legacy_Uniform_PureReads_16(b *testing.B) {
	runLegacyPureReads(b, accessUniform, 16)
}

func BenchmarkGetNode_Legacy_Uniform_PureReads_32(b *testing.B) {
	runLegacyPureReads(b, accessUniform, 32)
}
