package query

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// renderValue prints a result value in the notation of the openCypher TCK
// tables, with a whole float printed as an integer, because a list that went
// through JSON holds float64 for each number.
func renderValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "'" + x + "'"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = renderValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ": " + renderValue(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// propertyValue turns a Go value into the stored form of a node property.
// A nil value means "no property", which reads back as null.
func orderPropertyValue(t *testing.T, v any) (storage.Value, bool) {
	t.Helper()
	switch x := v.(type) {
	case nil:
		return storage.Value{}, false
	case bool:
		return storage.BoolValue(x), true
	case string:
		return storage.StringValue(x), true
	case int:
		return storage.IntValue(int64(x)), true
	case float64:
		return storage.FloatValue(x), true
	default:
		jv, err := storage.JSONValue(x)
		if err != nil {
			t.Fatalf("JSONValue(%v): %v", v, err)
		}
		return jv, true
	}
}

// sortProperty creates one :T node for each input value, in the order given,
// and returns the rendered values of ORDER BY on the property.
func sortProperty(t *testing.T, values []any, direction string) []string {
	t.Helper()
	gs, ex := newGroupingGraph(t)
	for i, v := range values {
		props := map[string]storage.Value{"seq": storage.IntValue(int64(i))}
		if pv, ok := orderPropertyValue(t, v); ok {
			props["v"] = pv
		}
		mustOracleNode(t, gs, "T", props)
	}
	rows := runRows(t, ex, `MATCH (n:T) RETURN n.v AS v ORDER BY v `+direction)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = renderValue(r["v"])
	}
	return out
}

func assertRendered(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("order mismatch\n got: %s\nwant: %s", strings.Join(got, " | "), strings.Join(want, " | "))
	}
}

// The TCK scenarios of ReturnOrderBy1 use UNWIND of a list literal, which the
// parser does not accept. Each test below builds the same values as the
// property v of :T nodes, created in the order of the TCK list, and sorts them
// with ORDER BY on that property.

func TestOrderByTCK_Booleans(t *testing.T) { // ReturnOrderBy1 [1], [2]
	in := []any{true, false}
	assertRendered(t, sortProperty(t, in, ""), "false", "true")
	assertRendered(t, sortProperty(t, in, "DESC"), "true", "false")
}

func TestOrderByTCK_Strings(t *testing.T) { // ReturnOrderBy1 [3], [4]
	in := []any{".*", "", " ", "one"}
	assertRendered(t, sortProperty(t, in, ""), "''", "' '", "'.*'", "'one'")
	assertRendered(t, sortProperty(t, in, "DESC"), "'one'", "'.*'", "' '", "''")
}

func TestOrderByTCK_Ints(t *testing.T) { // ReturnOrderBy1 [5], [6]
	in := []any{1, 3, 2}
	assertRendered(t, sortProperty(t, in, ""), "1", "2", "3")
	assertRendered(t, sortProperty(t, in, "DESC"), "3", "2", "1")
}

func TestOrderByTCK_Floats(t *testing.T) { // ReturnOrderBy1 [7], [8]
	in := []any{1.5, 1.3, 999.99}
	assertRendered(t, sortProperty(t, in, ""), "1.3", "1.5", "999.99")
	assertRendered(t, sortProperty(t, in, "DESC"), "999.99", "1.5", "1.3")
}

func tckLists() []any {
	return []any{
		[]any{}, []any{"a"}, []any{"a", 1}, []any{1}, []any{1, "a"}, []any{1, nil}, []any{nil, 1}, []any{nil, 2},
	}
}

func TestOrderByTCK_Lists(t *testing.T) { // ReturnOrderBy1 [9], [10]
	asc := []string{"[]", "['a']", "['a', 1]", "[1]", "[1, 'a']", "[1, null]", "[null, 1]", "[null, 2]"}
	assertRendered(t, sortProperty(t, tckLists(), ""), asc...)

	// Scramble the input with a fixed rotation, so that the result is not the
	// input order by chance.
	in := tckLists()
	in = append(in[3:], in[:3]...)
	assertRendered(t, sortProperty(t, in, ""), asc...)

	desc := make([]string, len(asc))
	for i, s := range asc {
		desc[len(asc)-1-i] = s
	}
	assertRendered(t, sortProperty(t, tckLists(), "DESC"), desc...)
}

