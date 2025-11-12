package algorithms

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupTestGraph creates a test graph for shortest path tests
func setupTestGraph(t *testing.T) *storage.GraphStorage {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	return gs
}

// TestShortestPath_SameNode tests path from node to itself
func TestShortestPath_SameNode(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"Test"}, nil)

	path, err := ShortestPath(gs, node.ID, node.ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if len(path) != 1 {
		t.Errorf("Expected path length 1, got %d", len(path))
	}
	if path[0] != node.ID {
		t.Errorf("Expected path [%d], got %v", node.ID, path)
	}
}

// TestShortestPath_DirectConnection tests a simple A->B path
func TestShortestPath_DirectConnection(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "CONNECTS", nil, 1.0)

	path, err := ShortestPath(gs, nodeA.ID, nodeB.ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if len(path) != 2 {
		t.Errorf("Expected path length 2, got %d", len(path))
	}
	if path[0] != nodeA.ID || path[1] != nodeB.ID {
		t.Errorf("Expected path [%d, %d], got %v", nodeA.ID, nodeB.ID, path)
	}
}

// TestShortestPath_LinearPath tests A->B->C path
func TestShortestPath_LinearPath(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "CONNECTS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "CONNECTS", nil, 1.0)

	path, err := ShortestPath(gs, nodeA.ID, nodeC.ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d", len(path))
	}
	if path[0] != nodeA.ID || path[1] != nodeB.ID || path[2] != nodeC.ID {
		t.Errorf("Expected path [%d, %d, %d], got %v", nodeA.ID, nodeB.ID, nodeC.ID, path)
	}
}

// TestShortestPath_MultiplePaths tests finding shortest among multiple paths
func TestShortestPath_MultiplePaths(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create graph:
	//   A -> B -> D
	//   A -> C -> D
	// Both paths have length 2, either is valid
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "CONNECTS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeD.ID, "CONNECTS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "CONNECTS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "CONNECTS", nil, 1.0)

	path, err := ShortestPath(gs, nodeA.ID, nodeD.ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d", len(path))
	}
	if path[0] != nodeA.ID || path[2] != nodeD.ID {
		t.Errorf("Expected path starting with %d and ending with %d, got %v", nodeA.ID, nodeD.ID, path)
	}
	// Middle node should be either B or C
	if path[1] != nodeB.ID && path[1] != nodeC.ID {
		t.Errorf("Expected middle node to be %d or %d, got %d", nodeB.ID, nodeC.ID, path[1])
	}
}

// TestShortestPath_NoPath tests when no path exists
func TestShortestPath_NoPath(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	// No edge between A and B

	path, err := ShortestPath(gs, nodeA.ID, nodeB.ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if path != nil {
		t.Errorf("Expected no path (nil), got %v", path)
	}
}

// TestShortestPath_ComplexGraph tests a more complex graph
func TestShortestPath_ComplexGraph(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create graph:
	//     1 -> 2 -> 4
	//     |    |    ^
	//     v    v    |
	//     3 -------> 5
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	node3, _ := gs.CreateNode([]string{"Node"}, nil)
	node4, _ := gs.CreateNode([]string{"Node"}, nil)
	node5, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(node1.ID, node2.ID, "E", nil, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "E", nil, 1.0)
	gs.CreateEdge(node2.ID, node4.ID, "E", nil, 1.0)
	gs.CreateEdge(node2.ID, node5.ID, "E", nil, 1.0)
	gs.CreateEdge(node3.ID, node5.ID, "E", nil, 1.0)
	gs.CreateEdge(node5.ID, node4.ID, "E", nil, 1.0)

	// Shortest path from 1 to 4: 1 -> 2 -> 4 (length 3)
	path, err := ShortestPath(gs, node1.ID, node4.ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d: %v", len(path), path)
	}
	if path[0] != node1.ID {
		t.Errorf("Expected path to start with %d, got %d", node1.ID, path[0])
	}
	if path[len(path)-1] != node4.ID {
		t.Errorf("Expected path to end with %d, got %d", node4.ID, path[len(path)-1])
	}
}

// TestAllShortestPaths_SingleSource tests BFS from single source
func TestAllShortestPaths_SingleSource(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create graph: A -> B -> C
	//                A -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeD.ID, "E", nil, 1.0)

	distances, err := AllShortestPaths(gs, nodeA.ID)
	if err != nil {
		t.Fatalf("AllShortestPaths failed: %v", err)
	}

	// Check distances
	if distances[nodeA.ID] != 0 {
		t.Errorf("Expected distance to A = 0, got %d", distances[nodeA.ID])
	}
	if distances[nodeB.ID] != 1 {
		t.Errorf("Expected distance to B = 1, got %d", distances[nodeB.ID])
	}
	if distances[nodeC.ID] != 2 {
		t.Errorf("Expected distance to C = 2, got %d", distances[nodeC.ID])
	}
	if distances[nodeD.ID] != 1 {
		t.Errorf("Expected distance to D = 1, got %d", distances[nodeD.ID])
	}
}

// TestAllShortestPaths_DisconnectedNodes tests handling disconnected nodes
func TestAllShortestPaths_DisconnectedNodes(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	// nodeC is disconnected

	distances, err := AllShortestPaths(gs, nodeA.ID)
	if err != nil {
		t.Fatalf("AllShortestPaths failed: %v", err)
	}

	// C should not be in distances
	if _, exists := distances[nodeC.ID]; exists {
		t.Error("Disconnected node C should not be in distances map")
	}

	// A and B should be reachable
	if distances[nodeA.ID] != 0 {
		t.Errorf("Expected distance to A = 0, got %d", distances[nodeA.ID])
	}
	if distances[nodeB.ID] != 1 {
		t.Errorf("Expected distance to B = 1, got %d", distances[nodeB.ID])
	}
}

// TestWeightedShortestPath_SimpleCase tests Dijkstra on simple weighted graph
func TestWeightedShortestPath_SimpleCase(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	// A -> B (weight 5)
	// A -> C -> B (weights 2 + 1 = 3) - shorter weighted path
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 5.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "E", nil, 2.0)
	gs.CreateEdge(nodeC.ID, nodeB.ID, "E", nil, 1.0)

	path, distance, err := WeightedShortestPath(gs, nodeA.ID, nodeB.ID)
	if err != nil {
		t.Fatalf("WeightedShortestPath failed: %v", err)
	}

	if distance != 3.0 {
		t.Errorf("Expected distance 3.0, got %.1f", distance)
	}

	// Path should be A -> C -> B
	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d", len(path))
	}
	if path[0] != nodeA.ID || path[1] != nodeC.ID || path[2] != nodeB.ID {
		t.Errorf("Expected path [%d, %d, %d], got %v", nodeA.ID, nodeC.ID, nodeB.ID, path)
	}
}

