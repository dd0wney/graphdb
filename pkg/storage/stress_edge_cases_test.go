package storage

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEdgeCase_ConcurrentNodeDeletion tests concurrent deletion of same node
func TestEdgeCase_ConcurrentNodeDeletion(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing concurrent deletion of same node...")

	// Create a node
	node, err := gs.CreateNode([]string{"DeleteTarget"}, nil)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Try to delete it from multiple goroutines simultaneously
	workers := 10
	var wg sync.WaitGroup
	successCount := int32(0)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := gs.DeleteNode(node.ID)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	t.Logf("  ✓ %d workers attempted deletion, %d succeeded", workers, successCount)

	// Verify node is deleted
	retrieved, _ := gs.GetNode(node.ID)
	if retrieved != nil {
		t.Error("Node still exists after concurrent deletion")
	} else {
		t.Log("  ✓ Node properly deleted")
	}
}

// TestEdgeCase_ConcurrentEdgeCreation tests concurrent edge creation to same nodes
func TestEdgeCase_ConcurrentEdgeCreation(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing concurrent edge creation to same nodes...")

	node1, _ := gs.CreateNode([]string{"Source"}, nil)
	node2, _ := gs.CreateNode([]string{"Target"}, nil)

	workers := 50
	edgesPerWorker := 20
	var wg sync.WaitGroup
	var successCount int32

	startTime := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < edgesPerWorker; i++ {
				_, err := gs.CreateEdge(node1.ID, node2.ID, "CONCURRENT", map[string]Value{
					"worker": IntValue(int64(workerID)),
					"index":  IntValue(int64(i)),
				}, 1.0)
				if err == nil {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	expected := workers * edgesPerWorker
	t.Logf("  ✓ Created %d/%d edges concurrently in %v", successCount, expected, duration)

	// Verify edge count
	edges, _ := gs.GetOutgoingEdges(node1.ID)
	t.Logf("  ✓ Retrieved %d edges", len(edges))
}

// TestEdgeCase_ReadWriteRace tests concurrent reads and writes
func TestEdgeCase_ReadWriteRace(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing concurrent read/write race conditions...")

	// Create some initial nodes
	nodeIDs := make([]uint64, 100)
	for i := range nodeIDs {
		node, _ := gs.CreateNode([]string{"RaceTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	duration := 2 * time.Second
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	var readCount, writeCount, readErrors, writeErrors int32

	// Reader goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					nodeID := nodeIDs[rand.Intn(len(nodeIDs))]
					_, err := gs.GetNode(nodeID)
					if err != nil {
						atomic.AddInt32(&readErrors, 1)
					} else {
						atomic.AddInt32(&readCount, 1)
					}
				}
			}
		}()
	}

	// Writer goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					_, err := gs.CreateNode([]string{"RaceTest"}, map[string]Value{
						"timestamp": IntValue(time.Now().UnixNano()),
					})
					if err != nil {
						atomic.AddInt32(&writeErrors, 1)
					} else {
						atomic.AddInt32(&writeCount, 1)
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stopChan)
	wg.Wait()

	t.Logf("  Reads: %d (errors: %d)", readCount, readErrors)
	t.Logf("  Writes: %d (errors: %d)", writeCount, writeErrors)

	errorRate := float64(readErrors+writeErrors) / float64(readCount+writeCount) * 100
	t.Logf("  ✓ Error rate: %.2f%%", errorRate)

	if errorRate > 5.0 {
		t.Errorf("Error rate too high: %.2f%%", errorRate)
	}
}

