package query

import (
	"fmt"
	"math"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// --- Namespaced function parsing tests ---

func TestParser_NamespacedFunctionInWhere(t *testing.T) {
	// vector.similarity(c.embedding, $query_embedding) > 0.8 should parse as
	// BinaryExpression{Left: FunctionCallExpression{Name: "vector.similarity"}, ...}
	input := `MATCH (c:Concept) WHERE vector.similarity(c.embedding, $query_embedding) > 0.8 RETURN c`
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	query, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if query.Where == nil {
		t.Fatal("expected WHERE clause")
	}

	// Should be BinaryExpression with > operator
	binExpr, ok := query.Where.Expression.(*BinaryExpression)
	if !ok {
		t.Fatalf("expected BinaryExpression, got %T", query.Where.Expression)
	}

	if binExpr.Operator != ">" {
		t.Errorf("expected operator '>', got %q", binExpr.Operator)
	}

	// Left should be FunctionCallExpression with name "vector.similarity"
	funcExpr, ok := binExpr.Left.(*FunctionCallExpression)
	if !ok {
		t.Fatalf("expected FunctionCallExpression, got %T", binExpr.Left)
	}

	if funcExpr.Name != "vector.similarity" {
		t.Errorf("expected function name 'vector.similarity', got %q", funcExpr.Name)
	}

	if len(funcExpr.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(funcExpr.Args))
	}

	// First arg: c.embedding (PropertyExpression)
	propArg, ok := funcExpr.Args[0].(*PropertyExpression)
	if !ok {
		t.Fatalf("expected PropertyExpression for arg 0, got %T", funcExpr.Args[0])
	}
	if propArg.Variable != "c" || propArg.Property != "embedding" {
		t.Errorf("expected c.embedding, got %s.%s", propArg.Variable, propArg.Property)
	}

	// Second arg: $query_embedding (ParameterExpression)
	paramArg, ok := funcExpr.Args[1].(*ParameterExpression)
	if !ok {
		t.Fatalf("expected ParameterExpression for arg 1, got %T", funcExpr.Args[1])
	}
	if paramArg.Name != "query_embedding" {
		t.Errorf("expected parameter name 'query_embedding', got %q", paramArg.Name)
	}
}

func TestParser_NamespacedFunctionInReturn(t *testing.T) {
	input := `MATCH (c:Concept) RETURN vector.similarity(c.embedding, $q) AS score`
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	query, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if query.Return == nil {
		t.Fatal("expected RETURN clause")
	}

	if len(query.Return.Items) != 1 {
		t.Fatalf("expected 1 return item, got %d", len(query.Return.Items))
	}

	item := query.Return.Items[0]
	if item.Alias != "score" {
		t.Errorf("expected alias 'score', got %q", item.Alias)
	}

	// Should be in ValueExpr as FunctionCallExpression
	funcExpr, ok := item.ValueExpr.(*FunctionCallExpression)
	if !ok {
		t.Fatalf("expected ValueExpr to be FunctionCallExpression, got %T", item.ValueExpr)
	}

	if funcExpr.Name != "vector.similarity" {
		t.Errorf("expected function name 'vector.similarity', got %q", funcExpr.Name)
	}
}

func TestParser_PropertyExpressionStillWorks(t *testing.T) {
	// Regression: c.name should still parse as PropertyExpression, not function call
	input := `MATCH (c:Concept) RETURN c.name`
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	query, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if query.Return == nil {
		t.Fatal("expected RETURN clause")
	}

	item := query.Return.Items[0]
	if item.Expression == nil {
		t.Fatal("expected Expression to be set (PropertyExpression)")
	}
	if item.Expression.Variable != "c" || item.Expression.Property != "name" {
		t.Errorf("expected c.name, got %s.%s", item.Expression.Variable, item.Expression.Property)
	}
	if item.ValueExpr != nil {
		t.Errorf("expected ValueExpr to be nil for property expression, got %T", item.ValueExpr)
	}
}

// --- vector.similarity function tests ---

