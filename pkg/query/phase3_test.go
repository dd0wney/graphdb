package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupPhase3Graph creates a test graph with diverse data for Phase 3 tests.
// Graph topology: Alice->Bob->Charlie->Diana (KNOWS chain), Alice->Charlie (KNOWS shortcut)
// Eve is an isolated node with no relationships.
func setupPhase3Graph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
		"role": storage.StringValue("Engineer"),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
		"role": storage.StringValue("Designer"),
	})
	charlie, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
		"role": storage.StringValue("Manager"),
	})
	diana, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Diana"),
		"age":  storage.IntValue(28),
		"role": storage.StringValue("Engineer"),
	})
	// Eve has no "role" property — useful for IS NULL tests
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Eve"),
		"age":  storage.IntValue(22),
	})

	// Chain: Alice -> Bob -> Charlie -> Diana
	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)
	gs.CreateEdge(bob.ID, charlie.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2019),
	}, 1.0)
	gs.CreateEdge(charlie.ID, diana.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2021),
	}, 1.0)
	// Shortcut: Alice -> Charlie
	gs.CreateEdge(alice.ID, charlie.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2018),
	}, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

// --- Feature 1: IS NULL / IS NOT NULL ---

func TestPhase3_IsNull_MissingProperty(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// Eve has no "role" property — should match IS NULL
	result := parseAndExecute(t, executor, `MATCH (n:Person) WHERE n.role IS NULL RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 result (Eve), got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Eve" {
		t.Errorf("expected Eve, got %v", result.Rows[0]["n.name"])
	}
}

func TestPhase3_IsNotNull_ExistingProperty(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// 4 people have "role" — Alice, Bob, Charlie, Diana
	result := parseAndExecute(t, executor, `MATCH (n:Person) WHERE n.role IS NOT NULL RETURN n.name`)

	if result.Count != 4 {
		t.Fatalf("expected 4 results, got %d", result.Count)
	}
}

func TestPhase3_IsNull_OptionalMatch(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// Eve has no outgoing KNOWS edges — OPTIONAL MATCH should return null for friend
	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Eve'}) OPTIONAL MATCH (n)-[:KNOWS]->(friend) RETURN n.name, friend IS NULL AS noFriend`)

	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["noFriend"] != true {
		t.Errorf("expected noFriend=true, got %v", result.Rows[0]["noFriend"])
	}
}

// --- Feature 2: IN operator ---

func TestPhase3_In_StringList(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.role IN ['Engineer', 'Manager'] RETURN n.name`)

	if result.Count != 3 {
		t.Fatalf("expected 3 results (Alice, Charlie, Diana), got %d", result.Count)
	}
	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["n.name"].(string)] = true
	}
	for _, expected := range []string{"Alice", "Charlie", "Diana"} {
		if !names[expected] {
			t.Errorf("expected %s in results", expected)
		}
	}
}

func TestPhase3_In_IntegerList(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.age IN [25, 35] RETURN n.name`)

	if result.Count != 2 {
		t.Fatalf("expected 2 results (Bob, Charlie), got %d", result.Count)
	}
}

