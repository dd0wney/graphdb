package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_SnapshotAndTruncate tests that Clean close creates snapshot, truncates WAL
func TestGraphStorage_SnapshotAndTruncate(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeIDs []uint64

	// Phase 1: Create nodes, close cleanly (snapshot + truncate)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 5 nodes
		for i := 0; i < 5; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Close cleanly - this should snapshot AND truncate WAL
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Created 5 nodes, closed cleanly (snapshot + truncate)")
	}

	// Phase 2: Recover - should load from snapshot with empty WAL
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

		// Verify all nodes recovered from snapshot
		stats := gs.stats
		if stats.NodeCount != 5 {
			t.Errorf("Expected 5 nodes after snapshot recovery, got %d", stats.NodeCount)
		}

		// Verify each node
		for i, nodeID := range nodeIDs {
			node, err := gs.GetNode(nodeID)
			if err != nil {
				t.Errorf("Node %d not recovered from snapshot: %v", nodeID, err)
				continue
			}

			if idVal, exists := node.Properties["id"]; !exists {
				t.Errorf("Node %d missing id property", nodeID)
			} else if id, err := idVal.AsInt(); err != nil || id != int64(i) {
				t.Errorf("Node %d has wrong id: got %v, want %d", nodeID, id, i)
			}
		}

		t.Log("All nodes correctly recovered from snapshot after WAL truncation")
	}
}

// TestGraphStorage_SnapshotThenMoreOps tests operations after snapshot but before truncate
func TestGraphStorage_SnapshotThenMoreOps(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create 3 nodes, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		for i := 0; i < 3; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"phase": IntValue(1)})
		}

		gs.Close() // Snapshot + truncate
		t.Log("Phase 1: Created 3 nodes, closed cleanly")
	}

	// Phase 2: Recover, create 2 more nodes, crash (no close)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover from phase 1: %v", err)
		}

		stats := gs.stats
		if stats.NodeCount != 3 {
			t.Fatalf("Expected 3 nodes from phase 1, got %d", stats.NodeCount)
		}

		// Create 2 more nodes
		for i := 0; i < 2; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"phase": IntValue(2)})
		}

		// DON'T CLOSE - simulate crash
		t.Log("Phase 2: Recovered 3 nodes, created 2 more, crashing...")
	}

	// Phase 3: Recover - should have snapshot (3 nodes) + WAL (2 nodes) = 5 total
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed final recovery: %v", err)
		}
		defer gs.Close()

		stats := gs.stats
		if stats.NodeCount != 5 {
			t.Errorf("Expected 5 nodes (3 from snapshot + 2 from WAL), got %d", stats.NodeCount)
		}

		// Count nodes by phase
		persons, _ := gs.FindNodesByLabel("Person")
		phase1Count := 0
		phase2Count := 0

		for _, node := range persons {
			if phase, exists := node.Properties["phase"]; exists {
				if phaseVal, err := phase.AsInt(); err == nil {
					if phaseVal == 1 {
						phase1Count++
					} else if phaseVal == 2 {
						phase2Count++
					}
				}
			}
		}

		if phase1Count != 3 {
			t.Errorf("Expected 3 phase 1 nodes (from snapshot), got %d", phase1Count)
		}
		if phase2Count != 2 {
			t.Errorf("Expected 2 phase 2 nodes (from WAL), got %d", phase2Count)
		}

		t.Log("Correctly recovered 3 nodes from snapshot + 2 nodes from WAL replay")
	}
}

// TestGraphStorage_MultipleSnapshotCycles tests multiple snapshot/truncate cycles
func TestGraphStorage_MultipleSnapshotCycles(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	expectedTotal := 0

	// Cycle 1: Create 3 nodes, close
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		for i := 0; i < 3; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(1)})
		}
		expectedTotal += 3

		gs.Close()
		t.Log("Cycle 1: Created 3 nodes, closed (snapshot + truncate)")
	}

	// Cycle 2: Recover, create 4 nodes, close
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed cycle 2 recovery: %v", err)
		}

		stats := gs.stats
		if stats.NodeCount != 3 {
			t.Errorf("Cycle 2: Expected 3 nodes, got %d", stats.NodeCount)
		}

		for i := 0; i < 4; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(2)})
		}
		expectedTotal += 4

		gs.Close()
		t.Log("Cycle 2: Recovered 3, created 4 more, closed (snapshot + truncate)")
	}

	// Cycle 3: Recover, create 2 nodes, close
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed cycle 3 recovery: %v", err)
		}

		stats := gs.stats
		if stats.NodeCount != 7 {
			t.Errorf("Cycle 3: Expected 7 nodes, got %d", stats.NodeCount)
		}

		for i := 0; i < 2; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"cycle": IntValue(3)})
		}
		expectedTotal += 2

		gs.Close()
		t.Log("Cycle 3: Recovered 7, created 2 more, closed (snapshot + truncate)")
	}

	// Final recovery: Verify all nodes from all cycles
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed final recovery: %v", err)
		}
		defer gs.Close()

		stats := gs.stats
		if stats.NodeCount != uint64(expectedTotal) {
			t.Errorf("Final: Expected %d nodes total, got %d", expectedTotal, stats.NodeCount)
		}

		// Verify cycle distribution
		persons, _ := gs.FindNodesByLabel("Person")
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
		if cycleCounts[2] != 4 {
			t.Errorf("Expected 4 nodes from cycle 2, got %d", cycleCounts[2])
		}
		if cycleCounts[3] != 2 {
			t.Errorf("Expected 2 nodes from cycle 3, got %d", cycleCounts[3])
		}

		t.Log("Multiple snapshot cycles succeeded - all nodes recovered correctly")
	}
}

