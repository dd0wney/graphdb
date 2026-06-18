package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestExecutor_Limit(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple nodes
	for i := 0; i < 5; i++ {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n.name LIMIT 2
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
		Limit: 2,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Expected 2 results, got %d", result.Count)
	}
}

// TestExecutor_Skip tests SKIP clause

func TestExecutor_Skip(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple nodes
	for i := 0; i < 5; i++ {
		_, _ = gs.CreateNode([]string{"Person"}, nil)
	}

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n SKIP 3
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
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
		Skip: 3,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Expected 2 results (5-3), got %d", result.Count)
	}
}

// TestExecutor_SkipExceedsResults tests SKIP exceeding result count

func TestExecutor_SkipExceedsResults(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create 2 nodes
	_, _ = gs.CreateNode([]string{"Person"}, nil)
	_, _ = gs.CreateNode([]string{"Person"}, nil)

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n SKIP 10
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
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
		Skip: 10,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("Expected 0 results when skip exceeds count, got %d", result.Count)
	}
}

// TestExecutor_Distinct tests DISTINCT clause

func TestExecutor_Distinct(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes with duplicate names
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN DISTINCT n.name
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
			Distinct: true,
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	// Should get 2 distinct names: Alice and Bob
	if result.Count != 2 {
		t.Errorf("Expected 2 distinct results, got %d", result.Count)
	}
}

// TestExecutor_EmptyMatch tests MATCH on empty graph

func TestExecutor_SortRows_Ascending(t *testing.T) {
	executor := NewExecutor(nil) // Don't need graph for sorting test

	rows := []map[string]any{
		{"n.age": int64(30)},
		{"n.age": int64(25)},
		{"n.age": int64(35)},
	}

	orderBy := []*OrderByItem{
		{
			Expression: &PropertyExpression{Variable: "n", Property: "age"},
			Ascending:  true,
		},
	}

	executor.sortRows(rows, orderBy)

	// Verify ascending order
	if rows[0]["n.age"].(int64) != 25 {
		t.Errorf("Expected first row age 25, got %d", rows[0]["n.age"])
	}
	if rows[1]["n.age"].(int64) != 30 {
		t.Errorf("Expected second row age 30, got %d", rows[1]["n.age"])
	}
	if rows[2]["n.age"].(int64) != 35 {
		t.Errorf("Expected third row age 35, got %d", rows[2]["n.age"])
	}
}

// TestExecutor_SortRows_Descending tests sorting rows in descending order

func TestExecutor_SortRows_Descending(t *testing.T) {
	executor := NewExecutor(nil)

	rows := []map[string]any{
		{"n.age": int64(30)},
		{"n.age": int64(25)},
		{"n.age": int64(35)},
	}

	orderBy := []*OrderByItem{
		{
			Expression: &PropertyExpression{Variable: "n", Property: "age"},
			Ascending:  false,
		},
	}

	executor.sortRows(rows, orderBy)

	// Verify descending order
	if rows[0]["n.age"].(int64) != 35 {
		t.Errorf("Expected first row age 35, got %d", rows[0]["n.age"])
	}
	if rows[1]["n.age"].(int64) != 30 {
		t.Errorf("Expected second row age 30, got %d", rows[1]["n.age"])
	}
	if rows[2]["n.age"].(int64) != 25 {
		t.Errorf("Expected third row age 25, got %d", rows[2]["n.age"])
	}
}

// TestExecutor_SortRows_EmptyOrderBy tests that no sorting occurs with empty order by

func TestExecutor_SortRows_EmptyOrderBy(t *testing.T) {
	executor := NewExecutor(nil)

	rows := []map[string]any{
		{"n.age": int64(30)},
		{"n.age": int64(25)},
	}

	originalFirst := rows[0]["n.age"]

	executor.sortRows(rows, []*OrderByItem{})

	// Should remain unchanged
	if rows[0]["n.age"] != originalFirst {
		t.Error("Rows should not be sorted with empty order by")
	}
}

// TestExecutor_SortRows_Strings tests sorting string values

func TestExecutor_SortRows_Strings(t *testing.T) {
	executor := NewExecutor(nil)

	rows := []map[string]any{
		{"n.name": "Charlie"},
		{"n.name": "Alice"},
		{"n.name": "Bob"},
	}

	orderBy := []*OrderByItem{
		{
			Expression: &PropertyExpression{Variable: "n", Property: "name"},
			Ascending:  true,
		},
	}

	executor.sortRows(rows, orderBy)

	// Verify alphabetical order
	if rows[0]["n.name"].(string) != "Alice" {
		t.Errorf("Expected first row name Alice, got %s", rows[0]["n.name"])
	}
	if rows[1]["n.name"].(string) != "Bob" {
		t.Errorf("Expected second row name Bob, got %s", rows[1]["n.name"])
	}
	if rows[2]["n.name"].(string) != "Charlie" {
		t.Errorf("Expected third row name Charlie, got %s", rows[2]["n.name"])
	}
}

// TestExecutor_Aggregation_COUNT tests COUNT aggregation
