package storage

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"
)

// PerformanceBaseline stores expected performance metrics
type PerformanceBaseline struct {
	NodeCreationRate      float64 // nodes/second
	EdgeCreationRate      float64 // edges/second
	NodeQueryRate         float64 // queries/second
	EdgeQueryRate         float64 // queries/second
	TraversalRate         float64 // traversals/second
	ConcurrentWriteRate   float64 // writes/second
	MaxMemoryPerNode      int64   // bytes
	CompactionMaxDuration time.Duration
}

// GetPerformanceBaseline returns expected performance thresholds
// These should be tuned based on actual hardware and requirements
func GetPerformanceBaseline() PerformanceBaseline {
	return PerformanceBaseline{
		NodeCreationRate:      50000,  // 50K nodes/sec minimum
		EdgeCreationRate:      50000,  // 50K edges/sec minimum
		NodeQueryRate:         500000, // 500K queries/sec minimum
		EdgeQueryRate:         100000, // 100K queries/sec minimum
		TraversalRate:         10000,  // 10K traversals/sec minimum
		ConcurrentWriteRate:   100000, // 100K concurrent writes/sec minimum
		MaxMemoryPerNode:      5000,   // 5KB per node maximum
		CompactionMaxDuration: 5 * time.Second,
	}
}

// TestPerformanceRegression_NodeCreation tests for regression in node creation speed
func TestPerformanceRegression_NodeCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	nodeCount := 10000
	t.Logf("Performance baseline: %.0f nodes/second", baseline.NodeCreationRate)
	t.Logf("Testing node creation with %d nodes...", nodeCount)

	startTime := time.Now()
	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id":   IntValue(int64(i)),
			"name": StringValue(fmt.Sprintf("Node_%d", i)),
		}
		_, err := gs.CreateNode([]string{"PerfTest"}, props)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}
	duration := time.Since(startTime)

	rate := float64(nodeCount) / duration.Seconds()
	t.Logf("Actual rate: %.0f nodes/second", rate)
	t.Logf("Duration: %v (%.2fµs per node)", duration, float64(duration.Microseconds())/float64(nodeCount))

	// Check for regression (allow 20% tolerance)
	tolerance := 0.80
	minAcceptable := baseline.NodeCreationRate * tolerance

	if rate < minAcceptable {
		t.Errorf("PERFORMANCE REGRESSION: Node creation rate %.0f nodes/sec is below threshold %.0f nodes/sec (baseline: %.0f)",
			rate, minAcceptable, baseline.NodeCreationRate)
	} else {
		improvement := ((rate - baseline.NodeCreationRate) / baseline.NodeCreationRate) * 100
		t.Logf("✓ Performance acceptable (%.1f%% vs baseline)", improvement)
	}
}

// TestPerformanceRegression_EdgeCreation tests for regression in edge creation speed
func TestPerformanceRegression_EdgeCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes first
	nodeCount := 1000
	nodes := make([]*Node, nodeCount)
	for i := 0; i < nodeCount; i++ {
		node, _ := gs.CreateNode([]string{"EdgePerfTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodes[i] = node
	}

	// Test edge creation
	edgeCount := nodeCount - 1
	t.Logf("Performance baseline: %.0f edges/second", baseline.EdgeCreationRate)
	t.Logf("Testing edge creation with %d edges...", edgeCount)

	startTime := time.Now()
	for i := 0; i < edgeCount; i++ {
		_, err := gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "CONNECTS", nil, 1.0)
		if err != nil {
			t.Fatalf("Failed to create edge: %v", err)
		}
	}
	duration := time.Since(startTime)

	rate := float64(edgeCount) / duration.Seconds()
	t.Logf("Actual rate: %.0f edges/second", rate)
	t.Logf("Duration: %v (%.2fµs per edge)", duration, float64(duration.Microseconds())/float64(edgeCount))

	// Check for regression (allow 20% tolerance)
	tolerance := 0.80
	minAcceptable := baseline.EdgeCreationRate * tolerance

	if rate < minAcceptable {
		t.Errorf("PERFORMANCE REGRESSION: Edge creation rate %.0f edges/sec is below threshold %.0f edges/sec (baseline: %.0f)",
			rate, minAcceptable, baseline.EdgeCreationRate)
	} else {
		improvement := ((rate - baseline.EdgeCreationRate) / baseline.EdgeCreationRate) * 100
		t.Logf("✓ Performance acceptable (%.1f%% vs baseline)", improvement)
	}
}

