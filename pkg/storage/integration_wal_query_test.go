package storage

import (
	"os"
	"testing"
)

// TestGraphStorage_LabelIndexRecovery tests that GetNodesByLabel works after crash recovery
func TestGraphStorage_LabelIndexRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var personIDs, companyIDs []uint64

	// Phase 1: Create nodes with different labels, crash (no Close)
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
		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			personIDs = append(personIDs, node.ID)
		}

		// Create Company nodes
		for i := 0; i < 3; i++ {
			node, _ := gs.CreateNode([]string{"Company"}, nil)
			companyIDs = append(companyIDs, node.ID)
		}

		// Create multi-label nodes
		for i := 0; i < 2; i++ {
			gs.CreateNode([]string{"Person", "Employee"}, nil)
		}

		// Verify labels work before crash
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 7 { // 5 + 2 multi-label
			t.Fatalf("Expected 7 Person nodes before crash, got %d", len(persons))
		}

		companies, _ := gs.FindNodesByLabel("Company")
		if len(companies) != 3 {
			t.Fatalf("Expected 3 Company nodes before crash, got %d", len(companies))
		}

		// DON'T CLOSE - simulate crash
		t.Log("Simulating crash after creating labeled nodes")
	}

	// Phase 2: Recover and verify label indexes were rebuilt
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

		// Verify Person label index
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel failed: %v", err)
		}

		if len(persons) != 7 {
			t.Errorf("Label index broken after crash! Expected 7 Person nodes, got %d", len(persons))
		}

		// Verify all original Person IDs are present
		personMap := make(map[uint64]bool)
		for _, node := range persons {
			personMap[node.ID] = true
		}

		for _, id := range personIDs {
			if !personMap[id] {
				t.Errorf("Person node %d missing from label index after crash!", id)
			}
		}

		// Verify Company label index
		companies, err := gs.FindNodesByLabel("Company")
		if err != nil {
			t.Fatalf("FindNodesByLabel failed: %v", err)
		}

		if len(companies) != 3 {
			t.Errorf("Label index broken after crash! Expected 3 Company nodes, got %d", len(companies))
		}

		// Verify Employee label index (from multi-label nodes)
		employees, _ := gs.FindNodesByLabel("Employee")
		if len(employees) != 2 {
			t.Errorf("Multi-label index broken after crash! Expected 2 Employee nodes, got %d", len(employees))
		}

		t.Log("Label indexes correctly recovered from crash via WAL")
	}
}

// TestGraphStorage_TypeIndexRecovery tests that GetEdgesByType works after crash recovery
func TestGraphStorage_TypeIndexRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var knowsIDs, worksAtIDs []uint64

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
		person1, _ := gs.CreateNode([]string{"Person"}, nil)
		person2, _ := gs.CreateNode([]string{"Person"}, nil)
		company, _ := gs.CreateNode([]string{"Company"}, nil)

		// Create KNOWS edges
		for i := 0; i < 4; i++ {
			edge, _ := gs.CreateEdge(person1.ID, person2.ID, "KNOWS", nil, 1.0)
			knowsIDs = append(knowsIDs, edge.ID)
		}

		// Create WORKS_AT edges
		for i := 0; i < 3; i++ {
			edge, _ := gs.CreateEdge(person1.ID, company.ID, "WORKS_AT", nil, 1.0)
			worksAtIDs = append(worksAtIDs, edge.ID)
		}

		// Verify type index before crash
		knows, _ := gs.FindEdgesByType("KNOWS")
		if len(knows) != 4 {
			t.Fatalf("Expected 4 KNOWS edges before crash, got %d", len(knows))
		}

		worksAt, _ := gs.FindEdgesByType("WORKS_AT")
		if len(worksAt) != 3 {
			t.Fatalf("Expected 3 WORKS_AT edges before crash, got %d", len(worksAt))
		}

		// DON'T CLOSE - simulate crash
		t.Log("Simulating crash after creating typed edges")
	}

	// Phase 2: Recover and verify type indexes were rebuilt
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

		// Verify KNOWS type index
		knows, err := gs.FindEdgesByType("KNOWS")
		if err != nil {
			t.Fatalf("FindEdgesByType failed: %v", err)
		}

		if len(knows) != 4 {
			t.Errorf("Type index broken after crash! Expected 4 KNOWS edges, got %d", len(knows))
		}

		// Verify all original KNOWS IDs are present
		knowsMap := make(map[uint64]bool)
		for _, edge := range knows {
			knowsMap[edge.ID] = true
		}

		for _, id := range knowsIDs {
			if !knowsMap[id] {
				t.Errorf("KNOWS edge %d missing from type index after crash!", id)
			}
		}

		// Verify WORKS_AT type index
		worksAt, err := gs.FindEdgesByType("WORKS_AT")
		if err != nil {
			t.Fatalf("FindEdgesByType failed: %v", err)
		}

		if len(worksAt) != 3 {
			t.Errorf("Type index broken after crash! Expected 3 WORKS_AT edges, got %d", len(worksAt))
		}

		t.Log("Type indexes correctly recovered from crash via WAL")
	}
}

