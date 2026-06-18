package query

import (
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupConformanceGraph creates a shared test graph for conformance tests
func setupConformanceGraph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	// Create a diverse dataset
	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice"),
		"age":        storage.IntValue(30),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(80000),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob"),
		"age":        storage.IntValue(25),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(60000),
	})
	charlie, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie"),
		"age":        storage.IntValue(35),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(90000),
	})

	_, _ = gs.CreateEdge(alice.ID, bob.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)
	_, _ = gs.CreateEdge(alice.ID, charlie.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2019),
	}, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

// parseAndExecute is a helper that parses and executes a query string
func parseAndExecute(t *testing.T, executor *Executor, queryText string) *ResultSet {
	t.Helper()

	lexer := NewLexer(queryText)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed for %q: %v", queryText, err)
	}

	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed for %q: %v", queryText, err)
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Execute failed for %q: %v", queryText, err)
	}

	return result
}

func TestConformance_Explain(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor, `EXPLAIN MATCH (n:Person) WHERE n.age > 25 RETURN n.name`)

	if len(result.Columns) != 2 || result.Columns[0] != "step" {
		t.Errorf("EXPLAIN should return step/detail columns, got %v", result.Columns)
	}
	if len(result.Rows) < 3 {
		t.Errorf("Expected at least 3 plan steps, got %d", len(result.Rows))
	}
}

func TestConformance_Profile(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor, `PROFILE MATCH (n:Person) RETURN n.name`)

	if result.Profile == nil {
		t.Fatal("PROFILE should populate Profile field")
	}
	if len(result.Profile) < 2 {
		t.Errorf("Expected at least 2 profile entries, got %d", len(result.Profile))
	}
	if result.Count == 0 {
		t.Error("PROFILE should also return actual results")
	}
}

func TestConformance_Collect(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN COLLECT(n.name) AS names`)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result row, got %d", result.Count)
	}

	names, ok := result.Rows[0]["names"].([]any)
	if !ok {
		t.Fatalf("Expected []any for COLLECT, got %T", result.Rows[0]["names"])
	}
	if len(names) != 3 {
		t.Errorf("Expected 3 names, got %d", len(names))
	}
}

func TestConformance_CollectGroupBy(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.department, COLLECT(n.name) AS names GROUP BY n.department`)

	if result.Count != 2 {
		t.Fatalf("Expected 2 groups, got %d", result.Count)
	}

	for _, row := range result.Rows {
		names, ok := row["names"].([]any)
		if !ok {
			t.Fatalf("Expected []any for names, got %T", row["names"])
		}
		dept := row["n.department"]
		switch dept {
		case "Engineering":
			if len(names) != 2 {
				t.Errorf("Engineering: expected 2 names, got %d", len(names))
			}
		case "Sales":
			if len(names) != 1 {
				t.Errorf("Sales: expected 1 name, got %d", len(names))
			}
		}
	}
}

func TestConformance_MergeCreate(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// MERGE should create the node
	parseAndExecute(t, executor,
		`MERGE (n:Person {name: "Diana"})`)

	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node after MERGE create, got %d", stats.NodeCount)
	}

	// Second MERGE should not create a duplicate
	parseAndExecute(t, executor,
		`MERGE (n:Person {name: "Diana"})`)

	stats = gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node after second MERGE (no duplicate), got %d", stats.NodeCount)
	}
}

func TestConformance_With(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WITH n AS person RETURN person.name`)

	if result.Count != 3 {
		t.Errorf("Expected 3 results from WITH pass-through, got %d", result.Count)
	}
}

func TestConformance_WithFilter(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WITH n.name AS name, n.age AS age WHERE age > 28 RETURN name`)

	// Alice (30) and Charlie (35)
	if result.Count != 2 {
		t.Errorf("Expected 2 results after WITH filter, got %d", result.Count)
	}
}

