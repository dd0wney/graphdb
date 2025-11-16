package storage

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// TestCache_HitVsMissLatency compares cache hit vs miss latencies
func TestCache_HitVsMissLatency(t *testing.T) {
	storage := NewGraphStorage()
	storage.config.UseDiskBackedEdges = true
	storage.initEdgeStore()

	// Create test nodes with edges
	const numNodes = 1000
	nodeIDs := make([]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		node, err := storage.CreateNode("Test", map[string]interface{}{
			"index": int64(i),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		nodeIDs[i] = node.ID

		// Add 10 edges per node
		for j := 0; j < 10; j++ {
			toIdx := (i + j + 1) % numNodes
			storage.CreateEdge(node.ID, nodeIDs[toIdx], "CONNECTS", nil)
		}
	}

	// Flush to ensure edges are on disk
	storage.edgeStore.cache.mu.Lock()
	for key := range storage.edgeStore.cache.items {
		delete(storage.edgeStore.cache.items, key)
	}
	storage.edgeStore.cache.mu.Unlock()

	// Measure cache miss latency (cold read from disk)
	missStart := time.Now()
	const numMissReads = 100
	for i := 0; i < numMissReads; i++ {
		nodeID := nodeIDs[i]
		storage.GetOutgoingEdges(nodeID)
	}
	missLatency := time.Since(missStart) / numMissReads

	// Measure cache hit latency (warm read from cache)
	hitStart := time.Now()
	const numHitReads = 100
	for i := 0; i < numHitReads; i++ {
		nodeID := nodeIDs[i] // Same nodes, should be cached now
		storage.GetOutgoingEdges(nodeID)
	}
	hitLatency := time.Since(hitStart) / numHitReads

	speedup := float64(missLatency) / float64(hitLatency)

	t.Logf("Cache Miss Latency: %v", missLatency)
	t.Logf("Cache Hit Latency:  %v", hitLatency)
	t.Logf("Speedup: %.2fx", speedup)

	// Verify cache hit is significantly faster
	if speedup < 5.0 {
		t.Errorf("cache hit speedup too low: %.2fx (expected >5x)", speedup)
	}

	// Verify cache statistics
	stats := storage.edgeStore.cache.GetStats()
	t.Logf("Cache Stats: Hits=%d, Misses=%d, Hit Rate=%.2f%%",
		stats.Hits, stats.Misses, stats.HitRate*100)

	if stats.Hits == 0 {
		t.Error("expected some cache hits")
	}
	if stats.Misses == 0 {
		t.Error("expected some cache misses")
	}
}

// BenchmarkCache_HitVsMiss benchmarks cache hit vs miss performance
func BenchmarkCache_HitVsMiss(b *testing.B) {
	storage := NewGraphStorage()
	storage.config.UseDiskBackedEdges = true
	storage.initEdgeStore()

	// Create nodes with edges
	const numNodes = 10000
	nodeIDs := make([]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
		nodeIDs[i] = node.ID

		// Add edges
		for j := 0; j < 10; j++ {
			toIdx := (i + j + 1) % numNodes
			storage.CreateEdge(node.ID, nodeIDs[toIdx], "EDGE", nil)
		}
	}

	b.Run("CacheHit", func(b *testing.B) {
		// Pre-warm cache with first 100 nodes
		for i := 0; i < 100; i++ {
			storage.GetOutgoingEdges(nodeIDs[i])
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			nodeID := nodeIDs[i%100] // Cycle through cached nodes
			storage.GetOutgoingEdges(nodeID)
		}
	})

	b.Run("CacheMiss", func(b *testing.B) {
		// Clear cache
		storage.edgeStore.cache.mu.Lock()
		for key := range storage.edgeStore.cache.items {
			delete(storage.edgeStore.cache.items, key)
		}
		storage.edgeStore.cache.mu.Unlock()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			nodeID := nodeIDs[i%numNodes] // Access different nodes (cache misses)
			storage.GetOutgoingEdges(nodeID)
		}
	})
}

// TestCache_HitRateWithTypicalWorkload tests realistic 80/20 access pattern
func TestCache_HitRateWithTypicalWorkload(t *testing.T) {
	storage := NewGraphStorage()
	storage.config.UseDiskBackedEdges = true
	storage.config.CacheSize = 1000 // Small cache to test eviction
	storage.initEdgeStore()

	// Create nodes
	const numNodes = 5000
	nodeIDs := make([]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
		nodeIDs[i] = node.ID

		for j := 0; j < 10; j++ {
			toIdx := (i + j + 1) % numNodes
			storage.CreateEdge(node.ID, nodeIDs[toIdx], "EDGE", nil)
		}
	}

	// Reset cache stats
	storage.edgeStore.cache.hits = 0
	storage.edgeStore.cache.misses = 0

	// Simulate 80/20 workload: 20% of nodes accessed 80% of the time
	const totalAccesses = 10000
	const hotSetSize = numNodes / 5 // 20% of nodes

	rng := rand.New(rand.NewSource(42))

	for i := 0; i < totalAccesses; i++ {
		var nodeID uint64

		if rng.Float64() < 0.80 {
			// 80% of accesses go to hot set (20% of nodes)
			nodeID = nodeIDs[rng.Intn(hotSetSize)]
		} else {
			// 20% of accesses go to cold set (80% of nodes)
			nodeID = nodeIDs[hotSetSize+rng.Intn(numNodes-hotSetSize)]
		}

		storage.GetOutgoingEdges(nodeID)
	}

	stats := storage.edgeStore.cache.GetStats()

	t.Logf("80/20 Workload Results:")
	t.Logf("  Total Accesses: %d", totalAccesses)
	t.Logf("  Cache Hits:     %d", stats.Hits)
	t.Logf("  Cache Misses:   %d", stats.Misses)
	t.Logf("  Hit Rate:       %.2f%%", stats.HitRate*100)

	// With 80/20 pattern and cache size = 1000 (covers hot set of 1000 nodes),
	// we expect hit rate around 70-80%
	if stats.HitRate < 0.70 {
		t.Errorf("hit rate too low: %.2f%% (expected >70%%)", stats.HitRate*100)
	}

	if stats.HitRate > 0.90 {
		t.Errorf("hit rate suspiciously high: %.2f%% (expected <90%%)", stats.HitRate*100)
	}
}

// TestCache_WorkloadPatterns tests different access patterns
func TestCache_WorkloadPatterns(t *testing.T) {
	tests := []struct {
		name           string
		cacheSize      int
		numNodes       int
		accessPattern  func(int, int) int // (iteration, numNodes) -> nodeIndex
		expectedHitMin float64
		expectedHitMax float64
	}{
		{
			name:      "Sequential_NoCaching",
			cacheSize: 100,
			numNodes:  1000,
			accessPattern: func(i, n int) int {
				return i % n // Sequential access, no reuse
			},
			expectedHitMin: 0.0,
			expectedHitMax: 0.15, // Very low hit rate
		},
		{
			name:      "Repeated_FullCaching",
			cacheSize: 100,
			numNodes:  50, // Fewer nodes than cache size
			accessPattern: func(i, n int) int {
				return i % n // All nodes fit in cache
			},
			expectedHitMin: 0.90, // Very high hit rate
			expectedHitMax: 1.00,
		},
		{
			name:      "Zipf_Realistic",
			cacheSize: 500,
			numNodes:  5000,
			accessPattern: func(i, n int) int {
				// Zipf-like: some nodes accessed much more frequently
				if i%10 < 8 {
					return i % (n / 10) // 80% access first 10% of nodes
				}
				return (n / 10) + (i % (n - n/10))
			},
			expectedHitMin: 0.70,
			expectedHitMax: 0.90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewGraphStorage()
			storage.config.UseDiskBackedEdges = true
			storage.config.CacheSize = tt.cacheSize
			storage.initEdgeStore()

			// Create nodes
			nodeIDs := make([]uint64, tt.numNodes)
			for i := 0; i < tt.numNodes; i++ {
				node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
				nodeIDs[i] = node.ID

				for j := 0; j < 5; j++ {
					toIdx := (i + j + 1) % tt.numNodes
					storage.CreateEdge(node.ID, nodeIDs[toIdx], "EDGE", nil)
				}
			}

			// Reset stats
			storage.edgeStore.cache.hits = 0
			storage.edgeStore.cache.misses = 0

			// Run access pattern
			const numAccesses = 10000
			for i := 0; i < numAccesses; i++ {
				nodeIdx := tt.accessPattern(i, tt.numNodes)
				storage.GetOutgoingEdges(nodeIDs[nodeIdx])
			}

			stats := storage.edgeStore.cache.GetStats()

			t.Logf("Hit Rate: %.2f%%", stats.HitRate*100)

			if stats.HitRate < tt.expectedHitMin {
				t.Errorf("hit rate %.2f%% below expected minimum %.2f%%",
					stats.HitRate*100, tt.expectedHitMin*100)
			}

			if stats.HitRate > tt.expectedHitMax {
				t.Errorf("hit rate %.2f%% above expected maximum %.2f%%",
					stats.HitRate*100, tt.expectedHitMax*100)
			}
		})
	}
}

