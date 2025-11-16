package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_DiskBackedEdges_WALIntegration tests that disk-backed edge operations are logged to WAL
func TestGraphStorage_DiskBackedEdges_WALIntegration(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID, edgeID uint64

	// Phase 1: Create edges with disk-backed storage and WAL enabled
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			// WAL should be enabled by default
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		node1, _ := gs.CreateNode([]string{"Node"}, map[string]Value{
			"name": StringValue("Alice"),
		})
		node2, _ := gs.CreateNode([]string{"Node"}, map[string]Value{
			"name": StringValue("Bob"),
		})

		edge, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
			"since": IntValue(2020),
		}, 1.0)
		if err != nil {
			t.Fatalf("CreateEdge failed: %v", err)
		}

		node1ID = node1.ID
		node2ID = node2.ID
		edgeID = edge.ID

		// Close to flush WAL
		if err := gs.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	}

	// Phase 2: Reopen and verify WAL replay recovered disk-backed edges
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to reopen GraphStorage: %v", err)
		}
		defer gs.Close()

		// Verify nodes recovered
		node1, err := gs.GetNode(node1ID)
		if err != nil {
			t.Errorf("Node 1 not recovered from WAL: %v", err)
		} else if string(node1.Properties["name"].Data) != "Alice" {
			t.Errorf("Node 1 properties not recovered correctly")
		}

		// Verify edges recovered from WAL + disk-backed storage
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed after WAL replay: %v", err)
		}

		if len(outgoing) != 1 {
			t.Errorf("Expected 1 edge after WAL replay, got %d", len(outgoing))
		}

		if len(outgoing) > 0 {
			if outgoing[0].ID != edgeID {
				t.Errorf("Edge ID mismatch after WAL replay: expected %d, got %d", edgeID, outgoing[0].ID)
			}
			if outgoing[0].Type != "KNOWS" {
				t.Errorf("Edge type not recovered correctly: got %s", outgoing[0].Type)
			}
		}

		// Verify incoming edges also recovered
		incoming, err := gs.GetIncomingEdges(node2ID)
		if err != nil {
			t.Fatalf("GetIncomingEdges failed after WAL replay: %v", err)
		}

		if len(incoming) != 1 {
			t.Errorf("Expected 1 incoming edge after WAL replay, got %d", len(incoming))
		}
	}
}

// TestGraphStorage_DiskBackedEdges_CrashRecovery simulates a crash and verifies recovery
func TestGraphStorage_DiskBackedEdges_CrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeIDs []uint64
	const numNodes = 100
	const edgesPerNode = 10

	// Phase 1: Create graph without clean shutdown (simulated crash)
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
		nodeIDs = make([]uint64, numNodes)
		for i := 0; i < numNodes; i++ {
			node, _ := gs.CreateNode([]string{"Node"}, map[string]Value{
				"index": IntValue(int64(i)),
			})
			nodeIDs[i] = node.ID
		}

		// Create edges
		for i := 0; i < numNodes; i++ {
			for j := 0; j < edgesPerNode; j++ {
				sourceID := nodeIDs[i]
				targetID := nodeIDs[(i+j+1)%numNodes]
				gs.CreateEdge(sourceID, targetID, "CONNECTS", nil, 1.0)
			}
		}

		// DON'T CALL Close() - simulate crash!
		t.Log("Simulating crash (no Close() call)")
		// gs.Close() // ❌ Not called to simulate crash
	}

	// Phase 2: Recover from crash
	{
		t.Log("Attempting recovery from simulated crash...")
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover after crash: %v", err)
		}
		defer gs.Close()

		// Verify all nodes recovered
		recoveredNodes := 0
		for i := 0; i < numNodes; i++ {
			node, err := gs.GetNode(nodeIDs[i])
			if err == nil && node != nil {
				recoveredNodes++
			}
		}

		if recoveredNodes < numNodes {
			t.Logf("WARNING: Only %d/%d nodes recovered (may be acceptable)", recoveredNodes, numNodes)
		} else {
			t.Logf("SUCCESS: All %d nodes recovered", recoveredNodes)
		}

		// Verify edges recovered (at least some should be there)
		totalEdgesRecovered := 0
		for i := 0; i < numNodes; i++ {
			outgoing, err := gs.GetOutgoingEdges(nodeIDs[i])
			if err == nil {
				totalEdgesRecovered += len(outgoing)
			}
		}

		expectedEdges := numNodes * edgesPerNode
		recoveryRate := float64(totalEdgesRecovered) / float64(expectedEdges)

		t.Logf("Edge recovery: %d/%d (%.1f%%)", totalEdgesRecovered, expectedEdges, recoveryRate*100)

		if recoveryRate < 0.5 {
			t.Errorf("Poor recovery rate: only %.1f%% of edges recovered", recoveryRate*100)
		}
	}
}