func TestConformance_CombinedFeatures(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Test COLLECT + GROUP BY parsed from text
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.department, COUNT(n) AS cnt, AVG(n.salary) AS avg_sal GROUP BY n.department`)

	if result.Count != 2 {
		t.Errorf("Expected 2 department groups, got %d", result.Count)
	}

	// Verify Engineering group
	for _, row := range result.Rows {
		if row["n.department"] == "Engineering" {
			cnt, _ := row["cnt"].(int)
			if cnt != 2 {
				t.Errorf("Engineering count: expected 2, got %v", row["cnt"])
			}
		}
	}
}

func TestConformance_ExplainParsed(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// EXPLAIN from parsed text
	result := parseAndExecute(t, executor,
		`EXPLAIN MATCH (n:Person) WHERE n.age > 25 RETURN n.name LIMIT 5`)

	if result.Columns[0] != "step" {
		t.Errorf("Expected 'step' column, got %q", result.Columns[0])
	}

	// Should have MatchStep, FilterStep, ReturnStep
	stepNames := make([]string, 0)
	for _, row := range result.Rows {
		if s, ok := row["step"].(string); ok {
			stepNames = append(stepNames, s)
		}
	}

	found := strings.Join(stepNames, ",")
	if !strings.Contains(found, "MatchStep") || !strings.Contains(found, "FilterStep") || !strings.Contains(found, "ReturnStep") {
		t.Errorf("Expected MatchStep, FilterStep, ReturnStep in plan, got: %s", found)
	}
}

// parseAndExecuteWithParams is a helper that parses and executes a parameterized query
func parseAndExecuteWithParams(t *testing.T, executor *Executor, queryText string, params map[string]any) *ResultSet {
	t.Helper()

	lexer := NewLexer(queryText)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed for %q: %v", queryText, err)
	}

	parser := NewParser(tokens)
	query, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse failed for %q: %v", queryText, err)
	}

	result, err := executor.ExecuteWithParams(query, params)
	if err != nil {
		t.Fatalf("ExecuteWithParams failed for %q: %v", queryText, err)
	}

	return result
}

// --- Phase 2A Conformance Tests ---

func TestConformance_ParameterizedQuery(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecuteWithParams(t, executor,
		`MATCH (n:Person {name: $name}) RETURN n.age`,
		map[string]any{"name": "Alice"},
	)

	if result.Count != 1 {
		t.Fatalf("Expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.age"] != int64(30) {
		t.Errorf("Expected age 30, got %v", result.Rows[0]["n.age"])
	}
}

func TestConformance_CaseInReturn(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name, CASE WHEN n.age > 30 THEN "senior" ELSE "junior" END AS tier`)

	if result.Count != 3 {
		t.Fatalf("Expected 3 results, got %d", result.Count)
	}

	for _, row := range result.Rows {
		name := row["n.name"]
		tier := row["tier"]
		switch name {
		case "Alice":
			if tier != "junior" {
				t.Errorf("Alice (age 30): tier = %v, want junior", tier)
			}
		case "Charlie":
			if tier != "senior" {
				t.Errorf("Charlie (age 35): tier = %v, want senior", tier)
			}
		case "Bob":
			if tier != "junior" {
				t.Errorf("Bob (age 25): tier = %v, want junior", tier)
			}
		}
	}
}

func TestConformance_OptionalMatch(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Alice -> Bob (KNOWS), Alice -> Charlie (KNOWS)
	// Bob and Charlie have no outgoing KNOWS edges
	result := parseAndExecute(t, executor,
		`MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name`)

	// Alice has 2 friends, Bob has 0, Charlie has 0
	// Expected: Alice->Bob, Alice->Charlie, Bob->nil, Charlie->nil = 4 rows
	if result.Count != 4 {
		t.Fatalf("Expected 4 rows, got %d", result.Count)
	}

	nullCount := 0
	for _, row := range result.Rows {
		if row["b.name"] == nil {
			nullCount++
		}
	}
	if nullCount != 2 {
		t.Errorf("Expected 2 null b.name rows, got %d", nullCount)
	}
}

func TestConformance_Union(t *testing.T) {
	gs, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	_, _ = gs.CreateNode([]string{"Article"}, map[string]storage.Value{
		"title": storage.StringValue("Graph Databases"),
	})

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name AS name UNION MATCH (n:Article) RETURN n.title AS name`)

	// 3 persons + 1 article = 4 unique names
	if result.Count != 4 {
		t.Errorf("Expected 4 rows, got %d", result.Count)
	}
}

func TestConformance_CombinedPhase2A(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Parameterized + OPTIONAL MATCH + CASE
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (a:Person {name: $name}) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.name, CASE WHEN b.name = "Bob" THEN "friend" ELSE "unknown" END AS relation`,
		map[string]any{"name": "Alice"},
	)

	// Alice knows Bob and Charlie
	if result.Count != 2 {
		t.Fatalf("Expected 2 rows, got %d", result.Count)
	}

	for _, row := range result.Rows {
		bName := row["b.name"]
		rel := row["relation"]
		if bName == "Bob" && rel != "friend" {
			t.Errorf("Bob relation = %v, want friend", rel)
		}
		if bName == "Charlie" && rel != "unknown" {
			t.Errorf("Charlie relation = %v, want unknown", rel)
		}
	}
}