func TestPhase3_In_NoMatch(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name IN ['Zara', 'Xavier'] RETURN n.name`)

	if result.Count != 0 {
		t.Fatalf("expected 0 results, got %d", result.Count)
	}
}

// --- Feature 3: REMOVE clause ---

func TestPhase3_Remove_Property(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// Add temp property, then remove it
	parseAndExecute(t, executor, `MATCH (n:Person {name: 'Alice'}) SET n.temp = 'hello'`)

	// Verify it was set
	result := parseAndExecute(t, executor, `MATCH (n:Person {name: 'Alice'}) RETURN n.temp`)
	if result.Count != 1 || result.Rows[0]["n.temp"] != "hello" {
		t.Fatalf("expected temp='hello', got %v", result.Rows[0]["n.temp"])
	}

	// Remove it
	parseAndExecute(t, executor, `MATCH (n:Person {name: 'Alice'}) REMOVE n.temp`)

	// Verify removed — property should be null
	result = parseAndExecute(t, executor, `MATCH (n:Person {name: 'Alice'}) RETURN n.temp`)
	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.temp"] != nil {
		t.Errorf("expected n.temp to be nil after REMOVE, got %v", result.Rows[0]["n.temp"])
	}
}

func TestPhase3_Remove_MultipleProperties(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	parseAndExecute(t, executor, `MATCH (n:Person {name: 'Bob'}) SET n.x = 'a'`)
	parseAndExecute(t, executor, `MATCH (n:Person {name: 'Bob'}) SET n.y = 'b'`)
	parseAndExecute(t, executor, `MATCH (n:Person {name: 'Bob'}) REMOVE n.x, n.y`)

	result := parseAndExecute(t, executor, `MATCH (n:Person {name: 'Bob'}) RETURN n.x, n.y`)
	if result.Rows[0]["n.x"] != nil || result.Rows[0]["n.y"] != nil {
		t.Errorf("expected both properties removed, got x=%v y=%v", result.Rows[0]["n.x"], result.Rows[0]["n.y"])
	}
}

// --- Feature 4: toBoolean function ---

func TestPhase3_ToBoolean_Strings(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	tests := []struct {
		query    string
		expected any
	}{
		{`RETURN toBoolean('true') AS val`, true},
		{`RETURN toBoolean('false') AS val`, false},
		{`RETURN toBoolean(true) AS val`, true},
		{`RETURN toBoolean(false) AS val`, false},
	}

	for _, tt := range tests {
		result := parseAndExecute(t, executor, tt.query)
		if result.Count != 1 {
			t.Fatalf("query %q: expected 1 row, got %d", tt.query, result.Count)
		}
		if result.Rows[0]["val"] != tt.expected {
			t.Errorf("query %q: expected %v, got %v", tt.query, tt.expected, result.Rows[0]["val"])
		}
	}
}

func TestPhase3_ToBoolean_Nil(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	result := parseAndExecute(t, executor, `RETURN toBoolean(null) AS val`)
	if result.Rows[0]["val"] != nil {
		t.Errorf("expected nil for toBoolean(null), got %v", result.Rows[0]["val"])
	}
}

// --- Feature 5: Variable-length path execution ---

func TestPhase3_VarLengthPath_ExactHops(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// Alice -[:KNOWS*2]-> should reach Charlie (via Bob) and Diana (via Bob->Charlie)
	// But *2 means exactly 2 hops: Alice->Bob->Charlie
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[:KNOWS*2]->(b) RETURN b.name`)

	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["b.name"].(string)] = true
	}
	// 2 hops from Alice: Alice->Bob->Charlie, Alice->Charlie->Diana
	if !names["Charlie"] {
		t.Error("expected Charlie at 2 hops from Alice (via Bob)")
	}
	if !names["Diana"] {
		t.Error("expected Diana at 2 hops from Alice (via Charlie)")
	}
}

func TestPhase3_VarLengthPath_Range(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// *1..3 from Alice: all reachable within 3 hops
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[:KNOWS*1..3]->(b) RETURN DISTINCT b.name`)

	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["b.name"].(string)] = true
	}
	// 1 hop: Bob, Charlie (direct edges)
	// 2 hops: Charlie (via Bob), Diana (via Charlie)
	// 3 hops: Diana (via Bob->Charlie->Diana)
	for _, expected := range []string{"Bob", "Charlie", "Diana"} {
		if !names[expected] {
			t.Errorf("expected %s reachable within 3 hops from Alice", expected)
		}
	}
}

func TestPhase3_VarLengthPath_NoMatch(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// Eve has no outgoing KNOWS edges
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Eve'})-[:KNOWS*1..3]->(b) RETURN b.name`)

	if result.Count != 0 {
		t.Errorf("expected 0 results for Eve's KNOWS paths, got %d", result.Count)
	}
}

func TestPhase3_VarLengthPath_WithRelVar(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// Bind the relationship variable — should get path of edges
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[r:KNOWS*1..2]->(b) RETURN a.name, b.name`)

	// Should find: Alice->Bob (1 hop), Alice->Charlie (1 hop),
	//              Alice->Bob->Charlie (2 hops), Alice->Charlie->Diana (2 hops)
	if result.Count < 2 {
		t.Errorf("expected at least 2 results, got %d", result.Count)
	}
}

func TestPhase3_VarLengthPath_UnboundedMax(t *testing.T) {
	_, executor, cleanup := setupPhase3Graph(t)
	defer cleanup()

	// *2.. means MinHops=2 with unbounded max (capped by safety limit)
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[:KNOWS*2..]->(b) RETURN b.name`)

	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["b.name"].(string)] = true
	}
	// 2+ hops from Alice: Charlie (via Bob), Diana (via Bob->Charlie or Charlie)
	if !names["Diana"] {
		t.Error("expected Diana reachable at 2+ hops from Alice")
	}
}
