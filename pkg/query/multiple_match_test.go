package query

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Several patterns in one MATCH, and several MATCH clauses, are a join: every
// row binds every pattern's variables, and a variable shared by two patterns
// is the same entity in both. The parser used to keep one slot per clause, so
// a second MATCH replaced the first; MatchStep returned each pattern's
// matches as separate rows; and a path pattern did not check a target
// variable an earlier pattern had bound.

// p1 and p2 work at c1, p3 works at c2.
func newEmploymentGraph(t *testing.T) (*storage.GraphStorage, *Executor) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)

	node := func(label, name string) *storage.Node {
		return mustOracleNode(t, gs, label, map[string]storage.Value{"name": storage.StringValue(name)})
	}
	p1, p2, p3 := node("Person", "p1"), node("Person", "p2"), node("Person", "p3")
	c1, c2 := node("Company", "c1"), node("Company", "c2")
	for _, e := range [][2]*storage.Node{{p1, c1}, {p2, c1}, {p3, c2}} {
		if _, err := gs.CreateEdge(e[0].ID, e[1].ID, "WORKS_AT", map[string]storage.Value{}, 1); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}
	return gs, NewExecutor(gs)
}

func sortedRows(t *testing.T, ex *Executor, query string) []string {
	t.Helper()
	rs, err := ex.Execute(parseCallInput(t, query))
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	rows := make([]string, 0, len(rs.Rows))
	for _, r := range rs.Rows {
		rows = append(rows, fmt.Sprint(r))
	}
	sort.Strings(rows)
	return rows
}

func assertRows(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows\n  got:  %v\n  want: %v", got, want)
	}
}

func TestMatch_CommaPatternsJoin(t *testing.T) {
	_, ex := newEmploymentGraph(t)

	got := sortedRows(t, ex, "MATCH (p:Person), (c:Company) RETURN p.name, c.name")

	assertRows(t, got,
		"map[c.name:c1 p.name:p1]", "map[c.name:c1 p.name:p2]", "map[c.name:c1 p.name:p3]",
		"map[c.name:c2 p.name:p1]", "map[c.name:c2 p.name:p2]", "map[c.name:c2 p.name:p3]")
}

// c is bound by the first pattern, so the second must reach that same c.
func TestMatch_VariableSharedAcrossPatterns(t *testing.T) {
	_, ex := newEmploymentGraph(t)

	got := sortedRows(t, ex,
		"MATCH (p:Person)-[:WORKS_AT]->(c:Company), (q:Person)-[:WORKS_AT]->(c) WHERE p.name = 'p1' RETURN q.name")

	assertRows(t, got, "map[q.name:p1]", "map[q.name:p2]")
}

func TestMatch_TwoMatchClausesWithTheirWheres(t *testing.T) {
	_, ex := newEmploymentGraph(t)

	got := sortedRows(t, ex,
		"MATCH (p:Person) WHERE p.name = 'p3' MATCH (c:Company) WHERE c.name = 'c1' RETURN p.name, c.name")

	assertRows(t, got, "map[c.name:c1 p.name:p3]")
}

func TestSetAndRemove_RepeatedClausesAllApply(t *testing.T) {
	gs, ex := newEmploymentGraph(t)

	if _, err := ex.Execute(parseCallInput(t, "MATCH (p:Person {name: 'p1'}) SET p.x = 1 SET p.y = 2")); err != nil {
		t.Fatalf("SET SET: %v", err)
	}
	if _, err := ex.Execute(parseCallInput(t, "MATCH (p:Person {name: 'p2'}) SET p.a = 1, p.b = 2")); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if _, err := ex.Execute(parseCallInput(t, "MATCH (p:Person {name: 'p2'}) REMOVE p.a REMOVE p.b")); err != nil {
		t.Fatalf("REMOVE REMOVE: %v", err)
	}

	props := map[string]map[string]storage.Value{}
	for _, n := range gs.GetAllNodesAcrossTenants() {
		props[string(n.Properties["name"].Data)] = n.Properties
	}
	if _, ok := props["p1"]["x"]; !ok {
		t.Errorf("first SET was dropped: p1 = %v", props["p1"])
	}
	if _, ok := props["p1"]["y"]; !ok {
		t.Errorf("second SET was dropped: p1 = %v", props["p1"])
	}
	if _, ok := props["p2"]["a"]; ok {
		t.Errorf("first REMOVE was dropped: p2 = %v", props["p2"])
	}
	if _, ok := props["p2"]["b"]; ok {
		t.Errorf("second REMOVE was dropped: p2 = %v", props["p2"])
	}
}

