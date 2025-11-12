package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// Helper to create a test graph
// Graph structure:
//     1 -> 2 -> 4
//     |    |
//     v    v
//     3 -> 5
func setupTraversalTestGraph(t *testing.T) (*storage.GraphStorage, func()) {
	tempDir := filepath.Join(os.TempDir(), "traversal_test_"+t.Name())
	gs, err := storage.NewGraphStorage(tempDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
	})
	node4, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("David"),
	})
	node5, _ := gs.CreateNode([]string{"Company"}, map[string]storage.Value{
		"name": storage.StringValue("TechCorp"),
	})

	// Create edges
	// 1 -> 2 (type: KNOWS)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	// 1 -> 3 (type: KNOWS)
	gs.CreateEdge(node1.ID, node3.ID, "KNOWS", nil, 1.0)
	// 2 -> 4 (type: KNOWS)
	gs.CreateEdge(node2.ID, node4.ID, "KNOWS", nil, 1.0)
	// 2 -> 5 (type: WORKS_AT)
	gs.CreateEdge(node2.ID, node5.ID, "WORKS_AT", nil, 1.0)
	// 3 -> 5 (type: WORKS_AT)
	gs.CreateEdge(node3.ID, node5.ID, "WORKS_AT", nil, 1.0)

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tempDir)
	}

	return gs, cleanup
}

// TestNewTraverser tests traverser creation
func TestNewTraverser(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "traverser_test_new")
	defer os.RemoveAll(tempDir)
	gs, _ := storage.NewGraphStorage(tempDir)
	defer gs.Close()

	traverser := NewTraverser(gs)

	if traverser == nil {
		t.Fatal("Expected traverser to be created")
	}

	if traverser.storage != gs {
		t.Error("Traverser should reference the storage")
	}
}

// TestBFS tests breadth-first search
func TestBFS(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	tests := []struct {
		name          string
		startNodeID   uint64
		maxDepth      int
		maxResults    int
		expectedCount int
	}{
		{
			name:          "BFS from node 1, depth 1",
			startNodeID:   1,
			maxDepth:      1,
			maxResults:    100,
			expectedCount: 3, // 1, 2, 3
		},
		{
			name:          "BFS from node 1, depth 2",
			startNodeID:   1,
			maxDepth:      2,
			maxResults:    100,
			expectedCount: 5, // All nodes
		},
		{
			name:          "BFS from node 1, depth 0",
			startNodeID:   1,
			maxDepth:      0,
			maxResults:    100,
			expectedCount: 1, // Only start node
		},
		{
			name:          "BFS with max results",
			startNodeID:   1,
			maxDepth:      10,
			maxResults:    2,
			expectedCount: 2, // Limited by maxResults
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := traverser.BFS(TraversalOptions{
				StartNodeID: tt.startNodeID,
				Direction:   DirectionOutgoing,
				EdgeTypes:   []string{},
				MaxDepth:    tt.maxDepth,
				MaxResults:  tt.maxResults,
			})

			if err != nil {
				t.Fatalf("BFS failed: %v", err)
			}

			if len(result.Nodes) != tt.expectedCount {
				t.Errorf("Expected %d nodes, got %d", tt.expectedCount, len(result.Nodes))
			}
		})
	}
}

// TestBFS_WithPredicate tests BFS with node filter
func TestBFS_WithPredicate(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Filter only Person nodes
	predicate := func(n *storage.Node) bool {
		return n.HasLabel("Person")
	}

	result, err := traverser.BFS(TraversalOptions{
		StartNodeID: 1,
		Direction:   DirectionOutgoing,
		EdgeTypes:   []string{},
		MaxDepth:    10,
		MaxResults:  100,
		Predicate:   predicate,
	})

	if err != nil {
		t.Fatalf("BFS failed: %v", err)
	}

	// Should get nodes 1, 2, 3, 4 (all Person nodes, not Company node 5)
	if len(result.Nodes) != 4 {
		t.Errorf("Expected 4 Person nodes, got %d", len(result.Nodes))
	}

	// Verify all are Person nodes
	for _, node := range result.Nodes {
		if !node.HasLabel("Person") {
			t.Error("Found non-Person node in filtered results")
		}
	}
}

// TestBFS_WithEdgeTypeFilter tests BFS with edge type filtering
func TestBFS_WithEdgeTypeFilter(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Only follow KNOWS edges
	result, err := traverser.BFS(TraversalOptions{
		StartNodeID: 1,
		Direction:   DirectionOutgoing,
		EdgeTypes:   []string{"KNOWS"},
		MaxDepth:    10,
		MaxResults:  100,
	})

	if err != nil {
		t.Fatalf("BFS failed: %v", err)
	}

	// Should get nodes 1, 2, 3, 4 (not 5, which is only reachable via WORKS_AT)
	if len(result.Nodes) != 4 {
		t.Errorf("Expected 4 nodes via KNOWS edges, got %d", len(result.Nodes))
	}
}

