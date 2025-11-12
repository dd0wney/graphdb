package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupCommunityTestGraph creates a test graph for community tests
func setupCommunityTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "community-test-*")
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

// TestConnectedComponents_EmptyGraph tests connected components on empty graph
func TestConnectedComponents_EmptyGraph(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	result, err := ConnectedComponents(gs)

	if err != nil {
		t.Fatalf("ConnectedComponents failed: %v", err)
	}

	if len(result.Communities) != 0 {
		t.Errorf("Expected 0 communities for empty graph, got %d", len(result.Communities))
	}
}

// TestConnectedComponents_SingleNode tests single node as one component
func TestConnectedComponents_SingleNode(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := ConnectedComponents(gs)

	if err != nil {
		t.Fatalf("ConnectedComponents failed: %v", err)
	}

	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 community, got %d", len(result.Communities))
	}

	if result.Communities[0].Size != 1 {
		t.Errorf("Expected community size 1, got %d", result.Communities[0].Size)
	}

	if result.NodeCommunity[node.ID] != 0 {
		t.Errorf("Expected node in community 0, got community %d", result.NodeCommunity[node.ID])
	}
}

// TestConnectedComponents_SingleComponent tests fully connected graph
func TestConnectedComponents_SingleComponent(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create connected chain A -> B -> C
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ConnectedComponents(gs)

	if err != nil {
		t.Fatalf("ConnectedComponents failed: %v", err)
	}

	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 component, got %d", len(result.Communities))
	}

	if result.Communities[0].Size != 3 {
		t.Errorf("Expected component size 3, got %d", result.Communities[0].Size)
	}
}

// TestConnectedComponents_MultipleComponents tests disconnected graph
func TestConnectedComponents_MultipleComponents(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create two disconnected components: A-B and C-D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	// Component 1: A <-> B
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)

	// Component 2: C <-> D
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ConnectedComponents(gs)

	if err != nil {
		t.Fatalf("ConnectedComponents failed: %v", err)
	}

	if len(result.Communities) != 2 {
		t.Errorf("Expected 2 components, got %d", len(result.Communities))
	}

	// Each component should have 2 nodes
	for i, community := range result.Communities {
		if community.Size != 2 {
			t.Errorf("Expected component %d size 2, got %d", i, community.Size)
		}
	}

	// Nodes A and B should be in same component
	commA := result.NodeCommunity[nodeA.ID]
	commB := result.NodeCommunity[nodeB.ID]
	if commA != commB {
		t.Errorf("Expected A and B in same component, got %d and %d", commA, commB)
	}

	// Nodes C and D should be in same component
	commC := result.NodeCommunity[nodeC.ID]
	commD := result.NodeCommunity[nodeD.ID]
	if commC != commD {
		t.Errorf("Expected C and D in same component, got %d and %d", commC, commD)
	}

	// But A-B should be in different component than C-D
	if commA == commC {
		t.Error("Expected A-B and C-D in different components")
	}
}

// TestConnectedComponents_IsolatedNodes tests graph with isolated nodes
func TestConnectedComponents_IsolatedNodes(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create 3 isolated nodes
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := ConnectedComponents(gs)

	if err != nil {
		t.Fatalf("ConnectedComponents failed: %v", err)
	}

	// Each isolated node is its own component
	if len(result.Communities) != 3 {
		t.Errorf("Expected 3 components, got %d", len(result.Communities))
	}

	// Each component should have size 1
	for _, community := range result.Communities {
		if community.Size != 1 {
			t.Errorf("Expected component size 1, got %d", community.Size)
		}
	}

	// Each node should be in different component
	commA := result.NodeCommunity[nodeA.ID]
	commB := result.NodeCommunity[nodeB.ID]
	commC := result.NodeCommunity[nodeC.ID]

	if commA == commB || commB == commC || commA == commC {
		t.Errorf("Expected isolated nodes in different components, got %d, %d, %d", commA, commB, commC)
	}
}

// TestLabelPropagation_SingleNode tests label propagation with single node
func TestLabelPropagation_SingleNode(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := LabelPropagation(gs, 10)

	if err != nil {
		t.Fatalf("LabelPropagation failed: %v", err)
	}

	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 community, got %d", len(result.Communities))
	}

	if result.Communities[0].Size != 1 {
		t.Errorf("Expected community size 1, got %d", result.Communities[0].Size)
	}

	if result.NodeCommunity[node.ID] < 0 {
		t.Error("Expected node to be assigned to a community")
	}
}

// TestLabelPropagation_FullyConnected tests fully connected graph
func TestLabelPropagation_FullyConnected(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create complete graph with bidirectional edges
	nodes := make([]*storage.Node, 4)
	for i := 0; i < 4; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodes[i] = node
	}

	// Connect all pairs bidirectionally
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			gs.CreateEdge(nodes[i].ID, nodes[j].ID, "LINKS", nil, 1.0)
			gs.CreateEdge(nodes[j].ID, nodes[i].ID, "LINKS", nil, 1.0)
		}
	}

	result, err := LabelPropagation(gs, 10)

	if err != nil {
		t.Fatalf("LabelPropagation failed: %v", err)
	}

	// In fully connected graph, all nodes should converge to same community
	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 community for fully connected graph, got %d", len(result.Communities))
	}

	if result.Communities[0].Size != 4 {
		t.Errorf("Expected community size 4, got %d", result.Communities[0].Size)
	}
}

