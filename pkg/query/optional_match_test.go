package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestLexer_OptionalToken(t *testing.T) {
	input := `OPTIONAL MATCH`
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tokens[0].Type != TokenOptional {
		t.Errorf("token[0] type = %v, want OPTIONAL", tokens[0].Type)
	}
	if tokens[1].Type != TokenMatch {
		t.Errorf("token[1] type = %v, want MATCH", tokens[1].Type)
	}
}

func TestParser_OptionalMatch(t *testing.T) {
	input := `MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name`
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

	if query.Match == nil {
		t.Fatal("expected required MATCH clause")
	}
	if len(query.OptionalMatches) != 1 {
		t.Fatalf("expected 1 optional match, got %d", len(query.OptionalMatches))
	}

	om := query.OptionalMatches[0]
	if len(om.Patterns) == 0 {
		t.Fatal("expected patterns in optional match")
	}
}

func TestParser_MultipleOptionalMatches(t *testing.T) {
	input := `MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b) OPTIONAL MATCH (a)-[:LIKES]->(c) RETURN a.name`
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

	if len(query.OptionalMatches) != 2 {
		t.Fatalf("expected 2 optional matches, got %d", len(query.OptionalMatches))
	}
}

func TestParser_OptionalMatchWithWhere(t *testing.T) {
	input := `MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b) WHERE b.age > 30 RETURN a.name, b.name`
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

	if len(query.OptionalMatches) != 1 {
		t.Fatalf("expected 1 optional match, got %d", len(query.OptionalMatches))
	}

	om := query.OptionalMatches[0]
	if om.Where == nil {
		t.Error("expected WHERE attached to optional match")
	}

	// The global WHERE should be nil — the WHERE belongs to the OPTIONAL MATCH
	if query.Where != nil {
		t.Error("global WHERE should be nil; WHERE should be attached to OPTIONAL MATCH")
	}
}

func TestExecutor_OptionalMatch_WithMatches(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)

	executor := NewExecutor(gs)

	result := parseAndExecute(t, executor,
		`MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name`)

	// Alice -> Bob match found, so Alice has b.name = "Bob"
	// Bob has no outgoing KNOWS, so b.name = nil
	if result.Count != 2 {
		t.Fatalf("expected 2 rows, got %d", result.Count)
	}

	foundAliceWithBob := false
	foundBobWithNil := false
	for _, row := range result.Rows {
		aName := row["a.name"]
		bName := row["b.name"]
		if aName == "Alice" && bName == "Bob" {
			foundAliceWithBob = true
		}
		if aName == "Bob" && bName == nil {
			foundBobWithNil = true
		}
	}

	if !foundAliceWithBob {
		t.Error("expected row: Alice -> Bob")
	}
	if !foundBobWithNil {
		t.Error("expected row: Bob -> nil (no match)")
	}
}

func TestExecutor_OptionalMatch_NullPropagation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Loner"),
	})

	executor := NewExecutor(gs)

	result := parseAndExecute(t, executor,
		`MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}

	row := result.Rows[0]
	if row["a.name"] != "Loner" {
		t.Errorf("a.name = %v, want Loner", row["a.name"])
	}
	if row["b.name"] != nil {
		t.Errorf("b.name = %v, want nil", row["b.name"])
	}
}

func TestExecutor_OptionalMatch_WithWhereFilter(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	charlie, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
	})
	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)
	gs.CreateEdge(alice.ID, charlie.ID, "KNOWS", nil, 1.0)

	executor := NewExecutor(gs)

	// WHERE on OPTIONAL MATCH filters within the optional, null if all filtered out
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: "Alice"}) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) WHERE b.age > 30 RETURN a.name, b.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}

	row := result.Rows[0]
	if row["a.name"] != "Alice" {
		t.Errorf("a.name = %v, want Alice", row["a.name"])
	}
	// Only Charlie (age 35) should match the WHERE
	if row["b.name"] != "Charlie" {
		t.Errorf("b.name = %v, want Charlie", row["b.name"])
	}
}

func TestExecutor_OptionalMatch_StandaloneOptional(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	// OPTIONAL MATCH without a preceding required MATCH
	result := parseAndExecute(t, executor,
		`OPTIONAL MATCH (n:Person) RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("n.name = %v, want Alice", result.Rows[0]["n.name"])
	}
}
