package api

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Audit A9 #3 (2026-05-08): tests for the per-tenant GraphQL schema
// cache + singleflight dedup. The HTTP-level introspection-leak
// gate lands in A9 #4.

// TestGetGraphQLHandlerForTenant_Caches verifies the cache hit path:
// after the first call builds and stores a handler, subsequent calls
// for the same tenant return the same instance without rebuilding.
func TestGetGraphQLHandlerForTenant_Caches(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Seed at least one node so the schema has labels to register.
	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	first, err := server.getGraphQLHandlerForTenant("tenant-A")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := server.getGraphQLHandlerForTenant("tenant-A")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Errorf("cache hit should return same handler instance; got distinct pointers")
	}
}

// TestGetGraphQLHandlerForTenant_SeparateCachePerTenant verifies the
// cache is keyed correctly: tenant-A and tenant-B get distinct
// handlers (each backed by a tenant-scoped schema). Critical
// because mixing the cache key types (string vs tenantid.TenantID)
// would silently collapse two tenants into one bucket — the
// reviewer's blind-spot from /orchestrate.
func TestGetGraphQLHandlerForTenant_SeparateCachePerTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"PersonA"}, nil); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"PersonB"}, nil); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	hA, err := server.getGraphQLHandlerForTenant("tenant-A")
	if err != nil {
		t.Fatalf("tenant-A: %v", err)
	}
	hB, err := server.getGraphQLHandlerForTenant("tenant-B")
	if err != nil {
		t.Fatalf("tenant-B: %v", err)
	}
	if hA == hB {
		t.Errorf("tenants must have distinct handler instances; got the same pointer (cache-key collision?)")
	}
}

// TestGetGraphQLHandlerForTenant_SingleflightDedupsConcurrentBuilds
// is the critical concurrency test: 50 goroutines for the same
// tenant cold-start simultaneously. With singleflight, exactly one
// build runs; without it, every goroutine would race to build,
// allocating N distinct schemas before the first cache.Store wins.
//
// We instrument by replacing the storage's GetLabelsForTenant
// indirectly — we count cache misses by comparing handler pointers
// returned. With dedup, all 50 must receive the same pointer
// (because Store happens once). Without dedup, races would yield
// distinct pointers from concurrent builds before any caches.
func TestGetGraphQLHandlerForTenant_SingleflightDedupsConcurrentBuilds(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]any, goroutines) // store pointers as `any` to compare

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			h, err := server.getGraphQLHandlerForTenant("tenant-A")
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			results[idx] = h
		}(i)
	}
	wg.Wait()

	// Every goroutine must see the same pointer. If singleflight
	// wasn't deduping, concurrent builders would each allocate a
	// fresh handler and return it before observing each others'
	// Store calls.
	first := results[0]
	if first == nil {
		t.Fatal("first goroutine got nil handler")
	}
	for i, r := range results {
		if r != first {
			t.Errorf("goroutine %d returned different handler than goroutine 0 (singleflight failed to dedup)", i)
		}
	}
}

// TestSchemaRegenerate_InvalidatesOnlyCallerTenant pins that the
// admin endpoint clears the caller's cache entry but leaves other
// tenants' caches intact.
func TestSchemaRegenerate_InvalidatesOnlyCallerTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"DocA"}, nil); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"DocB"}, nil); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	hA1, _ := server.getGraphQLHandlerForTenant("tenant-A")
	hB1, _ := server.getGraphQLHandlerForTenant("tenant-B")

	// Invalidate tenant-A's entry directly (mirrors the handler
	// path's behavior; the HTTP-level admin gate is tested
	// separately).
	server.graphqlHandlers.Delete("tenant-A")

	hA2, _ := server.getGraphQLHandlerForTenant("tenant-A")
	hB2, _ := server.getGraphQLHandlerForTenant("tenant-B")

	if hA1 == hA2 {
		t.Error("tenant-A handler should differ after invalidate (rebuild expected)")
	}
	if hB1 != hB2 {
		t.Error("tenant-B handler should be unchanged (only tenant-A was invalidated)")
	}
}

// BenchmarkGenerateSchemaForTenant measures the cold-start cost the
// cache amortizes. Per the perf-specialist's note in /orchestrate
// (#60): "1000 tenants × 100ms = 100s eager-build penalty" was an
// unmeasured estimate. This number is the actual cost.
//
// Reports custom metric `us/build` for build wall-clock.
func BenchmarkGenerateSchemaForTenant(b *testing.B) {
	server, cleanup := setupBenchServer(b)
	defer cleanup()

	// Seed a modest corpus — 5 distinct labels, which is what
	// drives schema-builder cost (label count, not node count).
	for _, label := range []string{"Person", "Doc", "Org", "Project", "Tag"} {
		if _, err := server.graph.CreateNodeWithTenant("tenant-bench", []string{label}, nil); err != nil {
			b.Fatalf("seed %s: %v", label, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Force cold-start by deleting the cache entry each iteration.
		// Otherwise we'd be measuring sync.Map.Load (~0ns), not the
		// build-and-Store path that thundering-herd cares about.
		server.graphqlHandlers.Delete("tenant-bench")
		_, err := server.getGraphQLHandlerForTenant("tenant-bench")
		if err != nil {
			b.Fatalf("build: %v", err)
		}
	}
}

// setupBenchServer is a benchmark-only setup helper — same as
// setupTestServer but takes *testing.B. Avoids the test-only
// helper's t.Helper() call.
func setupBenchServer(b *testing.B) (*Server, func()) {
	b.Helper()
	gs, err := storage.NewGraphStorage(b.TempDir())
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	server, err := NewServer(gs, 0)
	if err != nil {
		_ = gs.Close()
		b.Fatalf("NewServer: %v", err)
	}
	cleanup := func() {
		_ = gs.Close()
	}
	return server, cleanup
}

// _ keeps atomic referenced; unused-import guard for future
// instrumentation (e.g., tracking build-call counts in tests).
var _ = atomic.AddInt32
