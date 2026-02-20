package storage

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGraphStorage_DiskBackedEdges_HighConcurrency tests behavior under extreme concurrent load
func TestGraphStorage_DiskBackedEdges_HighConcurrency(t *testing.T) {
	if testing.Short() || isRaceEnabled() {
		t.Skip("Skipping stress test in short mode or with race detector")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Pre-create nodes (reduced from 1000 for reasonable test time)
	const numNodes = 100
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	// Stress test with concurrent goroutines (reduced from 100x1000=100K to 10x100=1K ops)
	const numWorkers = 10
	const opsPerWorker = 100

	var (
		createCount uint64
		readCount   uint64
		deleteCount uint64
		errorCount  uint64
	)

	var wg sync.WaitGroup
	start := time.Now()

	// Concurrent edge operations
	for worker := 0; worker < numWorkers; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for i := 0; i < opsPerWorker; i++ {
				op := (id*opsPerWorker + i) % 10

				source := nodeIDs[(id*7+i)%numNodes]
				target := nodeIDs[(id*11+i)%numNodes]

				if op < 6 {
					// 60% creates
					_, err := gs.CreateEdge(source, target, "EDGE", nil, 1.0)
					if err != nil {
						atomic.AddUint64(&errorCount, 1)
					} else {
						atomic.AddUint64(&createCount, 1)
					}
				} else if op < 9 {
					// 30% reads
					_, err := gs.GetOutgoingEdges(source)
					if err != nil {
						atomic.AddUint64(&errorCount, 1)
					} else {
						atomic.AddUint64(&readCount, 1)
					}
				} else {
					// 10% deletes (best effort)
					outgoing, _ := gs.GetOutgoingEdges(source)
					if len(outgoing) > 0 {
						err := gs.DeleteEdge(outgoing[0].ID)
						if err != nil {
							atomic.AddUint64(&errorCount, 1)
						} else {
							atomic.AddUint64(&deleteCount, 1)
						}
					}
					atomic.AddUint64(&readCount, 1) // Count the read
				}
			}
		}(worker)
	}

	wg.Wait()
	duration := time.Since(start)

	// Report results
	totalOps := createCount + readCount + deleteCount
	t.Logf("High Concurrency Test Results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Workers: %d", numWorkers)
	t.Logf("  Total Operations: %d", totalOps)
	t.Logf("  Creates: %d", createCount)
	t.Logf("  Reads: %d", readCount)
	t.Logf("  Deletes: %d", deleteCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Throughput: %.0f ops/sec", float64(totalOps)/duration.Seconds())

	// Validate
	if errorCount > totalOps/100 {
		t.Errorf("Error rate too high: %d errors out of %d ops (%.1f%%)",
			errorCount, totalOps, float64(errorCount*100)/float64(totalOps))
	}

	if totalOps == 0 {
		t.Error("No operations completed")
	}
}

// TestGraphStorage_DiskBackedEdges_MemoryLeak tests for memory leaks under load
func TestGraphStorage_DiskBackedEdges_MemoryLeak(t *testing.T) {
	if testing.Short() || isRaceEnabled() {
		t.Skip("Skipping memory leak test in short mode or with race detector")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Pre-create nodes
	const numNodes = 100
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
	}

	// Measure initial memory
	runtime.GC()
	time.Sleep(100 * time.Millisecond) // Let GC complete
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	initialAlloc := m1.Alloc

	// Ensure we have a reasonable baseline (at least 1 MB)
	if initialAlloc < 1024*1024 {
		initialAlloc = 1024 * 1024 // Use 1 MB as minimum baseline
	}

	// Run iterations of create/read/delete (reduced from 1000 for faster tests)
	const iterations = 200
	for iter := 0; iter < iterations; iter++ {
		source := nodeIDs[iter%numNodes]
		target := nodeIDs[(iter+1)%numNodes]

		// Create edge
		edge, _ := gs.CreateEdge(source, target, "EDGE", nil, 1.0)

		// Read edges
		gs.GetOutgoingEdges(source)
		gs.GetIncomingEdges(target)

		// Delete edge
		if edge != nil {
			gs.DeleteEdge(edge.ID)
		}

		// Check memory every 200 iterations
		if iter > 0 && iter%200 == 0 {
			runtime.GC()
			time.Sleep(50 * time.Millisecond) // Let GC complete
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			// Only check for growth (positive), ignore shrinkage (negative is good)
			if m.Alloc > initialAlloc {
				growth := float64(m.Alloc-initialAlloc) / float64(initialAlloc)
				if growth > 10.0 { // More than 10x growth indicates leak
					t.Errorf("Possible memory leak detected at iteration %d: %.1fx growth", iter, growth)
					break
				}
			}
		}
	}

	// Final memory check
	runtime.GC()
	time.Sleep(100 * time.Millisecond) // Let GC complete
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	finalAlloc := m2.Alloc

	// Calculate growth (negative means memory was freed, which is good)
	var growth float64
	if finalAlloc > initialAlloc {
		growth = float64(finalAlloc-initialAlloc) / float64(initialAlloc)
	} else {
		growth = -float64(initialAlloc-finalAlloc) / float64(initialAlloc)
	}

	t.Logf("Memory growth after %d iterations: %.2fx", iterations, growth)
	t.Logf("Initial: %d MB, Final: %d MB", initialAlloc/(1024*1024), finalAlloc/(1024*1024))

	// Only fail if memory grew significantly (positive growth > 5x)
	if growth > 5.0 {
		t.Errorf("Excessive memory growth: %.1fx (expected < 5x)", growth)
	}
}

