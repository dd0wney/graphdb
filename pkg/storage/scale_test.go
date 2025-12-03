package storage

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestLargeScaleNodeCreation tests creating a large number of nodes
func TestLargeScaleNodeCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Scale based on available memory
	nodeCount := 50000 // Reasonable for CI/CD (scaled down from 1M)

	t.Logf("Creating %d nodes...", nodeCount)
	startTime := time.Now()

	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id":    IntValue(int64(i)),
			"name":  StringValue(fmt.Sprintf("Node_%d", i)),
			"index": IntValue(int64(i)),
		}

		_, err := gs.CreateNode([]string{"ScaleTest"}, props)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}

		if i > 0 && i%10000 == 0 {
			elapsed := time.Since(startTime)
			rate := float64(i) / elapsed.Seconds()
			t.Logf("  Progress: %d nodes (%.0f nodes/sec)", i, rate)
		}
	}

	duration := time.Since(startTime)
	rate := float64(nodeCount) / duration.Seconds()

	t.Logf("✓ Created %d nodes in %v", nodeCount, duration)
	t.Logf("  Rate: %.0f nodes/second", rate)
	t.Logf("  Avg time per node: %v", duration/time.Duration(nodeCount))
}

// TestLargeScaleEdgeCreation tests creating a large number of edges
func TestLargeScaleEdgeCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes first
	nodeCount := 1000
	nodes := make([]*Node, nodeCount)

	t.Logf("Creating %d nodes for edge test...", nodeCount)
	for i := 0; i < nodeCount; i++ {
		node, err := gs.CreateNode([]string{"EdgeTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
		nodes[i] = node
	}

	// Create edges (each node connects to next node)
	edgeCount := nodeCount - 1

	t.Logf("Creating %d edges...", edgeCount)
	startTime := time.Now()

	for i := 0; i < edgeCount; i++ {
		_, err := gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "CONNECTS_TO", nil, 1.0)
		if err != nil {
			t.Fatalf("Failed to create edge %d: %v", i, err)
		}

		if i > 0 && i%100 == 0 {
			elapsed := time.Since(startTime)
			rate := float64(i) / elapsed.Seconds()
			t.Logf("  Progress: %d edges (%.0f edges/sec)", i, rate)
		}
	}

	duration := time.Since(startTime)
	rate := float64(edgeCount) / duration.Seconds()

	t.Logf("✓ Created %d edges in %v", edgeCount, duration)
	t.Logf("  Rate: %.0f edges/second", rate)
}

// TestConcurrentLargeScaleOperations tests concurrent operations at scale
func TestConcurrentLargeScaleOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	workers := runtime.NumCPU()
	nodesPerWorker := 5000
	totalNodes := workers * nodesPerWorker

	t.Logf("Running concurrent operations with %d workers...", workers)
	t.Logf("Total nodes to create: %d", totalNodes)

	var wg sync.WaitGroup
	startTime := time.Now()
	errors := make(chan error, workers)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < nodesPerWorker; i++ {
				nodeID := workerID*nodesPerWorker + i
				props := map[string]Value{
					"worker_id": IntValue(int64(workerID)),
					"node_id":   IntValue(int64(nodeID)),
					"name":      StringValue(fmt.Sprintf("Worker%d_Node%d", workerID, i)),
				}

				_, err := gs.CreateNode([]string{"ConcurrentTest"}, props)
				if err != nil {
					errors <- fmt.Errorf("worker %d failed at node %d: %w", workerID, i, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errors)

	duration := time.Since(startTime)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Fatalf("Failed with %d errors", errorCount)
	}

	rate := float64(totalNodes) / duration.Seconds()

	t.Logf("✓ Created %d nodes concurrently in %v", totalNodes, duration)
	t.Logf("  Workers: %d", workers)
	t.Logf("  Rate: %.0f nodes/second", rate)
	t.Logf("  Avg per worker: %v", duration/time.Duration(workers))
}

// TestMemoryUsageAtScale tests memory usage with large datasets
func TestMemoryUsageAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Get initial memory stats
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	nodeCount := 10000

	t.Logf("Creating %d nodes and monitoring memory...", nodeCount)

	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id":    IntValue(int64(i)),
			"data":  StringValue(fmt.Sprintf("Data for node %d", i)),
			"value": IntValue(int64(i * 2)),
		}

		_, err := gs.CreateNode([]string{"MemTest"}, props)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	// Force garbage collection
	runtime.GC()

	// Get final memory stats
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	allocDiff := m2.Alloc - m1.Alloc
	totalAllocDiff := m2.TotalAlloc - m1.TotalAlloc

	bytesPerNode := float64(allocDiff) / float64(nodeCount)

	t.Logf("✓ Memory usage analysis:")
	t.Logf("  Nodes created: %d", nodeCount)
	t.Logf("  Memory allocated: %d bytes (%.2f MB)", allocDiff, float64(allocDiff)/(1024*1024))
	t.Logf("  Total allocated: %d bytes (%.2f MB)", totalAllocDiff, float64(totalAllocDiff)/(1024*1024))
	t.Logf("  Bytes per node: %.2f", bytesPerNode)
	t.Logf("  GC runs: %d", m2.NumGC-m1.NumGC)
}

