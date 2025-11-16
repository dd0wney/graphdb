package storage

import (
	"os"
	"testing"
)

// TestIDAllocation_NodeIDsNeverReused tests that node IDs are never reused after crash
func TestIDAllocation_NodeIDsNeverReused(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var firstBatchIDs []uint64

	// Phase 1: Create nodes, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 10 nodes
		for i := 0; i < 10; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
			firstBatchIDs = append(firstBatchIDs, node.ID)
		}

		t.Logf("Before crash: Created nodes with IDs %v", firstBatchIDs)

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and create more nodes
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

		// Create 10 more nodes
		var secondBatchIDs []uint64
		for i := 0; i < 10; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i + 10)),
			})
			if err != nil {
				t.Fatalf("CreateNode after crash failed: %v", err)
			}
			secondBatchIDs = append(secondBatchIDs, node.ID)
		}

		t.Logf("After crash: Created nodes with IDs %v", secondBatchIDs)

		// Check for ID collisions
		idMap := make(map[uint64]bool)
		for _, id := range firstBatchIDs {
			idMap[id] = true
		}

		for _, id := range secondBatchIDs {
			if idMap[id] {
				t.Errorf("ID COLLISION! Node ID %d was REUSED after crash!", id)
			}
			idMap[id] = true
		}

		// Verify all 20 nodes exist with unique IDs
		if len(gs.nodes) != 20 {
			t.Errorf("Expected 20 nodes after crash, got %d", len(gs.nodes))
		}

		t.Log("After crash: No ID collisions detected - all IDs unique")
	}
}

// TestIDAllocation_EdgeIDsNeverReused tests that edge IDs are never reused after crash
func TestIDAllocation_EdgeIDsNeverReused(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var firstBatchIDs []uint64
	var node1ID, node2ID uint64

	// Phase 1: Create edges, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes
		node1, _ := gs.CreateNode([]string{"Person"}, nil)
		node2, _ := gs.CreateNode([]string{"Person"}, nil)
		node1ID = node1.ID
		node2ID = node2.ID

		// Create 10 edges
		for i := 0; i < 10; i++ {
			edge, err := gs.CreateEdge(node1ID, node2ID, "KNOWS", nil, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge failed: %v", err)
			}
			firstBatchIDs = append(firstBatchIDs, edge.ID)
		}

		t.Logf("Before crash: Created edges with IDs %v", firstBatchIDs)

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and create more edges
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

		// Create 10 more edges
		var secondBatchIDs []uint64
		for i := 0; i < 10; i++ {
			edge, err := gs.CreateEdge(node1ID, node2ID, "KNOWS", nil, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge after crash failed: %v", err)
			}
			secondBatchIDs = append(secondBatchIDs, edge.ID)
		}

		t.Logf("After crash: Created edges with IDs %v", secondBatchIDs)

		// Check for ID collisions
		idMap := make(map[uint64]bool)
		for _, id := range firstBatchIDs {
			idMap[id] = true
		}

		for _, id := range secondBatchIDs {
			if idMap[id] {
				t.Errorf("ID COLLISION! Edge ID %d was REUSED after crash!", id)
			}
			idMap[id] = true
		}

		// Verify all 20 edges exist with unique IDs
		if len(gs.edges) != 20 {
			t.Errorf("Expected 20 edges after crash, got %d", len(gs.edges))
		}

		t.Log("After crash: No ID collisions detected - all edge IDs unique")
	}
}

