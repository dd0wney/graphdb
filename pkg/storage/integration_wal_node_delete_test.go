package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_DiskBackedEdges_DeleteNodeWALReplay tests that deleted nodes stay deleted after crash recovery
func TestGraphStorage_DiskBackedEdges_DeleteNodeWALReplay(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID, deletedNodeID uint64

	// Phase 1: Create nodes and delete one
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
		deletedNode, _ := gs.CreateNode([]string{"ToDelete"}, nil)

		node1ID = node1.ID
		node2ID = node2.ID
		deletedNodeID = deletedNode.ID

		// Delete the node
		err = gs.DeleteNode(deletedNodeID)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Verify deletion worked
		_, err = gs.GetNode(deletedNodeID)
		if err != ErrNodeNotFound {
			t.Error("Expected ErrNodeNotFound for deleted node")
		}

		// Close cleanly (flush WAL)
		gs.Close()
	}

	// Phase 2: Reopen and verify deleted node stays deleted
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to reopen: %v", err)
		}
		defer gs.Close()

		// Verify deleted node is gone
		_, err = gs.GetNode(deletedNodeID)
		if err != ErrNodeNotFound {
			t.Error("Deleted node came back after recovery!")
		}

		// Verify surviving nodes exist
		_, err = gs.GetNode(node1ID)
		if err != nil {
			t.Errorf("Node1 not found after recovery: %v", err)
		}

		_, err = gs.GetNode(node2ID)
		if err != nil {
			t.Errorf("Node2 not found after recovery: %v", err)
		}
	}
}

// TestGraphStorage_DiskBackedEdges_DeleteNodeCrashRecovery tests node deletion with crash (no Close)
func TestGraphStorage_DiskBackedEdges_DeleteNodeCrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID uint64

	// Phase 1: Create nodes, close cleanly
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
		node2, _ := gs.CreateNode([]string{"ToDelete"}, nil)

		node1ID = node1.ID
		node2ID = node2.ID

		gs.Close() // Clean close
	}

	// Phase 2: Reopen, delete node, crash (no Close)
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Delete node2
		err := gs.DeleteNode(node2ID)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Verify deletion
		_, err = gs.GetNode(node2ID)
		if err != ErrNodeNotFound {
			t.Error("Expected deleted node to not exist")
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Simulating crash after node deletion")
	}

	// Phase 3: Recover and verify deletion persisted via WAL
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

		// Verify node2 is still deleted (WAL replay should have deleted it)
		_, err = gs.GetNode(node2ID)
		if err != ErrNodeNotFound {
			t.Error("Deleted node came back after crash! WAL delete replay failed")
		}

		// Verify node1 still exists
		_, err = gs.GetNode(node1ID)
		if err != nil {
			t.Errorf("Node1 not found after recovery: %v", err)
		}

		t.Log("Node deletion correctly recovered from crash via WAL")
	}
}

