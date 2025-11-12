package storage

import (
	"testing"
)

// Test edge creation and retrieval with properties
func TestEdgeWithProperties(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Bob"),
	})

	edge, err := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
		"since": IntValue(2020),
		"trust": FloatValue(0.9),
	}, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	// Verify edge fields
	if edge.FromNodeID != node1.ID {
		t.Errorf("FromNodeID mismatch: expected %d, got %d", node1.ID, edge.FromNodeID)
	}
	if edge.ToNodeID != node2.ID {
		t.Errorf("ToNodeID mismatch: expected %d, got %d", node2.ID, edge.ToNodeID)
	}
	if edge.Type != "KNOWS" {
		t.Errorf("Type mismatch: expected KNOWS, got %s", edge.Type)
	}

	// Verify properties
	sinceVal, ok := edge.Properties["since"]
	if !ok {
		t.Error("Property 'since' not found")
	}
	since, _ := sinceVal.AsInt()
	if since != 2020 {
		t.Errorf("Property 'since': expected 2020, got %d", since)
	}
}

// Test retrieving outgoing and incoming edges
func TestGetEdgeDirections(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)
	node3, _ := gs.CreateNode([]string{"Person"}, nil)

	// Create edges
	edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	edge2, _ := gs.CreateEdge(node1.ID, node3.ID, "LIKES", nil, 1.0)
	edge3, _ := gs.CreateEdge(node2.ID, node3.ID, "FOLLOWS", nil, 1.0)

	// Test outgoing edges from node1
	outgoing, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}
	if len(outgoing) != 2 {
		t.Errorf("Expected 2 outgoing edges from node1, got %d", len(outgoing))
	}

	// Test incoming edges to node3
	incoming, err := gs.GetIncomingEdges(node3.ID)
	if err != nil {
		t.Fatalf("GetIncomingEdges failed: %v", err)
	}
	if len(incoming) != 2 {
		t.Errorf("Expected 2 incoming edges to node3, got %d", len(incoming))
	}

	// Verify edge IDs
	foundEdge2 := false
	foundEdge3 := false
	for _, e := range incoming {
		if e.ID == edge2.ID {
			foundEdge2 = true
		}
		if e.ID == edge3.ID {
			foundEdge3 = true
		}
	}
	if !foundEdge2 || !foundEdge3 {
		t.Error("Not all expected incoming edges found")
	}

	// Test node with no edges
	node4, _ := gs.CreateNode([]string{"Person"}, nil)
	emptyOut, _ := gs.GetOutgoingEdges(node4.ID)
	emptyIn, _ := gs.GetIncomingEdges(node4.ID)
	if len(emptyOut) != 0 || len(emptyIn) != 0 {
		t.Error("Node with no edges should return empty slices")
	}

	// Test retrieving single edge
	retrieved, err := gs.GetEdge(edge1.ID)
	if err != nil {
		t.Fatalf("GetEdge failed: %v", err)
	}
	if retrieved.ID != edge1.ID || retrieved.Type != "KNOWS" {
		t.Error("Retrieved edge doesn't match")
	}
}

// Test finding edges by type
func TestFindEdgesByType(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)
	node3, _ := gs.CreateNode([]string{"Person"}, nil)

	// Create edges with different types
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	_, _ = gs.CreateEdge(node2.ID, node3.ID, "KNOWS", nil, 1.0)
	likesEdge, _ := gs.CreateEdge(node1.ID, node3.ID, "LIKES", nil, 1.0)

	// Find KNOWS edges
	knowsEdges, err := gs.FindEdgesByType("KNOWS")
	if err != nil {
		t.Fatalf("FindEdgesByType failed: %v", err)
	}
	if len(knowsEdges) != 2 {
		t.Errorf("Expected 2 KNOWS edges, got %d", len(knowsEdges))
	}

	// Find LIKES edges
	likesEdges, _ := gs.FindEdgesByType("LIKES")
	if len(likesEdges) != 1 {
		t.Errorf("Expected 1 LIKES edge, got %d", len(likesEdges))
	}
	if likesEdges[0].ID != likesEdge.ID {
		t.Error("Found wrong LIKES edge")
	}

	// Find non-existent type
	noEdges, _ := gs.FindEdgesByType("NONEXISTENT")
	if len(noEdges) != 0 {
		t.Errorf("Expected 0 NONEXISTENT edges, got %d", len(noEdges))
	}
}

// Test error cases
func TestEdgeErrors(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	// Test creating edge with invalid source node
	_, err := gs.CreateEdge(99999, 88888, "KNOWS", nil, 1.0)
	if err == nil {
		t.Error("Expected error creating edge with invalid nodes")
	}

	// Test creating edge with one valid, one invalid node
	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	_, err = gs.CreateEdge(node1.ID, 99999, "KNOWS", nil, 1.0)
	if err == nil {
		t.Error("Expected error creating edge with invalid target node")
	}

	// Test getting non-existent edge
	_, err = gs.GetEdge(99999)
	if err == nil {
		t.Error("Expected error getting non-existent edge")
	}
}

// Helper function
func setupTestStorage(t *testing.T) *GraphStorage {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	return gs
}
