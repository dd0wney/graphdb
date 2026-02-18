package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_LabelIndexDurableAfterCrash tests that label indexes survive crash recovery
func TestGraphStorage_LabelIndexDurableAfterCrash(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeIDs []uint64

	// Phase 1: Create nodes with labels, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create 5 Person nodes
		for i := 0; i < 5; i++ {
			node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Create 3 Company nodes
		for i := 0; i < 3; i++ {
			node, err := gs.CreateNode([]string{"Company"}, map[string]Value{
				"id": IntValue(int64(i + 100)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Verify indexes work before crash
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 5 {
			t.Fatalf("Before crash: Expected 5 Person nodes, got %d", len(persons))
		}

		companies, _ := gs.FindNodesByLabel("Company")
		if len(companies) != 3 {
			t.Fatalf("Before crash: Expected 3 Company nodes, got %d", len(companies))
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Created 5 Person and 3 Company nodes, simulating crash...")
	}

	// Phase 2: Recover and verify label indexes still work
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

		// Query by label - Person
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Person) failed after recovery: %v", err)
		}
		if len(persons) != 5 {
			t.Errorf("After recovery: Expected 5 Person nodes, got %d", len(persons))
		}

		// Query by label - Company
		companies, err := gs.FindNodesByLabel("Company")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Company) failed after recovery: %v", err)
		}
		if len(companies) != 3 {
			t.Errorf("After recovery: Expected 3 Company nodes, got %d", len(companies))
		}

		// Verify all nodes are correct
		for _, person := range persons {
			found := false
			for _, id := range nodeIDs[:5] {
				if person.ID == id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Person node %d not in original nodeIDs", person.ID)
			}
		}

		t.Log("Label indexes correctly recovered from WAL")
	}
}

// TestGraphStorage_TypeIndexDurableAfterCrash tests that type indexes survive crash recovery
func TestGraphStorage_TypeIndexDurableAfterCrash(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var edgeIDs []uint64
	var crashedStorage *GraphStorage

	// Phase 1: Create edges with types, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		crashedStorage = gs
		_ = crashedStorage // silence unused warning

		// Create nodes
		node1, _ := gs.CreateNode([]string{"Person"}, nil)
		node2, _ := gs.CreateNode([]string{"Person"}, nil)
		node3, _ := gs.CreateNode([]string{"Person"}, nil)

		// Create 3 KNOWS edges
		for i := 0; i < 3; i++ {
			edge, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
				"id": IntValue(int64(i)),
			}, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge(KNOWS) failed: %v", err)
			}
			edgeIDs = append(edgeIDs, edge.ID)
		}

		// Create 2 WORKS_AT edges
		for i := 0; i < 2; i++ {
			edge, err := gs.CreateEdge(node2.ID, node3.ID, "WORKS_AT", map[string]Value{
				"id": IntValue(int64(i + 100)),
			}, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge(WORKS_AT) failed: %v", err)
			}
			edgeIDs = append(edgeIDs, edge.ID)
		}

		// Verify indexes work before crash
		knows, _ := gs.FindEdgesByType("KNOWS")
		if len(knows) != 3 {
			t.Fatalf("Before crash: Expected 3 KNOWS edges, got %d", len(knows))
		}

		worksAt, _ := gs.FindEdgesByType("WORKS_AT")
		if len(worksAt) != 2 {
			t.Fatalf("Before crash: Expected 2 WORKS_AT edges, got %d", len(worksAt))
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Created 3 KNOWS and 2 WORKS_AT edges, simulating crash...")
	}

	// Phase 2: Recover and verify type indexes still work
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

		// Query by type - KNOWS
		knows, err := gs.FindEdgesByType("KNOWS")
		if err != nil {
			t.Fatalf("FindEdgesByType(KNOWS) failed after recovery: %v", err)
		}
		if len(knows) != 3 {
			t.Errorf("After recovery: Expected 3 KNOWS edges, got %d", len(knows))
		}

		// Query by type - WORKS_AT
		worksAt, err := gs.FindEdgesByType("WORKS_AT")
		if err != nil {
			t.Fatalf("FindEdgesByType(WORKS_AT) failed after recovery: %v", err)
		}
		if len(worksAt) != 2 {
			t.Errorf("After recovery: Expected 2 WORKS_AT edges, got %d", len(worksAt))
		}

		// Verify all edges are correct
		for _, edge := range knows {
			found := false
			for _, id := range edgeIDs[:3] {
				if edge.ID == id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("KNOWS edge %d not in original edgeIDs", edge.ID)
			}
		}

		t.Log("Type indexes correctly recovered from WAL")
	}
}

// TestGraphStorage_MultiLabelNodeDurability tests nodes with multiple labels
func TestGraphStorage_MultiLabelNodeDurability(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeID uint64

	// Phase 1: Create node with multiple labels, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create node with 3 labels
		node, err := gs.CreateNode([]string{"Person", "Employee", "Manager"}, map[string]Value{
			"name": StringValue("Alice"),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		nodeID = node.ID

		// Verify all label indexes work before crash
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 1 {
			t.Fatalf("Before crash: Expected 1 Person node, got %d", len(persons))
		}

		employees, _ := gs.FindNodesByLabel("Employee")
		if len(employees) != 1 {
			t.Fatalf("Before crash: Expected 1 Employee node, got %d", len(employees))
		}

		managers, _ := gs.FindNodesByLabel("Manager")
		if len(managers) != 1 {
			t.Fatalf("Before crash: Expected 1 Manager node, got %d", len(managers))
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Created node with 3 labels, simulating crash...")
	}

	// Phase 2: Recover and verify all label indexes work
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

		// Verify all 3 label indexes work after crash
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Person) failed: %v", err)
		}
		if len(persons) != 1 || persons[0].ID != nodeID {
			t.Errorf("After recovery: Expected Person node %d, got %d nodes", nodeID, len(persons))
		}

		employees, err := gs.FindNodesByLabel("Employee")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Employee) failed: %v", err)
		}
		if len(employees) != 1 || employees[0].ID != nodeID {
			t.Errorf("After recovery: Expected Employee node %d, got %d nodes", nodeID, len(employees))
		}

		managers, err := gs.FindNodesByLabel("Manager")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Manager) failed: %v", err)
		}
		if len(managers) != 1 || managers[0].ID != nodeID {
			t.Errorf("After recovery: Expected Manager node %d, got %d nodes", nodeID, len(managers))
		}

		t.Log("Multi-label node correctly indexed after crash recovery")
	}
}

