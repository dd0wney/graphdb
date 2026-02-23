package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestLexer_ParameterToken(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  TokenType
		wantValue string
		wantErr   bool
	}{
		{
			name:      "simple parameter",
			input:     "$name",
			wantType:  TokenParameter,
			wantValue: "name",
		},
		{
			name:      "parameter with digits",
			input:     "$param1",
			wantType:  TokenParameter,
			wantValue: "param1",
		},
		{
			name:      "parameter with underscore",
			input:     "$min_age",
			wantType:  TokenParameter,
			wantValue: "min_age",
		},
		{
			name:    "dollar alone is error",
			input:   "$ ",
			wantErr: true,
		},
		{
			name:    "dollar followed by digit is error",
			input:   "$123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// First token should be the parameter (last is EOF)
			if len(tokens) < 2 {
				t.Fatalf("expected at least 2 tokens, got %d", len(tokens))
			}
			tok := tokens[0]
			if tok.Type != tt.wantType {
				t.Errorf("token type = %v, want %v", tok.Type, tt.wantType)
			}
			if tok.Value != tt.wantValue {
				t.Errorf("token value = %q, want %q", tok.Value, tt.wantValue)
			}
		})
	}
}

func TestParser_ParameterInWhere(t *testing.T) {
	input := `MATCH (n:Person) WHERE n.name = $name RETURN n`
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

	if query.Where == nil {
		t.Fatal("expected WHERE clause")
	}

	// The WHERE should contain a BinaryExpression with a ParameterExpression on the right
	binExpr, ok := query.Where.Expression.(*BinaryExpression)
	if !ok {
		t.Fatalf("expected BinaryExpression, got %T", query.Where.Expression)
	}

	paramExpr, ok := binExpr.Right.(*ParameterExpression)
	if !ok {
		t.Fatalf("expected ParameterExpression on right, got %T", binExpr.Right)
	}
	if paramExpr.Name != "name" {
		t.Errorf("parameter name = %q, want %q", paramExpr.Name, "name")
	}
}

func TestParser_ParameterInProperties(t *testing.T) {
	input := `MATCH (n:Person {name: $name}) RETURN n`
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

	if query.Match == nil || len(query.Match.Patterns) == 0 {
		t.Fatal("expected MATCH with patterns")
	}

	node := query.Match.Patterns[0].Nodes[0]
	val, exists := node.Properties["name"]
	if !exists {
		t.Fatal("expected 'name' property in pattern")
	}

	paramRef, ok := val.(*ParameterRef)
	if !ok {
		t.Fatalf("expected *ParameterRef, got %T", val)
	}
	if paramRef.Name != "name" {
		t.Errorf("parameter name = %q, want %q", paramRef.Name, "name")
	}
}

func TestExecutor_ParameterizedQuery(t *testing.T) {
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

	tests := []struct {
		name     string
		query    string
		params   map[string]any
		wantRows int
	}{
		{
			name:     "match by name parameter",
			query:    `MATCH (n:Person) WHERE n.name = $name RETURN n.name`,
			params:   map[string]any{"name": "Alice"},
			wantRows: 1,
		},
		{
			name:     "match by age parameter",
			query:    `MATCH (n:Person) WHERE n.age > $minAge RETURN n.name`,
			params:   map[string]any{"minAge": int64(26)},
			wantRows: 1,
		},
		{
			name:     "parameter in property map",
			query:    `MATCH (n:Person {name: $name}) RETURN n.name`,
			params:   map[string]any{"name": "Bob"},
			wantRows: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.query)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("lexer error: %v", err)
			}

			parser := NewParser(tokens)
			query, err := parser.Parse()
			if err != nil {
				t.Fatalf("parser error: %v", err)
			}

			result, err := executor.ExecuteWithParams(query, tt.params)
			if err != nil {
				t.Fatalf("execute error: %v", err)
			}

			if result.Count != tt.wantRows {
				t.Errorf("got %d rows, want %d", result.Count, tt.wantRows)
			}
		})
	}
}

func TestExecutor_MissingParameter(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	lexer := NewLexer(`MATCH (n:Person) WHERE n.name = $name RETURN n.name`)
	tokens, _ := lexer.Tokenize()
	parser := NewParser(tokens)
	query, _ := parser.Parse()

	// Empty params — $name is missing
	_, err := executor.ExecuteWithParams(query, map[string]any{})
	if err == nil {
		t.Error("expected error for missing parameter, got nil")
	}
}

func TestExecutor_ParameterInjectionSafety(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	lexer := NewLexer(`MATCH (n:Person) WHERE n.name = $name RETURN n.name`)
	tokens, _ := lexer.Tokenize()
	parser := NewParser(tokens)
	query, _ := parser.Parse()

	// Cypher injection attempt — should be treated as a literal string
	result, err := executor.ExecuteWithParams(query, map[string]any{
		"name": `" OR 1=1 RETURN n --`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match nothing since no node has that literal name
	if result.Count != 0 {
		t.Errorf("injection attempt should return 0 rows, got %d", result.Count)
	}
}
