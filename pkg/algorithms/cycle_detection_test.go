package algorithms

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestDetectCycles_NoCycles tests a graph with no cycles (linear path)
func TestDetectCycles_NoCycles(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// A -> B -> C (linear, no cycle)
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) != 0 {
		t.Errorf("Expected no cycles, got %d", len(cycles))
	}
}

// TestDetectCycles_SimpleCycle tests a simple 2-node cycle
func TestDetectCycles_SimpleCycle(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// A -> B -> A (simple 2-node cycle)
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d", len(cycles))
	}
	if len(cycles) > 0 && len(cycles[0]) != 2 {
		t.Errorf("Expected cycle length 2, got %d", len(cycles[0]))
	}
}

// TestDetectCycles_SelfLoop tests a self-referencing node
func TestDetectCycles_SelfLoop(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// A -> A (self loop)
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeA.ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d", len(cycles))
	}
	if len(cycles) > 0 && len(cycles[0]) != 1 {
		t.Errorf("Expected cycle length 1, got %d", len(cycles[0]))
	}
}

// TestDetectCycles_TriangleCycle tests a 3-node cycle
func TestDetectCycles_TriangleCycle(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// A -> B -> C -> A
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d", len(cycles))
	}
	if len(cycles) > 0 && len(cycles[0]) != 3 {
		t.Errorf("Expected cycle length 3, got %d", len(cycles[0]))
	}
}

// TestDetectCycles_MultipleCycles tests detection of multiple independent cycles
func TestDetectCycles_MultipleCycles(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Cycle 1: A -> B -> A
	// Cycle 2: C -> D -> E -> C
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeE, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeE.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeE.ID, nodeC.ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) < 2 {
		t.Errorf("Expected at least 2 cycles, got %d", len(cycles))
	}
}

// TestDetectCycles_ComplexGraph tests cycle detection in a complex graph
func TestDetectCycles_ComplexGraph(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Mixed: some cycles, some acyclic paths
	//     1 -> 2 -> 3
	//     ^    |    |
	//     |    v    v
	//     5 <- 4 <- 6
	nodes := make([]*storage.Node, 7)
	for i := 1; i <= 6; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[4].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[3].ID, nodes[6].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[4].ID, nodes[5].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[5].ID, nodes[1].ID, "E", nil, 1.0) // Creates cycle: 1->2->4->5->1
	gs.CreateEdge(nodes[6].ID, nodes[4].ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) == 0 {
		t.Error("Expected to find cycles in complex graph")
	}
}

// TestDetectCycles_EmptyGraph tests an empty graph
func TestDetectCycles_EmptyGraph(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) != 0 {
		t.Errorf("Expected no cycles in empty graph, got %d", len(cycles))
	}
}

// TestDetectCycles_SingleNode tests a graph with a single node and no edges
func TestDetectCycles_SingleNode(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	gs.CreateNode([]string{"Node"}, nil)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}
	if len(cycles) != 0 {
		t.Errorf("Expected no cycles for single node, got %d", len(cycles))
	}
}

// TestDetectCyclesWithOptions_MinLength tests filtering by minimum cycle length
func TestDetectCyclesWithOptions_MinLength(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create self-loop (length 1)
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeA.ID, "E", nil, 1.0)

	// Create triangle (length 3)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeD.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeD.ID, nodeB.ID, "E", nil, 1.0)

	opts := CycleDetectionOptions{MinCycleLength: 3}
	cycles, err := DetectCyclesWithOptions(gs, opts)
	if err != nil {
		t.Fatalf("DetectCyclesWithOptions failed: %v", err)
	}

	// Should only get the triangle, not the self-loop
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d", len(cycles))
	}
	if len(cycles) > 0 && len(cycles[0]) != 3 {
		t.Errorf("Expected cycle length 3, got %d", len(cycles[0]))
	}
}

// TestDetectCyclesWithOptions_MaxLength tests filtering by maximum cycle length
func TestDetectCyclesWithOptions_MaxLength(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create 2-node cycle
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "E", nil, 1.0)

	// Create 4-node cycle
	nodes := make([]*storage.Node, 4)
	for i := 0; i < 4; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}
	gs.CreateEdge(nodes[0].ID, nodes[1].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "E", nil, 1.0)
	gs.CreateEdge(nodes[3].ID, nodes[0].ID, "E", nil, 1.0)

	opts := CycleDetectionOptions{MaxCycleLength: 2}
	cycles, err := DetectCyclesWithOptions(gs, opts)
	if err != nil {
		t.Fatalf("DetectCyclesWithOptions failed: %v", err)
	}

	// Should only get the 2-node cycle
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d", len(cycles))
	}
	if len(cycles) > 0 && len(cycles[0]) != 2 {
		t.Errorf("Expected cycle length 2, got %d", len(cycles[0]))
	}
}

// TestDetectCyclesWithOptions_NodePredicate tests filtering by node properties
func TestDetectCyclesWithOptions_NodePredicate(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create cycle with Router nodes
	router1, _ := gs.CreateNode([]string{"Router"}, map[string]storage.Value{
		"priority": storage.IntValue(1),
	})
	router2, _ := gs.CreateNode([]string{"Router"}, map[string]storage.Value{
		"priority": storage.IntValue(1),
	})
	gs.CreateEdge(router1.ID, router2.ID, "E", nil, 1.0)
	gs.CreateEdge(router2.ID, router1.ID, "E", nil, 1.0)

	// Create cycle with Server nodes
	server1, _ := gs.CreateNode([]string{"Server"}, nil)
	server2, _ := gs.CreateNode([]string{"Server"}, nil)
	gs.CreateEdge(server1.ID, server2.ID, "E", nil, 1.0)
	gs.CreateEdge(server2.ID, server1.ID, "E", nil, 1.0)

	// Filter for only Router nodes
	opts := CycleDetectionOptions{
		NodePredicate: func(n *storage.Node) bool {
			return n.HasLabel("Router")
		},
	}
	cycles, err := DetectCyclesWithOptions(gs, opts)
	if err != nil {
		t.Fatalf("DetectCyclesWithOptions failed: %v", err)
	}

	// Should only get the Router cycle
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d", len(cycles))
	}
}

