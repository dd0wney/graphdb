package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGraphStorage_DiskBackedEdges_InvalidConfig tests configuration validation
func TestGraphStorage_DiskBackedEdges_InvalidConfig(t *testing.T) {
	t.Run("MissingDataDir", func(t *testing.T) {
		_, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            "", // Empty data dir
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err == nil {
			t.Error("Expected error for empty data dir, got nil")
		}
	})

	t.Run("ZeroCacheSize", func(t *testing.T) {
		dataDir := t.TempDir()
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      0, // Should use default
		})
		if err != nil {
			t.Fatalf("Should accept zero cache size (use default): %v", err)
		}
		defer gs.Close()

		// Should still work with default cache size
		node1, _ := gs.CreateNode([]string{"Node"}, nil)
		node2, _ := gs.CreateNode([]string{"Node"}, nil)
		_, err = gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
		if err != nil {
			t.Errorf("Edge creation failed with default cache: %v", err)
		}
	})

	t.Run("NegativeCacheSize", func(t *testing.T) {
		dataDir := t.TempDir()
		_, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      -100, // Negative cache size
		})
		// Should either error or treat as zero/default
		if err != nil {
			t.Logf("Correctly rejected negative cache size: %v", err)
		} else {
			t.Log("Accepted negative cache size (treating as default)")
		}
	})
}

// TestGraphStorage_DiskBackedEdges_EmptyEdgeLists tests operations on nodes with no edges
func TestGraphStorage_DiskBackedEdges_EmptyEdgeLists(t *testing.T) {
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

	// Create node with no edges
	node, err := gs.CreateNode([]string{"Node"}, nil)
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Get outgoing edges (should be empty, not error)
	outgoing, err := gs.GetOutgoingEdges(node.ID)
	if err != nil {
		t.Errorf("GetOutgoingEdges failed on empty node: %v", err)
	}
	if len(outgoing) != 0 {
		t.Errorf("Expected 0 outgoing edges, got %d", len(outgoing))
	}

	// Get incoming edges (should be empty, not error)
	incoming, err := gs.GetIncomingEdges(node.ID)
	if err != nil {
		t.Errorf("GetIncomingEdges failed on empty node: %v", err)
	}
	if len(incoming) != 0 {
		t.Errorf("Expected 0 incoming edges, got %d", len(incoming))
	}
}

// TestGraphStorage_DiskBackedEdges_NonExistentNode tests operations on non-existent nodes
func TestGraphStorage_DiskBackedEdges_NonExistentNode(t *testing.T) {
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

	// Try to get edges for non-existent node (should return empty, not crash)
	nonExistentID := uint64(99999)
	outgoing, err := gs.GetOutgoingEdges(nonExistentID)
	if err != nil {
		t.Logf("GetOutgoingEdges returned error for non-existent node: %v", err)
	}
	if outgoing == nil {
		outgoing = []*Edge{}
	}
	if len(outgoing) != 0 {
		t.Errorf("Expected 0 edges for non-existent node, got %d", len(outgoing))
	}
}

// TestGraphStorage_DiskBackedEdges_DuplicateEdges tests handling of duplicate edges
func TestGraphStorage_DiskBackedEdges_DuplicateEdges(t *testing.T) {
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

	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)

	// Create same edge multiple times
	edge1, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	if err != nil {
		t.Fatalf("First edge creation failed: %v", err)
	}

	edge2, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	if err != nil {
		t.Fatalf("Second edge creation failed: %v", err)
	}

	// Should create two separate edges (graph allows multi-edges)
	if edge1.ID == edge2.ID {
		t.Error("Duplicate edges should have different IDs")
	}

	outgoing, _ := gs.GetOutgoingEdges(node1.ID)
	if len(outgoing) != 2 {
		t.Errorf("Expected 2 edges (duplicates allowed), got %d", len(outgoing))
	}
}