func TestConformance_MergeOnCreateSet(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	parseAndExecute(t, executor,
		`MERGE (n:Person {name: "Eve"}) ON CREATE SET n.status = "new"`)

	// Verify the node was created with the ON CREATE SET property
	nodes, _ := gs.FindNodesByLabelAcrossTenants("Person")
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	if statusVal, ok := nodes[0].Properties["status"]; ok {
		status, _ := statusVal.AsString()
		if status != "new" {
			t.Errorf("Expected status 'new', got %q", status)
		}
	} else {
		t.Error("Expected 'status' property from ON CREATE SET")
	}
}

// --- Phase 3: Core Operators + Variable-Length Paths Conformance Tests ---

// setupPhase3ConformanceGraph creates a graph for combined Phase 3 conformance scenarios.
// Topology: Alice->Bob->Charlie->Diana (KNOWS chain), Alice->Charlie (shortcut).
// Eve is isolated with no edges and missing "role" property.
func setupPhase3ConformanceGraph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
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
	// Eve: no role, no edges
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Eve"),
		"age":  storage.IntValue(22),
	})

	_, _ = gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)
	_, _ = gs.CreateEdge(bob.ID, charlie.ID, "KNOWS", nil, 1.0)
	_, _ = gs.CreateEdge(charlie.ID, diana.ID, "KNOWS", nil, 1.0)
	_, _ = gs.CreateEdge(alice.ID, charlie.ID, "KNOWS", nil, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

// Tests IS NULL with OPTIONAL MATCH on variable-length paths
func TestConformance_Phase3_IsNull_OptionalVarPath(t *testing.T) {
	_, executor, cleanup := setupPhase3ConformanceGraph(t)
	defer cleanup()

	// OPTIONAL MATCH a variable-length path from each person.
	// Eve has no outgoing KNOWS, so friend should be null.
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) OPTIONAL MATCH (n)-[:KNOWS*1..2]->(friend:Person) RETURN n.name, friend IS NULL AS isolated`)

	isolatedNames := make(map[string]bool)
	for _, row := range result.Rows {
		if row["isolated"] == true {
			isolatedNames[row["n.name"].(string)] = true
		}
	}

	// Eve has no outgoing edges at all; Diana has no outgoing KNOWS edges
	if !isolatedNames["Eve"] {
		t.Error("expected Eve to be isolated (no outgoing KNOWS paths)")
	}
}

// Tests IN combined with CASE expression
func TestConformance_Phase3_InWithCase(t *testing.T) {
	_, executor, cleanup := setupPhase3ConformanceGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.role IN ['Engineer', 'Manager']
		 RETURN n.name, CASE WHEN n.age > 30 THEN 'senior' ELSE 'junior' END AS level`)

	if result.Count != 3 {
		t.Fatalf("expected 3 results (Alice, Charlie, Diana), got %d", result.Count)
	}

	levels := make(map[string]string)
	for _, row := range result.Rows {
		levels[row["n.name"].(string)] = row["level"].(string)
	}

	// Charlie is 35 (> 30) → senior; Alice is 30 (not > 30) → junior
	if levels["Charlie"] != "senior" {
		t.Errorf("expected Charlie=senior, got %s", levels["Charlie"])
	}
	if levels["Alice"] != "junior" {
		t.Errorf("expected Alice=junior, got %s", levels["Alice"])
	}
}