// TestDFS tests depth-first search
func TestDFS(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	tests := []struct {
		name          string
		startNodeID   uint64
		maxDepth      int
		maxResults    int
		expectedCount int
	}{
		{
			name:          "DFS from node 1, depth 1",
			startNodeID:   1,
			maxDepth:      1,
			maxResults:    100,
			expectedCount: 3, // 1, 2, 3
		},
		{
			name:          "DFS from node 1, depth 2",
			startNodeID:   1,
			maxDepth:      2,
			maxResults:    100,
			expectedCount: 5, // All nodes
		},
		{
			name:          "DFS with max results",
			startNodeID:   1,
			maxDepth:      10,
			maxResults:    2,
			expectedCount: 2, // Limited by maxResults
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := traverser.DFS(TraversalOptions{
				StartNodeID: tt.startNodeID,
				Direction:   DirectionOutgoing,
				EdgeTypes:   []string{},
				MaxDepth:    tt.maxDepth,
				MaxResults:  tt.maxResults,
			})

			if err != nil {
				t.Fatalf("DFS failed: %v", err)
			}

			if len(result.Nodes) != tt.expectedCount {
				t.Errorf("Expected %d nodes, got %d", tt.expectedCount, len(result.Nodes))
			}
		})
	}
}

// TestFindShortestPath tests shortest path finding
func TestFindShortestPath(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	tests := []struct {
		name               string
		fromID             uint64
		toID               uint64
		edgeTypes          []string
		shouldFindPath     bool
		expectedPathLength int // number of nodes
	}{
		{
			name:               "path from 1 to 4",
			fromID:             1,
			toID:               4,
			edgeTypes:          []string{},
			shouldFindPath:     true,
			expectedPathLength: 3, // 1 -> 2 -> 4
		},
		{
			name:               "path from 1 to 5",
			fromID:             1,
			toID:               5,
			edgeTypes:          []string{},
			shouldFindPath:     true,
			expectedPathLength: 3, // 1 -> 2/3 -> 5
		},
		{
			name:               "same node",
			fromID:             1,
			toID:               1,
			edgeTypes:          []string{},
			shouldFindPath:     true,
			expectedPathLength: 1, // Just the node itself
		},
		{
			name:               "no path (wrong edge type)",
			fromID:             1,
			toID:               5,
			edgeTypes:          []string{"KNOWS"},
			shouldFindPath:     false,
			expectedPathLength: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := traverser.FindShortestPath(tt.fromID, tt.toID, tt.edgeTypes)

			if tt.shouldFindPath {
				if err != nil {
					t.Fatalf("Expected to find path, got error: %v", err)
				}

				if len(path.Nodes) != tt.expectedPathLength {
					t.Errorf("Expected path length %d, got %d", tt.expectedPathLength, len(path.Nodes))
				}

				// Verify path starts and ends correctly
				if path.Nodes[0].ID != tt.fromID {
					t.Errorf("Path should start at node %d, starts at %d", tt.fromID, path.Nodes[0].ID)
				}

				if path.Nodes[len(path.Nodes)-1].ID != tt.toID {
					t.Errorf("Path should end at node %d, ends at %d", tt.toID, path.Nodes[len(path.Nodes)-1].ID)
				}

				// Verify edges match nodes
				if len(path.Edges) != len(path.Nodes)-1 {
					t.Errorf("Expected %d edges for %d nodes, got %d edges",
						len(path.Nodes)-1, len(path.Nodes), len(path.Edges))
				}
			} else {
				if err == nil {
					t.Error("Expected error for non-existent path")
				}
			}
		})
	}
}

// TestFindAllPaths tests finding all paths
func TestFindAllPaths(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Find all paths from 1 to 5 with max depth 3
	paths, err := traverser.FindAllPaths(1, 5, 3, []string{})
	if err != nil {
		t.Fatalf("FindAllPaths failed: %v", err)
	}

	// There are 2 paths: 1->2->5 and 1->3->5
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths from 1 to 5, got %d", len(paths))
	}

	// Verify each path
	for i, path := range paths {
		if len(path.Nodes) != 3 {
			t.Errorf("Path %d: expected 3 nodes, got %d", i, len(path.Nodes))
		}

		if path.Nodes[0].ID != 1 {
			t.Errorf("Path %d: should start at node 1, starts at %d", i, path.Nodes[0].ID)
		}

		if path.Nodes[len(path.Nodes)-1].ID != 5 {
			t.Errorf("Path %d: should end at node 5, ends at %d", i, path.Nodes[len(path.Nodes)-1].ID)
		}

		// Verify edges
		if len(path.Edges) != 2 {
			t.Errorf("Path %d: expected 2 edges, got %d", i, len(path.Edges))
		}
	}
}

