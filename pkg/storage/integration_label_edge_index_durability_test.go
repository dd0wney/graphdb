package storage

import (
	"os"
	"testing"
)

// TestLabelIndexDurability_CrashRecovery tests label indexes survive crash
func TestLabelIndexDurability_CrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes with labels, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes with Person label
		for i := 0; i < 10; i++ {
			_, err := gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
		}

		// Create nodes with Company label
		for i := 0; i < 5; i++ {
			_, err := gs.CreateNode([]string{"Company"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
		}

		// Verify before crash
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel failed: %v", err)
		}
		if len(persons) != 10 {
			t.Fatalf("Before crash: Expected 10 Person nodes, got %d", len(persons))
		}

		companies, err := gs.FindNodesByLabel("Company")
		if err != nil {
			t.Fatalf("FindNodesByLabel failed: %v", err)
		}
		if len(companies) != 5 {
			t.Fatalf("Before crash: Expected 5 Company nodes, got %d", len(companies))
		}

		t.Logf("Before crash: 10 Person nodes, 5 Company nodes")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify label indexes rebuilt
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

		// Verify Person nodes
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Errorf("After crash: FindNodesByLabel(Person) failed: %v", err)
		}
		if len(persons) != 10 {
			t.Errorf("After crash: Expected 10 Person nodes, got %d (label index LOST!)", len(persons))
		}

		// Verify Company nodes
		companies, err := gs.FindNodesByLabel("Company")
		if err != nil {
			t.Errorf("After crash: FindNodesByLabel(Company) failed: %v", err)
		}
		if len(companies) != 5 {
			t.Errorf("After crash: Expected 5 Company nodes, got %d (label index LOST!)", len(companies))
		}

		t.Logf("After crash recovery: %d Person nodes, %d Company nodes",
			len(persons), len(companies))
	}
}

// TestLabelIndexDurability_MultipleLabels tests nodes with multiple labels
func TestLabelIndexDurability_MultipleLabels(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes with multiple labels, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes with multiple labels
		for i := 0; i < 5; i++ {
			_, err := gs.CreateNode([]string{"Person", "Employee"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			if err != nil {
				t.Fatalf("CreateNode failed: %v", err)
			}
		}

		// Verify both labels work before crash
		persons, _ := gs.FindNodesByLabel("Person")
		employees, _ := gs.FindNodesByLabel("Employee")
		if len(persons) != 5 || len(employees) != 5 {
			t.Fatalf("Before crash: Expected 5 for each label, got Person=%d, Employee=%d",
				len(persons), len(employees))
		}

		t.Log("Before crash: 5 nodes with both Person and Employee labels")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify multiple labels
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

		// Verify Person label
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Errorf("After crash: FindNodesByLabel(Person) failed: %v", err)
		}
		if len(persons) != 5 {
			t.Errorf("After crash: Expected 5 Person nodes, got %d", len(persons))
		}

		// Verify Employee label
		employees, err := gs.FindNodesByLabel("Employee")
		if err != nil {
			t.Errorf("After crash: FindNodesByLabel(Employee) failed: %v", err)
		}
		if len(employees) != 5 {
			t.Errorf("After crash: Expected 5 Employee nodes, got %d", len(employees))
		}

		t.Logf("After crash recovery: %d Person nodes, %d Employee nodes (both labels preserved)",
			len(persons), len(employees))
	}
}

// TestLabelIndexDurability_DeleteNode tests label index cleanup after delete
func TestLabelIndexDurability_DeleteNode(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var node2ID uint64

	// Phase 1: Create nodes, delete some, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 3 Person nodes
		gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Alice")})
		node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Bob")})
		gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Charlie")})
		node2ID = node2.ID

		// Delete Bob
		err = gs.DeleteNode(node2ID)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Verify 2 Person nodes remain before crash
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 2 {
			t.Fatalf("Before crash: Expected 2 Person nodes after delete, got %d", len(persons))
		}

		t.Log("Before crash: Created 3 Person nodes, deleted 1, 2 remain")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify deleted node not in label index
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

		// Verify only 2 Person nodes
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Errorf("After crash: FindNodesByLabel failed: %v", err)
		}
		if len(persons) != 2 {
			t.Errorf("After crash: Expected 2 Person nodes, got %d (delete not reflected!)", len(persons))
		}

		// Verify deleted node doesn't exist
		deletedNode, _ := gs.GetNode(node2ID)
		if deletedNode != nil {
			t.Errorf("After crash: Deleted node still exists!")
		}

		t.Logf("After crash recovery: %d Person nodes (deleted node correctly excluded)", len(persons))
	}
}

