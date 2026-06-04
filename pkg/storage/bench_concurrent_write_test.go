package storage

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/tenantid"
	"github.com/dd0wney/graphdb/pkg/vector"
)

// Performance audit (2026-06-02): concurrent multi-tenant write benchmark
// for the "perf under realistic SaaS load" audit
// (docs/internals/design/AUDIT_performance_saas_load_2026-06-02.md).
//
// What this benchmark isolates
// ----------------------------
// Every writer (CreateNodeWithTenant -> createNodeLocked) holds the process-
// global gs.mu.Lock for the whole critical section. That serialization is
// KNOWN and accepted (CLAUDE.md: "Currently moot because gs.mu.Lock
// serializes writers"). What changed is WHAT lives inside that section:
// the Track-R auto-embed path means createNodeLocked now calls
// UpdateNodeVectorIndexes, which — when the tenant has a vector index —
// runs RemoveVectorForTenant THEN AddVectorForTenant, i.e. two synchronous
// HNSW graph traversals (O(log N) distance computations each), under the
// global lock.
//
// Design
// ------
//   - Each writer goroutine writes to its OWN tenant. Per-shard locks and
//     per-tenant indexes therefore never contend across goroutines — the
//     ONLY shared contention point is gs.mu.Lock. Any failure of throughput
//     to scale with goroutine count is attributable solely to that lock.
//   - Two variants write the SAME node shape (a 128-dim vector property):
//     - NoIndex:   no vector index exists -> UpdateNodeVectorIndexes hits
//       HasIndexForTenant==false and `continue`s. Measures the global-lock +
//       index-bookkeeping + WAL-fsync floor WITHOUT any HNSW work.
//     - WithIndex: a vector index exists, pre-populated to a realistic size
//       so each insert does real log-N traversal. Measures the same floor
//       PLUS the HNSW remove+add now living in the critical section.
//   - The WithIndex/NoIndex delta is the cost the auto-embed path added to
//     the serialized critical section. Because it is paid under the global
//     lock, it does not parallelize away — it scales the serialization.
//
// Honest-measurement note: the sibling read benchmark
// (bench_concurrent_read_test.go) empirically DISPROVED the 2026-05-06
// audit's "2-4x" read projection (RWMutex RLockers don't contend). Writers
// are different: gs.mu.Lock is a full mutex, so writers genuinely serialize.
// This benchmark reports whatever the hardware shows; if the HNSW delta is
// swamped by the per-write fsync floor, that is itself a reportable finding
// (the fsync floor dominates, per AUDIT_performance_2026-05-06 HIGH-3).

const (
	benchWriteDims       = 128
	benchPrepopPerTenant = 1000 // pre-seed each tenant's index so inserts do real log-N work
)

// makeBenchVector returns a deterministic non-degenerate unit-ish vector.
// Deterministic (no rand) keeps the benchmark reproducible; non-degenerate
// (varies per seed) avoids the concentration-of-measure pathology that makes
// HNSW build O(N^2) on uniform/zero vectors (see memory: HNSW construction
// cost is data-dependent).
func makeBenchVector(seed int) []float32 {
	v := make([]float32, benchWriteDims)
	var norm float64
	for i := range v {
		// cheap deterministic pseudo-spread; distinct per (seed,i)
		x := math.Sin(float64(seed*131+i*7) * 0.6180339887)
		v[i] = float32(x)
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		norm = 1
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

func benchWriteProps(seed int) map[string]Value {
	return map[string]Value{
		"embedding": VectorValue(makeBenchVector(seed)),
		"name":      StringValue(fmt.Sprintf("n%d", seed)),
	}
}

// runConcurrentWriteBench fans `goroutines` writers out, one per tenant, each
// creating nodes in a tight loop with the DEFAULT WAL config (fsync per write).
// withIndex toggles whether each tenant has a pre-populated vector index (so
// createNodeLocked -> UpdateNodeVectorIndexes does HNSW remove+add work).
//
// batched selects the WAL mode. false = default fsync-per-write (the
// production-shaped path). true = WAL batching, which — counterintuitively —
// is WORSE under the global write lock: BatchedWAL.Append parks on its
// done-channel while the caller still holds gs.mu.Lock, so no second writer
// can enter to fill the batch; every write waits the full FlushInterval. The
// _Batched benchmark exists to measure that pathology, not to recommend it.
//
// The HNSW term the auto-embed path adds is quantified cleanly, without WAL
// noise, by BenchmarkVectorIndexInsert below.
func runConcurrentWriteBench(b *testing.B, goroutines int, withIndex, batched bool) {
	b.Helper()
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:               b.TempDir(),
		EnableBatching:        batched,
		EnableCompression:     false,
		EnableEdgeCompression: true,
		BatchSize:             100,
		FlushInterval:         10 * time.Millisecond,
	})
	if err != nil {
		b.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer gs.Close()

	tenants := make([]string, goroutines)
	for g := 0; g < goroutines; g++ {
		tenants[g] = fmt.Sprintf("tenant-%d", g)
		if withIndex {
			if err := gs.CreateVectorIndexForTenant(
				tenants[g], "embedding", benchWriteDims, 16, 200, vector.MetricCosine,
			); err != nil {
				b.Fatalf("CreateVectorIndexForTenant: %v", err)
			}
			// Pre-populate so each timed insert does realistic log-N traversal.
			for i := 0; i < benchPrepopPerTenant; i++ {
				if _, err := gs.CreateNodeWithTenant(tenants[g], []string{"Doc"}, benchWriteProps(i)); err != nil {
					b.Fatalf("prepop CreateNodeWithTenant: %v", err)
				}
			}
		}
	}

	// Each goroutine gets b.N/goroutines iterations (ceil) so total work is ~b.N.
	per := (b.N + goroutines - 1) / goroutines

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(tenant string, base int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				seed := benchPrepopPerTenant + base*per + i
				if _, err := gs.CreateNodeWithTenant(tenant, []string{"Doc"}, benchWriteProps(seed)); err != nil {
					// b.Fatalf is not goroutine-safe; record and bail.
					b.Errorf("CreateNodeWithTenant: %v", err)
					return
				}
			}
		}(tenants[g], g)
	}
	wg.Wait()
	b.StopTimer()
}

