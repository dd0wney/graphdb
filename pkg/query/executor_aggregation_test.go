package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestExecutor_Aggregation_COUNT(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes
	for i := 0; i < 5; i++ {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(int64(25 + i*5)),
		})
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN COUNT(n)
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
					Aggregate:  "COUNT",
					Expression: &PropertyExpression{Variable: "n"},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COUNT query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation, got %d", result.Count)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}

	// Check the count value
	count := result.Rows[0]["COUNT(n.)"]
	if count != 5 {
		t.Errorf("Expected COUNT=5, got %v", count)
	}
}

// TestExecutor_Aggregation_SUM tests SUM aggregation

func TestExecutor_Aggregation_SUM(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with salaries
	salaries := []int64{50000, 60000, 70000}
	for _, sal := range salaries {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"salary": storage.IntValue(sal),
		})
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN SUM(n.salary)
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
					Aggregate:  "SUM",
					Expression: &PropertyExpression{Variable: "n", Property: "salary"},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute SUM query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation, got %d", result.Count)
	}

	sum := result.Rows[0]["SUM(n.salary)"]
	if sum != int64(180000) {
		t.Errorf("Expected SUM=180000, got %v (type %T)", sum, sum)
	}
}

// TestExecutor_Aggregation_AVG tests AVG aggregation

func TestExecutor_Aggregation_AVG(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with ages
	ages := []int64{20, 30, 40}
	for _, age := range ages {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(age),
		})
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN AVG(n.age)
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
					Aggregate:  "AVG",
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute AVG query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation, got %d", result.Count)
	}

	avg := result.Rows[0]["AVG(n.age)"]
	if avg != float64(30) {
		t.Errorf("Expected AVG=30.0, got %v (type %T)", avg, avg)
	}
}

// TestExecutor_Aggregation_MIN_MAX tests MIN and MAX aggregations

func TestExecutor_Aggregation_MIN_MAX(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with ages
	ages := []int64{25, 30, 35, 28, 32}
	for _, age := range ages {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(age),
		})
	}

	executor := NewExecutor(gs)

	// Test MIN
	minQuery := &Query{
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
					Aggregate:  "MIN",
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
				},
			},
		},
	}

	result, err := executor.Execute(minQuery)
	if err != nil {
		t.Fatalf("Failed to execute MIN query: %v", err)
	}

	min := result.Rows[0]["MIN(n.age)"]
	if min != int64(25) {
		t.Errorf("Expected MIN=25, got %v (type %T)", min, min)
	}

	// Test MAX
	maxQuery := &Query{
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
					Aggregate:  "MAX",
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
				},
			},
		},
	}

	result, err = executor.Execute(maxQuery)
	if err != nil {
		t.Fatalf("Failed to execute MAX query: %v", err)
	}

	max := result.Rows[0]["MAX(n.age)"]
	if max != int64(35) {
		t.Errorf("Expected MAX=35, got %v (type %T)", max, max)
	}
}

// TestExecutor_GroupBy_Single tests GROUP BY with single property

func TestExecutor_Aggregation_MixedTypes(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes with mixed integer and float salaries
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"salary": storage.IntValue(50000),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"salary": storage.FloatValue(55000.50),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"salary": storage.IntValue(60000),
	})

	executor := NewExecutor(gs)

	// Test SUM with mixed types
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
				{Aggregate: "SUM", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute SUM with mixed types: %v", err)
	}

	sum := result.Rows[0]["SUM(n.salary)"]
	// Should handle mixed int/float correctly
	expectedSum := 165000.50
	if sumFloat, ok := sum.(float64); ok {
		if sumFloat < expectedSum-0.01 || sumFloat > expectedSum+0.01 {
			t.Errorf("Expected SUM≈%.2f, got %.2f", expectedSum, sumFloat)
		}
	} else {
		t.Errorf("Expected SUM to be float64, got %T", sum)
	}
}

// TestExecutor_Aggregation_NullValues tests aggregations with null/missing values

func TestExecutor_Aggregation_NullValues(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes where some have salary and some don't
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"salary": storage.IntValue(50000),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		// No salary property
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"salary": storage.IntValue(60000),
	})

	executor := NewExecutor(gs)

	// Test COUNT - should only count nodes with the property
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
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COUNT with nulls: %v", err)
	}

	count := result.Rows[0]["COUNT(n.salary)"]
	if count != 2 {
		t.Errorf("Expected COUNT=2 (only nodes with salary), got %v", count)
	}
}