// ReturnOrderBy1 [11], [12] put a node, a relationship, a path and the values
// below in one column. A node, a relationship and a path cannot be a node
// property, and this parser has no named path, so the scenarios are split:
// the storable values here, the node and the relationship in the tests that
// follow, and the type ladder with a node and a relationship in
// TestCompareTotal_TypeLadder.
func TestOrderByTCK_DistinctTypes(t *testing.T) {
	in := func() []any {
		return []any{1.5, []any{"list"}, "text", nil, false, math.NaN(), map[string]any{"a": "map"}}
	}
	asc := []string{"{a: 'map'}", "['list']", "'text'", "false", "1.5", "NaN", "null"}
	assertRendered(t, sortProperty(t, in(), ""), asc...)
	rev := make([]string, len(asc))
	for i, s := range asc {
		rev[len(asc)-1-i] = s
	}
	assertRendered(t, sortProperty(t, in(), "DESC"), rev...)

	// A different input order, with null first.
	in2 := in()
	in2[0], in2[3] = in2[3], in2[0]
	assertRendered(t, sortProperty(t, in2, ""), asc...)
}

func TestOrderByTCK_SkippedScenarios(t *testing.T) {
	t.Run("[11] path and node and relationship in one column", func(t *testing.T) {
		t.Skip("a named path (MATCH p = ...) does not parse, and UNWIND of a list literal is not supported; " +
			"the node and relationship ranks are covered by TestCompareTotal_TypeLadder")
	})
	t.Run("[12] path and node and relationship in one column, DESC", func(t *testing.T) {
		t.Skip("same reason as [11]")
	})
}

func TestOrderBy_NodesAndRelationshipsByID(t *testing.T) {
	gs, ex := newGroupingGraph(t)
	var nodes []*storage.Node
	for i := 0; i < 4; i++ {
		nodes = append(nodes, mustOracleNode(t, gs, "N", map[string]storage.Value{"k": storage.IntValue(int64(i))}))
	}
	// Edges created against the id order of their ends.
	for _, pair := range [][2]int{{2, 3}, {0, 1}, {1, 2}} {
		if _, err := gs.CreateEdge(nodes[pair[0]].ID, nodes[pair[1]].ID, "REL", nil, 1); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}

	rows := runRows(t, ex, `MATCH (n:N) RETURN n ORDER BY n DESC`)
	for i, r := range rows {
		node, ok := r["n"].(*storage.Node)
		if !ok {
			t.Fatalf("row %d holds %T, want *storage.Node", i, r["n"])
		}
		if want := nodes[len(nodes)-1-i].ID; node.ID != want {
			t.Errorf("DESC row %d: node %d, want %d", i, node.ID, want)
		}
	}

	rows = runRows(t, ex, `MATCH (a:N)-[r:REL]->(b:N) RETURN r ORDER BY r`)
	var last uint64
	for i, r := range rows {
		e, ok := r["r"].(*storage.Edge)
		if !ok {
			t.Fatalf("row %d holds %T, want *storage.Edge", i, r["r"])
		}
		if e.ID < last {
			t.Errorf("row %d: relationship %d after %d", i, e.ID, last)
		}
		last = e.ID
	}
	if len(rows) != 3 {
		t.Errorf("rows = %d, want 3", len(rows))
	}
}

// TestOrderBy_MissingPropertyLastForEachInputOrder: a node with no age is last
// for ASC and first for DESC, wherever it is in the input.
func TestOrderBy_MissingPropertyLastForEachInputOrder(t *testing.T) {
	type who struct {
		name string
		age  int64 // -1: no age
	}
	orders := [][]who{
		{{"zed", -1}, {"dave", 41}, {"Bob", 20}, {"alice", 30}},
		{{"dave", 41}, {"zed", -1}, {"alice", 30}, {"Bob", 20}},
		{{"Bob", 20}, {"alice", 30}, {"dave", 41}, {"zed", -1}},
	}
	for i, people := range orders {
		t.Run(fmt.Sprintf("order %d", i), func(t *testing.T) {
			gs, ex := newGroupingGraph(t)
			for _, p := range people {
				props := map[string]storage.Value{"name": storage.StringValue(p.name)}
				if p.age >= 0 {
					props["age"] = storage.IntValue(p.age)
				}
				person(t, gs, props)
			}
			asc := columnOf(runRows(t, ex, `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age`), "n")
			if asc != "Bob,alice,dave,zed" {
				t.Errorf("ASC = %s, want Bob,alice,dave,zed", asc)
			}
			desc := columnOf(runRows(t, ex, `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age DESC`), "n")
			if desc != "zed,dave,alice,Bob" {
				t.Errorf("DESC = %s, want zed,dave,alice,Bob", desc)
			}
		})
	}
}

