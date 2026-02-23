package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestLexer_CaseTokens(t *testing.T) {
	input := `CASE WHEN THEN ELSE END`
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []TokenType{TokenCase, TokenWhen, TokenThen, TokenElse, TokenEnd, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(tokens))
	}
	for i, want := range expected {
		if tokens[i].Type != want {
			t.Errorf("token[%d] type = %v, want %v", i, tokens[i].Type, want)
		}
	}
}

func TestParser_SearchedCase(t *testing.T) {
	input := `MATCH (n:Person) RETURN CASE WHEN n.age > 30 THEN "senior" ELSE "junior" END AS category`
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}

	if query.Return == nil || len(query.Return.Items) != 1 {
		t.Fatal("expected 1 return item")
	}

	item := query.Return.Items[0]
	if item.Alias != "category" {
		t.Errorf("alias = %q, want %q", item.Alias, "category")
	}

	caseExpr, ok := item.ValueExpr.(*CaseExpression)
	if !ok {
		t.Fatalf("expected CaseExpression, got %T", item.ValueExpr)
	}
	if caseExpr.Operand != nil {
		t.Error("searched CASE should have nil Operand")
	}
	if len(caseExpr.WhenClauses) != 1 {
		t.Fatalf("expected 1 WHEN clause, got %d", len(caseExpr.WhenClauses))
	}
	if caseExpr.ElseResult == nil {
		t.Error("expected ELSE result")
	}
}

func TestParser_SimpleCase(t *testing.T) {
	input := `MATCH (n:Person) RETURN CASE n.department WHEN "Engineering" THEN "eng" WHEN "Sales" THEN "sales" END AS dept`
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("lexer error: %v", err)
	}

	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser error: %v", err)
	}

	item := query.Return.Items[0]
	caseExpr, ok := item.ValueExpr.(*CaseExpression)
	if !ok {
		t.Fatalf("expected CaseExpression, got %T", item.ValueExpr)
	}
	if caseExpr.Operand == nil {
		t.Error("simple CASE should have non-nil Operand")
	}
	if len(caseExpr.WhenClauses) != 2 {
		t.Fatalf("expected 2 WHEN clauses, got %d", len(caseExpr.WhenClauses))
	}
	if caseExpr.ElseResult != nil {
		t.Error("expected nil ELSE result")
	}
}

func TestCaseExpression_EvalValue_Searched(t *testing.T) {
	// CASE WHEN age > 30 THEN "senior" ELSE "junior" END
	caseExpr := &CaseExpression{
		WhenClauses: []CaseWhen{
			{
				Condition: &BinaryExpression{
					Left:     &PropertyExpression{Variable: "n", Property: "age"},
					Operator: ">",
					Right:    &LiteralExpression{Value: int64(30)},
				},
				Result: &LiteralExpression{Value: "senior"},
			},
		},
		ElseResult: &LiteralExpression{Value: "junior"},
	}

	tests := []struct {
		name string
		age  int64
		want string
	}{
		{"age 35 → senior", 35, "senior"},
		{"age 25 → junior", 25, "junior"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := map[string]any{
				"n": &storage.Node{
					Properties: map[string]storage.Value{
						"age": storage.IntValue(tt.age),
					},
				},
			}
			result, err := caseExpr.EvalValue(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("result = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestCaseExpression_EvalValue_Simple(t *testing.T) {
	// CASE n.department WHEN "Engineering" THEN 1 WHEN "Sales" THEN 2 END
	caseExpr := &CaseExpression{
		Operand: &PropertyExpression{Variable: "n", Property: "department"},
		WhenClauses: []CaseWhen{
			{
				Condition: &LiteralExpression{Value: "Engineering"},
				Result:    &LiteralExpression{Value: int64(1)},
			},
			{
				Condition: &LiteralExpression{Value: "Sales"},
				Result:    &LiteralExpression{Value: int64(2)},
			},
		},
	}

	tests := []struct {
		name string
		dept string
		want any
	}{
		{"Engineering → 1", "Engineering", int64(1)},
		{"Sales → 2", "Sales", int64(2)},
		{"Marketing → nil (no ELSE)", "Marketing", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := map[string]any{
				"n": &storage.Node{
					Properties: map[string]storage.Value{
						"department": storage.StringValue(tt.dept),
					},
				},
			}
			result, err := caseExpr.EvalValue(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("result = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestCaseExpression_SimpleCrosType(t *testing.T) {
	// Verify int64 matches float64 via compareValues (not interface equality)
	caseExpr := &CaseExpression{
		Operand: &LiteralExpression{Value: int64(1)},
		WhenClauses: []CaseWhen{
			{
				Condition: &LiteralExpression{Value: float64(1.0)},
				Result:    &LiteralExpression{Value: "matched"},
			},
		},
		ElseResult: &LiteralExpression{Value: "no match"},
	}

	result, err := caseExpr.EvalValue(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "matched" {
		t.Errorf("int64(1) should match float64(1.0), got %v", result)
	}
}

func TestCaseExpression_NoElse_ReturnsNil(t *testing.T) {
	caseExpr := &CaseExpression{
		WhenClauses: []CaseWhen{
			{
				Condition: &LiteralExpression{Value: false},
				Result:    &LiteralExpression{Value: "never"},
			},
		},
	}

	result, err := caseExpr.EvalValue(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestExecutor_CaseInReturn(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(35),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})

	executor := NewExecutor(gs)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name, CASE WHEN n.age > 30 THEN "senior" ELSE "junior" END AS category`)

	if result.Count != 2 {
		t.Fatalf("expected 2 rows, got %d", result.Count)
	}

	for _, row := range result.Rows {
		name := row["n.name"]
		cat := row["category"]
		switch name {
		case "Alice":
			if cat != "senior" {
				t.Errorf("Alice category = %v, want senior", cat)
			}
		case "Bob":
			if cat != "junior" {
				t.Errorf("Bob category = %v, want junior", cat)
			}
		}
	}
}

func TestExecutor_CaseInWhere(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(35),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})

	executor := NewExecutor(gs)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE CASE WHEN n.age > 30 THEN true ELSE false END RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("expected Alice, got %v", result.Rows[0]["n.name"])
	}
}
