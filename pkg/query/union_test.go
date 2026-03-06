package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestLexer_UnionTokens(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		types  []TokenType
	}{
		{
			name:  "UNION",
			input: "UNION",
			types: []TokenType{TokenUnion, TokenEOF},
		},
		{
			name:  "UNION ALL",
			input: "UNION ALL",
			types: []TokenType{TokenUnion, TokenAll, TokenEOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tokens, err := lexer.Tokenize()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tokens) != len(tt.types) {
				t.Fatalf("expected %d tokens, got %d", len(tt.types), len(tokens))
			}
			for i, want := range tt.types {
				if tokens[i].Type != want {
					t.Errorf("token[%d] type = %v, want %v", i, tokens[i].Type, want)
				}
			}
		})
	}
}

func TestParser_Union(t *testing.T) {
	input := `MATCH (n:Person) RETURN n.name AS name UNION MATCH (n:Company) RETURN n.name AS name`
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

	if query.Union == nil {
		t.Fatal("expected Union clause")
	}
	if query.Union.All {
		t.Error("expected UNION (not ALL)")
	}
	if query.UnionNext == nil {
		t.Fatal("expected UnionNext query")
	}
	if query.UnionNext.Match == nil {
		t.Error("expected MATCH in second segment")
	}
}

func TestParser_UnionAll(t *testing.T) {
	input := `MATCH (n:Person) RETURN n.name UNION ALL MATCH (n:Person) RETURN n.name`
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

	if query.Union == nil {
		t.Fatal("expected Union clause")
	}
	if !query.Union.All {
		t.Error("expected UNION ALL")
	}
}

func TestParser_UnionChain(t *testing.T) {
	input := `MATCH (a) RETURN a.x UNION MATCH (b) RETURN b.x UNION MATCH (c) RETURN c.x`
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

	if query.Union == nil || query.UnionNext == nil {
		t.Fatal("expected first UNION link")
	}
	if query.UnionNext.Union == nil || query.UnionNext.UnionNext == nil {
		t.Fatal("expected second UNION link (three-way chain)")
	}
}

func TestExecutor_UnionDedup(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	executor := NewExecutor(gs)

	// UNION deduplicates: same query twice should produce unique results
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name UNION MATCH (n:Person) RETURN n.name AS name`)

	if result.Count != 2 {
		t.Errorf("UNION should deduplicate: expected 2 rows, got %d", result.Count)
	}
}

func TestExecutor_UnionAll(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	executor := NewExecutor(gs)

	// UNION ALL preserves duplicates
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name UNION ALL MATCH (n:Person) RETURN n.name AS name`)

	if result.Count != 4 {
		t.Errorf("UNION ALL should keep duplicates: expected 4 rows, got %d", result.Count)
	}
}

func TestExecutor_UnionDifferentLabels(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Company"}, map[string]storage.Value{
		"name": storage.StringValue("Acme"),
	})

	executor := NewExecutor(gs)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name UNION MATCH (n:Company) RETURN n.name AS name`)

	if result.Count != 2 {
		t.Errorf("expected 2 rows, got %d", result.Count)
	}

	names := make(map[string]bool)
	for _, row := range result.Rows {
		if n, ok := row["name"].(string); ok {
			names[n] = true
		}
	}
	if !names["Alice"] || !names["Acme"] {
		t.Errorf("expected Alice and Acme, got %v", names)
	}
}

func TestExecutor_UnionColumnMismatch(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	lexer := NewLexer(`MATCH (n:Person) RETURN n.name UNION MATCH (n:Person) RETURN n.name, n.age`)
	tokens, _ := lexer.Tokenize()
	parser := NewParser(tokens)
	query, _ := parser.Parse()

	_, err := executor.Execute(query)
	if err == nil {
		t.Error("expected error for column count mismatch, got nil")
	}
}

func TestExecutor_UnionEmptySide(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	// No Company nodes exist, so second side is empty
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name UNION MATCH (n:Company) RETURN n.name AS name`)

	if result.Count != 1 {
		t.Errorf("expected 1 row (only Person side), got %d", result.Count)
	}
}
