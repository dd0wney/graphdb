package search

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Four hybrid-search-motivated benchmark scenarios that were open on
// the plan (#11). Captured as Go benchmarks rather than a standalone
// cmd/benchmark-search CLI so they run in CI via `go test -bench` and
// report in standard Go benchmark format. The cold-cache scenario is
// inherently environmental (needs OS page-cache eviction) and is
// approximated here by "fresh index per iteration" which measures the
// first-query-after-rebuild cost — a reasonable proxy.

// corpus builds a tenant-scoped corpus of N docs with label "Doc",
// each containing "common" plus a per-doc unique token, plus some
// boilerplate text. Half of the docs also carry an "Archive" label
// so label-filter benches have two populations to select between.
func corpus(b *testing.B, gs *storage.GraphStorage, tenantID string, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		labels := []string{"Doc"}
		if i%2 == 0 {
			labels = append(labels, "Archive")
		}
		if _, err := gs.CreateNodeWithTenant(tenantID, labels, map[string]storage.Value{
			"body": storage.StringValue(fmt.Sprintf("common uniq%d lorem ipsum dolor sit amet", i)),
		}); err != nil {
			b.Fatalf("CreateNode %d: %v", i, err)
		}
	}
}

// BenchmarkScenario_SingleTermWarm is the canonical "most common query"
// baseline. The same common token hits every indexed doc; the bench
// measures the full FTS pipeline (token normalize, posting lookup,
// score, hydrate top-K) on a warm index.
func BenchmarkScenario_SingleTermWarm(b *testing.B) {
	tmpDir := b.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const n = 2000 // CI-friendly; scale up locally for production-scale numbers.
	corpus(b, gs, "default", n)

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("default", []string{"Doc"}, []string{"body"}); err != nil {
		b.Fatalf("IndexForTenant: %v", err)
	}
	idx := ti.Get("default")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := idx.SearchTopK("common", 20)
		if err != nil || len(r) == 0 {
			b.Fatalf("SearchTopK: err=%v len=%d", err, len(r))
		}
	}
}

// BenchmarkScenario_ConcurrentReadDuringWrite measures read latency
// while the index is being continuously updated by a writer goroutine.
// This exercises the RLock/WLock interaction that the nodeTerms reverse
// posting made O(doc-terms) instead of O(vocab). A background writer
// calls UpdateNode on a rotating set of nodes; the measured loop
// issues parallel Search calls.
func BenchmarkScenario_ConcurrentReadDuringWrite(b *testing.B) {
	tmpDir := b.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const n = 2000
	corpus(b, gs, "default", n)

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("default", []string{"Doc"}, []string{"body"}); err != nil {
		b.Fatalf("IndexForTenant: %v", err)
	}
	idx := ti.Get("default")

	// Gather the NodeIDs we just created so the writer can rotate over
	// them with UpdateNode calls.
	var targets []uint64
	for _, node := range gs.GetNodesByLabelForTenant("default", "Doc") {
		targets = append(targets, node.ID)
	}
	if len(targets) == 0 {
		b.Fatal("no targets")
	}

	done := make(chan struct{})
	var writeCount atomic.Uint64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-done:
				return
			default:
				_ = idx.UpdateNode(targets[i%len(targets)])
				writeCount.Add(1)
				i++
			}
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r, err := idx.SearchTopK("common", 20)
			if err != nil || len(r) == 0 {
				b.Fatalf("SearchTopK under contention: err=%v len=%d", err, len(r))
			}
		}
	})
	b.StopTimer()

	close(done)
	wg.Wait()
	b.ReportMetric(float64(writeCount.Load()), "writes/total")
}

// BenchmarkScenario_LabelFilterDelta compares two variants of the same
// query: one with no label filter (all 2000 candidates score + hydrate
// via SearchTopK), one with a label filter that selects half the
// corpus. The handler applies label filters post-search in user code,
// not inside SearchTopK, so this bench stresses the SearchTopK API
// first — the handler-layer filter delta lives in the API tests.
func BenchmarkScenario_LabelFilterDelta(b *testing.B) {
	tmpDir := b.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const n = 2000
	corpus(b, gs, "default", n)

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("default", []string{"Doc", "Archive"}, []string{"body"}); err != nil {
		b.Fatalf("IndexForTenant: %v", err)
	}
	idx := ti.Get("default")

	b.Run("no_label_filter", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r, err := idx.SearchTopK("common", 20)
			if err != nil || len(r) == 0 {
				b.Fatalf("SearchTopK: %v %d", err, len(r))
			}
		}
	})

	b.Run("post_filter_archive_only", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r, err := idx.SearchTopK("common", 20)
			if err != nil || len(r) == 0 {
				b.Fatalf("SearchTopK: %v %d", err, len(r))
			}
			kept := 0
			for _, res := range r {
				if res.Node == nil {
					continue
				}
				for _, label := range res.Node.Labels {
					if label == "Archive" {
						kept++
						break
					}
				}
			}
			_ = kept
		}
	})
}

// BenchmarkScenario_MultiTenantIsolation measures the per-tenant
// lookup + per-tenant index build/access cost. Two tenants each with
// a 1000-doc corpus; the benchmark alternates between them to
// exercise the TenantIndexes map + per-tenant index state. Scaling
// here is the signal for "adding tenants N→N+1 does not materially
// slow tenant N's queries" (RLock contention on the map should not
// show up since it's RWMutex-protected and reads are cheap).
func BenchmarkScenario_MultiTenantIsolation(b *testing.B) {
	tmpDir := b.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const perTenant = 1000
	corpus(b, gs, "tenant-A", perTenant)
	corpus(b, gs, "tenant-B", perTenant)

	ti := NewTenantIndexes(gs)
	if err := ti.IndexForTenant("tenant-A", []string{"Doc"}, []string{"body"}); err != nil {
		b.Fatalf("IndexForTenant A: %v", err)
	}
	if err := ti.IndexForTenant("tenant-B", []string{"Doc"}, []string{"body"}); err != nil {
		b.Fatalf("IndexForTenant B: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tenantID := "tenant-A"
		if i&1 == 1 {
			tenantID = "tenant-B"
		}
		idx := ti.Get(tenantID)
		r, err := idx.SearchTopK("common", 20)
		if err != nil || len(r) == 0 {
			b.Fatalf("SearchTopK %s: %v %d", tenantID, err, len(r))
		}
	}
}

// BenchmarkScenario_FirstQueryAfterRebuild measures the cost of the
// first query against a freshly-rebuilt index. This is the closest
// in-process proxy for cold-cache behavior — at the Go level we can't
// evict the OS page cache, but rebuilding the index forces every
// subsequent SearchTopK through newly-allocated maps with no per-
// iteration warm-up. The b.N loop resets the index inside each
// iteration to isolate rebuild + first-query cost.
func BenchmarkScenario_FirstQueryAfterRebuild(b *testing.B) {
	tmpDir := b.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const n = 1000
	corpus(b, gs, "default", n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Evict & rebuild. The TenantIndexes API doesn't expose a Drop,
		// so we construct a fresh TenantIndexes each iteration, which
		// guarantees "first access" cost for the tenant.
		fresh := NewTenantIndexes(gs)
		if err := fresh.IndexForTenant("default", []string{"Doc"}, []string{"body"}); err != nil {
			b.Fatalf("IndexForTenant: %v", err)
		}
		r, err := fresh.Get("default").SearchTopK("common", 20)
		if err != nil || len(r) == 0 {
			b.Fatalf("SearchTopK: %v %d", err, len(r))
		}
	}
}