// Tests REMOVE followed by IS NULL verification
func TestConformance_Phase3_RemoveThenIsNull(t *testing.T) {
	_, executor, cleanup := setupPhase3ConformanceGraph(t)
	defer cleanup()

	// Remove Bob's role, then verify it's null
	parseAndExecute(t, executor, `MATCH (n:Person {name: 'Bob'}) REMOVE n.role`)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.role IS NULL RETURN n.name`)

	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["n.name"].(string)] = true
	}

	// Eve never had role; Bob had it removed
	if !names["Eve"] || !names["Bob"] {
		t.Errorf("expected Eve and Bob to have null role, got %v", names)
	}
	if len(names) != 2 {
		t.Errorf("expected exactly 2 null-role persons, got %d", len(names))
	}
}

// Tests variable-length path with IN filter on target node
func TestConformance_Phase3_VarPathWithInFilter(t *testing.T) {
	_, executor, cleanup := setupPhase3ConformanceGraph(t)
	defer cleanup()

	// Find people reachable from Alice in 1-3 hops who are Engineers
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[:KNOWS*1..3]->(b:Person)
		 WHERE b.role IN ['Engineer', 'Designer']
		 RETURN DISTINCT b.name`)

	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["b.name"].(string)] = true
	}

	// Bob (Designer, 1 hop), Diana (Engineer, 2+ hops)
	if !names["Bob"] {
		t.Error("expected Bob reachable from Alice with role in [Engineer, Designer]")
	}
	if !names["Diana"] {
		t.Error("expected Diana reachable from Alice with role in [Engineer, Designer]")
	}
}

// Tests toBoolean with CASE in a WHERE clause
func TestConformance_Phase3_ToBooleanInWhere(t *testing.T) {
	_, executor, cleanup := setupPhase3ConformanceGraph(t)
	defer cleanup()

	// Use toBoolean to convert a string to boolean in a WHERE check
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE toBoolean('true') AND n.age > 30 RETURN n.name`)

	if result.Count != 1 || result.Rows[0]["n.name"] != "Charlie" {
		t.Errorf("expected only Charlie (age 35 > 30), got %d rows", result.Count)
	}
}

// Tests all Phase 3 features in a single query pipeline
func TestConformance_Phase3_AllFeaturesCombined(t *testing.T) {
	_, executor, cleanup := setupPhase3ConformanceGraph(t)
	defer cleanup()

	// Pipeline: MATCH -> WHERE with IN -> variable-length OPTIONAL MATCH -> IS NULL check
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.role IN ['Engineer', 'Manager']
		 OPTIONAL MATCH (n)-[:KNOWS*1..2]->(friend:Person)
		 RETURN n.name, friend IS NULL AS noFriends`)

	if result.Count < 3 {
		t.Fatalf("expected at least 3 rows, got %d", result.Count)
	}

	// Diana is an Engineer but has no outgoing KNOWS edges
	foundDianaIsolated := false
	for _, row := range result.Rows {
		if row["n.name"] == "Diana" && row["noFriends"] == true {
			foundDianaIsolated = true
		}
	}
	if !foundDianaIsolated {
		t.Error("expected Diana (Engineer, no outgoing edges) to have noFriends=true")
	}
}

// --- Phase 4 Conformance Tests: Expression Operators ---

func TestConformance_Phase4_ArithmeticWithOptionalMatch(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Arithmetic on nullable values — OPTIONAL MATCH may bind nil for b
	// Alice has friends, Bob/Charlie do not. b.age will be nil for unmatched.
	// Arithmetic with nil should propagate null (not crash).
	result := parseAndExecute(t, executor,
		`MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person) RETURN a.name, b.age + 10 AS agePlus`)

	if result.Count < 3 {
		t.Fatalf("expected at least 3 rows, got %d", result.Count)
	}

	// For Bob and Charlie (no outgoing KNOWS), b.age is nil → nil + 10 = nil
	for _, row := range result.Rows {
		name := row["a.name"]
		val := row["agePlus"]
		if name == "Bob" || name == "Charlie" {
			if val != nil {
				t.Errorf("%s: expected nil for b.age + 10 (no friends), got %v", name, val)
			}
		}
	}
}

func TestConformance_Phase4_NotInWithParameters(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// NOT IN with parameter-provided threshold
	// n.salary NOT IN [60000, 90000] → Alice(80000) is the only match
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.salary NOT IN [60000, 90000] RETURN n.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 result, got %d", result.Count)
	}
	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("expected Alice, got %v", result.Rows[0]["n.name"])
	}
}