// TestCache_Eviction validates LRU eviction policy
func TestCache_Eviction(t *testing.T) {
	storage := NewGraphStorage()
	storage.config.UseDiskBackedEdges = true
	storage.config.CacheSize = 10 // Very small cache
	storage.initEdgeStore()

	// Create 20 nodes (2x cache size)
	const numNodes = 20
	nodeIDs := make([]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
		nodeIDs[i] = node.ID

		storage.CreateEdge(node.ID, nodeIDs[(i+1)%numNodes], "EDGE", nil)
	}

	// Access first 10 nodes (fill cache)
	for i := 0; i < 10; i++ {
		storage.GetOutgoingEdges(nodeIDs[i])
	}

	initialHits := storage.edgeStore.cache.hits
	initialMisses := storage.edgeStore.cache.misses

	// Access first 10 nodes again (should all be cache hits)
	for i := 0; i < 10; i++ {
		storage.GetOutgoingEdges(nodeIDs[i])
	}

	newHits := storage.edgeStore.cache.hits - initialHits
	if newHits != 10 {
		t.Errorf("expected 10 cache hits, got %d", newHits)
	}

	// Access next 10 nodes (should be cache misses and evict old entries)
	for i := 10; i < 20; i++ {
		storage.GetOutgoingEdges(nodeIDs[i])
	}

	// Access first 10 nodes again (should be misses, as they were evicted)
	preEvictionMisses := storage.edgeStore.cache.misses
	for i := 0; i < 10; i++ {
		storage.GetOutgoingEdges(nodeIDs[i])
	}
	postEvictionMisses := storage.edgeStore.cache.misses - preEvictionMisses

	if postEvictionMisses < 5 {
		t.Errorf("expected at least 5 cache misses after eviction, got %d", postEvictionMisses)
	}

	t.Logf("LRU eviction working correctly: %d misses after eviction", postEvictionMisses)
}