func TestVectorSimilarity_IdenticalVectors(t *testing.T) {
	executor := setupVectorExecutor(t)
	_ = executor // ensure registration happened

	fn, err := GetFunction("vector.similarity")
	if err != nil {
		t.Fatalf("vector.similarity not registered: %v", err)
	}

	a := []float32{1.0, 0.0, 0.0}
	b := []float32{1.0, 0.0, 0.0}

	result, err := fn([]any{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	score, ok := result.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", result)
	}

	if score < 0.99 || score > 1.01 {
		t.Errorf("expected similarity ~1.0 for identical vectors, got %f", score)
	}
}

func TestVectorSimilarity_OrthogonalVectors(t *testing.T) {
	executor := setupVectorExecutor(t)
	_ = executor

	fn, err := GetFunction("vector.similarity")
	if err != nil {
		t.Fatalf("vector.similarity not registered: %v", err)
	}

	a := []float32{1.0, 0.0, 0.0}
	b := []float32{0.0, 1.0, 0.0}

	result, err := fn([]any{a, b})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	score, ok := result.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", result)
	}

	if score < -0.01 || score > 0.01 {
		t.Errorf("expected similarity ~0.0 for orthogonal vectors, got %f", score)
	}
}

func TestVectorSimilarity_MismatchedDimensions(t *testing.T) {
	executor := setupVectorExecutor(t)
	_ = executor

	fn, err := GetFunction("vector.similarity")
	if err != nil {
		t.Fatalf("vector.similarity not registered: %v", err)
	}

	a := []float32{1.0, 0.0}
	b := []float32{1.0, 0.0, 0.0}

	_, err = fn([]any{a, b})
	if err == nil {
		t.Fatal("expected error for mismatched dimensions")
	}
}

func TestVectorSimilarity_NonVectorArguments(t *testing.T) {
	executor := setupVectorExecutor(t)
	_ = executor

	fn, err := GetFunction("vector.similarity")
	if err != nil {
		t.Fatalf("vector.similarity not registered: %v", err)
	}

	_, err = fn([]any{"not a vector", 42})
	if err == nil {
		t.Fatal("expected error for non-vector arguments")
	}
}

func TestToFloat32Slice(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantOK  bool
		wantLen int
	}{
		{"[]float32", []float32{1.0, 2.0}, true, 2},
		{"[]float64", []float64{1.0, 2.0}, true, 2},
		{"[]any with float64", []any{float64(1.0), float64(2.0)}, true, 2},
		{"[]any with float32", []any{float32(1.0), float32(2.0)}, true, 2},
		{"string", "not a vector", false, 0},
		{"nil", nil, false, 0},
		{"int", 42, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toFloat32Slice(tt.input)
			if ok != tt.wantOK {
				t.Errorf("toFloat32Slice ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && len(result) != tt.wantLen {
				t.Errorf("toFloat32Slice len = %d, want %d", len(result), tt.wantLen)
			}
		})
	}
}

// setupVectorExecutor creates an executor with vector search wired up
func setupVectorExecutor(t *testing.T) *Executor {
	t.Helper()
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create graph storage: %v", err)
	}
	executor := NewExecutor(graph)

	// Provide cosine similarity as the similarity function
	executor.SetVectorSearch(
		func(a, b []float32) (float64, error) {
			if len(a) != len(b) {
				return 0, fmt.Errorf("dimension mismatch: %d vs %d", len(a), len(b))
			}
			var dotProd, normA, normB float64
			for i := range a {
				dotProd += float64(a[i]) * float64(b[i])
				normA += float64(a[i]) * float64(a[i])
				normB += float64(b[i]) * float64(b[i])
			}
			if normA == 0 || normB == 0 {
				return 0, nil
			}
			return dotProd / (math.Sqrt(normA) * math.Sqrt(normB)), nil
		},
		nil, // searchFn - not needed for basic tests
		nil, // hasIndexFn
		nil, // getNodeFn
	)
	return executor
}

// --- Score binding Approach B tests ---

