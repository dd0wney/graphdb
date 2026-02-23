package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupPhase5Graph creates a test graph for Phase 5 (query language gap closure).
// Nodes: Alice(30, Engineer), Bob(25, Designer), Charlie(35, Manager)
// Edges: Alice-[:KNOWS {since:2020}]->Bob, Alice-[:WORKS_WITH {since:2019, project:"Alpha"}]->Charlie
func setupPhase5Graph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"age":    storage.IntValue(30),
		"role":   storage.StringValue("Engineer"),
		"salary": storage.IntValue(80000),
		"active": storage.BoolValue(true),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Bob"),
		"age":    storage.IntValue(25),
		"role":   storage.StringValue("Designer"),
		"salary": storage.IntValue(60000),
		"active": storage.BoolValue(false),
	})
	charlie, _ := gs.CreateNode([]string{"Person", "Manager"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"age":    storage.IntValue(35),
		"role":   storage.StringValue("Manager"),
		"salary": storage.IntValue(90000),
		"active": storage.BoolValue(true),
	})

	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)
	gs.CreateEdge(alice.ID, charlie.ID, "WORKS_WITH", map[string]storage.Value{
		"since":   storage.IntValue(2019),
		"project": storage.StringValue("Alpha"),
	}, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

// --- Feature 1: ORDER BY Parsing ---

func TestPhase5_OrderBy_SingleAsc(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name ORDER BY n.age`)

	if len(result.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(result.Rows))
	}
	// Ascending by age: Bob(25), Alice(30), Charlie(35)
	expectOrder(t, result, "n.name", []string{"Bob", "Alice", "Charlie"})
}

func TestPhase5_OrderBy_SingleDesc(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name, n.age ORDER BY n.age DESC`)

	expectOrder(t, result, "n.name", []string{"Charlie", "Alice", "Bob"})
}

func TestPhase5_OrderBy_MultiColumn(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	// All have different roles, so secondary sort on name won't matter,
	// but this tests the parser accepts multiple ORDER BY columns
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name ORDER BY n.role, n.name`)

	if len(result.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(result.Rows))
	}
}

func TestPhase5_OrderBy_WithLimit(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name ORDER BY n.age DESC LIMIT 2`)

	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	expectOrder(t, result, "n.name", []string{"Charlie", "Alice"})
}

func TestPhase5_OrderBy_WithAlias(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name, n.age AS age ORDER BY age`)

	expectOrder(t, result, "name", []string{"Bob", "Alice", "Charlie"})
}

// --- Feature 2: Edge Property Access ---

func TestPhase5_EdgeProperty_Return(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN a.name, r.since, b.name`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	since := result.Rows[0]["r.since"]
	if since != int64(2020) {
		t.Errorf("expected r.since=2020, got %v (%T)", since, since)
	}
}

func TestPhase5_EdgeProperty_Where(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r]->(b:Person) WHERE r.since > 2019 RETURN b.name`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["b.name"] != "Bob" {
		t.Errorf("expected Bob, got %v", result.Rows[0]["b.name"])
	}
}

func TestPhase5_EdgeProperty_String(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r:WORKS_WITH]->(b:Person) RETURN r.project`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["r.project"] != "Alpha" {
		t.Errorf("expected Alpha, got %v", result.Rows[0]["r.project"])
	}
}

// --- Feature 3: SET with Expressions ---

func TestPhase5_SetExpression_Increment(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' SET n.age = n.age + 1`)

	// Verify the update
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' RETURN n.age`)

	if result.Rows[0]["n.age"] != int64(31) {
		t.Errorf("expected age=31 after increment, got %v", result.Rows[0]["n.age"])
	}
}