// BenchmarkCache_DifferentSizes benchmarks cache with various sizes
func BenchmarkCache_DifferentSizes(b *testing.B) {
	cacheSizes := []int{100, 1000, 10000, 100000}

	for _, size := range cacheSizes {
		b.Run(fmt.Sprintf("CacheSize_%d", size), func(b *testing.B) {
			storage := NewGraphStorage()
			storage.config.UseDiskBackedEdges = true
			storage.config.CacheSize = size
			storage.initEdgeStore()

			// Create nodes (10x cache size)
			numNodes := size * 10
			nodeIDs := make([]uint64, numNodes)

			for i := 0; i < numNodes; i++ {
				node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
				nodeIDs[i] = node.ID

				for j := 0; j < 5; j++ {
					toIdx := (i + j + 1) % numNodes
					storage.CreateEdge(node.ID, nodeIDs[toIdx], "EDGE", nil)
				}
			}

			// Reset stats
			storage.edgeStore.cache.hits = 0
			storage.edgeStore.cache.misses = 0

			b.ResetTimer()

			// 80/20 access pattern
			rng := rand.New(rand.NewSource(42))
			hotSetSize := numNodes / 5

			for i := 0; i < b.N; i++ {
				var nodeID uint64
				if rng.Float64() < 0.80 {
					nodeID = nodeIDs[rng.Intn(hotSetSize)]
				} else {
					nodeID = nodeIDs[hotSetSize+rng.Intn(numNodes-hotSetSize)]
				}
				storage.GetOutgoingEdges(nodeID)
			}

			stats := storage.edgeStore.cache.GetStats()
			b.ReportMetric(stats.HitRate*100, "hit_rate_%")
			b.ReportMetric(float64(stats.Hits), "hits")
			b.ReportMetric(float64(stats.Misses), "misses")
		})
	}
}