// A repeated clause that cannot be merged, or a MATCH after a write that the
// fixed plan order would run before the write, must fail to parse rather than
// lose a clause.
func TestParse_RefusesClausesItCannotKeep(t *testing.T) {
	for _, query := range []string{
		"CREATE (a:X) CREATE (b:Y)",
		"MATCH (a:Person) RETURN a.name RETURN a.name",
		"MATCH (n:Person) UNWIND n.l AS x UNWIND n.m AS y RETURN x",
		"CREATE (a:X) MATCH (b:Person) RETURN b.name",
		"MATCH (a:Person) SET a.x = 1 MATCH (b:Company) RETURN b.name",
		"MATCH (a:Person) DELETE a DETACH DELETE a",
		"MATCH (n:Person) UNWIND n.l AS x WHERE x = 1 RETURN x",
		"MERGE (a:X {k: 1}) WHERE a.k = 1 RETURN a.k",
		"OPTIONAL MATCH (a:Person) MATCH (b:Company) RETURN b.name",
		"MATCH (a:Person) SET a.x = 1 MERGE (b:Y {k: 1})",
		"CREATE (a:X) MERGE (b:Y {k: 1})",
	} {
		t.Run(query, func(t *testing.T) {
			gs, _ := newEmploymentGraph(t)
			before := len(gs.GetAllNodesAcrossTenants())

			tokens, err := NewLexer(query).Tokenize()
			if err != nil {
				t.Fatalf("Tokenize: %v", err)
			}
			if _, err := NewParser(tokens).Parse(); err == nil {
				t.Fatalf("parsed; want a parse error naming the clause it cannot keep")
			}
			if after := len(gs.GetAllNodesAcrossTenants()); after != before {
				t.Fatalf("node count changed from %d to %d", before, after)
			}
		})
	}
}

// Each MERGE runs in text order on the rows the one before it left; a second
// MERGE used to replace the first.
func TestMerge_RepeatedNodeMergesBothApply(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	ex := NewExecutor(gs)

	for run := 1; run <= 2; run++ {
		if _, err := ex.Execute(parseCallInput(t, "MERGE (a:X {k: 1}) MERGE (b:Y {k: 2})")); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		labels := map[string]int{}
		for _, n := range gs.GetAllNodesAcrossTenants() {
			labels[n.Labels[0]]++
		}
		if labels["X"] != 1 || labels["Y"] != 1 || len(labels) != 2 {
			t.Fatalf("run %d: nodes by label = %v, want X:1 Y:1", run, labels)
		}
	}
}

// A variable-length relationship variable bound by an earlier pattern must be
// the same relationship, and a variable bound to null (WITH of an OPTIONAL
// MATCH miss) matches nothing, as in openCypher.
func TestMatch_BoundVariableConstraints(t *testing.T) {
	_, ex := newEmploymentGraph(t)

	t.Run("relationship variable reused by a variable-length pattern", func(t *testing.T) {
		got := sortedRows(t, ex,
			"MATCH (p:Person {name: 'p1'})-[r:WORKS_AT]->(c:Company), (q:Person)-[r*1..2]->(d) RETURN q.name")
		assertRows(t, got)
	})
	t.Run("variable bound to null matches nothing", func(t *testing.T) {
		got := sortedRows(t, ex,
			"OPTIONAL MATCH (x:Nobody) WITH x MATCH (x)-[:WORKS_AT]->(c:Company) RETURN c.name")
		assertRows(t, got)
	})
}
