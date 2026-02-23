package algorithms

import (
	"os"
	"sort"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupSCCTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "scc-test-*")
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

func TestSCC_EmptyGraph(t *testing.T) {
	gs := setupSCCTestGraph(t)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	if len(result.Communities) != 0 {
		t.Errorf("Expected 0 SCCs, got %d", len(result.Communities))
	}
	if result.SingletonCount != 0 {
		t.Errorf("Expected 0 singletons, got %d", result.SingletonCount)
	}
}

func TestSCC_SingleNode(t *testing.T) {
	gs := setupSCCTestGraph(t)
	gs.CreateNode([]string{"Node"}, nil)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 SCC, got %d", len(result.Communities))
	}
	if result.SingletonCount != 1 {
		t.Errorf("Expected 1 singleton, got %d", result.SingletonCount)
	}
}

func TestSCC_SimpleCycle(t *testing.T) {
	gs := setupSCCTestGraph(t)

	// A -> B -> C -> A (one strongly connected component)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	if len(result.Communities) != 1 {
		t.Errorf("Expected 1 SCC, got %d", len(result.Communities))
	}
	if result.Communities[0].Size != 3 {
		t.Errorf("Expected SCC size 3, got %d", result.Communities[0].Size)
	}
	if result.SingletonCount != 0 {
		t.Errorf("Expected 0 singletons, got %d", result.SingletonCount)
	}
	if result.LargestSCC.Size != 3 {
		t.Errorf("Expected largest SCC size 3, got %d", result.LargestSCC.Size)
	}
}

func TestSCC_Chain(t *testing.T) {
	gs := setupSCCTestGraph(t)

	// A -> B -> C (no back edge, each node is its own SCC)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	if len(result.Communities) != 3 {
		t.Errorf("Expected 3 SCCs, got %d", len(result.Communities))
	}
	if result.SingletonCount != 3 {
		t.Errorf("Expected 3 singletons, got %d", result.SingletonCount)
	}

	// Each node should be in a different SCC
	if result.NodeCommunity[a.ID] == result.NodeCommunity[b.ID] {
		t.Error("A and B should be in different SCCs")
	}
}

func TestSCC_TwoComponentsWithBridge(t *testing.T) {
	gs := setupSCCTestGraph(t)

	// SCC1: A -> B -> A
	// SCC2: C -> D -> C
	// Bridge: B -> C (one-directional, not part of any cycle)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, c.ID, "LINKS", nil, 1.0)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	if len(result.Communities) != 2 {
		t.Errorf("Expected 2 SCCs, got %d", len(result.Communities))
	}

	// A and B in same SCC
	if result.NodeCommunity[a.ID] != result.NodeCommunity[b.ID] {
		t.Error("A and B should be in the same SCC")
	}

	// C and D in same SCC
	if result.NodeCommunity[c.ID] != result.NodeCommunity[d.ID] {
		t.Error("C and D should be in the same SCC")
	}

	// But SCC(A,B) != SCC(C,D)
	if result.NodeCommunity[a.ID] == result.NodeCommunity[c.ID] {
		t.Error("SCC(A,B) and SCC(C,D) should be different")
	}

	if result.LargestSCC.Size != 2 {
		t.Errorf("Expected largest SCC size 2, got %d", result.LargestSCC.Size)
	}
}

func TestSCC_Modularity(t *testing.T) {
	gs := setupSCCTestGraph(t)
	gs.CreateNode([]string{"Node"}, nil)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	// SCC should always set Modularity to 0.0
	if result.Modularity != 0.0 {
		t.Errorf("Expected Modularity 0.0, got %f", result.Modularity)
	}
}

func TestCondensation_TwoSCCs(t *testing.T) {
	gs := setupSCCTestGraph(t)

	// SCC0: A <-> B, SCC1: C <-> D, bridge B -> C
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, c.ID, "LINKS", nil, 1.0)

	sccResult, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	edges, err := Condensation(gs, sccResult)
	if err != nil {
		t.Fatalf("Condensation failed: %v", err)
	}

	// Should have exactly 1 condensation edge (from SCC(A,B) to SCC(C,D))
	if len(edges) != 1 {
		t.Fatalf("Expected 1 condensation edge, got %d", len(edges))
	}

	// The edge should have EdgeCount = 1 (just B -> C)
	if edges[0].EdgeCount != 1 {
		t.Errorf("Expected 1 original edge, got %d", edges[0].EdgeCount)
	}

	// Verify direction: from SCC containing A/B to SCC containing C/D
	sccAB := sccResult.NodeCommunity[a.ID]
	sccCD := sccResult.NodeCommunity[c.ID]
	if edges[0].FromSCCID != sccAB || edges[0].ToSCCID != sccCD {
		t.Errorf("Expected edge from SCC %d to SCC %d, got from %d to %d",
			sccAB, sccCD, edges[0].FromSCCID, edges[0].ToSCCID)
	}
}

func TestCondensation_NoInterSCCEdges(t *testing.T) {
	gs := setupSCCTestGraph(t)

	// Two isolated SCCs with no bridges
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, c.ID, "LINKS", nil, 1.0)

	sccResult, _ := StronglyConnectedComponents(gs)
	edges, err := Condensation(gs, sccResult)
	if err != nil {
		t.Fatalf("Condensation failed: %v", err)
	}

	if len(edges) != 0 {
		t.Errorf("Expected 0 condensation edges, got %d", len(edges))
	}
}

func TestSCC_ClassicExample(t *testing.T) {
	gs := setupSCCTestGraph(t)

	// Classic 8-node SCC example:
	// SCC1: {1,2,3} cycle: 1->2->3->1
	// SCC2: {4,5} cycle: 4->5->4
	// SCC3: {6} singleton
	// Bridges: 3->4, 5->6
	n := make([]*storage.Node, 7) // 1-indexed
	for i := 1; i <= 6; i++ {
		n[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	// SCC1
	gs.CreateEdge(n[1].ID, n[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(n[2].ID, n[3].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(n[3].ID, n[1].ID, "LINKS", nil, 1.0)

	// SCC2
	gs.CreateEdge(n[4].ID, n[5].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(n[5].ID, n[4].ID, "LINKS", nil, 1.0)

	// Bridges
	gs.CreateEdge(n[3].ID, n[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(n[5].ID, n[6].ID, "LINKS", nil, 1.0)

	result, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	if len(result.Communities) != 3 {
		t.Errorf("Expected 3 SCCs, got %d", len(result.Communities))
	}

	// Count by size
	sizes := make([]int, 0, len(result.Communities))
	for _, c := range result.Communities {
		sizes = append(sizes, c.Size)
	}
	sort.Ints(sizes)

	expected := []int{1, 2, 3}
	for i, s := range expected {
		if sizes[i] != s {
			t.Errorf("SCC sizes: expected %v, got %v", expected, sizes)
			break
		}
	}

	if result.SingletonCount != 1 {
		t.Errorf("Expected 1 singleton, got %d", result.SingletonCount)
	}

	if result.LargestSCC.Size != 3 {
		t.Errorf("Expected largest SCC size 3, got %d", result.LargestSCC.Size)
	}

	// Condensation should have 2 edges: SCC1->SCC2, SCC2->SCC3
	edges, err := Condensation(gs, result)
	if err != nil {
		t.Fatalf("Condensation failed: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("Expected 2 condensation edges, got %d", len(edges))
	}
}