// TestWeightedShortestPath_NoPath tests when no weighted path exists
func TestWeightedShortestPath_NoPath(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)

	path, distance, err := WeightedShortestPath(gs, nodeA.ID, nodeB.ID)
	if err != nil {
		t.Fatalf("WeightedShortestPath failed: %v", err)
	}

	if path != nil {
		t.Errorf("Expected no path (nil), got %v", path)
	}
	if distance != 0 {
		t.Errorf("Expected distance 0 for no path, got %.1f", distance)
	}
}

// TestWeightedShortestPath_ComplexWeights tests complex weighted graph
func TestWeightedShortestPath_ComplexWeights(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create graph with multiple weighted paths
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	node3, _ := gs.CreateNode([]string{"Node"}, nil)
	node4, _ := gs.CreateNode([]string{"Node"}, nil)

	// 1 -> 2 -> 4 (weights 1 + 10 = 11)
	// 1 -> 3 -> 4 (weights 5 + 2 = 7) - shorter
	gs.CreateEdge(node1.ID, node2.ID, "E", nil, 1.0)
	gs.CreateEdge(node2.ID, node4.ID, "E", nil, 10.0)
	gs.CreateEdge(node1.ID, node3.ID, "E", nil, 5.0)
	gs.CreateEdge(node3.ID, node4.ID, "E", nil, 2.0)

	path, distance, err := WeightedShortestPath(gs, node1.ID, node4.ID)
	if err != nil {
		t.Fatalf("WeightedShortestPath failed: %v", err)
	}

	if distance != 7.0 {
		t.Errorf("Expected distance 7.0, got %.1f", distance)
	}

	// Path should be 1 -> 3 -> 4
	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d", len(path))
	}
	if path[0] != node1.ID || path[1] != node3.ID || path[2] != node4.ID {
		t.Errorf("Expected path [%d, %d, %d], got %v", node1.ID, node3.ID, node4.ID, path)
	}
}

// TestWeightedShortestPath_SameNode tests weighted path from node to itself
func TestWeightedShortestPath_SameNode(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	path, distance, err := WeightedShortestPath(gs, node.ID, node.ID)
	if err != nil {
		t.Fatalf("WeightedShortestPath failed: %v", err)
	}

	if distance != 0.0 {
		t.Errorf("Expected distance 0.0 for same node, got %.1f", distance)
	}
	if len(path) != 1 {
		t.Errorf("Expected path length 1, got %d", len(path))
	}
	if path[0] != node.ID {
		t.Errorf("Expected path [%d], got %v", node.ID, path)
	}
}

// TestShortestPath_BidirectionalEfficiency tests that bidirectional search works
func TestShortestPath_BidirectionalEfficiency(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create a long chain to test bidirectional search meets in middle
	nodes := make([]*storage.Node, 10)
	for i := 0; i < 10; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
		if i > 0 {
			gs.CreateEdge(nodes[i-1].ID, nodes[i].ID, "E", nil, 1.0)
		}
	}

	path, err := ShortestPath(gs, nodes[0].ID, nodes[9].ID)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	if len(path) != 10 {
		t.Errorf("Expected path length 10, got %d", len(path))
	}
	if path[0] != nodes[0].ID {
		t.Error("Path should start at first node")
	}
	if path[9] != nodes[9].ID {
		t.Error("Path should end at last node")
	}
}

// TestAllShortestPaths_EmptyGraph tests handling of isolated node
func TestAllShortestPaths_EmptyGraph(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	distances, err := AllShortestPaths(gs, node.ID)
	if err != nil {
		t.Fatalf("AllShortestPaths failed: %v", err)
	}

	// Should only contain the source node
	if len(distances) != 1 {
		t.Errorf("Expected 1 entry in distances, got %d", len(distances))
	}
	if distances[node.ID] != 0 {
		t.Errorf("Expected distance 0 to self, got %d", distances[node.ID])
	}
}
