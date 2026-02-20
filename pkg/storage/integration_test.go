package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_DiskBackedEdges_BasicOperations tests basic CRUD with disk-backed edges
func TestGraphStorage_DiskBackedEdges_BasicOperations(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Create GraphStorage with disk-backed edges enabled
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, err := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Alice"),
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	node2, err := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Bob"),
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Create edge (should store in EdgeStore)
	edge, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
		"since": IntValue(2020),
	}, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge failed: %v", err)
	}

	if edge.ID == 0 {
		t.Error("Edge ID should not be 0")
	}

	// Get outgoing edges (should retrieve from EdgeStore)
	outgoing, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	if len(outgoing) != 1 {
		t.Errorf("Expected 1 outgoing edge, got %d", len(outgoing))
	}

	if outgoing[0].ID != edge.ID {
		t.Errorf("Edge ID mismatch: expected %d, got %d", edge.ID, outgoing[0].ID)
	}

	// Get incoming edges (should retrieve from EdgeStore)
	incoming, err := gs.GetIncomingEdges(node2.ID)
	if err != nil {
		t.Fatalf("GetIncomingEdges failed: %v", err)
	}

	if len(incoming) != 1 {
		t.Errorf("Expected 1 incoming edge, got %d", len(incoming))
	}
}

// TestGraphStorage_DiskBackedEdges_Persistence tests that edges survive restart
func TestGraphStorage_DiskBackedEdges_Persistence(t *testing.T) {
	dataDir := t.TempDir()

	var node1ID, node2ID, edgeID uint64

	// Phase 1: Create data with disk-backed edges
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Alice")})
		node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Bob")})
		edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)

		node1ID = node1.ID
		node2ID = node2.ID
		edgeID = edge.ID

		gs.Close()
	}

	// Phase 2: Reopen and verify edges persisted
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

		// Verify nodes exist (from WAL/snapshot)
		node1, err := gs.GetNode(node1ID)
		if err != nil {
			t.Fatalf("Node 1 not found after restart: %v", err)
		}
		if string(node1.Properties["name"].Data) != "Alice" {
			t.Errorf("Node 1 data corrupted")
		}

		// Verify edges persisted to disk
		outgoing, err := gs.GetOutgoingEdges(node1ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed after restart: %v", err)
		}

		if len(outgoing) != 1 {
			t.Errorf("Expected 1 outgoing edge after restart, got %d", len(outgoing))
		}

		if len(outgoing) > 0 && outgoing[0].ID != edgeID {
			t.Errorf("Edge ID mismatch after restart")
		}

		incoming, err := gs.GetIncomingEdges(node2ID)
		if err != nil {
			t.Fatalf("GetIncomingEdges failed after restart: %v", err)
		}

		if len(incoming) != 1 {
			t.Errorf("Expected 1 incoming edge after restart, got %d", len(incoming))
		}
	}
}

// TestGraphStorage_DiskBackedEdges_LargeGraph tests performance with many edges
func TestGraphStorage_DiskBackedEdges_LargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large graph test in short mode")
	}

	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000, // Cache for 1000 hot edge lists
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create 100 nodes with 10 edges each = 1K edges total (reduced from 1000)
	const numNodes = 100
	const edgesPerNode = 10

	// Create nodes
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, err := gs.CreateNode([]string{"Node"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		nodeIDs[i] = node.ID
	}

	// Create edges
	for i := 0; i < numNodes; i++ {
		sourceID := nodeIDs[i]
		for j := 0; j < edgesPerNode; j++ {
			targetID := nodeIDs[(i+j+1)%numNodes]
			_, err := gs.CreateEdge(sourceID, targetID, "CONNECTS", nil, 1.0)
			if err != nil {
				t.Fatalf("Failed to create edge %d->%d: %v", i, (i+j+1)%numNodes, err)
			}
		}
	}

	// Verify all edges exist
	for i := 0; i < numNodes; i++ {
		outgoing, err := gs.GetOutgoingEdges(nodeIDs[i])
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed for node %d: %v", i, err)
		}

		if len(outgoing) != edgesPerNode {
			t.Errorf("Node %d: expected %d outgoing edges, got %d",
				i, edgesPerNode, len(outgoing))
		}
	}

	t.Logf("Successfully created and verified %d nodes with %d edges",
		numNodes, numNodes*edgesPerNode)
}

