package storage

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkShardedVsGlobal_ConcurrentNodeCreation compares sharded vs global locking
// for concurrent node creation operations
func BenchmarkShardedVsGlobal_ConcurrentNodeCreation(b *testing.B) {
	tests := []struct {
		name       string
		goroutines int
	}{
		{"01_goroutines", 1},
		{"10_goroutines", 10},
		{"50_goroutines", 50},
		{"100_goroutines", 100},
		{"256_goroutines", 256},
	}

	for _, tt := range tests {
		b.Run("Sharded_"+tt.name, func(b *testing.B) {
			storage := NewGraphStorage()
			benchmarkConcurrentNodeCreation(b, storage, tt.goroutines)
		})

		b.Run("Global_"+tt.name, func(b *testing.B) {
			storage := NewGraphStorageGlobalLock()
			benchmarkConcurrentNodeCreationGlobal(b, storage, tt.goroutines)
		})
	}
}

func benchmarkConcurrentNodeCreation(b *testing.B, storage *GraphStorage, numGoroutines int) {
	b.SetParallelism(numGoroutines)

	var counter uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddUint64(&counter, 1)
			_, err := storage.CreateNode("TestNode", map[string]interface{}{
				"index": int64(idx),
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

func benchmarkConcurrentNodeCreationGlobal(b *testing.B, storage *GraphStorageGlobalLock, numGoroutines int) {
	b.SetParallelism(numGoroutines)

	var counter uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := atomic.AddUint64(&counter, 1)
			_, err := storage.CreateNode("TestNode", map[string]interface{}{
				"index": int64(idx),
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

// BenchmarkShardedVsGlobal_MixedWorkload tests mixed read/write workload
func BenchmarkShardedVsGlobal_MixedWorkload(b *testing.B) {
	tests := []struct {
		name       string
		goroutines int
		readPct    int // Percentage of reads (0-100)
	}{
		{"50read_50write_10goroutines", 10, 50},
		{"50read_50write_100goroutines", 100, 50},
		{"90read_10write_100goroutines", 100, 90},
	}

	for _, tt := range tests {
		b.Run("Sharded_"+tt.name, func(b *testing.B) {
			storage := NewGraphStorage()

			// Pre-populate with nodes
			for i := 0; i < 10000; i++ {
				storage.CreateNode("Person", map[string]interface{}{"id": int64(i)})
			}

			benchmarkMixedWorkload(b, storage, tt.goroutines, tt.readPct)
		})

		b.Run("Global_"+tt.name, func(b *testing.B) {
			storage := NewGraphStorageGlobalLock()

			// Pre-populate with nodes
			for i := 0; i < 10000; i++ {
				storage.CreateNode("Person", map[string]interface{}{"id": int64(i)})
			}

			benchmarkMixedWorkloadGlobal(b, storage, tt.goroutines, tt.readPct)
		})
	}
}

func benchmarkMixedWorkload(b *testing.B, storage *GraphStorage, numGoroutines, readPct int) {
	b.SetParallelism(numGoroutines)

	var counter uint64
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		for pb.Next() {
			if rng.Intn(100) < readPct {
				// Read operation
				nodeID := uint64(rng.Intn(10000)) + 1
				storage.GetNode(nodeID)
			} else {
				// Write operation
				idx := atomic.AddUint64(&counter, 1)
				storage.CreateNode("Person", map[string]interface{}{"id": int64(idx)})
			}
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

func benchmarkMixedWorkloadGlobal(b *testing.B, storage *GraphStorageGlobalLock, numGoroutines, readPct int) {
	b.SetParallelism(numGoroutines)

	var counter uint64
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		for pb.Next() {
			if rng.Intn(100) < readPct {
				// Read operation
				nodeID := uint64(rng.Intn(10000)) + 1
				storage.GetNode(nodeID)
			} else {
				// Write operation
				idx := atomic.AddUint64(&counter, 1)
				storage.CreateNode("Person", map[string]interface{}{"id": int64(idx)})
			}
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

// BenchmarkShardedLocking_Scalability tests how performance scales with goroutines
func BenchmarkShardedLocking_Scalability(b *testing.B) {
	goroutineCounts := []int{1, 2, 4, 8, 16, 32, 64, 128, 256}

	for _, count := range goroutineCounts {
		b.Run(fmt.Sprintf("Sharded_%03d_goroutines", count), func(b *testing.B) {
			storage := NewGraphStorage()
			benchmarkConcurrentNodeCreation(b, storage, count)
		})
	}
}

// BenchmarkShardedLocking_ContentionLevel tests different contention scenarios
func BenchmarkShardedLocking_ContentionLevel(b *testing.B) {
	tests := []struct {
		name        string
		keyRange    int // Range of node IDs to access (smaller = more contention)
		goroutines  int
	}{
		{"HighContention_10keys_100goroutines", 10, 100},
		{"MediumContention_100keys_100goroutines", 100, 100},
		{"LowContention_10000keys_100goroutines", 10000, 100},
	}

	for _, tt := range tests {
		b.Run("Sharded_"+tt.name, func(b *testing.B) {
			storage := NewGraphStorage()

			// Pre-populate nodes
			for i := 0; i < tt.keyRange; i++ {
				storage.CreateNode("Hot", map[string]interface{}{"id": int64(i)})
			}

			b.ResetTimer()
			b.SetParallelism(tt.goroutines)

			b.RunParallel(func(pb *testing.PB) {
				rng := rand.New(rand.NewSource(time.Now().UnixNano()))

				for pb.Next() {
					// Access hot keys repeatedly
					nodeID := uint64(rng.Intn(tt.keyRange)) + 1
					storage.GetNode(nodeID)
				}
			})

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
		})

		b.Run("Global_"+tt.name, func(b *testing.B) {
			storage := NewGraphStorageGlobalLock()

			// Pre-populate nodes
			for i := 0; i < tt.keyRange; i++ {
				storage.CreateNode("Hot", map[string]interface{}{"id": int64(i)})
			}

			b.ResetTimer()
			b.SetParallelism(tt.goroutines)

			b.RunParallel(func(pb *testing.PB) {
				rng := rand.New(rand.NewSource(time.Now().UnixNano()))

				for pb.Next() {
					// Access hot keys repeatedly
					nodeID := uint64(rng.Intn(tt.keyRange)) + 1
					storage.GetNode(nodeID)
				}
			})

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
		})
	}
}

// BenchmarkShardedLocking_LoadDistribution verifies even shard distribution
func BenchmarkShardedLocking_LoadDistribution(b *testing.B) {
	storage := NewGraphStorage()

	// Track which shards are accessed
	shardAccess := make(map[int]uint64)
	var mu sync.Mutex

	b.RunParallel(func(pb *testing.PB) {
		var counter uint64

		for pb.Next() {
			idx := atomic.AddUint64(&counter, 1)
			node, err := storage.CreateNode("Test", map[string]interface{}{"i": int64(idx)})
			if err != nil {
				b.Fatal(err)
			}

			// Track shard for this node ID
			shard := int(node.ID % NumShards)
			mu.Lock()
			shardAccess[shard]++
			mu.Unlock()
		}
	})

	// Report distribution statistics
	var min, max uint64 = ^uint64(0), 0
	var total uint64

	for _, count := range shardAccess {
		total += count
		if count < min {
			min = count
		}
		if count > max {
			max = count
		}
	}

	avg := float64(total) / float64(NumShards)
	deviation := float64(max-min) / avg * 100

	b.ReportMetric(avg, "avg_ops_per_shard")
	b.ReportMetric(deviation, "deviation_%")
}

// TestShardedVsGlobal_Correctness validates both implementations produce same results
func TestShardedVsGlobal_Correctness(t *testing.T) {
	sharded := NewGraphStorage()
	global := NewGraphStorageGlobalLock()

	const numOps = 1000
	const numGoroutines = 10

	var wg sync.WaitGroup

	// Concurrent operations on both storages
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		go func(threadID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				sharded.CreateNode("Test", map[string]interface{}{
					"thread": int64(threadID),
					"index":  int64(j),
				})
			}
		}(i)

		go func(threadID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				global.CreateNode("Test", map[string]interface{}{
					"thread": int64(threadID),
					"index":  int64(j),
				})
			}
		}(i)
	}

	wg.Wait()

	// Verify same node count
	shardedStats := sharded.GetStatistics()
	globalStats := global.GetStatistics()

	if shardedStats.NodeCount != globalStats.NodeCount {
		t.Errorf("node count mismatch: sharded=%d, global=%d",
			shardedStats.NodeCount, globalStats.NodeCount)
	}

	expected := uint64(numOps * numGoroutines)
	if shardedStats.NodeCount != expected {
		t.Errorf("sharded: expected %d nodes, got %d", expected, shardedStats.NodeCount)
	}
	if globalStats.NodeCount != expected {
		t.Errorf("global: expected %d nodes, got %d", expected, globalStats.NodeCount)
	}

	t.Logf("Both implementations correctly created %d nodes", expected)
}

// TestShardedLocking_NoRaceConditions runs with -race flag to detect races
func TestShardedLocking_NoRaceConditions(t *testing.T) {
	storage := NewGraphStorage()

	const numGoroutines = 100
	const numOps = 1000

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(threadID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				_, err := storage.CreateNode("Test", map[string]interface{}{
					"thread": int64(threadID),
					"index":  int64(j),
				})
				if err != nil {
					t.Errorf("CreateNode failed: %v", err)
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))

			for j := 0; j < numOps; j++ {
				nodeID := uint64(rng.Intn(numGoroutines*numOps)) + 1
				storage.GetNode(nodeID) // Ignore errors (node may not exist yet)
			}
		}()
	}

	wg.Wait()

	stats := storage.GetStatistics()
	expected := uint64(numGoroutines * numOps)

	if stats.NodeCount != expected {
		t.Errorf("expected %d nodes, got %d", expected, stats.NodeCount)
	}

	t.Logf("Completed %d concurrent operations with no race conditions", expected)
}
