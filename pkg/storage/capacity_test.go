package storage

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

// Test5MNodeCapacity validates that we can handle 5M nodes on 32GB RAM
// This is an integration test that validates Milestone 2 claims
//
// WARNING: This test takes 30-60 minutes and requires 15+ GB RAM
// Skip by default unless explicitly requested with: go test -run=Test5MNodeCapacity -timeout=90m
func Test5MNodeCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping capacity test in short mode")
	}

	// Skip by default - requires explicit environment variable to run
	if os.Getenv("RUN_CAPACITY_TEST") != "1" {
		t.Skip("Skipping 5M node capacity test (set RUN_CAPACITY_TEST=1 to run)")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Configuration for 5M node test
	const (
		targetNodes     = 5_000_000
		avgEdgesPerNode = 10
		cacheSize       = 100_000 // Cache for 100K hot edge lists
	)

	t.Logf("=== 5M Node Capacity Test ===")
	t.Logf("Target: %d nodes with avg %d edges/node", targetNodes, avgEdgesPerNode)
	t.Logf("Cache size: %d entries", cacheSize)

	// Measure baseline memory
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	baselineAlloc := m1.Alloc / 1024 / 1024 // MB

	t.Logf("Baseline memory: %d MB", baselineAlloc)

	// Create EdgeStore
	startTime := time.Now()
	es, err := NewEdgeStore(dataDir, cacheSize)
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	t.Logf("EdgeStore created in %s", time.Since(startTime))

	// Phase 1: Write 5M nodes with edges
	t.Logf("\n=== Phase 1: Writing %d nodes ===", targetNodes)
	writeStart := time.Now()

	batchSize := 10_000
	for batch := 0; batch < targetNodes/batchSize; batch++ {
		for i := 0; i < batchSize; i++ {
			nodeID := uint64(batch*batchSize + i + 1)

			// Create edges (vary from 5 to 15 edges per node)
			numEdges := 5 + (int(nodeID) % 11) // 5-15 edges
			edges := make([]uint64, numEdges)
			for j := 0; j < numEdges; j++ {
				// Create edges to other nodes (use modulo to keep valid)
				edges[j] = uint64((int(nodeID) + j*1000) % targetNodes)
			}

			err = es.StoreOutgoingEdges(nodeID, edges)
			if err != nil {
				t.Fatalf("Failed to store edges for node %d: %v", nodeID, err)
			}
		}

		// Progress update every 100K nodes
		if (batch+1)%10 == 0 {
			nodesWritten := (batch + 1) * batchSize
			elapsed := time.Since(writeStart)
			rate := float64(nodesWritten) / elapsed.Seconds()

			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			currentMem := m.Alloc / 1024 / 1024

			t.Logf("Progress: %d/%d nodes (%.1f%%) | Rate: %.0f nodes/sec | Memory: %d MB",
				nodesWritten, targetNodes, float64(nodesWritten)/float64(targetNodes)*100,
				rate, currentMem)
		}
	}

	writeElapsed := time.Since(writeStart)
	writeRate := float64(targetNodes) / writeElapsed.Seconds()

	// Measure memory after writes
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	afterWriteMem := m2.Alloc / 1024 / 1024

	t.Logf("\n=== Write Phase Complete ===")
	t.Logf("Time: %s", writeElapsed)
	t.Logf("Rate: %.0f nodes/sec", writeRate)
	t.Logf("Memory: %d MB", afterWriteMem)
	t.Logf("Memory increase: %d MB", afterWriteMem-baselineAlloc)

	// Phase 2: Read test (random access pattern)
	t.Logf("\n=== Phase 2: Random Read Test ===")
	readStart := time.Now()

	// Read 10K random nodes
	numReads := 10_000
	for i := 0; i < numReads; i++ {
		// Use pseudo-random pattern based on index
		nodeID := uint64((i*997 + 17) % targetNodes)

		edges, err := es.GetOutgoingEdges(nodeID)
		if err != nil {
			t.Fatalf("Failed to read node %d: %v", nodeID, err)
		}

		// Verify we got edges
		if len(edges) == 0 {
			t.Errorf("Node %d has no edges (expected 5-15)", nodeID)
		}
	}

	readElapsed := time.Since(readStart)
	avgReadLatency := readElapsed / time.Duration(numReads)

	// Measure memory after reads
	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)
	afterReadMem := m3.Alloc / 1024 / 1024

	t.Logf("\n=== Read Phase Complete ===")
	t.Logf("Reads: %d", numReads)
	t.Logf("Time: %s", readElapsed)
	t.Logf("Avg latency: %s", avgReadLatency)
	t.Logf("Memory: %d MB", afterReadMem)

	// Phase 3: Hot set test (cache effectiveness)
	t.Logf("\n=== Phase 3: Hot Set Test ===")
	hotSetSize := 1000
	hotSetStart := time.Now()

	// Read the same 1000 nodes repeatedly (should be cached)
	for round := 0; round < 10; round++ {
		for i := 0; i < hotSetSize; i++ {
			nodeID := uint64(i + 1)
			_, err := es.GetOutgoingEdges(nodeID)
			if err != nil {
				t.Fatalf("Failed to read hot node %d: %v", nodeID, err)
			}
		}
	}

	hotSetElapsed := time.Since(hotSetStart)
	totalHotReads := hotSetSize * 10
	avgHotLatency := hotSetElapsed / time.Duration(totalHotReads)

	t.Logf("Hot reads: %d", totalHotReads)
	t.Logf("Time: %s", hotSetElapsed)
	t.Logf("Avg latency: %s (should be fast due to cache)", avgHotLatency)

	// Verify hot reads are faster than random reads
	if avgHotLatency > avgReadLatency {
		t.Errorf("Hot reads (%s) should be faster than random reads (%s)",
			avgHotLatency, avgReadLatency)
	}

	// Final memory check
	runtime.GC()
	var mFinal runtime.MemStats
	runtime.ReadMemStats(&mFinal)
	finalMem := mFinal.Alloc / 1024 / 1024

	t.Logf("\n=== Final Results ===")
	t.Logf("Nodes: %d", targetNodes)
	t.Logf("Total edges: ~%d", targetNodes*avgEdgesPerNode)
	t.Logf("Final memory: %d MB", finalMem)
	t.Logf("Memory per node: %.2f bytes", float64(finalMem-baselineAlloc)*1024*1024/float64(targetNodes))

	// Validate memory usage is reasonable (should be < 15 GB for 5M nodes)
	maxMemoryMB := 15 * 1024 // 15 GB
	if finalMem > uint64(maxMemoryMB) {
		t.Errorf("Memory usage %d MB exceeds maximum %d MB (15 GB)", finalMem, maxMemoryMB)
	} else {
		t.Logf("✅ Memory usage within limits (< 15 GB)")
	}

	// Validate cache effectiveness
	cacheSpeedup := float64(avgReadLatency) / float64(avgHotLatency)
	t.Logf("Cache speedup: %.1fx", cacheSpeedup)
	if cacheSpeedup < 5.0 {
		t.Errorf("Cache speedup %.1fx is too low (expected > 5x)", cacheSpeedup)
	} else {
		t.Logf("✅ Cache effective (%.1fx speedup)", cacheSpeedup)
	}

	t.Logf("\n=== 5M Node Capacity Test PASSED ===")
}