// TestGraphStorage_PropertyIndexRecovery tests that property indexes are rebuilt after crash
func TestGraphStorage_PropertyIndexRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create property index and nodes, crash
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
			"age":  IntValue(25), // Same age as Alice
		})

		// Verify index works before crash
		nodes, _ := gs.FindNodesByPropertyIndexed("age", IntValue(25))
		if len(nodes) != 2 {
			t.Fatalf("Expected 2 nodes with age=25 before crash, got %d", len(nodes))
		}

		// DON'T CLOSE - simulate crash
		t.Log("Simulating crash after creating property index and nodes")
	}

	// Phase 2: Recover and verify property index was rebuilt
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

		// Verify property index query works
		nodes, err := gs.FindNodesByPropertyIndexed("age", IntValue(25))
		if err != nil {
			t.Fatalf("FindNodesByPropertyIndexed failed after crash: %v", err)
		}

		if len(nodes) != 2 {
			t.Errorf("Property index broken after crash! Expected 2 nodes with age=25, got %d", len(nodes))
		}

		// Verify other query
		nodes, err = gs.FindNodesByPropertyIndexed("age", IntValue(30))
		if err != nil {
			t.Fatalf("FindNodesByPropertyIndexed failed: %v", err)
		}

		if len(nodes) != 1 {
			t.Errorf("Property index broken after crash! Expected 1 node with age=30, got %d", len(nodes))
		}

		t.Log("Property indexes correctly recovered from crash via WAL")
	}
}

// TestGraphStorage_DeletedNodeLabelIndexRecovery tests that deleted nodes are removed from label indexes
func TestGraphStorage_DeletedNodeLabelIndexRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	var nodeToDelete uint64

	// Phase 1: Create nodes, delete one, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create 5 Person nodes
		for i := 0; i < 5; i++ {
			node, _ := gs.CreateNode([]string{"Person"}, nil)
			if i == 2 {
				nodeToDelete = node.ID
			}
		}

		// Delete one node
		err = gs.DeleteNode(nodeToDelete)
		if err != nil {
			t.Fatalf("DeleteNode failed: %v", err)
		}

		// Verify before crash
		persons, _ := gs.FindNodesByLabel("Person")
		if len(persons) != 4 {
			t.Fatalf("Expected 4 Person nodes after deletion, got %d", len(persons))
		}

		// DON'T CLOSE - simulate crash
		t.Log("Simulating crash after node deletion")
	}

	// Phase 2: Recover and verify label index is correct
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

		// Verify label index doesn't include deleted node
		persons, err := gs.FindNodesByLabel("Person")
		if err != nil {
			t.Fatalf("FindNodesByLabel failed: %v", err)
		}

		if len(persons) != 4 {
			t.Errorf("Label index incorrect after crash! Expected 4 Person nodes, got %d", len(persons))
		}

		// Verify deleted node is not in the results
		for _, node := range persons {
			if node.ID == nodeToDelete {
				t.Error("Deleted node still in label index after crash!")
			}
		}

		t.Log("Label indexes correctly reflect node deletion after crash")
	}
}
