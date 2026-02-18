package storage

import (
	"os"
	"testing"
	"time"
)

// TestGraphStorage_BatchedWAL_NodesDurable tests that batched node creations survive crashes
func TestGraphStorage_BatchedWAL_NodesDurable(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeIDs []uint64

	// Phase 1: Create nodes with batching enabled, flush, then crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})

		// Create nodes
		for i := 0; i < 5; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"name": StringValue("User" + string(rune('A'+i))),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Wait a moment to ensure background flusher completes
		time.Sleep(100 * time.Millisecond)

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Simulating crash after batched node creation")
	}

	// Phase 2: Recover and verify all nodes exist
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify all nodes recovered
		stats := gs.stats
		if stats.NodeCount != 5 {
			t.Errorf("Expected 5 nodes after recovery, got %d", stats.NodeCount)
		}

		// Verify each node by ID
		for i, nodeID := range nodeIDs {
			node, err := gs.GetNode(nodeID)
			if err != nil {
				t.Errorf("Node %d not recovered: %v", nodeID, err)
				continue
			}

			expectedName := "User" + string(rune('A'+i))
			if name, exists := node.Properties["name"]; !exists {
				t.Errorf("Node %d missing name property", nodeID)
			} else if nameStr, err := name.AsString(); err != nil || nameStr != expectedName {
				t.Errorf("Node %d has wrong name: got %v, want %s", nodeID, nameStr, expectedName)
			}
		}

		t.Log("Batched nodes correctly recovered from WAL")
	}
}

// TestGraphStorage_BatchedWAL_EdgesDurable tests that batched edge creations survive crashes
func TestGraphStorage_BatchedWAL_EdgesDurable(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var edgeIDs []uint64
	var node1ID, node2ID uint64

	// Phase 1: Create nodes and edges with batching, flush, then crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})

		// Create nodes
		node1, _ := gs.CreateNode([]string{"Person"}, nil)
		node2, _ := gs.CreateNode([]string{"Person"}, nil)
		node1ID = node1.ID
		node2ID = node2.ID

		// Create edges
		for i := 0; i < 5; i++ {
			edge, err := gs.CreateEdge(node1ID, node2ID, "KNOWS", map[string]Value{
				"since": IntValue(int64(2020 + i)),
			}, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge failed: %v", err)
			}
			edgeIDs = append(edgeIDs, edge.ID)
		}

		// Wait a moment to ensure background flusher completes
		time.Sleep(100 * time.Millisecond)

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Simulating crash after batched edge creation")
	}

	// Phase 2: Recover and verify all edges exist
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify all edges recovered
		stats := gs.stats
		if stats.EdgeCount != 5 {
			t.Errorf("Expected 5 edges after recovery, got %d", stats.EdgeCount)
		}

		// Verify each edge by ID
		for i, edgeID := range edgeIDs {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				t.Errorf("Edge %d not recovered: %v", edgeID, err)
				continue
			}

			expectedSince := int64(2020 + i)
			if since, exists := edge.Properties["since"]; !exists {
				t.Errorf("Edge %d missing since property", edgeID)
			} else if sinceVal, err := since.AsInt(); err != nil || sinceVal != expectedSince {
				t.Errorf("Edge %d has wrong since: got %v, want %d", edgeID, sinceVal, expectedSince)
			}
		}

		// Verify adjacency lists
		outgoing, _ := gs.GetOutgoingEdges(node1ID)
		if len(outgoing) != 5 {
			t.Errorf("Expected 5 outgoing edges, got %d", len(outgoing))
		}

		incoming, _ := gs.GetIncomingEdges(node2ID)
		if len(incoming) != 5 {
			t.Errorf("Expected 5 incoming edges, got %d", len(incoming))
		}

		t.Log("Batched edges correctly recovered from WAL")
	}
}