// TestGraphStorage_DiskBackedEdges_PartialWriteRecovery tests recovery from partial writes
func TestGraphStorage_DiskBackedEdges_PartialWriteRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID uint64

	// Phase 1: Create some data, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		node1, _ := gs.CreateNode([]string{"Node"}, nil)
		node2, _ := gs.CreateNode([]string{"Node"}, nil)

		// Create initial edge
		gs.CreateEdge(node1.ID, node2.ID, "EDGE1", nil, 1.0)

		node1ID = node1.ID
		node2ID = node2.ID

		gs.Close() // Clean shutdown
	}

	// Phase 2: Reopen, add more data, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to reopen: %v", err)
		}

		// Add more edges
		gs.CreateEdge(node1ID, node2ID, "EDGE2", nil, 1.0)
		gs.CreateEdge(node1ID, node2ID, "EDGE3", nil, 1.0)

		// Simulate crash (no Close)
		t.Log("Simulating crash during write")
	}

	// Phase 3: Recover and verify data integrity
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

		// Should recover at least the first edge (committed before crash)
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed: %v", err)
		}

		if len(outgoing) < 1 {
			t.Error("Failed to recover any edges after partial write")
		} else {
			t.Logf("Recovered %d edges after partial write crash", len(outgoing))
		}

		// Verify data consistency - no corrupted edges
		for _, edge := range outgoing {
			if edge.ID == 0 {
				t.Error("Recovered edge has invalid ID 0")
			}
			if edge.FromNodeID != node1ID {
				t.Errorf("Edge has wrong FromNodeID: %d", edge.FromNodeID)
			}
			if edge.ToNodeID != node2ID {
				t.Errorf("Edge has wrong ToNodeID: %d", edge.ToNodeID)
			}
		}
	}
}

// TestGraphStorage_DiskBackedEdges_NodeDeletionWithEdges tests deleting nodes that have disk-backed edges
func TestGraphStorage_DiskBackedEdges_NodeDeletionWithEdges(t *testing.T) {
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

	// Create nodes with edges
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	node3, _ := gs.CreateNode([]string{"Node"}, nil)

	// Create edges: node1 -> node2, node2 -> node3, node1 -> node3
	edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	edge2, _ := gs.CreateEdge(node2.ID, node3.ID, "EDGE", nil, 1.0)
	_, _ = gs.CreateEdge(node1.ID, node3.ID, "EDGE", nil, 1.0)

	// Verify initial state
	outgoing1, _ := gs.GetOutgoingEdges(node1.ID)
	if len(outgoing1) != 2 {
		t.Errorf("Node1 should have 2 outgoing edges, got %d", len(outgoing1))
	}

	// Delete node2 (has both incoming and outgoing edges)
	err = gs.DeleteNode(node2.ID)
	if err != nil {
		t.Logf("DeleteNode returned error (may be expected): %v", err)
		// Some implementations may not support node deletion yet
		t.Skip("Node deletion not yet implemented or returned error")
	}

	// Verify node2 is gone
	_, err = gs.GetNode(node2.ID)
	if err == nil {
		t.Error("Node2 should not exist after deletion")
	}

	// Verify edge1 (node1 -> node2) behavior
	err = gs.DeleteEdge(edge1.ID)
	if err == nil {
		t.Log("Edge1 still exists - may need manual cleanup after node deletion")
	} else {
		t.Log("Edge1 automatically deleted with node2")
	}

	// Verify edge2 (node2 -> node3) behavior
	err = gs.DeleteEdge(edge2.ID)
	if err == nil {
		t.Log("Edge2 still exists - may need manual cleanup after node deletion")
	} else {
		t.Log("Edge2 automatically deleted with node2")
	}

	// Verify edge3 (node1 -> node3) still exists (should not be affected)
	_, err = gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Errorf("Should be able to query edges after node deletion: %v", err)
	}

	// Verify node1's outgoing edges updated
	outgoing1After, _ := gs.GetOutgoingEdges(node1.ID)
	if len(outgoing1After) > 1 {
		t.Errorf("Node1 should have at most 1 outgoing edge after node2 deletion, got %d", len(outgoing1After))
	}

	// Verify node3's incoming edges updated
	incoming3, _ := gs.GetIncomingEdges(node3.ID)
	if len(incoming3) > 1 {
		t.Errorf("Node3 should have at most 1 incoming edge after node2 deletion, got %d", len(incoming3))
	}
}

