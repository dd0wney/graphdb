package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/search"
	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestConformance_StringFunctionsInWhere(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	tests := []struct {
		name     string
		query    string
		expected int // expected row count
	}{
		{
			name:     "toLower comparison",
			query:    `MATCH (n:Person) WHERE toLower(n.name) = "alice" RETURN n.name`,
			expected: 1,
		},
		{
			name:     "startsWith",
			query:    `MATCH (n:Person) WHERE startsWith(n.name, "Al") RETURN n.name`,
			expected: 1,
		},
		{
			name:     "contains",
			query:    `MATCH (n:Person) WHERE contains(n.name, "li") RETURN n.name`,
			expected: 2, // Alice and Charlie both contain "li"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAndExecute(t, executor, tt.query)
			if result.Count != tt.expected {
				t.Errorf("Expected %d results, got %d", tt.expected, result.Count)
			}
		})
	}
}

func TestConformance_StringFunctionsInReturn(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = "Alice" RETURN toUpper(n.name) AS upper_name`)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["upper_name"] != "ALICE" {
		t.Errorf("Expected 'ALICE', got %v", result.Rows[0]["upper_name"])
	}
}

func TestConformance_NumericFunctionsInWhere(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// abs(-5) = 5, toFloat(n.age) works
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE toFloat(n.age) > 29.5 RETURN n.name`)

	// Alice (30) and Charlie (35) should match
	if result.Count != 2 {
		t.Errorf("Expected 2 results for toFloat(n.age) > 29.5, got %d", result.Count)
	}
}

func TestConformance_SearchFunction(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	_, _ = gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Quantum Computing Revolution"),
	})
	_, _ = gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Classical Music Guide"),
	})

	executor := NewExecutor(gs)
	idx := search.NewFullTextIndex(gs)
	executor.SetSearchIndex(idx)

	result := parseAndExecute(t, executor,
		`MATCH (n:Article) WHERE search(n.title, "quantum computing") > 0.5 RETURN n.title`)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.title"] != "Quantum Computing Revolution" {
		t.Errorf("Expected 'Quantum Computing Revolution', got %v", result.Rows[0]["n.title"])
	}
}
