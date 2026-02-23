package algorithms

import (
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupKHopTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "khop-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}
	t.Cleanup(func() { gs.Close() })
	return gs
}

func TestKHop_EmptyGraph(t *testing.T) {
	gs := setupKHopTestGraph(t)
	a, _ := gs.CreateNode([]string{"Node"}, nil)

	result, err := KHopNeighbours(gs, a.ID, DefaultKHopOptions())
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	if result.TotalReachable != 0 {
		t.Errorf("Expected 0 reachable, got %d", result.TotalReachable)
	}
}

func TestKHop_LinearChain(t *testing.T) {
	gs := setupKHopTestGraph(t)

	// A -> B -> C -> D (chain, 3 hops from A)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, d.ID, "LINKS", nil, 1.0)

	opts := DefaultKHopOptions()
	opts.MaxHops = 2
	opts.Direction = DirectionOut

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	// 2 hops: B (hop 1), C (hop 2). D is at hop 3, excluded.
	if result.TotalReachable != 2 {
		t.Errorf("Expected 2 reachable, got %d", result.TotalReachable)
	}

	if len(result.ByHop[1]) != 1 || result.ByHop[1][0] != b.ID {
		t.Errorf("Hop 1: expected [%d], got %v", b.ID, result.ByHop[1])
	}
	if len(result.ByHop[2]) != 1 || result.ByHop[2][0] != c.ID {
		t.Errorf("Hop 2: expected [%d], got %v", c.ID, result.ByHop[2])
	}

	if result.Distances[b.ID] != 1 {
		t.Errorf("Distance to B: expected 1, got %d", result.Distances[b.ID])
	}
	if result.Distances[c.ID] != 2 {
		t.Errorf("Distance to C: expected 2, got %d", result.Distances[c.ID])
	}
}

func TestKHop_DirectionIn(t *testing.T) {
	gs := setupKHopTestGraph(t)

	// B -> A, C -> B (following incoming to A reaches B at hop 1, C at hop 2)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, b.ID, "LINKS", nil, 1.0)

	opts := DefaultKHopOptions()
	opts.MaxHops = 2
	opts.Direction = DirectionIn

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	if result.TotalReachable != 2 {
		t.Errorf("Expected 2 reachable, got %d", result.TotalReachable)
	}
	if result.Distances[b.ID] != 1 {
		t.Errorf("B distance: expected 1, got %d", result.Distances[b.ID])
	}
	if result.Distances[c.ID] != 2 {
		t.Errorf("C distance: expected 2, got %d", result.Distances[c.ID])
	}
}

func TestKHop_DirectionBoth(t *testing.T) {
	gs := setupKHopTestGraph(t)

	// A -> B, C -> A (both B and C reachable from A with DirectionBoth at hop 1)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)

	opts := DefaultKHopOptions()
	opts.MaxHops = 1
	opts.Direction = DirectionBoth

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	if result.TotalReachable != 2 {
		t.Errorf("Expected 2 reachable, got %d", result.TotalReachable)
	}
}

func TestKHop_MaxResults(t *testing.T) {
	gs := setupKHopTestGraph(t)

	// Star: A -> B, A -> C, A -> D, A -> E
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	gs.CreateNode([]string{"Node"}, nil) // B
	gs.CreateNode([]string{"Node"}, nil) // C
	gs.CreateNode([]string{"Node"}, nil) // D
	gs.CreateNode([]string{"Node"}, nil) // E

	for i := uint64(2); i <= 5; i++ {
		gs.CreateEdge(a.ID, i, "LINKS", nil, 1.0)
	}

	opts := DefaultKHopOptions()
	opts.MaxHops = 1
	opts.Direction = DirectionOut
	opts.MaxResults = 2

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	if result.TotalReachable != 2 {
		t.Errorf("MaxResults=2 but got %d reachable", result.TotalReachable)
	}
}

func TestKHop_EdgeTypeFilter(t *testing.T) {
	gs := setupKHopTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "FRIEND", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "ENEMY", nil, 1.0)

	opts := DefaultKHopOptions()
	opts.MaxHops = 1
	opts.Direction = DirectionOut
	opts.EdgeTypes = []string{"FRIEND"}

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	if result.TotalReachable != 1 {
		t.Errorf("Expected 1 FRIEND neighbor, got %d", result.TotalReachable)
	}
	if result.Distances[b.ID] != 1 {
		t.Error("Expected B in results")
	}
	if _, exists := result.Distances[c.ID]; exists {
		t.Error("C (ENEMY) should be filtered out")
	}
}

func TestKHop_SourceNotIncluded(t *testing.T) {
	gs := setupKHopTestGraph(t)

	// Cycle: A -> B -> A
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)

	opts := DefaultKHopOptions()
	opts.MaxHops = 3
	opts.Direction = DirectionOut

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	if _, exists := result.Distances[a.ID]; exists {
		t.Error("Source node should not be in results")
	}
	if result.TotalReachable != 1 {
		t.Errorf("Expected 1 reachable (B only), got %d", result.TotalReachable)
	}
}

func TestKHop_InvalidMaxHops(t *testing.T) {
	gs := setupKHopTestGraph(t)
	a, _ := gs.CreateNode([]string{"Node"}, nil)

	opts := DefaultKHopOptions()
	opts.MaxHops = 0

	_, err := KHopNeighbours(gs, a.ID, opts)
	if err == nil {
		t.Error("Expected error for MaxHops=0")
	}
}

func TestKHop_LargerBFS(t *testing.T) {
	gs := setupKHopTestGraph(t)

	// Binary tree: A -> B, A -> C, B -> D, B -> E, C -> F, C -> G
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)
	f, _ := gs.CreateNode([]string{"Node"}, nil)
	g, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, e.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, f.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, g.ID, "LINKS", nil, 1.0)

	opts := DefaultKHopOptions()
	opts.MaxHops = 3
	opts.Direction = DirectionOut

	result, err := KHopNeighbours(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	// Hop 1: B, C. Hop 2: D, E, F, G. Total: 6
	if result.TotalReachable != 6 {
		t.Errorf("Expected 6 reachable, got %d", result.TotalReachable)
	}
	if len(result.ByHop[1]) != 2 {
		t.Errorf("Hop 1: expected 2 nodes, got %d", len(result.ByHop[1]))
	}
	if len(result.ByHop[2]) != 4 {
		t.Errorf("Hop 2: expected 4 nodes, got %d", len(result.ByHop[2]))
	}

	_ = d
	_ = e
	_ = f
	_ = g
}
