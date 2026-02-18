package storage

import (
	"os"
	"testing"
)

// TestPropertyIndexDurability_CreateIndexThenNodes tests index creation before nodes
func TestPropertyIndexDurability_CreateIndexThenNodes(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create index, add nodes, crash
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		// Create property index FIRST
		err := gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Then create nodes with that property
		for i := 0; i < 10; i++ {
			_, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"name": StringValue("User" + string(rune('A'+i))),
				"age":  IntValue(int64(20 + i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
		}

		// Verify index works before crash
		nodes, err := gs.FindNodesByProperty("age", IntValue(25))
		if err != nil {
			t.Fatalf("FindNodesByProperty failed: %v", err)
		}
		if len(nodes) != 1 {
			t.Fatalf("Before crash: Expected 1 node with age=25, got %d", len(nodes))
		}

		t.Logf("Before crash: Index has %d nodes", gs.propertyIndexes["age"].GetStatistics().TotalNodes)

		// DON'T CLOSE - simulate crash (testCrashableStorage handles cleanup)
	}

	// Phase 2: Recover and verify index rebuilt
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

		// Check if index exists
		if _, exists := gs.propertyIndexes["age"]; !exists {
			t.Errorf("After crash: Property index 'age' was LOST!")
		}

		// Check if index is populated
		nodes, err := gs.FindNodesByProperty("age", IntValue(25))
		if err != nil {
			t.Errorf("After crash: FindNodesByProperty failed: %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("After crash: Expected 1 node with age=25, got %d", len(nodes))
		}

		// Verify all 10 ages are indexed
		indexSize := gs.propertyIndexes["age"].GetStatistics().TotalNodes
		if indexSize != 10 {
			t.Errorf("After crash: Expected index size=10, got %d", indexSize)
		}

		t.Logf("After crash recovery: Index exists and has %d nodes", indexSize)
	}
}

// TestPropertyIndexDurability_NodesBeforeIndex tests nodes created before index
func TestPropertyIndexDurability_NodesBeforeIndex(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes FIRST, then index, then crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes FIRST (without index)
		for i := 0; i < 5; i++ {
			_, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"name": StringValue("User" + string(rune('A'+i))),
				"age":  IntValue(int64(30 + i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
		}

		// THEN create index (should populate with existing nodes)
		err = gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Verify index was populated
		indexSize := gs.propertyIndexes["age"].GetStatistics().TotalNodes
		if indexSize != 5 {
			t.Fatalf("Before crash: Expected index size=5, got %d", indexSize)
		}

		t.Logf("Before crash: Created 5 nodes, then index, index populated with %d entries", indexSize)

		// DON'T CLOSE - crash
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

		// Check if index exists
		if _, exists := gs.propertyIndexes["age"]; !exists {
			t.Errorf("After crash: Property index 'age' was LOST!")
			return
		}

		// Check if index has all 5 nodes
		indexSize := gs.propertyIndexes["age"].GetStatistics().TotalNodes
		if indexSize != 5 {
			t.Errorf("After crash: Expected index size=5, got %d (index not properly rebuilt!)", indexSize)
		}

		// Verify we can query using the index
		nodes, err := gs.FindNodesByProperty("age", IntValue(32))
		if err != nil {
			t.Errorf("After crash: FindNodesByProperty failed: %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("After crash: Expected 1 node with age=32, got %d", len(nodes))
		}

		t.Logf("After crash recovery: Index exists and has %d nodes (correctly rebuilt)", indexSize)
	}
}

// TestPropertyIndexDurability_UpdateNodes tests index updates survive crash
func TestPropertyIndexDurability_UpdateNodes(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node1ID uint64

	// Phase 1: Create index, create node, update node, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create index
		err = gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Create node with age=25
		node1, err := gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(25),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		node1ID = node1.ID

		// Update node's age to 30
		err = gs.UpdateNode(node1ID, map[string]Value{
			"age": IntValue(30),
		})
		if err != nil {
			t.Fatalf("UpdateNode failed: %v", err)
		}

		// Verify update worked before crash
		nodes, _ := gs.FindNodesByProperty("age", IntValue(30))
		if len(nodes) != 1 {
			t.Fatalf("Before crash: Expected 1 node with age=30, got %d", len(nodes))
		}

		// Old value should not be found
		nodes, _ = gs.FindNodesByProperty("age", IntValue(25))
		if len(nodes) != 0 {
			t.Fatalf("Before crash: Expected 0 nodes with age=25, got %d", len(nodes))
		}

		t.Log("Before crash: Updated age 25->30, index updated correctly")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify index reflects updates
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

		// Check if updated value is in index
		nodes, err := gs.FindNodesByProperty("age", IntValue(30))
		if err != nil {
			t.Errorf("After crash: FindNodesByProperty(age=30) failed: %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("After crash: Expected 1 node with age=30, got %d (update LOST!)", len(nodes))
		}

		// Old value should NOT be in index
		nodes, _ = gs.FindNodesByProperty("age", IntValue(25))
		if len(nodes) != 0 {
			t.Errorf("After crash: Expected 0 nodes with age=25, got %d (old value NOT removed!)", len(nodes))
		}

		t.Log("After crash recovery: Index correctly reflects updated value")
	}
}

// TestPropertyIndexDurability_DeleteNodes tests index handles deletes
func TestPropertyIndexDurability_DeleteNodes(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node2ID uint64

	// Phase 1: Create index, create nodes, delete some, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create index
		err = gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Create 3 nodes
		gs.CreateNode([]string{"Person"}, map[string]Value{"age": IntValue(20)})
		node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"age": IntValue(21)})
		gs.CreateNode([]string{"Person"}, map[string]Value{"age": IntValue(22)})
		node2ID = node2.ID

		// Delete node2
		err = gs.DeleteNode(node2ID)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Verify index has 2 entries (node1 and node3)
		indexSize := gs.propertyIndexes["age"].GetStatistics().TotalNodes
		if indexSize != 2 {
			t.Fatalf("Before crash: Expected index size=2 after delete, got %d", indexSize)
		}

		// Verify deleted node not in index
		nodes, _ := gs.FindNodesByProperty("age", IntValue(21))
		if len(nodes) != 0 {
			t.Fatalf("Before crash: Deleted node still in index!")
		}

		t.Log("Before crash: Created 3 nodes, deleted 1, index has 2 entries")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify index doesn't have deleted nodes
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

		// Check index size
		indexSize := gs.propertyIndexes["age"].GetStatistics().TotalNodes
		if indexSize != 2 {
			t.Errorf("After crash: Expected index size=2, got %d (delete not reflected!)", indexSize)
		}

		// Verify deleted node NOT in index
		nodes, _ := gs.FindNodesByProperty("age", IntValue(21))
		if len(nodes) != 0 {
			t.Errorf("After crash: Deleted node (age=21) still in index!")
		}

		// Verify other nodes ARE in index
		nodes1, _ := gs.FindNodesByProperty("age", IntValue(20))
		nodes3, _ := gs.FindNodesByProperty("age", IntValue(22))
		if len(nodes1) != 1 || len(nodes3) != 1 {
			t.Errorf("After crash: Expected nodes with age=20 and age=22, got %d and %d",
				len(nodes1), len(nodes3))
		}

		t.Logf("After crash recovery: Index correctly excludes deleted node")
	}
}

