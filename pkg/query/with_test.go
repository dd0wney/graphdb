package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestParser_With(t *testing.T) {
	input := `MATCH (n:Person) WITH n.name AS name RETURN name`
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

	if query.Match == nil {
		t.Fatal("Expected non-nil Match clause")
	}
	if query.With == nil {
		t.Fatal("Expected non-nil With clause")
	}
	if len(query.With.Items) != 1 {
		t.Fatalf("Expected 1 WITH item, got %d", len(query.With.Items))
	}
	if query.With.Items[0].Alias != "name" {
		t.Errorf("Expected alias 'name', got %q", query.With.Items[0].Alias)
	}
	if query.Next == nil {
		t.Fatal("Expected non-nil Next query")
	}
	if query.Next.Return == nil {
		t.Fatal("Expected non-nil Return in Next query")
	}
}

func TestParser_WithWhere(t *testing.T) {
	input := `MATCH (n:Person) WITH n.name AS name, n.age AS age WHERE age > 25 RETURN name`
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

	if query.With == nil {
		t.Fatal("Expected non-nil With clause")
	}
	if len(query.With.Items) != 2 {
		t.Fatalf("Expected 2 WITH items, got %d", len(query.With.Items))
	}
	if query.With.Where == nil {
		t.Fatal("Expected non-nil WHERE after WITH")
	}
}

func TestWith_BasicProjection(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})

	executor := NewExecutor(gs)

	// MATCH (n:Person) WITH n.name AS name RETURN name
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
		With: &WithClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{Variable: "n", Property: "name"},
					Alias:      "name",
				},
			},
		},
		Next: &Query{
			Return: &ReturnClause{
				Items: []*ReturnItem{
					{Expression: &PropertyExpression{Variable: "name", Property: ""}},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Count != 2 {
		t.Fatalf("Expected 2 results, got %d", result.Count)
	}

	// Verify the projected names are present
	names := make(map[string]bool)
	for _, row := range result.Rows {
		if name, ok := row["name."].(string); ok {
			names[name] = true
		}
	}
	if !names["Alice"] {
		t.Error("Expected 'Alice' in results")
	}
	if !names["Bob"] {
		t.Error("Expected 'Bob' in results")
	}
}

func TestWith_FilterAfterWith(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
	})

	executor := NewExecutor(gs)

	// MATCH (n:Person) WITH n.name AS name, n.age AS age WHERE age > 28 RETURN name
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
		With: &WithClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{Variable: "n", Property: "name"},
					Alias:      "name",
				},
				{
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
					Alias:      "age",
				},
			},
			Where: &WhereClause{
				Expression: &BinaryExpression{
					Left:     &PropertyExpression{Variable: "age", Property: ""},
					Operator: ">",
					Right:    &LiteralExpression{Value: int64(28)},
				},
			},
		},
		Next: &Query{
			Return: &ReturnClause{
				Items: []*ReturnItem{
					{Expression: &PropertyExpression{Variable: "name", Property: ""}},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should only return Alice (30) and Charlie (35), not Bob (25)
	if result.Count != 2 {
		t.Fatalf("Expected 2 results, got %d", result.Count)
	}
}

func TestWith_PassWholeNode(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	// MATCH (n:Person) WITH n AS person RETURN person.name
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
		With: &WithClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{Variable: "n", Property: ""},
					Alias:      "person",
				},
			},
		},
		Next: &Query{
			Return: &ReturnClause{
				Items: []*ReturnItem{
					{Expression: &PropertyExpression{Variable: "person", Property: "name"}},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["person.name"] != "Alice" {
		t.Errorf("Expected 'Alice', got %v", result.Rows[0]["person.name"])
	}
}