// TestExecutor_GroupBy_LIMIT_SKIP tests GROUP BY with LIMIT and SKIP

func TestExecutor_Aggregation_StringMinMax(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	names := []string{"Zebra", "Alice", "Mike", "Bob"}
	for _, name := range names {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
	}

	executor := NewExecutor(gs)

	// Test MIN on strings
	minQuery := &Query{
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
				{Aggregate: "MIN", Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(minQuery)
	if err != nil {
		t.Fatalf("Failed to execute MIN on strings: %v", err)
	}

	min := result.Rows[0]["MIN(n.name)"]
	if min != "Alice" {
		t.Errorf("Expected MIN='Alice' (alphabetically first), got %v", min)
	}

	// Test MAX on strings
	maxQuery := &Query{
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
				{Aggregate: "MAX", Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err = executor.Execute(maxQuery)
	if err != nil {
		t.Fatalf("Failed to execute MAX on strings: %v", err)
	}

	max := result.Rows[0]["MAX(n.name)"]
	if max != "Zebra" {
		t.Errorf("Expected MAX='Zebra' (alphabetically last), got %v", max)
	}
}

// TestExecutor_DISTINCT_With_Aggregation tests DISTINCT with aggregation

func TestExecutor_Aggregation_COLLECT(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	names := []string{"Alice", "Bob", "Charlie"}
	for _, name := range names {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
	}

	executor := NewExecutor(gs)

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
					Aggregate:  "COLLECT",
					Expression: &PropertyExpression{Variable: "n", Property: "name"},
					Alias:      "names",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COLLECT query: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("Expected 1 row for aggregation, got %d", result.Count)
	}

	collected, ok := result.Rows[0]["names"].([]any)
	if !ok {
		t.Fatalf("Expected []any, got %T: %v", result.Rows[0]["names"], result.Rows[0]["names"])
	}

	if len(collected) != 3 {
		t.Errorf("Expected 3 collected values, got %d", len(collected))
	}

	// All names should be present (order depends on storage)
	nameSet := make(map[string]bool)
	for _, v := range collected {
		if s, ok := v.(string); ok {
			nameSet[s] = true
		}
	}
	for _, name := range names {
		if !nameSet[name] {
			t.Errorf("Expected %q in collected names", name)
		}
	}
}

// TestExecutor_Aggregation_COLLECT_Empty tests COLLECT on empty results

func TestExecutor_Aggregation_COLLECT_Empty(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

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
					Aggregate:  "COLLECT",
					Expression: &PropertyExpression{Variable: "n", Property: "name"},
					Alias:      "names",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COLLECT on empty: %v", err)
	}

	collected, ok := result.Rows[0]["names"].([]any)
	if !ok {
		t.Fatalf("Expected []any, got %T", result.Rows[0]["names"])
	}

	if len(collected) != 0 {
		t.Errorf("Expected empty collection, got %d items", len(collected))
	}
}

// TestExecutor_Aggregation_COLLECT_WithGroupBy tests COLLECT with GROUP BY

func TestExecutor_Aggregation_COLLECT_WithGroupBy(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	data := []struct{ dept, name string }{
		{"Engineering", "Alice"},
		{"Engineering", "Bob"},
		{"Sales", "Charlie"},
	}
	for _, d := range data {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(d.dept),
			"name":       storage.StringValue(d.name),
		})
	}

	executor := NewExecutor(gs)

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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{
					Aggregate:  "COLLECT",
					Expression: &PropertyExpression{Variable: "n", Property: "name"},
					Alias:      "names",
				},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COLLECT with GROUP BY: %v", err)
	}

	if result.Count != 2 {
		t.Fatalf("Expected 2 groups, got %d", result.Count)
	}

	for _, row := range result.Rows {
		dept := row["n.department"].(string)
		collected := row["names"].([]any)
		switch dept {
		case "Engineering":
			if len(collected) != 2 {
				t.Errorf("Engineering: expected 2 names, got %d", len(collected))
			}
		case "Sales":
			if len(collected) != 1 {
				t.Errorf("Sales: expected 1 name, got %d", len(collected))
			}
		}
	}
}

// Helper function to find a row by column value