// TestFindAllPaths_MaxDepth tests depth limiting
func TestFindAllPaths_MaxDepth(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Find paths with insufficient depth
	paths, err := traverser.FindAllPaths(1, 5, 1, []string{})
	if err != nil {
		t.Fatalf("FindAllPaths failed: %v", err)
	}

	// Should find no paths (depth 1 is too shallow)
	if len(paths) != 0 {
		t.Errorf("Expected 0 paths with depth 1, got %d", len(paths))
	}

	// Find paths with exact depth needed
	paths, err = traverser.FindAllPaths(1, 5, 2, []string{})
	if err != nil {
		t.Fatalf("FindAllPaths failed: %v", err)
	}

	// Should find 2 paths
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths with depth 2, got %d", len(paths))
	}
}

// TestGetNeighborhood tests neighborhood retrieval
func TestGetNeighborhood(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Get 1-hop neighborhood from node 1
	nodes, err := traverser.GetNeighborhood(1, 1, DirectionOutgoing)
	if err != nil {
		t.Fatalf("GetNeighborhood failed: %v", err)
	}

	// Should get nodes 1, 2, 3 (start + 1 hop)
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes in 1-hop neighborhood, got %d", len(nodes))
	}

	// Get 2-hop neighborhood
	nodes, err = traverser.GetNeighborhood(1, 2, DirectionOutgoing)
	if err != nil {
		t.Fatalf("GetNeighborhood failed: %v", err)
	}

	// Should get all 5 nodes
	if len(nodes) != 5 {
		t.Errorf("Expected 5 nodes in 2-hop neighborhood, got %d", len(nodes))
	}
}

// TestGetNeighborhood_Incoming tests incoming direction
func TestGetNeighborhood_Incoming(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Get incoming neighbors of node 5 (nodes 2 and 3 point to it)
	nodes, err := traverser.GetNeighborhood(5, 1, DirectionIncoming)
	if err != nil {
		t.Fatalf("GetNeighborhood failed: %v", err)
	}

	// Should get nodes 5, 2, 3
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes in incoming 1-hop neighborhood, got %d", len(nodes))
	}
}

// TestGetNeighborhood_Both tests bidirectional traversal
func TestGetNeighborhood_Both(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Get bidirectional neighbors
	nodes, err := traverser.GetNeighborhood(2, 1, DirectionBoth)
	if err != nil {
		t.Fatalf("GetNeighborhood failed: %v", err)
	}

	// Should get: 2, 4, 5 (outgoing), 1 (incoming)
	// Total: 2, 1, 4, 5
	if len(nodes) < 4 {
		t.Errorf("Expected at least 4 nodes in bidirectional neighborhood, got %d", len(nodes))
	}
}

// TestBFS_IsolatedNode tests BFS on node with no edges
func TestBFS_IsolatedNode(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "traversal_test_isolated")
	defer os.RemoveAll(tempDir)
	gs, _ := storage.NewGraphStorage(tempDir)
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"Isolated"}, nil)

	traverser := NewTraverser(gs)

	result, err := traverser.BFS(TraversalOptions{
		StartNodeID: node.ID,
		Direction:   DirectionOutgoing,
		MaxDepth:    10,
		MaxResults:  100,
	})

	if err != nil {
		t.Fatalf("BFS failed: %v", err)
	}

	// Should return only the start node
	if len(result.Nodes) != 1 {
		t.Errorf("Expected 1 node (isolated), got %d", len(result.Nodes))
	}
}

// TestFindShortestPath_NonExistent tests path between disconnected nodes
func TestFindShortestPath_NonExistent(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "traversal_test_disconnected")
	defer os.RemoveAll(tempDir)
	gs, _ := storage.NewGraphStorage(tempDir)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"A"}, nil)
	node2, _ := gs.CreateNode([]string{"B"}, nil)

	traverser := NewTraverser(gs)

	_, err := traverser.FindShortestPath(node1.ID, node2.ID, []string{})
	if err == nil {
		t.Error("Expected error for disconnected nodes")
	}
}

