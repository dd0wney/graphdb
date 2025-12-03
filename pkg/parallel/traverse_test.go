package parallel

import (
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupTraverseTestGraph creates a test graph for traversal tests
func setupTraverseTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()

	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "traverse-test-*")
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

// TestNewParallelTraverser tests creating a parallel traverser
func TestNewParallelTraverser(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Test with default workers (0 should use NumCPU)
	pt, _ := NewParallelTraverser(gs, 0)
	if pt == nil {
		t.Fatal("Expected non-nil traverser")
	}
	if pt.numWorkers <= 0 {
		t.Errorf("Expected positive numWorkers, got %d", pt.numWorkers)
	}
	pt.Close()

	// Test with specific worker count
	pt2, _ := NewParallelTraverser(gs, 4)
	if pt2.numWorkers != 4 {
		t.Errorf("Expected 4 workers, got %d", pt2.numWorkers)
	}
	pt2.Close()

	// Test with negative workers (should default to NumCPU)
	pt3, _ := NewParallelTraverser(gs, -1)
	if pt3.numWorkers <= 0 {
		t.Errorf("Expected positive numWorkers for negative input, got %d", pt3.numWorkers)
	}
	pt3.Close()
}

// TestTraverseBFS_EmptyStartNodes tests BFS with empty start nodes
func TestTraverseBFS_EmptyStartNodes(t *testing.T) {
	gs := setupTraverseTestGraph(t)
	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	result := pt.TraverseBFS([]uint64{}, 5)

	if result != nil {
		t.Errorf("Expected nil for empty start nodes, got %v", result)
	}
}

// TestTraverseBFS_SingleNode tests BFS from single node
func TestTraverseBFS_SingleNode(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create simple graph: A -> B -> C
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	result := pt.TraverseBFS([]uint64{nodeA.ID}, 3)

	// Should find B and C (A is the start, not included in results)
	if len(result) != 2 {
		t.Errorf("Expected 2 nodes in BFS result, got %d: %v", len(result), result)
	}
}

// TestTraverseBFS_MaxDepth tests BFS respects max depth
func TestTraverseBFS_MaxDepth(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create chain: A -> B -> C -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	// Depth 1 should only reach B
	result1 := pt.TraverseBFS([]uint64{nodeA.ID}, 1)
	if len(result1) != 1 {
		t.Errorf("Expected 1 node at depth 1, got %d", len(result1))
	}

	// Depth 2 should reach B and C
	result2 := pt.TraverseBFS([]uint64{nodeA.ID}, 2)
	if len(result2) != 2 {
		t.Errorf("Expected 2 nodes at depth 2, got %d", len(result2))
	}

	// Depth 3 should reach B, C, and D
	result3 := pt.TraverseBFS([]uint64{nodeA.ID}, 3)
	if len(result3) != 3 {
		t.Errorf("Expected 3 nodes at depth 3, got %d", len(result3))
	}
}

// TestTraverseBFS_MultipleStartNodes tests BFS from multiple starts
func TestTraverseBFS_MultipleStartNodes(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create two separate chains
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	// Start from both A and C
	result := pt.TraverseBFS([]uint64{nodeA.ID, nodeC.ID}, 2)

	// Should find B and D
	if len(result) != 2 {
		t.Errorf("Expected 2 nodes from multiple starts, got %d", len(result))
	}
}

// TestTraverseBFS_Cycle tests BFS handles cycles correctly
func TestTraverseBFS_Cycle(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create cycle: A -> B -> C -> A
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	result := pt.TraverseBFS([]uint64{nodeA.ID}, 10)

	// Should visit each node only once (B and C, not A again)
	if len(result) != 2 {
		t.Errorf("Expected 2 unique nodes in cycle, got %d", len(result))
	}
}

// TestTraverseDFS_SingleNode tests DFS from single node
func TestTraverseDFS_SingleNode(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create simple graph: A -> B -> C
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	result := pt.TraverseDFS(nodeA.ID, 10)

	// DFS should visit A, B, C
	if len(result) != 3 {
		t.Errorf("Expected 3 nodes in DFS result, got %d", len(result))
	}
}

// TestTraverseDFS_MaxDepth tests DFS respects max depth
func TestTraverseDFS_MaxDepth(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create chain: A -> B -> C -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	// Depth 1 should only reach A
	result1 := pt.TraverseDFS(nodeA.ID, 1)
	if len(result1) != 1 {
		t.Errorf("Expected 1 node at depth 1, got %d", len(result1))
	}

	// Depth 2 should reach A and B
	result2 := pt.TraverseDFS(nodeA.ID, 2)
	if len(result2) != 2 {
		t.Errorf("Expected 2 nodes at depth 2, got %d", len(result2))
	}
}