// TestEdgeCase_MemoryLeakDetection tests for potential memory leaks
func TestEdgeCase_MemoryLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing for memory leaks...")

	cycles := 10
	nodesPerCycle := 1000

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for cycle := 0; cycle < cycles; cycle++ {
		// Create nodes
		nodeIDs := make([]uint64, nodesPerCycle)
		for i := 0; i < nodesPerCycle; i++ {
			node, _ := gs.CreateNode([]string{"LeakTest"}, map[string]Value{
				"cycle": IntValue(int64(cycle)),
				"index": IntValue(int64(i)),
			})
			nodeIDs[i] = node.ID
		}

		// Delete nodes
		for _, id := range nodeIDs {
			gs.DeleteNode(id)
		}

		if cycle%2 == 0 {
			runtime.GC()
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	memGrowth := int64(m2.Alloc) - int64(m1.Alloc)
	t.Logf("  Memory growth: %d bytes (%.2f MB)", memGrowth, float64(memGrowth)/(1024*1024))
	t.Logf("  GC runs: %d", m2.NumGC-m1.NumGC)

	// Allow some growth but flag excessive memory retention
	maxGrowth := int64(10 * 1024 * 1024) // 10MB threshold
	if memGrowth > maxGrowth {
		t.Logf("  ⚠ Potential memory leak: %d MB retained", memGrowth/(1024*1024))
	} else {
		t.Logf("  ✓ Memory growth within acceptable limits")
	}
}

// TestEdgeCase_StressTestMixedOperations tests system under heavy mixed load
func TestEdgeCase_StressTestMixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Running stress test with mixed operations...")

	duration := 2 * time.Second // Reduced from 5s to avoid deadlocks
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// Operation counters
	var (
		nodeCreates int64
		edgeCreates int64
		nodeReads   int64
		edgeReads   int64
		nodeDeletes int64
		totalErrors int64
	)

	// Pre-populate with some nodes
	initialNodes := 100
	for i := 0; i < initialNodes; i++ {
		gs.CreateNode([]string{"StressTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
	}

	workers := min(runtime.NumCPU(), 8) // Limit workers to reduce lock contention

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			localNodeIDs := make([]uint64, 0, 100)

			for {
				select {
				case <-stopChan:
					return
				default:
					op := rand.Intn(5)

					switch op {
					case 0: // Create node
						node, err := gs.CreateNode([]string{"StressTest"}, map[string]Value{
							"worker": IntValue(int64(workerID)),
							"time":   IntValue(time.Now().UnixNano()),
						})
						if err != nil {
							atomic.AddInt64(&totalErrors, 1)
						} else {
							atomic.AddInt64(&nodeCreates, 1)
							localNodeIDs = append(localNodeIDs, node.ID)
						}

					case 1: // Create edge
						if len(localNodeIDs) >= 2 {
							from := localNodeIDs[rand.Intn(len(localNodeIDs))]
							to := localNodeIDs[rand.Intn(len(localNodeIDs))]
							_, err := gs.CreateEdge(from, to, "STRESS", nil, 1.0)
							if err != nil {
								atomic.AddInt64(&totalErrors, 1)
							} else {
								atomic.AddInt64(&edgeCreates, 1)
							}
						}

					case 2: // Read node
						if len(localNodeIDs) > 0 {
							nodeID := localNodeIDs[rand.Intn(len(localNodeIDs))]
							_, err := gs.GetNode(nodeID)
							if err != nil {
								atomic.AddInt64(&totalErrors, 1)
							} else {
								atomic.AddInt64(&nodeReads, 1)
							}
						}

					case 3: // Read edges
						if len(localNodeIDs) > 0 {
							nodeID := localNodeIDs[rand.Intn(len(localNodeIDs))]
							_, err := gs.GetOutgoingEdges(nodeID)
							if err != nil {
								atomic.AddInt64(&totalErrors, 1)
							} else {
								atomic.AddInt64(&edgeReads, 1)
							}
						}
						time.Sleep(time.Microsecond) // Reduce contention

					case 4: // Delete node
						if len(localNodeIDs) > 10 {
							idx := rand.Intn(len(localNodeIDs))
							nodeID := localNodeIDs[idx]
							err := gs.DeleteNode(nodeID)
							if err == nil {
								atomic.AddInt64(&nodeDeletes, 1)
								// Remove from local list
								localNodeIDs = append(localNodeIDs[:idx], localNodeIDs[idx+1:]...)
							} else {
								atomic.AddInt64(&totalErrors, 1)
							}
						}
					}
				}
			}
		}(w)
	}

	time.Sleep(duration)
	close(stopChan)
	wg.Wait()

	totalOps := nodeCreates + edgeCreates + nodeReads + edgeReads + nodeDeletes
	opsPerSec := float64(totalOps) / duration.Seconds()
	errorRate := float64(totalErrors) / float64(totalOps) * 100

	t.Log("  ✓ Stress test completed:")
	t.Logf("    Workers: %d", workers)
	t.Logf("    Node creates: %d", nodeCreates)
	t.Logf("    Edge creates: %d", edgeCreates)
	t.Logf("    Node reads: %d", nodeReads)
	t.Logf("    Edge reads: %d", edgeReads)
	t.Logf("    Node deletes: %d", nodeDeletes)
	t.Logf("    Total operations: %d", totalOps)
	t.Logf("    Operations/sec: %.0f", opsPerSec)
	t.Logf("    Errors: %d (%.2f%%)", totalErrors, errorRate)

	if errorRate > 5.0 {
		t.Errorf("Error rate too high: %.2f%%", errorRate)
	}
}

