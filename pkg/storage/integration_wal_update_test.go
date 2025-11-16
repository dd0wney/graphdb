package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_NodePropertyUpdateDurable tests that node property updates survive crashes
func TestGraphStorage_NodePropertyUpdateDurable(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeID uint64

	// Phase 1: Create node with initial properties, update them, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create node with initial properties
		node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(25),
			"city": StringValue("NYC"),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		nodeID = node.ID

		// Update node properties
		err = gs.UpdateNode(nodeID, map[string]Value{
			"age":     IntValue(26),      // Update existing
			"city":    StringValue("SF"),  // Update existing
			"country": StringValue("USA"), // Add new
		})
		if err != nil {
			t.Fatalf("UpdateNode failed: %v", err)
		}

		// DON'T CLOSE - simulate crash
		t.Log("Created node and updated properties, simulating crash...")
	}

	// Phase 2: Recover and verify updates persisted
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

		// Get the node
		node, err := gs.GetNode(nodeID)
		if err != nil {
			t.Fatalf("GetNode failed after recovery: %v", err)
		}

		// Verify updated properties
		if name, _ := node.Properties["name"].AsString(); name != "Alice" {
			t.Errorf("Expected name 'Alice', got '%s'", name)
		}
		if age, _ := node.Properties["age"].AsInt(); age != 26 {
			t.Errorf("Expected age 26, got %d", age)
		}
		if city, _ := node.Properties["city"].AsString(); city != "SF" {
			t.Errorf("Expected city 'SF', got '%s'", city)
		}
		if country, _ := node.Properties["country"].AsString(); country != "USA" {
			t.Errorf("Expected country 'USA', got '%s'", country)
		}

		t.Log("Node property updates correctly recovered from WAL")
	}
}

// NOTE: UpdateEdge method does not exist in GraphStorage API
// OpUpdateEdge is defined in WAL but the method is not implemented
// This test is skipped until UpdateEdge is implemented

// TestGraphStorage_PropertyIndexUpdateOnNodeUpdate tests that property indexes update when node properties change
func TestGraphStorage_PropertyIndexUpdateOnNodeUpdate(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeID uint64

	// Phase 1: Create property index, create node, update indexed property, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create property index on "score"
		err = gs.CreatePropertyIndex("score", TypeInt)
		if err != nil {
			t.Fatalf("CreatePropertyIndex failed: %v", err)
		}

		// Create node with score=100
		node, err := gs.CreateNode([]string{"Player"}, map[string]Value{
			"name":  StringValue("Alice"),
			"score": IntValue(100),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		nodeID = node.ID

		// Verify node is in index with score=100
		nodes, err := gs.FindNodesByPropertyIndexed("score", IntValue(100))
		if err != nil {
			t.Fatalf("FindNodesByPropertyIndexed failed: %v", err)
		}
		if len(nodes) != 1 || nodes[0].ID != nodeID {
			t.Fatalf("Expected node in index with score=100")
		}

		// Update node's score to 200
		err = gs.UpdateNode(nodeID, map[string]Value{
			"score": IntValue(200),
		})
		if err != nil {
			t.Fatalf("UpdateNode failed: %v", err)
		}

		// Verify node is now in index with score=200, not score=100
		nodesOld, _ := gs.FindNodesByPropertyIndexed("score", IntValue(100))
		if len(nodesOld) != 0 {
			t.Errorf("Expected no nodes with score=100 after update, got %d", len(nodesOld))
		}

		nodesNew, err := gs.FindNodesByPropertyIndexed("score", IntValue(200))
		if err != nil {
			t.Fatalf("FindNodesByPropertyIndexed failed: %v", err)
		}
		if len(nodesNew) != 1 || nodesNew[0].ID != nodeID {
			t.Fatalf("Expected node in index with score=200 after update")
		}

		// DON'T CLOSE - simulate crash
		t.Log("Updated indexed property, simulating crash...")
	}

	// Phase 2: Recover and verify index reflects updated value
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

		// Verify node is NOT in index with old value (score=100)
		nodesOld, err := gs.FindNodesByPropertyIndexed("score", IntValue(100))
		if err != nil {
			t.Fatalf("FindNodesByPropertyIndexed failed: %v", err)
		}
		if len(nodesOld) != 0 {
			t.Errorf("After recovery: Expected no nodes with score=100, got %d", len(nodesOld))
		}

		// Verify node IS in index with new value (score=200)
		nodesNew, err := gs.FindNodesByPropertyIndexed("score", IntValue(200))
		if err != nil {
			t.Fatalf("FindNodesByPropertyIndexed failed: %v", err)
		}
		if len(nodesNew) != 1 || nodesNew[0].ID != nodeID {
			t.Errorf("After recovery: Expected node in index with score=200")
		}

		// Verify node's property is correct
		node, err := gs.GetNode(nodeID)
		if err != nil {
			t.Fatalf("GetNode failed: %v", err)
		}
		if score, _ := node.Properties["score"].AsInt(); score != 200 {
			t.Errorf("Expected score 200, got %d", score)
		}

		t.Log("Property index correctly updated after node property update and crash recovery")
	}
}

