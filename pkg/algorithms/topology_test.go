package algorithms

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestIsDAG_EmptyGraph tests DAG check on empty graph
func TestIsDAG_EmptyGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if !isDAG {
		t.Error("Empty graph should be a DAG")
	}
}

// TestIsDAG_SingleNode tests DAG check on single node
func TestIsDAG_SingleNode(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	graph.CreateNode([]string{"Node"}, nil)

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if !isDAG {
		t.Error("Single node should be a DAG")
	}
}

// TestIsDAG_LinearChain tests DAG check on linear directed chain
func TestIsDAG_LinearChain(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// A -> B -> C (DAG)
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if !isDAG {
		t.Error("Linear chain should be a DAG")
	}
}

// TestIsDAG_SimpleCycle tests DAG check with simple cycle
func TestIsDAG_SimpleCycle(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// A -> B -> A (cycle, not a DAG)
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeA.ID, "E", nil, 1.0)

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if isDAG {
		t.Error("Graph with cycle should not be a DAG")
	}
}

// TestIsDAG_SelfLoop tests DAG check with self-loop
func TestIsDAG_SelfLoop(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// A -> A (self-loop, not a DAG)
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	graph.CreateEdge(nodeA.ID, nodeA.ID, "E", nil, 1.0)

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if isDAG {
		t.Error("Graph with self-loop should not be a DAG")
	}
}

// TestIsDAG_Diamond tests DAG check on diamond shape
func TestIsDAG_Diamond(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeD, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeA.ID, nodeC.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeD.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if !isDAG {
		t.Error("Diamond graph should be a DAG")
	}
}

// TestIsDAG_ComplexDAG tests DAG check on complex acyclic graph
func TestIsDAG_ComplexDAG(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create a complex DAG (dependency graph style)
	nodes := make([]*storage.Node, 6)
	for i := 0; i < 6; i++ {
		nodes[i], _ = graph.CreateNode([]string{"Task"}, nil)
	}

	// Dependencies: 0->1, 0->2, 1->3, 2->3, 3->4, 2->5
	graph.CreateEdge(nodes[0].ID, nodes[1].ID, "DEPENDS_ON", nil, 1.0)
	graph.CreateEdge(nodes[0].ID, nodes[2].ID, "DEPENDS_ON", nil, 1.0)
	graph.CreateEdge(nodes[1].ID, nodes[3].ID, "DEPENDS_ON", nil, 1.0)
	graph.CreateEdge(nodes[2].ID, nodes[3].ID, "DEPENDS_ON", nil, 1.0)
	graph.CreateEdge(nodes[3].ID, nodes[4].ID, "DEPENDS_ON", nil, 1.0)
	graph.CreateEdge(nodes[2].ID, nodes[5].ID, "DEPENDS_ON", nil, 1.0)

	isDAG, err := IsDAG(graph)
	if err != nil {
		t.Fatalf("IsDAG failed: %v", err)
	}

	if !isDAG {
		t.Error("Complex dependency graph should be a DAG")
	}
}

// TestTopologicalSort_LinearChain tests topological sort on linear chain
func TestTopologicalSort_LinearChain(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// A -> B -> C
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	if len(sorted) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(sorted))
	}

	// Verify A comes before B, and B comes before C
	posA, posB, posC := -1, -1, -1
	for i, id := range sorted {
		if id == nodeA.ID {
			posA = i
		} else if id == nodeB.ID {
			posB = i
		} else if id == nodeC.ID {
			posC = i
		}
	}

	if posA >= posB || posB >= posC {
		t.Errorf("Invalid topological order: A=%d, B=%d, C=%d", posA, posB, posC)
	}
}

// TestTopologicalSort_Diamond tests topological sort on diamond
func TestTopologicalSort_Diamond(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeD, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeA.ID, nodeC.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeD.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	if len(sorted) != 4 {
		t.Errorf("Expected 4 nodes, got %d", len(sorted))
	}

	// Verify valid topological order
	positions := make(map[uint64]int)
	for i, id := range sorted {
		positions[id] = i
	}

	// A must come before B and C
	if positions[nodeA.ID] >= positions[nodeB.ID] {
		t.Error("A should come before B")
	}
	if positions[nodeA.ID] >= positions[nodeC.ID] {
		t.Error("A should come before C")
	}

	// B and C must come before D
	if positions[nodeB.ID] >= positions[nodeD.ID] {
		t.Error("B should come before D")
	}
	if positions[nodeC.ID] >= positions[nodeD.ID] {
		t.Error("C should come before D")
	}
}

