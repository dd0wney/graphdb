package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestGraphStorage_CreateNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a node
	node, err := gs.CreateNode(
		[]string{"User", "Verified"},
		map[string]Value{
			"id":         StringValue("user123"),
			"trustScore": IntValue(750),
			"active":     BoolValue(true),
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
	idVal, ok := node.GetProperty("id")
	if !ok {
		t.Fatal("Property 'id' not found")
	}

	id, err := idVal.AsString()
	if err != nil {
		t.Fatalf("Failed to decode id: %v", err)
	}

	if id != "user123" {
		t.Errorf("Expected id 'user123', got '%s'", id)
	}

	trustScore, ok := node.GetProperty("trustScore")
	if !ok {
		t.Fatal("Property 'trustScore' not found")
	}

	score, err := trustScore.AsInt()
	if err != nil {
		t.Fatalf("Failed to decode trustScore: %v", err)
	}

	if score != 750 {
		t.Errorf("Expected trustScore 750, got %d", score)
	}
}

func TestGraphStorage_CreateEdge(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create two nodes
	node1, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user1")},
	)

	node2, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user2")},
	)

	// Create an edge between them
	edge, err := gs.CreateEdge(
		node1.ID,
		node2.ID,
		"VERIFIED_BY",
		map[string]Value{
			"timestamp": TimestampValue(time.Now()),
			"confidence": FloatValue(0.95),
		},
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

	if edge.Type != "VERIFIED_BY" {
		t.Errorf("Expected edge type 'VERIFIED_BY', got '%s'", edge.Type)
	}

	// Verify edge can be retrieved
	retrievedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to get edge: %v", err)
	}

	if retrievedEdge.ID != edge.ID {
		t.Errorf("Retrieved edge ID mismatch")
	}
}

func TestGraphStorage_GetOutgoingEdges(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node3, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	// Create edges from node1 to node2 and node3
	gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "VERIFIED_BY", map[string]Value{}, 1.0)

	// Get outgoing edges
	edges, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get outgoing edges: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("Expected 2 outgoing edges, got %d", len(edges))
	}
}

func TestGraphStorage_FindNodesByLabel(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with different labels
	gs.CreateNode([]string{"User"}, map[string]Value{})
	gs.CreateNode([]string{"User", "Verified"}, map[string]Value{})
	gs.CreateNode([]string{"Book"}, map[string]Value{})

	// Find User nodes
	userNodes, err := gs.FindNodesByLabel("User")
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(userNodes) != 2 {
		t.Errorf("Expected 2 User nodes, got %d", len(userNodes))
	}

	// Find Book nodes
	bookNodes, err := gs.FindNodesByLabel("Book")
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(bookNodes) != 1 {
		t.Errorf("Expected 1 Book node, got %d", len(bookNodes))
	}
}

func TestGraphStorage_DeleteNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes and edges
	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)

	// Delete node1
	err = gs.DeleteNode(node1.ID)
	if err != nil {
		t.Fatalf("Failed to delete node: %v", err)
	}

	// Verify node is gone
	_, err = gs.GetNode(node1.ID)
	if err != ErrNodeNotFound {
		t.Error("Node should not exist after deletion")
	}

	// Verify statistics updated
	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", stats.NodeCount)
	}
}

func TestGraphStorage_Snapshot(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create some data
	node1, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user1")},
	)
	node2, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user2")},
	)
	gs.CreateEdge(node1.ID, node2.ID, "VERIFIED_BY", map[string]Value{}, 1.0)

	// Save snapshot
	err = gs.Snapshot()
	if err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Close storage
	gs.Close()

	// Load from disk
	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to load from disk: %v", err)
	}
	defer gs2.Close()

	// Verify data was restored
	stats := gs2.GetStatistics()
	if stats.NodeCount != 2 {
		t.Errorf("Expected 2 nodes after reload, got %d", stats.NodeCount)
	}

	if stats.EdgeCount != 1 {
		t.Errorf("Expected 1 edge after reload, got %d", stats.EdgeCount)
	}

	// Verify node data
	reloadedNode, err := gs2.GetNode(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get node after reload: %v", err)
	}

	idVal, _ := reloadedNode.GetProperty("id")
	id, _ := idVal.AsString()
	if id != "user1" {
		t.Errorf("Expected id 'user1', got '%s'", id)
	}
}