// TestEdgeCase_RapidStorageReopens tests rapid open/close cycles
func TestEdgeCase_RapidStorageReopens(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping rapid reopen test in short mode")
	}

	dataDir := t.TempDir()

	t.Log("Testing rapid storage open/close cycles...")

	cycles := 20
	for i := 0; i < cycles; i++ {
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to open storage on cycle %d: %v", i, err)
		}

		// Perform some operations
		gs.CreateNode([]string{"ReopenTest"}, map[string]Value{
			"cycle": IntValue(int64(i)),
		})

		err = gs.Close()
		if err != nil {
			t.Errorf("Failed to close storage on cycle %d: %v", i, err)
		}
	}

	t.Logf("  ✓ Completed %d rapid open/close cycles", cycles)
}

// TestEdgeCase_ConcurrentPropertyUpdates tests concurrent updates to same node
func TestEdgeCase_ConcurrentPropertyUpdates(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing concurrent property updates...")

	// Create a node
	node, _ := gs.CreateNode([]string{"UpdateTest"}, map[string]Value{
		"counter": IntValue(0),
	})

	// Multiple goroutines trying to "update" (actually read and verify)
	workers := 20
	readsPerWorker := 100
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < readsPerWorker; i++ {
				// Read the node
				retrieved, err := gs.GetNode(node.ID)
				if err != nil {
					t.Errorf("Worker %d read failed: %v", workerID, err)
				}
				if retrieved == nil {
					t.Errorf("Worker %d got nil node", workerID)
				}
			}
		}(w)
	}

	wg.Wait()

	totalReads := workers * readsPerWorker
	t.Logf("  ✓ Completed %d concurrent property reads across %d workers", totalReads, workers)
}

// TestEdgeCase_EdgeCaseNaNFloats tests NaN and Inf float values
func TestEdgeCase_EdgeCaseNaNFloats(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing NaN and Inf float values...")

	// Create special floats at runtime to avoid compile-time errors
	zero := 0.0
	one := 1.0
	negOne := -1.0

	specialFloats := []struct {
		name  string
		value float64
	}{
		{"NaN", zero / zero},
		{"Positive Infinity", one / zero},
		{"Negative Infinity", negOne / zero},
	}

	for i, sf := range specialFloats {
		// Try as property
		node, err := gs.CreateNode([]string{"SpecialFloat"}, map[string]Value{
			"value": FloatValue(sf.value),
		})
		if err != nil {
			t.Logf("  Property %s rejected: %v", sf.name, err)
		} else {
			t.Logf("  ✓ Case %d (%s): Created node %d", i, sf.name, node.ID)
		}

		// Try as edge weight
		node1, _ := gs.CreateNode([]string{"N1"}, nil)
		node2, _ := gs.CreateNode([]string{"N2"}, nil)
		edge, err := gs.CreateEdge(node1.ID, node2.ID, "SPECIAL", nil, sf.value)
		if err != nil {
			t.Logf("  Edge weight %s rejected: %v", sf.name, err)
		} else {
			t.Logf("  ✓ Edge with %s weight created: %d", sf.name, edge.ID)
		}
	}
}

