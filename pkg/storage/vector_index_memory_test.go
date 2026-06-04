package storage

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/dd0wney/graphdb/pkg/tenantid"
	"github.com/dd0wney/graphdb/pkg/vector"
)

// TestVectorIndex_PerTenantMemoryFootprint measures the actual Go-heap
// memory cost of per-tenant HNSW indexes at three representative scales,
// validating the F4 spike §5 estimate that motivated Decision 2 (tier-based
// resolution: OSS = Option A per-tenant HNSW; enterprise plugin = Option B
// filtered HNSW for thousands-of-tenants customers).
//
// The spike's Option A footprint estimate was 3.2 GB for 100 tenants ×
// 10000 vectors × 768 dims. This test produces the actual number under
// the current implementation so reviewers and the next session can
// compare against the estimate without re-deriving it.
//
// Output format: structured key=value lines suitable for grep / awk
// processing across runs:
//
//	HNSW_MEMORY tenants=N vectors_per_tenant=M dims=D heap_bytes=B per_tenant_bytes=T per_vector_bytes=V
//
// Caveats:
//
//   - HNSW insertion is randomized (math/rand layer assignment). Per-run
//     variance is ~5-10%; if a number looks anomalous, run 3 times and
//     take the median.
//   - Test uses zero-filled vectors. Real-world vectors have non-zero
//     values, but the bytes-per-vector cost is identical (HNSW stores
//     them verbatim). What varies is the graph-edge density.
//   - This measures Go-heap allocation only. Disk-backed / mmap'd
//     storage (BTreeGraphStorage from R3) reports different numbers
//     and is not covered here.
//   - GRAPHDB_BENCH_LARGE=1 enables the 100-tenant × 10k-vector × 768-dim
//     scenario. Without it, only the small + medium scenarios run; CI
//     stays fast.
//
// To run interactively with verbose output:
//
//	go test -v -run TestVectorIndex_PerTenantMemoryFootprint ./pkg/storage/
//
// To run the large scenario as well:
//
//	GRAPHDB_BENCH_LARGE=1 go test -v -run TestVectorIndex_PerTenantMemoryFootprint -timeout 600s ./pkg/storage/
func TestVectorIndex_PerTenantMemoryFootprint(t *testing.T) {
	scenarios := []struct {
		name             string
		tenants          int
		vectorsPerTenant int
		dims             int
		large            bool // skipped unless GRAPHDB_BENCH_LARGE is set
	}{
		{name: "small", tenants: 5, vectorsPerTenant: 100, dims: 128},
		// Gated behind GRAPHDB_BENCH_LARGE: 20k inserts. This footprint
		// benchmark was fast only while HNSW search was broken (early
		// termination skipped most insert work); with correct search the
		// 256-dim build runs for minutes and times out the standard suite.
		// The O(N^2) insert cost is tracked separately for the perf track.
		{name: "medium", tenants: 20, vectorsPerTenant: 1000, dims: 256, large: true},
		{name: "spike_estimate", tenants: 100, vectorsPerTenant: 10000, dims: 768, large: true},
		// Count-scaling row. Holds vectors_per_tenant=1000 and dims=768
		// constant so per_tenant_bytes is directly comparable across the
		// three points. Validates whether Option A's "scales linearly in
		// tenants" assumption holds beyond the documented 100-tenant
		// baseline.
		{name: "count_scale_100", tenants: 100, vectorsPerTenant: 1000, dims: 768, large: true},
		{name: "count_scale_500", tenants: 500, vectorsPerTenant: 1000, dims: 768, large: true},
		{name: "count_scale_1000", tenants: 1000, vectorsPerTenant: 1000, dims: 768, large: true},
	}

	// Per-tenant bytes from each scenario, keyed by scenario name. Filled
	// inside t.Run subtests; consumed by the count-scaling check below.
	perTenantBytesByScenario := make(map[string]uint64)

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			if sc.large && os.Getenv("GRAPHDB_BENCH_LARGE") == "" {
				t.Skipf("skipping %s scenario (GRAPHDB_BENCH_LARGE not set; expected runtime ~minutes)", sc.name)
			}

			heapBytes := measureVectorIndexHeapDelta(t, sc.tenants, sc.vectorsPerTenant, sc.dims)
			perTenantBytes := heapBytes / uint64(sc.tenants)
			perVectorBytes := heapBytes / uint64(sc.tenants*sc.vectorsPerTenant)
			perTenantBytesByScenario[sc.name] = perTenantBytes

			// Structured line for grep / parse across runs. Keep field
			// names stable — downstream tooling may key on them.
			t.Logf("HNSW_MEMORY tenants=%d vectors_per_tenant=%d dims=%d heap_bytes=%d per_tenant_bytes=%d per_vector_bytes=%d",
				sc.tenants, sc.vectorsPerTenant, sc.dims, heapBytes, perTenantBytes, perVectorBytes)

			// Sanity floor: a 128-dim float32 vector is 512 bytes raw;
			// HNSW per-vector overhead pushes that to ~1-2 KB. If
			// per_vector_bytes is below the raw size, the measurement
			// is wrong (e.g., the index didn't actually accept inserts).
			rawBytesPerVector := uint64(sc.dims * 4)
			if perVectorBytes < rawBytesPerVector {
				t.Errorf("per_vector_bytes=%d is below raw vector size %d — measurement looks broken",
					perVectorBytes, rawBytesPerVector)
			}
		})
	}

	// Count-scaling assertion. If all three count_scale_* scenarios ran,
	// per_tenant_bytes should remain roughly constant as tenant count
	// grows — Option A's working assumption. We allow up to 1.5× drift
	// from the 100-tenant baseline: small-N runs amortize fixed per-process
	// overhead worse than large-N runs, so a modest *decrease* with N is
	// expected; an *increase* would signal that per-tenant container cost
	// dominates and is the failure mode that surfaces the enterprise
	// filtered-HNSW plugin work.
	t.Run("count_scale_linearity", func(t *testing.T) {
		baseline, ok := perTenantBytesByScenario["count_scale_100"]
		if !ok {
			t.Skip("count_scale_100 didn't run; cannot evaluate scaling")
		}
		const maxInflation = 1.5
		for _, name := range []string{"count_scale_500", "count_scale_1000"} {
			got, ok := perTenantBytesByScenario[name]
			if !ok {
				continue
			}
			ratio := float64(got) / float64(baseline)
			t.Logf("HNSW_COUNT_SCALING scenario=%s per_tenant_bytes=%d baseline_bytes=%d ratio=%.3f", name, got, baseline, ratio)
			if ratio > maxInflation {
				t.Errorf("%s per_tenant_bytes %d is %.2fx the 100-tenant baseline %d (max allowed %.2fx) — Option A may not scale linearly; surface enterprise filtered-HNSW plugin work",
					name, got, ratio, baseline, maxInflation)
			}
		}
	})
}

