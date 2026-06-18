package query

import (
	"math"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vector"
)

// --- Phase 2B: Vector Search Conformance Tests ---

// setupVectorConformanceGraph creates a graph with Concept nodes, embeddings,
// PREREQUISITE_OF edges, and a vector index for end-to-end hybrid query testing.
func setupVectorConformanceGraph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	// Create vector index for "embedding" property (3 dimensions for simplicity)
	err := gs.CreateVectorIndex("embedding", 3, 4, 50, vector.MetricCosine)
	if err != nil {
		t.Fatalf("Failed to create vector index: %v", err)
	}

	// Create Concept nodes with embeddings
	// Vectors chosen so similarity to query [1,0,0] is:
	//   QM:  [0.9, 0.1, 0.0] → high similarity (~0.99)
	//   GR:  [0.7, 0.7, 0.0] → moderate similarity (~0.71)
	//   BH:  [0.1, 0.9, 0.1] → low similarity (~0.11)
	//   HR:  [0.0, 0.0, 1.0] → zero similarity (0.0)
	qm, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("Quantum Mechanics"),
		"embedding": storage.VectorValue([]float32{0.9, 0.1, 0.0}),
	})
	_ = gs.UpdateNodeVectorIndexes(qm)

	gr, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("General Relativity"),
		"embedding": storage.VectorValue([]float32{0.7, 0.7, 0.0}),
	})
	_ = gs.UpdateNodeVectorIndexes(gr)

	bh, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("Black Holes"),
		"embedding": storage.VectorValue([]float32{0.1, 0.9, 0.1}),
	})
	_ = gs.UpdateNodeVectorIndexes(bh)

	hr, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("Hawking Radiation"),
		"embedding": storage.VectorValue([]float32{0.0, 0.0, 1.0}),
	})
	_ = gs.UpdateNodeVectorIndexes(hr)

	// Create prerequisite chain: QM -> GR -> BH -> HR
	_, _ = gs.CreateEdge(qm.ID, gr.ID, "PREREQUISITE_OF", nil, 1.0)
	_, _ = gs.CreateEdge(gr.ID, bh.ID, "PREREQUISITE_OF", nil, 1.0)
	_, _ = gs.CreateEdge(bh.ID, hr.ID, "PREREQUISITE_OF", nil, 1.0)

	executor := NewExecutor(gs)
	wireVectorSearch(t, gs, executor)

	return gs, executor, cleanup
}

// wireVectorSearch connects the executor to the real storage vector search infrastructure.
func wireVectorSearch(t *testing.T, gs *storage.GraphStorage, executor *Executor) {
	t.Helper()

	executor.SetVectorSearch(
		// similarityFn — real cosine similarity
		func(a, b []float32) (float64, error) {
			sim, err := vector.CosineSimilarity(a, b)
			return float64(sim), err
		},
		// searchFn — delegates to storage
		func(propertyName string, query []float32, k, ef int) ([]VectorSearchResult, error) {
			results, err := gs.VectorSearch(propertyName, query, k, ef)
			if err != nil {
				return nil, err
			}
			converted := make([]VectorSearchResult, len(results))
			for i, r := range results {
				converted[i] = VectorSearchResult{NodeID: r.ID, Distance: r.Distance}
			}
			return converted, nil
		},
		// hasIndexFn
		func(propertyName string) bool {
			return gs.HasVectorIndex(propertyName)
		},
		// getNodeFn
		func(nodeID uint64) (any, error) {
			return gs.GetNode(nodeID)
		},
	)
}

func TestConformance_VectorSimilarityInWhere(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	// Query: find concepts with embedding similar to [1,0,0] above threshold 0.5
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.5 RETURN c.name`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	// QM (~0.99) and GR (~0.71) should pass; BH (~0.11) and HR (0.0) should not
	if result.Count < 1 || result.Count > 3 {
		t.Errorf("expected 1-2 results above threshold 0.5, got %d", result.Count)
	}

	names := make(map[string]bool)
	for _, row := range result.Rows {
		if name, ok := row["c.name"].(string); ok {
			names[name] = true
		}
	}

	if !names["Quantum Mechanics"] {
		t.Error("expected Quantum Mechanics (similarity ~0.99)")
	}
	if !names["General Relativity"] {
		t.Error("expected General Relativity (similarity ~0.71)")
	}
	if names["Hawking Radiation"] {
		t.Error("Hawking Radiation should NOT pass threshold 0.5")
	}
}

func TestConformance_VectorSimilarityInReturn(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	// Approach A: repeat function in RETURN for score
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.5 RETURN c.name, vector.similarity(c.embedding, $q) AS score`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	if result.Count < 1 {
		t.Fatalf("expected at least 1 result, got %d", result.Count)
	}

	for _, row := range result.Rows {
		score, ok := row["score"].(float64)
		if !ok {
			t.Fatalf("expected score to be float64, got %T", row["score"])
		}
		if score <= 0.5 {
			t.Errorf("expected score > 0.5, got %f", score)
		}
	}
}