// TestGraphStorage_BatchedWAL_MultipleCycles tests multiple crash/recovery cycles
func TestGraphStorage_BatchedWAL_MultipleCycles(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Track all crashed storages for cleanup
	var crashedStorages []*GraphStorage
	t.Cleanup(func() {
		for _, gs := range crashedStorages {
			gs.Close()
		}
	})

	// Cycle 1: Create 3 nodes, flush, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}
		crashedStorages = append(crashedStorages, gs)

		gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(1)})
		gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(1)})
		gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(1)})

		// Wait for background flush
		time.Sleep(100 * time.Millisecond)

		t.Log("Cycle 1: Created 3 nodes, flushed, crashing...")
		// DON'T CLOSE - simulate crash (cleanup handles it)
	}

	// Cycle 2: Recover, create 2 more nodes, flush, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to recover from cycle 1: %v", err)
		}
		crashedStorages = append(crashedStorages, gs)

		stats := gs.stats
		if stats.NodeCount != 3 {
			t.Fatalf("Cycle 2: Expected 3 nodes from cycle 1, got %d", stats.NodeCount)
		}

		gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(2)})
		gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(2)})

		// Wait for background flush
		time.Sleep(100 * time.Millisecond)

		t.Log("Cycle 2: Recovered 3 nodes, created 2 more, flushed, crashing...")
		// DON'T CLOSE - simulate crash (cleanup handles it)
	}

	// Cycle 3: Recover, create 1 more node, flush, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to recover from cycle 2: %v", err)
		}
		crashedStorages = append(crashedStorages, gs)

		stats := gs.stats
		if stats.NodeCount != 5 {
			t.Fatalf("Cycle 3: Expected 5 nodes from cycles 1+2, got %d", stats.NodeCount)
		}

		gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(3)})

		// Wait for background flush
		time.Sleep(100 * time.Millisecond)

		t.Log("Cycle 3: Recovered 5 nodes, created 1 more, flushed, crashing...")
		// DON'T CLOSE - simulate crash (cleanup handles it)
	}

	// Final recovery: Verify all 6 nodes exist
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed final recovery: %v", err)
		}
		defer gs.Close()

		stats := gs.stats
		if stats.NodeCount != 6 {
			t.Errorf("Final recovery: Expected 6 nodes total, got %d", stats.NodeCount)
		}

		// Verify nodes from each cycle exist
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 6 {
			t.Errorf("Expected 6 Person nodes, got %d", len(persons))
		}

		// Count nodes by cycle
		cycleCounts := map[int64]int{1: 0, 2: 0, 3: 0}
		for _, node := range persons {
			if cycle, exists := node.Properties["cycle"]; exists {
				if cycleVal, err := cycle.AsInt(); err == nil {
					cycleCounts[cycleVal]++
				}
			}
		}

		if cycleCounts[1] != 3 {
			t.Errorf("Expected 3 nodes from cycle 1, got %d", cycleCounts[1])
		}
		if cycleCounts[2] != 2 {
			t.Errorf("Expected 2 nodes from cycle 2, got %d", cycleCounts[2])
		}
		if cycleCounts[3] != 1 {
			t.Errorf("Expected 1 node from cycle 3, got %d", cycleCounts[3])
		}

		t.Log("Multiple crash/recovery cycles succeeded with batched WAL")
	}
}

// TestGraphStorage_BatchedWAL_DeletionDurable tests that batched deletions survive crashes
func TestGraphStorage_BatchedWAL_DeletionDurable(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeToDelete uint64

	// Phase 1: Create 5 nodes, flush, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			if i == 2 {
				nodeToDelete = node.ID
			}
		}

		// Wait for background flush
		time.Sleep(100 * time.Millisecond)

		gs.Close() // Clean close
	}

	// Phase 2: Recover, delete one node, flush, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})

		stats := gs.stats
		if stats.NodeCount != 5 {
			t.Fatalf("Expected 5 nodes after recovery, got %d", stats.NodeCount)
		}

		// Delete node
		err := gs.DeleteNode(nodeToDelete)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Wait for background flush
		time.Sleep(100 * time.Millisecond)

		t.Log("Deleted node, simulating crash...")
		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
	}

	// Phase 3: Recover and verify deletion persisted
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
			EnableBatching:     true,
			BatchSize:          10,
			FlushInterval:      1 * time.Second,
		})
		if err != nil {
			t.Fatalf("Failed final recovery: %v", err)
		}
		defer gs.Close()

		stats := gs.stats
		if stats.NodeCount != 4 {
			t.Errorf("Expected 4 nodes after deletion recovery, got %d", stats.NodeCount)
		}

		// Verify deleted node doesn't exist
		_, err = gs.GetNode(nodeToDelete)
		if err == nil {
			t.Error("Deleted node still exists after crash recovery!")
		}

		t.Log("Batched deletion correctly persisted through crash")
	}
}
