package storage

import (
	"os"
	"sync"
	"testing"
)

// TestGraphStorage_StatisticsAfterCrash tests that NodeCount and EdgeCount survive crashes
func TestGraphStorage_StatisticsAfterCrash(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes and edges, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create 10 nodes - store actual node IDs
		var nodeIDs []uint64
		for i := 0; i < 10; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Create 15 edges using actual node IDs
		for i := 0; i < 15; i++ {
			_, err := gs.CreateEdge(nodeIDs[i%10], nodeIDs[(i+1)%10], "KNOWS", nil, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge failed: %v", err)
			}
		}

		// Check stats before crash
		stats := gs.stats
		if stats.NodeCount != 10 {
			t.Fatalf("Before crash: Expected NodeCount=10, got %d", stats.NodeCount)
		}
		if stats.EdgeCount != 15 {
			t.Fatalf("Before crash: Expected EdgeCount=15, got %d", stats.EdgeCount)
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Created 10 nodes and 15 edges, simulating crash...")
	}

	// Phase 2: Recover and verify statistics
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify statistics recovered correctly
		stats := gs.stats
		if stats.NodeCount != 10 {
			t.Errorf("After recovery: Expected NodeCount=10, got %d", stats.NodeCount)
		}
		if stats.EdgeCount != 15 {
			t.Errorf("After recovery: Expected EdgeCount=15, got %d", stats.EdgeCount)
		}

		t.Log("Statistics correctly recovered from WAL")
	}
}

// TestGraphStorage_StatisticsAfterSnapshot tests that statistics survive clean shutdown
func TestGraphStorage_StatisticsAfterSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create data, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 7 nodes - store actual node IDs
		var nodeIDs []uint64
		for i := 0; i < 7; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			nodeIDs = append(nodeIDs, node.ID)
		}
		// Create 12 edges using actual node IDs
		for i := 0; i < 12; i++ {
			gs.CreateEdge(nodeIDs[i%7], nodeIDs[(i+1)%7], "KNOWS", nil, 1.0)
		}

		// Verify stats before close
		stats := gs.stats
		if stats.NodeCount != 7 || stats.EdgeCount != 12 {
			t.Fatalf("Before close: Expected 7 nodes and 12 edges, got %d nodes and %d edges",
				stats.NodeCount, stats.EdgeCount)
		}

		// Close cleanly - snapshot + truncate
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Created 7 nodes and 12 edges, closed cleanly")
	}

	// Phase 2: Recover from snapshot
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify statistics from snapshot
		stats := gs.stats
		if stats.NodeCount != 7 {
			t.Errorf("After snapshot recovery: Expected NodeCount=7, got %d", stats.NodeCount)
		}
		if stats.EdgeCount != 12 {
			t.Errorf("After snapshot recovery: Expected EdgeCount=12, got %d", stats.EdgeCount)
		}

		t.Log("Statistics correctly recovered from snapshot")
	}
}

// TestGraphStorage_StatisticsAfterDeletion tests that statistics decrement correctly
func TestGraphStorage_StatisticsAfterDeletion(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create data, delete some, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create 10 nodes
		var nodeIDs []uint64
		for i := 0; i < 10; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Create 15 edges
		var edgeIDs []uint64
		for i := 0; i < 15; i++ {
			edge, _ := gs.CreateEdge(nodeIDs[i%10], nodeIDs[(i+1)%10], "KNOWS", nil, 1.0)
			edgeIDs = append(edgeIDs, edge.ID)
		}

		// Delete 3 edges
		gs.DeleteEdge(edgeIDs[0])
		gs.DeleteEdge(edgeIDs[5])
		gs.DeleteEdge(edgeIDs[10])

		// Delete 2 nodes (will cascade delete their edges)
		gs.DeleteNode(nodeIDs[3])
		gs.DeleteNode(nodeIDs[7])

		// Check stats before crash
		stats := gs.stats
		expectedNodes := 8 // 10 - 2 deleted
		if stats.NodeCount != uint64(expectedNodes) {
			t.Errorf("Before crash: Expected NodeCount=%d, got %d", expectedNodes, stats.NodeCount)
		}

		// Edge count is trickier due to cascade deletion
		// At minimum, we deleted 3 explicit edges
		if stats.EdgeCount >= 15 {
			t.Errorf("Before crash: Expected EdgeCount < 15 after deletions, got %d", stats.EdgeCount)
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Logf("After deletions: %d nodes, %d edges, simulating crash...", stats.NodeCount, stats.EdgeCount)
	}

	// Phase 2: Recover and verify statistics
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify statistics after recovery match reality
		stats := gs.stats

		// Count actual nodes
		persons, _ := gs.FindNodesByLabel("Person")
		actualNodes := len(persons)
		if stats.NodeCount != uint64(actualNodes) {
			t.Errorf("After recovery: NodeCount mismatch - stats=%d, actual=%d",
				stats.NodeCount, actualNodes)
		}

		// Count actual edges
		knows, _ := gs.FindEdgesByType("KNOWS")
		actualEdges := len(knows)
		if stats.EdgeCount != uint64(actualEdges) {
			t.Errorf("After recovery: EdgeCount mismatch - stats=%d, actual=%d",
				stats.EdgeCount, actualEdges)
		}

		t.Logf("Statistics after recovery: %d nodes, %d edges (accurate)", stats.NodeCount, stats.EdgeCount)
	}
}

