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

// Test FindEdgeBetween
func TestFindEdgeBetween(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Alice")})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Bob")})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("Charlie")})

	// Create some edges
	edge1, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{"since": IntValue(2020)}, 1.0)
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "LIKES", nil, 0.5) // Different type between same nodes
	_, _ = gs.CreateEdge(node1.ID, node3.ID, "KNOWS", nil, 1.0)

	// Find existing edge
	found, err := gs.FindEdgeBetween(node1.ID, node2.ID, "KNOWS")
	if err != nil {
		t.Fatalf("FindEdgeBetween failed: %v", err)
	}
	if found == nil {
		t.Fatal("Expected to find edge, got nil")
	}
	if found.ID != edge1.ID {
		t.Errorf("Expected edge ID %d, got %d", edge1.ID, found.ID)
	}
	since, _ := found.Properties["since"].AsInt()
	if since != 2020 {
		t.Errorf("Expected since=2020, got %d", since)
	}

	// Find edge with different type
	found, err = gs.FindEdgeBetween(node1.ID, node2.ID, "LIKES")
	if err != nil {
		t.Fatalf("FindEdgeBetween failed: %v", err)
	}
	if found == nil {
		t.Fatal("Expected to find LIKES edge")
	}
	if found.Type != "LIKES" {
		t.Errorf("Expected type LIKES, got %s", found.Type)
	}

	// Find non-existent edge (wrong type)
	found, err = gs.FindEdgeBetween(node1.ID, node2.ID, "FOLLOWS")
	if err != nil {
		t.Fatalf("FindEdgeBetween failed: %v", err)
	}
	if found != nil {
		t.Error("Expected nil for non-existent edge type")
	}

	// Find non-existent edge (wrong direction)
	found, err = gs.FindEdgeBetween(node2.ID, node1.ID, "KNOWS")
	if err != nil {
		t.Fatalf("FindEdgeBetween failed: %v", err)
	}
	if found != nil {
		t.Error("Expected nil for reversed direction")
	}

	// Find edge between nodes with no relationship
	found, err = gs.FindEdgeBetween(node2.ID, node3.ID, "KNOWS")
	if err != nil {
		t.Fatalf("FindEdgeBetween failed: %v", err)
	}
	if found != nil {
		t.Error("Expected nil for unconnected nodes")
	}
}

// Test FindAllEdgesBetween
func TestFindAllEdgesBetween(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)

	// Create multiple edges between same nodes
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "LIKES", nil, 0.5)
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", nil, 0.8)

	// Find all edges
	edges, err := gs.FindAllEdgesBetween(node1.ID, node2.ID)
	if err != nil {
		t.Fatalf("FindAllEdgesBetween failed: %v", err)
	}
	if len(edges) != 3 {
		t.Errorf("Expected 3 edges, got %d", len(edges))
	}

	// Verify all types present
	types := make(map[string]bool)
	for _, e := range edges {
		types[e.Type] = true
	}
	if !types["KNOWS"] || !types["LIKES"] || !types["FOLLOWS"] {
		t.Error("Missing expected edge types")
	}

	// Check reverse direction (should be empty)
	edges, _ = gs.FindAllEdgesBetween(node2.ID, node1.ID)
	if len(edges) != 0 {
		t.Errorf("Expected 0 edges in reverse direction, got %d", len(edges))
	}
}

// Test UpsertEdge - create new edge
func TestUpsertEdge_Create(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"User"}, nil)
	node2, _ := gs.CreateNode([]string{"Concept"}, nil)

	// Upsert should create new edge
	edge, created, err := gs.UpsertEdge(node1.ID, node2.ID, "MASTERY",
		map[string]Value{
			"status": StringValue("studying"),
			"score":  IntValue(75),
		}, 0.75)

	if err != nil {
		t.Fatalf("UpsertEdge failed: %v", err)
	}
	if !created {
		t.Error("Expected created=true for new edge")
	}
	if edge.Type != "MASTERY" {
		t.Errorf("Expected type MASTERY, got %s", edge.Type)
	}
	status, _ := edge.Properties["status"].AsString()
	if status != "studying" {
		t.Errorf("Expected status=studying, got %s", status)
	}

	// Verify it exists
	found, _ := gs.FindEdgeBetween(node1.ID, node2.ID, "MASTERY")
	if found == nil {
		t.Fatal("Edge not found after upsert")
	}
}

// Test UpsertEdge - update existing edge
func TestUpsertEdge_Update(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"User"}, nil)
	node2, _ := gs.CreateNode([]string{"Concept"}, nil)

	// Create initial edge
	_, created1, _ := gs.UpsertEdge(node1.ID, node2.ID, "MASTERY",
		map[string]Value{
			"status":      StringValue("studying"),
			"score":       IntValue(50),
			"verifyCount": IntValue(1),
		}, 0.5)

	if !created1 {
		t.Error("First upsert should create")
	}

	// Upsert again with updated values
	edge, created2, err := gs.UpsertEdge(node1.ID, node2.ID, "MASTERY",
		map[string]Value{
			"status":      StringValue("verified"),
			"score":       IntValue(95),
			"verifyCount": IntValue(2),
		}, 0.95)

	if err != nil {
		t.Fatalf("Second UpsertEdge failed: %v", err)
	}
	if created2 {
		t.Error("Expected created=false for existing edge update")
	}

	// Verify updated values
	status, _ := edge.Properties["status"].AsString()
	if status != "verified" {
		t.Errorf("Expected status=verified, got %s", status)
	}
	score, _ := edge.Properties["score"].AsInt()
	if score != 95 {
		t.Errorf("Expected score=95, got %d", score)
	}
	if edge.Weight != 0.95 {
		t.Errorf("Expected weight=0.95, got %f", edge.Weight)
	}

	// Verify only one edge exists
	edges, _ := gs.FindAllEdgesBetween(node1.ID, node2.ID)
	if len(edges) != 1 {
		t.Errorf("Expected exactly 1 edge after upsert, got %d", len(edges))
	}
}

