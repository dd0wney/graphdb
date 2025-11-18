package visualization

import (
	"math"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestForceDirectedLayout tests the force-directed layout algorithm
func TestForceDirectedLayout(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a simple graph
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
	})

	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{}, 1.0)
	gs.CreateEdge(node2.ID, node3.ID, "KNOWS", map[string]storage.Value{}, 1.0)

	// Apply force-directed layout
	layout := NewForceDirectedLayout(&LayoutConfig{
		Width:      800,
		Height:     600,
		Iterations: 50,
	})

	positions, err := layout.ComputeLayout(gs, []uint64{node1.ID, node2.ID, node3.ID})
	if err != nil {
		t.Fatalf("Layout computation failed: %v", err)
	}

	// Verify all nodes have positions
	if len(positions) != 3 {
		t.Errorf("Expected 3 positions, got %d", len(positions))
	}

	// Verify positions are within bounds
	for nodeID, pos := range positions {
		if pos.X < 0 || pos.X > 800 {
			t.Errorf("Node %d X position %f out of bounds", nodeID, pos.X)
		}
		if pos.Y < 0 || pos.Y > 600 {
			t.Errorf("Node %d Y position %f out of bounds", nodeID, pos.Y)
		}
	}

	// Connected nodes should be closer than unconnected ones
	dist12 := distance(positions[node1.ID], positions[node2.ID])
	dist23 := distance(positions[node2.ID], positions[node3.ID])
	dist13 := distance(positions[node1.ID], positions[node3.ID])

	// Node 1 and 3 are not directly connected, should be furthest apart
	if dist13 < dist12 || dist13 < dist23 {
		t.Error("Force-directed layout did not separate unconnected nodes properly")
	}
}

// TestCircularLayout tests circular layout algorithm
func TestCircularLayout(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	nodeIDs := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})
		nodeIDs[i] = node.ID
	}

	// Apply circular layout
	layout := NewCircularLayout(&LayoutConfig{
		Width:  400,
		Height: 400,
	})

	positions, err := layout.ComputeLayout(gs, nodeIDs)
	if err != nil {
		t.Fatalf("Layout computation failed: %v", err)
	}

	// Verify all nodes are roughly the same distance from center
	centerX, centerY := 200.0, 200.0
	distances := make([]float64, len(nodeIDs))

	for i, nodeID := range nodeIDs {
		pos := positions[nodeID]
		dx := pos.X - centerX
		dy := pos.Y - centerY
		distances[i] = math.Sqrt(dx*dx + dy*dy)
	}

	// All distances should be approximately equal (within 5% tolerance)
	avgDist := distances[0]
	for _, dist := range distances {
		ratio := dist / avgDist
		if ratio < 0.95 || ratio > 1.05 {
			t.Errorf("Circular layout not uniform: distance ratio %f", ratio)
		}
	}
}

// TestHierarchicalLayout tests hierarchical/tree layout
func TestHierarchicalLayout(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a tree structure
	root, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})
	child1, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})
	child2, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})
	grandchild1, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})
	grandchild2, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})

	gs.CreateEdge(root.ID, child1.ID, "CHILD", map[string]storage.Value{}, 1.0)
	gs.CreateEdge(root.ID, child2.ID, "CHILD", map[string]storage.Value{}, 1.0)
	gs.CreateEdge(child1.ID, grandchild1.ID, "CHILD", map[string]storage.Value{}, 1.0)
	gs.CreateEdge(child1.ID, grandchild2.ID, "CHILD", map[string]storage.Value{}, 1.0)

	// Apply hierarchical layout
	layout := NewHierarchicalLayout(&LayoutConfig{
		Width:  600,
		Height: 400,
	})

	positions, err := layout.ComputeLayout(gs, []uint64{
		root.ID, child1.ID, child2.ID, grandchild1.ID, grandchild2.ID,
	})
	if err != nil {
		t.Fatalf("Layout computation failed: %v", err)
	}

	// Verify root is at top (lowest Y value)
	rootY := positions[root.ID].Y
	for nodeID, pos := range positions {
		if nodeID != root.ID && pos.Y <= rootY {
			t.Errorf("Node %d has Y=%f, should be below root Y=%f", nodeID, pos.Y, rootY)
		}
	}

	// Children should be at same level
	child1Y := positions[child1.ID].Y
	child2Y := positions[child2.ID].Y
	if math.Abs(child1Y-child2Y) > 1.0 {
		t.Errorf("Children not at same level: Y1=%f, Y2=%f", child1Y, child2Y)
	}

	// Grandchildren should be at same level
	gc1Y := positions[grandchild1.ID].Y
	gc2Y := positions[grandchild2.ID].Y
	if math.Abs(gc1Y-gc2Y) > 1.0 {
		t.Errorf("Grandchildren not at same level: Y1=%f, Y2=%f", gc1Y, gc2Y)
	}
}