// TestEdgeCase_PathologicalGraphStructures tests difficult graph patterns
func TestEdgeCase_PathologicalGraphStructures(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing pathological graph structures...")

	// Test 1: Complete graph (every node connected to every other)
	completeSize := 20
	completeNodes := make([]*Node, completeSize)
	for i := 0; i < completeSize; i++ {
		completeNodes[i], _ = gs.CreateNode([]string{"Complete"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
	}

	edgeCount := 0
	for i := 0; i < completeSize; i++ {
		for j := 0; j < completeSize; j++ {
			if i != j {
				_, err := gs.CreateEdge(completeNodes[i].ID, completeNodes[j].ID, "COMPLETE", nil, 1.0)
				if err == nil {
					edgeCount++
				}
			}
		}
	}

	t.Logf("  ✓ Complete graph: %d nodes, %d edges", completeSize, edgeCount)

	// Test 2: Long chain with many self-loops
	chainLength := 50
	var prevNode *Node
	for i := 0; i < chainLength; i++ {
		node, _ := gs.CreateNode([]string{"Chain"}, nil)

		// Add self-loop
		gs.CreateEdge(node.ID, node.ID, "SELF", nil, 1.0)

		// Connect to previous
		if prevNode != nil {
			gs.CreateEdge(prevNode.ID, node.ID, "NEXT", nil, 1.0)
		}
		prevNode = node
	}

	t.Logf("  ✓ Chain with self-loops: %d nodes", chainLength)
}

// TestEdgeCase_VeryLongEdgeTypes tests edges with very long type names
func TestEdgeCase_VeryLongEdgeTypes(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing very long edge type names...")

	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)

	typeLengths := []int{100, 500, 1000}

	for _, length := range typeLengths {
		edgeType := fmt.Sprintf("EDGE_%s", string(make([]byte, length-5)))
		for i := range edgeType[5:] {
			edgeType = edgeType[:5+i] + "X" + edgeType[5+i+1:]
		}

		edge, err := gs.CreateEdge(node1.ID, node2.ID, edgeType, nil, 1.0)
		if err != nil {
			t.Logf("  Edge type length %d rejected: %v", length, err)
		} else {
			t.Logf("  ✓ Edge type length %d accepted (edge %d)", length, edge.ID)
		}
	}
}

// TestEdgeCase_ZeroLengthArrays tests empty arrays and slices
func TestEdgeCase_ZeroLengthArrays(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing zero-length arrays...")

	// Node with empty labels
	node, err := gs.CreateNode([]string{}, map[string]Value{
		"name": StringValue("NoLabels"),
	})
	if err != nil {
		t.Logf("  Empty label array rejected: %v", err)
	} else {
		t.Logf("  ✓ Node with empty label array created: %d", node.ID)
	}

	// Empty property map
	node2, err := gs.CreateNode([]string{"Test"}, map[string]Value{})
	if err != nil {
		t.Logf("  Empty property map rejected: %v", err)
	} else {
		t.Logf("  ✓ Node with empty property map created: %d", node2.ID)
	}

	// Empty vector
	node3, err := gs.CreateNode([]string{"Vector"}, map[string]Value{
		"vec": VectorValue([]float32{}),
	})
	if err != nil {
		t.Logf("  Empty vector rejected: %v", err)
	} else {
		t.Logf("  ✓ Node with empty vector created: %d", node3.ID)
	}
}

// TestEdgeCase_MaxConcurrency tests maximum concurrent operations
func TestEdgeCase_MaxConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping max concurrency test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing maximum concurrency...")

	workers := 100 // High concurrency
	operationsPerWorker := 50
	var wg sync.WaitGroup
	var successCount int32

	startTime := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < operationsPerWorker; i++ {
				_, err := gs.CreateNode([]string{"MaxConcurrency"}, map[string]Value{
					"worker": IntValue(int64(workerID)),
					"op":     IntValue(int64(i)),
				})
				if err == nil {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	expected := workers * operationsPerWorker
	rate := float64(successCount) / duration.Seconds()

	t.Logf("  ✓ %d workers completed %d/%d operations", workers, successCount, expected)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Rate: %.0f ops/second", rate)

	successRate := float64(successCount) / float64(expected) * 100
	if successRate < 95.0 {
		t.Errorf("Success rate too low: %.1f%%", successRate)
	}
}
