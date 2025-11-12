package storage

import (
	"testing"
)

// TestNewLSMGraphStorage tests creating a new LSM-backed graph storage
func TestNewLSMGraphStorage(t *testing.T) {
	dataDir := t.TempDir()

	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create LSM graph storage: %v", err)
	}
	defer gs.Close()

	if gs == nil {
		t.Fatal("Expected non-nil storage")
	}

	if gs.nextNodeID != 1 {
		t.Errorf("Expected nextNodeID=1, got %d", gs.nextNodeID)
	}

	if gs.nextEdgeID != 1 {
		t.Errorf("Expected nextEdgeID=1, got %d", gs.nextEdgeID)
	}
}

// TestLSMGraphStorage_CreateNode tests creating nodes
func TestLSMGraphStorage_CreateNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a node
	node, err := gs.CreateNode(
		[]string{"User", "Verified"},
		map[string]Value{
			"name": StringValue("Alice"),
			"age":  IntValue(25),
		},
	)

	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	if node.ID != 1 {
		t.Errorf("Expected node ID 1, got %d", node.ID)
	}

	if len(node.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(node.Labels))
	}

	if !node.HasLabel("User") {
		t.Error("Node should have 'User' label")
	}

	// Verify property values
	name, ok := node.GetProperty("name")
	if !ok {
		t.Fatal("Property 'name' not found")
	}

	nameStr, _ := name.AsString()
	if nameStr != "Alice" {
		t.Errorf("Expected name 'Alice', got '%s'", nameStr)
	}
}

// TestLSMGraphStorage_GetNode tests retrieving nodes
func TestLSMGraphStorage_GetNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a node
	node1, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]Value{"name": StringValue("Bob")},
	)

	// Retrieve it
	node2, err := gs.GetNode(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	if node2.ID != node1.ID {
		t.Errorf("Expected ID %d, got %d", node1.ID, node2.ID)
	}

	name, _ := node2.GetProperty("name")
	nameStr, _ := name.AsString()
	if nameStr != "Bob" {
		t.Errorf("Expected name 'Bob', got '%s'", nameStr)
	}
}

// TestLSMGraphStorage_GetNode_NotFound tests getting non-existent node
func TestLSMGraphStorage_GetNode_NotFound(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	_, err = gs.GetNode(999)
	if err != ErrNodeNotFound {
		t.Errorf("Expected ErrNodeNotFound, got %v", err)
	}
}

// TestLSMGraphStorage_UpdateNode tests updating node properties
func TestLSMGraphStorage_UpdateNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a node
	node, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]Value{"name": StringValue("Alice"), "age": IntValue(25)},
	)

	// Update it
	err = gs.UpdateNode(node.ID, map[string]Value{
		"age": IntValue(26),
	})

	if err != nil {
		t.Fatalf("Failed to update node: %v", err)
	}

	// Verify update
	updated, _ := gs.GetNode(node.ID)
	age, _ := updated.GetProperty("age")
	ageVal, _ := age.AsInt()
	if ageVal != 26 {
		t.Errorf("Expected age 26, got %d", ageVal)
	}
}

// TestLSMGraphStorage_DeleteNode tests deleting nodes
func TestLSMGraphStorage_DeleteNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a node
	node, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]Value{"name": StringValue("Alice")},
	)

	// Delete it
	err = gs.DeleteNode(node.ID)
	if err != nil {
		t.Fatalf("Failed to delete node: %v", err)
	}

	// Verify it's gone
	_, err = gs.GetNode(node.ID)
	if err != ErrNodeNotFound {
		t.Error("Node should not exist after deletion")
	}
}

// TestLSMGraphStorage_CreateEdge tests creating edges
func TestLSMGraphStorage_CreateEdge(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create two nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})

	// Create an edge
	edge, err := gs.CreateEdge(
		node1.ID,
		node2.ID,
		"KNOWS",
		map[string]Value{"since": IntValue(2023)},
		1.0,
	)

	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	if edge.FromNodeID != node1.ID {
		t.Errorf("Expected FromNodeID %d, got %d", node1.ID, edge.FromNodeID)
	}

	if edge.ToNodeID != node2.ID {
		t.Errorf("Expected ToNodeID %d, got %d", node2.ID, edge.ToNodeID)
	}

	if edge.Type != "KNOWS" {
		t.Errorf("Expected type 'KNOWS', got '%s'", edge.Type)
	}
}

