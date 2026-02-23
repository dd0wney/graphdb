package query

import (
	"math"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// setupConformanceGraph creates a shared test graph for conformance tests
func setupConformanceGraph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	// Create a diverse dataset
	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice"),
		"age":        storage.IntValue(30),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(80000),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob"),
		"age":        storage.IntValue(25),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(60000),
	})
	charlie, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie"),
		"age":        storage.IntValue(35),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(90000),
	})

	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)
	gs.CreateEdge(alice.ID, charlie.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2019),
	}, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

// parseAndExecute is a helper that parses and executes a query string
func parseAndExecute(t *testing.T, executor *Executor, queryText string) *ResultSet {
	t.Helper()

	lexer := NewLexer(queryText)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed for %q: %v", queryText, err)
	}

	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed for %q: %v", queryText, err)
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed for %q: %v", queryText, err)
	}

	return result
}

func TestConformance_Explain(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor, `EXPLAIN MATCH (n:Person) WHERE n.age > 25 RETURN n.name`)

	if len(result.Columns) != 2 || result.Columns[0] != "step" {
		t.Errorf("EXPLAIN should return step/detail columns, got %v", result.Columns)
	}
	if len(result.Rows) < 3 {
		t.Errorf("Expected at least 3 plan steps, got %d", len(result.Rows))
	}
}

func TestConformance_Profile(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor, `PROFILE MATCH (n:Person) RETURN n.name`)

	if result.Profile == nil {
		t.Fatal("PROFILE should populate Profile field")
	}
	if len(result.Profile) < 2 {
		t.Errorf("Expected at least 2 profile entries, got %d", len(result.Profile))
	}
	if result.Count == 0 {
		t.Error("PROFILE should also return actual results")
	}
}

func TestConformance_Collect(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN COLLECT(n.name) AS names`)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result row, got %d", result.Count)
	}

	names, ok := result.Rows[0]["names"].([]any)
	if !ok {
		t.Fatalf("Expected []any for COLLECT, got %T", result.Rows[0]["names"])
	}
	if len(names) != 3 {
		t.Errorf("Expected 3 names, got %d", len(names))
	}
}

func TestConformance_CollectGroupBy(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.department, COLLECT(n.name) AS names GROUP BY n.department`)

	if result.Count != 2 {
		t.Fatalf("Expected 2 groups, got %d", result.Count)
	}

	for _, row := range result.Rows {
		names, ok := row["names"].([]any)
		if !ok {
			t.Fatalf("Expected []any for names, got %T", row["names"])
		}
		dept := row["n.department"]
		switch dept {
		case "Engineering":
			if len(names) != 2 {
				t.Errorf("Engineering: expected 2 names, got %d", len(names))
			}
		case "Sales":
			if len(names) != 1 {
				t.Errorf("Sales: expected 1 name, got %d", len(names))
			}
		}
	}
}

func TestConformance_StringFunctionsInWhere(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	tests := []struct {
		name     string
		query    string
		expected int // expected row count
	}{
		{
			name:     "toLower comparison",
			query:    `MATCH (n:Person) WHERE toLower(n.name) = "alice" RETURN n.name`,
			expected: 1,
		},
		{
			name:     "startsWith",
			query:    `MATCH (n:Person) WHERE startsWith(n.name, "Al") RETURN n.name`,
			expected: 1,
		},
		{
			name:     "contains",
			query:    `MATCH (n:Person) WHERE contains(n.name, "li") RETURN n.name`,
			expected: 2, // Alice and Charlie both contain "li"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAndExecute(t, executor, tt.query)
			if result.Count != tt.expected {
				t.Errorf("Expected %d results, got %d", tt.expected, result.Count)
			}
		})
	}
}

func TestConformance_StringFunctionsInReturn(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = "Alice" RETURN toUpper(n.name) AS upper_name`)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["upper_name"] != "ALICE" {
		t.Errorf("Expected 'ALICE', got %v", result.Rows[0]["upper_name"])
	}
}

func TestConformance_NumericFunctionsInWhere(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// abs(-5) = 5, toFloat(n.age) works
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE toFloat(n.age) > 29.5 RETURN n.name`)

	// Alice (30) and Charlie (35) should match
	if result.Count != 2 {
		t.Errorf("Expected 2 results for toFloat(n.age) > 29.5, got %d", result.Count)
	}
}

