package queryutil

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

func TestWireCapabilities_VectorSearch(t *testing.T) {
	dataDir := t.TempDir()
	graph, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create vector index: 3 dimensions, m=4, efConstruction=50, cosine metric
	if err := graph.CreateVectorIndex("embedding", 3, 4, 50, vector.MetricCosine); err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}

	// Create nodes with embeddings and add to index
	type conceptData struct {
		name      string
		embedding []float32
	}
	concepts := []conceptData{
		{"concept-A", []float32{1.0, 0.0, 0.0}},
		{"concept-B", []float32{0.9, 0.1, 0.0}},
		{"concept-C", []float32{0.0, 0.0, 1.0}},
	}
	for _, c := range concepts {
		node, err := graph.CreateNode(
			[]string{"Concept"},
			map[string]storage.Value{
				"name":      storage.StringValue(c.name),
				"embedding": storage.VectorValue(c.embedding),
			},
		)
		if err != nil {
			t.Fatalf("failed to create node %s: %v", c.name, err)
		}
		graph.UpdateNodeVectorIndexes(node)
	}

	// Wire capabilities
	executor := query.NewExecutor(graph)
	WireCapabilities(executor, graph)

	// Execute a vector similarity query
	queryStr := "MATCH (c:Concept) WHERE vector.similarity(c.embedding, $query_vec) > 0.8 RETURN c.name"
	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	parser := query.NewParser(tokens)
	parsed, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	params := map[string]any{
		"query_vec": []float32{1.0, 0.0, 0.0},
	}
	results, err := executor.ExecuteWithParams(parsed, params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if results.Count < 1 {
		t.Errorf("expected at least 1 result from vector query, got %d", results.Count)
	}

	// Verify concept-A (identical vector) is in the results
	found := false
	for _, row := range results.Rows {
		if name, ok := row["c.name"].(string); ok && name == "concept-A" {
			found = true
		}
	}
	if !found {
		t.Error("expected concept-A in results (identical vector), not found")
	}
}

func TestWireCapabilities_FullTextSearch(t *testing.T) {
	dataDir := t.TempDir()
	graph, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create nodes with text content
	names := []string{"graph database systems", "machine learning", "graph neural networks"}
	for _, name := range names {
		_, err := graph.CreateNode(
			[]string{"Topic"},
			map[string]storage.Value{
				"title": storage.StringValue(name),
			},
		)
		if err != nil {
			t.Fatalf("failed to create node: %v", err)
		}
	}

	// Wire capabilities
	executor := query.NewExecutor(graph)
	WireCapabilities(executor, graph)

	// Execute a search query — search() scores text relevance
	queryStr := `MATCH (t:Topic) WHERE search(t.title, "graph") > 0.0 RETURN t.title`
	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	parser := query.NewParser(tokens)
	parsed, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	results, err := executor.Execute(parsed)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// "graph database systems" and "graph neural networks" both contain "graph"
	if results.Count < 2 {
		t.Errorf("expected at least 2 results matching 'graph', got %d", results.Count)
	}
}

func TestWireCapabilities_ReturnsSameExecutor(t *testing.T) {
	dataDir := t.TempDir()
	graph, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer graph.Close()

	executor := query.NewExecutor(graph)
	result := WireCapabilities(executor, graph)

	if result != executor {
		t.Error("WireCapabilities should return the same executor pointer")
	}
}
