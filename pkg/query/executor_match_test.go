package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestExecutor_MatchSingleNode(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test node
	props := map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	}
	node, err := gs.CreateNode([]string{"Person"}, props)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
						},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "name",
					},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 result, got %d", result.Count)
	}

	if len(result.Rows) != 1 {
		t.Errorf("Expected 1 row, got %d", len(result.Rows))
	}

	// Verify the result
	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("Expected name 'Alice', got %v", result.Rows[0]["n.name"])
	}

	_ = node // use node variable
}

// TestExecutor_MatchWithProperties tests MATCH with property filtering

func TestExecutor_MatchWithProperties(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple nodes
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person {name: "Alice"}) RETURN n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
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
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "name",
					},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 result, got %d", result.Count)
	}

	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("Expected name 'Alice', got %v", result.Rows[0]["n.name"])
	}
}

// TestExecutor_MatchPath tests MATCH query with relationships

func TestExecutor_MatchPath(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes
	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	// Create relationship
	_, _ = gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)

	executor := NewExecutor(gs)

	// Build query: MATCH (a:Person)-[:KNOWS]->(b:Person) RETURN a.name, b.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "a", Labels: []string{"Person"}},
						{Variable: "b", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{
						{
							Type:      "KNOWS",
							Direction: DirectionOutgoing,
						},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "a", Property: "name"}},
				{Expression: &PropertyExpression{Variable: "b", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 result, got %d", result.Count)
	}

	if result.Rows[0]["a.name"] != "Alice" {
		t.Errorf("Expected a.name 'Alice', got %v", result.Rows[0]["a.name"])
	}

	if result.Rows[0]["b.name"] != "Bob" {
		t.Errorf("Expected b.name 'Bob', got %v", result.Rows[0]["b.name"])
	}
}

// TestExecutor_CreateNode tests CREATE query

func TestExecutor_EmptyMatch(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
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
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("Expected 0 results on empty graph, got %d", result.Count)
	}
}

// TestExecutor_ExecuteWithText tests query caching

func TestExecutor_ExecuteWithText(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create node
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	queryText := "MATCH (n:Person) RETURN n.name"

	// First execution - should cache
	result1, err := executor.ExecuteWithText(queryText, query)
	if err != nil {
		t.Fatalf("Failed to execute query first time: %v", err)
	}

	// Second execution - should use cache
	result2, err := executor.ExecuteWithText(queryText, query)
	if err != nil {
		t.Fatalf("Failed to execute query second time: %v", err)
	}

	if result1.Count != result2.Count {
		t.Errorf("Expected same results from cached query, got %d and %d", result1.Count, result2.Count)
	}
}

// TestMatchStep_CopyBinding tests binding copy