// TestPerformanceRegression_NodeQuery tests for regression in node query speed
func TestPerformanceRegression_NodeQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create dataset
	nodeCount := 5000
	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id": IntValue(int64(i)),
		}
		gs.CreateNode([]string{"QueryPerfTest"}, props)
	}

	// Test query performance
	queryCount := 10000
	t.Logf("Performance baseline: %.0f queries/second", baseline.NodeQueryRate)
	t.Logf("Testing %d node queries...", queryCount)

	startTime := time.Now()
	for i := 0; i < queryCount; i++ {
		nodeID := uint64(i % nodeCount)
		_, err := gs.GetNode(nodeID)
		if err != nil {
			// Some queries may fail, that's OK for this test
		}
	}
	duration := time.Since(startTime)

	rate := float64(queryCount) / duration.Seconds()
	t.Logf("Actual rate: %.0f queries/second", rate)
	t.Logf("Duration: %v (%.0fns per query)", duration, float64(duration.Nanoseconds())/float64(queryCount))

	// Check for regression (allow 30% tolerance for queries)
	tolerance := 0.70
	minAcceptable := baseline.NodeQueryRate * tolerance

	if rate < minAcceptable {
		t.Errorf("PERFORMANCE REGRESSION: Node query rate %.0f queries/sec is below threshold %.0f queries/sec (baseline: %.0f)",
			rate, minAcceptable, baseline.NodeQueryRate)
	} else {
		improvement := ((rate - baseline.NodeQueryRate) / baseline.NodeQueryRate) * 100
		t.Logf("✓ Performance acceptable (%.1f%% vs baseline)", improvement)
	}
}