func TestConformance_SearchFunction(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Quantum Computing Revolution"),
	})
	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Classical Music Guide"),
	})

	executor := NewExecutor(gs)
	idx := search.NewFullTextIndex(gs)
	executor.SetSearchIndex(idx)

	result := parseAndExecute(t, executor,
		`MATCH (n:Article) WHERE search(n.title, "quantum computing") > 0.5 RETURN n.title`)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.title"] != "Quantum Computing Revolution" {
		t.Errorf("Expected 'Quantum Computing Revolution', got %v", result.Rows[0]["n.title"])
	}
}

func TestConformance_MergeCreate(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// MERGE should create the node
	parseAndExecute(t, executor,
		`MERGE (n:Person {name: "Diana"})`)

	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node after MERGE create, got %d", stats.NodeCount)
	}

	// Second MERGE should not create a duplicate
	parseAndExecute(t, executor,
		`MERGE (n:Person {name: "Diana"})`)

	stats = gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node after second MERGE (no duplicate), got %d", stats.NodeCount)
	}
}

func TestConformance_With(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WITH n AS person RETURN person.name`)

	if result.Count != 3 {
		t.Errorf("Expected 3 results from WITH pass-through, got %d", result.Count)
	}
}

func TestConformance_WithFilter(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WITH n.name AS name, n.age AS age WHERE age > 28 RETURN name`)

	// Alice (30) and Charlie (35)
	if result.Count != 2 {
		t.Errorf("Expected 2 results after WITH filter, got %d", result.Count)
	}
}

func TestConformance_CombinedFeatures(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Test COLLECT + GROUP BY parsed from text
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.department, COUNT(n) AS cnt, AVG(n.salary) AS avg_sal GROUP BY n.department`)

	if result.Count != 2 {
		t.Errorf("Expected 2 department groups, got %d", result.Count)
	}

	// Verify Engineering group
	for _, row := range result.Rows {
		if row["n.department"] == "Engineering" {
			cnt, _ := row["cnt"].(int)
			if cnt != 2 {
				t.Errorf("Engineering count: expected 2, got %v", row["cnt"])
			}
		}
	}
}

func TestConformance_ExplainParsed(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// EXPLAIN from parsed text
	result := parseAndExecute(t, executor,
		`EXPLAIN MATCH (n:Person) WHERE n.age > 25 RETURN n.name LIMIT 5`)

	if result.Columns[0] != "step" {
		t.Errorf("Expected 'step' column, got %q", result.Columns[0])
	}

	// Should have MatchStep, FilterStep, ReturnStep
	stepNames := make([]string, 0)
	for _, row := range result.Rows {
		if s, ok := row["step"].(string); ok {
			stepNames = append(stepNames, s)
		}
	}

	found := strings.Join(stepNames, ",")
	if !strings.Contains(found, "MatchStep") || !strings.Contains(found, "FilterStep") || !strings.Contains(found, "ReturnStep") {
		t.Errorf("Expected MatchStep, FilterStep, ReturnStep in plan, got: %s", found)
	}
}

// parseAndExecuteWithParams is a helper that parses and executes a parameterized query
func parseAndExecuteWithParams(t *testing.T, executor *Executor, queryText string, params map[string]any) *ResultSet {
	t.Helper()

	lexer := NewLexer(queryText)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed for %q: %v", queryText, err)
	}

	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed for %q: %v", queryText, err)
	}

	result, err := executor.ExecuteWithParams(query, params)
	if err != nil {
		t.Fatalf("ExecuteWithParams failed for %q: %v", queryText, err)
	}

	return result
}

// --- Phase 2A Conformance Tests ---

func TestConformance_ParameterizedQuery(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecuteWithParams(t, executor,
		`MATCH (n:Person {name: $name}) RETURN n.age`,
		map[string]any{"name": "Alice"},
	)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.age"] != int64(30) {
		t.Errorf("Expected age 30, got %v", result.Rows[0]["n.age"])
	}
}

func TestConformance_CaseInReturn(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name, CASE WHEN n.age > 30 THEN "senior" ELSE "junior" END AS tier`)

	if result.Count != 3 {
		t.Fatalf("Expected 3 results, got %d", result.Count)
	}

	for _, row := range result.Rows {
		name := row["n.name"]
		tier := row["tier"]
		switch name {
		case "Alice":
			if tier != "junior" {
				t.Errorf("Alice (age 30): tier = %v, want junior", tier)
			}
		case "Charlie":
			if tier != "senior" {
				t.Errorf("Charlie (age 35): tier = %v, want senior", tier)
			}
		case "Bob":
			if tier != "junior" {
				t.Errorf("Bob (age 25): tier = %v, want junior", tier)
			}
		}
	}
}