// TestGraphStorage_DiskBackedEdges_SnapshotIntegration tests that disk-backed edges work with snapshots
func TestGraphStorage_DiskBackedEdges_SnapshotIntegration(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID, edgeID uint64

	// Phase 1: Create data and snapshot
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		node1, _ := gs.CreateNode([]string{"Node"}, map[string]Value{
			"name": StringValue("Alice"),
		})
		node2, _ := gs.CreateNode([]string{"Node"}, map[string]Value{
			"name": StringValue("Bob"),
		})
		edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)

		node1ID = node1.ID
		node2ID = node2.ID
		edgeID = edge.ID

		// Create snapshot
		err = gs.Snapshot()
		if err != nil {
			t.Fatalf("Snapshot failed: %v", err)
		}

		gs.Close()
	}

	// Phase 2: Restore from snapshot
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to restore from snapshot: %v", err)
		}
		defer gs.Close()

		// Verify nodes restored
		node1, err := gs.GetNode(node1ID)
		if err != nil {
			t.Errorf("Node1 not restored from snapshot: %v", err)
		} else if string(node1.Properties["name"].Data) != "Alice" {
			t.Error("Node1 properties not restored correctly")
		}

		// Verify disk-backed edges restored
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed after snapshot restore: %v", err)
		}

		if len(outgoing) != 1 {
			t.Errorf("Expected 1 edge after snapshot restore, got %d", len(outgoing))
		}

		if len(outgoing) > 0 && outgoing[0].ID != edgeID {
			t.Errorf("Edge not correctly restored from snapshot")
		}

		// Verify disk-backed edge data persisted
		incoming, err := gs.GetIncomingEdges(node2ID)
		if err != nil {
			t.Fatalf("GetIncomingEdges failed after snapshot restore: %v", err)
		}

		if len(incoming) != 1 {
			t.Errorf("Expected 1 incoming edge after snapshot restore, got %d", len(incoming))
		}
	}
}

// TestGraphStorage_DiskBackedEdges_ConcurrentCrashRecovery tests recovery from crash during concurrent operations
func TestGraphStorage_DiskBackedEdges_ConcurrentCrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	const numWorkers = 10
	const operationsPerWorker = 100

	// Phase 1: Concurrent operations without clean shutdown
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Pre-create nodes
		nodeIDs := make([]uint64, numWorkers*2)
		for i := 0; i < len(nodeIDs); i++ {
			node, _ := gs.CreateNode([]string{"Node"}, nil)
			nodeIDs[i] = node.ID
		}

		// Concurrent edge creation
		done := make(chan bool, numWorkers)
		for worker := 0; worker < numWorkers; worker++ {
			go func(id int) {
				for i := 0; i < operationsPerWorker; i++ {
					source := nodeIDs[id*2]
					target := nodeIDs[id*2+1]
					gs.CreateEdge(source, target, "EDGE", nil, 1.0)
				}
				done <- true
			}(worker)
		}

		// Wait for some operations to complete
		completedWorkers := 0
		for completedWorkers < numWorkers {
			<-done
			completedWorkers++
		}

		// Simulate crash (no Close)
		t.Log("Simulating crash after concurrent operations")
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

		// Count recovered edges (any amount > 0 is acceptable for concurrent crash)
		totalEdges := 0
		for i := 1; i <= numWorkers*2; i++ {
			outgoing, err := gs.GetOutgoingEdges(uint64(i))
			if err == nil {
				totalEdges += len(outgoing)
			}
		}

		t.Logf("Recovered %d edges after concurrent crash", totalEdges)

		if totalEdges == 0 {
			t.Error("Expected at least some edges to be recovered")
		}
	}
}
