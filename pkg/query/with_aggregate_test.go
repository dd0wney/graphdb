package query

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// An aggregate inside WITH used to be ignored: executeWithChain projected each
// binding on its own, so WITH n.age AS age, collect(n.id) AS ids gave one row
// per input row and bound ids to a single id. These tests run Cypher text end
// to end, and use the RETURN path (grouped since #664) as the oracle.

var withAggAges = []int64{30, 30, 40, 40, 25, 50}

// withAggCities leaves the last person without a city, for the null group.
var withAggCities = []string{"A", "A", "B", "B", "C", ""}

func newWithAggGraph(t *testing.T) *Executor {
	t.Helper()
	gs, ex := newGroupingGraph(t)
	nodes := make([]*storage.Node, len(withAggAges))
	for i, age := range withAggAges {
		props := map[string]storage.Value{
			"id":  storage.IntValue(int64(i)),
			"age": storage.IntValue(age),
		}
		if withAggCities[i] != "" {
			props["city"] = storage.StringValue(withAggCities[i])
		}
		nodes[i] = person(t, gs, props)
	}
	for _, e := range [][2]int{{0, 1}, {1, 2}, {2, 0}, {0, 3}} {
		if _, err := gs.CreateEdge(nodes[e[0]].ID, nodes[e[1]].ID, "KNOWS", map[string]storage.Value{}, 1); err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
	}
	return ex
}

func sortedInts(t *testing.T, v any) []int {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("value = %#v (%T), want a list", v, v)
	}
	out := make([]int, len(list))
	for i, e := range list {
		out[i] = int(countOf(e))
	}
	sort.Ints(out)
	return out
}

func TestWithAggregate_CollectGroupsByKey(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:Person) WITH p.age AS age, collect(p.id) AS ids RETURN age AS age, ids AS ids")

	if len(rows) != 4 {
		t.Fatalf("rows = %d (%v), want 4", len(rows), rows)
	}
	want := map[int64]string{30: "[0 1]", 40: "[2 3]", 25: "[4]", 50: "[5]"}
	for _, row := range rows {
		age := countOf(row["age"])
		w, ok := want[age]
		if !ok {
			t.Fatalf("unexpected age %v in %v", row["age"], row)
		}
		if got := fmt.Sprint(sortedInts(t, row["ids"])); got != w {
			t.Errorf("age %d: ids = %s, want %s", age, got, w)
		}
	}
}

// RETURN is the oracle: the WITH form must give the same rows.
func TestWithAggregate_MatchesReturnPath(t *testing.T) {
	ex := newWithAggGraph(t)
	for _, agg := range []string{"count", "sum", "min", "max", "avg"} {
		t.Run(agg, func(t *testing.T) {
			viaWith := runRows(t, ex, fmt.Sprintf(
				"MATCH (p:Person) WITH p.age AS age, %s(p.id) AS v RETURN age AS age, v AS v", agg))
			viaReturn := runRows(t, ex, fmt.Sprintf(
				"MATCH (p:Person) RETURN p.age AS age, %s(p.id) AS v", agg))

			if len(viaReturn) != 4 {
				t.Fatalf("oracle gave %d rows, want 4: the oracle is broken", len(viaReturn))
			}
			if a, b := fmt.Sprint(rowStrings(viaWith)), fmt.Sprint(rowStrings(viaReturn)); a != b {
				t.Errorf("WITH rows = %s\nRETURN rows = %s", a, b)
			}
		})
	}
}

func TestWithAggregate_OnlyAggregateIsOneGroup(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:Person) WITH count(p) AS c RETURN c AS c")

	if len(rows) != 1 || countOf(rows[0]["c"]) != 6 {
		t.Fatalf("rows = %v, want one row with c = 6", rows)
	}
}

func TestWithAggregate_NoRowsOnlyAggregateAgreesWithReturn(t *testing.T) {
	ex := newWithAggGraph(t)

	viaWith := runRows(t, ex, "MATCH (p:NoSuchLabel) WITH count(p) AS c RETURN c AS c")
	viaReturn := runRows(t, ex, "MATCH (p:NoSuchLabel) RETURN count(p) AS c")

	if len(viaReturn) != 1 {
		t.Fatalf("oracle gave %v, want one row: the oracle is broken", viaReturn)
	}
	if a, b := fmt.Sprint(rowStrings(viaWith)), fmt.Sprint(rowStrings(viaReturn)); a != b {
		t.Errorf("WITH rows = %s, RETURN rows = %s", a, b)
	}
}

func TestWithAggregate_NoRowsWithKeyGivesNoRows(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:NoSuchLabel) WITH p.age AS age, count(p) AS c RETURN age AS age, c AS c")

	if len(rows) != 0 {
		t.Fatalf("rows = %v, want none", rows)
	}
}