// TestAnalyzeCycles tests cycle statistics computation
func TestAnalyzeCycles(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	// Create various cycles
	// Self-loop
	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeA.ID, "E", nil, 1.0)

	// 2-node cycle
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeC, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeB.ID, "E", nil, 1.0)

	// 3-node cycle
	nodeD, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeE, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeF, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeD.ID, nodeE.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeE.ID, nodeF.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeF.ID, nodeD.ID, "E", nil, 1.0)

	cycles, err := DetectCycles(gs)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}

	stats := AnalyzeCycles(cycles)

	if stats.TotalCycles != 3 {
		t.Errorf("Expected 3 total cycles, got %d", stats.TotalCycles)
	}
	if stats.ShortestCycle != 1 {
		t.Errorf("Expected shortest cycle 1, got %d", stats.ShortestCycle)
	}
	if stats.LongestCycle != 3 {
		t.Errorf("Expected longest cycle 3, got %d", stats.LongestCycle)
	}
	if stats.SelfLoops != 1 {
		t.Errorf("Expected 1 self-loop, got %d", stats.SelfLoops)
	}

	expectedAvg := (1.0 + 2.0 + 3.0) / 3.0
	if stats.AverageLength != expectedAvg {
		t.Errorf("Expected average length %.2f, got %.2f", expectedAvg, stats.AverageLength)
	}
}

// TestAnalyzeCycles_Empty tests statistics on empty cycle list
func TestAnalyzeCycles_Empty(t *testing.T) {
	stats := AnalyzeCycles([]Cycle{})

	if stats.TotalCycles != 0 {
		t.Errorf("Expected 0 total cycles, got %d", stats.TotalCycles)
	}
	if stats.AverageLength != 0 {
		t.Errorf("Expected 0 average length, got %.2f", stats.AverageLength)
	}
}

// TestHasCycle_True tests detection of cycle existence
func TestHasCycle_True(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeA.ID, "E", nil, 1.0)

	hasCycle, err := HasCycle(gs)
	if err != nil {
		t.Fatalf("HasCycle failed: %v", err)
	}
	if !hasCycle {
		t.Error("Expected HasCycle to return true")
	}
}

// TestHasCycle_False tests when no cycles exist
func TestHasCycle_False(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	nodeA, _ := gs.CreateNode([]string{"Node"}, nil)
	nodeB, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateEdge(nodeA.ID, nodeB.ID, "E", nil, 1.0)

	hasCycle, err := HasCycle(gs)
	if err != nil {
		t.Fatalf("HasCycle failed: %v", err)
	}
	if hasCycle {
		t.Error("Expected HasCycle to return false")
	}
}

// TestHasCycle_EmptyGraph tests empty graph
func TestHasCycle_EmptyGraph(t *testing.T) {
	gs := setupTestGraph(t)
	defer gs.Close()

	hasCycle, err := HasCycle(gs)
	if err != nil {
		t.Fatalf("HasCycle failed: %v", err)
	}
	if hasCycle {
		t.Error("Expected HasCycle to return false for empty graph")
	}
}

// Benchmarks

// BenchmarkDetectCycles benchmarks cycle detection
func BenchmarkDetectCycles(b *testing.B) {
	gs, _ := storage.NewGraphStorage(b.TempDir())
	defer gs.Close()

	// Create graph with 100 nodes and some cycles
	nodes := make([]*storage.Node, 100)
	for i := 0; i < 100; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	// Create cycles every 10 nodes
	for i := 0; i < 90; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "E", nil, 1.0)
		if i%10 == 9 {
			gs.CreateEdge(nodes[i+1].ID, nodes[i-9].ID, "E", nil, 1.0)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectCycles(gs)
	}
}

// BenchmarkHasCycle benchmarks cycle existence check
func BenchmarkHasCycle(b *testing.B) {
	gs, _ := storage.NewGraphStorage(b.TempDir())
	defer gs.Close()

	// Create graph with 100 nodes and a cycle
	nodes := make([]*storage.Node, 100)
	for i := 0; i < 100; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	// Create linear chain with one cycle at the end
	for i := 0; i < 99; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "E", nil, 1.0)
	}
	gs.CreateEdge(nodes[99].ID, nodes[90].ID, "E", nil, 1.0) // Create cycle

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HasCycle(gs)
	}
}

// BenchmarkDetectCycles_LargeGraph benchmarks on a larger graph
func BenchmarkDetectCycles_LargeGraph(b *testing.B) {
	gs, _ := storage.NewGraphStorage(b.TempDir())
	defer gs.Close()

	// Create graph with 1000 nodes
	nodes := make([]*storage.Node, 1000)
	for i := 0; i < 1000; i++ {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	// Create cycles every 50 nodes
	for i := 0; i < 950; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "E", nil, 1.0)
		if i%50 == 49 {
			gs.CreateEdge(nodes[i+1].ID, nodes[i-49].ID, "E", nil, 1.0)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DetectCycles(gs)
	}
}