// TestFindAllPaths_SameNode tests finding paths from node to itself
func TestFindAllPaths_SameNode(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	paths, err := traverser.FindAllPaths(1, 1, 0, []string{})
	if err != nil {
		t.Fatalf("FindAllPaths failed: %v", err)
	}

	// Should find one path containing just the start node
	if len(paths) != 1 {
		t.Errorf("Expected 1 path from node to itself, got %d", len(paths))
	}

	if len(paths) > 0 && len(paths[0].Nodes) != 1 {
		t.Errorf("Expected path with 1 node, got %d", len(paths[0].Nodes))
	}
}

// TestDirectionHandling tests different traversal directions
func TestDirectionHandling(t *testing.T) {
	gs, cleanup := setupTraversalTestGraph(t)
	defer cleanup()

	traverser := NewTraverser(gs)

	// Test outgoing from node 1
	resultOut, _ := traverser.BFS(TraversalOptions{
		StartNodeID: 1,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
		MaxResults:  100,
	})

	// Should get 1, 2, 3
	if len(resultOut.Nodes) != 3 {
		t.Errorf("Outgoing: expected 3 nodes, got %d", len(resultOut.Nodes))
	}

	// Test incoming to node 5 (has edges from 2 and 3)
	resultIn, _ := traverser.BFS(TraversalOptions{
		StartNodeID: 5,
		Direction:   DirectionIncoming,
		MaxDepth:    1,
		MaxResults:  100,
	})

	// Should get 5, 2, 3
	if len(resultIn.Nodes) != 3 {
		t.Errorf("Incoming: expected 3 nodes, got %d", len(resultIn.Nodes))
	}

	// Test both directions from node 2
	resultBoth, _ := traverser.BFS(TraversalOptions{
		StartNodeID: 2,
		Direction:   DirectionBoth,
		MaxDepth:    1,
		MaxResults:  100,
	})

	// Should get: 2 (start), 1 (incoming), 4, 5 (outgoing)
	if len(resultBoth.Nodes) < 4 {
		t.Errorf("Both: expected at least 4 nodes, got %d", len(resultBoth.Nodes))
	}
}

// Benchmarks

// setupBenchmarkGraph creates a larger graph for benchmarking
func setupBenchmarkGraph(b *testing.B) (*storage.GraphStorage, func()) {
	tempDir := filepath.Join(os.TempDir(), "traversal_bench_"+b.Name())
	gs, err := storage.NewGraphStorage(tempDir)
	if err != nil {
		b.Fatalf("Failed to create graph storage: %v", err)
	}

	// Create a graph with 100 nodes and 300 edges
	nodeIDs := make([]uint64, 100)
	for i := 0; i < 100; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	// Create edges to form a connected graph
	for i := 0; i < 100; i++ {
		// Connect to next 3 nodes (circular)
		for j := 1; j <= 3; j++ {
			target := (i + j) % 100
			gs.CreateEdge(nodeIDs[i], nodeIDs[target], "CONNECTS", map[string]storage.Value{}, 1.0)
		}
	}

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tempDir)
	}

	return gs, cleanup
}

// BenchmarkBFS benchmarks breadth-first search
func BenchmarkBFS(b *testing.B) {
	gs, cleanup := setupBenchmarkGraph(b)
	defer cleanup()

	traverser := NewTraverser(gs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traverser.BFS(TraversalOptions{
			StartNodeID: 1,
			Direction:   DirectionOutgoing,
			MaxDepth:    5,
			MaxResults:  100,
		})
	}
}

// BenchmarkDFS benchmarks depth-first search
func BenchmarkDFS(b *testing.B) {
	gs, cleanup := setupBenchmarkGraph(b)
	defer cleanup()

	traverser := NewTraverser(gs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traverser.DFS(TraversalOptions{
			StartNodeID: 1,
			Direction:   DirectionOutgoing,
			MaxDepth:    5,
			MaxResults:  100,
		})
	}
}

// BenchmarkFindShortestPath benchmarks shortest path finding
func BenchmarkFindShortestPath(b *testing.B) {
	gs, cleanup := setupBenchmarkGraph(b)
	defer cleanup()

	traverser := NewTraverser(gs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traverser.FindShortestPath(1, 50, []string{})
	}
}

// BenchmarkFindAllPaths benchmarks finding all paths
func BenchmarkFindAllPaths(b *testing.B) {
	gs, cleanup := setupBenchmarkGraph(b)
	defer cleanup()

	traverser := NewTraverser(gs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traverser.FindAllPaths(1, 10, 4, []string{})
	}
}

// BenchmarkGetNeighborhood benchmarks getting node neighborhoods
func BenchmarkGetNeighborhood(b *testing.B) {
	gs, cleanup := setupBenchmarkGraph(b)
	defer cleanup()

	traverser := NewTraverser(gs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traverser.GetNeighborhood(1, 2, DirectionOutgoing)
	}
}