// measureVectorIndexHeapDelta builds a VectorIndex populated with the
// requested (tenants × vectorsPerTenant × dims) shape and returns the
// Go-heap delta in bytes.
//
// Double-GC before each ReadMemStats is the canonical Go-runtime idiom
// for memory-footprint measurement: a single GC() call doesn't guarantee
// the heap is fully drained because Go's GC is concurrent. Without the
// second GC, measurements drift by 10-20%.
func measureVectorIndexHeapDelta(t *testing.T, tenants, vectorsPerTenant, dims int) uint64 {
	t.Helper()

	// Anchor "before" memory after a full settle. Two GCs +
	// ReadMemStats is the documented idiom; runtime's own
	// memory tests use this pattern.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)

	vi := NewVectorIndex()
	zeroVec := make([]float32, dims)

	for tenantIdx := 0; tenantIdx < tenants; tenantIdx++ {
		tenantID := tenantid.TenantID(fmt.Sprintf("tenant-%d", tenantIdx))
		if err := vi.CreateIndexForTenant(tenantID, "embedding", dims, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateIndexForTenant(%s): %v", tenantID, err)
		}
		for vIdx := 0; vIdx < vectorsPerTenant; vIdx++ {
			// nodeID must be unique within a tenant's HNSW; across
			// tenants the nodeIDs can collide harmlessly because
			// VectorIndex's outer map partitions by tenant.
			if err := vi.AddVectorForTenant(tenantID, "embedding", uint64(vIdx)+1, zeroVec); err != nil {
				t.Fatalf("AddVectorForTenant(%s, id=%d): %v", tenantID, vIdx, err)
			}
		}
	}

	// Anchor "after" memory at full settle.
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)

	// HeapAlloc is currently-live heap bytes. Subtracting before from
	// after gives the cost of the structure we just built.
	delta := after.HeapAlloc - before.HeapAlloc

	// Keep vi alive past ReadMemStats so the GC doesn't reclaim it
	// before measurement. The reference is via the deferred sink
	// assignment below; runtime.KeepAlive is the idiomatic guarantee.
	runtime.KeepAlive(vi)
	return delta
}
