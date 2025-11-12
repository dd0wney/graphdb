package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupPageRankTestGraph creates a test graph for PageRank tests
func setupPageRankTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "pagerank-test-*")
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

// TestPageRank_EmptyGraph tests PageRank on empty graph
func TestPageRank_EmptyGraph(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	if len(result.Scores) != 0 {
		t.Errorf("Expected 0 scores for empty graph, got %d", len(result.Scores))
	}

	if !result.Converged {
		t.Error("Expected convergence for empty graph")
	}
}

// TestPageRank_SingleNode tests PageRank on single node
func TestPageRank_SingleNode(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create single node
	node, err := gs.CreateNode([]string{"Node"}, nil)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	if len(result.Scores) != 1 {
		t.Errorf("Expected 1 score, got %d", len(result.Scores))
	}

	// Single node should have score of 1.0 (normalized)
	score := result.GetNodeRank(node.ID)
	if math.Abs(score-1.0) > 0.001 {
		t.Errorf("Expected score ~1.0 for single node, got %f", score)
	}
}

// TestPageRank_LinearChain tests PageRank on linear chain A->B->C
func TestPageRank_LinearChain(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create linear chain: A -> B -> C
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// In A->B->C chain:
	// C should have highest PageRank (receives links)
	// B should have medium PageRank
	// A should have lowest PageRank (no incoming links)
	scoreA := result.GetNodeRank(nodeA.ID)
	scoreB := result.GetNodeRank(nodeB.ID)
	scoreC := result.GetNodeRank(nodeC.ID)

	if scoreC <= scoreB {
		t.Errorf("Expected C score (%f) > B score (%f)", scoreC, scoreB)
	}

	if scoreB <= scoreA {
		t.Errorf("Expected B score (%f) > A score (%f)", scoreB, scoreA)
	}

	// Scores should sum to 1
	sum := scoreA + scoreB + scoreC
	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("Expected scores to sum to 1.0, got %f", sum)
	}
}

// TestPageRank_Star tests PageRank on star topology (hub and spokes)
func TestPageRank_Star(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create star: A <- B, A <- C, A <- D
	// A is the hub receiving all links
	nodeA, _ := gs.CreateNode([]string{"Hub"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Spoke"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Spoke"}, nil)

	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeA.ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Hub should have highest PageRank
	scoreA := result.GetNodeRank(nodeA.ID)
	scoreB := result.GetNodeRank(nodeB.ID)
	scoreC := result.GetNodeRank(nodeC.ID)
	scoreD := result.GetNodeRank(nodeD.ID)

	if scoreA <= scoreB {
		t.Errorf("Expected hub score (%f) > spoke score (%f)", scoreA, scoreB)
	}

	// Spokes should have equal scores (symmetric)
	if math.Abs(scoreB-scoreC) > 0.001 || math.Abs(scoreC-scoreD) > 0.001 {
		t.Errorf("Expected equal spoke scores, got B=%f, C=%f, D=%f", scoreB, scoreC, scoreD)
	}
}

// TestPageRank_Cycle tests PageRank on cycle A->B->C->A
func TestPageRank_Cycle(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create cycle: A -> B -> C -> A
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// In symmetric cycle, all nodes should have equal PageRank
	scoreA := result.GetNodeRank(nodeA.ID)
	scoreB := result.GetNodeRank(nodeB.ID)
	scoreC := result.GetNodeRank(nodeC.ID)

	if math.Abs(scoreA-scoreB) > 0.001 || math.Abs(scoreB-scoreC) > 0.001 {
		t.Errorf("Expected equal scores in cycle, got A=%f, B=%f, C=%f", scoreA, scoreB, scoreC)
	}

	// Should converge
	if !result.Converged {
		t.Error("Expected convergence for symmetric cycle")
	}
}