// TestEdgeTypeIndexDurability_CrashRecovery tests edge type indexes survive crash
func TestEdgeTypeIndexDurability_CrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create edges with different types, crash
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
		var nodeIDs []uint64
		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
			nodeIDs = append(nodeIDs, node.ID)
		}

		// Create KNOWS edges
		for i := 0; i < 3; i++ {
			_, err := gs.CreateEdge(nodeIDs[i], nodeIDs[i+1], "KNOWS", nil, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge(KNOWS) failed: %v", err)
			}
		}

		// Create WORKS_WITH edges
		for i := 0; i < 2; i++ {
			_, err := gs.CreateEdge(nodeIDs[i], nodeIDs[i+2], "WORKS_WITH", nil, 1.0)
			if err != nil {
				t.Fatalf("CreateEdge(WORKS_WITH) failed: %v", err)
			}
		}

		// Verify before crash
		knows, _ := gs.FindEdgesByType("KNOWS")
		worksWith, _ := gs.FindEdgesByType("WORKS_WITH")
		if len(knows) != 3 || len(worksWith) != 2 {
			t.Fatalf("Before crash: Expected KNOWS=3, WORKS_WITH=2, got KNOWS=%d, WORKS_WITH=%d",
				len(knows), len(worksWith))
		}

		t.Log("Before crash: 3 KNOWS edges, 2 WORKS_WITH edges")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify edge type indexes
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

		// Verify KNOWS edges
		knows, err := gs.FindEdgesByType("KNOWS")
		if err != nil {
			t.Errorf("After crash: FindEdgesByType(KNOWS) failed: %v", err)
		}
		if len(knows) != 3 {
			t.Errorf("After crash: Expected 3 KNOWS edges, got %d (edge type index LOST!)", len(knows))
		}

		// Verify WORKS_WITH edges
		worksWith, err := gs.FindEdgesByType("WORKS_WITH")
		if err != nil {
			t.Errorf("After crash: FindEdgesByType(WORKS_WITH) failed: %v", err)
		}
		if len(worksWith) != 2 {
			t.Errorf("After crash: Expected 2 WORKS_WITH edges, got %d (edge type index LOST!)", len(worksWith))
		}

		t.Logf("After crash recovery: %d KNOWS edges, %d WORKS_WITH edges",
			len(knows), len(worksWith))
	}
}

// TestEdgeTypeIndexDurability_DeleteEdge tests edge type index cleanup after delete
func TestEdgeTypeIndexDurability_DeleteEdge(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var edge2ID uint64

	// Phase 1: Create edges, delete some, crash
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

		// Create 3 KNOWS edges
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
		edge2, _ := gs.CreateEdge(node2.ID, node1.ID, "KNOWS", nil, 1.0)
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
		edge2ID = edge2.ID

		// Delete edge2
		err = gs.DeleteEdge(edge2ID)
		if err != nil {
			t.Fatalf("DeleteEdge failed: %v", err)
		}

		// Verify 2 KNOWS edges remain before crash
		knows, _ := gs.FindEdgesByType("KNOWS")
		if len(knows) != 2 {
			t.Fatalf("Before crash: Expected 2 KNOWS edges after delete, got %d", len(knows))
		}

		t.Log("Before crash: Created 3 KNOWS edges, deleted 1, 2 remain")

		// DON'T CLOSE - crash
	}

	// Phase 2: Recover and verify deleted edge not in type index
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

		// Verify only 2 KNOWS edges
		knows, err := gs.FindEdgesByType("KNOWS")
		if err != nil {
			t.Errorf("After crash: FindEdgesByType failed: %v", err)
		}
		if len(knows) != 2 {
			t.Errorf("After crash: Expected 2 KNOWS edges, got %d (delete not reflected!)", len(knows))
		}

		// Verify deleted edge doesn't exist
		deletedEdge, _ := gs.GetEdge(edge2ID)
		if deletedEdge != nil {
			t.Errorf("After crash: Deleted edge still exists!")
		}

		t.Logf("After crash recovery: %d KNOWS edges (deleted edge correctly excluded)", len(knows))
	}
}

