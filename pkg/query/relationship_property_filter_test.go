package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupRelPropertyFilterGraph builds a small graph that separates edges by
// an inline relationship property (since) as well as by hop count, so a
// property filter and a type filter give different answers.
//
// Nodes: Alice, Bob, Carol, Dave (all :Person)
// Edges:
//
//	Alice -[:KNOWS {since: 2020}]-> Bob
//	Alice -[:KNOWS {since: 2021}]-> Dave
//	Bob   -[:KNOWS {since: 2020}]-> Carol
func setupRelPropertyFilterGraph(t *testing.T) (*Executor, func()) {
	t.Helper()

	gs, cleanup := setupExecutorTestGraph(t)

	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	carol, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Carol"),
	})
	dave, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Dave"),
	})

	_, _ = gs.CreateEdge(alice.ID, bob.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)
	_, _ = gs.CreateEdge(alice.ID, dave.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2021),
	}, 1.0)
	_, _ = gs.CreateEdge(bob.ID, carol.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	executor := NewExecutor(gs)
	return executor, cleanup
}

func namesOf(t *testing.T, rows []map[string]any, column string) map[string]bool {
	t.Helper()
	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		name, ok := row[column].(string)
		if !ok {
			t.Fatalf("row %v missing string column %q", row, column)
		}
		names[name] = true
	}
	return names
}

// Test 1 (red-first): a single-hop pattern's inline property filter must
// narrow the two KNOWS edges from Alice down to the one matching {since:
// 2020}. Against the pre-fix getEdges (no property filter at all) this
// test failed with 2 rows, {Bob, Dave} - see the PR body for the recorded
// failure output.
func TestRelationshipPropertyFilter_SingleHop(t *testing.T) {
	executor, cleanup := setupRelPropertyFilterGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[r:KNOWS {since: 2020}]->(b) RETURN b.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row for {since: 2020}, got %d: %v", result.Count, result.Rows)
	}
	names := namesOf(t, result.Rows, "b.name")
	if !names["Bob"] {
		t.Errorf("expected Bob (since 2020), got %v", names)
	}
	if names["Dave"] {
		t.Errorf("Dave (since 2021) must not appear, got %v", names)
	}
}

// Test 2 (red-first): the same property filter, proven on the
// variable-length traversal path (traverseVariablePath), which also calls
// getEdges at every BFS expansion. *1..2 {since: 2020} from Alice must
// reach Bob (1 hop) and Carol (2 hops, via the since:2020 Bob->Carol edge)
// but never Dave, whose only edge from Alice is since:2021. Against the
// pre-fix code this returned Dave too - see the PR body for the recorded
// failure output.
func TestRelationshipPropertyFilter_VariableLength(t *testing.T) {
	executor, cleanup := setupRelPropertyFilterGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[:KNOWS*1..2 {since: 2020}]->(b) RETURN DISTINCT b.name`)

	names := namesOf(t, result.Rows, "b.name")
	if !names["Bob"] {
		t.Errorf("expected Bob at 1 hop (since 2020), got %v", names)
	}
	if !names["Carol"] {
		t.Errorf("expected Carol at 2 hops (since 2020 edge), got %v", names)
	}
	if names["Dave"] {
		t.Errorf("Dave (since 2021) must not appear, got %v", names)
	}
	if len(names) != 2 {
		t.Errorf("expected exactly 2 distinct names, got %d: %v", len(names), names)
	}
}

// Test 3 (red-first): the early-return hazard. A pattern with properties
// and NO type - MATCH (a)-[{since: 2020}]->(b) - must still filter, even
// though rel.Type == "" used to trigger an unfiltered early return in
// getEdges. Against the pre-fix code this returned both Bob and Dave -
// see the PR body for the recorded failure output.
func TestRelationshipPropertyFilter_NoTypeEarlyReturn(t *testing.T) {
	executor, cleanup := setupRelPropertyFilterGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[{since: 2020}]->(b) RETURN b.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row for {since: 2020} with no type, got %d: %v", result.Count, result.Rows)
	}
	names := namesOf(t, result.Rows, "b.name")
	if !names["Bob"] {
		t.Errorf("expected Bob (since 2020), got %v", names)
	}
}

// Control 4: a type with no properties must still return every edge of
// that type. This proves the fix does not over-filter. Must pass before
// and after the fix.
func TestRelationshipPropertyFilter_TypeOnlyControl(t *testing.T) {
	executor, cleanup := setupRelPropertyFilterGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[r:KNOWS]->(b) RETURN b.name`)

	if result.Count != 2 {
		t.Fatalf("expected 2 rows (Bob, Dave) with no property filter, got %d: %v", result.Count, result.Rows)
	}
	names := namesOf(t, result.Rows, "b.name")
	if !names["Bob"] || !names["Dave"] {
		t.Errorf("expected {Bob, Dave}, got %v", names)
	}
}

// Control 5: a pattern with neither a type nor properties must return
// every edge. Must pass before and after the fix.
func TestRelationshipPropertyFilter_NeitherControl(t *testing.T) {
	executor, cleanup := setupRelPropertyFilterGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[]->(b) RETURN b.name`)

	if result.Count != 2 {
		t.Fatalf("expected 2 rows (Bob, Dave) with no filter at all, got %d: %v", result.Count, result.Rows)
	}
	names := namesOf(t, result.Rows, "b.name")
	if !names["Bob"] || !names["Dave"] {
		t.Errorf("expected {Bob, Dave}, got %v", names)
	}
}

// Control 6: the WHERE-clause path (WHERE r.since = 2020) was never
// broken by the defect and must not change. This does not go through the
// inline-properties branch of getEdges at all.
func TestRelationshipPropertyFilter_WhereClauseControl(t *testing.T) {
	executor, cleanup := setupRelPropertyFilterGraph(t)
	defer cleanup()

	result := parseAndExecute(t, executor,
		`MATCH (a:Person {name: 'Alice'})-[r:KNOWS]->(b) WHERE r.since = 2020 RETURN b.name`)

	if result.Count != 1 {
		t.Fatalf("expected 1 row for WHERE r.since = 2020, got %d: %v", result.Count, result.Rows)
	}
	names := namesOf(t, result.Rows, "b.name")
	if !names["Bob"] {
		t.Errorf("expected Bob (since 2020), got %v", names)
	}
}
