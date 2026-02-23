package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupLinkPredictionTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "linkpred-test-*")
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

func TestPredictLinkScore_CommonNeighbours(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	// A -> C, A -> D; B -> C, B -> D, B -> E
	// Common neighbours of A,B = {C, D} → score = 2
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

	opts := DefaultLinkPredictionOptions()
	opts.Method = LinkPredCommonNeighbours
	opts.Direction = DirectionOut

	score, err := PredictLinkScore(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinkScore failed: %v", err)
	}

	if score != 2.0 {
		t.Errorf("Common neighbours: expected 2.0, got %f", score)
	}

	_ = c
	_ = d
	_ = e
}

func TestPredictLinkScore_AdamicAdar(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	// A -> C, A -> D; B -> C, B -> D
	// C has 0 outgoing neighbors (so degree = 0, skip 1/log(0) → skip)
	// D has 0 outgoing neighbors (same)
	// But using DirectionBoth:
	// C: incoming from A,B → degree 2, D: incoming from A,B → degree 2
	// AA = 1/log(2) + 1/log(2) = 2/log(2)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)

	opts := DefaultLinkPredictionOptions()
	opts.Method = LinkPredAdamicAdar
	opts.Direction = DirectionBoth

	score, err := PredictLinkScore(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinkScore failed: %v", err)
	}

	expected := 2.0 / math.Log(2.0)
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("Adamic-Adar: expected ~%f, got %f", expected, score)
	}

	_ = c
	_ = d
}

func TestPredictLinkScore_PreferentialAttachment(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	// A has 2 outgoing, B has 3 outgoing → PA = 2 * 3 = 6
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

	opts := DefaultLinkPredictionOptions()
	opts.Method = LinkPredPreferentialAttachment
	opts.Direction = DirectionOut

	score, err := PredictLinkScore(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinkScore failed: %v", err)
	}

	if score != 6.0 {
		t.Errorf("Preferential attachment: expected 6.0, got %f", score)
	}

	_ = c
	_ = d
	_ = e
}

func TestPredictLinkScore_NoCommonNeighbours(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)

	opts := DefaultLinkPredictionOptions()
	opts.Method = LinkPredCommonNeighbours
	opts.Direction = DirectionOut

	score, err := PredictLinkScore(gs, a.ID, b.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinkScore failed: %v", err)
	}

	if score != 0.0 {
		t.Errorf("Expected 0 common neighbours, got %f", score)
	}
}

func TestPredictLinksFor_ExcludeExisting(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	// A-B already connected; A->C, B->C so C is predicted for A
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)

	opts := DefaultLinkPredictionOptions()
	opts.ExcludeExisting = true
	opts.Direction = DirectionOut

	result, err := PredictLinksFor(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinksFor failed: %v", err)
	}

	// B and C are already connected to A, so should be excluded
	for _, pred := range result.Predictions {
		if pred.ToNodeID == b.ID || pred.ToNodeID == c.ID {
			t.Errorf("Existing neighbor %d should be excluded", pred.ToNodeID)
		}
	}
}

func TestPredictLinksFor_IncludeExisting(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)

	opts := DefaultLinkPredictionOptions()
	opts.ExcludeExisting = false
	opts.Direction = DirectionOut

	result, err := PredictLinksFor(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinksFor failed: %v", err)
	}

	// B should appear (shared neighbor C)
	found := false
	for _, pred := range result.Predictions {
		if pred.ToNodeID == b.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected B in predictions when ExcludeExisting=false")
	}
}

func TestPredictLinksFor_Sorted(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)
	e, _ := gs.CreateNode([]string{"Node"}, nil)

	// A and B share 2 common neighbors (C,D); A and E share 1 (C)
	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(e.ID, c.ID, "LINKS", nil, 1.0)

	opts := DefaultLinkPredictionOptions()
	opts.ExcludeExisting = false
	opts.Direction = DirectionOut

	result, err := PredictLinksFor(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinksFor failed: %v", err)
	}

	// Results should be sorted descending by score
	for i := 1; i < len(result.Predictions); i++ {
		if result.Predictions[i].Score > result.Predictions[i-1].Score {
			t.Error("Predictions should be sorted descending by score")
		}
	}
}

func TestPredictLinksFor_TopK(t *testing.T) {
	gs := setupLinkPredictionTestGraph(t)

	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, c.ID, "LINKS", nil, 1.0)

	opts := DefaultLinkPredictionOptions()
	opts.ExcludeExisting = false
	opts.Direction = DirectionOut
	opts.TopK = 1

	result, err := PredictLinksFor(gs, a.ID, opts)
	if err != nil {
		t.Fatalf("PredictLinksFor failed: %v", err)
	}

	if len(result.Predictions) > 1 {
		t.Errorf("TopK=1 but got %d predictions", len(result.Predictions))
	}
}
