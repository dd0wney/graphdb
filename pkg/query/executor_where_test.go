package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestExecutor_WhereClause(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with different ages
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(25),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(30),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
	})

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) WHERE n.age > 28 RETURN n
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
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "n",
					Property: "age",
				},
				Operator: ">",
				Right: &LiteralExpression{
					Value: int64(28),
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute WHERE query: %v", err)
	}

	// Should only return Bob (30) and Charlie (35)
	if result.Count != 2 {
		t.Errorf("Expected 2 results, got %d", result.Count)
	}
}

// TestExecutor_WhereClause_Equals tests WHERE with equality

func TestExecutor_WhereClause_Equals(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(25),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) WHERE n.age = 25 RETURN n
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
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "n",
					Property: "age",
				},
				Operator: "=",
				Right: &LiteralExpression{
					Value: int64(25),
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute WHERE query: %v", err)
	}

	// Should return Alice and Bob (both age 25)
	if result.Count != 2 {
		t.Errorf("Expected 2 results, got %d", result.Count)
	}
}

// TestExecutor_CreateRelationship tests creating relationships