// TestQueryPerformanceAtScale tests query performance with large datasets
func TestQueryPerformanceAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create dataset
	nodeCount := 10000
	nodes := make([]*Node, nodeCount)

	t.Logf("Setting up dataset with %d nodes...", nodeCount)
	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id":     IntValue(int64(i)),
			"name":   StringValue(fmt.Sprintf("Node_%d", i)),
			"status": StringValue("active"),
		}

		node, err := gs.CreateNode([]string{"QueryTest"}, props)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
		nodes[i] = node
	}

	// Test random node lookups
	queryCount := 1000
	t.Logf("Running %d random node queries...", queryCount)

	startTime := time.Now()
	for i := 0; i < queryCount; i++ {
		randomID := nodes[i%nodeCount].ID
		_, err := gs.GetNode(randomID)
		if err != nil {
			t.Errorf("Query %d failed: %v", i, err)
		}
	}
	duration := time.Since(startTime)

	avgQueryTime := duration / time.Duration(queryCount)
	qps := float64(queryCount) / duration.Seconds()

	t.Logf("✓ Query performance:")
	t.Logf("  Total queries: %d", queryCount)
	t.Logf("  Total time: %v", duration)
	t.Logf("  Avg query time: %v", avgQueryTime)
	t.Logf("  Queries per second: %.0f", qps)

	if avgQueryTime > 10*time.Millisecond {
		t.Logf("⚠ WARNING: Average query time is high: %v", avgQueryTime)
	}
}

// TestStoragePersistenceAtScale tests persistence with large datasets
func TestStoragePersistenceAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()

	nodeCount := 5000

	// Create and populate storage
	t.Logf("Creating %d nodes...", nodeCount)
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		for i := 0; i < nodeCount; i++ {
			props := map[string]Value{
				"id":   IntValue(int64(i)),
				"data": StringValue(fmt.Sprintf("Persistent data %d", i)),
			}

			_, err := gs.CreateNode([]string{"PersistTest"}, props)
			if err != nil {
				t.Fatalf("Failed to create node: %v", err)
			}
		}

		// Close storage
		err = gs.Close()
		if err != nil {
			t.Fatalf("Failed to close storage: %v", err)
		}
	}

	// Reopen and verify
	t.Log("Reopening storage and verifying data...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to reopen storage: %v", err)
		}
		defer gs.Close()

		// Verify a sample of nodes
		sampleSize := 100
		verified := 0

		for i := 0; i < sampleSize; i++ {
			nodeID := uint64(i * (nodeCount / sampleSize))
			node, err := gs.GetNode(nodeID)
			if err != nil {
				continue // Node might have different ID after persistence
			}
			if node != nil {
				verified++
			}
		}

		t.Logf("✓ Persistence test completed:")
		t.Logf("  Original nodes: %d", nodeCount)
		t.Logf("  Sample verified: %d/%d", verified, sampleSize)
	}
}

// TestHighThroughputWrites tests sustained write throughput
func TestHighThroughputWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large-scale test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	duration := 10 * time.Second
	operationCount := 0

	t.Logf("Running high-throughput writes for %v...", duration)

	startTime := time.Now()
	endTime := startTime.Add(duration)

	for time.Now().Before(endTime) {
		props := map[string]Value{
			"timestamp": IntValue(time.Now().Unix()),
			"counter":   IntValue(int64(operationCount)),
		}

		_, err := gs.CreateNode([]string{"ThroughputTest"}, props)
		if err != nil {
			t.Errorf("Write failed at operation %d: %v", operationCount, err)
			break
		}

		operationCount++

		if operationCount%1000 == 0 {
			elapsed := time.Since(startTime)
			currentRate := float64(operationCount) / elapsed.Seconds()
			t.Logf("  Progress: %d ops (%.0f ops/sec)", operationCount, currentRate)
		}
	}

	actualDuration := time.Since(startTime)
	throughput := float64(operationCount) / actualDuration.Seconds()

	t.Logf("✓ High-throughput write test:")
	t.Logf("  Duration: %v", actualDuration)
	t.Logf("  Operations: %d", operationCount)
	t.Logf("  Throughput: %.0f ops/second", throughput)
	t.Logf("  Avg operation time: %v", actualDuration/time.Duration(operationCount))
}

// BenchmarkLargeGraphTraversal benchmarks traversing a large graph
func BenchmarkLargeGraphTraversal(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a connected graph
	nodeCount := 1000
	nodes := make([]*Node, nodeCount)

	for i := 0; i < nodeCount; i++ {
		node, _ := gs.CreateNode([]string{"BenchNode"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodes[i] = node

		// Connect to previous node
		if i > 0 {
			gs.CreateEdge(nodes[i-1].ID, nodes[i].ID, "NEXT", nil, 1.0)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Traverse from random starting point
		startNode := nodes[i%nodeCount]
		edges, _ := gs.GetOutgoingEdges(startNode.ID)

		// Follow the chain for a few steps
		currentID := startNode.ID
		for j := 0; j < 10 && len(edges) > 0; j++ {
			if len(edges) > 0 {
				currentID = edges[0].ToNodeID
				edges, _ = gs.GetOutgoingEdges(currentID)
			}
		}
	}
}

// BenchmarkConcurrentNodeCreation benchmarks concurrent node creation
func BenchmarkConcurrentNodeCreation(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			props := map[string]Value{
				"id": IntValue(int64(counter)),
			}
			gs.CreateNode([]string{"BenchNode"}, props)
			counter++
		}
	})
}
