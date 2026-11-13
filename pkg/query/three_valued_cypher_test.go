package query

import (
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupThreeValuedGraph builds four Person nodes. id 1 has age 10, id 2 has
// age 30, id 3 has no age, id 4 has the string age "x".
func setupThreeValuedGraph(t *testing.T) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	nodes := []map[string]storage.Value{
		{"id": storage.IntValue(1), "age": storage.IntValue(10)},
		{"id": storage.IntValue(2), "age": storage.IntValue(30)},
		{"id": storage.IntValue(3)},
		{"id": storage.IntValue(4), "age": storage.StringValue("x")},
	}
	for _, props := range nodes {
		if _, err := gs.CreateNode([]string{"Person"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return NewExecutor(gs)
}

func TestThreeValued_Where(t *testing.T) {
	ex := setupThreeValuedGraph(t)
	tests := []struct {
		where string
		want  []int64
	}{
		{"NOT (p.age >= 18)", []int64{1}},
		{"NOT (p.age > 18)", []int64{1}},
		{"NOT (p.age <= 18)", []int64{2}},
		{"NOT (p.age < 18)", []int64{2}},
		{"NOT (NOT (p.age >= 18))", []int64{2}},
		{"NOT (p.age >= 18 AND p.id >= 0)", []int64{1}},
		{"NOT (p.age >= 18 OR p.id < 0)", []int64{1}},
		{"p.age >= 18 OR p.id >= 3", []int64{2, 3, 4}},
		{"p.age >= 18 AND p.id >= 3", []int64{}},
		{"NOT (p.age >= 18 AND p.id < 0)", []int64{1, 2, 3, 4}},
		{"NOT (p.nosuch > 5)", []int64{}},
		// A null operand makes = and <> unknown. The string "x" is not null,
		// so it differs from 1 and node 4 stays.
		{"p.nosuch <> 1", []int64{}},
		{"p.age <> 1", []int64{1, 2, 4}},
		{"p.age IN [10.0]", []int64{1}},
	}
	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			rs := parseAndExecute(t, ex, "MATCH (p:Person) WHERE "+tt.where+" RETURN p.id AS id")
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s: ids = %v, want %v", tt.where, got, tt.want)
			}
		})
	}
}

func TestThreeValued_ReturnComparisonIsNull(t *testing.T) {
	ex := setupThreeValuedGraph(t)
	rs := parseAndExecute(t, ex, "MATCH (p:Person) RETURN p.id AS id, p.age >= 18 AS b ORDER BY p.id")
	want := map[int64]any{1: false, 2: true, 3: nil, 4: nil}
	if len(rs.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rs.Rows))
	}
	for _, row := range rs.Rows {
		id, _ := row["id"].(int64)
		if got := row["b"]; got != want[id] {
			t.Errorf("id %d: b = %#v, want %#v", id, got, want[id])
		}
	}
}

func TestThreeValued_CaseWhenNullDoesNotMatch(t *testing.T) {
	ex := setupThreeValuedGraph(t)
	rs := parseAndExecute(t, ex, "MATCH (p:Person) RETURN CASE p.nosuch WHEN null THEN 'hit' ELSE 'miss' END AS k")
	if len(rs.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rs.Rows))
	}
	for _, row := range rs.Rows {
		if row["k"] != "miss" {
			t.Errorf("k = %v, want miss", row["k"])
		}
	}
}

// The nodes are created out of sorted order, so a sort that did nothing would
// fail these tests.
func orderByGraph(t *testing.T, vals []storage.Value) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	for i, v := range vals {
		props := map[string]storage.Value{"id": storage.IntValue(int64(i)), "v": v}
		if _, err := gs.CreateNode([]string{"Person"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return NewExecutor(gs)
}

func TestOrderBy_NumbersOutOfSequence(t *testing.T) {
	ex := orderByGraph(t, []storage.Value{
		storage.IntValue(30), storage.FloatValue(2.5), storage.IntValue(10), storage.FloatValue(40.5), storage.IntValue(3),
	})
	rs := parseAndExecute(t, ex, "MATCH (p:Person) RETURN p.id AS id ORDER BY p.v")
	if got, want := idsOf(t, rs), []int64{1, 4, 2, 0, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("ORDER BY p.v = %v, want %v", got, want)
	}
	rs = parseAndExecute(t, ex, "MATCH (p:Person) RETURN p.id AS id ORDER BY p.v DESC")
	if got, want := idsOf(t, rs), []int64{3, 0, 2, 4, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("ORDER BY p.v DESC = %v, want %v", got, want)
	}
}

func TestOrderBy_BooleansOutOfSequence(t *testing.T) {
	ex := orderByGraph(t, []storage.Value{
		storage.BoolValue(true), storage.BoolValue(false), storage.BoolValue(true), storage.BoolValue(false),
	})
	rs := parseAndExecute(t, ex, "MATCH (p:Person) RETURN p.id AS id ORDER BY p.v")
	got := idsOf(t, rs)
	if len(got) != 4 || got[0] == 0 || got[0] == 2 || got[1] == 0 || got[1] == 2 {
		t.Errorf("ORDER BY boolean: false rows (ids 1 and 3) must come first, got %v", got)
	}
}
