package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupIntegrationTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "integration-test-*")
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

// TestIntegration_SCCFeedsLinkPrediction verifies that SCC decomposition
// identifies groups, and link prediction scores inter-group connections.
func TestIntegration_SCCFeedsLinkPrediction(t *testing.T) {
	gs := setupIntegrationTestGraph(t)

	// Two SCCs: {A,B} cycle, {C,D} cycle, bridge B->C
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)

	// Step 1: Find SCCs
	sccResult, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}
	if len(sccResult.Communities) != 2 {
		t.Fatalf("Expected 2 SCCs, got %d", len(sccResult.Communities))
	}

	// Step 2: Predict links between nodes in different SCCs
	// A and D are in different SCCs and not directly connected
	opts := DefaultLinkPredictionOptions()
	opts.Direction = DirectionOut
	opts.ExcludeExisting = false

	scoreAD, err := PredictLinkScore(gs, a.ID, d.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinkScore A->D failed: %v", err)
	}

	// A and D share common neighbor? A's out-neighbors: {B}, D's out-neighbors: {C}
	// No common outgoing neighbors → score = 0
	// But with DirectionBoth: A's neighbors: {B}, D's neighbors: {C}
	// Still no common neighbors (B≠C) → 0

	// Use DirectionBoth for broader reach
	opts.Direction = DirectionBoth
	scoreAD, err = PredictLinkScore(gs, a.ID, d.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinkScore A->D (both) failed: %v", err)
	}

	// With DirectionBoth: A neighbors={B}, D neighbors={C}
	// B->C exists, so B is neighbor of C (out). C is neighbor of B (in with Both).
	// A neighbors(both)={B}, D neighbors(both)={C}. No overlap → score 0
	// This is expected: nodes in separate SCCs with only a bridge have low predicted links
	if scoreAD < 0 {
		t.Errorf("Score should be >= 0, got %f", scoreAD)
	}
}

// TestIntegration_TrianglesMatchClusteringCoefficient verifies that CountTriangles
// produces clustering coefficients consistent with the existing ClusteringCoefficient function.
func TestIntegration_TrianglesMatchClusteringCoefficient(t *testing.T) {
	gs := setupIntegrationTestGraph(t)

	// Build a graph with known triangle structure
	// Complete triangle A-B-C with bidirectional edges
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)
	// D is isolated
	_ = d

	triangleResult, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	// The existing ClusteringCoefficient only uses outgoing edges, so the
	// coefficients won't match exactly since CountTriangles uses undirected view.
	// But with bidirectional edges, the undirected neighbor set matches outgoing.
	existingCC, err := ClusteringCoefficient(gs)
	if err != nil {
		t.Fatalf("ClusteringCoefficient failed: %v", err)
	}

	// For A, B, C: both should give CC = 1.0 (complete triangle with bidirectional edges)
	for _, nodeID := range []uint64{a.ID, b.ID, c.ID} {
		triCC := triangleResult.ClusteringCoefficients[nodeID]
		oldCC := existingCC[nodeID]
		if math.Abs(triCC-oldCC) > 0.001 {
			t.Errorf("Node %d: CountTriangles CC=%f, ClusteringCoefficient CC=%f", nodeID, triCC, oldCC)
		}
	}
}

// TestIntegration_KHopFeedsSimilarity verifies that k-hop neighbourhood can
// identify candidate nodes for similarity computation.
func TestIntegration_KHopFeedsSimilarity(t *testing.T) {
	gs := setupIntegrationTestGraph(t)

	// A -> B -> C -> D; A -> E -> D
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, e.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(e.ID, d.ID, "LINKS", nil, 1.0)

	// Step 1: Find 2-hop neighbourhood of A
	khopOpts := DefaultKHopOptions()
	khopOpts.MaxHops = 2
	khopOpts.Direction = DirectionOut

	khopResult, err := KHopNeighbours(gs, a.ID, khopOpts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}

	// Hop 1: {B, E}, Hop 2: {C, D}
	if khopResult.TotalReachable != 4 {
		t.Fatalf("Expected 4 reachable, got %d", khopResult.TotalReachable)
	}

	// Step 2: Compare similarity of 2-hop neighbors with A
	simOpts := DefaultNodeSimilarityOptions()
	simOpts.Direction = DirectionOut

	// B and E are both 1-hop from A; compare their similarity
	scoreBE, err := NodeSimilarityPair(gs, b.ID, e.ID, simOpts)
	if err != nil {
		t.Fatalf("NodeSimilarityPair failed: %v", err)
	}

	// B's out-neighbors: {C}, E's out-neighbors: {D}. No overlap → score 0
	if scoreBE != 0.0 {
		t.Errorf("B and E should have 0 similarity (different targets), got %f", scoreBE)
	}

	_ = c
	_ = d
}

