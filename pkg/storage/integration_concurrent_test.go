package storage

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

// TestGraphStorage_ConcurrentNodeCreation tests concurrent node creation with disk-backed edges
func TestGraphStorage_ConcurrentNodeCreation(t *testing.T) {
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

	// Create 100 nodes concurrently from 10 goroutines
	numGoroutines := 10
	nodesPerGoroutine := 10
	expectedTotal := numGoroutines * nodesPerGoroutine

	var wg sync.WaitGroup
	var createdCount atomic.Uint64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < nodesPerGoroutine; j++ {
				_, err := gs.CreateNode([]string{"Person"}, map[string]Value{
					"worker": IntValue(int64(workerID)),
					"index":  IntValue(int64(j)),
				})
				if err != nil {
					t.Errorf("Worker %d: CreateNode failed: %v", workerID, err)
				} else {
					createdCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all nodes were created
	if createdCount.Load() != uint64(expectedTotal) {
		t.Errorf("Expected %d nodes created, got %d", expectedTotal, createdCount.Load())
	}

	stats := gs.stats
	if stats.NodeCount != uint64(expectedTotal) {
		t.Errorf("Expected node count %d, got %d", expectedTotal, stats.NodeCount)
	}

	// Verify all nodes are retrievable
	persons, _ := gs.FindNodesByLabel("Person")
	if len(persons) != expectedTotal {
		t.Errorf("Expected %d Person nodes, got %d", expectedTotal, len(persons))
	}

	t.Logf("Successfully created %d nodes concurrently", expectedTotal)
}

// TestGraphStorage_ConcurrentEdgeCreation tests concurrent edge creation to same nodes
func TestGraphStorage_ConcurrentEdgeCreation(t *testing.T) {
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

	// Create two nodes
	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)

	// Create 100 edges concurrently from 10 goroutines
	numGoroutines := 10
	edgesPerGoroutine := 10
	expectedTotal := numGoroutines * edgesPerGoroutine

	var wg sync.WaitGroup
	var createdCount atomic.Uint64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < edgesPerGoroutine; j++ {
				_, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
					"worker": IntValue(int64(workerID)),
					"index":  IntValue(int64(j)),
				}, 1.0)
				if err != nil {
					t.Errorf("Worker %d: CreateEdge failed: %v", workerID, err)
				} else {
					createdCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all edges were created
	if createdCount.Load() != uint64(expectedTotal) {
		t.Errorf("Expected %d edges created, got %d", expectedTotal, createdCount.Load())
	}

	stats := gs.stats
	if stats.EdgeCount != uint64(expectedTotal) {
		t.Errorf("Expected edge count %d, got %d", expectedTotal, stats.EdgeCount)
	}

	// Verify adjacency lists have correct count
	outgoing, _ := gs.GetOutgoingEdges(node1.ID)
	if len(outgoing) != expectedTotal {
		t.Errorf("Expected %d outgoing edges, got %d", expectedTotal, len(outgoing))
	}

	incoming, _ := gs.GetIncomingEdges(node2.ID)
	if len(incoming) != expectedTotal {
		t.Errorf("Expected %d incoming edges, got %d", expectedTotal, len(incoming))
	}

	t.Logf("Successfully created %d edges concurrently to same node pair", expectedTotal)
}

// TestGraphStorage_ConcurrentReadWrite tests concurrent reads and writes
func TestGraphStorage_ConcurrentReadWrite(t *testing.T) {
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
		node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodeIDs = append(nodeIDs, node.ID)
	}

	// Run concurrent readers and writers
	var wg sync.WaitGroup
	numReaders := 5
	numWriters := 5
	duration := 100 // iterations

	// Readers
	var readCount atomic.Uint64
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for j := 0; j < duration; j++ {
				// Read random nodes
				for _, nodeID := range nodeIDs {
					_, err := gs.GetNode(nodeID)
					if err != nil {
						t.Errorf("Reader %d: GetNode failed: %v", readerID, err)
					}
					readCount.Add(1)
				}

				// Query by label
				_, err := gs.FindNodesByLabel("Person")
				if err != nil {
					t.Errorf("Reader %d: FindNodesByLabel failed: %v", readerID, err)
				}
			}
		}(i)
	}

	// Writers
	var writeCount atomic.Uint64
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			for j := 0; j < duration; j++ {
				// Create new nodes
				_, err := gs.CreateNode([]string{"Person"}, map[string]Value{
					"writer": IntValue(int64(writerID)),
					"iter":   IntValue(int64(j)),
				})
				if err != nil {
					t.Errorf("Writer %d: CreateNode failed: %v", writerID, err)
				} else {
					writeCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Completed %d reads and %d writes concurrently", readCount.Load(), writeCount.Load())

	// Verify final state
	expectedNodes := 10 + (numWriters * duration)
	stats := gs.stats
	if stats.NodeCount != uint64(expectedNodes) {
		t.Errorf("Expected %d total nodes, got %d", expectedNodes, stats.NodeCount)
	}
}

// TestGraphStorage_ConcurrentDeletion tests concurrent node and edge deletion
func TestGraphStorage_ConcurrentDeletion(t *testing.T) {
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
	numNodes := 100
	var nodeIDs []uint64
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		nodeIDs = append(nodeIDs, node.ID)
	}

	// Create edges between consecutive nodes
	for i := 0; i < numNodes-1; i++ {
		gs.CreateEdge(nodeIDs[i], nodeIDs[i+1], "KNOWS", nil, 1.0)
	}

	// Delete half the nodes concurrently
	numGoroutines := 10
	var wg sync.WaitGroup
	var deleteCount atomic.Uint64
	var errorCount atomic.Uint64

	// Each goroutine deletes a subset of nodes
	nodesPerGoroutine := numNodes / (2 * numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			start := workerID * nodesPerGoroutine
			end := start + nodesPerGoroutine

			for j := start; j < end && j < len(nodeIDs); j++ {
				err := gs.DeleteNode(nodeIDs[j])
				if err != nil {
					// Errors are expected if cascade deletion deleted the node
					errorCount.Add(1)
				} else {
					deleteCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Deleted %d nodes concurrently (%d errors from cascade deletion)",
		deleteCount.Load(), errorCount.Load())

	// Verify remaining nodes
	persons, _ := gs.FindNodesByLabel("Person")
	remaining := len(persons)

	// We should have deleted roughly half
	if remaining > numNodes*3/4 {
		t.Errorf("Expected roughly half nodes remaining, got %d out of %d", remaining, numNodes)
	}

	t.Logf("Remaining nodes: %d (started with %d)", remaining, numNodes)
}

// TestGraphStorage_ConcurrentPropertyIndex tests concurrent property index operations
func TestGraphStorage_ConcurrentPropertyIndex(t *testing.T) {
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

	// Create property index
	err = gs.CreatePropertyIndex("score", TypeInt)
	if err != nil {
		t.Fatalf("CreatePropertyIndex failed: %v", err)
	}

	// Concurrently create nodes with indexed property
	numGoroutines := 10
	nodesPerGoroutine := 10
	expectedTotal := numGoroutines * nodesPerGoroutine

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < nodesPerGoroutine; j++ {
				score := (workerID * nodesPerGoroutine) + j
				_, err := gs.CreateNode([]string{"Player"}, map[string]Value{
					"score": IntValue(int64(score)),
					"name":  StringValue(fmt.Sprintf("Player-%d-%d", workerID, j)),
				})
				if err != nil {
					t.Errorf("Worker %d: CreateNode failed: %v", workerID, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify index queries work correctly
	// Query for specific score
	nodes, err := gs.FindNodesByPropertyIndexed("score", IntValue(50))
	if err != nil {
		t.Fatalf("FindNodesByPropertyIndexed failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node with score=50, got %d", len(nodes))
	}

	// Verify all nodes are indexed
	players, _ := gs.FindNodesByLabel("Player")
	if len(players) != expectedTotal {
		t.Errorf("Expected %d Player nodes, got %d", expectedTotal, len(players))
	}

	t.Logf("Successfully created %d indexed nodes concurrently", expectedTotal)
}

// TestGraphStorage_ConcurrentCrashRecovery tests concurrent operations then crash recovery
func TestGraphStorage_ConcurrentCrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Concurrent writes, then crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes concurrently
		var wg sync.WaitGroup
		numGoroutines := 5
		nodesPerGoroutine := 20

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for j := 0; j < nodesPerGoroutine; j++ {
					gs.CreateNode([]string{"Person"}, map[string]Value{
						"worker": IntValue(int64(workerID)),
						"index":  IntValue(int64(j)),
					})
				}
			}(i)
		}

		wg.Wait()

		// DON'T CLOSE - simulate crash
		t.Log("Created 100 nodes concurrently, simulating crash...")
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

		// Verify all nodes recovered
		expectedTotal := 5 * 20
		stats := gs.stats
		if stats.NodeCount != uint64(expectedTotal) {
			t.Errorf("Expected %d nodes after recovery, got %d", expectedTotal, stats.NodeCount)
		}

		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != expectedTotal {
			t.Errorf("Expected %d Person nodes after recovery, got %d", expectedTotal, len(persons))
		}

		t.Logf("Successfully recovered %d concurrently-created nodes", expectedTotal)
	}
}