// TestPerformanceRegression_EdgeQuery tests for regression in edge query speed
func TestPerformanceRegression_EdgeQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create graph with edges
	nodeCount := 1000
	nodes := make([]*Node, nodeCount)
	for i := 0; i < nodeCount; i++ {
		node, _ := gs.CreateNode([]string{"EdgeQueryTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodes[i] = node

		// Create edges to next 3 nodes
		for j := 1; j <= 3 && i+j < nodeCount; j++ {
			if nodes[i+j] != nil {
				gs.CreateEdge(node.ID, nodes[i+j].ID, "LINKS", nil, 1.0)
			}
		}
	}

	// Test edge query performance
	queryCount := 10000
	t.Logf("Performance baseline: %.0f edge queries/second", baseline.EdgeQueryRate)
	t.Logf("Testing %d edge queries...", queryCount)

	startTime := time.Now()
	for i := 0; i < queryCount; i++ {
		nodeID := nodes[i%nodeCount].ID
		_, err := gs.GetOutgoingEdges(nodeID)
		if err != nil {
			t.Errorf("Edge query failed: %v", err)
		}
	}
	duration := time.Since(startTime)

	rate := float64(queryCount) / duration.Seconds()
	t.Logf("Actual rate: %.0f edge queries/second", rate)
	t.Logf("Duration: %v (%.0fns per query)", duration, float64(duration.Nanoseconds())/float64(queryCount))

	// Check for regression (allow 30% tolerance)
	tolerance := 0.70
	minAcceptable := baseline.EdgeQueryRate * tolerance

	if rate < minAcceptable {
		t.Errorf("PERFORMANCE REGRESSION: Edge query rate %.0f queries/sec is below threshold %.0f queries/sec (baseline: %.0f)",
			rate, minAcceptable, baseline.EdgeQueryRate)
	} else {
		improvement := ((rate - baseline.EdgeQueryRate) / baseline.EdgeQueryRate) * 100
		t.Logf("✓ Performance acceptable (%.1f%% vs baseline)", improvement)
	}
}

// TestPerformanceRegression_GraphTraversal tests for regression in graph traversal speed
func TestPerformanceRegression_GraphTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a chain of nodes
	chainLength := 1000
	nodes := make([]*Node, chainLength)
	for i := 0; i < chainLength; i++ {
		node, _ := gs.CreateNode([]string{"TraversalTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodes[i] = node

		if i > 0 {
			gs.CreateEdge(nodes[i-1].ID, node.ID, "NEXT", nil, 1.0)
		}
	}

	// Test traversal performance (depth-limited BFS)
	traversalCount := 1000
	depth := 10
	t.Logf("Performance baseline: %.0f traversals/second", baseline.TraversalRate)
	t.Logf("Testing %d traversals (depth %d)...", traversalCount, depth)

	startTime := time.Now()
	for i := 0; i < traversalCount; i++ {
		startNode := nodes[i%chainLength]
		visited := 0

		// Simple BFS traversal
		currentID := startNode.ID
		for d := 0; d < depth; d++ {
			edges, err := gs.GetOutgoingEdges(currentID)
			if err != nil || len(edges) == 0 {
				break
			}
			visited++
			currentID = edges[0].ToNodeID
		}
	}
	duration := time.Since(startTime)

	rate := float64(traversalCount) / duration.Seconds()
	t.Logf("Actual rate: %.0f traversals/second", rate)
	t.Logf("Duration: %v (%.2fµs per traversal)", duration, float64(duration.Microseconds())/float64(traversalCount))

	// Check for regression (allow 30% tolerance)
	tolerance := 0.70
	minAcceptable := baseline.TraversalRate * tolerance

	if rate < minAcceptable {
		t.Errorf("PERFORMANCE REGRESSION: Traversal rate %.0f traversals/sec is below threshold %.0f traversals/sec (baseline: %.0f)",
			rate, minAcceptable, baseline.TraversalRate)
	} else {
		improvement := ((rate - baseline.TraversalRate) / baseline.TraversalRate) * 100
		t.Logf("✓ Performance acceptable (%.1f%% vs baseline)", improvement)
	}
}

// TestPerformanceRegression_ConcurrentWrites tests for regression in concurrent write performance
func TestPerformanceRegression_ConcurrentWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	workers := runtime.NumCPU()
	writesPerWorker := 5000
	totalWrites := workers * writesPerWorker

	t.Logf("Performance baseline: %.0f concurrent writes/second", baseline.ConcurrentWriteRate)
	t.Logf("Testing concurrent writes with %d workers (%d total writes)...", workers, totalWrites)

	var wg sync.WaitGroup
	startTime := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < writesPerWorker; i++ {
				props := map[string]Value{
					"worker": IntValue(int64(workerID)),
					"index":  IntValue(int64(i)),
				}
				gs.CreateNode([]string{"ConcurrentPerfTest"}, props)
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)

	rate := float64(totalWrites) / duration.Seconds()
	t.Logf("Actual rate: %.0f concurrent writes/second", rate)
	t.Logf("Duration: %v (%.2fµs per write)", duration, float64(duration.Microseconds())/float64(totalWrites))
	t.Logf("Workers: %d, Writes per worker: %d", workers, writesPerWorker)

	// Check for regression (allow 30% tolerance for concurrent operations)
	tolerance := 0.70
	minAcceptable := baseline.ConcurrentWriteRate * tolerance

	if rate < minAcceptable {
		t.Errorf("PERFORMANCE REGRESSION: Concurrent write rate %.0f writes/sec is below threshold %.0f writes/sec (baseline: %.0f)",
			rate, minAcceptable, baseline.ConcurrentWriteRate)
	} else {
		improvement := ((rate - baseline.ConcurrentWriteRate) / baseline.ConcurrentWriteRate) * 100
		t.Logf("✓ Performance acceptable (%.1f%% vs baseline)", improvement)
	}
}

// TestPerformanceRegression_MemoryEfficiency tests for memory usage regression
func TestPerformanceRegression_MemoryEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	baseline := GetPerformanceBaseline()
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Measure memory before
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	nodeCount := 10000
	t.Logf("Performance baseline: %d bytes per node maximum", baseline.MaxMemoryPerNode)
	t.Logf("Creating %d nodes and measuring memory...", nodeCount)

	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id":   IntValue(int64(i)),
			"name": StringValue(fmt.Sprintf("Node_%d", i)),
			"data": StringValue(fmt.Sprintf("Data for node %d", i)),
		}
		gs.CreateNode([]string{"MemPerfTest"}, props)
	}

	// Measure memory after
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	allocDiff := int64(m2.Alloc) - int64(m1.Alloc)
	bytesPerNode := allocDiff / int64(nodeCount)

	t.Logf("Memory allocated: %d bytes (%.2f MB)", allocDiff, float64(allocDiff)/(1024*1024))
	t.Logf("Bytes per node: %d", bytesPerNode)

	// Check for regression (memory should not exceed baseline)
	if bytesPerNode > baseline.MaxMemoryPerNode {
		t.Errorf("PERFORMANCE REGRESSION: Memory usage %d bytes/node exceeds threshold %d bytes/node",
			bytesPerNode, baseline.MaxMemoryPerNode)
	} else {
		efficiency := float64(baseline.MaxMemoryPerNode-bytesPerNode) / float64(baseline.MaxMemoryPerNode) * 100
		t.Logf("✓ Memory efficiency acceptable (%.1f%% better than baseline)", efficiency)
	}
}