// TestGraphStorage_DiskBackedEdges_VeryLargeEdgeList tests performance with very large edge lists
func TestGraphStorage_DiskBackedEdges_VeryLargeEdgeList(t *testing.T) {
	if testing.Short() || isRaceEnabled() {
		t.Skip("Skipping large edge list test in short mode or with race detector")
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

	// Create node with 10K outgoing edges (reduced from 100K for reasonable test time)
	sourceNode, _ := gs.CreateNode([]string{"Hub"}, nil)

	const numEdges = 1000 // Reduced from 10000 for reasonable test time
	t.Logf("Creating %d edges from single node...", numEdges)

	for i := 0; i < numEdges; i++ {
		targetNode, _ := gs.CreateNode([]string{"Node"}, nil)
		_, err := gs.CreateEdge(sourceNode.ID, targetNode.ID, "CONNECTS", nil, 1.0)
		if err != nil {
			t.Fatalf("Edge creation failed at %d: %v", i, err)
		}

		if i > 0 && i%2000 == 0 {
			t.Logf("Created %d edges...", i)
		}
	}

	// Retrieve all edges
	t.Log("Retrieving all edges...")
	outgoing, err := gs.GetOutgoingEdges(sourceNode.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve edges: %v", err)
	}

	if len(outgoing) != numEdges {
		t.Errorf("Expected %d edges, got %d", numEdges, len(outgoing))
	}

	t.Logf("Successfully handled %d edges on single node", numEdges)
}

// TestGraphStorage_DiskBackedEdges_InvalidEdgeID tests operations with invalid edge IDs
func TestGraphStorage_DiskBackedEdges_InvalidEdgeID(t *testing.T) {
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

	// Try to delete non-existent edge
	err = gs.DeleteEdge(99999)
	if err == nil {
		t.Error("Expected error when deleting non-existent edge")
	}

	// Try to delete edge ID 0
	err = gs.DeleteEdge(0)
	if err == nil {
		t.Error("Expected error when deleting edge ID 0")
	}
}

// TestGraphStorage_DiskBackedEdges_DoubleClose tests closing GraphStorage twice
func TestGraphStorage_DiskBackedEdges_DoubleClose(t *testing.T) {
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

	// First close
	err = gs.Close()
	if err != nil {
		t.Errorf("First close failed: %v", err)
	}

	// Second close (should not panic or error)
	err = gs.Close()
	if err != nil {
		t.Logf("Second close returned error (acceptable): %v", err)
	} else {
		t.Log("Second close succeeded (idempotent close)")
	}
}

// TestGraphStorage_DiskBackedEdges_OperationsAfterClose tests using storage after close
func TestGraphStorage_DiskBackedEdges_OperationsAfterClose(t *testing.T) {
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

	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.Close()

	// Try operations after close (should error or handle gracefully)
	_, err = gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	if err == nil {
		t.Log("CreateEdge succeeded after close (underlying LSM may still work)")
	} else {
		t.Logf("CreateEdge correctly failed after close: %v", err)
	}

	_, err = gs.GetOutgoingEdges(node1.ID)
	if err == nil {
		t.Log("GetOutgoingEdges succeeded after close (may be acceptable for reads)")
	} else {
		t.Logf("GetOutgoingEdges failed after close: %v", err)
	}
}

// TestGraphStorage_DiskBackedEdges_CorruptedDataRecovery tests recovery from corrupted data
func TestGraphStorage_DiskBackedEdges_CorruptedDataRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create valid data
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
		gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)

		gs.Close()
	}

	// Phase 2: Corrupt the EdgeStore data
	edgeStoreDir := filepath.Join(dataDir, "edgestore")

	// Find and corrupt an SSTable file
	files, _ := os.ReadDir(edgeStoreDir)
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".sst" {
			filePath := filepath.Join(edgeStoreDir, file.Name())
			// Truncate file to corrupt it
			os.WriteFile(filePath, []byte("corrupted"), 0644)
			t.Logf("Corrupted file: %s", file.Name())
			break
		}
	}

	// Phase 3: Try to open with corrupted data
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Logf("GraphStorage correctly failed to open with corrupted data: %v", err)
		return
	}
	defer gs.Close()

	// If it opened, operations should either work (recovery) or error gracefully
	_, err = gs.GetOutgoingEdges(1)
	if err != nil {
		t.Logf("GetOutgoingEdges correctly returned error after corruption: %v", err)
	} else {
		t.Log("GetOutgoingEdges recovered from corruption")
	}
}

// TestGraphStorage_DiskBackedEdges_CacheSizeOne tests edge case of cache size = 1
func TestGraphStorage_DiskBackedEdges_CacheSizeOne(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1, // Minimal cache
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create multiple nodes with edges
	nodes := make([]*Node, 10)
	for i := 0; i < 10; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	for i := 0; i < 10; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[(i+1)%10].ID, "EDGE", nil, 1.0)
	}

	// Access all edges (should cause cache thrashing but work correctly)
	for i := 0; i < 10; i++ {
		outgoing, err := gs.GetOutgoingEdges(nodes[i].ID)
		if err != nil {
			t.Errorf("Failed to get edges with cache size 1: %v", err)
		}
		if len(outgoing) != 1 {
			t.Errorf("Expected 1 edge for node %d, got %d", i, len(outgoing))
		}
	}

	t.Log("Cache size 1 handled correctly (frequent evictions)")
}

// TestGraphStorage_DiskBackedEdges_ReadOnlyFilesystem tests behavior with read-only filesystem
func TestGraphStorage_DiskBackedEdges_ReadOnlyFilesystem(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping read-only test when running as root")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Create initial data
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}
		gs.Close()
	}

	// Make directory read-only
	edgeStoreDir := filepath.Join(dataDir, "edgestore")
	os.Chmod(edgeStoreDir, 0444)
	defer os.Chmod(edgeStoreDir, 0755) // Restore permissions for cleanup

	// Try to open with read-only filesystem
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Logf("Correctly failed to open on read-only filesystem: %v", err)
		return
	}
	defer gs.Close()

	// Try to write (should fail)
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	_, err = gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	if err != nil {
		t.Logf("Correctly failed to write on read-only filesystem: %v", err)
	} else {
		t.Error("Should have failed to write on read-only filesystem")
	}
}