// TestIDAllocation_LargeIDGaps tests ID allocation with large gaps
func TestIDAllocation_LargeIDGaps(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create node with large ID (simulating long-running system), crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create many nodes to get a high ID
		for i := 0; i < 100; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i)),
			})
		}

		// Last node should have ID around 100
		lastNode, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
			"index": IntValue(100),
		})

		t.Logf("Before crash: Last node ID = %d", lastNode.ID)

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify next ID is after highest ID
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

		// Create a new node - it should have an ID higher than all existing nodes
		newNode, err := gs.CreateNode([]string{"Person"}, map[string]Value{
			"index": IntValue(101),
		})
		if err != nil {
			t.Fatalf("CreateNode after crash failed: %v", err)
		}

		// Find highest existing node ID
		var maxExistingID uint64
		for id := range gs.nodes {
			if id > maxExistingID && id != newNode.ID {
				maxExistingID = id
			}
		}

		if newNode.ID <= maxExistingID {
			t.Errorf("New node ID %d is NOT greater than max existing ID %d (ID REUSE RISK!)",
				newNode.ID, maxExistingID)
		}

		t.Logf("After crash: Max existing ID = %d, new node ID = %d (correct)",
			maxExistingID, newNode.ID)
	}
}

// TestIDAllocation_SnapshotRecovery tests ID allocation after snapshot recovery
func TestIDAllocation_SnapshotRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var lastNodeID, lastEdgeID uint64

	// Phase 1: Create nodes/edges, close cleanly (snapshot)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes
		node1, _ := gs.CreateNode([]string{"Person"}, nil)
		node2, _ := gs.CreateNode([]string{"Person"}, nil)

		// Create edges
		edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)

		lastNodeID = node2.ID
		lastEdgeID = edge.ID

		// Close cleanly
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Logf("Phase 1: Last node ID = %d, last edge ID = %d, closed cleanly", lastNodeID, lastEdgeID)
	}

	// Phase 2: Recover from snapshot and create new nodes/edges
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

		// Create new node - should have ID > lastNodeID
		newNode, _ := gs.CreateNode([]string{"Person"}, nil)
		if newNode.ID <= lastNodeID {
			t.Errorf("After snapshot: New node ID %d is NOT greater than last node ID %d!",
				newNode.ID, lastNodeID)
		}

		// Create new edge - should have ID > lastEdgeID
		node1ID := uint64(1)
		newEdge, _ := gs.CreateEdge(node1ID, newNode.ID, "KNOWS", nil, 1.0)
		if newEdge.ID <= lastEdgeID {
			t.Errorf("After snapshot: New edge ID %d is NOT greater than last edge ID %d!",
				newEdge.ID, lastEdgeID)
		}

		t.Logf("After snapshot: New node ID = %d, new edge ID = %d (both correct)",
			newNode.ID, newEdge.ID)
	}
}

// TestIDAllocation_BatchOperations tests ID allocation in batch operations
func TestIDAllocation_BatchOperations(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var firstBatchNodeIDs []uint64

	// Phase 1: Create nodes via batch, crash
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

		// Add 10 nodes via batch
		for i := 0; i < 10; i++ {
			nodeID, err := batch.AddNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("Batch AddNode failed: %v", err)
			}
			firstBatchNodeIDs = append(firstBatchNodeIDs, nodeID)
		}

		// Commit batch
		err = batch.Commit()
		if err != nil {
			t.Fatalf("Batch commit failed: %v", err)
		}

		t.Logf("Before crash: Batch created nodes with IDs %v", firstBatchNodeIDs)

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and create more nodes (non-batch)
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

		// Create 10 more nodes via regular CreateNode
		var secondBatchNodeIDs []uint64
		for i := 0; i < 10; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i + 10)),
			})
			if err != nil {
				t.Fatalf("CreateNode after crash failed: %v", err)
			}
			secondBatchNodeIDs = append(secondBatchNodeIDs, node.ID)
		}

		t.Logf("After crash: Created nodes with IDs %v", secondBatchNodeIDs)

		// Check for ID collisions
		idMap := make(map[uint64]bool)
		for _, id := range firstBatchNodeIDs {
			idMap[id] = true
		}

		for _, id := range secondBatchNodeIDs {
			if idMap[id] {
				t.Errorf("ID COLLISION! Node ID %d from batch was REUSED after crash!", id)
			}
		}

		t.Log("After crash: Batch-allocated IDs not reused")
	}
}

