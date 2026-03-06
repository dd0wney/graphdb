package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupSimilarityTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "similarity-test-*")
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

func TestGetNeighborSet_DirectionOut(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0) // incoming, should be excluded

	neighbors := getNeighborSet(gs, a.ID, DirectionOut, nil)
	if len(neighbors) != 2 {
		t.Errorf("Expected 2 outgoing neighbors, got %d", len(neighbors))
	}
	if !neighbors[b.ID] || !neighbors[c.ID] {
		t.Error("Expected B and C in neighbor set")
	}
}

func TestGetNeighborSet_DirectionIn(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(b.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0) // outgoing, should be excluded

	neighbors := getNeighborSet(gs, a.ID, DirectionIn, nil)
	if len(neighbors) != 2 {
		t.Errorf("Expected 2 incoming neighbors, got %d", len(neighbors))
	}
}

func TestGetNeighborSet_DirectionBoth(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)

	neighbors := getNeighborSet(gs, a.ID, DirectionBoth, nil)
	if len(neighbors) != 2 {
		t.Errorf("Expected 2 total neighbors, got %d", len(neighbors))
	}
}

func TestGetNeighborSet_EdgeTypeFilter(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "FRIEND", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "ENEMY", nil, 1.0)

	neighbors := getNeighborSet(gs, a.ID, DirectionOut, []string{"FRIEND"})
	if len(neighbors) != 1 {
		t.Errorf("Expected 1 FRIEND neighbor, got %d", len(neighbors))
	}
	if !neighbors[b.ID] {
		t.Error("Expected B in filtered neighbor set")
	}
}

func TestNodeSimilarityPair_Jaccard(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	// A -> C, A -> D
	// B -> C, B -> D, B -> E
	// Jaccard = |{C,D}| / |{C,D,E}| = 2/3
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, e.ID, "LINKS", nil, 1.0)

	opts := DefaultNodeSimilarityOptions()
	opts.Metric = SimilarityJaccard
	opts.Direction = DirectionOut

	score, err := NodeSimilarityPair(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityPair failed: %v", err)
	}

	expected := 2.0 / 3.0
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("Jaccard: expected ~%f, got %f", expected, score)
	}

	_ = c
	_ = d
	_ = e
}

func TestNodeSimilarityPair_Overlap(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	// A -> C, A -> D (2 neighbors)
	// B -> C, B -> D, B -> E (3 neighbors)
	// Overlap = |{C,D}| / min(2,3) = 2/2 = 1.0
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, e.ID, "LINKS", nil, 1.0)

	opts := DefaultNodeSimilarityOptions()
	opts.Metric = SimilarityOverlap
	opts.Direction = DirectionOut

	score, err := NodeSimilarityPair(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityPair failed: %v", err)
	}

	if math.Abs(score-1.0) > 0.001 {
		t.Errorf("Overlap: expected ~1.0, got %f", score)
	}

	_ = c
	_ = d
	_ = e
}

func TestNodeSimilarityPair_Cosine(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	// A -> C, A -> D (2 neighbors)
	// B -> C, B -> D, B -> E (3 neighbors)
	// Cosine = |{C,D}| / sqrt(2*3) = 2/sqrt(6)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, e.ID, "LINKS", nil, 1.0)

	opts := DefaultNodeSimilarityOptions()
	opts.Metric = SimilarityCosine
	opts.Direction = DirectionOut

	score, err := NodeSimilarityPair(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityPair failed: %v", err)
	}

	expected := 2.0 / math.Sqrt(6.0)
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("Cosine: expected ~%f, got %f", expected, score)
	}

	_ = c
	_ = d
	_ = e
}

func TestNodeSimilarityPair_NoOverlap(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)

	opts := DefaultNodeSimilarityOptions()
	score, err := NodeSimilarityPair(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityPair failed: %v", err)
	}

	if score != 0.0 {
		t.Errorf("Expected 0 similarity, got %f", score)
	}
}

func TestNodeSimilarityPair_EmptyNeighbors(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)

	opts := DefaultNodeSimilarityOptions()
	score, err := NodeSimilarityPair(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityPair failed: %v", err)
	}

	if score != 0.0 {
		t.Errorf("Expected 0 for empty neighborhoods, got %f", score)
	}
}

func TestNodeSimilarityFor(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	// A -> C, A -> D; B -> C, B -> D, B -> E; F -> C
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)
	f, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, e.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(f.ID, c.ID, "LINKS", nil, 1.0)

	opts := DefaultNodeSimilarityOptions()
	opts.Direction = DirectionOut

	result, err := NodeSimilarityFor(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityFor failed: %v", err)
	}

	if result.SourceNodeID != a.ID {
		t.Errorf("Expected source %d, got %d", a.ID, result.SourceNodeID)
	}

	// Should be sorted descending by score
	for i := 1; i < len(result.Similar); i++ {
		if result.Similar[i].Score > result.Similar[i-1].Score {
			t.Error("Results should be sorted descending by score")
		}
	}

	// B should rank highest (2/3 Jaccard with A)
	if len(result.Similar) == 0 {
		t.Fatal("Expected non-empty results")
	}

	// Zero-similarity nodes (C, D, E have no outgoing neighbors to compare) should be excluded
	for _, s := range result.Similar {
		if s.Score == 0.0 {
			t.Error("Zero-score nodes should be excluded")
		}
	}

	_ = e
	_ = f
}

func TestNodeSimilarityAll(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)

	opts := DefaultNodeSimilarityOptions()
	opts.Direction = DirectionOut
	opts.TopK = 5

	results, err := NodeSimilarityAll(gs, opts)
	if err != nil {
		t.Fatalf("NodeSimilarityAll failed: %v", err)
	}

	// Should have one result per node (some may be empty)
	if len(results) == 0 {
		t.Error("Expected non-empty results")
	}
}

func TestNodeSimilarityPair_IdenticalNeighbors(t *testing.T) {
	gs := setupSimilarityTestGraph(t)

	// A and B have identical neighbor sets → all metrics = 1.0
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)

	for _, metric := range []SimilarityMetric{SimilarityJaccard, SimilarityOverlap, SimilarityCosine} {
		opts := DefaultNodeSimilarityOptions()
		opts.Metric = metric
		opts.Direction = DirectionOut

		score, err := NodeSimilarityPair(gs, a.ID, b.ID, opts)
		if err != nil {
			t.Fatalf("Metric %d failed: %v", metric, err)
		}
		if math.Abs(score-1.0) > 0.001 {
			t.Errorf("Metric %d: expected 1.0 for identical neighborhoods, got %f", metric, score)
		}
	}
}
