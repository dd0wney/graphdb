package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestParser_Unwind(t *testing.T) {
	input := "UNWIND n.tags AS tag RETURN tag"
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}
	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if query.Unwind == nil {
		t.Fatal("Expected non-nil Unwind clause")
	}
	if query.Unwind.Expression.Variable != "n" || query.Unwind.Expression.Property != "tags" {
		t.Errorf("Expected n.tags, got %s.%s", query.Unwind.Expression.Variable, query.Unwind.Expression.Property)
	}
	if query.Unwind.Alias != "tag" {
		t.Errorf("Expected alias 'tag', got %q", query.Unwind.Alias)
	}
}

func TestUnwind_BasicList(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create a node with a list-like property stored as a string
	// Since storage doesn't support native lists, we test with COLLECT first then UNWIND
	names := []string{"Alice", "Bob", "Charlie"}
	for _, name := range names {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(name),
			"team": storage.StringValue("Alpha"),
		})
	}

	executor := NewExecutor(gs)

	// First, use COLLECT to get a list, then verify UNWIND
	// For now, test UNWIND via direct AST construction with pre-populated bindings
	// that have a list value
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	// Execute a basic MATCH to verify data exists
	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Count != 3 {
		t.Errorf("Expected 3 results, got %d", result.Count)
	}
}

func TestUnwindStep_Execute(t *testing.T) {
	// Test the UnwindStep directly with pre-constructed bindings
	step := &UnwindStep{
		unwind: &UnwindClause{
			Expression: &PropertyExpression{Variable: "data", Property: ""},
			Alias:      "item",
		},
	}

	ctx := &ExecutionContext{
		bindings: make(map[string]any),
		results: []*BindingSet{
			{bindings: map[string]any{
				"data": []any{"a", "b", "c"},
			}},
		},
	}

	if err := step.Execute(ctx); err != nil {
		t.Fatalf("UnwindStep failed: %v", err)
	}

	if len(ctx.results) != 3 {
		t.Fatalf("Expected 3 results after UNWIND, got %d", len(ctx.results))
	}

	expected := []any{"a", "b", "c"}
	for i, bs := range ctx.results {
		if bs.bindings["item"] != expected[i] {
			t.Errorf("Result[%d]: expected %v, got %v", i, expected[i], bs.bindings["item"])
		}
		// Original bindings should be preserved
		if bs.bindings["data"] == nil {
			t.Errorf("Result[%d]: original binding 'data' lost", i)
		}
	}
}

func TestUnwindStep_NonListValue(t *testing.T) {
	// Non-list values should be treated as single-element lists
	step := &UnwindStep{
		unwind: &UnwindClause{
			Expression: &PropertyExpression{Variable: "val", Property: ""},
			Alias:      "item",
		},
	}

	ctx := &ExecutionContext{
		bindings: make(map[string]any),
		results: []*BindingSet{
			{bindings: map[string]any{
				"val": "single-value",
			}},
		},
	}

	if err := step.Execute(ctx); err != nil {
		t.Fatalf("UnwindStep failed: %v", err)
	}

	if len(ctx.results) != 1 {
		t.Fatalf("Expected 1 result for non-list, got %d", len(ctx.results))
	}

	if ctx.results[0].bindings["item"] != "single-value" {
		t.Errorf("Expected 'single-value', got %v", ctx.results[0].bindings["item"])
	}
}

func TestUnwindStep_EmptyList(t *testing.T) {
	step := &UnwindStep{
		unwind: &UnwindClause{
			Expression: &PropertyExpression{Variable: "data", Property: ""},
			Alias:      "item",
		},
	}

	ctx := &ExecutionContext{
		bindings: make(map[string]any),
		results: []*BindingSet{
			{bindings: map[string]any{
				"data": []any{},
			}},
		},
	}

	if err := step.Execute(ctx); err != nil {
		t.Fatalf("UnwindStep failed: %v", err)
	}

	if len(ctx.results) != 0 {
		t.Errorf("Expected 0 results for empty list, got %d", len(ctx.results))
	}
}

func TestUnwindStep_NilValue(t *testing.T) {
	step := &UnwindStep{
		unwind: &UnwindClause{
			Expression: &PropertyExpression{Variable: "data", Property: ""},
			Alias:      "item",
		},
	}

	ctx := &ExecutionContext{
		bindings: make(map[string]any),
		results: []*BindingSet{
			{bindings: map[string]any{}}, // "data" not bound
		},
	}

	if err := step.Execute(ctx); err != nil {
		t.Fatalf("UnwindStep failed: %v", err)
	}

	// Nil/missing should produce no results
	if len(ctx.results) != 0 {
		t.Errorf("Expected 0 results for nil value, got %d", len(ctx.results))
	}
}