// TestLabelIndexDurability_SnapshotRecovery tests labels survive snapshot
func TestLabelIndexDurability_SnapshotRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create nodes, close cleanly (snapshot)
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
		for i := 0; i < 10; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{"id": IntValue(int64(i))})
		}
		for i := 0; i < 5; i++ {
			gs.CreateNode([]string{"Company"}, map[string]Value{"id": IntValue(int64(i))})
		}

		// Close cleanly
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Phase 1: Created 10 Person and 5 Company nodes, closed cleanly")
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

		// Verify label indexes
		persons, _ := gs.FindNodesByLabel("Person")
		companies, _ := gs.FindNodesByLabel("Company")

		if len(persons) != 10 {
			t.Errorf("After snapshot recovery: Expected 10 Person nodes, got %d", len(persons))
		}
		if len(companies) != 5 {
			t.Errorf("After snapshot recovery: Expected 5 Company nodes, got %d", len(companies))
		}

		t.Logf("After snapshot recovery: %d Person nodes, %d Company nodes",
			len(persons), len(companies))
	}
}

// TestEdgeTypeIndexDurability_SnapshotRecovery tests edge types survive snapshot
func TestEdgeTypeIndexDurability_SnapshotRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create edges, close cleanly (snapshot)
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
		for i := 0; i < 5; i++ {
			gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
		}
		for i := 0; i < 3; i++ {
			gs.CreateEdge(node1.ID, node2.ID, "WORKS_WITH", nil, 1.0)
		}

		// Close cleanly
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Phase 1: Created 5 KNOWS and 3 WORKS_WITH edges, closed cleanly")
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

		// Verify edge type indexes
		knows, _ := gs.FindEdgesByType("KNOWS")
		worksWith, _ := gs.FindEdgesByType("WORKS_WITH")

		if len(knows) != 5 {
			t.Errorf("After snapshot recovery: Expected 5 KNOWS edges, got %d", len(knows))
		}
		if len(worksWith) != 3 {
			t.Errorf("After snapshot recovery: Expected 3 WORKS_WITH edges, got %d", len(worksWith))
		}

		t.Logf("After snapshot recovery: %d KNOWS edges, %d WORKS_WITH edges",
			len(knows), len(worksWith))
	}
}

// TestMixedIndexDurability tests both label and edge type indexes together
func TestMixedIndexDurability(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create complex graph, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create Person nodes
		var personIDs []uint64
		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
				"name": StringValue("Person" + string(rune('A'+i))),
			})
			personIDs = append(personIDs, node.ID)
		}

		// Create Company nodes
		var companyIDs []uint64
		for i := 0; i < 3; i++ {
			node, _ := gs.CreateNode([]string{"Company"}, map[string]Value{
				"name": StringValue("Company" + string(rune('A'+i))),
			})
			companyIDs = append(companyIDs, node.ID)
		}

		// Create KNOWS edges between Persons
		for i := 0; i < 4; i++ {
			gs.CreateEdge(personIDs[i], personIDs[i+1], "KNOWS", nil, 1.0)
		}

		// Create WORKS_FOR edges from Persons to Companies
		for i := 0; i < 5; i++ {
			gs.CreateEdge(personIDs[i], companyIDs[i%3], "WORKS_FOR", nil, 1.0)
		}

		t.Log("Before crash: 5 Person, 3 Company, 4 KNOWS, 5 WORKS_FOR")

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

		// Verify label indexes
		persons, _ := gs.FindNodesByLabel("Person")
		companies, _ := gs.FindNodesByLabel("Company")

		if len(persons) != 5 {
			t.Errorf("After crash: Expected 5 Person nodes, got %d", len(persons))
		}
		if len(companies) != 3 {
			t.Errorf("After crash: Expected 3 Company nodes, got %d", len(companies))
		}

		// Verify edge type indexes
		knows, _ := gs.FindEdgesByType("KNOWS")
		worksFor, _ := gs.FindEdgesByType("WORKS_FOR")

		if len(knows) != 4 {
			t.Errorf("After crash: Expected 4 KNOWS edges, got %d", len(knows))
		}
		if len(worksFor) != 5 {
			t.Errorf("After crash: Expected 5 WORKS_FOR edges, got %d", len(worksFor))
		}

		t.Logf("After crash recovery: Person=%d, Company=%d, KNOWS=%d, WORKS_FOR=%d",
			len(persons), len(companies), len(knows), len(worksFor))
	}
}