func TestSyntheticProperty_SimilarityScore(t *testing.T) {
	node := &storage.Node{
		ID:     1,
		Labels: []string{"Concept"},
		Properties: map[string]storage.Value{
			"name": storage.StringValue("Quantum Mechanics"),
		},
	}

	binding := &BindingSet{
		bindings:     map[string]any{"c": node},
		vectorScores: map[string]float64{"c": 0.95},
	}

	computer := &AggregationComputer{}

	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer graph.Close()
	executor := NewExecutor(graph)

	expr := &PropertyExpression{Variable: "c", Property: "similarity_score"}
	result := executor.extractValueFromBinding(binding, expr, computer)

	score, ok := result.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", result)
	}

	if score != 0.95 {
		t.Errorf("expected score 0.95, got %f", score)
	}
}

func TestSyntheticProperty_RealPropertyTakesPrecedence(t *testing.T) {
	node := &storage.Node{
		ID:     1,
		Labels: []string{"Concept"},
		Properties: map[string]storage.Value{
			"similarity_score": storage.FloatValue(0.42),
		},
	}

	binding := &BindingSet{
		bindings:     map[string]any{"c": node},
		vectorScores: map[string]float64{"c": 0.95},
	}

	computer := &AggregationComputer{}

	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer graph.Close()
	executor := NewExecutor(graph)

	expr := &PropertyExpression{Variable: "c", Property: "similarity_score"}
	result := executor.extractValueFromBinding(binding, expr, computer)

	score, ok := result.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", result)
	}

	// Real property (0.42) should take precedence over synthetic (0.95)
	if score != 0.42 {
		t.Errorf("expected real property 0.42, got %f", score)
	}
}

func TestSyntheticProperty_NilWhenNoVectorSearchRan(t *testing.T) {
	node := &storage.Node{
		ID:         1,
		Labels:     []string{"Concept"},
		Properties: map[string]storage.Value{},
	}

	binding := &BindingSet{
		bindings: map[string]any{"c": node},
		// No vectorScores
	}

	computer := &AggregationComputer{}

	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer graph.Close()
	executor := NewExecutor(graph)

	expr := &PropertyExpression{Variable: "c", Property: "similarity_score"}
	result := executor.extractValueFromBinding(binding, expr, computer)

	if result != nil {
		t.Errorf("expected nil when no VectorSearchStep ran, got %v", result)
	}
}

func TestExtractValue_TypeVector(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	node := &storage.Node{
		ID:     1,
		Labels: []string{"Concept"},
		Properties: map[string]storage.Value{
			"name":      storage.StringValue("Quantum Mechanics"),
			"embedding": storage.VectorValue(embedding),
		},
	}

	context := map[string]any{
		"c": node,
	}

	tests := []struct {
		name     string
		expr     Expression
		wantNil  bool
		wantLen  int
		wantVals []float32
	}{
		{
			name:     "extract vector property from node",
			expr:     &PropertyExpression{Variable: "c", Property: "embedding"},
			wantLen:  4,
			wantVals: embedding,
		},
		{
			name:    "extract missing property returns nil",
			expr:    &PropertyExpression{Variable: "c", Property: "nonexistent"},
			wantNil: true,
		},
		{
			name:    "extract from unbound variable returns nil",
			expr:    &PropertyExpression{Variable: "x", Property: "embedding"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractValue(tt.expr, context)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			vec, ok := result.([]float32)
			if !ok {
				t.Fatalf("expected []float32, got %T", result)
			}

			if len(vec) != tt.wantLen {
				t.Errorf("expected length %d, got %d", tt.wantLen, len(vec))
			}

			for i, v := range tt.wantVals {
				if vec[i] != v {
					t.Errorf("vec[%d] = %f, want %f", i, vec[i], v)
				}
			}
		})
	}
}

func TestAggregationExtractValue_TypeVector(t *testing.T) {
	embedding := []float32{1.0, 2.0, 3.0}
	val := storage.VectorValue(embedding)

	computer := &AggregationComputer{}
	result := computer.ExtractValue(val)

	vec, ok := result.([]float32)
	if !ok {
		t.Fatalf("expected []float32, got %T", result)
	}

	if len(vec) != 3 {
		t.Errorf("expected length 3, got %d", len(vec))
	}

	for i, v := range embedding {
		if vec[i] != v {
			t.Errorf("vec[%d] = %f, want %f", i, vec[i], v)
		}
	}
}