// TestLayoutNormalization tests that coordinates are normalized to bounds
func TestLayoutNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	nodeIDs := make([]uint64, 3)
	for i := 0; i < 3; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})
		nodeIDs[i] = node.ID
	}

	layout := NewForceDirectedLayout(&LayoutConfig{
		Width:      100,
		Height:     100,
		Iterations: 10,
	})

	positions, _ := layout.ComputeLayout(gs, nodeIDs)

	// All positions should be within bounds
	for nodeID, pos := range positions {
		if pos.X < 0 || pos.X > 100 {
			t.Errorf("Node %d X=%f out of bounds [0, 100]", nodeID, pos.X)
		}
		if pos.Y < 0 || pos.Y > 100 {
			t.Errorf("Node %d Y=%f out of bounds [0, 100]", nodeID, pos.Y)
		}
	}
}

// TestEmptyGraph tests layout on empty graph
func TestEmptyGraph(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	layout := NewForceDirectedLayout(&LayoutConfig{
		Width:  800,
		Height: 600,
	})

	positions, err := layout.ComputeLayout(gs, []uint64{})
	if err != nil {
		t.Fatalf("Empty graph should not error: %v", err)
	}

	if len(positions) != 0 {
		t.Errorf("Expected 0 positions for empty graph, got %d", len(positions))
	}
}

// TestSingleNodeLayout tests layout with single node
func TestSingleNodeLayout(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{})

	layout := NewForceDirectedLayout(&LayoutConfig{
		Width:  800,
		Height: 600,
	})

	positions, err := layout.ComputeLayout(gs, []uint64{node.ID})
	if err != nil {
		t.Fatalf("Single node layout failed: %v", err)
	}

	if len(positions) != 1 {
		t.Errorf("Expected 1 position, got %d", len(positions))
	}

	// Single node should be centered
	pos := positions[node.ID]
	centerX, centerY := 400.0, 300.0
	if math.Abs(pos.X-centerX) > 100 || math.Abs(pos.Y-centerY) > 100 {
		t.Errorf("Single node not centered: (%f, %f)", pos.X, pos.Y)
	}
}

// TestVisualizationExport tests exporting layout to various formats
func TestVisualizationExport(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create simple graph
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{}, 1.0)

	layout := NewForceDirectedLayout(&LayoutConfig{
		Width:      800,
		Height:     600,
		Iterations: 20,
	})

	positions, _ := layout.ComputeLayout(gs, []uint64{node1.ID, node2.ID})

	// Export to JSON
	viz := &Visualization{
		Nodes:     []*storage.Node{node1, node2},
		Positions: positions,
	}

	jsonData, err := viz.ExportJSON()
	if err != nil {
		t.Fatalf("JSON export failed: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("JSON export returned empty data")
	}

	// Verify JSON contains node data
	jsonStr := string(jsonData)
	if !contains(jsonStr, "Alice") || !contains(jsonStr, "Bob") {
		t.Error("JSON export missing node data")
	}
}

// Helper function to calculate distance between two positions
func distance(p1, p2 Position) float64 {
	dx := p1.X - p2.X
	dy := p1.Y - p2.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || contains(s[1:], substr)))
}
