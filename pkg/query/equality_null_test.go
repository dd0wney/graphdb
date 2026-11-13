package query

import (
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// equalityNullStore holds four T nodes. Node 1 has a=1, s="x", b=true. Node 2
// has a=2, s="y". Node 3 has a=1. Node 4 has none of the three. Every node has
// an id. withIndex puts a property index on a.
func equalityNullStore(t *testing.T, withIndex bool) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	if withIndex {
		if err := gs.CreatePropertyIndex("a", storage.TypeInt); err != nil {
			t.Fatalf("CreatePropertyIndex: %v", err)
		}
	}
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "a": storage.IntValue(1), "s": storage.StringValue("x"), "b": storage.BoolValue(true)},
		{"id": storage.IntValue(2), "a": storage.IntValue(2), "s": storage.StringValue("y")},
		{"id": storage.IntValue(3), "a": storage.IntValue(1)},
		{"id": storage.IntValue(4)},
	} {
		if _, err := gs.CreateNode([]string{"T"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return NewExecutor(gs)
}

// A comparison with a null operand is null, and WHERE removes a null row. Only
// a null operand does this: two values of different types are not equal.
func TestEqualityNull_Where(t *testing.T) {
	tests := []struct {
		where  string
		params map[string]any
		want   []int64
	}{
		{"p.a = 1", nil, []int64{1, 3}},
		{"p.a <> 1", nil, []int64{2}},
		{"p.a != 1", nil, []int64{2}},
		{"p.nosuch = 1", nil, []int64{}},
		{"p.nosuch <> 1", nil, []int64{}},
		{"1 <> p.nosuch", nil, []int64{}},
		{"p.nosuch = null", nil, []int64{}},
		{"p.a = null", nil, []int64{}},
		{"p.a <> null", nil, []int64{}},
		{"null = null", nil, []int64{}},
		{"null <> null", nil, []int64{}},
		{"NOT (p.a = 1)", nil, []int64{2}},
		{"NOT (p.nosuch = 1)", nil, []int64{}},
		{"p.a = 1 OR p.nosuch = 5", nil, []int64{1, 3}},
		{"p.a = 1 AND p.nosuch <> 5", nil, []int64{}},
		{"p.s <> 1", nil, []int64{1, 2}},
		{"p.s = 1", nil, []int64{}},
		{"p.a = $v", map[string]any{"v": nil}, []int64{}},
		{"p.a <> $v", map[string]any{"v": nil}, []int64{}},
		{"p.a = $v", map[string]any{"v": float64(1)}, []int64{1, 3}},
		{"p.a = 1.0", nil, []int64{1, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			ex := equalityNullStore(t, false)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE "+tt.where+" RETURN p.id AS id", tt.params)
			if err != nil {
				t.Fatalf("WHERE %s: %v", tt.where, err)
			}
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s: ids = %v, want %v", tt.where, got, tt.want)
			}
		})
	}
}

// The index path and the scan path must agree on a null operand.
func TestEqualityNull_IndexAndScanAgree(t *testing.T) {
	tests := []struct {
		where  string
		params map[string]any
		want   []int64
	}{
		{"p.a = $v", map[string]any{"v": nil}, []int64{}},
		{"p.a = null", nil, []int64{}},
		{"p.a = 1", nil, []int64{1, 3}},
	}
	for _, withIndex := range []bool{false, true} {
		for _, tt := range tests {
			name := tt.where + "/scan"
			if withIndex {
				name = tt.where + "/index"
			}
			t.Run(name, func(t *testing.T) {
				ex := equalityNullStore(t, withIndex)
				rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE "+tt.where+" RETURN p.id AS id", tt.params)
				if err != nil {
					t.Fatalf("WHERE %s: %v", tt.where, err)
				}
				if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
					t.Errorf("WHERE %s (index=%v): ids = %v, want %v", tt.where, withIndex, got, tt.want)
				}
			})
		}
	}
}