func TestExecutor_GroupBy_Single(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with different departments
	departments := map[string][]int64{
		"Engineering": {50000, 60000, 70000},
		"Sales":       {40000, 45000},
		"Marketing":   {55000},
	}

	for dept, salaries := range departments {
		for _, sal := range salaries {
			_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
				"department": storage.StringValue(dept),
				"salary":     storage.IntValue(sal),
			})
		}
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN n.department, COUNT(n), AVG(n.salary) GROUP BY n.department
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY query: %v", err)
	}

	// Should return 3 rows (one per department)
	if result.Count != 3 {
		t.Errorf("Expected 3 grouped rows, got %d", result.Count)
	}

	// Verify each group
	groups := make(map[string]map[string]any)
	for _, row := range result.Rows {
		dept := row["n.department"].(string)
		groups[dept] = row
	}

	// Check Engineering group
	if eng, ok := groups["Engineering"]; ok {
		if count := eng["COUNT(n.)"]; count != 3 {
			t.Errorf("Expected Engineering count=3, got %v", count)
		}
		if avg := eng["AVG(n.salary)"]; avg != float64(60000) {
			t.Errorf("Expected Engineering avg=60000, got %v", avg)
		}
	} else {
		t.Error("Engineering department not found in results")
	}

	// Check Sales group
	if sales, ok := groups["Sales"]; ok {
		if count := sales["COUNT(n.)"]; count != 2 {
			t.Errorf("Expected Sales count=2, got %v", count)
		}
		if avg := sales["AVG(n.salary)"]; avg != float64(42500) {
			t.Errorf("Expected Sales avg=42500, got %v", avg)
		}
	} else {
		t.Error("Sales department not found in results")
	}
}

// TestExecutor_GroupBy_Multiple tests GROUP BY with multiple properties

func TestExecutor_GroupBy_Multiple(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with department and location
	nodes := []struct {
		dept     string
		location string
		salary   int64
	}{
		{"Engineering", "NYC", 100000},
		{"Engineering", "NYC", 110000},
		{"Engineering", "SF", 120000},
		{"Sales", "NYC", 80000},
		{"Sales", "SF", 85000},
	}

	for _, n := range nodes {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(n.dept),
			"location":   storage.StringValue(n.location),
			"salary":     storage.IntValue(n.salary),
		})
	}

	executor := NewExecutor(gs)

	// Query: GROUP BY department AND location
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Expression: &PropertyExpression{Variable: "n", Property: "location"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
				{Variable: "n", Property: "location"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute multi-GROUP BY query: %v", err)
	}

	// Should return 4 groups: Eng-NYC, Eng-SF, Sales-NYC, Sales-SF
	if result.Count != 4 {
		t.Errorf("Expected 4 grouped rows, got %d", result.Count)
	}

	// Verify Engineering-NYC group (2 people, avg 105000)
	found := false
	for _, row := range result.Rows {
		if row["n.department"] == "Engineering" && row["n.location"] == "NYC" {
			found = true
			if count := row["COUNT(n.)"]; count != 2 {
				t.Errorf("Expected Engineering-NYC count=2, got %v", count)
			}
			if avg := row["AVG(n.salary)"]; avg != float64(105000) {
				t.Errorf("Expected Engineering-NYC avg=105000, got %v", avg)
			}
		}
	}
	if !found {
		t.Error("Engineering-NYC group not found")
	}
}

// TestExecutor_GroupBy_WithOrderBy tests GROUP BY combined with ORDER BY

func TestExecutor_GroupBy_WithOrderBy(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes
	for _, dept := range []string{"Zebra", "Alpha", "Beta"} {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(dept),
			"salary":     storage.IntValue(50000),
		})
	}

	executor := NewExecutor(gs)

	// Query: GROUP BY department, ORDER BY department
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
			OrderBy: []*OrderByItem{
				{
					Expression: &PropertyExpression{Variable: "n", Property: "department"},
					Ascending:  true,
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY + ORDER BY query: %v", err)
	}

	if result.Count != 3 {
		t.Errorf("Expected 3 rows, got %d", result.Count)
	}

	// Verify alphabetical order
	if result.Rows[0]["n.department"] != "Alpha" {
		t.Errorf("Expected first department to be Alpha, got %v", result.Rows[0]["n.department"])
	}
	if result.Rows[1]["n.department"] != "Beta" {
		t.Errorf("Expected second department to be Beta, got %v", result.Rows[1]["n.department"])
	}
	if result.Rows[2]["n.department"] != "Zebra" {
		t.Errorf("Expected third department to be Zebra, got %v", result.Rows[2]["n.department"])
	}
}

// TestExecutor_GroupBy_EmptyGroup tests GROUP BY with no matching nodes