// TestLSMGraphStorage_GetEdge tests retrieving edges
func TestLSMGraphStorage_GetEdge(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{}, 1.0)

	// Retrieve edge
	edge2, err := gs.GetEdge(edge1.ID)
	if err != nil {
		t.Fatalf("Failed to get edge: %v", err)
	}

	if edge2.ID != edge1.ID {
		t.Errorf("Expected ID %d, got %d", edge1.ID, edge2.ID)
	}
}

// TestLSMGraphStorage_DeleteEdge tests deleting edges
func TestLSMGraphStorage_DeleteEdge(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{}, 1.0)

	// Delete edge
	err = gs.DeleteEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to delete edge: %v", err)
	}

	// Verify it's gone
	_, err = gs.GetEdge(edge.ID)
	if err != ErrEdgeNotFound {
		t.Error("Edge should not exist after deletion")
	}
}

// TestLSMGraphStorage_GetOutgoingEdges tests getting outgoing edges
func TestLSMGraphStorage_GetOutgoingEdges(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})

	// Create edges from node1
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{}, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "FOLLOWS", map[string]Value{}, 1.0)

	// Get outgoing edges
	edges, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get outgoing edges: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("Expected 2 outgoing edges, got %d", len(edges))
	}
}

// TestLSMGraphStorage_GetIncomingEdges tests getting incoming edges
func TestLSMGraphStorage_GetIncomingEdges(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})

	// Create edges TO node1
	gs.CreateEdge(node2.ID, node1.ID, "KNOWS", map[string]Value{}, 1.0)
	gs.CreateEdge(node3.ID, node1.ID, "FOLLOWS", map[string]Value{}, 1.0)

	// Get incoming edges
	edges, err := gs.GetIncomingEdges(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get incoming edges: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("Expected 2 incoming edges, got %d", len(edges))
	}
}

// TestLSMGraphStorage_FindNodesByLabel tests finding nodes by label
func TestLSMGraphStorage_FindNodesByLabel(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with different labels
	gs.CreateNode([]string{"User"}, map[string]Value{})
	gs.CreateNode([]string{"User", "Verified"}, map[string]Value{})
	gs.CreateNode([]string{"Admin"}, map[string]Value{})

	// Find User nodes
	users, err := gs.FindNodesByLabel("User")
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(users) != 2 {
		t.Errorf("Expected 2 User nodes, got %d", len(users))
	}
}

// TestLSMGraphStorage_Persistence tests data persists across restarts
func TestLSMGraphStorage_Persistence(t *testing.T) {
	dataDir := t.TempDir()

	var nodeID uint64

	// Create storage and add data
	{
		gs, err := NewLSMGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		node, _ := gs.CreateNode(
			[]string{"Person"},
			map[string]Value{"name": StringValue("Alice")},
		)
		nodeID = node.ID

		gs.Close()
	}

	// Reopen storage and verify data persists
	{
		gs, err := NewLSMGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to reopen storage: %v", err)
		}
		defer gs.Close()

		node, err := gs.GetNode(nodeID)
		if err != nil {
			t.Fatalf("Failed to get node after restart: %v", err)
		}

		name, _ := node.GetProperty("name")
		nameStr, _ := name.AsString()
		if nameStr != "Alice" {
			t.Errorf("Expected name 'Alice' after restart, got '%s'", nameStr)
		}

		// Verify counters persisted
		if gs.nextNodeID <= nodeID {
			t.Errorf("Counter not properly restored: nextNodeID=%d, nodeID=%d", gs.nextNodeID, nodeID)
		}
	}
}

// TestLSMGraphStorage_GetStatistics tests getting statistics
func TestLSMGraphStorage_GetStatistics(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewLSMGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create some data
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{})
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{}, 1.0)

	stats := gs.GetStatistics()

	if stats.NodeCount != 2 {
		t.Errorf("Expected 2 nodes, got %d", stats.NodeCount)
	}

	if stats.EdgeCount != 1 {
		t.Errorf("Expected 1 edge, got %d", stats.EdgeCount)
	}
}