// TestTopologicalSort_WithCycle tests topological sort on graph with cycle
func TestTopologicalSort_WithCycle(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// A -> B -> A (cycle)
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeA.ID, "E", nil, 1.0)

	_, err := TopologicalSort(graph)
	if err == nil {
		t.Error("TopologicalSort should fail on graph with cycle")
	}
}

// TestTopologicalSort_EmptyGraph tests topological sort on empty graph
func TestTopologicalSort_EmptyGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	if len(sorted) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(sorted))
	}
}

// TestTopologicalSort_DisconnectedComponents tests sorting with disconnected parts
func TestTopologicalSort_DisconnectedComponents(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Component 1: A -> B
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)

	// Component 2: C -> D
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeD, _ := graph.CreateNode([]string{"Node"}, nil)
	graph.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)

	sorted, err := TopologicalSort(graph)
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	if len(sorted) != 4 {
		t.Errorf("Expected 4 nodes, got %d", len(sorted))
	}

	// Verify each component's ordering
	positions := make(map[uint64]int)
	for i, id := range sorted {
		positions[id] = i
	}

	if positions[nodeA.ID] >= positions[nodeB.ID] {
		t.Error("A should come before B")
	}
	if positions[nodeC.ID] >= positions[nodeD.ID] {
		t.Error("C should come before D")
	}
}

// TestIsTree_SingleNode tests tree check on single node
func TestIsTree_SingleNode(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	graph.CreateNode([]string{"Node"}, nil)

	isTree, err := IsTree(graph)
	if err != nil {
		t.Fatalf("IsTree failed: %v", err)
	}

	if !isTree {
		t.Error("Single node should be a tree")
	}
}

// TestIsTree_ValidTree tests tree check on valid tree
func TestIsTree_ValidTree(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	//     A (root)
	//    / \
	//   B   C
	//  /
	// D
	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeD, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "CHILD", nil, 1.0)
	graph.CreateEdge(nodeA.ID, nodeC.ID, "CHILD", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeD.ID, "CHILD", nil, 1.0)

	isTree, err := IsTree(graph)
	if err != nil {
		t.Fatalf("IsTree failed: %v", err)
	}

	if !isTree {
		t.Error("Valid tree structure should be detected as tree")
	}
}

// TestIsTree_WithCycle tests tree check on graph with cycle
func TestIsTree_WithCycle(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeC.ID, nodeA.ID, "E", nil, 1.0)

	isTree, err := IsTree(graph)
	if err != nil {
		t.Fatalf("IsTree failed: %v", err)
	}

	if isTree {
		t.Error("Graph with cycle should not be a tree")
	}
}

// TestIsConnected_ConnectedGraph tests connected graph
func TestIsConnected_ConnectedGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)

	isConnected, err := IsConnected(graph)
	if err != nil {
		t.Fatalf("IsConnected failed: %v", err)
	}

	if !isConnected {
		t.Error("Connected chain should be detected as connected")
	}
}

// TestIsConnected_DisconnectedGraph tests disconnected graph
func TestIsConnected_DisconnectedGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeD, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)

	isConnected, err := IsConnected(graph)
	if err != nil {
		t.Fatalf("IsConnected failed: %v", err)
	}

	if isConnected {
		t.Error("Disconnected graph should not be connected")
	}
}

// TestIsBipartite_ValidBipartite tests valid bipartite graph
func TestIsBipartite_ValidBipartite(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeD, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeA.ID, nodeC.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeD.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)

	isBipartite, p1, p2, err := IsBipartite(graph)
	if err != nil {
		t.Fatalf("IsBipartite failed: %v", err)
	}

	if !isBipartite {
		t.Error("Valid bipartite graph should be detected")
	}

	if len(p1)+len(p2) != 4 {
		t.Errorf("Expected 4 total nodes in partitions, got %d", len(p1)+len(p2))
	}
}

// TestIsBipartite_Triangle tests triangle (not bipartite)
func TestIsBipartite_Triangle(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	nodeA, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeB, _ := graph.CreateNode([]string{"Node"}, nil)
	nodeC, _ := graph.CreateNode([]string{"Node"}, nil)

	graph.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)
	graph.CreateEdge(nodeC.ID, nodeA.ID, "E", nil, 1.0)

	isBipartite, _, _, err := IsBipartite(graph)
	if err != nil {
		t.Fatalf("IsBipartite failed: %v", err)
	}

	if isBipartite {
		t.Error("Triangle (odd cycle) should not be bipartite")
	}
}