func TestExecutor_GroupBy_EmptyGroup(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Query on empty graph
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY on empty graph: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("Expected 0 rows for empty graph, got %d", result.Count)
	}
}

// TestExecutor_Aggregation_MixedTypes tests aggregations with mixed data types

func TestExecutor_GroupBy_LIMIT_SKIP(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple departments
	for i, dept := range []string{"A", "B", "C", "D", "E"} {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(dept),
			"salary":     storage.IntValue(int64(50000 + i*5000)),
		})
	}

	executor := NewExecutor(gs)

	// Query with GROUP BY, ORDER BY, SKIP, and LIMIT
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
			OrderBy: []*OrderByItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}, Ascending: true},
			},
		},
		Skip:  1,
		Limit: 3,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY with LIMIT/SKIP: %v", err)
	}

	// Should skip first group (A) and return 3 groups (B, C, D)
	if result.Count != 3 {
		t.Errorf("Expected 3 rows after SKIP 1 LIMIT 3, got %d", result.Count)
	}

	if result.Rows[0]["n.department"] != "B" {
		t.Errorf("Expected first department to be B, got %v", result.Rows[0]["n.department"])
	}
}

// TestExecutor_ComplexQuery tests multiple features combined

func TestExecutor_DISTINCT_With_Aggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create duplicate department data
	for i := 0; i < 3; i++ {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue("Engineering"),
		})
	}

	executor := NewExecutor(gs)

	// COUNT should return 3, but DISTINCT departments should be 1
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
			},
			Distinct: true,
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute DISTINCT: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 distinct department, got %d", result.Count)
	}
}

// TestExecutor_EmptyAggregation tests aggregation on empty result set

func TestExecutor_EmptyAggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Query empty graph
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
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "SUM", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute aggregation on empty set: %v", err)
	}

	// Should return one row with aggregate results for empty set
	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation on empty set, got %d", result.Count)
	}

	count := result.Rows[0]["COUNT(n.)"]
	if count != 0 {
		t.Errorf("Expected COUNT=0 on empty set, got %v", count)
	}

	sum := result.Rows[0]["SUM(n.salary)"]
	if sum != 0 {
		t.Errorf("Expected SUM=0 on empty set, got %v", sum)
	}
}

// TestExecutor_Integration_WHERE_GroupBy_Aggregation tests filtering + grouping + aggregation

func TestExecutor_ComplexQuery(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create diverse dataset
	testData := []struct {
		name   string
		dept   string
		age    int64
		salary int64
	}{
		{"Alice", "Engineering", 30, 80000},
		{"Bob", "Engineering", 25, 70000},
		{"Charlie", "Sales", 35, 60000},
		{"Diana", "Sales", 28, 65000},
		{"Eve", "Marketing", 32, 75000},
		{"Frank", "Engineering", 40, 90000},
	}

	for _, person := range testData {
		_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name":       storage.StringValue(person.name),
			"department": storage.StringValue(person.dept),
			"age":        storage.IntValue(person.age),
			"salary":     storage.IntValue(person.salary),
		})
	}

	executor := NewExecutor(gs)

	// Complex query: GROUP BY department, filter, aggregate, order, limit
	// "Find top 2 departments by average salary where avg salary > 65000"
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
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
			OrderBy: []*OrderByItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}, Ascending: true},
			},
		},
		Limit: 2,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute complex query: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Expected 2 rows (LIMIT 2), got %d", result.Count)
	}

	// Verify structure
	for _, row := range result.Rows {
		if _, ok := row["n.department"]; !ok {
			t.Error("Expected n.department in results")
		}
		if _, ok := row["COUNT(n.)"]; !ok {
			t.Error("Expected COUNT(n.) in results")
		}
		if _, ok := row["AVG(n.salary)"]; !ok {
			t.Error("Expected AVG(n.salary) in results")
		}
	}
}

// TestExecutor_Aggregation_StringMinMax tests MIN/MAX with strings

