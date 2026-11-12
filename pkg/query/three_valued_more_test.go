package query

import (
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func expiresGraph(t *testing.T) (*storage.GraphStorage, *Executor, []uint64) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	var ids []uint64
	for _, props := range []map[string]storage.Value{
		{"k": storage.IntValue(1)},
		{"k": storage.IntValue(2), "expires": storage.IntValue(50)},
		{"k": storage.IntValue(3), "expires": storage.IntValue(150)},
	} {
		n, err := gs.CreateNode([]string{"T"}, props)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	return gs, NewExecutor(gs), ids
}

func assertSurvivors(t *testing.T, gs *storage.GraphStorage, ids []uint64, want []bool) {
	t.Helper()
	for i, id := range ids {
		_, err := gs.GetNode(id)
		if exists := err == nil; exists != want[i] {
			t.Errorf("node %d (k=%d) exists = %v, want %v", id, i+1, exists, want[i])
		}
	}
}

func TestThreeValued_DetachDeleteNotOfNullVariable(t *testing.T) {
	gs, ex, ids := expiresGraph(t)
	parseAndExecute(t, ex, "MATCH (n:T) WITH n, n.expires >= 100 AS live WHERE NOT live DETACH DELETE n")
	assertSurvivors(t, gs, ids, []bool{true, false, true})
}

func TestThreeValued_DetachDeleteNotOfEqualsOfUnknown(t *testing.T) {
	gs, ex, ids := expiresGraph(t)
	parseAndExecute(t, ex, "MATCH (n:T) WHERE NOT ((n.expires >= 100) = true) DETACH DELETE n")
	assertSurvivors(t, gs, ids, []bool{true, false, true})
}

func TestThreeValued_NotOfNullValue(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "flag": storage.BoolValue(true), "b": storage.BoolValue(true), "c": storage.BoolValue(true)},
		{"id": storage.IntValue(2), "flag": storage.BoolValue(false)},
		{"id": storage.IntValue(3)},
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
		{"NOT p.nosuch", []int64{}},
		{"NOT p.flag", []int64{2}},
		{"NOT (p.b AND p.c)", []int64{}},
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

func TestThreeValued_EqualityOverUnknownComparison(t *testing.T) {
	ex := setupThreeValuedGraph(t)
	tests := []struct {
		where string
		want  []int64
	}{
		{"(p.age >= 18) <> true", []int64{1}},
		{"(p.age >= 18) = true", []int64{2}},
		{"(p.age >= 18) = false", []int64{1}},
		{"NOT ((p.age >= 18) = true)", []int64{1}},
		{"p.age >= 18 OR p.id < 0", []int64{2}},
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