// TestGraphStorage_LabelIndexAfterNodeDeletion tests that label indexes are cleaned up when nodes are deleted
func TestGraphStorage_LabelIndexAfterNodeDeletion(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes, delete some, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create 5 Person nodes
		var nodeIDs []uint64
		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"id": IntValue(int64(i))})
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Delete 2 of them
		gs.DeleteNode(nodeIDs[1])
		gs.DeleteNode(nodeIDs[3])

		// Verify label index before crash
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 3 {
			t.Fatalf("Before crash: Expected 3 Person nodes after deletion, got %d", len(persons))
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Created 5 nodes, deleted 2, simulating crash...")
	}

	// Phase 2: Recover and verify label index reflects deletions
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

		// Verify label index after crash - should have 3 nodes, not 5
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel failed: %v", err)
		}
		if len(persons) != 3 {
			t.Errorf("After recovery: Expected 3 Person nodes, got %d", len(persons))
		}

		t.Log("Label index correctly reflects node deletions after crash recovery")
	}
}

// TestGraphStorage_TypeIndexAfterEdgeDeletion tests that type indexes are cleaned up when edges are deleted
func TestGraphStorage_TypeIndexAfterEdgeDeletion(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create edges, delete some, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create nodes
		node1, _ := gs.CreateNode([]string{"Person"}, nil)
		node2, _ := gs.CreateNode([]string{"Person"}, nil)

		// Create 5 KNOWS edges
		var edgeIDs []uint64
		for i := 0; i < 5; i++ {
			edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
			edgeIDs = append(edgeIDs, edge.ID)
		}

		// Delete 2 of them
		gs.DeleteEdge(edgeIDs[1])
		gs.DeleteEdge(edgeIDs[3])

		// Verify type index before crash
		knows, _ := gs.FindEdgesByType("KNOWS")
		if len(knows) != 3 {
			t.Fatalf("Before crash: Expected 3 KNOWS edges after deletion, got %d", len(knows))
		}

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
		t.Log("Created 5 edges, deleted 2, simulating crash...")
	}

	// Phase 2: Recover and verify type index reflects deletions
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

		// Verify type index after crash - should have 3 edges, not 5
		knows, err := gs.FindEdgesByType("KNOWS")
		if err != nil {
			t.Fatalf("FindEdgesByType failed: %v", err)
		}
		if len(knows) != 3 {
			t.Errorf("After recovery: Expected 3 KNOWS edges, got %d", len(knows))
		}

		t.Log("Type index correctly reflects edge deletions after crash recovery")
	}
}

// TestGraphStorage_LabelIndexSnapshot tests that label indexes survive clean shutdown
func TestGraphStorage_LabelIndexSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

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

		// Create nodes with different labels
		gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Alice")})
		gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Bob")})
		gs.CreateNode([]string{"Company"}, map[string]Value{"name": StringValue("Acme")})

		// Close cleanly - snapshot + truncate
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Created 2 Person and 1 Company nodes, closed cleanly")
	}

	// Phase 2: Recover from snapshot and verify label indexes
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

		// Verify label indexes from snapshot
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Person) failed: %v", err)
		}
		if len(persons) != 2 {
			t.Errorf("Expected 2 Person nodes from snapshot, got %d", len(persons))
		}

		companies, err := gs.FindNodesByLabel("Company")
		if err != nil {
			t.Fatalf("FindNodesByLabel(Company) failed: %v", err)
		}
		if len(companies) != 1 {
			t.Errorf("Expected 1 Company node from snapshot, got %d", len(companies))
		}

		t.Log("Label indexes correctly recovered from snapshot")
	}
}

// TestGraphStorage_TypeIndexSnapshot tests that type indexes survive clean shutdown
func TestGraphStorage_TypeIndexSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

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

		// Create nodes
		node1, _ := gs.CreateNode([]string{"Person"}, nil)
		node2, _ := gs.CreateNode([]string{"Person"}, nil)

		// Create edges with different types
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
		gs.CreateEdge(node1.ID, node2.ID, "LIKES", nil, 1.0)

		// Close cleanly - snapshot + truncate
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Created 2 KNOWS and 1 LIKES edges, closed cleanly")
	}

	// Phase 2: Recover from snapshot and verify type indexes
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

		// Verify type indexes from snapshot
		knows, err := gs.FindEdgesByType("KNOWS")
		if err != nil {
			t.Fatalf("FindEdgesByType(KNOWS) failed: %v", err)
		}
		if len(knows) != 2 {
			t.Errorf("Expected 2 KNOWS edges from snapshot, got %d", len(knows))
		}

		likes, err := gs.FindEdgesByType("LIKES")
		if err != nil {
			t.Fatalf("FindEdgesByType(LIKES) failed: %v", err)
		}
		if len(likes) != 1 {
			t.Errorf("Expected 1 LIKES edge from snapshot, got %d", len(likes))
		}

		t.Log("Type indexes correctly recovered from snapshot")
	}
}