func TestGraphStorage_FindNodesByProperty(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with different trust scores
	gs.CreateNode(
		[]string{"User"},
		map[string]Value{"trustScore": IntValue(750)},
	)
	gs.CreateNode(
		[]string{"User"},
		map[string]Value{"trustScore": IntValue(850)},
	)
	gs.CreateNode(
		[]string{"User"},
		map[string]Value{"trustScore": IntValue(750)},
	)

	// Find nodes with trustScore = 750
	nodes, err := gs.FindNodesByProperty("trustScore", IntValue(750))
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes with trustScore 750, got %d", len(nodes))
	}
}

func BenchmarkGraphStorage_CreateNode(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.CreateNode(
			[]string{"User"},
			map[string]Value{"id": StringValue("user")},
		)
	}
}

func BenchmarkGraphStorage_CreateEdge(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-create nodes
	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)
	}
}

func BenchmarkGraphStorage_GetNode(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetNode(node.ID)
	}
}

func BenchmarkGraphStorage_GetOutgoingEdges(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	// Create 10 outgoing edges
	for i := 0; i < 10; i++ {
		node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
		gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetOutgoingEdges(node1.ID)
	}
}

// BenchmarkGraphStorage_Snapshot benchmarks creating snapshots
func BenchmarkGraphStorage_Snapshot(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-populate with 1000 nodes and 3000 edges
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		node, _ := gs.CreateNode([]string{"User"}, map[string]Value{
			"id": StringValue(fmt.Sprintf("user%d", i)),
		})
		nodeIDs[i] = node.ID
	}

	for i := 0; i < 3000; i++ {
		from := nodeIDs[i%1000]
		to := nodeIDs[(i+1)%1000]
		gs.CreateEdge(from, to, "FOLLOWS", map[string]Value{}, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.Snapshot()
	}
}

// BenchmarkGraphStorage_FindNodesByLabel benchmarks label-based lookups
func BenchmarkGraphStorage_FindNodesByLabel(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-populate with mixed labels
	for i := 0; i < 1000; i++ {
		labels := []string{"User"}
		if i%2 == 0 {
			labels = append(labels, "Verified")
		}
		gs.CreateNode(labels, map[string]Value{})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.FindNodesByLabel("Verified")
	}
}

// BenchmarkGraphStorage_FindNodesByProperty benchmarks property-based lookups
func BenchmarkGraphStorage_FindNodesByProperty(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-populate with properties
	for i := 0; i < 1000; i++ {
		gs.CreateNode([]string{"User"}, map[string]Value{
			"trustScore": IntValue(int64(i % 100)),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.FindNodesByProperty("trustScore", IntValue(50))
	}
}

// BenchmarkGraphStorage_DeleteNode benchmarks node deletion
func BenchmarkGraphStorage_DeleteNode(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-create nodes for deletion
	nodeIDs := make([]uint64, b.N)
	for i := 0; i < b.N; i++ {
		node, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
		nodeIDs[i] = node.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.DeleteNode(nodeIDs[i])
	}
}

// BenchmarkGraphStorage_GetIncomingEdges benchmarks getting incoming edges
func BenchmarkGraphStorage_GetIncomingEdges(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Create target node
	target, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	// Create 10 nodes pointing to target
	for i := 0; i < 10; i++ {
		source, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
		gs.CreateEdge(source.ID, target.ID, "FOLLOWS", map[string]Value{}, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetIncomingEdges(target.ID)
	}
}