// TestGraphStorage_DiskBackedEdges_DeleteNodeWithEdgesCrash tests that cascade edge deletion is durable
func TestGraphStorage_DiskBackedEdges_DeleteNodeWithEdgesCrash(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID, node3ID uint64
	var edge1ID, edge2ID, edge3ID uint64

	// Phase 1: Create nodes and edges, close cleanly
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
		node2, _ := gs.CreateNode([]string{"ToDelete"}, nil)
		node3, _ := gs.CreateNode([]string{"Node"}, nil)

		node1ID = node1.ID
		node2ID = node2.ID
		node3ID = node3.ID

		// Create edges connected to node2
		edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE1", nil, 1.0)
		edge2, _ := gs.CreateEdge(node2.ID, node3.ID, "EDGE2", nil, 1.0)
		edge3, _ := gs.CreateEdge(node1.ID, node3.ID, "EDGE3", nil, 1.0) // Unrelated edge

		edge1ID = edge1.ID
		edge2ID = edge2.ID
		edge3ID = edge3.ID

		gs.Close() // Clean close
	}

	// Phase 2: Reopen, delete node with edges, crash (no Close)
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Delete node2 (should cascade delete edge1 and edge2)
		err := gs.DeleteNode(node2ID)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Verify node2 is deleted
		_, err = gs.GetNode(node2ID)
		if err != ErrNodeNotFound {
			t.Error("Expected deleted node to not exist")
		}

		// Verify edges connected to node2 are deleted
		_, err = gs.GetEdge(edge1ID)
		if err != ErrEdgeNotFound {
			t.Error("Expected edge1 to be cascade deleted")
		}

		_, err = gs.GetEdge(edge2ID)
		if err != ErrEdgeNotFound {
			t.Error("Expected edge2 to be cascade deleted")
		}

		// Verify unrelated edge still exists
		_, err = gs.GetEdge(edge3ID)
		if err != nil {
			t.Error("Unrelated edge should still exist")
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Simulating crash after node deletion with cascade")
	}

	// Phase 3: Recover and verify cascade deletions persisted
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

		// Verify node2 is still deleted
		_, err = gs.GetNode(node2ID)
		if err != ErrNodeNotFound {
			t.Error("Deleted node came back after crash!")
		}

		// Verify cascade deleted edges are still deleted
		_, err = gs.GetEdge(edge1ID)
		if err != ErrEdgeNotFound {
			t.Error("Cascade deleted edge1 came back after crash!")
		}

		_, err = gs.GetEdge(edge2ID)
		if err != ErrEdgeNotFound {
			t.Error("Cascade deleted edge2 came back after crash!")
		}

		// Verify unrelated edge still exists
		_, err = gs.GetEdge(edge3ID)
		if err != nil {
			t.Errorf("Unrelated edge not found after recovery: %v", err)
		}

		// Verify adjacency lists are correct
		outgoing, _ := gs.GetOutgoingEdges(node1ID)
		if len(outgoing) != 1 {
			t.Errorf("Expected 1 outgoing edge from node1, got %d", len(outgoing))
		}

		incoming, _ := gs.GetIncomingEdges(node3ID)
		if len(incoming) != 1 {
			t.Errorf("Expected 1 incoming edge to node3, got %d", len(incoming))
		}

		t.Log("Node deletion with cascade edge deletion correctly recovered from crash via WAL")
	}
}

// TestGraphStorage_DiskBackedEdges_MultipleNodeDeletesWAL tests multiple node deletions in WAL
func TestGraphStorage_DiskBackedEdges_MultipleNodeDeletesWAL(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var survivingNodes []uint64
	var deletedNodes []uint64

	// Phase 1: Create many nodes, delete some, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create 10 nodes
		nodeIDs := make([]uint64, 10)
		for i := 0; i < 10; i++ {
			node, _ := gs.CreateNode([]string{"Node"}, nil)
			nodeIDs[i] = node.ID
		}

		// Delete even-numbered nodes (0, 2, 4, 6, 8)
		for i := 0; i < 10; i += 2 {
			err := gs.DeleteNode(nodeIDs[i])
			if err != nil {
				t.Fatalf("DeleteNode %d failed: %v", i, err)
			}
			deletedNodes = append(deletedNodes, nodeIDs[i])
		}

		// Odd-numbered nodes survive (1, 3, 5, 7, 9)
		for i := 1; i < 10; i += 2 {
			survivingNodes = append(survivingNodes, nodeIDs[i])
		}

		// Verify before crash
		gs.mu.RLock()
		nodeCount := len(gs.nodes)
		gs.mu.RUnlock()

		if nodeCount != 5 {
			t.Fatalf("Expected 5 nodes before crash, got %d", nodeCount)
		}

		// DON'T CLOSE - crash (testCrashableStorage handles cleanup)
		t.Log("Simulating crash after multiple node deletes")
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

		// Verify deleted nodes are gone
		for _, nodeID := range deletedNodes {
			_, err := gs.GetNode(nodeID)
			if err != ErrNodeNotFound {
				t.Errorf("Deleted node %d came back after recovery!", nodeID)
			}
		}

		// Verify surviving nodes exist
		for _, nodeID := range survivingNodes {
			_, err := gs.GetNode(nodeID)
			if err != nil {
				t.Errorf("Surviving node %d not found after recovery: %v", nodeID, err)
			}
		}

		// Verify node count
		gs.mu.RLock()
		nodeCount := len(gs.nodes)
		gs.mu.RUnlock()

		if nodeCount != 5 {
			t.Errorf("Expected 5 nodes after recovery, got %d", nodeCount)
		}

		t.Log("Multiple node deletions correctly recovered from WAL")
	}
}