// TestLabelPropagation_TwoClusters tests graph with clear community structure
func TestLabelPropagation_TwoClusters(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create two densely connected clusters: A-B-C and D-E-F
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeE, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeF, _ := gs.CreateNode([]string{"Node"}, nil)

	// Cluster 1: A-B-C (bidirectional)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)

	// Cluster 2: D-E-F (bidirectional)
	gs.CreateEdge(nodeD.ID, nodeE.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeE.ID, nodeD.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeE.ID, nodeF.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeF.ID, nodeE.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeF.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeF.ID, nodeD.ID, "LINKS", nil, 1.0)

	// Weak link between clusters (B -> E only)
	gs.CreateEdge(nodeB.ID, nodeE.ID, "LINKS", nil, 1.0)

	result, err := LabelPropagation(gs, 20)

	if err != nil {
		t.Fatalf("LabelPropagation failed: %v", err)
	}

	// Should detect at least 1 community, ideally 2
	if len(result.Communities) == 0 {
		t.Error("Expected at least 1 community")
	}

	// All nodes should be assigned to communities
	if len(result.NodeCommunity) != 6 {
		t.Errorf("Expected 6 nodes in communities, got %d", len(result.NodeCommunity))
	}
}

// TestClusteringCoefficient_EmptyGraph tests clustering coefficient on empty graph
func TestClusteringCoefficient_EmptyGraph(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	result, err := ClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("ClusteringCoefficient failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 coefficients for empty graph, got %d", len(result))
	}
}

// TestClusteringCoefficient_SingleNode tests single node
func TestClusteringCoefficient_SingleNode(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := ClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("ClusteringCoefficient failed: %v", err)
	}

	// Single node has no neighbors, coefficient = 0
	coef := result[node.ID]
	if coef != 0.0 {
		t.Errorf("Expected coefficient 0 for single node, got %f", coef)
	}
}

// TestClusteringCoefficient_Triangle tests complete triangle
func TestClusteringCoefficient_Triangle(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create complete triangle: A -> B, B -> C, C -> A, and reverse
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("ClusteringCoefficient failed: %v", err)
	}

	// In complete triangle, clustering coefficient should be 1.0
	coefA := result[nodeA.ID]

	// Should be 1.0 (all neighbors are connected)
	if math.Abs(coefA-1.0) > 0.001 {
		t.Errorf("Expected coefficient ~1.0 for complete triangle, got %f", coefA)
	}
}

// TestClusteringCoefficient_Star tests star topology
func TestClusteringCoefficient_Star(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create star: A -> B, A -> C, A -> D (no connections between spokes)
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeD.ID, "LINKS", nil, 1.0)

	result, err := ClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("ClusteringCoefficient failed: %v", err)
	}

	// Hub's neighbors (spokes) are not connected, so coefficient = 0
	coefA := result[nodeA.ID]

	if coefA != 0.0 {
		t.Errorf("Expected coefficient 0 for star hub, got %f", coefA)
	}

	// Spokes have < 2 neighbors, so coefficient = 0
	coefB := result[nodeB.ID]
	if coefB != 0.0 {
		t.Errorf("Expected coefficient 0 for spoke, got %f", coefB)
	}
}

// TestClusteringCoefficient_PartialTriangle tests partial triangle
func TestClusteringCoefficient_PartialTriangle(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create partial triangle: A -> B, A -> C, but B and C not connected
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := ClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("ClusteringCoefficient failed: %v", err)
	}

	// A's neighbors (B and C) are not connected
	// 0 triangles / 1 possible triangle = 0.0
	coefA := result[nodeA.ID]

	if coefA != 0.0 {
		t.Errorf("Expected coefficient 0 for partial triangle, got %f", coefA)
	}
}

// TestAverageClusteringCoefficient tests average calculation
func TestAverageClusteringCoefficient(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create simple graph
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	result, err := AverageClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("AverageClusteringCoefficient failed: %v", err)
	}

	// For triangle, average should be relatively high
	if result < 0 || result > 1 {
		t.Errorf("Expected average coefficient in [0,1], got %f", result)
	}

	// Should be positive for connected graph
	if result == 0 {
		t.Errorf("Expected non-zero average coefficient for triangle")
	}
}

// TestAverageClusteringCoefficient_EmptyGraph tests average on empty graph
func TestAverageClusteringCoefficient_EmptyGraph(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	result, err := AverageClusteringCoefficient(gs)

	if err != nil {
		t.Fatalf("AverageClusteringCoefficient failed: %v", err)
	}

	// Empty graph should have average 0
	if result != 0.0 {
		t.Errorf("Expected average 0 for empty graph, got %f", result)
	}
}

// TestConnectedComponents_BidirectionalEdges tests with bidirectional edges
func TestConnectedComponents_BidirectionalEdges(t *testing.T) {
	gs := setupCommunityTestGraph(t)

	// Create graph with bidirectional edges
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)

	result, err := ConnectedComponents(gs)

	if err != nil {
		t.Fatalf("ConnectedComponents failed: %v", err)
	}

	// Should be one component
	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 component, got %d", len(result.Communities))
	}

	// Both nodes should be in same component
	commA := result.NodeCommunity[nodeA.ID]
	commB := result.NodeCommunity[nodeB.ID]

	if commA != commB {
		t.Errorf("Expected both nodes in same component, got %d and %d", commA, commB)
	}
}