// TestGraphStorage_StatisticsAccuracyAfterManyOperations tests stats accuracy with many operations
func TestGraphStorage_StatisticsAccuracyAfterManyOperations(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create, update, delete in sequence, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create 20 nodes
		var nodeIDs []uint64
		for i := 0; i < 20; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"id": IntValue(int64(i))})
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Create 30 edges
		var edgeIDs []uint64
		for i := 0; i < 30; i++ {
			edge, _ := gs.CreateEdge(nodeIDs[i%20], nodeIDs[(i+1)%20], "KNOWS", nil, 1.0)
			edgeIDs = append(edgeIDs, edge.ID)
		}

		// Update some nodes (shouldn't affect counts)
		gs.UpdateNode(nodeIDs[0], map[string]Value{"updated": BoolValue(true)})
		gs.UpdateNode(nodeIDs[5], map[string]Value{"updated": BoolValue(true)})
		gs.UpdateNode(nodeIDs[10], map[string]Value{"updated": BoolValue(true)})

		// Delete 5 edges
		for i := 0; i < 5; i++ {
			gs.DeleteEdge(edgeIDs[i])
		}

		// Delete 3 nodes
		for i := 0; i < 3; i++ {
			gs.DeleteNode(nodeIDs[i])
		}

		// Create 5 more nodes
		for i := 0; i < 5; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"id": IntValue(int64(i + 100))})
		}

		// Expected: (20 - 3 + 5) = 22 nodes
		expectedNodes := 22
		stats := gs.stats
		if stats.NodeCount != uint64(expectedNodes) {
			t.Errorf("Before crash: Expected NodeCount=%d, got %d", expectedNodes, stats.NodeCount)
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Logf("After many operations: %d nodes, %d edges, simulating crash...", stats.NodeCount, stats.EdgeCount)
	}

	// Phase 2: Recover and verify
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify node count
		stats := gs.stats
		persons, _ := gs.FindNodesByLabel("Person")
		actualNodes := len(persons)

		if stats.NodeCount != uint64(actualNodes) {
			t.Errorf("After recovery: NodeCount mismatch - stats=%d, actual=%d",
				stats.NodeCount, actualNodes)
		}

		// Verify edge count
		knows, _ := gs.FindEdgesByType("KNOWS")
		actualEdges := len(knows)

		if stats.EdgeCount != uint64(actualEdges) {
			t.Errorf("After recovery: EdgeCount mismatch - stats=%d, actual=%d",
				stats.EdgeCount, actualEdges)
		}

		t.Log("Statistics remain accurate after many operations and crash recovery")
	}
}