// TestPropertyIndexDurability_DropIndex tests dropping index survives crash
func TestPropertyIndexDurability_DropIndex(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create index, drop it, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create some nodes
		for i := 0; i < 5; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"age": IntValue(int64(20 + i)),
			})
		}

		// Create index
		err = gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Verify index exists
		if _, exists := gs.propertyIndexes["age"]; !exists {
			t.Fatal("Index should exist before drop")
		}

		// Drop the index
		err = gs.DropPropertyIndex("age")
		if err != nil {
			t.Fatalf("DropPropertyIndex failed: %v", err)
		}

		// Verify index dropped
		if _, exists := gs.propertyIndexes["age"]; exists {
			t.Fatal("Index should NOT exist after drop")
		}

		t.Log("Before crash: Created and dropped index")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify index stays dropped
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

		// Verify index does NOT exist
		if _, exists := gs.propertyIndexes["age"]; exists {
			t.Errorf("After crash: Index should stay DROPPED, but it exists!")
		}

		t.Log("After crash recovery: Index correctly stays dropped")
	}
}

// TestPropertyIndexDurability_SnapshotRecovery tests index survives snapshot
func TestPropertyIndexDurability_SnapshotRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create index, close cleanly (snapshot)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create index
		err = gs.CreatePropertyIndex("age", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Create nodes
		for i := 0; i < 10; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"age": IntValue(int64(25 + i)),
			})
		}

		// Close cleanly (creates snapshot)
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Phase 1: Created index with 10 nodes, closed cleanly")
	}

	// Phase 2: Recover from snapshot
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover from snapshot: %v", err)
		}
		defer gs.Close()

		// Verify index exists
		if _, exists := gs.propertyIndexes["age"]; !exists {
			t.Errorf("After snapshot recovery: Index was LOST!")
			return
		}

		// Verify index size
		indexSize := gs.propertyIndexes["age"].GetStatistics().TotalNodes
		if indexSize != 10 {
			t.Errorf("After snapshot recovery: Expected index size=10, got %d", indexSize)
		}

		// Verify queries work
		nodes, err := gs.FindNodesByProperty("age", IntValue(30))
		if err != nil {
			t.Errorf("After snapshot recovery: FindNodesByProperty failed: %v", err)
		}
		if len(nodes) != 1 {
			t.Errorf("After snapshot recovery: Expected 1 node with age=30, got %d", len(nodes))
		}

		t.Logf("After snapshot recovery: Index exists with %d entries and queries work", indexSize)
	}
}