func TestPhase5_SetExpression_Multiply(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Bob' SET n.salary = n.salary * 2`)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Bob' RETURN n.salary`)

	if result.Rows[0]["n.salary"] != int64(120000) {
		t.Errorf("expected salary=120000, got %v", result.Rows[0]["n.salary"])
	}
}

func TestPhase5_SetExpression_MultipleAssignments(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' SET n.age = n.age + 1, n.salary = 85000`)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' RETURN n.age, n.salary`)

	if result.Rows[0]["n.age"] != int64(31) {
		t.Errorf("expected age=31, got %v", result.Rows[0]["n.age"])
	}
	if result.Rows[0]["n.salary"] != int64(85000) {
		t.Errorf("expected salary=85000, got %v", result.Rows[0]["n.salary"])
	}
}

// --- Feature 4: Type/Schema Functions ---

func TestPhase5_TypeFunction(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN type(r)`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["type(...)"] != "KNOWS" {
		t.Errorf("expected KNOWS, got %v", result.Rows[0]["type(...)"])
	}
}

func TestPhase5_LabelsFunction(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Charlie' RETURN labels(n)`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	labels, ok := result.Rows[0]["labels(...)"].([]any)
	if !ok {
		t.Fatalf("expected []any for labels, got %T", result.Rows[0]["labels(...)"])
	}
	// Charlie has both Person and Manager labels
	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d: %v", len(labels), labels)
	}
}

func TestPhase5_IdFunction(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' RETURN id(n)`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	id, ok := result.Rows[0]["id(...)"].(int64)
	if !ok {
		t.Fatalf("expected int64, got %T: %v", result.Rows[0]["id(...)"], result.Rows[0]["id(...)"])
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

func TestPhase5_KeysFunction(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' RETURN keys(n)`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	keys, ok := result.Rows[0]["keys(...)"].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result.Rows[0]["keys(...)"])
	}
	// Alice has: name, age, role, salary, active
	if len(keys) != 5 {
		t.Errorf("expected 5 keys, got %d: %v", len(keys), keys)
	}
}

func TestPhase5_PropertiesFunction(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Bob' RETURN properties(n)`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	props, ok := result.Rows[0]["properties(...)"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result.Rows[0]["properties(...)"])
	}
	if props["name"] != "Bob" {
		t.Errorf("expected name=Bob, got %v", props["name"])
	}
}

func TestPhase5_IdFunction_Edge(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN id(r)`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	id, ok := result.Rows[0]["id(...)"].(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", result.Rows[0]["id(...)"])
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

// --- Feature 5: String Predicate Operators ---

func TestPhase5_StartsWith(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name STARTS WITH 'Al' RETURN n.name`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("expected Alice, got %v", result.Rows[0]["n.name"])
	}
}

func TestPhase5_EndsWith(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name ENDS WITH 'ob' RETURN n.name`)

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["n.name"] != "Bob" {
		t.Errorf("expected Bob, got %v", result.Rows[0]["n.name"])
	}
}

func TestPhase5_Contains(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name CONTAINS 'li' RETURN n.name`)

	names := collectNames(t, result, "n.name")
	assertContains(t, names, "Alice")
	assertContains(t, names, "Charlie")
	assertNotContains(t, names, "Bob")
}

func TestPhase5_StartsWithNullPropagation(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	// Non-existent property should return no results (null propagation)
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.missing STARTS WITH 'x' RETURN n.name`)

	if len(result.Rows) != 0 {
		t.Errorf("expected 0 rows for null property, got %d", len(result.Rows))
	}
}

// --- Test helpers ---

func expectOrder(t *testing.T, result *ResultSet, column string, expected []string) {
	t.Helper()
	if len(result.Rows) != len(expected) {
		t.Fatalf("expected %d rows, got %d", len(expected), len(result.Rows))
	}
	for i, want := range expected {
		got, ok := result.Rows[i][column].(string)
		if !ok {
			t.Errorf("row %d: expected string for column %s, got %T", i, column, result.Rows[i][column])
			continue
		}
		if got != want {
			t.Errorf("row %d: expected %q, got %q", i, want, got)
		}
	}
}
