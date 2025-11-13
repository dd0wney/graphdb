package storage

import (
	"testing"
)

// TestEdgeStore_StoreAndRetrieve tests basic put/get operations
func TestEdgeStore_StoreAndRetrieve(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 100) // 100 cache entries
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Store outgoing edges for node 1
	nodeID := uint64(1)
	edges := []uint64{10, 20, 30, 40, 50}

	err = es.StoreOutgoingEdges(nodeID, edges)
	if err != nil {
		t.Fatalf("StoreOutgoingEdges failed: %v", err)
	}

	// Retrieve outgoing edges
	retrieved, err := es.GetOutgoingEdges(nodeID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	// Verify
	if len(retrieved) != len(edges) {
		t.Errorf("Got %d edges, want %d", len(retrieved), len(edges))
	}

	for i, edge := range retrieved {
		if edge != edges[i] {
			t.Errorf("Edge[%d] = %d, want %d", i, edge, edges[i])
		}
	}
}

// TestEdgeStore_EmptyNode tests retrieving edges for node with no edges
func TestEdgeStore_EmptyNode(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 100)
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Try to get edges for non-existent node
	edges, err := es.GetOutgoingEdges(999)
	if err != nil {
		t.Fatalf("GetOutgoingEdges should not error on empty node: %v", err)
	}

	if len(edges) != 0 {
		t.Errorf("Expected empty edge list, got %d edges", len(edges))
	}
}

// TestEdgeStore_IncomingEdges tests incoming edge storage
func TestEdgeStore_IncomingEdges(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 100)
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	nodeID := uint64(1)
	edges := []uint64{100, 200, 300}

	// Store incoming edges
	err = es.StoreIncomingEdges(nodeID, edges)
	if err != nil {
		t.Fatalf("StoreIncomingEdges failed: %v", err)
	}

	// Retrieve incoming edges
	retrieved, err := es.GetIncomingEdges(nodeID)
	if err != nil {
		t.Fatalf("GetIncomingEdges failed: %v", err)
	}

	// Verify
	if len(retrieved) != len(edges) {
		t.Errorf("Got %d edges, want %d", len(retrieved), len(edges))
	}
}

// TestEdgeStore_LargeEdgeList tests handling of large edge lists (10K edges)
func TestEdgeStore_LargeEdgeList(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 10) // Small cache to force disk access
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	nodeID := uint64(1)

	// Create 10,000 edges
	const numEdges = 10000
	edges := make([]uint64, numEdges)
	for i := 0; i < numEdges; i++ {
		edges[i] = uint64(i + 1)
	}

	// Store large edge list
	err = es.StoreOutgoingEdges(nodeID, edges)
	if err != nil {
		t.Fatalf("StoreOutgoingEdges with %d edges failed: %v", numEdges, err)
	}

	// Retrieve
	retrieved, err := es.GetOutgoingEdges(nodeID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	// Verify count
	if len(retrieved) != numEdges {
		t.Errorf("Got %d edges, want %d", len(retrieved), numEdges)
	}

	// Verify data integrity (spot check)
	if retrieved[0] != 1 || retrieved[numEdges-1] != numEdges {
		t.Errorf("Edge data corrupted: first=%d, last=%d", retrieved[0], retrieved[numEdges-1])
	}
}

// TestEdgeStore_Persistence tests that data survives restart
func TestEdgeStore_Persistence(t *testing.T) {
	dataDir := t.TempDir()

	// Phase 1: Create and store data
	{
		es, err := NewEdgeStore(dataDir, 100)
		if err != nil {
			t.Fatalf("Failed to create EdgeStore: %v", err)
		}

		nodeID := uint64(1)
		edges := []uint64{10, 20, 30}

		err = es.StoreOutgoingEdges(nodeID, edges)
		if err != nil {
			t.Fatalf("StoreOutgoingEdges failed: %v", err)
		}

		es.Close()
	}

	// Phase 2: Reopen and verify data persisted
	{
		es, err := NewEdgeStore(dataDir, 100)
		if err != nil {
			t.Fatalf("Failed to reopen EdgeStore: %v", err)
		}
		defer es.Close()

		retrieved, err := es.GetOutgoingEdges(1)
		if err != nil {
			t.Fatalf("GetOutgoingEdges failed after restart: %v", err)
		}

		if len(retrieved) != 3 {
			t.Errorf("After restart, got %d edges, want 3", len(retrieved))
		}

		if retrieved[0] != 10 || retrieved[1] != 20 || retrieved[2] != 30 {
			t.Errorf("Data corrupted after restart: %v", retrieved)
		}
	}
}

// TestEdgeStore_UpdateEdges tests updating edge lists
func TestEdgeStore_UpdateEdges(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 100)
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	nodeID := uint64(1)

	// Store initial edges
	edges1 := []uint64{10, 20, 30}
	err = es.StoreOutgoingEdges(nodeID, edges1)
	if err != nil {
		t.Fatalf("Initial store failed: %v", err)
	}

	// Update with different edges
	edges2 := []uint64{100, 200, 300, 400}
	err = es.StoreOutgoingEdges(nodeID, edges2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Retrieve and verify updated edges
	retrieved, err := es.GetOutgoingEdges(nodeID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	if len(retrieved) != 4 {
		t.Errorf("After update, got %d edges, want 4", len(retrieved))
	}

	if retrieved[0] != 100 {
		t.Errorf("First edge = %d, want 100", retrieved[0])
	}
}

// TestEdgeStore_ConcurrentAccess tests thread safety
func TestEdgeStore_ConcurrentAccess(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 100)
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	// Concurrent writes and reads
	const numGoroutines = 10
	const opsPerGoroutine = 100

	done := make(chan bool, numGoroutines)

	// Writers
	for i := 0; i < numGoroutines/2; i++ {
		go func(id int) {
			for j := 0; j < opsPerGoroutine; j++ {
				nodeID := uint64(id*1000 + j)
				edges := []uint64{nodeID * 10, nodeID * 20}
				es.StoreOutgoingEdges(nodeID, edges)
			}
			done <- true
		}(i)
	}

	// Readers
	for i := 0; i < numGoroutines/2; i++ {
		go func(id int) {
			for j := 0; j < opsPerGoroutine; j++ {
				nodeID := uint64(id * 1000)
				es.GetOutgoingEdges(nodeID)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// If we got here without panics, thread safety is working
}

// TestEdgeStore_DeleteEdges tests edge deletion
func TestEdgeStore_DeleteEdges(t *testing.T) {
	dataDir := t.TempDir()
	es, err := NewEdgeStore(dataDir, 100)
	if err != nil {
		t.Fatalf("Failed to create EdgeStore: %v", err)
	}
	defer es.Close()

	nodeID := uint64(1)

	// Store edges
	edges := []uint64{10, 20, 30}
	err = es.StoreOutgoingEdges(nodeID, edges)
	if err != nil {
		t.Fatalf("StoreOutgoingEdges failed: %v", err)
	}

	// Delete edges (store empty list)
	err = es.StoreOutgoingEdges(nodeID, []uint64{})
	if err != nil {
		t.Fatalf("Delete (empty store) failed: %v", err)
	}

	// Verify deleted
	retrieved, err := es.GetOutgoingEdges(nodeID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("After delete, got %d edges, want 0", len(retrieved))
	}
}
