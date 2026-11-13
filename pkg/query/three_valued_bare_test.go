package query

import (
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Bare operands: a property, a variable, a parameter or a literal used as a
// boolean. null is unknown and a boolean is its value.
func TestThreeValued_BareBooleanOperands(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "b": storage.BoolValue(true), "c": storage.BoolValue(true)},
		{"id": storage.IntValue(2), "b": storage.BoolValue(true), "c": storage.BoolValue(false)},
		{"id": storage.IntValue(3), "b": storage.BoolValue(true)},
		{"id": storage.IntValue(4), "b": storage.BoolValue(false)},
	} {
		if _, err := gs.CreateNode([]string{"Person"}, props); err != nil {
			t.Fatal(err)
		}
	}
	ex := NewExecutor(gs)
	tests := []struct {
		where string
		want  []int64
	}{
		{"p.b AND p.c", []int64{1}},
		{"p.b OR p.c", []int64{1, 2, 3}},
		// id 3 is true AND unknown = unknown. id 4 is false AND unknown = false.
		{"NOT (p.b AND p.c)", []int64{2, 4}},
		// id 4 is false OR unknown = unknown.
		{"NOT (p.b OR p.c)", []int64{}},
		{"NOT p.b", []int64{4}},
		{"NOT p.c", []int64{2}},
		{"p.b AND NOT p.c", []int64{2}},
		{"p.b AND p.id >= 2", []int64{2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			rs := parseAndExecute(t, ex, "MATCH (p:Person) WHERE "+tt.where+" RETURN p.id AS id")
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s: ids = %v, want %v", tt.where, got, tt.want)
			}
		})
	}

	// A bare boolean alone in WHERE selects the rows where it is true. The
	// test expected no rows here: Eval of a bare property was always an error
	// and the lenient filter dropped each row. That was a defect.
	t.Run("bare p.b alone", func(t *testing.T) {
		rs := parseAndExecute(t, ex, "MATCH (p:Person) WHERE p.b RETURN p.id AS id")
		if got := sortedIDs(t, rs); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
			t.Errorf("WHERE p.b alone: ids = %v, want [1 2 3]", got)
		}
	})
}