// TestGraphStorage_MultipleUpdatesSequential tests multiple sequential updates to same node
func TestGraphStorage_MultipleUpdatesSequential(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeID uint64

	// Phase 1: Create node and apply multiple updates, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create node
		node, err := gs.CreateNode([]string{"Counter"}, map[string]Value{
			"value": IntValue(0),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		nodeID = node.ID

		// Apply 5 sequential updates
		for i := 1; i <= 5; i++ {
			err = gs.UpdateNode(nodeID, map[string]Value{
				"value": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("UpdateNode iteration %d failed: %v", i, err)
			}
		}

		// DON'T CLOSE - simulate crash
		t.Log("Applied 5 sequential updates, simulating crash...")
	}

	// Phase 2: Recover and verify final value is correct
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

		// Get the node
		node, err := gs.GetNode(nodeID)
		if err != nil {
			t.Fatalf("GetNode failed after recovery: %v", err)
		}

		// Verify final value is 5 (last update)
		if value, _ := node.Properties["value"].AsInt(); value != 5 {
			t.Errorf("Expected value 5 (final update), got %d", value)
		}

		t.Log("Multiple sequential updates correctly recovered - final value is 5")
	}
}

// TestGraphStorage_UpdateThenSnapshot tests that updates survive clean shutdown via snapshot
func TestGraphStorage_UpdateThenSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeID uint64

	// Phase 1: Create node, update it, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create node
		node, err := gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(25),
		})
		if err != nil {
			t.Fatalf("CreateNode failed: %v", err)
		}
		nodeID = node.ID

		// Update node
		gs.UpdateNode(nodeID, map[string]Value{
			"age": IntValue(30),
		})

		// Close cleanly - snapshot + truncate
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Created and updated node, closed cleanly (snapshot)")
	}

	// Phase 2: Recover from snapshot and verify updates
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

		// Verify node update persisted in snapshot
		node, err := gs.GetNode(nodeID)
		if err != nil {
			t.Fatalf("GetNode failed after snapshot recovery: %v", err)
		}
		if age, _ := node.Properties["age"].AsInt(); age != 30 {
			t.Errorf("Expected age 30 from snapshot, got %d", age)
		}

		t.Log("Node update correctly recovered from snapshot after clean shutdown")
	}
}

// TestGraphStorage_UpdateNonExistentNode tests error handling for updating non-existent nodes
func TestGraphStorage_UpdateNonExistentNode(t *testing.T) {
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

	// Try to update non-existent node
	err = gs.UpdateNode(99999, map[string]Value{
		"foo": StringValue("bar"),
	})

	if err == nil {
		t.Error("Expected error when updating non-existent node, got nil")
	}

	t.Logf("Correctly returned error for non-existent node: %v", err)
}

// NOTE: UpdateEdge method does not exist - skipping edge update error test