// TestGraphStorage_StatisticsMultipleRecoveries tests stats through multiple crash/recovery cycles
func TestGraphStorage_StatisticsMultipleRecoveries(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Track all crashed storages for cleanup
	var crashedStorages []*GraphStorage
	t.Cleanup(func() {
		for _, gs := range crashedStorages {
			gs.Close()
		}
	})

	// Cycle 1: Create some data, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}
		crashedStorages = append(crashedStorages, gs)

		var nodeIDs []uint64
		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			nodeIDs = append(nodeIDs, node.ID)
		}
		for i := 0; i < 7; i++ {
			gs.CreateEdge(nodeIDs[i%5], nodeIDs[(i+1)%5], "KNOWS", nil, 1.0)
		}

		// DON'T CLOSE - simulate crash (cleanup registered above)
		t.Log("Cycle 1: Created 5 nodes, 7 edges")
	}

	// Cycle 2: Recover, add more, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed cycle 2 recovery: %v", err)
		}
		crashedStorages = append(crashedStorages, gs)

		stats := gs.stats
		if stats.NodeCount != 5 || stats.EdgeCount != 7 {
			t.Errorf("Cycle 2: Expected 5 nodes, 7 edges, got %d nodes, %d edges",
				stats.NodeCount, stats.EdgeCount)
		}

		// Get existing nodes to create edges between old and new nodes
		persons, _ := gs.FindNodesByLabel("Person")
		var nodeIDs []uint64
		for _, node := range persons {
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Add 3 more nodes
		for i := 0; i < 3; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Create 5 edges using actual node IDs (now have 8 nodes total)
		for i := 0; i < 5; i++ {
			gs.CreateEdge(nodeIDs[i%8], nodeIDs[(i+1)%8], "KNOWS", nil, 1.0)
		}

		// DON'T CLOSE - simulate crash (cleanup registered above)
		t.Logf("Cycle 2: Now have %d nodes, %d edges", gs.stats.NodeCount, gs.stats.EdgeCount)
	}

	// Cycle 3: Recover, verify final stats
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed final recovery: %v", err)
		}
		defer gs.Close()

		stats := gs.stats
		expectedNodes := 8 // 5 + 3
		expectedEdges := 12 // 7 + 5

		if stats.NodeCount != uint64(expectedNodes) {
			t.Errorf("Final: Expected NodeCount=%d, got %d", expectedNodes, stats.NodeCount)
		}
		if stats.EdgeCount != uint64(expectedEdges) {
			t.Errorf("Final: Expected EdgeCount=%d, got %d", expectedEdges, stats.EdgeCount)
		}

		// Verify against actual counts
		persons, _ := gs.FindNodesByLabel("Person")
		knows, _ := gs.FindEdgesByType("KNOWS")

		if len(persons) != expectedNodes {
			t.Errorf("Final: Actual node count %d doesn't match expected %d", len(persons), expectedNodes)
		}
		if len(knows) != expectedEdges {
			t.Errorf("Final: Actual edge count %d doesn't match expected %d", len(knows), expectedEdges)
		}

		t.Log("Statistics remain accurate through multiple crash/recovery cycles")
	}
}

// TestGraphStorage_StatisticsWithConcurrentOperations tests stats accuracy under concurrency
func TestGraphStorage_StatisticsWithConcurrentOperations(t *testing.T) {
	if isRaceEnabled() {
		t.Skip("Skipping heavy concurrency test with race detector")
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

	// Create initial nodes
	var nodeIDs []uint64
	for i := 0; i < 10; i++ {
		node, _ := gs.CreateNode([]string{"Person"}, nil)
		nodeIDs = append(nodeIDs, node.ID)
	}

	// Concurrently create 100 more nodes
	numGoroutines := 10
	nodesPerGoroutine := 10
	expectedNewNodes := numGoroutines * nodesPerGoroutine

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < nodesPerGoroutine; j++ {
				gs.CreateNode([]string{"Person"}, map[string]Value{
					"worker": IntValue(int64(workerID)),
				})
			}
		}(i)
	}
	wg.Wait()

	// Verify statistics
	stats := gs.stats
	expectedTotal := 10 + expectedNewNodes
	if stats.NodeCount != uint64(expectedTotal) {
		t.Errorf("After concurrent operations: Expected NodeCount=%d, got %d",
			expectedTotal, stats.NodeCount)
	}

	// Verify against actual count
	persons, _ := gs.FindNodesByLabel("Person")
	if len(persons) != expectedTotal {
		t.Errorf("Actual node count %d doesn't match stats %d", len(persons), stats.NodeCount)
	}

	t.Logf("Statistics remain accurate with concurrent operations: %d nodes", stats.NodeCount)
}