func TestConformance_Phase4_ArithmeticInWhereWithCase(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Combine arithmetic in WHERE with CASE in RETURN
	result := parseAndExecute(t, executor,
		`MATCH (n:Person)
		 WHERE n.salary + 10000 > 85000
		 RETURN n.name, CASE WHEN n.salary > 85000 THEN 'high' ELSE 'mid' END AS tier`)

	// salary + 10000 > 85000 means salary > 75000 → Alice(80000), Charlie(90000)
	if result.Count != 2 {
		t.Fatalf("expected 2 results, got %d", result.Count)
	}

	for _, row := range result.Rows {
		name := row["n.name"]
		tier := row["tier"]
		switch name {
		case "Alice":
			if tier != "mid" {
				t.Errorf("Alice(80000): tier=%v, want mid", tier)
			}
		case "Charlie":
			if tier != "high" {
				t.Errorf("Charlie(90000): tier=%v, want high", tier)
			}
		}
	}
}

func TestConformance_Phase4_StringConcatInWith(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// String concatenation piped through WITH
	result := parseAndExecute(t, executor,
		`MATCH (n:Person)
		 WITH n.name + '_user' AS username
		 RETURN username`)

	if result.Count != 3 {
		t.Fatalf("expected 3 results, got %d", result.Count)
	}

	// Column name for projected variable "username" is "username."
	// (PropertyExpression with empty Property)
	col := result.Columns[0]
	usernames := make(map[string]bool)
	for _, row := range result.Rows {
		if u, ok := row[col].(string); ok {
			usernames[u] = true
		}
	}
	for _, expected := range []string{"Alice_user", "Bob_user", "Charlie_user"} {
		if !usernames[expected] {
			t.Errorf("expected %q in results, got %v", expected, usernames)
		}
	}
}

func TestConformance_Phase4_UnaryMinusWithOrderBy(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Negate salary in RETURN and verify it works with AS alias
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) RETURN n.name, -n.salary AS negSalary`)

	if result.Count != 3 {
		t.Fatalf("expected 3 results, got %d", result.Count)
	}

	for _, row := range result.Rows {
		name := row["n.name"]
		neg := row["negSalary"]
		switch name {
		case "Alice":
			if neg != int64(-80000) {
				t.Errorf("Alice negSalary=%v, want -80000", neg)
			}
		case "Bob":
			if neg != int64(-60000) {
				t.Errorf("Bob negSalary=%v, want -60000", neg)
			}
		case "Charlie":
			if neg != int64(-90000) {
				t.Errorf("Charlie negSalary=%v, want -90000", neg)
			}
		}
	}
}

func TestConformance_Phase4_NotWithIsNull(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Combine NOT with IS NOT NULL — double negative becomes IS NULL
	// NOT (n.salary IS NOT NULL) is equivalent to n.salary IS NULL
	// All 3 people have salary, so this should return 0 rows
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE NOT (n.salary IS NOT NULL) RETURN n.name`)

	if result.Count != 0 {
		t.Errorf("expected 0 results (all have salary), got %d", result.Count)
	}
}

func TestConformance_Phase4_ArithmeticWithUnion(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Arithmetic in both branches of UNION
	result := parseAndExecute(t, executor,
		`MATCH (n:Person {name: 'Alice'}) RETURN n.salary * 2 AS doubled
		 UNION ALL
		 MATCH (n:Person {name: 'Bob'}) RETURN n.salary * 2 AS doubled`)

	if result.Count != 2 {
		t.Fatalf("expected 2 results, got %d", result.Count)
	}

	values := make(map[any]bool)
	for _, row := range result.Rows {
		values[row["doubled"]] = true
	}
	if !values[int64(160000)] {
		t.Error("expected 160000 (Alice 80000*2)")
	}
	if !values[int64(120000)] {
		t.Error("expected 120000 (Bob 60000*2)")
	}
}