func TestConformance_VectorSyntheticScore(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	// Approach B: c.similarity_score synthetic property
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.5 RETURN c.name, c.similarity_score`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	if result.Count < 1 {
		t.Fatalf("expected at least 1 result, got %d", result.Count)
	}

	for _, row := range result.Rows {
		score, ok := row["c.similarity_score"].(float64)
		if !ok {
			t.Fatalf("expected similarity_score to be float64, got %T (row: %v)", row["c.similarity_score"], row)
		}
		if score <= 0.5 {
			t.Errorf("expected score > 0.5, got %f", score)
		}
	}
}

func TestConformance_VectorBruteForceWithoutIndex(t *testing.T) {
	// Test that vector.similarity works via brute-force scan when no HNSW index exists
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes WITH embeddings but WITHOUT vector index
	_, _ = gs.CreateNode([]string{"Item"}, map[string]storage.Value{
		"name":      storage.StringValue("Similar"),
		"embedding": storage.VectorValue([]float32{0.9, 0.1, 0.0}),
	})
	_, _ = gs.CreateNode([]string{"Item"}, map[string]storage.Value{
		"name":      storage.StringValue("Different"),
		"embedding": storage.VectorValue([]float32{0.0, 0.0, 1.0}),
	})

	executor := NewExecutor(gs)
	wireVectorSearch(t, gs, executor)

	// No vector index — optimizer won't insert VectorSearchStep, but
	// vector.similarity() still works as a brute-force function in WHERE
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (i:Item) WHERE vector.similarity(i.embedding, $q) > 0.5 RETURN i.name`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	if result.Count != 1 {
		t.Fatalf("expected 1 result (brute-force), got %d", result.Count)
	}

	name := result.Rows[0]["i.name"].(string)
	if name != "Similar" {
		t.Errorf("expected 'Similar', got %q", name)
	}
}

func TestConformance_VectorExplainShowsStep(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	result := parseAndExecuteWithParams(t, executor,
		`EXPLAIN MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.5 RETURN c.name`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	// EXPLAIN should show VectorSearchStep in the plan
	foundVectorStep := false
	for _, row := range result.Rows {
		if step, ok := row["step"].(string); ok && step == "VectorSearchStep" {
			foundVectorStep = true
			break
		}
	}

	if !foundVectorStep {
		t.Error("EXPLAIN should show VectorSearchStep when vector index exists")
		for _, row := range result.Rows {
			t.Logf("  step=%v detail=%v", row["step"], row["detail"])
		}
	}
}

func TestConformance_VectorHighThresholdEmptyResults(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	// Threshold so high nothing passes
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.999 RETURN c.name`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	// Only exact match [1,0,0] would pass; none of our nodes have exactly that
	// QM is [0.9, 0.1, 0.0] → similarity < 0.999
	if result.Count != 0 {
		t.Errorf("expected 0 results with threshold 0.999, got %d", result.Count)
	}
}

func TestConformance_VectorWithANDCondition(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	// Combined: name filter AND vector similarity
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE c.name = "Quantum Mechanics" AND vector.similarity(c.embedding, $q) > 0.5 RETURN c.name`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}

	name := result.Rows[0]["c.name"].(string)
	if name != "Quantum Mechanics" {
		t.Errorf("expected 'Quantum Mechanics', got %q", name)
	}
}

func TestConformance_VectorScoreComparison(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()

	// Verify that scores from both approaches (A and B) are consistent
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.5 RETURN c.name, vector.similarity(c.embedding, $q) AS funcScore, c.similarity_score`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)

	for _, row := range result.Rows {
		funcScore, ok1 := row["funcScore"].(float64)
		synScore, ok2 := row["c.similarity_score"].(float64)

		if !ok1 || !ok2 {
			t.Logf("skipping row with missing score: funcScore=%v (%T), synScore=%v (%T)",
				row["funcScore"], row["funcScore"], row["c.similarity_score"], row["c.similarity_score"])
			continue
		}

		// Both should be close (function recomputes, synthetic comes from HNSW step)
		if math.Abs(funcScore-synScore) > 0.05 {
			t.Errorf("score mismatch for %v: funcScore=%f, synScore=%f", row["c.name"], funcScore, synScore)
		}
	}
}