// TestTraverseDFS_Cycle tests DFS handles cycles correctly
func TestTraverseDFS_Cycle(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create cycle: A -> B -> C -> A
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	result := pt.TraverseDFS(nodeA.ID, 10)

	// Should visit each node only once
	if len(result) != 3 {
		t.Errorf("Expected 3 unique nodes in cycle, got %d", len(result))
	}
}

// TestTraverseDFS_HighDegreeNode tests parallel branch processing
func TestTraverseDFS_HighDegreeNode(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create hub with many spokes (>10 to trigger parallel processing)
	hub, _ := gs.CreateNode([]string{"Hub"}, nil)

	spokes := make([]*storage.Node, 15)
	for i := 0; i < 15; i++ {
		spoke, _ := gs.CreateNode([]string{"Spoke"}, nil)
		spokes[i] = spoke
		gs.CreateEdge(hub.ID, spoke.ID, "LINKS", nil, 1.0)
	}

	pt, _ := NewParallelTraverser(gs, 4)
	defer pt.Close()

	result := pt.TraverseDFS(hub.ID, 5)

	// Should visit hub + all spokes
	if len(result) != 16 {
		t.Errorf("Expected 16 nodes (hub + 15 spokes), got %d", len(result))
	}
}

// TestParallelShortestPath_SameNode tests shortest path to itself
func TestParallelShortestPath_SameNode(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	node, _ := gs.CreateNode([]string{"Node"}, nil)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	path, err := pt.ParallelShortestPath(node.ID, node.ID, 5)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(path) != 1 || path[0] != node.ID {
		t.Errorf("Expected path [%d], got %v", node.ID, path)
	}
}

// TestParallelShortestPath_DirectConnection tests direct path
func TestParallelShortestPath_DirectConnection(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	path, err := pt.ParallelShortestPath(nodeA.ID, nodeB.ID, 5)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(path) != 2 {
		t.Errorf("Expected path length 2, got %d: %v", len(path), path)
	}

	if path[0] != nodeA.ID || path[1] != nodeB.ID {
		t.Errorf("Expected path [%d, %d], got %v", nodeA.ID, nodeB.ID, path)
	}
}

// TestParallelShortestPath_LinearPath tests path through multiple nodes
func TestParallelShortestPath_LinearPath(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	path, err := pt.ParallelShortestPath(nodeA.ID, nodeC.ID, 5)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d: %v", len(path), path)
	}

	// Verify path order
	if path[0] != nodeA.ID || path[1] != nodeB.ID || path[2] != nodeC.ID {
		t.Errorf("Expected path [%d, %d, %d], got %v", nodeA.ID, nodeB.ID, nodeC.ID, path)
	}
}

// TestParallelShortestPath_NoPath tests when no path exists
func TestParallelShortestPath_NoPath(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	// No edges between A and B

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	path, err := pt.ParallelShortestPath(nodeA.ID, nodeB.ID, 5)

	if err != storage.ErrNodeNotFound {
		t.Errorf("Expected ErrNodeNotFound, got %v", err)
	}

	if path != nil {
		t.Errorf("Expected nil path, got %v", path)
	}
}

// TestParallelShortestPath_MaxDepth tests max depth limit
func TestParallelShortestPath_MaxDepth(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create long chain: A -> B -> C -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	// With maxDepth=2, can only reach B and C, not D
	path, err := pt.ParallelShortestPath(nodeA.ID, nodeD.ID, 2)

	if err != storage.ErrNodeNotFound {
		t.Errorf("Expected ErrNodeNotFound due to depth limit, got %v", err)
	}

	if path != nil {
		t.Errorf("Expected nil path due to depth limit, got %v", path)
	}
}

// TestParallelShortestPath_MultiplePaths tests shortest among multiple paths
func TestParallelShortestPath_MultiplePaths(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	// Create diamond: A -> B -> D, A -> C -> D
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeA.ID, nodeC.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeD.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "LINKS", nil, 1.0)

	pt, _ := NewParallelTraverser(gs, 2)
	defer pt.Close()

	path, err := pt.ParallelShortestPath(nodeA.ID, nodeD.ID, 5)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Both paths have length 3, either is correct
	if len(path) != 3 {
		t.Errorf("Expected path length 3, got %d: %v", len(path), path)
	}

	if path[0] != nodeA.ID || path[2] != nodeD.ID {
		t.Errorf("Expected path from A to D, got %v", path)
	}
}

// TestClose tests closing the traverser
func TestClose(t *testing.T) {
	gs := setupTraverseTestGraph(t)

	pt, _ := NewParallelTraverser(gs, 2)
	pt.Close()

	// Closing twice should not panic
	pt.Close()
}