// TestCache_ConcurrentAccess validates thread-safe cache access
func TestCache_ConcurrentAccess(t *testing.T) {
	storage := NewGraphStorage()
	storage.config.UseDiskBackedEdges = true
	storage.config.CacheSize = 1000
	storage.initEdgeStore()

	// Create nodes
	const numNodes = 500
	nodeIDs := make([]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
		nodeIDs[i] = node.ID

		for j := 0; j < 10; j++ {
			toIdx := (i + j + 1) % numNodes
			storage.CreateEdge(node.ID, nodeIDs[toIdx], "EDGE", nil)
		}
	}

	// Concurrent access to cache
	const numGoroutines = 50
	const accessesPerGoroutine = 1000

	done := make(chan bool, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(threadID int) {
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(threadID)))

			for i := 0; i < accessesPerGoroutine; i++ {
				nodeID := nodeIDs[rng.Intn(numNodes)]
				storage.GetOutgoingEdges(nodeID)
			}

			done <- true
		}(g)
	}

	// Wait for all goroutines
	for g := 0; g < numGoroutines; g++ {
		<-done
	}

	stats := storage.edgeStore.cache.GetStats()

	t.Logf("Concurrent Access Results:")
	t.Logf("  Total Accesses: %d", numGoroutines*accessesPerGoroutine)
	t.Logf("  Cache Hits:     %d", stats.Hits)
	t.Logf("  Cache Misses:   %d", stats.Misses)
	t.Logf("  Hit Rate:       %.2f%%", stats.HitRate*100)

	totalAccesses := uint64(numGoroutines * accessesPerGoroutine)
	if stats.Hits+stats.Misses != totalAccesses {
		t.Errorf("hit+miss count mismatch: %d+%d != %d",
			stats.Hits, stats.Misses, totalAccesses)
	}

	// Should have reasonable hit rate with random access
	if stats.HitRate < 0.30 {
		t.Errorf("hit rate too low: %.2f%%", stats.HitRate*100)
	}
}

// TestCache_Statistics validates cache statistics tracking
func TestCache_Statistics(t *testing.T) {
	storage := NewGraphStorage()
	storage.config.UseDiskBackedEdges = true
	storage.config.CacheSize = 100
	storage.initEdgeStore()

	// Create nodes
	nodeIDs := make([]uint64, 200)
	for i := 0; i < 200; i++ {
		node, _ := storage.CreateNode("Test", map[string]interface{}{"i": int64(i)})
		nodeIDs[i] = node.ID
		storage.CreateEdge(node.ID, nodeIDs[(i+1)%200], "EDGE", nil)
	}

	// Initial stats should be zero
	stats := storage.edgeStore.cache.GetStats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("initial stats not zero: hits=%d, misses=%d", stats.Hits, stats.Misses)
	}

	// Access node (cache miss)
	storage.GetOutgoingEdges(nodeIDs[0])
	stats = storage.edgeStore.cache.GetStats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}

	// Access same node (cache hit)
	storage.GetOutgoingEdges(nodeIDs[0])
	stats = storage.edgeStore.cache.GetStats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}

	// Verify hit rate calculation
	expectedHitRate := float64(stats.Hits) / float64(stats.Hits+stats.Misses)
	if stats.HitRate != expectedHitRate {
		t.Errorf("hit rate mismatch: got %.4f, expected %.4f", stats.HitRate, expectedHitRate)
	}
}