func TestExecutor_Integration_WHERE_GroupBy_Aggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data: employees with varying salaries
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(80000),
		"level":      storage.StringValue("Senior"),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(60000),
		"level":      storage.StringValue("Junior"),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie"),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(70000),
		"level":      storage.StringValue("Senior"),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Diana"),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(50000),
		"level":      storage.StringValue("Junior"),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Eve"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(90000),
		"level":      storage.StringValue("Senior"),
	})

	executor := NewExecutor(gs)

	// Query: Find average salary by department, but only for Senior employees, ordered by avg salary DESC
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "e",
							Labels:   []string{"Employee"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "e",
					Property: "level",
				},
				Operator: "=",
				Right: &LiteralExpression{
					Value: "Senior",
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "department",
					},
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "salary",
					},
					Alias: "avg_salary",
				},
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "name",
					},
					Alias: "employee_count",
				},
			},
			GroupBy: []*PropertyExpression{
				{
					Variable: "e",
					Property: "department",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	// Should have 2 departments (Engineering and Sales) with Senior employees
	if result.Count != 2 {
		t.Errorf("Expected 2 departments, got %d", result.Count)
	}

	// Verify Engineering department (2 Senior: Alice 80k, Eve 90k = avg 85k)
	engRow := findRowByColumn(result.Rows, "e.department", "Engineering")
	if engRow == nil {
		t.Fatal("Engineering department not found")
	}
	avgSalary := engRow["avg_salary"].(float64)
	if avgSalary != 85000.0 {
		t.Errorf("Engineering avg salary: expected 85000, got %v", avgSalary)
	}
	empCount := engRow["employee_count"].(int)
	if empCount != 2 {
		t.Errorf("Engineering employee count: expected 2, got %d", empCount)
	}

	// Verify Sales department (1 Senior: Charlie 70k)
	salesRow := findRowByColumn(result.Rows, "e.department", "Sales")
	if salesRow == nil {
		t.Fatal("Sales department not found")
	}
	avgSalary = salesRow["avg_salary"].(float64)
	if avgSalary != 70000.0 {
		t.Errorf("Sales avg salary: expected 70000, got %v", avgSalary)
	}
	empCount = salesRow["employee_count"].(int)
	if empCount != 1 {
		t.Errorf("Sales employee count: expected 1, got %d", empCount)
	}
}

// TestExecutor_Integration_MultipleAggregations tests multiple aggregates in one query

func TestExecutor_Integration_MultipleAggregations(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data
	_, _ = gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name":     storage.StringValue("Laptop"),
		"price":    storage.FloatValue(999.99),
		"quantity": storage.IntValue(10),
		"rating":   storage.FloatValue(4.5),
	})
	_, _ = gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name":     storage.StringValue("Mouse"),
		"price":    storage.FloatValue(29.99),
		"quantity": storage.IntValue(100),
		"rating":   storage.FloatValue(4.2),
	})
	_, _ = gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name":     storage.StringValue("Keyboard"),
		"price":    storage.FloatValue(79.99),
		"quantity": storage.IntValue(50),
		"rating":   storage.FloatValue(4.8),
	})

	executor := NewExecutor(gs)

	// Query with multiple aggregations
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "p",
							Labels:   []string{"Product"},
						},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "name",
					},
					Alias: "total_products",
				},
				{
					Aggregate: "SUM",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "quantity",
					},
					Alias: "total_quantity",
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "price",
					},
					Alias: "avg_price",
				},
				{
					Aggregate: "MIN",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "price",
					},
					Alias: "min_price",
				},
				{
					Aggregate: "MAX",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "price",
					},
					Alias: "max_price",
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "rating",
					},
					Alias: "avg_rating",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("Expected 1 result row, got %d", result.Count)
	}

	row := result.Rows[0]

	// Verify all aggregations
	if row["total_products"] != 3 {
		t.Errorf("total_products: expected 3, got %v", row["total_products"])
	}
	if row["total_quantity"] != int64(160) {
		t.Errorf("total_quantity: expected 160, got %v", row["total_quantity"])
	}
	expectedAvgPrice := (999.99 + 29.99 + 79.99) / 3.0
	if avgPrice := row["avg_price"].(float64); avgPrice != expectedAvgPrice {
		t.Errorf("avg_price: expected %v, got %v", expectedAvgPrice, avgPrice)
	}
	if row["min_price"] != 29.99 {
		t.Errorf("min_price: expected 29.99, got %v", row["min_price"])
	}
	if row["max_price"] != 999.99 {
		t.Errorf("max_price: expected 999.99, got %v", row["max_price"])
	}
	expectedAvgRating := (4.5 + 4.2 + 4.8) / 3.0
	if avgRating := row["avg_rating"].(float64); avgRating != expectedAvgRating {
		t.Errorf("avg_rating: expected %v, got %v", expectedAvgRating, avgRating)
	}
}

// TestExecutor_Integration_ComplexWHERE_Aggregation tests complex WHERE with AND/OR + aggregation

