package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestParser_Merge(t *testing.T) {
	input := `MERGE (n:Person {name: "Alice"}) ON CREATE SET n.created = true ON MATCH SET n.seen = true`
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

	if query.Merge == nil {
		t.Fatal("Expected non-nil Merge clause")
	}
	if query.Merge.Pattern == nil {
		t.Fatal("Expected non-nil Merge pattern")
	}
	if len(query.Merge.Pattern.Nodes) != 1 {
		t.Fatalf("Expected 1 node in pattern, got %d", len(query.Merge.Pattern.Nodes))
	}
	if query.Merge.Pattern.Nodes[0].Variable != "n" {
		t.Errorf("Expected variable 'n', got %q", query.Merge.Pattern.Nodes[0].Variable)
	}

	if query.Merge.OnCreate == nil {
		t.Fatal("Expected non-nil OnCreate")
	}
	if len(query.Merge.OnCreate.Assignments) != 1 {
		t.Fatalf("Expected 1 OnCreate assignment, got %d", len(query.Merge.OnCreate.Assignments))
	}
	if query.Merge.OnCreate.Assignments[0].Property != "created" {
		t.Errorf("Expected OnCreate property 'created', got %q", query.Merge.OnCreate.Assignments[0].Property)
	}

	if query.Merge.OnMatch == nil {
		t.Fatal("Expected non-nil OnMatch")
	}
	if len(query.Merge.OnMatch.Assignments) != 1 {
		t.Fatalf("Expected 1 OnMatch assignment, got %d", len(query.Merge.OnMatch.Assignments))
	}
	if query.Merge.OnMatch.Assignments[0].Property != "seen" {
		t.Errorf("Expected OnMatch property 'seen', got %q", query.Merge.OnMatch.Assignments[0].Property)
	}
}

func TestParser_Merge_Simple(t *testing.T) {
	input := `MERGE (n:Person {name: "Alice"})`
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

	if query.Merge == nil {
		t.Fatal("Expected non-nil Merge clause")
	}
	if query.Merge.OnCreate != nil {
		t.Error("Expected nil OnCreate for simple MERGE")
	}
	if query.Merge.OnMatch != nil {
		t.Error("Expected nil OnMatch for simple MERGE")
	}
}

func TestMerge_CreateWhenNotExists(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// MERGE should create the node since it doesn't exist
	query := &Query{
		Merge: &MergeClause{
			Pattern: &Pattern{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
						Properties: map[string]any{
							"name": "Alice",
						},
					},
				},
				Relationships: []*RelationshipPattern{},
			},
		},
	}

	_, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify node was created
	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", stats.NodeCount)
	}
}

func TestMerge_MatchWhenExists(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Pre-create Alice
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	// MERGE should find the existing node, not create a duplicate
	query := &Query{
		Merge: &MergeClause{
			Pattern: &Pattern{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
						Properties: map[string]any{
							"name": "Alice",
						},
					},
				},
				Relationships: []*RelationshipPattern{},
			},
		},
	}

	_, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify no duplicate was created
	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node (no duplicate), got %d", stats.NodeCount)
	}
}

func TestMerge_OnCreateSet(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// MERGE with ON CREATE SET
	query := &Query{
		Merge: &MergeClause{
			Pattern: &Pattern{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
						Properties: map[string]any{
							"name": "Alice",
						},
					},
				},
				Relationships: []*RelationshipPattern{},
			},
			OnCreate: &SetClause{
				Assignments: []*Assignment{
					{Variable: "n", Property: "created", Value: true},
				},
			},
		},
	}

	_, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify node was created with the ON CREATE SET property
	node, err := gs.GetNode(1)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	if created, ok := node.Properties["created"]; ok {
		b, _ := created.AsBool()
		if !b {
			t.Error("Expected created=true")
		}
	} else {
		t.Error("Expected 'created' property from ON CREATE SET")
	}
}

func TestMerge_OnMatchSet(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Pre-create Alice
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	// MERGE with ON MATCH SET
	query := &Query{
		Merge: &MergeClause{
			Pattern: &Pattern{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
						Properties: map[string]any{
							"name": "Alice",
						},
					},
				},
				Relationships: []*RelationshipPattern{},
			},
			OnMatch: &SetClause{
				Assignments: []*Assignment{
					{Variable: "n", Property: "seen", Value: true},
				},
			},
		},
	}

	_, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify ON MATCH SET was applied
	node, err := gs.GetNode(1)
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	if seen, ok := node.Properties["seen"]; ok {
		b, _ := seen.AsBool()
		if !b {
			t.Error("Expected seen=true")
		}
	} else {
		t.Error("Expected 'seen' property from ON MATCH SET")
	}

	// Verify no duplicate created
	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", stats.NodeCount)
	}
}