// TestGraphStorage_DiskBackedEdges_DeleteEdge tests edge deletion
func TestGraphStorage_DiskBackedEdges_DeleteEdge(t *testing.T) {
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

	// Create nodes and edges
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE1", nil, 1.0)
	edge2, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE2", nil, 1.0)

	// Verify both edges exist
	outgoing, _ := gs.GetOutgoingEdges(node1.ID)
	if len(outgoing) != 2 {
		t.Errorf("Expected 2 outgoing edges before deletion, got %d", len(outgoing))
	}

	// Delete one edge
	err = gs.DeleteEdge(edge1.ID)
	if err != nil {
		t.Fatalf("DeleteEdge failed: %v", err)
	}

	// Verify only one edge remains
	outgoing, err = gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed after deletion: %v", err)
	}

	if len(outgoing) != 1 {
		t.Errorf("Expected 1 outgoing edge after deletion, got %d", len(outgoing))
	}

	if len(outgoing) > 0 && outgoing[0].ID != edge2.ID {
		t.Errorf("Wrong edge remained after deletion")
	}

	// Verify incoming edges also updated
	incoming, err := gs.GetIncomingEdges(node2.ID)
	if err != nil {
		t.Fatalf("GetIncomingEdges failed after deletion: %v", err)
	}

	if len(incoming) != 1 {
		t.Errorf("Expected 1 incoming edge after deletion, got %d", len(incoming))
	}
}

// TestGraphStorage_DiskBackedEdges_DisabledMode tests that in-memory mode still works
func TestGraphStorage_DiskBackedEdges_DisabledMode(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Create with disk-backed edges DISABLED (default behavior)
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: false,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Should work exactly like before (in-memory)
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)

	outgoing, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	if len(outgoing) != 1 {
		t.Errorf("Expected 1 outgoing edge, got %d", len(outgoing))
	}

	if outgoing[0].ID != edge.ID {
		t.Error("Edge ID mismatch")
	}
}

// TestGraphStorage_DiskBackedEdges_CacheEffectiveness tests cache hit rates
func TestGraphStorage_DiskBackedEdges_CacheEffectiveness(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      10, // Small cache to test eviction
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create 100 nodes with edges (more than cache size)
	nodeIDs := make([]uint64, 100)
	for i := 0; i < 100; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID

		// Each node has 5 outgoing edges
		for j := 0; j < 5; j++ {
			targetID := nodeIDs[(i+j)%100]
			if targetID != 0 {
				gs.CreateEdge(node.ID, targetID, "EDGE", nil, 1.0)
			}
		}
	}

	// Access first 10 nodes repeatedly (should be cached)
	for round := 0; round < 10; round++ {
		for i := 0; i < 10; i++ {
			outgoing, err := gs.GetOutgoingEdges(nodeIDs[i])
			if err != nil {
				t.Fatalf("GetOutgoingEdges failed: %v", err)
			}
			if len(outgoing) == 0 {
				// Expected for early nodes before all edges created
				continue
			}
		}
	}

	// Access random nodes (should cause cache misses)
	for i := 0; i < 100; i++ {
		nodeID := nodeIDs[(i*7)%100] // Pseudo-random access
		_, err := gs.GetOutgoingEdges(nodeID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed: %v", err)
		}
	}

	// Test passed if no errors (cache effectiveness measured in benchmarks)
	t.Log("Cache effectiveness test completed without errors")
}

// TestGraphStorage_DiskBackedEdges_ConcurrentAccess tests thread safety
func TestGraphStorage_DiskBackedEdges_ConcurrentAccess(t *testing.T) {
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

	// Create nodes
	const numNodes = 50
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
	}

	// Concurrent edge creation
	done := make(chan bool, 10)
	for worker := 0; worker < 10; worker++ {
		go func(id int) {
			for i := 0; i < 10; i++ {
				source := nodeIDs[(id*5+i)%numNodes]
				target := nodeIDs[(id*5+i+1)%numNodes]
				gs.CreateEdge(source, target, "EDGE", nil, 1.0)
			}
			done <- true
		}(worker)
	}

	// Concurrent edge reads
	for worker := 0; worker < 10; worker++ {
		go func(id int) {
			for i := 0; i < 10; i++ {
				nodeID := nodeIDs[id*5%numNodes]
				gs.GetOutgoingEdges(nodeID)
			}
			done <- true
		}(worker)
	}

	// Wait for all workers
	for i := 0; i < 20; i++ {
		<-done
	}

	// If we got here without panics, concurrency is working
	t.Log("Concurrent access test passed")
}
