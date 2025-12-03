package storage

import (
	"fmt"
	"runtime"
	"testing"
)

// mustNewCompressedEdgeListForBench is a helper for benchmarks
func mustNewCompressedEdgeListForBench(b *testing.B, nodeIDs []uint64) *CompressedEdgeList {
	b.Helper()
	cel, err := NewCompressedEdgeList(nodeIDs)
	if err != nil {
		b.Fatalf("failed to create compressed edge list: %v", err)
	}
	return cel
}

// BenchmarkEdgeStore_CacheHit measures performance when data is in cache
func BenchmarkEdgeStore_CacheHit(b *testing.B) {
	dataDir := b.TempDir()
	es, err := NewEdgeStore(dataDir, 1000)
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Populate with test data
	nodeID := uint64(1)
	edges := make([]uint64, 100)
	for i := range edges {
		edges[i] = uint64(i + 1)
	}

	err = es.StoreOutgoingEdges(nodeID, edges)
	if err != nil {
		b.Fatalf("StoreOutgoingEdges failed: %v", err)
	}

	// Prime the cache
	_, _ = es.GetOutgoingEdges(nodeID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = es.GetOutgoingEdges(nodeID)
	}
}

// BenchmarkEdgeStore_CacheMiss measures performance on cache miss (disk read)
func BenchmarkEdgeStore_CacheMiss(b *testing.B) {
	dataDir := b.TempDir()
	es, err := NewEdgeStore(dataDir, 1) // Tiny cache to force misses
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Populate with test data (multiple nodes)
	const numNodes = 100
	for i := 0; i < numNodes; i++ {
		nodeID := uint64(i + 1)
		edges := []uint64{uint64(i * 10), uint64(i * 10 + 1)}
		err = es.StoreOutgoingEdges(nodeID, edges)
		if err != nil {
			b.Fatalf("StoreOutgoingEdges failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Access different nodes to force cache misses
		nodeID := uint64((i % numNodes) + 1)
		_, _ = es.GetOutgoingEdges(nodeID)
	}
}

// BenchmarkEdgeStore_Write measures write performance
func BenchmarkEdgeStore_Write(b *testing.B) {
	dataDir := b.TempDir()
	es, err := NewEdgeStore(dataDir, 1000)
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	edges := make([]uint64, 100)
	for i := range edges {
		edges[i] = uint64(i + 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeID := uint64(i + 1)
		_ = es.StoreOutgoingEdges(nodeID, edges)
	}
}

// BenchmarkEdgeStore_SmallEdgeList benchmarks small edge lists (10 edges)
func BenchmarkEdgeStore_SmallEdgeList(b *testing.B) {
	dataDir := b.TempDir()
	es, err := NewEdgeStore(dataDir, 1000)
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	edges := make([]uint64, 10)
	for i := range edges {
		edges[i] = uint64(i + 1)
	}

	// Store and prime cache
	es.StoreOutgoingEdges(1, edges)
	es.GetOutgoingEdges(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = es.GetOutgoingEdges(1)
	}
}

// BenchmarkEdgeStore_LargeEdgeList benchmarks large edge lists (10K edges)
func BenchmarkEdgeStore_LargeEdgeList(b *testing.B) {
	dataDir := b.TempDir()
	es, err := NewEdgeStore(dataDir, 1000)
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	edges := make([]uint64, 10000)
	for i := range edges {
		edges[i] = uint64(i + 1)
	}

	// Store and prime cache
	es.StoreOutgoingEdges(1, edges)
	es.GetOutgoingEdges(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = es.GetOutgoingEdges(1)
	}
}

// BenchmarkEdgeCache_Hit benchmarks cache hit performance
func BenchmarkEdgeCache_Hit(b *testing.B) {
	cache := NewEdgeCache(1000)

	edges := mustNewCompressedEdgeListForBench(b, []uint64{1, 2, 3, 4, 5})
	cache.Put("test-key", edges)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Get("test-key")
	}
}

// BenchmarkEdgeCache_Miss benchmarks cache miss performance
func BenchmarkEdgeCache_Miss(b *testing.B) {
	cache := NewEdgeCache(1000)

	// Add some entries, but not the one we'll query
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		edges := mustNewCompressedEdgeListForBench(b, []uint64{uint64(i)})
		cache.Put(key, edges)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Get("nonexistent")
	}
}

// BenchmarkEdgeCache_Put benchmarks cache insertion
func BenchmarkEdgeCache_Put(b *testing.B) {
	cache := NewEdgeCache(10000)

	edges := mustNewCompressedEdgeListForBench(b, []uint64{1, 2, 3, 4, 5})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Put(key, edges)
	}
}

