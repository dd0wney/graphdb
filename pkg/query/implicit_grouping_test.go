package query

import (
	"fmt"
	"math"
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

// ORDER BY may name a grouping key by its expression even when RETURN aliased
// it; ordering by anything else after aggregation has no defined value per
// group, so it is refused rather than silently ignored.
func TestGrouping_OrderByKeyExpressionAndRefusal(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	for _, c := range []string{"A", "C", "B", "A"} {
		person(t, gs, map[string]storage.Value{"city": storage.StringValue(c), "name": storage.StringValue("p" + c)})
	}

	rows := runRows(t, ex, "MATCH (n:Person) RETURN n.city AS city, count(n) AS c ORDER BY n.city DESC")
	var order []string
	for _, r := range rows {
		order = append(order, fmt.Sprint(r["city"]))
	}
	if got := fmt.Sprint(order); got != "[C B A]" {
		t.Fatalf("order = %s, want [C B A]; rows %v", got, rows)
	}
	for _, r := range rows {
		if _, leaked := r["n.city"]; leaked {
			t.Fatalf("the sort column leaked into the result: %v", r)
		}
	}

	tokens, err := NewLexer("MATCH (n:Person) RETURN n.city AS city, count(n) AS c ORDER BY n.name").Tokenize()
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	if _, err := NewParser(tokens).Parse(); err == nil {
		t.Fatal("ORDER BY a non-key expression after aggregation parsed; want a refusal")
	}
}

// Two key values share a group only when openCypher treats them as the same:
// numbers compare as numbers, lists element by element.
func TestGrouping_GroupKeyEquivalence(t *testing.T) {
	same := [][2]any{
		{int64(0), -0.0},
		{int64(1), 1.0},
		{math.NaN(), math.NaN()},
		{[]any{int64(1), "a"}, []any{1.0, "a"}},
	}
	for _, p := range same {
		if groupKey([]any{p[0]}) != groupKey([]any{p[1]}) {
			t.Errorf("%#v and %#v must share a group", p[0], p[1])
		}
	}
	different := [][2]any{
		{int64(1), "1"},
		{[]any{int64(1)}, []any{"1"}},
		{[]any{"a b"}, []any{"a", "b"}},
		{"a", []any{"a"}},
		{nil, "null"},
	}
	for _, p := range different {
		if groupKey([]any{p[0]}) == groupKey([]any{p[1]}) {
			t.Errorf("%#v and %#v must not share a group", p[0], p[1])
		}
	}
}

// sum, avg, min and max of a node or relationship have no value; they used to
// return 0 or an arbitrary entity with no error.
func TestGrouping_NumericAggregateOfAnEntityRefuses(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	person(t, gs, map[string]storage.Value{"city": storage.StringValue("A")})

	for _, q := range []string{
		"MATCH (n:Person) RETURN sum(n) AS s",
		"MATCH (n:Person) RETURN n.city AS city, max(n) AS m",
	} {
		if _, err := ex.Execute(parseCallInput(t, q)); err == nil {
			t.Errorf("%s succeeded; want a refusal", q)
		}
	}
	if _, err := ex.Execute(parseCallInput(t, "MATCH (n:Person) RETURN count(n) AS c, collect(n) AS ns")); err != nil {
		t.Errorf("count and collect of a node must work: %v", err)
	}
}
