package query

import (
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestFunctionRegistry(t *testing.T) {
	// Register a test function
	RegisterFunction("testFunc", func(args []any) (any, error) {
		return "hello", nil
	})

	fn, err := GetFunction("testFunc")
	if err != nil {
		t.Fatalf("GetFunction failed: %v", err)
	}

	result, err := fn(nil)
	if err != nil {
		t.Fatalf("Function call failed: %v", err)
	}
	if result != "hello" {
		t.Errorf("Expected 'hello', got %v", result)
	}

	// Unknown function
	_, err = GetFunction("noSuchFunc")
	if err == nil {
		t.Error("Expected error for unknown function")
	}
}

func TestFunctionCallExpression_InWhere(t *testing.T) {
	// Register a test function
	RegisterFunction("testContains", func(args []any) (any, error) {
		if len(args) < 2 {
			return false, nil
		}
		s, _ := args[0].(string)
		substr, _ := args[1].(string)
		return strings.Contains(s, substr), nil
	})

	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice Smith"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob Jones"),
	})

	executor := NewExecutor(gs)

	// WHERE testContains(n.name, "Smith") — should match Alice only
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
		Where: &WhereClause{
			Expression: &FunctionCallExpression{
				Name: "testContains",
				Args: []Expression{
					&PropertyExpression{Variable: "n", Property: "name"},
					&LiteralExpression{Value: "Smith"},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
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
	if result.Rows[0]["n.name"] != "Alice Smith" {
		t.Errorf("Expected 'Alice Smith', got %v", result.Rows[0]["n.name"])
	}
}

func TestFunctionCallExpression_InComparison(t *testing.T) {
	RegisterFunction("testLower", func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		s, _ := args[0].(string)
		return strings.ToLower(s), nil
	})

	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("ALICE"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	executor := NewExecutor(gs)

	// WHERE testLower(n.name) = "alice"
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
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &FunctionCallExpression{
					Name: "testLower",
					Args: []Expression{
						&PropertyExpression{Variable: "n", Property: "name"},
					},
				},
				Operator: "=",
				Right:    &LiteralExpression{Value: "alice"},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
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
	if result.Rows[0]["n.name"] != "ALICE" {
		t.Errorf("Expected 'ALICE', got %v", result.Rows[0]["n.name"])
	}
}

func TestFunctionCallExpression_InReturn(t *testing.T) {
	RegisterFunction("testUpper", func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		s, _ := args[0].(string)
		return strings.ToUpper(s), nil
	})

	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("alice"),
	})

	executor := NewExecutor(gs)

	// RETURN testUpper(n.name) AS upper_name
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
				{
					ValueExpr: &FunctionCallExpression{
						Name: "testUpper",
						Args: []Expression{
							&PropertyExpression{Variable: "n", Property: "name"},
						},
					},
					Alias: "upper_name",
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
	if result.Rows[0]["upper_name"] != "ALICE" {
		t.Errorf("Expected 'ALICE', got %v", result.Rows[0]["upper_name"])
	}
}

func TestParser_FunctionCallInWhere(t *testing.T) {
	RegisterFunction("toLower", func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		s, _ := args[0].(string)
		return strings.ToLower(s), nil
	})

	input := `MATCH (n:Person) WHERE toLower(n.name) = "alice" RETURN n.name`
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

	if query.Where == nil {
		t.Fatal("Expected non-nil Where clause")
	}

	// The WHERE clause should contain a binary expression with a function call on the left
	binExpr, ok := query.Where.Expression.(*BinaryExpression)
	if !ok {
		t.Fatalf("Expected BinaryExpression, got %T", query.Where.Expression)
	}

	fnExpr, ok := binExpr.Left.(*FunctionCallExpression)
	if !ok {
		t.Fatalf("Expected FunctionCallExpression on left, got %T", binExpr.Left)
	}

	if fnExpr.Name != "toLower" {
		t.Errorf("Expected function name 'toLower', got %q", fnExpr.Name)
	}
	if len(fnExpr.Args) != 1 {
		t.Fatalf("Expected 1 argument, got %d", len(fnExpr.Args))
	}
}

func TestParser_FunctionCallInReturn(t *testing.T) {
	RegisterFunction("toUpper", func(args []any) (any, error) { return nil, nil })

	input := `MATCH (n:Person) RETURN toUpper(n.name) AS upper`
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

	if query.Return == nil {
		t.Fatal("Expected non-nil Return clause")
	}

	if len(query.Return.Items) != 1 {
		t.Fatalf("Expected 1 return item, got %d", len(query.Return.Items))
	}

	item := query.Return.Items[0]
	if item.ValueExpr == nil {
		t.Fatal("Expected non-nil ValueExpr")
	}

	fnExpr, ok := item.ValueExpr.(*FunctionCallExpression)
	if !ok {
		t.Fatalf("Expected FunctionCallExpression, got %T", item.ValueExpr)
	}
	if fnExpr.Name != "toUpper" {
		t.Errorf("Expected 'toUpper', got %q", fnExpr.Name)
	}
	if item.Alias != "upper" {
		t.Errorf("Expected alias 'upper', got %q", item.Alias)
	}
}
