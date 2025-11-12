package storage

import (
	"fmt"
	"sync"
	"testing"
)

// Helper function to create test storage
func newTestStorage(t *testing.T) *GraphStorage {
	storage, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	return storage
}

// TestBatchBasicOperations tests basic batch operations
func TestBatchBasicOperations(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	batch := storage.BeginBatch()

	// Add nodes
	props := map[string]Value{
		"name": StringValue("Alice"),
		"age":  IntValue(30),
	}

	nodeID1, err := batch.AddNode([]string{"Person"}, props)
	if err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	nodeID2, err := batch.AddNode([]string{"Person"}, props)
	if err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	// Add edge
	edgeProps := map[string]Value{
		"since": IntValue(2020),
	}

	edgeID, err := batch.AddEdge(nodeID1, nodeID2, "KNOWS", edgeProps, 1.0)
	if err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}

	// Verify batch size
	if batch.Size() != 3 {
		t.Errorf("Expected batch size 3, got %d", batch.Size())
	}

	// Commit batch
	if err := batch.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify nodes exist
	node1, err := storage.GetNode(nodeID1)
	if err != nil {
		t.Errorf("Node1 not found: %v", err)
	}
	if node1 == nil {
		t.Error("Node1 is nil")
	}

	// Verify edge exists
	edge, err := storage.GetEdge(edgeID)
	if err != nil {
		t.Errorf("Edge not found: %v", err)
	}
	if edge == nil {
		t.Error("Edge is nil")
	}

	// Verify statistics
	stats := storage.GetStatistics()
	if stats.NodeCount != 2 {
		t.Errorf("Expected NodeCount 2, got %d", stats.NodeCount)
	}
	if stats.EdgeCount != 1 {
		t.Errorf("Expected EdgeCount 1, got %d", stats.EdgeCount)
	}
}

// TestBatchIDAllocationRace tests concurrent ID allocation doesn't produce duplicates
// This validates the atomic ID allocation fix
func TestBatchIDAllocationRace(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	numBatches := 10
	nodesPerBatch := 100

	var wg sync.WaitGroup
	nodeIDs := make(chan uint64, numBatches*nodesPerBatch)

	// Create multiple batches concurrently allocating node IDs
	for i := 0; i < numBatches; i++ {
		wg.Add(1)
		go func(batchNum int) {
			defer wg.Done()

			batch := storage.BeginBatch()
			props := map[string]Value{
				"batch": IntValue(int64(batchNum)),
			}

			for j := 0; j < nodesPerBatch; j++ {
				nodeID, err := batch.AddNode([]string{"Test"}, props)
				if err != nil {
					t.Errorf("Batch %d: AddNode failed: %v", batchNum, err)
					return
				}
				nodeIDs <- nodeID
			}

			if err := batch.Commit(); err != nil {
				t.Errorf("Batch %d: Commit failed: %v", batchNum, err)
			}
		}(i)
	}

	wg.Wait()
	close(nodeIDs)

	// Verify all IDs are unique
	seen := make(map[uint64]bool)
	duplicates := 0

	for id := range nodeIDs {
		if seen[id] {
			duplicates++
			t.Errorf("Duplicate node ID detected: %d", id)
		}
		seen[id] = true
	}

	if duplicates > 0 {
		t.Errorf("Found %d duplicate IDs - BUG: ID allocation not thread-safe", duplicates)
	}

	totalExpected := numBatches * nodesPerBatch
	if len(seen) != totalExpected {
		t.Errorf("Expected %d unique IDs, got %d", totalExpected, len(seen))
	}
}

// TestBatchEdgeIDAllocationRace tests concurrent edge ID allocation
func TestBatchEdgeIDAllocationRace(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	// Create some nodes first
	node1, _ := storage.CreateNode([]string{"Node"}, map[string]Value{})
	node2, _ := storage.CreateNode([]string{"Node"}, map[string]Value{})

	numBatches := 10
	edgesPerBatch := 50

	var wg sync.WaitGroup
	edgeIDs := make(chan uint64, numBatches*edgesPerBatch)

	// Create multiple batches concurrently allocating edge IDs
	for i := 0; i < numBatches; i++ {
		wg.Add(1)
		go func(batchNum int) {
			defer wg.Done()

			batch := storage.BeginBatch()
			props := map[string]Value{
				"batch": IntValue(int64(batchNum)),
			}

			for j := 0; j < edgesPerBatch; j++ {
				edgeID, err := batch.AddEdge(node1.ID, node2.ID, "TEST", props, 1.0)
				if err != nil {
					t.Errorf("Batch %d: AddEdge failed: %v", batchNum, err)
					return
				}
				edgeIDs <- edgeID
			}

			if err := batch.Commit(); err != nil {
				t.Errorf("Batch %d: Commit failed: %v", batchNum, err)
			}
		}(i)
	}

	wg.Wait()
	close(edgeIDs)

	// Verify all IDs are unique
	seen := make(map[uint64]bool)
	duplicates := 0

	for id := range edgeIDs {
		if seen[id] {
			duplicates++
			t.Errorf("Duplicate edge ID detected: %d", id)
		}
		seen[id] = true
	}

	if duplicates > 0 {
		t.Errorf("Found %d duplicate edge IDs - BUG: Edge ID allocation not thread-safe", duplicates)
	}
}

