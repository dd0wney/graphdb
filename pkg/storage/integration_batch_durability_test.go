package storage

import (
	"os"
	"testing"
)

// TestBatchDurability_CrashAfterCommit tests if batch operations survive crash
func TestBatchDurability_CrashAfterCommit(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create batch, commit, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create batch with multiple operations
		batch := gs.BeginBatch()

		// Add 5 nodes
		var nodeIDs []uint64
		for i := 0; i < 5; i++ {
			nodeID, err := batch.AddNode([]string{"Person"}, map[string]Value{
				"id":   IntValue(int64(i)),
				"name": StringValue("User" + string(rune('A'+i))),
			})
			if err != nil {
				t.Fatalf("AddNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, nodeID)
		}

		// Add 3 edges
		edgeIDs := make([]uint64, 0)
		for i := 0; i < 3; i++ {
			edgeID, err := batch.AddEdge(nodeIDs[i], nodeIDs[i+1], "KNOWS", map[string]Value{
				"since": IntValue(2020 + int64(i)),
			}, 1.0)
			if err != nil {
				t.Fatalf("AddEdge failed: %v", err)
			}
			edgeIDs = append(edgeIDs, edgeID)
		}

		// Commit batch
		if err := batch.Commit(); err != nil {
			t.Fatalf("Batch commit failed: %v", err)
		}

		// Verify data exists before crash
		if len(gs.nodes) != 5 {
			t.Fatalf("Expected 5 nodes before crash, got %d", len(gs.nodes))
		}
		if len(gs.edges) != 3 {
			t.Fatalf("Expected 3 edges before crash, got %d", len(gs.edges))
		}

		t.Logf("Before crash: %d nodes, %d edges", len(gs.nodes), len(gs.edges))

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
	}

	// Phase 2: Recover and verify batch operations persisted
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

		// Check if batch-created nodes survived crash
		if len(gs.nodes) != 5 {
			t.Errorf("After crash: Expected 5 nodes, got %d", len(gs.nodes))
			t.Errorf("Batch operations were LOST after crash!")
		}

		// Check if batch-created edges survived crash
		if len(gs.edges) != 3 {
			t.Errorf("After crash: Expected 3 edges, got %d", len(gs.edges))
			t.Errorf("Batch edge operations were LOST after crash!")
		}

		// Verify specific data
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 5 {
			t.Errorf("After crash: Expected 5 Person nodes, got %d", len(persons))
		}

		knows, _ := gs.FindEdgesByType("KNOWS")
		if len(knows) != 3 {
			t.Errorf("After crash: Expected 3 KNOWS edges, got %d", len(knows))
		}

		t.Logf("After crash recovery: %d nodes, %d edges (expected 5 nodes, 3 edges)",
			len(gs.nodes), len(gs.edges))
	}
}

// TestBatchDurability_MixedOperations tests batch with creates, updates, and deletes
func TestBatchDurability_MixedOperations(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create initial data
	var node1ID, node2ID, node3ID uint64
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 3 initial nodes
		node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Alice")})
		node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Bob")})
		node3, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Charlie")})
		node1ID = node1.ID
		node2ID = node2.ID
		node3ID = node3.ID

		// Close cleanly so they're persisted
		gs.Close()
	}

	// Phase 2: Use batch to mix operations, then crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}

		// Create batch with mixed operations
		batch := gs.BeginBatch()

		// Update existing node
		batch.UpdateNode(node1ID, map[string]Value{
			"name": StringValue("Alice Updated"),
			"age":  IntValue(30),
		})

		// Delete an existing node
		batch.DeleteNode(node3ID)

		// Add new nodes
		newNode, _ := batch.AddNode([]string{"Person"}, map[string]Value{"name": StringValue("Dave")})

		// Add edge
		batch.AddEdge(node1ID, node2ID, "KNOWS", nil, 1.0)
		batch.AddEdge(node2ID, newNode, "KNOWS", nil, 1.0)

		// Commit
		if err := batch.Commit(); err != nil {
			t.Fatalf("Batch commit failed: %v", err)
		}

		// Verify before crash
		node1After, _ := gs.GetNode(node1ID)
		if string(node1After.Properties["name"].Data) != "Alice Updated" {
			t.Fatal("Update didn't work before crash")
		}

		t.Logf("Before crash: Applied mixed batch operations")

		// DON'T CLOSE - crash
	}

	// Phase 3: Recover and verify
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

		// Check if update survived
		node1, _ := gs.GetNode(node1ID)
		if node1 == nil {
			t.Error("Node1 disappeared after crash")
		} else if string(node1.Properties["name"].Data) != "Alice Updated" {
			t.Errorf("After crash: Update lost - expected 'Alice Updated', got '%s'",
				string(node1.Properties["name"].Data))
		}

		// Check if delete survived
		node3, _ := gs.GetNode(node3ID)
		if node3 != nil {
			t.Errorf("After crash: Deleted node still exists!")
		}

		// Check if new nodes survived
		persons, _ := gs.FindNodesByLabel("Person")
		expectedNodes := 3 // Alice, Bob, Dave (Charlie deleted)
		if len(persons) != expectedNodes {
			t.Errorf("After crash: Expected %d Person nodes, got %d",
				expectedNodes, len(persons))
		}

		// Check if edges survived
		knows, _ := gs.FindEdgesByType("KNOWS")
		expectedEdges := 2
		if len(knows) != expectedEdges {
			t.Errorf("After crash: Expected %d KNOWS edges, got %d",
				expectedEdges, len(knows))
		}

		t.Logf("After crash: Verified batch operations persistence")
	}
}