// Test UpsertEdge preserves unmodified properties
func TestUpsertEdge_MergesProperties(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"User"}, nil)
	node2, _ := gs.CreateNode([]string{"Concept"}, nil)

	// Create with initial properties
	_, _, _ = gs.UpsertEdge(node1.ID, node2.ID, "MASTERY",
		map[string]Value{
			"createdAt": IntValue(1000),
			"score":     IntValue(50),
		}, 0.5)

	// Update with only some properties
	edge, _, _ := gs.UpsertEdge(node1.ID, node2.ID, "MASTERY",
		map[string]Value{
			"score":     IntValue(90),
			"updatedAt": IntValue(2000),
		}, 0.9)

	// Both old and new properties should exist
	createdAt, _ := edge.Properties["createdAt"].AsInt()
	if createdAt != 1000 {
		t.Errorf("Expected createdAt preserved as 1000, got %d", createdAt)
	}
	score, _ := edge.Properties["score"].AsInt()
	if score != 90 {
		t.Errorf("Expected score updated to 90, got %d", score)
	}
	updatedAt, _ := edge.Properties["updatedAt"].AsInt()
	if updatedAt != 2000 {
		t.Errorf("Expected updatedAt added as 2000, got %d", updatedAt)
	}
}

// Test UpsertEdge with different edge types between same nodes
func TestUpsertEdge_DifferentTypes(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"User"}, nil)
	node2, _ := gs.CreateNode([]string{"User"}, nil)

	// Create TAUGHT edge
	_, created1, _ := gs.UpsertEdge(node1.ID, node2.ID, "TAUGHT",
		map[string]Value{"rating": FloatValue(4.5)}, 4.5)

	// Create KNOWS edge (different type, same nodes)
	_, created2, _ := gs.UpsertEdge(node1.ID, node2.ID, "KNOWS",
		map[string]Value{"since": IntValue(2023)}, 1.0)

	if !created1 || !created2 {
		t.Error("Both should be creates for different types")
	}

	// Verify both exist
	allEdges, _ := gs.FindAllEdgesBetween(node1.ID, node2.ID)
	if len(allEdges) != 2 {
		t.Errorf("Expected 2 edges of different types, got %d", len(allEdges))
	}

	// Update TAUGHT edge
	_, created3, _ := gs.UpsertEdge(node1.ID, node2.ID, "TAUGHT",
		map[string]Value{"rating": FloatValue(5.0)}, 5.0)

	if created3 {
		t.Error("TAUGHT update should return created=false")
	}

	// Still only 2 edges
	allEdges, _ = gs.FindAllEdgesBetween(node1.ID, node2.ID)
	if len(allEdges) != 2 {
		t.Errorf("Still expected 2 edges, got %d", len(allEdges))
	}
}

// Test DeleteEdgeBetween
func TestDeleteEdgeBetween(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)

	// Create edges
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	_, _ = gs.CreateEdge(node1.ID, node2.ID, "LIKES", nil, 0.5)

	// Delete KNOWS edge
	deleted, err := gs.DeleteEdgeBetween(node1.ID, node2.ID, "KNOWS")
	if err != nil {
		t.Fatalf("DeleteEdgeBetween failed: %v", err)
	}
	if !deleted {
		t.Error("Expected deleted=true")
	}

	// Verify KNOWS is gone but LIKES remains
	knows, _ := gs.FindEdgeBetween(node1.ID, node2.ID, "KNOWS")
	if knows != nil {
		t.Error("KNOWS edge should be deleted")
	}
	likes, _ := gs.FindEdgeBetween(node1.ID, node2.ID, "LIKES")
	if likes == nil {
		t.Error("LIKES edge should still exist")
	}

	// Delete non-existent edge
	deleted, err = gs.DeleteEdgeBetween(node1.ID, node2.ID, "FOLLOWS")
	if err != nil {
		t.Fatalf("DeleteEdgeBetween failed: %v", err)
	}
	if deleted {
		t.Error("Expected deleted=false for non-existent edge")
	}
}

// Test UpsertEdge with invalid nodes
func TestUpsertEdge_InvalidNodes(t *testing.T) {
	gs := setupTestStorage(t)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"Person"}, nil)

	// Invalid source
	_, _, err := gs.UpsertEdge(99999, node1.ID, "KNOWS", nil, 1.0)
	if err == nil {
		t.Error("Expected error for invalid source node")
	}

	// Invalid target
	_, _, err = gs.UpsertEdge(node1.ID, 99999, "KNOWS", nil, 1.0)
	if err == nil {
		t.Error("Expected error for invalid target node")
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