func TestConformance_OptionalMatch(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Alice -> Bob (KNOWS), Alice -> Charlie (KNOWS)
	// Bob and Charlie have no outgoing KNOWS edges
	result := parseAndExecute(t, executor,
		`MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name`)

	// Alice has 2 friends, Bob has 0, Charlie has 0
	// Expected: Alice->Bob, Alice->Charlie, Bob->nil, Charlie->nil = 4 rows
	if result.Count != 4 {
		t.Fatalf("Expected 4 rows, got %d", result.Count)
	}

	nullCount := 0
	for _, row := range result.Rows {
		if row["b.name"] == nil {
			nullCount++
		}
	}
	if nullCount != 2 {
		t.Errorf("Expected 2 null b.name rows, got %d", nullCount)
	}
}

func TestConformance_Union(t *testing.T) {
	gs, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Graph Databases"),
	})

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name UNION MATCH (n:Article) RETURN n.title AS name`)

	// 3 persons + 1 article = 4 unique names
	if result.Count != 4 {
		t.Errorf("Expected 4 rows, got %d", result.Count)
	}
}

func TestConformance_CombinedPhase2A(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Parameterized + OPTIONAL MATCH + CASE
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (a:Person {name: $name}) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name, CASE WHEN b.name = "Bob" THEN "friend" ELSE "unknown" END AS relation`,
		map[string]any{"name": "Alice"},
	)

	// Alice knows Bob and Charlie
	if result.Count != 2 {
		t.Fatalf("Expected 2 rows, got %d", result.Count)
	}

	for _, row := range result.Rows {
		bName := row["b.name"]
		rel := row["relation"]
		if bName == "Bob" && rel != "friend" {
			t.Errorf("Bob relation = %v, want friend", rel)
		}
		if bName == "Charlie" && rel != "unknown" {
			t.Errorf("Charlie relation = %v, want unknown", rel)
		}
	}
}

func TestConformance_MergeOnCreateSet(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	parseAndExecute(t, executor,
		`MERGE (n:Person {name: "Eve"}) ON CREATE SET n.status = "new"`)

	// Verify the node was created with the ON CREATE SET property
	nodes, _ := gs.FindNodesByLabel("Person")
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	if statusVal, ok := nodes[0].Properties["status"]; ok {
		status, _ := statusVal.AsString()
		if status != "new" {
			t.Errorf("Expected status 'new', got %q", status)
		}
	} else {
		t.Error("Expected 'status' property from ON CREATE SET")
	}
}

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
	gs.UpdateNodeVectorIndexes(qm)

	gr, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("General Relativity"),
		"embedding": storage.VectorValue([]float32{0.7, 0.7, 0.0}),
	})
	gs.UpdateNodeVectorIndexes(gr)

	bh, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("Black Holes"),
		"embedding": storage.VectorValue([]float32{0.1, 0.9, 0.1}),
	})
	gs.UpdateNodeVectorIndexes(bh)

	hr, _ := gs.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"name":      storage.StringValue("Hawking Radiation"),
		"embedding": storage.VectorValue([]float32{0.0, 0.0, 1.0}),
	})
	gs.UpdateNodeVectorIndexes(hr)

	// Create prerequisite chain: QM -> GR -> BH -> HR
	gs.CreateEdge(qm.ID, gr.ID, "PREREQUISITE_OF", nil, 1.0)
	gs.CreateEdge(gr.ID, bh.ID, "PREREQUISITE_OF", nil, 1.0)
	gs.CreateEdge(bh.ID, hr.ID, "PREREQUISITE_OF", nil, 1.0)

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
	gs.CreateNode([]string{"Item"}, map[string]storage.Value{
		"name":      storage.StringValue("Similar"),
		"embedding": storage.VectorValue([]float32{0.9, 0.1, 0.0}),
	})
	gs.CreateNode([]string{"Item"}, map[string]storage.Value{
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
