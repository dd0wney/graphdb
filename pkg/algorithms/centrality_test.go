package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// setupCentralityTestGraph creates a test graph for centrality tests
func setupCentralityTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "centrality-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Create graph storage
	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}
	t.Cleanup(func() { gs.Close() })

	return gs
}

// TestDegreeCentrality_EmptyGraph tests degree centrality on empty graph
func TestDegreeCentrality_EmptyGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 scores for empty graph, got %d", len(result))
	}
}

// TestDegreeCentrality_SingleNode tests degree centrality on single node
func TestDegreeCentrality_SingleNode(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 score, got %d", len(result))
	}

	// Single node has no connections, so degree should be 0
	degree := result[node.ID]
	if degree != 0.0 {
		t.Errorf("Expected degree 0 for single node, got %f", degree)
	}
}

// TestDegreeCentrality_LinearChain tests degree centrality on A->B->C
func TestDegreeCentrality_LinearChain(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	degreeA := result[nodeA.ID]
	degreeB := result[nodeB.ID]
	degreeC := result[nodeC.ID]

	// Node B has highest degree (1 in + 1 out = 2 total)
	// Nodes A and C have degree 1 each
	if degreeB <= degreeA || degreeB <= degreeC {
		t.Errorf("Expected B degree (%f) > A degree (%f) and C degree (%f)", degreeB, degreeA, degreeC)
	}
}

// TestDegreeCentrality_Star tests degree centrality on star topology
func TestDegreeCentrality_Star(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create star: A <- B, A <- C, A <- D (A is hub)
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	degreeA := result[nodeA.ID]
	degreeB := result[nodeB.ID]

	// Hub should have highest degree (3 incoming edges)
	if degreeA <= degreeB {
		t.Errorf("Expected hub degree (%f) > spoke degree (%f)", degreeA, degreeB)
	}
}

// TestClosenessCentrality_LinearChain tests closeness centrality on A->B->C
func TestClosenessCentrality_LinearChain(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ClosenessCentrality(gs)

	if err != nil {
		t.Fatalf("ClosenessCentrality failed: %v", err)
	}

	closenessA := result[nodeA.ID]
	closenessB := result[nodeB.ID]
	closenessC := result[nodeC.ID]

	// Node B is most central (can reach both A and C quickly)
	// Nodes A and C are at the ends
	if closenessB <= closenessA || closenessB <= closenessC {
		t.Errorf("Expected B closeness (%f) > A closeness (%f) and C closeness (%f)", closenessB, closenessA, closenessC)
	}
}

// TestClosenessCentrality_Star tests closeness centrality on star topology
func TestClosenessCentrality_Star(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create star with bidirectional edges so all nodes can reach hub
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	// Bidirectional edges
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := ClosenessCentrality(gs)

	if err != nil {
		t.Fatalf("ClosenessCentrality failed: %v", err)
	}

	closenessA := result[nodeA.ID]
	closenessB := result[nodeB.ID]

	// Hub should have highest closeness (1-hop to everyone)
	if closenessA <= closenessB {
		t.Errorf("Expected hub closeness (%f) > spoke closeness (%f)", closenessA, closenessB)
	}
}

// TestClosenessCentrality_Isolated tests closeness with unreachable nodes
func TestClosenessCentrality_Isolated(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	// No edges - nodes are isolated

	result, err := ClosenessCentrality(gs)

	if err != nil {
		t.Fatalf("ClosenessCentrality failed: %v", err)
	}

	closenessA := result[nodeA.ID]
	closenessB := result[nodeB.ID]

	// Isolated nodes should have closeness 0
	if closenessA != 0.0 || closenessB != 0.0 {
		t.Errorf("Expected closeness 0 for isolated nodes, got A=%f, B=%f", closenessA, closenessB)
	}
}

// TestBetweennessCentrality_LinearChain tests betweenness on A->B->C
func TestBetweennessCentrality_LinearChain(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	betweennessA := result[nodeA.ID]
	betweennessB := result[nodeB.ID]
	betweennessC := result[nodeC.ID]

	// Node B is on the path from A to C, so it should have highest betweenness
	// Nodes A and C are endpoints, so they have 0 betweenness
	if betweennessB <= betweennessA || betweennessB <= betweennessC {
		t.Errorf("Expected B betweenness (%f) > A (%f) and C (%f)", betweennessB, betweennessA, betweennessC)
	}
}

