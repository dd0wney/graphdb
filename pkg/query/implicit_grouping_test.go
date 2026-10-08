package query

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// In openCypher every non-aggregate RETURN item is a grouping key. graphdb
// grouped only on its own GROUP BY keyword, so RETURN n.city, count(n) gave one
// row per node with count null. The GROUP BY key was also a %v string parsed
// back, so 1 and "1" shared a group and every key came back as a string.

func newGroupingGraph(t *testing.T) (*storage.GraphStorage, *Executor) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	return gs, NewExecutor(gs)
}

func person(t *testing.T, gs *storage.GraphStorage, props map[string]storage.Value) *storage.Node {
	t.Helper()
	return mustOracleNode(t, gs, "Person", props)
}

func runRows(t *testing.T, ex *Executor, query string) []map[string]any {
	t.Helper()
	rs, err := ex.Execute(parseCallInput(t, query))
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return rs.Rows
}

// countOf reads an aggregate count whatever integer type the engine used.
func countOf(v any) int64 {
	var n int64
	fmt.Sscan(fmt.Sprint(v), &n)
	return n
}

func rowStrings(rows []map[string]any) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprint(r)
	}
	sort.Strings(out)
	return out
}

func TestGrouping_ImplicitKeysFromNonAggregateItems(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	for _, c := range []string{"A", "A", "B"} {
		person(t, gs, map[string]storage.Value{"city": storage.StringValue(c)})
	}

	got := rowStrings(runRows(t, ex, "MATCH (n:Person) RETURN n.city AS city, count(n) AS c"))

	want := []string{"map[c:1 city:B]", "map[c:2 city:A]"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

// A node is a grouping key by identity.
func TestGrouping_WholeNodeKey(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	x := person(t, gs, map[string]storage.Value{"name": storage.StringValue("x")})
	y := person(t, gs, map[string]storage.Value{"name": storage.StringValue("y")})
	q := mustOracleNode(t, gs, "Q", nil)
	for _, from := range []*storage.Node{x, x, y} {
		if _, err := gs.CreateEdge(from.ID, q.ID, "R", map[string]storage.Value{}, 1); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}

	rows := runRows(t, ex, "MATCH (p:Person)-[r:R]->(q) RETURN p AS p, count(r) AS c")

	counts := map[uint64]int64{}
	for _, row := range rows {
		node, ok := row["p"].(*storage.Node)
		if !ok {
			t.Fatalf("p = %T, want *storage.Node; row %v", row["p"], row)
		}
		c := countOf(row["c"])
		counts[node.ID] = c
	}
	if len(rows) != 2 || counts[x.ID] != 2 || counts[y.ID] != 1 {
		t.Fatalf("counts by node = %v over %d rows, want x:2 y:1", counts, len(rows))
	}
}

// null is its own group, and a value of another type is another group.
func TestGrouping_NullAndTypedKeys(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	person(t, gs, map[string]storage.Value{"k": storage.IntValue(1)})
	person(t, gs, map[string]storage.Value{"k": storage.IntValue(1)})
	person(t, gs, map[string]storage.Value{"k": storage.StringValue("1")})
	person(t, gs, nil)

	rows := runRows(t, ex, "MATCH (n:Person) RETURN n.k AS k, count(n) AS c")

	if len(rows) != 3 {
		t.Fatalf("got %d groups, want 3 (int 1, string \"1\", null): %v", len(rows), rows)
	}
	byKey := map[string]int64{}
	for _, row := range rows {
		c := countOf(row["c"])
		byKey[fmt.Sprintf("%T:%v", row["k"], row["k"])] = c
	}
	if byKey["int64:1"] != 2 || byKey["string:1"] != 1 || byKey["<nil>:<nil>"] != 1 {
		t.Fatalf("groups = %v, want int64:1→2, string:1→1, <nil>→1", byKey)
	}
}

// GROUP BY keeps working, and keeps the key's type.
func TestGrouping_GroupByKeepsWorkingAndTypes(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	for _, k := range []int64{7, 7, 9} {
		person(t, gs, map[string]storage.Value{"k": storage.IntValue(k)})
	}

	rows := runRows(t, ex, "MATCH (n:Person) RETURN n.k, count(n) AS c GROUP BY n.k")

	byKey := map[any]int64{}
	for _, row := range rows {
		c := countOf(row["c"])
		byKey[row["n.k"]] = c
	}
	if len(rows) != 2 || byKey[int64(7)] != 2 || byKey[int64(9)] != 1 {
		t.Fatalf("GROUP BY rows = %v, want int64 keys 7→2 and 9→1", rows)
	}
}

func TestGrouping_OrderByAndLimitOnGroups(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	for _, c := range []string{"A", "B", "A", "C", "A", "B"} {
		person(t, gs, map[string]storage.Value{"city": storage.StringValue(c)})
	}

	rows := runRows(t, ex, "MATCH (n:Person) RETURN n.city AS city, count(n) AS c ORDER BY c DESC LIMIT 2")

	if got := fmt.Sprint(rows); got != "[map[c:3 city:A] map[c:2 city:B]]" {
		t.Fatalf("rows = %s, want A:3 then B:2", got)
	}
}

// With no rows there are no groups; an aggregate with no key still gives one row.
func TestGrouping_NoRows(t *testing.T) {
	_, ex := newGroupingGraph(t)

	if rows := runRows(t, ex, "MATCH (n:Nobody) RETURN n.city AS city, count(n) AS c"); len(rows) != 0 {
		t.Fatalf("keyed aggregate over no rows = %v, want no rows", rows)
	}
	rows := runRows(t, ex, "MATCH (n:Nobody) RETURN count(n) AS c")
	if len(rows) != 1 {
		t.Fatalf("unkeyed aggregate over no rows = %v, want one row", rows)
	}
}

// Aggregates read relationship properties and whole entities too; they used
// to see only nodes, and collected a bare node as the number 1.
func TestGrouping_AggregatesOverRelationshipsAndEntities(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	x := person(t, gs, map[string]storage.Value{"name": storage.StringValue("x")})
	q := mustOracleNode(t, gs, "Q", nil)
	for _, w := range []int64{2, 5} {
		if _, err := gs.CreateEdge(x.ID, q.ID, "R", map[string]storage.Value{"w": storage.IntValue(w)}, 1); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}

	rows := runRows(t, ex, "MATCH (p:Person)-[r:R]->(q) RETURN sum(r.w) AS total, collect(q) AS qs")
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}
	if got := fmt.Sprint(rows[0]["total"]); got != "7" {
		t.Errorf("sum(r.w) = %s, want 7", got)
	}
	qs, ok := rows[0]["qs"].([]any)
	if !ok || len(qs) != 2 {
		t.Fatalf("collect(q) = %#v, want two nodes", rows[0]["qs"])
	}
	if node, ok := qs[0].(*storage.Node); !ok || node.ID != q.ID {
		t.Errorf("collect(q)[0] = %#v, want the node %d", qs[0], q.ID)
	}
}