// TestPropertyIndexDurability_MultipleIndexes tests multiple indexes survive crash
func TestPropertyIndexDurability_MultipleIndexes(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create multiple indexes, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 3 different indexes
		gs.CreatePropertyIndex("age", TypeInt)
		gs.CreatePropertyIndex("name", TypeString)
		gs.CreatePropertyIndex("active", TypeBool)

		// Create nodes with all properties
		for i := 0; i < 5; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"age":    IntValue(int64(20 + i)),
				"name":   StringValue("User" + string(rune('A'+i))),
				"active": BoolValue(i%2 == 0),
			})
		}

		// Verify all indexes work
		if len(gs.propertyIndexes) != 3 {
			t.Fatalf("Before crash: Expected 3 indexes, got %d", len(gs.propertyIndexes))
		}

		t.Log("Before crash: Created 3 indexes with 5 nodes each")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify all indexes
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

		// Check all 3 indexes exist
		if len(gs.propertyIndexes) != 3 {
			t.Errorf("After crash: Expected 3 indexes, got %d (indexes LOST!)", len(gs.propertyIndexes))
		}

		// Verify each index works
		ageNodes, _ := gs.FindNodesByProperty("age", IntValue(22))
		nameNodes, _ := gs.FindNodesByProperty("name", StringValue("UserC"))
		activeNodes, _ := gs.FindNodesByProperty("active", BoolValue(true))

		if len(ageNodes) != 1 || len(nameNodes) != 1 || len(activeNodes) != 3 {
			t.Errorf("After crash: Index queries failed - age:%d, name:%d, active:%d",
				len(ageNodes), len(nameNodes), len(activeNodes))
		}

		t.Logf("After crash recovery: All 3 indexes exist and work correctly")
	}
}