// TestGraphStorage_EdgesDurableAcrossSnapshot tests that edges survive snapshot/truncate
func TestGraphStorage_EdgesDurableAcrossSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID, node2ID uint64
	var edgeIDs []uint64

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

		node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Alice")})
		node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Bob")})
		node1ID = node1.ID
		node2ID = node2.ID

		// Create 3 edges
		for i := 0; i < 3; i++ {
			edge, err := gs.CreateEdge(node1ID, node2ID, "KNOWS", map[string]Value{
				"strength": IntValue(int64(i + 1)),
			}, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge failed: %v", err)
			}
			edgeIDs = append(edgeIDs, edge.ID)
		}

		gs.Close() // Snapshot + truncate
		t.Log("Created 2 nodes and 3 edges, closed cleanly")
	}

	// Phase 2: Recover from snapshot, verify edges
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

		// Verify edge count
		stats := gs.stats
		if stats.EdgeCount != 3 {
			t.Errorf("Expected 3 edges after snapshot recovery, got %d", stats.EdgeCount)
		}

		// Verify each edge
		for i, edgeID := range edgeIDs {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				t.Errorf("Edge %d not recovered: %v", edgeID, err)
				continue
			}

			if edge.FromNodeID != node1ID {
				t.Errorf("Edge %d has wrong FromNodeID", edgeID)
			}
			if edge.ToNodeID != node2ID {
				t.Errorf("Edge %d has wrong ToNodeID", edgeID)
			}

			expectedStrength := int64(i + 1)
			if strength, exists := edge.Properties["strength"]; !exists {
				t.Errorf("Edge %d missing strength property", edgeID)
			} else if strengthVal, err := strength.AsInt(); err != nil || strengthVal != expectedStrength {
				t.Errorf("Edge %d has wrong strength: got %v, want %d", edgeID, strengthVal, expectedStrength)
			}
		}

		// Verify adjacency lists
		outgoing, _ := gs.GetOutgoingEdges(node1ID)
		if len(outgoing) != 3 {
			t.Errorf("Expected 3 outgoing edges, got %d", len(outgoing))
		}

		incoming, _ := gs.GetIncomingEdges(node2ID)
		if len(incoming) != 3 {
			t.Errorf("Expected 3 incoming edges, got %d", len(incoming))
		}

		t.Log("Edges correctly recovered from snapshot with adjacency lists intact")
	}
}

// TestGraphStorage_IndexesDurableAcrossSnapshot tests that indexes survive snapshot/truncate
func TestGraphStorage_IndexesDurableAcrossSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create property index and nodes, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create property index
		err = gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Create nodes with indexed property
		gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(25),
		})
		gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("Bob"),
			"age":  IntValue(30),
		})
		gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("Charlie"),
			"age":  IntValue(25),
		})

		gs.Close() // Snapshot + truncate
		t.Log("Created property index and 3 nodes, closed cleanly")
	}

	// Phase 2: Recover from snapshot, verify indexes work
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

		// The property index metadata should be in snapshot, but NOT the index data
		// After recovery, the index needs to be rebuilt from WAL or snapshot
		//
		// CRITICAL QUESTION: Are property indexes serialized in the snapshot?
		// Looking at the Snapshot() code, I don't see property indexes being saved!

		// Try to query by property index
		nodes, err := gs.FindNodesByPropertyIndexed("age", IntValue(25))
		if err != nil {
			t.Fatalf("Property index query failed after snapshot: %v", err)
		}

		if len(nodes) != 2 {
			t.Errorf("Expected 2 nodes with age=25, got %d", len(nodes))
		}

		// Verify label index
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 3 {
			t.Errorf("Expected 3 Person nodes, got %d", len(persons))
		}

		t.Log("Indexes correctly recovered from snapshot")
	}
}