// TestBatchDurability_LargeBatch tests large batch durability
func TestBatchDurability_LargeBatch(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	const numNodes = 100
	const numEdges = 150

	// Phase 1: Create large batch and crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		batch := gs.BeginBatch()

		// Add many nodes
		var nodeIDs []uint64
		for i := 0; i < numNodes; i++ {
			nodeID, err := batch.AddNode([]string{"TestNode"}, map[string]Value{
				"index": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("AddNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, nodeID)
		}

		// Add many edges
		for i := 0; i < numEdges; i++ {
			from := nodeIDs[i%numNodes]
			to := nodeIDs[(i+1)%numNodes]
			_, err := batch.AddEdge(from, to, "LINK", nil, 1.0)
			if err != nil {
				t.Fatalf("AddEdge failed: %v", err)
			}
		}

		// Commit large batch
		if err := batch.Commit(); err != nil {
			t.Fatalf("Large batch commit failed: %v", err)
		}

		t.Logf("Before crash: Committed batch with %d nodes and %d edges", numNodes, numEdges)

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify all data
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

		// Check node count
		if len(gs.nodes) != numNodes {
			t.Errorf("After crash: Expected %d nodes, got %d (LOST: %d nodes)",
				numNodes, len(gs.nodes), numNodes-len(gs.nodes))
		}

		// Check edge count
		if len(gs.edges) != numEdges {
			t.Errorf("After crash: Expected %d edges, got %d (LOST: %d edges)",
				numEdges, len(gs.edges), numEdges-len(gs.edges))
		}

		// Verify statistics
		stats := gs.GetStatistics()
		if stats.NodeCount != uint64(numNodes) {
			t.Errorf("After crash: Stats show %d nodes, expected %d",
				stats.NodeCount, numNodes)
		}
		if stats.EdgeCount != uint64(numEdges) {
			t.Errorf("After crash: Stats show %d edges, expected %d",
				stats.EdgeCount, numEdges)
		}

		t.Logf("After crash recovery: %d nodes, %d edges (large batch durability check)",
			len(gs.nodes), len(gs.edges))
	}
}

// TestBatchDurability_SnapshotAfterBatch tests batch operations preserved in snapshot
func TestBatchDurability_SnapshotAfterBatch(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Batch operations, then clean close (snapshot)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		batch := gs.BeginBatch()

		// Add nodes via batch
		for i := 0; i < 10; i++ {
			batch.AddNode([]string{"BatchNode"}, map[string]Value{
				"batch_index": IntValue(int64(i)),
			})
		}

		// Commit batch
		if err := batch.Commit(); err != nil {
			t.Fatalf("Batch commit failed: %v", err)
		}

		// Close cleanly (creates snapshot)
		if err := gs.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Phase 1: Committed batch and created snapshot")
	}

	// Phase 2: Recover from snapshot and verify batch data
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover from snapshot: %v", err)
		}
		defer gs.Close()

		// Verify batch nodes in snapshot
		batchNodes, _ := gs.FindNodesByLabel("BatchNode")
		if len(batchNodes) != 10 {
			t.Errorf("After snapshot recovery: Expected 10 BatchNode nodes, got %d",
				len(batchNodes))
		}

		t.Logf("After snapshot recovery: Successfully recovered %d batch nodes",
			len(batchNodes))
	}
}

// TestBatchDurability_EmptyBatch tests empty batch doesn't cause issues
func TestBatchDurability_EmptyBatch(t *testing.T) {
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

	// Create and commit empty batch
	batch := gs.BeginBatch()
	if err := batch.Commit(); err != nil {
		t.Errorf("Empty batch commit failed: %v", err)
	}

	// Verify no crash
	stats := gs.GetStatistics()
	if stats.NodeCount != 0 {
		t.Errorf("Empty batch created %d nodes", stats.NodeCount)
	}
}