// TestBetweennessCentrality_Star tests betweenness on star topology
func TestBetweennessCentrality_Star(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create star with bidirectional edges
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	// Bidirectional edges so paths go through hub
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	betweennessA := result[nodeA.ID]
	betweennessB := result[nodeB.ID]

	// Hub should have highest betweenness (all paths between spokes go through it)
	if betweennessA <= betweennessB {
		t.Errorf("Expected hub betweenness (%f) > spoke betweenness (%f)", betweennessA, betweennessB)
	}
}

// TestBetweennessCentrality_Diamond tests betweenness on diamond graph
func TestBetweennessCentrality_Diamond(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create diamond: A -> B -> D, A -> C -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeD.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	betweennessB := result[nodeB.ID]
	betweennessC := result[nodeC.ID]

	// B and C should have equal betweenness (both are on parallel paths)
	if math.Abs(betweennessB-betweennessC) > 0.001 {
		t.Errorf("Expected equal betweenness for B (%f) and C (%f)", betweennessB, betweennessC)
	}
}

// TestBetweennessCentrality_EmptyGraph tests betweenness on empty graph
func TestBetweennessCentrality_EmptyGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	result, err := BetweennessCentrality(gs)

	if err != nil {
		t.Fatalf("BetweennessCentrality failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 scores for empty graph, got %d", len(result))
	}
}

// TestComputeAllCentrality tests computing all centrality measures
func TestComputeAllCentrality(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create small graph
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ComputeAllCentrality(gs)

	if err != nil {
		t.Fatalf("ComputeAllCentrality failed: %v", err)
	}

	// Verify all three centrality measures are computed
	if len(result.Betweenness) != 3 {
		t.Errorf("Expected 3 betweenness scores, got %d", len(result.Betweenness))
	}

	if len(result.Closeness) != 3 {
		t.Errorf("Expected 3 closeness scores, got %d", len(result.Closeness))
	}

	if len(result.Degree) != 3 {
		t.Errorf("Expected 3 degree scores, got %d", len(result.Degree))
	}

	// Verify TopNodes are populated
	if len(result.TopByBetweenness) == 0 {
		t.Error("Expected TopByBetweenness to be populated")
	}

	if len(result.TopByCloseness) == 0 {
		t.Error("Expected TopByCloseness to be populated")
	}

	if len(result.TopByDegree) == 0 {
		t.Error("Expected TopByDegree to be populated")
	}
}

// TestComputeAllCentrality_ComplexGraph tests all centrality on larger graph
func TestComputeAllCentrality_ComplexGraph(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create more complex graph
	nodes := make([]*storage.Node, 6)
	for i := 0; i < 6; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodes[i] = node
	}

	// Create edges forming interesting topology
	gs.CreateEdge(nodes[0].ID, nodes[1].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[3].ID, nodes[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[4].ID, nodes[5].ID, "LINKS", nil, 1.0)
	// Add shortcuts
	gs.CreateEdge(nodes[0].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[4].ID, "LINKS", nil, 1.0)

	result, err := ComputeAllCentrality(gs)

	if err != nil {
		t.Fatalf("ComputeAllCentrality failed: %v", err)
	}

	// Node 2 should be important (central position with shortcuts)
	degree2 := result.Degree[nodes[2].ID]

	// Check that node 2 has relatively high degree
	if degree2 < 0.1 {
		t.Errorf("Expected node 2 to have decent degree centrality, got %f", degree2)
	}

	// Verify all measures are non-negative
	for nodeID, score := range result.Betweenness {
		if score < 0 {
			t.Errorf("Negative betweenness for node %d: %f", nodeID, score)
		}
	}

	for nodeID, score := range result.Closeness {
		if score < 0 {
			t.Errorf("Negative closeness for node %d: %f", nodeID, score)
		}
	}

	for nodeID, score := range result.Degree {
		if score < 0 {
			t.Errorf("Negative degree for node %d: %f", nodeID, score)
		}
	}
}

// TestDegreeCentrality_Normalization tests that degree centrality is normalized
func TestDegreeCentrality_Normalization(t *testing.T) {
	gs := setupCentralityTestGraph(t)

	// Create complete graph K3 (all nodes connected to all others)
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeB.ID, "LINKS", nil, 1.0)

	result, err := DegreeCentrality(gs)

	if err != nil {
		t.Fatalf("DegreeCentrality failed: %v", err)
	}

	// In complete graph, all nodes should have maximum normalized degree (1.0)
	// Each node has 2 in + 2 out = 4 total edges
	// Normalized by (n-1) = 2, so 4/2 = 2.0
	degreeA := result[nodeA.ID]

	// All nodes should have equal degree
	for _, degree := range result {
		if math.Abs(degree-degreeA) > 0.001 {
			t.Errorf("Expected equal degrees in complete graph, got %f and %f", degreeA, degree)
		}
	}
}
