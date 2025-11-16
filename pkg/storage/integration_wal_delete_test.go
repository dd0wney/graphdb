package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_DiskBackedEdges_DeleteEdgeWALReplay tests that deleted edges stay deleted after crash recovery
func TestGraphStorage_DiskBackedEdges_DeleteEdgeWALReplay(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID, deletedEdgeID, survivingEdgeID uint64

	// Phase 1: Create edges and delete one
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

		node1ID = node1.ID
		node2ID = node2.ID

		// Create two edges
		edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "DELETED", nil, 1.0)
		edge2, _ := gs.CreateEdge(node1.ID, node2.ID, "SURVIVING", nil, 1.0)

		deletedEdgeID = edge1.ID
		survivingEdgeID = edge2.ID

		// Delete the first edge
		err = gs.DeleteEdge(edge1.ID)
		if err != nil {
			t.Fatalf("DeleteEdge failed: %v", err)
		}

		// Verify deletion worked
		outgoing, _ := gs.GetOutgoingEdges(node1.ID)
		if len(outgoing) != 1 {
			t.Fatalf("Expected 1 edge after deletion, got %d", len(outgoing))
		}
		if outgoing[0].ID != edge2.ID {
			t.Error("Wrong edge survived deletion")
		}

		// Close cleanly (flush WAL)
		gs.Close()
	}

	// Phase 2: Reopen and verify deleted edge stays deleted
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

		// Verify only surviving edge exists
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed: %v", err)
		}

		if len(outgoing) != 1 {
			t.Errorf("Expected 1 edge after recovery, got %d", len(outgoing))
		}

		if len(outgoing) > 0 {
			if outgoing[0].ID == deletedEdgeID {
				t.Error("Deleted edge came back after recovery!")
			}
			if outgoing[0].ID != survivingEdgeID {
				t.Errorf("Wrong edge ID after recovery: expected %d, got %d",
					survivingEdgeID, outgoing[0].ID)
			}
			if outgoing[0].Type != "SURVIVING" {
				t.Errorf("Wrong edge type: expected SURVIVING, got %s", outgoing[0].Type)
			}
		}

		// Verify incoming edges are also correct
		incoming, _ := gs.GetIncomingEdges(node2ID)
		if len(incoming) != 1 {
			t.Errorf("Expected 1 incoming edge after recovery, got %d", len(incoming))
		}
	}
}

// TestGraphStorage_DiskBackedEdges_DeleteEdgeCrashRecovery tests edge deletion with crash (no Close)
func TestGraphStorage_DiskBackedEdges_DeleteEdgeCrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, edge1ID, edge2ID uint64

	// Phase 1: Create edges, close cleanly
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

		node1ID = node1.ID
		_ = node2.ID  // node2 is needed for edge creation

		edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE1", nil, 1.0)
		edge2, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE2", nil, 1.0)

		edge1ID = edge1.ID
		edge2ID = edge2.ID

		gs.Close() // Clean close
	}

	// Phase 2: Reopen, delete edge, crash (no Close)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to reopen: %v", err)
		}

		// Delete edge1
		err = gs.DeleteEdge(edge1ID)
		if err != nil {
			t.Fatalf("DeleteEdge failed: %v", err)
		}

		// Verify deletion
		outgoing, _ := gs.GetOutgoingEdges(node1ID)
		if len(outgoing) != 1 {
			t.Logf("After delete: expected 1 edge, got %d", len(outgoing))
		}

		// DON'T CLOSE - simulate crash
		t.Log("Simulating crash after edge deletion")
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

		// Verify edge1 is still deleted (WAL replay should have deleted it)
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed: %v", err)
		}

		if len(outgoing) != 1 {
			t.Errorf("Expected 1 edge after crash recovery, got %d", len(outgoing))
		}

		if len(outgoing) > 0 {
			if outgoing[0].ID == edge1ID {
				t.Error("Deleted edge came back after crash! WAL delete replay failed")
			}
			if outgoing[0].ID != edge2ID {
				t.Errorf("Wrong edge survived: expected %d, got %d", edge2ID, outgoing[0].ID)
			}
		}

		t.Logf("Edge deletion correctly recovered from crash via WAL")
	}
}