func TestConformance_Phase4_AllFeaturesCombined(t *testing.T) {
	_, executor, cleanup := setupConformanceGraph(t)
	defer cleanup()

	// Grand composition: arithmetic in WHERE + NOT IN + string concat in RETURN + CASE
	result := parseAndExecute(t, executor,
		`MATCH (n:Person)
		 WHERE n.salary * 1.0 > 55000 AND n.name NOT IN ['Charlie']
		 RETURN n.name + ' (' + n.department + ')' AS label,
		        n.salary - 50000 AS overBase,
		        CASE WHEN n.age > 28 THEN 'senior' ELSE 'junior' END AS level`)

	// salary > 55000 AND NOT Charlie → Alice(80k), Bob(60k)
	if result.Count != 2 {
		t.Fatalf("expected 2 results, got %d", result.Count)
	}

	for _, row := range result.Rows {
		label := row["label"]
		overBase := row["overBase"]
		level := row["level"]
		switch {
		case label == "Alice (Engineering)":
			// salary - 50000 = 30000 (float since salary * 1.0 forced context, but stored as int)
			if overBase != int64(30000) {
				t.Errorf("Alice overBase=%v, want 30000", overBase)
			}
			if level != "senior" {
				t.Errorf("Alice level=%v, want senior", level)
			}
		case label == "Bob (Sales)":
			if overBase != int64(10000) {
				t.Errorf("Bob overBase=%v, want 10000", overBase)
			}
			if level != "junior" {
				t.Errorf("Bob level=%v, want junior", level)
			}
		default:
			t.Errorf("unexpected label: %v", label)
		}
	}
}

// --- Phase 5 Conformance Tests: Cross-Feature Composition ---

// setupPhase5ConformanceGraph creates a graph suitable for testing Phase 5 features
// combined with prior phases. Includes edge properties, multiple labels, varied names.
func setupPhase5ConformanceGraph(t *testing.T) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	alice, _ := gs.CreateNode([]string{"Person", "Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice Anderson"),
		"age":        storage.IntValue(30),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(80000),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob Baker"),
		"age":        storage.IntValue(25),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(60000),
	})
	charlie, _ := gs.CreateNode([]string{"Person", "Manager"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie Chen"),
		"age":        storage.IntValue(35),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(90000),
	})

	_, _ = gs.CreateEdge(alice.ID, bob.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
		"trust": storage.FloatValue(0.9),
	}, 1.0)
	_, _ = gs.CreateEdge(alice.ID, charlie.ID, "MANAGES", map[string]storage.Value{
		"since": storage.IntValue(2018),
	}, 1.0)
	_, _ = gs.CreateEdge(charlie.ID, bob.ID, "MENTORS", map[string]storage.Value{
		"since":   storage.IntValue(2021),
		"project": storage.StringValue("Atlas"),
	}, 1.0)

	executor := NewExecutor(gs)
	return gs, executor, cleanup
}

func TestConformance_Phase5_OrderByWithStartsWith(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// STARTS WITH to filter, ORDER BY to sort remaining
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name STARTS WITH 'Alice' OR n.name STARTS WITH 'Charlie'
		 RETURN n.name ORDER BY n.salary DESC`)

	if result.Count != 2 {
		t.Fatalf("expected 2 rows, got %d", result.Count)
	}
	// Charlie(90k) first, Alice(80k) second
	if result.Rows[0]["n.name"] != "Charlie Chen" {
		t.Errorf("row 0: expected Charlie Chen, got %v", result.Rows[0]["n.name"])
	}
	if result.Rows[1]["n.name"] != "Alice Anderson" {
		t.Errorf("row 1: expected Alice Anderson, got %v", result.Rows[1]["n.name"])
	}
}

func TestConformance_Phase5_EdgePropsWithSchemaFunctions(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// Combine type(r), r.since, and labels(n) in a single query
	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r]->(b:Person) RETURN a.name, type(r) AS relType, r.since, labels(b) AS bLabels`)

	if result.Count != 3 {
		t.Fatalf("expected 3 edges, got %d", result.Count)
	}

	foundMentors := false
	for _, row := range result.Rows {
		if row["relType"] == "MENTORS" {
			foundMentors = true
			if row["r.since"] != int64(2021) {
				t.Errorf("MENTORS since=%v, want 2021", row["r.since"])
			}
		}
	}
	if !foundMentors {
		t.Error("expected to find MENTORS edge")
	}
}

func TestConformance_Phase5_SetExprWithEdgeProperty(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// Use edge property in WHERE, then SET expression to update the node
	parseAndExecute(t, executor,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) WHERE r.since > 2019 SET b.salary = b.salary + 5000`)

	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Bob Baker' RETURN n.salary`)

	if result.Rows[0]["n.salary"] != int64(65000) {
		t.Errorf("expected salary=65000 after raise, got %v", result.Rows[0]["n.salary"])
	}
}