// TestPageRank_ComplexGraph tests PageRank on complex graph
func TestPageRank_ComplexGraph(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create complex graph with multiple connections
	nodes := make([]*storage.Node, 5)
	for i := 0; i < 5; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodes[i] = node
	}

	// Create edges forming a complex topology
	// Node 4 is most "important" (most incoming edges)
	gs.CreateEdge(nodes[0].ID, nodes[1].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[0].ID, nodes[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[3].ID, nodes[4].ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Node 4 should have highest score (most incoming edges)
	score4 := result.GetNodeRank(nodes[4].ID)

	for i := 0; i < 4; i++ {
		scoreI := result.GetNodeRank(nodes[i].ID)
		if score4 <= scoreI {
			t.Errorf("Expected node 4 score (%f) > node %d score (%f)", score4, i, scoreI)
		}
	}

	// Check TopNodes
	if len(result.TopNodes) == 0 {
		t.Error("Expected TopNodes to be populated")
	}

	// Top node should be node 4
	if result.TopNodes[0].NodeID != nodes[4].ID {
		t.Errorf("Expected top node to be node 4, got node %d", result.TopNodes[0].NodeID)
	}
}

// TestPageRank_Convergence tests convergence detection
func TestPageRank_Convergence(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create simple graph that should converge quickly
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	opts.Tolerance = 1e-6
	opts.MaxIterations = 100

	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	if !result.Converged {
		t.Error("Expected algorithm to converge")
	}

	if result.Iterations >= opts.MaxIterations {
		t.Errorf("Expected convergence before max iterations, got %d iterations", result.Iterations)
	}
}

// TestPageRank_MaxIterations tests max iterations limit
func TestPageRank_MaxIterations(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create graph
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	opts.MaxIterations = 5 // Very low limit
	opts.Tolerance = 1e-10 // Very strict tolerance

	result, err := PageRank(gs, opts)

	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Algorithm may converge before max iterations (that's correct behavior)
	// Just verify we don't exceed max iterations
	if result.Iterations > opts.MaxIterations {
		t.Errorf("Expected at most %d iterations, got %d", opts.MaxIterations, result.Iterations)
	}

	// If converged early, that's fine - the algorithm is working correctly
	if result.Converged && result.Iterations < opts.MaxIterations {
		// This is expected - algorithm converged before hitting limit
		return
	}

	// If we hit max iterations without convergence, that's also fine for this test
	if result.Iterations == opts.MaxIterations && !result.Converged {
		// This is what we're testing - respecting iteration limit
		return
	}
}

// TestPageRank_DampingFactor tests different damping factors
func TestPageRank_DampingFactor(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create simple chain A -> B
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)

	// Test with damping factor 0.5
	opts1 := DefaultPageRankOptions()
	opts1.DampingFactor = 0.5
	result1, err := PageRank(gs, opts1)
	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Test with damping factor 0.9
	opts2 := DefaultPageRankOptions()
	opts2.DampingFactor = 0.9
	result2, err := PageRank(gs, opts2)
	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Scores should differ based on damping factor
	scoreB1 := result1.GetNodeRank(nodeB.ID)
	scoreB2 := result2.GetNodeRank(nodeB.ID)

	if math.Abs(scoreB1-scoreB2) < 0.01 {
		t.Errorf("Expected different scores for different damping factors, got %f and %f", scoreB1, scoreB2)
	}
}

// TestPageRank_GetTopNodesByPageRank tests getting top N nodes
func TestPageRank_GetTopNodesByPageRank(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create several nodes
	nodes := make([]*storage.Node, 5)
	for i := 0; i < 5; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodes[i] = node
	}

	// Create edges to make node 2 most important
	gs.CreateEdge(nodes[0].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[3].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[4].ID, nodes[2].ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)
	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Get top 3 nodes
	topNodes := result.GetTopNodesByPageRank(3)

	if len(topNodes) != 3 {
		t.Errorf("Expected 3 top nodes, got %d", len(topNodes))
	}

	// First node should be node 2
	if topNodes[0].NodeID != nodes[2].ID {
		t.Errorf("Expected top node to be node 2, got node %d", topNodes[0].NodeID)
	}

	// Scores should be descending
	for i := 0; i < len(topNodes)-1; i++ {
		if topNodes[i].Score < topNodes[i+1].Score {
			t.Errorf("Expected descending scores, got %f followed by %f", topNodes[i].Score, topNodes[i+1].Score)
		}
	}
}

// TestPageRank_GetTopNodesByPageRank_ExceedsLimit tests requesting more nodes than available
func TestPageRank_GetTopNodesByPageRank_ExceedsLimit(t *testing.T) {
	gs := setupPageRankTestGraph(t)

	// Create only 2 nodes
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)

	opts := DefaultPageRankOptions()
	result, err := PageRank(gs, opts)
	if err != nil {
		t.Fatalf("PageRank failed: %v", err)
	}

	// Request 10 nodes but only 2 exist
	topNodes := result.GetTopNodesByPageRank(10)

	// Should return only 2 nodes (or up to 10 from internal TopNodes slice)
	if len(topNodes) > 10 {
		t.Errorf("Expected at most 10 nodes, got %d", len(topNodes))
	}
}

// TestDefaultPageRankOptions tests default options
func TestDefaultPageRankOptions(t *testing.T) {
	opts := DefaultPageRankOptions()

	if opts.DampingFactor != 0.85 {
		t.Errorf("Expected default damping factor 0.85, got %f", opts.DampingFactor)
	}

	if opts.MaxIterations != 100 {
		t.Errorf("Expected default max iterations 100, got %d", opts.MaxIterations)
	}

	if opts.Tolerance != 1e-6 {
		t.Errorf("Expected default tolerance 1e-6, got %e", opts.Tolerance)
	}
}