// An optional variable that is null makes the comparison null, in RETURN.
func TestEqualityNull_OptionalMatchVariable(t *testing.T) {
	ex := equalityNullStore(t, false)
	rs, err := runWithParams(t, ex,
		"MATCH (p:T) OPTIONAL MATCH (p)-[:R]->(q) RETURN p.id AS id, q.a = 1 AS eq, q.a <> 1 AS ne", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rs.Rows))
	}
	for _, row := range rs.Rows {
		if row["eq"] != nil || row["ne"] != nil {
			t.Errorf("id %v: eq = %#v, ne = %#v, want null for both", row["id"], row["eq"], row["ne"])
		}
	}
}

func TestEqualityNull_Return(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{"null = null", nil},
		{"null <> null", nil},
		{"1 = null", nil},
		{"null = 1", nil},
		{"p.nosuch <> 1", nil},
		{"p.nosuch = 1", nil},
		{"1 = 1", true},
		{"1 <> 1", false},
		{"'a' = 1", false},
		{"'a' <> 1", true},
		{"1 = 1.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			ex := equalityNullStore(t, false)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE p.id = 4 RETURN "+tt.expr+" AS x", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(rs.Rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rs.Rows))
			}
			if got := rs.Rows[0]["x"]; got != tt.want {
				t.Errorf("RETURN %s = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}
}

// A missing parameter stays an error and is not read as null.
func TestEqualityNull_MissingParameterIsError(t *testing.T) {
	ex := equalityNullStore(t, false)
	if _, err := runWithParams(t, ex, "MATCH (p:T) WHERE p.a = $v RETURN p.id AS id", nil); err == nil {
		t.Fatal("missing parameter: want an error, got none")
	}
}

func TestEqualityNull_SimpleCase(t *testing.T) {
	ex := equalityNullStore(t, false)
	rs, err := runWithParams(t, ex, "MATCH (p:T) RETURN CASE p.nosuch WHEN null THEN 'm' ELSE 'e' END AS k", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rs.Rows {
		if row["k"] != "e" {
			t.Errorf("k = %v, want e", row["k"])
		}
	}
	rs, err = runWithParams(t, ex,
		"MATCH (p:T) RETURN p.id AS id, CASE p.a WHEN 1 THEN 'one' ELSE 'other' END AS k ORDER BY p.id", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one", "other", "one", "other"}
	for i, row := range rs.Rows {
		if row["k"] != want[i] {
			t.Errorf("row %d: k = %v, want %v", i, row["k"], want[i])
		}
	}
}

// SET n.x = (unknown) removes the property (#739).
func TestEqualityNull_SetUnknownRemovesProperty(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	n, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{
		"id": storage.IntValue(1), "a": storage.IntValue(1), "x": storage.StringValue("before"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := NewExecutor(gs)
	if _, err := runWithParams(t, ex, "MATCH (p:T) SET p.x = (p.a = p.nosuch)", nil); err != nil {
		t.Fatal(err)
	}
	got, err := gs.GetNode(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got.Properties["x"]; has {
		t.Errorf("x = %v, want the property removed", got.Properties["x"])
	}
}

// IN and the equality of lists give null where the comparison needs a null, as
// openCypher does (TCK Comparison1 scenarios 6 and 7, List5 scenarios 20 to
// 41). This pins the rows that the earlier = and <> null rule left out.
func TestEqualityNull_InAndListEquality(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{"null IN [1]", nil},
		{"1 IN [null]", nil},
		{"1 IN [1, null]", true},
		{"[1, null] = [1, null]", nil},
		{"[1, 2] = [1, 2]", true},
		{"[1, 2] <> [1, 3]", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			ex := equalityNullStore(t, false)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE p.id = 4 RETURN "+tt.expr+" AS x", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := rs.Rows[0]["x"]; got != tt.want {
				t.Errorf("RETURN %s = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}
}