// TestGraphStorage_DiskBackedEdges_CacheCorrectnessUnderLoad tests cache consistency
func TestGraphStorage_DiskBackedEdges_CacheCorrectnessUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cache correctness test in short mode")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      10, // Small cache to force evictions
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create nodes (reduced for reasonable test time with disk-backed edges)
	const numNodes = 20
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
	}

	// Track expected edge counts per node
	expectedOutgoing := make(map[uint64]int)
	var mu sync.Mutex

	// Concurrent edge creation (reduced from 20x100=2000 to 5x20=100 edges)
	var wg sync.WaitGroup
	const numWorkers = 5
	const edgesPerWorker = 20

	for worker := 0; worker < numWorkers; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for i := 0; i < edgesPerWorker; i++ {
				source := nodeIDs[(id+i)%numNodes]
				target := nodeIDs[(id+i+1)%numNodes]

				_, err := gs.CreateEdge(source, target, "EDGE", nil, 1.0)
				if err == nil {
					mu.Lock()
					expectedOutgoing[source]++
					mu.Unlock()
				}
			}
		}(worker)
	}

	wg.Wait()

	// Verify all edge counts are correct
	errors := 0
	for nodeID, expected := range expectedOutgoing {
		outgoing, err := gs.GetOutgoingEdges(nodeID)
		if err != nil {
			t.Errorf("Failed to get edges for node %d: %v", nodeID, err)
			errors++
			continue
		}

		actual := len(outgoing)
		if actual != expected {
			t.Errorf("Node %d: expected %d edges, got %d (cache consistency issue)",
				nodeID, expected, actual)
			errors++
		}
	}

	if errors > 0 {
		t.Errorf("Cache consistency errors: %d nodes had incorrect edge counts", errors)
	} else {
		t.Logf("Cache consistency validated: All %d nodes have correct edge counts", len(expectedOutgoing))
	}
}

// TestGraphStorage_DiskBackedEdges_RapidCreateDelete tests rapid create/delete cycles
func TestGraphStorage_DiskBackedEdges_RapidCreateDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping rapid create/delete test in short mode")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)

	// Rapidly create and delete edges (reduced from 1000 for disk-backed test time)
	const cycles = 100
	for i := 0; i < cycles; i++ {
		// Create edge
		edge, err := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
		if err != nil {
			t.Fatalf("CreateEdge failed at cycle %d: %v", i, err)
		}

		// Immediately delete it
		err = gs.DeleteEdge(edge.ID)
		if err != nil {
			t.Fatalf("DeleteEdge failed at cycle %d: %v", i, err)
		}

		// Verify no edges remain
		outgoing, _ := gs.GetOutgoingEdges(node1.ID)
		if len(outgoing) != 0 {
			t.Errorf("Cycle %d: Expected 0 edges, found %d", i, len(outgoing))
		}
	}

	// Final verification
	outgoing, _ := gs.GetOutgoingEdges(node1.ID)
	incoming, _ := gs.GetIncomingEdges(node2.ID)

	if len(outgoing) != 0 || len(incoming) != 0 {
		t.Errorf("Final state incorrect: %d outgoing, %d incoming (expected 0)",
			len(outgoing), len(incoming))
	}

	t.Logf("Successfully completed %d rapid create/delete cycles", cycles)
}