// TestBatchStatisticsRace tests that statistics are updated atomically
// This validates the atomic statistics fix
func TestBatchStatisticsRace(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	numBatches := 20
	nodesPerBatch := 50

	var wg sync.WaitGroup

	// Create nodes concurrently and verify statistics
	for i := 0; i < numBatches; i++ {
		wg.Add(1)
		go func(batchNum int) {
			defer wg.Done()

			batch := storage.BeginBatch()
			props := map[string]Value{
				"id": IntValue(int64(batchNum)),
			}

			for j := 0; j < nodesPerBatch; j++ {
				batch.AddNode([]string{"Test"}, props)
			}

			if err := batch.Commit(); err != nil {
				t.Errorf("Batch %d: Commit failed: %v", batchNum, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify final statistics
	stats := storage.GetStatistics()
	expectedNodes := uint64(numBatches * nodesPerBatch)

	if stats.NodeCount != expectedNodes {
		t.Errorf("Expected NodeCount %d, got %d - BUG: statistics not atomic", expectedNodes, stats.NodeCount)
	}
}

// TestBatchUpdateOperations tests batch update operations
func TestBatchUpdateOperations(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	// Create a node
	props := map[string]Value{
		"name": StringValue("Alice"),
		"age":  IntValue(30),
	}

	node, _ := storage.CreateNode([]string{"Person"}, props)

	// Update via batch
	batch := storage.BeginBatch()
	newProps := map[string]Value{
		"name": StringValue("Alice Updated"),
		"age":  IntValue(31),
	}

	batch.UpdateNode(node.ID, newProps)

	if err := batch.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify update
	updatedNode, _ := storage.GetNode(node.ID)
	if string(updatedNode.Properties["name"].Data) != "Alice Updated" {
		t.Error("Node update failed")
	}
}

// TestBatchDeleteOperations tests batch delete operations
func TestBatchDeleteOperations(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	// Create nodes and edge
	node1, _ := storage.CreateNode([]string{"Node"}, map[string]Value{})
	node2, _ := storage.CreateNode([]string{"Node"}, map[string]Value{})
	edge, _ := storage.CreateEdge(node1.ID, node2.ID, "LINKS", map[string]Value{}, 1.0)

	initialStats := storage.GetStatistics()

	// Delete via batch
	batch := storage.BeginBatch()
	batch.DeleteEdge(edge.ID)
	batch.DeleteNode(node1.ID)

	if err := batch.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify deletions
	_, err := storage.GetNode(node1.ID)
	if err == nil {
		t.Error("Node should be deleted")
	}

	_, err = storage.GetEdge(edge.ID)
	if err == nil {
		t.Error("Edge should be deleted")
	}

	// Verify statistics decreased
	stats := storage.GetStatistics()
	if stats.NodeCount >= initialStats.NodeCount {
		t.Error("NodeCount should decrease after deletion")
	}
	if stats.EdgeCount >= initialStats.EdgeCount {
		t.Error("EdgeCount should decrease after deletion")
	}
}

// TestBatchClear tests clearing a batch
func TestBatchClear(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	batch := storage.BeginBatch()

	// Add operations
	props := map[string]Value{"test": StringValue("value")}
	batch.AddNode([]string{"Test"}, props)
	batch.AddNode([]string{"Test"}, props)

	if batch.Size() != 2 {
		t.Errorf("Expected size 2, got %d", batch.Size())
	}

	// Clear batch
	batch.Clear()

	if batch.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", batch.Size())
	}

	// Commit empty batch should succeed
	if err := batch.Commit(); err != nil {
		t.Errorf("Empty batch commit failed: %v", err)
	}

	// No nodes should be created
	stats := storage.GetStatistics()
	if stats.NodeCount != 0 {
		t.Errorf("Expected NodeCount 0, got %d", stats.NodeCount)
	}
}

// TestBatchConcurrentCommits tests multiple batches committing concurrently
func TestBatchConcurrentCommits(t *testing.T) {
	storage := newTestStorage(t)
	defer storage.Close()

	numBatches := 10
	var wg sync.WaitGroup

	for i := 0; i < numBatches; i++ {
		wg.Add(1)
		go func(batchNum int) {
			defer wg.Done()

			batch := storage.BeginBatch()
			props := map[string]Value{
				"batch": IntValue(int64(batchNum)),
			}

			// Add multiple nodes
			for j := 0; j < 10; j++ {
				batch.AddNode([]string{fmt.Sprintf("Batch%d", batchNum)}, props)
			}

			if err := batch.Commit(); err != nil {
				t.Errorf("Batch %d commit failed: %v", batchNum, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all nodes created
	stats := storage.GetStatistics()
	expectedNodes := uint64(numBatches * 10)
	if stats.NodeCount != expectedNodes {
		t.Errorf("Expected %d nodes, got %d", expectedNodes, stats.NodeCount)
	}
}