func TestConformance_Phase5_ContainsWithOrderByAlias(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// CONTAINS filter + ORDER BY aliased column
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name CONTAINS 'e'
		 RETURN n.name AS name, n.age AS age ORDER BY age`)

	// "Alice Anderson" and "Charlie Chen" contain 'e'; "Bob Baker" has 'e' in Baker
	if result.Count < 2 {
		t.Fatalf("expected at least 2 results containing 'e', got %d", result.Count)
	}
	// Verify ascending order
	for i := 1; i < len(result.Rows); i++ {
		prev, _ := result.Rows[i-1]["age"].(int64)
		curr, _ := result.Rows[i]["age"].(int64)
		if curr < prev {
			t.Errorf("ORDER BY age broken: row %d has %d < row %d with %d", i, curr, i-1, prev)
		}
	}
}

func TestConformance_Phase5_IdAndKeysWithOptionalMatch(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// Use id() and keys() with OPTIONAL MATCH
	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice Anderson'})
		 OPTIONAL MATCH (a)-[r:MENTORS]->(b:Person)
		 RETURN id(a) AS aid, keys(a) AS akeys, type(r) AS relType`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}

	// Alice has no outgoing MENTORS (she KNOWS Bob and MANAGES Charlie)
	if result.Rows[0]["relType"] != nil {
		t.Errorf("expected nil relType (no MENTORS from Alice), got %v", result.Rows[0]["relType"])
	}
	aid, ok := result.Rows[0]["aid"].(int64)
	if !ok || aid <= 0 {
		t.Errorf("expected positive id for Alice, got %v", result.Rows[0]["aid"])
	}
	akeys, ok := result.Rows[0]["akeys"].([]any)
	if !ok || len(akeys) < 3 {
		t.Errorf("expected keys for Alice, got %v", result.Rows[0]["akeys"])
	}
}

func TestConformance_Phase5_EndsWithInUnion(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// ENDS WITH in both branches of UNION
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name ENDS WITH 'Anderson' RETURN n.name AS name
		 UNION ALL
		 MATCH (n:Person) WHERE n.name ENDS WITH 'Chen' RETURN n.name AS name`)

	if result.Count != 2 {
		t.Fatalf("expected 2 rows, got %d", result.Count)
	}

	names := make(map[string]bool)
	for _, row := range result.Rows {
		names[row["name"].(string)] = true
	}
	if !names["Alice Anderson"] || !names["Charlie Chen"] {
		t.Errorf("expected Alice Anderson and Charlie Chen, got %v", names)
	}
}

func TestConformance_Phase5_PropertiesWithArithmetic(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// Use properties() combined with arithmetic in RETURN
	result := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Bob Baker'
		 RETURN properties(n) AS props, n.salary * 2 AS doubleSalary`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row, got %d", result.Count)
	}

	props, ok := result.Rows[0]["props"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any for properties, got %T", result.Rows[0]["props"])
	}
	if props["name"] != "Bob Baker" {
		t.Errorf("expected name=Bob Baker in properties, got %v", props["name"])
	}
	if result.Rows[0]["doubleSalary"] != int64(120000) {
		t.Errorf("expected doubleSalary=120000, got %v", result.Rows[0]["doubleSalary"])
	}
}

func TestConformance_Phase5_AllFeaturesCombined(t *testing.T) {
	_, executor, cleanup := setupPhase5ConformanceGraph(t)
	defer cleanup()

	// Grand composition: edge properties + type() + CONTAINS + ORDER BY + SET expression + CASE
	// 1. Query edges where relationship started after 2019
	// 2. Return edge type, node names, and a tier classification
	// 3. Order by since date
	result := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r]->(b:Person)
		 WHERE r.since >= 2020 AND a.name CONTAINS 'Alice'
		 RETURN type(r) AS relType, b.name, r.since,
		        CASE WHEN r.since > 2019 THEN 'recent' ELSE 'old' END AS era
		 ORDER BY r.since`)

	// Alice -> Bob (KNOWS, 2020), Alice -> Charlie (MANAGES, 2018 filtered out)
	if result.Count != 1 {
		t.Fatalf("expected 1 result (KNOWS since 2020), got %d", result.Count)
	}

	row := result.Rows[0]
	if row["relType"] != "KNOWS" {
		t.Errorf("expected relType=KNOWS, got %v", row["relType"])
	}
	if row["b.name"] != "Bob Baker" {
		t.Errorf("expected b.name=Bob Baker, got %v", row["b.name"])
	}
	if row["era"] != "recent" {
		t.Errorf("expected era=recent, got %v", row["era"])
	}
}