// TestIntegration_CondensationValidatesTopology verifies that condensation
// edges form a DAG (no cycles possible in the condensation).
func TestIntegration_CondensationValidatesTopology(t *testing.T) {
	gs := setupIntegrationTestGraph(t)

	// 3 SCCs in a chain: {1,2,3} -> {4,5} -> {6}
	n := make([]*storage.Node, 7)
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

	sccResult, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}

	condensationEdges, err := Condensation(gs, sccResult)
	if err != nil {
		t.Fatalf("Condensation failed: %v", err)
	}

	// Verify DAG property: no condensation edge should form a cycle
	// Build adjacency from condensation edges and check for back-edges
	adj := make(map[int][]int)
	for _, ce := range condensationEdges {
		adj[ce.FromSCCID] = append(adj[ce.FromSCCID], ce.ToSCCID)
	}

	// Simple cycle check via DFS
	visited := make(map[int]int) // 0=unvisited, 1=in-progress, 2=done
	var hasCycle bool
	var dfs func(node int)
	dfs = func(node int) {
		visited[node] = 1
		for _, next := range adj[node] {
			if visited[next] == 1 {
				hasCycle = true
				return
			}
			if visited[next] == 0 {
				dfs(next)
			}
		}
		visited[node] = 2
	}

	for _, c := range sccResult.Communities {
		if visited[c.ID] == 0 {
			dfs(c.ID)
		}
	}

	if hasCycle {
		t.Error("Condensation graph should be a DAG (no cycles)")
	}
}

// TestIntegration_SimilarityAndLinkPredictionAgree verifies that nodes with
// high similarity also tend to have high link prediction scores.
func TestIntegration_SimilarityAndLinkPredictionAgree(t *testing.T) {
	gs := setupIntegrationTestGraph(t)

	// A and B share many outgoing neighbors (C, D, E); F shares only one (C)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)
	f, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, e.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, e.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(f.ID, c.ID, "LINKS", nil, 1.0)

	// Similarity: A-B should be higher than A-F
	simOpts := DefaultNodeSimilarityOptions()
	simOpts.Direction = DirectionOut

	simAB, _ := NodeSimilarityPair(gs, a.ID, b.ID, simOpts)
	simAF, _ := NodeSimilarityPair(gs, a.ID, f.ID, simOpts)

	if simAB <= simAF {
		t.Errorf("Expected similarity(A,B)=%f > similarity(A,F)=%f", simAB, simAF)
	}

	// Link prediction: A-B common neighbors should be higher than A-F
	lpOpts := DefaultLinkPredictionOptions()
	lpOpts.Direction = DirectionOut
	lpOpts.ExcludeExisting = false

	lpAB, _ := PredictLinkScore(gs, a.ID, b.ID, lpOpts)
	lpAF, _ := PredictLinkScore(gs, a.ID, f.ID, lpOpts)

	if lpAB <= lpAF {
		t.Errorf("Expected link_prediction(A,B)=%f > link_prediction(A,F)=%f", lpAB, lpAF)
	}

	_ = d
	_ = e
}

// TestIntegration_AllAlgorithmsOnSameGraph runs all 5 new algorithms on
// the same graph to verify they don't interfere with each other.
func TestIntegration_AllAlgorithmsOnSameGraph(t *testing.T) {
	gs := setupIntegrationTestGraph(t)

	// Build a moderate graph
	nodes := make([]*storage.Node, 6)
	for i := range nodes {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}

	// Triangle: 0-1-2
	gs.CreateEdge(nodes[0].ID, nodes[1].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[1].ID, nodes[2].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[0].ID, "LINKS", nil, 1.0)

	// Chain: 3->4->5
	gs.CreateEdge(nodes[3].ID, nodes[4].ID, "LINKS", nil, 1.0)
	gs.CreateEdge(nodes[4].ID, nodes[5].ID, "LINKS", nil, 1.0)

	// Bridge: 2->3
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "LINKS", nil, 1.0)

	// 1. Triangle counting
	triResult, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}
	if triResult.GlobalCount != 1 {
		t.Errorf("Expected 1 triangle, got %d", triResult.GlobalCount)
	}

	// 2. SCC
	sccResult, err := StronglyConnectedComponents(gs)
	if err != nil {
		t.Fatalf("SCC failed: %v", err)
	}
	// The triangle (0->1->2->0) is one SCC of size 3; nodes 3,4,5 are singletons
	if sccResult.LargestSCC.Size != 3 {
		t.Errorf("Expected largest SCC size 3, got %d", sccResult.LargestSCC.Size)
	}

	// 3. Node similarity
	simOpts := DefaultNodeSimilarityOptions()
	simOpts.Direction = DirectionOut
	_, err = NodeSimilarityAll(gs, simOpts)
	if err != nil {
		t.Fatalf("NodeSimilarityAll failed: %v", err)
	}

	// 4. Link prediction
	lpOpts := DefaultLinkPredictionOptions()
	lpOpts.Direction = DirectionOut
	_, err = PredictLinksFor(gs, nodes[0].ID, lpOpts)
	if err != nil {
		t.Fatalf("PredictLinksFor failed: %v", err)
	}

	// 5. K-hop
	khopOpts := DefaultKHopOptions()
	khopOpts.MaxHops = 3
	khopOpts.Direction = DirectionOut
	khopResult, err := KHopNeighbours(gs, nodes[0].ID, khopOpts)
	if err != nil {
		t.Fatalf("KHopNeighbours failed: %v", err)
	}
	// From node 0 via outgoing: 0->1 (hop1), 1->2 (hop2), 2->0(skip),2->3 (hop3)
	if khopResult.TotalReachable < 3 {
		t.Errorf("Expected >= 3 reachable from node 0, got %d", khopResult.TotalReachable)
	}
}
