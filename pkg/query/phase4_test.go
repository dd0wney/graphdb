package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupPhase4Graph creates a test graph for Phase 4 (expression operators).
// Nodes: Alice(30, Engineer, 80000), Bob(25, Designer, 60000),
//
//	Charlie(35, Manager, 90000), Diana(28, Engineer, 70000)
//
// Edges: Alice-[:KNOWS]->Bob, Alice-[:KNOWS]->Charlie
func setupPhase4Graph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice"),
		"age":        storage.IntValue(30),
		"role":       storage.StringValue("Engineer"),
		"salary":     storage.IntValue(80000),
		"first_name": storage.StringValue("Alice"),
		"last_name":  storage.StringValue("Smith"),
		"active":     storage.BoolValue(true),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob"),
		"age":        storage.IntValue(25),
		"role":       storage.StringValue("Designer"),
		"salary":     storage.IntValue(60000),
		"first_name": storage.StringValue("Bob"),
		"last_name":  storage.StringValue("Jones"),
		"active":     storage.BoolValue(false),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie"),
		"age":        storage.IntValue(35),
		"role":       storage.StringValue("Manager"),
		"salary":     storage.IntValue(90000),
		"first_name": storage.StringValue("Charlie"),
		"last_name":  storage.StringValue("Brown"),
		"active":     storage.BoolValue(true),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Diana"),
		"age":        storage.IntValue(28),
		"role":       storage.StringValue("Engineer"),
		"salary":     storage.IntValue(70000),
		"first_name": storage.StringValue("Diana"),
		"last_name":  storage.StringValue("Prince"),
		"active":     storage.BoolValue(true),
	})

	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

// --- Feature 1: Arithmetic in WHERE ---

func TestPhase4_Arithmetic_AddInWhere(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// n.age + 5 > 32 → Alice(35), Charlie(40), Diana(33) pass; Bob(30) fails
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.age + 5 > 32 RETURN n.name`)

	names := collectNames(t, result, "n.name")
	assertContains(t, names, "Alice")
	assertContains(t, names, "Charlie")
	assertContains(t, names, "Diana")
	assertNotContains(t, names, "Bob")
}

func TestPhase4_Arithmetic_MulInWhere(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// salary * 1.1 > 75000 → Alice(88000), Charlie(99000), Diana(77000)
	// Bob(66000) fails
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.salary * 1.1 > 75000 RETURN n.name`)

	names := collectNames(t, result, "n.name")
	assertContains(t, names, "Alice")
	assertContains(t, names, "Charlie")
	assertContains(t, names, "Diana")
	assertNotContains(t, names, "Bob")
}

func TestPhase4_Arithmetic_Precedence(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// 2 + 3 * 4 = 14 (not 20) — tests mul-before-add precedence
	result := parseAndExecute(t, executor,
		`RETURN 2 + 3 * 4 AS val`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	// 2 + (3*4) = 14
	val := result.Rows[0]["val"]
	if val != int64(14) {
		t.Errorf("expected 14, got %v (%T)", val, val)
	}
}

func TestPhase4_Arithmetic_Modulo(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// n.age % 2 = 0 → even ages: Alice(30), Diana(28)
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.age % 2 = 0 RETURN n.name`)

	names := collectNames(t, result, "n.name")
	assertContains(t, names, "Alice")
	assertContains(t, names, "Diana")
	// Bob(25) and Charlie(35) are odd
	assertNotContains(t, names, "Bob")
	assertNotContains(t, names, "Charlie")
}

// --- Feature 1: Arithmetic in RETURN ---

func TestPhase4_Arithmetic_InReturn(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Alice'}) RETURN n.age + 5 AS agePlus5`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["agePlus5"] != int64(35) {
		t.Errorf("expected 35, got %v", result.Rows[0]["agePlus5"])
	}
}

func TestPhase4_Arithmetic_SubtractInReturn(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Charlie'}) RETURN n.salary - 10000 AS reduced`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["reduced"] != int64(80000) {
		t.Errorf("expected 80000, got %v", result.Rows[0]["reduced"])
	}
}

func TestPhase4_Arithmetic_DivisionInReturn(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Alice'}) RETURN n.salary / 1000 AS salaryK`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["salaryK"] != int64(80) {
		t.Errorf("expected 80, got %v", result.Rows[0]["salaryK"])
	}
}

// --- Feature 2: String concatenation ---

func TestPhase4_StringConcat(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Alice'}) RETURN n.first_name + ' ' + n.last_name AS fullName`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["fullName"] != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %v", result.Rows[0]["fullName"])
	}
}