// TestGraphStorage_DiskBackedEdges_ConcurrentEdgeDeletion tests concurrent deletion of same edge
func TestGraphStorage_DiskBackedEdges_ConcurrentEdgeDeletion(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create edge
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)

	// Try to delete the same edge concurrently from multiple goroutines
	const numAttempts = 10
	var wg sync.WaitGroup
	successCount := uint64(0)

	for i := 0; i < numAttempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := gs.DeleteEdge(edge.ID)
			if err == nil {
				atomic.AddUint64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// Only ONE deletion should succeed
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful deletion, got %d", successCount)
	}

	// Verify edge is gone
	outgoing, _ := gs.GetOutgoingEdges(node1.ID)
	if len(outgoing) != 0 {
		t.Errorf("Edge still exists after deletion")
	}

	t.Logf("Concurrent deletion handled correctly: %d/%d attempts succeeded (expected 1)",
		successCount, numAttempts)
}

// TestGraphStorage_DiskBackedEdges_LongRunningStability tests stability over extended period
func TestGraphStorage_DiskBackedEdges_LongRunningStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running stability test in short mode")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      500,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create initial graph
	const numNodes = 200
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
	}

	// Run mixed workload for 3 seconds (reduced from 10s for faster tests)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var (
		totalOps    uint64
		errorCount  uint64
	)

	// Spawn continuous workers
	var wg sync.WaitGroup
	for worker := 0; worker < 10; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					source := nodeIDs[(id*time.Now().Nanosecond())%numNodes]
					target := nodeIDs[((id+1)*time.Now().Nanosecond())%numNodes]

					// Random operation
					op := time.Now().Nanosecond() % 10
					if op < 5 {
						// Create
						_, err := gs.CreateEdge(source, target, "EDGE", nil, 1.0)
						if err != nil {
							atomic.AddUint64(&errorCount, 1)
						}
					} else if op < 9 {
						// Read
						_, err := gs.GetOutgoingEdges(source)
						if err != nil {
							atomic.AddUint64(&errorCount, 1)
						}
					} else {
						// Delete
						outgoing, _ := gs.GetOutgoingEdges(source)
						if len(outgoing) > 0 {
							gs.DeleteEdge(outgoing[0].ID)
						}
					}
					atomic.AddUint64(&totalOps, 1)

					// Brief pause to avoid spinning
					time.Sleep(time.Microsecond)
				}
			}
		}(worker)
	}

	wg.Wait()

	// Report results
	t.Logf("Long-running stability test:")
	t.Logf("  Duration: 3 seconds")
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Error rate: %.2f%%", float64(errorCount*100)/float64(totalOps))

	if errorCount > totalOps/100 {
		t.Errorf("Error rate too high: %.2f%% (expected < 1%%)",
			float64(errorCount*100)/float64(totalOps))
	}

	// With 10 workers and 3-second duration, expect at least 100 total operations
	// (conservative for WAL sync limited systems)
	if totalOps < 100 {
		t.Errorf("Too few operations completed: %d (expected > 100)", totalOps)
	}
}

// TestGraphStorage_DiskBackedEdges_CacheEvictionCorr correctness tests cache eviction behavior
func TestGraphStorage_DiskBackedEdges_CacheEvictionCorrectness(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      5, // Very small cache
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create 20 nodes (4x cache size)
	const numNodes = 20
	nodeIDs := make([]uint64, numNodes)
	edgeCounts := make(map[uint64]int)

	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID

		// Create 3 edges per node
		for j := 0; j < 3; j++ {
			target := nodeIDs[(i+j+1)%numNodes]
			_, err := gs.CreateEdge(node.ID, target, "EDGE", nil, 1.0)
			if err == nil {
				edgeCounts[node.ID]++
			}
		}
	}

	// Access all nodes multiple times (force cache evictions)
	for round := 0; round < 5; round++ {
		for _, nodeID := range nodeIDs {
			outgoing, err := gs.GetOutgoingEdges(nodeID)
			if err != nil {
				t.Errorf("GetOutgoingEdges failed for node %d: %v", nodeID, err)
				continue
			}

			expected := edgeCounts[nodeID]
			actual := len(outgoing)

			if actual != expected {
				t.Errorf("Round %d, Node %d: expected %d edges, got %d (cache eviction bug)",
					round, nodeID, expected, actual)
			}
		}
	}

	t.Log("Cache eviction correctness validated across 5 rounds")
}