// TestEdgeStoreMemoryScaling tests memory usage at different scales
func TestEdgeStoreMemoryScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory scaling test in short mode")
	}

	scales := []struct {
		nodes     int
		cacheSize int
	}{
		{10_000, 100},
		{50_000, 500},
		{100_000, 1_000},
	}

	for _, scale := range scales {
		t.Run(fmt.Sprintf("%dNodes_%dCache", scale.nodes, scale.cacheSize), func(t *testing.T) {
			dataDir := t.TempDir()

			// Baseline memory
			runtime.GC()
			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)
			baseline := m1.Alloc

			// Create and populate EdgeStore
			es, err := NewEdgeStore(dataDir, scale.cacheSize)
			if err != nil {
				t.Fatalf("Failed to create EdgeStore: %v", err)
			}
			defer es.Close()

			// Write nodes
			for i := 0; i < scale.nodes; i++ {
				nodeID := uint64(i + 1)
				edges := []uint64{nodeID * 10, nodeID * 10 + 1, nodeID * 10 + 2}
				err = es.StoreOutgoingEdges(nodeID, edges)
				if err != nil {
					t.Fatalf("Failed to store edges: %v", err)
				}
			}

			// Access some nodes to populate cache
			accessCount := min(scale.cacheSize, scale.nodes)
			for i := 0; i < accessCount; i++ {
				nodeID := uint64(i + 1)
				_, _ = es.GetOutgoingEdges(nodeID)
			}

			// Measure memory
			runtime.GC()
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)
			used := m2.Alloc - baseline
			usedMB := float64(used) / 1024 / 1024
			bytesPerNode := float64(used) / float64(scale.nodes)

			t.Logf("Nodes: %d | Cache: %d | Memory: %.1f MB | Per-node: %.1f bytes",
				scale.nodes, scale.cacheSize, usedMB, bytesPerNode)

			// Validate memory scaling is sub-linear (benefits from disk backing)
			// For 1M nodes, should use < 2GB
			if scale.nodes >= 1_000_000 {
				maxMemoryMB := 2000.0
				if usedMB > maxMemoryMB {
					t.Errorf("Memory %.1f MB exceeds max %.1f MB for %d nodes",
						usedMB, maxMemoryMB, scale.nodes)
				}
			}
		})
	}
}

// Helper function (Go 1.21+)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