// TestPerformanceRegression_MixedWorkload tests realistic mixed workload performance
func TestPerformanceRegression_MixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Pre-populate with some nodes to avoid race conditions
	nodeCount := 100
	for i := 0; i < nodeCount; i++ {
		props := map[string]Value{
			"id": IntValue(int64(i)),
		}
		gs.CreateNode([]string{"MixedTest"}, props)
	}

	duration := 3 * time.Second
	t.Logf("Testing mixed workload for %v...", duration)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Counters
	var (
		nodeCreations int64
		nodeQueries   int64
	)
	var counterMutex sync.Mutex

	// Worker 1: Create nodes (lighter load)
	wg.Add(1)
	go func() {
		defer wg.Done()
		localCount := int64(0)
		for {
			select {
			case <-stopChan:
				counterMutex.Lock()
				nodeCreations = localCount
				counterMutex.Unlock()
				return
			default:
				props := map[string]Value{
					"id": IntValue(localCount + 1000),
				}
				_, err := gs.CreateNode([]string{"MixedTest"}, props)
				if err == nil {
					localCount++
				}
				// Small sleep to reduce contention
				time.Sleep(10 * time.Microsecond)
			}
		}
	}()

	// Worker 2: Query nodes (read-heavy)
	wg.Add(1)
	go func() {
		defer wg.Done()
		localCount := int64(0)
		for {
			select {
			case <-stopChan:
				counterMutex.Lock()
				nodeQueries = localCount
				counterMutex.Unlock()
				return
			default:
				nodeID := uint64(rand.Int63n(100))
				gs.GetNode(nodeID)
				localCount++
			}
		}
	}()

	// Run for specified duration
	time.Sleep(duration)
	close(stopChan)
	wg.Wait()

	counterMutex.Lock()
	totalOps := nodeCreations + nodeQueries
	counterMutex.Unlock()

	// Report results
	t.Log("✓ Mixed workload performance:")
	t.Logf("  Node creations: %d (%.0f ops/sec)", nodeCreations, float64(nodeCreations)/duration.Seconds())
	t.Logf("  Node queries: %d (%.0f ops/sec)", nodeQueries, float64(nodeQueries)/duration.Seconds())
	t.Logf("  Total operations: %d (%.0f ops/sec)", totalOps, float64(totalOps)/duration.Seconds())

	// Basic sanity check - we should have done *something*
	if totalOps < 1000 {
		t.Errorf("Mixed workload performed poorly: only %d operations in %v", totalOps, duration)
	}
}

// TestPerformanceRegression_LargePropertyHandling tests handling of large properties
func TestPerformanceRegression_LargePropertyHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance regression test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with varying property sizes
	nodeCount := 1000
	propertySizes := []int{100, 1000, 10000} // bytes

	t.Log("Testing large property handling performance...")

	for _, size := range propertySizes {
		largeData := make([]byte, size)
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}

		startTime := time.Now()
		for i := 0; i < nodeCount; i++ {
			props := map[string]Value{
				"id":   IntValue(int64(i)),
				"data": StringValue(string(largeData)),
			}
			_, err := gs.CreateNode([]string{"LargePropTest"}, props)
			if err != nil {
				t.Errorf("Failed to create node with %d byte property: %v", size, err)
				break
			}
		}
		duration := time.Since(startTime)

		rate := float64(nodeCount) / duration.Seconds()
		throughput := float64(nodeCount*size) / duration.Seconds() / (1024 * 1024) // MB/s

		t.Logf("Property size %d bytes:", size)
		t.Logf("  Creation rate: %.0f nodes/sec", rate)
		t.Logf("  Data throughput: %.2f MB/sec", throughput)
		t.Logf("  Duration: %v", duration)
	}
}