// TestGraphStorage_DiskBackedEdges_MultipleDeletesWAL tests multiple edge deletions in WAL
func TestGraphStorage_DiskBackedEdges_MultipleDeletesWAL(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID uint64
	var deletedEdges, survivingEdges []uint64

	// Phase 1: Create many edges, delete some, crash
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

		node1ID = node1.ID
		_ = node2.ID  // node2 is needed for edge creation

		// Create 10 edges
		edgeIDs := make([]uint64, 10)
		for i := 0; i < 10; i++ {
			edge, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
			edgeIDs[i] = edge.ID
		}

		// Delete even-numbered edges (0, 2, 4, 6, 8)
		for i := 0; i < 10; i += 2 {
			err := gs.DeleteEdge(edgeIDs[i])
			if err != nil {
				t.Fatalf("DeleteEdge %d failed: %v", i, err)
			}
			deletedEdges = append(deletedEdges, edgeIDs[i])
		}

		// Odd-numbered edges survive (1, 3, 5, 7, 9)
		for i := 1; i < 10; i += 2 {
			survivingEdges = append(survivingEdges, edgeIDs[i])
		}

		// Verify before crash
		outgoing, _ := gs.GetOutgoingEdges(node1.ID)
		if len(outgoing) != 5 {
			t.Fatalf("Expected 5 edges before crash, got %d", len(outgoing))
		}

		// DON'T CLOSE - crash
		t.Log("Simulating crash after multiple deletes")
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

		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed: %v", err)
		}

		// Should have 5 surviving edges
		if len(outgoing) != 5 {
			t.Errorf("Expected 5 edges after recovery, got %d", len(outgoing))
		}

		// Verify none of the deleted edges came back
		for _, edge := range outgoing {
			for _, deletedID := range deletedEdges {
				if edge.ID == deletedID {
					t.Errorf("Deleted edge %d came back after recovery!", deletedID)
				}
			}
		}

		// Verify all surviving edges are present
		foundCount := 0
		for _, edge := range outgoing {
			for _, survivingID := range survivingEdges {
				if edge.ID == survivingID {
					foundCount++
					break
				}
			}
		}

		if foundCount != 5 {
			t.Errorf("Expected to find 5 surviving edges, found %d", foundCount)
		}

		t.Logf("Multiple edge deletions correctly recovered from WAL")
	}
}

// TestGraphStorage_DiskBackedEdges_DeleteAllEdgesWAL tests deleting all edges from a node
func TestGraphStorage_DiskBackedEdges_DeleteAllEdgesWAL(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID uint64

	// Phase 1: Create edges, delete all, crash
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

		node1ID = node1.ID
		node2ID = node2.ID

		// Create 5 edges
		edgeIDs := make([]uint64, 5)
		for i := 0; i < 5; i++ {
			edge, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
			edgeIDs[i] = edge.ID
		}

		// Delete all edges
		for _, edgeID := range edgeIDs {
			err := gs.DeleteEdge(edgeID)
			if err != nil {
				t.Fatalf("DeleteEdge failed: %v", err)
			}
		}

		// Verify all deleted
		outgoing, _ := gs.GetOutgoingEdges(node1.ID)
		if len(outgoing) != 0 {
			t.Errorf("Expected 0 edges after deleting all, got %d", len(outgoing))
		}

		// DON'T CLOSE - crash
		t.Log("Simulating crash after deleting all edges")
	}

	// Phase 2: Recover and verify empty edge list
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

		// Verify no edges recovered
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed: %v", err)
		}

		if len(outgoing) != 0 {
			t.Errorf("Expected 0 edges after recovery, got %d (deleted edges came back!)", len(outgoing))
		}

		incoming, _ := gs.GetIncomingEdges(node2ID)
		if len(incoming) != 0 {
			t.Errorf("Expected 0 incoming edges after recovery, got %d", len(incoming))
		}

		t.Log("All edge deletions correctly recovered - empty edge list maintained")
	}
}