// TestOrderBy_MixedTypesOnOneProperty: a string, a number, a boolean, a list
// and no value on different nodes, in two input orders.
func TestOrderBy_MixedTypesOnOneProperty(t *testing.T) {
	asc := []string{"['x']", "'s'", "true", "3", "null"}
	for _, in := range [][]any{
		{"s", 3, true, []any{"x"}, nil},
		{nil, []any{"x"}, 3, true, "s"},
		{true, nil, "s", 3, []any{"x"}},
	} {
		assertRendered(t, sortProperty(t, in, ""), asc...)
	}
	assertRendered(t, sortProperty(t, []any{3, nil, "s", true, []any{"x"}}, "DESC"),
		"null", "3", "true", "'s'", "['x']")
}

// TestOrderBy_TwoItemsWithNullInTheFirst: the second item breaks the tie among
// the rows whose first item is null, and null rows are last for ASC.
func TestOrderBy_TwoItemsWithNullInTheFirst(t *testing.T) {
	type row struct {
		a any
		b int
	}
	inputs := [][]row{
		{{nil, 3}, {2, 9}, {nil, 1}, {1, 5}, {nil, 2}},
		{{1, 5}, {nil, 2}, {nil, 3}, {2, 9}, {nil, 1}},
	}
	for i, in := range inputs {
		gs, ex := newGroupingGraph(t)
		for _, r := range in {
			props := map[string]storage.Value{"b": storage.IntValue(int64(r.b))}
			if pv, ok := orderPropertyValue(t, r.a); ok {
				props["a"] = pv
			}
			mustOracleNode(t, gs, "T", props)
		}
		got := columnOf(runRows(t, ex, `MATCH (n:T) RETURN n.b AS b ORDER BY n.a, n.b`), "b")
		if got != "5,9,1,2,3" {
			t.Errorf("input %d: ASC,ASC = %s, want 5,9,1,2,3", i, got)
		}
		got = columnOf(runRows(t, ex, `MATCH (n:T) RETURN n.b AS b ORDER BY n.a DESC, n.b DESC`), "b")
		if got != "3,2,1,9,5" {
			t.Errorf("input %d: DESC,DESC = %s, want 3,2,1,9,5", i, got)
		}
	}
}

// TestMinMax_TCKAggregation2 runs the scenarios of Aggregation2 with the values
// as node properties, because UNWIND of a list literal is not supported.
func TestMinMax_TCKAggregation2(t *testing.T) {
	cases := []struct {
		scenario string
		values   []any
		max, min string
	}{
		{"[1][2] integers", []any{1, 2, 0, nil, -1}, "2", "-1"},
		{"[3][4] floats", []any{1.0, 2.0, 0.5, nil}, "2", "0.5"},
		{"[5][6] mixed numeric", []any{1, 2.0, 5, nil, 3.2, 0.1}, "5", "0.1"},
		{"[7][8] strings", []any{"a", "b", "B", nil, "abc", "abc1"}, "'b'", "'B'"},
		{"[9][10] lists", []any{[]any{1}, []any{2}, []any{2, 1}}, "[2, 1]", "[1]"},
		{"[11][12] mixed", []any{1, "a", nil, []any{1, 2}, 0.2, "b"}, "1", "[1, 2]"},
		// The two examples of the coord task, in both row orders.
		{"coord a,1", []any{"a", 1}, "1", "'a'"},
		{"coord 1,a", []any{1, "a"}, "1", "'a'"},
	}
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			gs, ex := newGroupingGraph(t)
			for _, v := range tc.values {
				props := map[string]storage.Value{}
				if pv, ok := orderPropertyValue(t, v); ok {
					props["v"] = pv
				}
				mustOracleNode(t, gs, "T", props)
			}
			rows := runRows(t, ex, `MATCH (n:T) RETURN MAX(n.v) AS hi, MIN(n.v) AS lo`)
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			if got := renderValue(rows[0]["hi"]); got != tc.max {
				t.Errorf("max = %s, want %s", got, tc.max)
			}
			if got := renderValue(rows[0]["lo"]); got != tc.min {
				t.Errorf("min = %s, want %s", got, tc.min)
			}
		})
	}
}

// TestMinMax_DoNotDependOnRowOrder reverses and rotates the rows of a mixed
// property and requires one answer.
func TestMinMax_DoNotDependOnRowOrder(t *testing.T) {
	base := []any{"a", 1, true, []any{1}, nil, 2.5}
	seen := map[string]bool{}
	permute(base, func(p []any) {
		ac := &AggregationComputer{}
		seen[renderValue(ac.max(p))+" / "+renderValue(ac.min(p))] = true
	})
	if len(seen) != 1 {
		t.Errorf("%d different answers over the permutations of one row set: %v", len(seen), seen)
	}
}