func TestWithAggregate_WhereFiltersGroups(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:Person) WITH p.age AS age, count(p) AS c WHERE c > 1 RETURN age AS age, c AS c")

	if len(rows) != 2 {
		t.Fatalf("rows = %v, want 2 (ages 30 and 40)", rows)
	}
	for _, row := range rows {
		if age := countOf(row["age"]); age != 30 && age != 40 {
			t.Errorf("age %d should have been filtered out", age)
		}
		if countOf(row["c"]) != 2 {
			t.Errorf("row %v: c != 2", row)
		}
	}
}

func TestWithAggregate_WholeNodeKey(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:Person)-[:KNOWS]->(f) WITH p, count(f) AS c RETURN p.id AS id, c AS c")

	got := map[int64]int64{}
	for _, row := range rows {
		got[countOf(row["id"])] = countOf(row["c"])
	}
	want := map[int64]int64{0: 2, 1: 1, 2: 1}
	if fmt.Sprint(got) != fmt.Sprint(want) || len(rows) != 3 {
		t.Fatalf("id->count = %v (%d rows), want %v", got, len(rows), want)
	}
}

func TestWithAggregate_NullKeyIsItsOwnGroup(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:Person) WITH p.city AS city, count(p) AS c RETURN city AS city, c AS c")

	if len(rows) != 4 {
		t.Fatalf("rows = %v, want 4 (A, B, C, null)", rows)
	}
	nulls := 0
	for _, row := range rows {
		if row["city"] != nil {
			continue
		}
		nulls++
		if countOf(row["c"]) != 1 {
			t.Errorf("null group c = %v, want 1", row["c"])
		}
	}
	if nulls != 1 {
		t.Errorf("null groups = %d, want 1", nulls)
	}
}

func TestWithAggregate_GroupsFeedSecondMatch(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex,
		"MATCH (p:Person) WITH p.age AS age, count(p) AS c MATCH (q:Person) WHERE q.age = age RETURN q.id AS id, c AS c")

	if len(rows) != 6 {
		t.Fatalf("rows = %d (%v), want 6", len(rows), rows)
	}
	for _, row := range rows {
		id, c := countOf(row["id"]), countOf(row["c"])
		want := int64(1)
		if id <= 3 {
			want = 2
		}
		if c != want {
			t.Errorf("id %d: c = %d, want %d", id, c, want)
		}
	}
}

// The refusal must not depend on the data, so it also fires for zero rows.
func TestWithAggregate_UnaliasedAggregateIsRefused(t *testing.T) {
	ex := newWithAggGraph(t)
	for _, label := range []string{"Person", "NoSuchLabel"} {
		t.Run(label, func(t *testing.T) {
			_, err := ex.Execute(parseCallInput(t,
				fmt.Sprintf("MATCH (p:%s) WITH p.age AS age, count(p) RETURN age AS age", label)))

			if err == nil {
				t.Fatal("want an error for an aggregate with no alias, got none")
			}
			if msg := err.Error(); !strings.Contains(msg, "WITH") || !strings.Contains(msg, "AS") {
				t.Errorf("error %q should name WITH and tell the caller to add AS <name>", msg)
			}
		})
	}
}

// Same refusal as RETURN: sum of a whole node has no value.
func TestWithAggregate_SumOfNodeIsRefused(t *testing.T) {
	ex := newWithAggGraph(t)

	_, err := ex.Execute(parseCallInput(t, "MATCH (p:Person) WITH p.age AS age, sum(p) AS s RETURN age AS age, s AS s"))

	if err == nil || !strings.Contains(err.Error(), "sum(p)") {
		t.Fatalf("err = %v, want a refusal naming sum(p)", err)
	}
}

// The parser leaves Expression nil for an aggregate over an arithmetic
// argument. The RETURN path then counts rows and collects nothing, which is a
// wrong answer; WITH refuses.
func TestWithAggregate_NonPropertyArgumentIsRefused(t *testing.T) {
	ex := newWithAggGraph(t)

	_, err := ex.Execute(parseCallInput(t, "MATCH (p:Person) WITH p.age AS age, collect(1) AS xs RETURN age AS age, xs AS xs"))

	if err == nil || !strings.Contains(err.Error(), "WITH") {
		t.Fatalf("err = %v, want a refusal that names WITH", err)
	}
}

// Regression: a WITH with no aggregate still projects row by row.
func TestWithAggregate_NoAggregateKeepsRowPerBinding(t *testing.T) {
	ex := newWithAggGraph(t)

	rows := runRows(t, ex, "MATCH (p:Person) WITH p.age AS age RETURN age AS age")

	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6 (no grouping without an aggregate)", len(rows))
	}
}