var benchWriteGoroutines = []int{1, 4, 8, 16}

// BenchmarkConcurrentWrite_NoIndex measures the write critical-section floor
// (global lock + index bookkeeping + WAL fsync) with NO HNSW work. The
// goroutine axis is the headline: each goroutine writes to its own tenant, so
// the only shared contention is gs.mu.Lock. If per-op latency stays flat as
// goroutines grow, aggregate throughput does NOT scale with tenant count —
// i.e. N tenants writing concurrently share one tenant's throughput
// (noisy-neighbor serialization). gs.mu.Lock is a full mutex, so this is the
// expected — and now measured — shape.
func BenchmarkConcurrentWrite_NoIndex(b *testing.B) {
	for _, g := range benchWriteGoroutines {
		b.Run(fmt.Sprintf("g=%d", g), func(b *testing.B) {
			runConcurrentWriteBench(b, g, false /*withIndex*/, false /*batched*/)
		})
	}
}

// BenchmarkConcurrentWrite_NoIndex_Batched measures the SAME path with WAL
// batching enabled.
//
// History: the 2026-05-06 audit recommended batching to "amortize fsyncs across
// concurrent writes," but pre-Track-P that premise was defeated by the global
// lock — the create path held gs.mu while BatchedWAL.Append parked on its
// done-channel, so the batch never exceeded one entry and every write paid the
// full FlushInterval (~10ms). Measured pre-fix: ~10–13ms/op, flat across
// goroutines, WORSE than the fsync default.
//
// Track P item (1) fixed this: createNodeLocked now ENQUEUES under gs.mu and the
// public method WAITS on durability AFTER releasing gs.mu, so concurrent writers
// fill one batch (group commit). Expect per-op latency to now DROP sharply with
// goroutine count (single fsync amortized across the batch), e.g. ~10ms at g=1
// (one writer can't fill a batch) down to sub-millisecond at g=16. This is the
// only write path converted so far; the other write methods still take the
// pre-fix synchronous path.
func BenchmarkConcurrentWrite_NoIndex_Batched(b *testing.B) {
	for _, g := range benchWriteGoroutines {
		b.Run(fmt.Sprintf("g=%d", g), func(b *testing.B) {
			runConcurrentWriteBench(b, g, false /*withIndex*/, true /*batched*/)
		})
	}
}

// BenchmarkConcurrentWrite_WithIndex measures the same path WITH a
// pre-populated per-tenant vector index, so createNodeLocked runs HNSW
// remove+add under the global lock. The delta vs _NoIndex at matched goroutine
// count is the in-path cost of the Track-R auto-embed HNSW work as paid TODAY
// (under the default fsync WAL). Because the ~ms fsync floor dominates, this
// delta reads as small here — the clean HNSW cost (what gets EXPOSED once the
// fsync floor is removed by group-commit) is isolated in
// BenchmarkVectorIndexInsert.
func BenchmarkConcurrentWrite_WithIndex(b *testing.B) {
	for _, g := range benchWriteGoroutines {
		b.Run(fmt.Sprintf("g=%d", g), func(b *testing.B) {
			runConcurrentWriteBench(b, g, true /*withIndex*/, false /*batched*/)
		})
	}
}

// BenchmarkVectorIndexInsert isolates the HNSW insert cost the Track-R
// auto-embed path added to the write critical section — the remove+add pair
// createNodeLocked runs via UpdateNodeVectorIndexes when a tenant vector index
// exists. No WAL, no global lock: this is the pure per-insert HNSW cost, which
// grows ~O(log N) with index size. It is the term that becomes the dominant
// serialized cost once the fsync floor is amortized away. Single-threaded by
// design — the point is the per-insert cost, not concurrency.
func BenchmarkVectorIndexInsert(b *testing.B) {
	for _, prepop := range []int{0, 1000, 10000} {
		b.Run(fmt.Sprintf("prepop=%d", prepop), func(b *testing.B) {
			vi := NewVectorIndex()
			tenant := tenantid.TenantID("t0")
			if err := vi.CreateIndexForTenant(tenant, "embedding", benchWriteDims, 16, 200, vector.MetricCosine); err != nil {
				b.Fatalf("CreateIndexForTenant: %v", err)
			}
			for i := 0; i < prepop; i++ {
				if err := vi.AddVectorForTenant(tenant, "embedding", uint64(i+1), makeBenchVector(i)); err != nil {
					b.Fatalf("prepop AddVectorForTenant: %v", err)
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id := uint64(prepop + i + 1)
				// Mirror the production path: remove (no-op on first write) then add.
				_ = vi.RemoveVectorForTenant(tenant, "embedding", id)
				if err := vi.AddVectorForTenant(tenant, "embedding", id, makeBenchVector(prepop+i)); err != nil {
					b.Fatalf("AddVectorForTenant: %v", err)
				}
			}
		})
	}
}