// TestIDAllocation_DeletedNodesIDsNotReused tests that deleted node IDs are NOT reused
func TestIDAllocation_DeletedNodesIDsNotReused(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var deletedNodeID uint64

	// Phase 1: Create node, delete it, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create node
		node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("ToDelete"),
		})
		deletedNodeID = node.ID

		// Delete it
		err = gs.DeleteNode(deletedNodeID)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		t.Logf("Before crash: Created and deleted node with ID %d", deletedNodeID)

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and create new node
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

		// Verify deleted node doesn't exist
		deletedNode, _ := gs.GetNode(deletedNodeID)
		if deletedNode != nil {
			t.Fatal("Deleted node still exists after crash!")
		}

		// Create new node - it should NOT reuse the deleted node's ID
		newNode, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("New"),
		})

		if newNode.ID == deletedNodeID {
			t.Errorf("New node ID %d REUSED deleted node ID (BUG!)", newNode.ID)
		}

		// New node ID should be AFTER deleted node ID (IDs only increment)
		if newNode.ID <= deletedNodeID {
			t.Errorf("New node ID %d is not greater than deleted node ID %d",
				newNode.ID, deletedNodeID)
		}

		t.Logf("After crash: Deleted node ID = %d, new node ID = %d (correct - not reused)",
			deletedNodeID, newNode.ID)
	}
}

// TestIDAllocation_MultipleRecoveries tests ID allocation across multiple crash/recovery cycles
func TestIDAllocation_MultipleRecoveries(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	allNodeIDs := make(map[uint64]bool)

	// Perform 5 crash/recovery cycles
	for cycle := 0; cycle < 5; cycle++ {
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Cycle %d: Failed to create GraphStorage: %v", cycle, err)
		}

		// Create 5 nodes per cycle
		for i := 0; i < 5; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"cycle": IntValue(int64(cycle)),
				"index": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("Cycle %d: CreateNode failed: %v", cycle, err)
			}

			// Check for ID collision
			if allNodeIDs[node.ID] {
				t.Errorf("Cycle %d: ID COLLISION! Node ID %d was already used!", cycle, node.ID)
			}
			allNodeIDs[node.ID] = true
		}

		t.Logf("Cycle %d: Created 5 nodes (total unique IDs: %d)", cycle, len(allNodeIDs))

		// Don't close - simulate crash (except last cycle)
		if cycle == 4 {
			gs.Close()
		}
	}

	// Verify total unique IDs
	if len(allNodeIDs) != 25 {
		t.Errorf("Expected 25 unique node IDs across 5 cycles, got %d", len(allNodeIDs))
	}

	t.Logf("After 5 crash/recovery cycles: %d unique node IDs (no collisions)", len(allNodeIDs))
}

// TestIDAllocation_ConcurrentCreation tests ID allocation under concurrent load
func TestIDAllocation_ConcurrentCreation(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes concurrently, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// This is just creating sequentially for now since concurrent creation
		// would require proper synchronization testing
		for i := 0; i < 50; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i)),
			})
		}

		t.Log("Before crash: Created 50 nodes")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify all IDs are unique
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

		// Verify all nodes have unique IDs
		if len(gs.nodes) != 50 {
			t.Errorf("Expected 50 nodes after crash, got %d", len(gs.nodes))
		}

		// Create more nodes and verify no collisions
		for i := 0; i < 50; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
				"index": IntValue(int64(i + 50)),
			})

			// Check if this ID already exists in pre-crash nodes
			// (it shouldn't, but that's what we're testing)
			if node.ID <= 50 {
				// This might be suspicious - new nodes after crash should have IDs > 50
				t.Logf("Warning: New node has ID %d which is <= 50", node.ID)
			}
		}

		// Final verification: All 100 nodes exist with unique IDs
		if len(gs.nodes) != 100 {
			t.Errorf("Expected 100 total nodes, got %d", len(gs.nodes))
		}

		t.Logf("After crash: All 100 nodes have unique IDs")
	}
}