// BenchmarkEdgeStore_CacheSize compares different cache sizes
func BenchmarkEdgeStore_CacheSize(b *testing.B) {
	cacheSizes := []int{10, 100, 1000, 10000}

	for _, cacheSize := range cacheSizes {
		b.Run(fmt.Sprintf("CacheSize%d", cacheSize), func(b *testing.B) {
			dataDir := b.TempDir()
			es, err := NewEdgeStore(dataDir, cacheSize)
			if err != nil {
				b.Fatalf("Failed to create EdgeStore: %v", err)
			}
			defer es.Close()

			// Populate with more nodes than cache size
			const numNodes = 1000
			for i := 0; i < numNodes; i++ {
				nodeID := uint64(i + 1)
				edges := []uint64{uint64(i * 10)}
				es.StoreOutgoingEdges(nodeID, edges)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Zipf-like distribution (some nodes accessed more than others)
				nodeID := uint64((i % 100) + 1) // Hot set of 100 nodes
				_, _ = es.GetOutgoingEdges(nodeID)
			}
		})
	}
}

// BenchmarkMemoryUsage_InMemory measures memory for in-memory edge lists
func BenchmarkMemoryUsage_InMemory(b *testing.B) {
	const numNodes = 10000
	const edgesPerNode = 10

	// Measure baseline memory
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Create in-memory edge lists (old approach)
	edgeLists := make(map[uint64][]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		nodeID := uint64(i + 1)
		edges := make([]uint64, edgesPerNode)
		for j := 0; j < edgesPerNode; j++ {
			edges[j] = uint64(i*10 + j)
		}
		edgeLists[nodeID] = edges
	}

	// Measure after allocation
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	memoryUsed := m2.Alloc - m1.Alloc
	b.ReportMetric(float64(memoryUsed)/1024/1024, "MB")
	b.ReportMetric(float64(memoryUsed)/float64(numNodes), "bytes/node")

	// Keep reference so GC doesn't collect
	_ = len(edgeLists)
}

// BenchmarkMemoryUsage_DiskBacked measures memory for disk-backed edge lists
func BenchmarkMemoryUsage_DiskBacked(b *testing.B) {
	const numNodes = 10000
	const edgesPerNode = 10
	const cacheSize = 100 // Only 100 in cache vs 10000 total

	dataDir := b.TempDir()

	// Measure baseline memory
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Create disk-backed edge store
	es, err := NewEdgeStore(dataDir, cacheSize)
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Store edges (goes to disk)
	for i := 0; i < numNodes; i++ {
		nodeID := uint64(i + 1)
		edges := make([]uint64, edgesPerNode)
		for j := 0; j < edgesPerNode; j++ {
			edges[j] = uint64(i*10 + j)
		}
		es.StoreOutgoingEdges(nodeID, edges)
	}

	// Access some nodes to populate cache
	for i := 0; i < cacheSize; i++ {
		nodeID := uint64(i + 1)
		es.GetOutgoingEdges(nodeID)
	}

	// Measure after storing
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	memoryUsed := m2.Alloc - m1.Alloc
	b.ReportMetric(float64(memoryUsed)/1024/1024, "MB")
	b.ReportMetric(float64(memoryUsed)/float64(numNodes), "bytes/node")
}

// BenchmarkEdgeStore_Throughput measures throughput with concurrent access
func BenchmarkEdgeStore_Throughput(b *testing.B) {
	dataDir := b.TempDir()
	es, err := NewEdgeStore(dataDir, 1000)
	if err != nil {
		b.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Populate
	const numNodes = 1000
	for i := 0; i < numNodes; i++ {
		nodeID := uint64(i + 1)
		edges := []uint64{uint64(i * 10), uint64(i * 10 + 1)}
		es.StoreOutgoingEdges(nodeID, edges)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			nodeID := uint64((i % numNodes) + 1)
			_, _ = es.GetOutgoingEdges(nodeID)
			i++
		}
	})
}