// --- Feature 3: Unary NOT ---

func TestPhase4_Not_SimpleProperty(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// NOT n.active → Bob (active=false)
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE NOT n.active RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Bob" {
		t.Errorf("expected Bob, got %v", result.Rows[0]["n.name"])
	}
}

func TestPhase4_Not_WithComparison(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// NOT (n.age > 30) → Bob(25), Alice(30), Diana(28)
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE NOT (n.age > 30) RETURN n.name`)

	names := collectNames(t, result, "n.name")
	assertContains(t, names, "Bob")
	assertContains(t, names, "Alice")
	assertContains(t, names, "Diana")
	assertNotContains(t, names, "Charlie")
}

func TestPhase4_Not_AndComposition(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// NOT n.active AND n.age < 30 → Bob (active=false AND age=25)
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE NOT n.active AND n.age < 30 RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Bob" {
		t.Errorf("expected Bob, got %v", result.Rows[0]["n.name"])
	}
}

// --- Feature 4: NOT IN ---

func TestPhase4_NotIn_StringList(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// NOT IN ['Engineer', 'Manager'] → Bob(Designer) and Diana(Engineer excluded)
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.role NOT IN ['Engineer', 'Manager'] RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Bob" {
		t.Errorf("expected Bob, got %v", result.Rows[0]["n.name"])
	}
}

func TestPhase4_NotIn_IntList(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.age NOT IN [25, 35] RETURN n.name`)

	names := collectNames(t, result, "n.name")
	assertContains(t, names, "Alice")  // 30
	assertContains(t, names, "Diana")  // 28
	assertNotContains(t, names, "Bob") // 25 excluded
}

// --- Feature 5: Unary minus ---

func TestPhase4_UnaryMinus_InReturn(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Alice'}) RETURN -n.age AS negAge`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["negAge"] != int64(-30) {
		t.Errorf("expected -30, got %v", result.Rows[0]["negAge"])
	}
}

func TestPhase4_UnaryMinus_InWhere(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// All ages are > -100, so should return all
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.age > -100 RETURN n.name`)

	if result.Count != 4 {
		t.Fatalf("expected 4 results, got %d", result.Count)
	}
}

func TestPhase4_UnaryMinus_Literal(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor, `RETURN -42 AS val`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["val"] != int64(-42) {
		t.Errorf("expected -42, got %v (%T)", result.Rows[0]["val"], result.Rows[0]["val"])
	}
}

// --- Precedence / Composition ---

func TestPhase4_Precedence_ParensOverrideMul(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// (2 + 3) * 4 = 20
	result := parseAndExecute(t, executor, `RETURN (2 + 3) * 4 AS val`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["val"] != int64(20) {
		t.Errorf("expected 20, got %v", result.Rows[0]["val"])
	}
}

func TestPhase4_Arithmetic_LeftAssociative(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// 10 - 3 - 2 = 5 (left-associative: (10-3)-2, not 10-(3-2)=9)
	result := parseAndExecute(t, executor, `RETURN 10 - 3 - 2 AS val`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	if result.Rows[0]["val"] != int64(5) {
		t.Errorf("expected 5, got %v", result.Rows[0]["val"])
	}
}

func TestPhase4_Arithmetic_ComplexExpression(t *testing.T) {
	_, executor, cleanup := setupPhase4Graph(t)
	defer cleanup()

	// Salary with 10% raise minus 5000 bonus threshold
	// Alice: 80000 * 1.1 - 5000 = 83000
	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Alice'}) RETURN n.salary * 1.1 - 5000 AS adjusted`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}
	val, ok := result.Rows[0]["adjusted"].(float64)
	if !ok {
		t.Fatalf("expected float64, got %T(%v)", result.Rows[0]["adjusted"], result.Rows[0]["adjusted"])
	}
	if val < 82999 || val > 83001 {
		t.Errorf("expected ~83000, got %f", val)
	}
}

// --- Helpers ---

func collectNames(t *testing.T, result *ResultSet, column string) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	for _, row := range result.Rows {
		if name, ok := row[column].(string); ok {
			names[name] = true
		}
	}
	return names
}

func assertContains(t *testing.T, set map[string]bool, key string) {
	t.Helper()
	if !set[key] {
		t.Errorf("expected %q in results, got %v", key, set)
	}
}

func assertNotContains(t *testing.T, set map[string]bool, key string) {
	t.Helper()
	if set[key] {
		t.Errorf("did NOT expect %q in results, got %v", key, set)
	}
}