func TestExecutor_Integration_ComplexWHERE_Aggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"age":    storage.IntValue(30),
		"salary": storage.IntValue(80000),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Bob"),
		"age":    storage.IntValue(25),
		"salary": storage.IntValue(50000),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"age":    storage.IntValue(35),
		"salary": storage.IntValue(90000),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Diana"),
		"age":    storage.IntValue(28),
		"salary": storage.IntValue(75000),
	})
	_, _ = gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Eve"),
		"age":    storage.IntValue(40),
		"salary": storage.IntValue(95000),
	})

	executor := NewExecutor(gs)

	// Query: Find avg salary for employees where (age > 30) OR (salary >= 75000)
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "e",
							Labels:   []string{"Employee"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &BinaryExpression{
					Left: &PropertyExpression{
						Variable: "e",
						Property: "age",
					},
					Operator: ">",
					Right: &LiteralExpression{
						Value: int64(30),
					},
				},
				Operator: "OR",
				Right: &BinaryExpression{
					Left: &PropertyExpression{
						Variable: "e",
						Property: "salary",
					},
					Operator: ">=",
					Right: &LiteralExpression{
						Value: int64(75000),
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "name",
					},
					Alias: "count",
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "salary",
					},
					Alias: "avg_salary",
				},
				{
					Aggregate: "MIN",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "age",
					},
					Alias: "min_age",
				},
				{
					Aggregate: "MAX",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "age",
					},
					Alias: "max_age",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("Expected 1 result row, got %d", result.Count)
	}

	row := result.Rows[0]

	// Should match: Alice (age 30, salary 80000 ✓), Charlie (age 35 ✓), Diana (age 28, salary 75000 ✓), Eve (age 40 ✓)
	// Total: 4 employees
	if row["count"] != 4 {
		t.Errorf("count: expected 4, got %v", row["count"])
	}

	// Avg salary: (80000 + 90000 + 75000 + 95000) / 4 = 85000
	expectedAvg := float64(340000) / 4.0
	if avgSalary := row["avg_salary"].(float64); avgSalary != expectedAvg {
		t.Errorf("avg_salary: expected %v, got %v", expectedAvg, avgSalary)
	}

	if row["min_age"] != int64(28) {
		t.Errorf("min_age: expected 28, got %v", row["min_age"])
	}

	if row["max_age"] != int64(40) {
		t.Errorf("max_age: expected 40, got %v", row["max_age"])
	}
}

// TestExecutor_Integration_DISTINCT_WHERE_OrderBy tests DISTINCT with filtering and ordering

func TestExecutor_Integration_DISTINCT_WHERE_OrderBy(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data with duplicate cities
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"city": storage.StringValue("NYC"),
		"age":  storage.IntValue(30),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"city": storage.StringValue("LA"),
		"age":  storage.IntValue(25),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"city": storage.StringValue("NYC"),
		"age":  storage.IntValue(35),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Diana"),
		"city": storage.StringValue("Chicago"),
		"age":  storage.IntValue(28),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Eve"),
		"city": storage.StringValue("LA"),
		"age":  storage.IntValue(32),
	})
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Frank"),
		"city": storage.StringValue("Seattle"),
		"age":  storage.IntValue(22),
	})

	executor := NewExecutor(gs)

	// Query: Get distinct cities for people age >= 25, ordered by city name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "p",
							Labels:   []string{"Person"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "p",
					Property: "age",
				},
				Operator: ">=",
				Right: &LiteralExpression{
					Value: int64(25),
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "city",
					},
				},
			},
			Distinct: true,
			OrderBy: []*OrderByItem{
				{
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "city",
					},
					Ascending: true,
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	// Should have 3 distinct cities: Alice(30), Bob(25), Charlie(35), Diana(28), Eve(32) all >= 25
	// Frank (age 22) is excluded because 22 < 25
	// Cities: NYC, LA, NYC, Chicago, LA -> distinct: Chicago, LA, NYC (3 cities)
	if result.Count != 3 {
		t.Errorf("Expected 3 distinct cities, got %d", result.Count)
	}

	// Verify they're sorted alphabetically
	expectedCities := []string{"Chicago", "LA", "NYC"}
	for i, expected := range expectedCities {
		if i >= len(result.Rows) {
			t.Errorf("Missing city at index %d", i)
			continue
		}
		actual := result.Rows[i]["p.city"]
		if actual != expected {
			t.Errorf("Row %d: expected city %s, got %v", i, expected, actual)
		}
	}
}

// TestExecutor_Aggregation_COLLECT tests COLLECT aggregation
