package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// booleanOperandStore holds the four nodes the boolean-operand rule is tested
// on. Node 4 carries none of the property names, so its operands are unknown.
func booleanOperandStore(t *testing.T) *Executor {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "a": storage.BoolValue(true), "b": storage.BoolValue(true), "c": storage.BoolValue(true), "s": storage.StringValue("x"), "z": storage.IntValue(0)},
		{"id": storage.IntValue(2), "a": storage.BoolValue(false), "b": storage.BoolValue(true), "s": storage.StringValue(""), "z": storage.IntValue(5)},
		{"id": storage.IntValue(3), "b": storage.BoolValue(false), "c": storage.BoolValue(false)},
		{"id": storage.IntValue(4)},
	} {
		if _, err := gs.CreateNode([]string{"T"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return NewExecutor(gs)
}

func runWithParams(t *testing.T, ex *Executor, text string, params map[string]any) (*ResultSet, error) {
	t.Helper()
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize %q: %v", text, err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse %q: %v", text, err)
	}
	return ex.ExecuteWithParams(q, params)
}

func TestBooleanOperand_NonBooleanIsRefused(t *testing.T) {
	ex := booleanOperandStore(t)
	cases := []struct {
		name, text, typeWord string
		params               map[string]any
	}{
		{"string AND", `MATCH (p) WHERE p.s AND true RETURN p.id AS id`, "string", nil},
		{"integer OR", `MATCH (p) WHERE p.z OR false RETURN p.id AS id`, "integer", nil},
		{"node AND", `MATCH (p) WHERE p AND true RETURN p.id AS id`, "node", nil},
		{"literal AND", `MATCH (p) WHERE 1 AND true RETURN p.id AS id`, "integer", nil},
		{"NOT string", `MATCH (p) WHERE NOT p.s RETURN p.id AS id`, "string", nil},
		{"bare string", `MATCH (p) WHERE p.s RETURN p.id AS id`, "string", nil},
		{"parameter", `MATCH (p) WHERE $f AND p.b RETURN p.id AS id`, "string", map[string]any{"f": "no"}},
		{"RETURN", `MATCH (p) RETURN p.s AND true AS v`, "string", nil},
		{"CASE WHEN", `MATCH (p) RETURN CASE WHEN p.s THEN 1 ELSE 0 END AS v`, "string", nil},
		{"WITH WHERE", `MATCH (p) WITH p.s AS s WHERE s RETURN s`, "string", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := runWithParams(t, ex, tc.text, tc.params)
			if err == nil {
				t.Fatalf("want a RefusalError, got rows=%v", rs.Rows)
			}
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("error %q is %T, want *RefusalError", err, err)
			}
			if !strings.Contains(err.Error(), "boolean") || !strings.Contains(err.Error(), tc.typeWord) {
				t.Errorf("message %q must name a boolean and %q", err, tc.typeWord)
			}
			for _, leak := range []string{"secret", "float64", "int64", "storage."} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("message %q leaks %q", err, leak)
				}
			}
		})
	}
}

func TestBooleanOperand_BareBooleanInEachPosition(t *testing.T) {
	ex := booleanOperandStore(t)
	cases := []struct {
		where  string
		params map[string]any
		want   []int64
	}{
		{"p.b", nil, []int64{1, 2}},
		{"NOT p.b", nil, []int64{3}},
		{"NOT NOT p.b", nil, []int64{1, 2}},
		{"NOT (p.b OR p.c)", nil, []int64{3}},
		{"$t AND p.b", map[string]any{"t": true}, []int64{1, 2}},
		{"true", nil, []int64{1, 2, 3, 4}},
		{"false", nil, []int64{}},
		{"null", nil, []int64{}},
		{"NOT (CASE WHEN p.id = 1 THEN null ELSE false END)", nil, []int64{2, 3, 4}},
		{"NOT toBoolean(p.s)", nil, []int64{}},
	}
	for _, tc := range cases {
		t.Run(tc.where, func(t *testing.T) {
			rs, err := runWithParams(t, ex, "MATCH (p) WHERE "+tc.where+" RETURN p.id AS id", tc.params)
			if err != nil {
				t.Fatalf("WHERE %s: %v", tc.where, err)
			}
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("WHERE %s: ids = %v, want %v", tc.where, got, tc.want)
			}
		})
	}

	t.Run("WITH WHERE", func(t *testing.T) {
		rs, err := runWithParams(t, ex, `MATCH (p) WITH p.b AS b, p.id AS id WHERE b RETURN id AS id`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := sortedIDs(t, rs); !reflect.DeepEqual(got, []int64{1, 2}) {
			t.Errorf("ids = %v, want [1 2]", got)
		}
	})

	t.Run("CASE WHEN bare boolean", func(t *testing.T) {
		rs, err := runWithParams(t, ex, `MATCH (p) RETURN p.id AS id, CASE WHEN p.b THEN 'y' ELSE 'n' END AS v`, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := map[int64]any{}
		for _, row := range rs.Rows {
			got[row["id"].(int64)] = row["v"]
		}
		want := map[int64]any{1: "y", 2: "y", 3: "n", 4: "n"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("CASE = %v, want %v", got, want)
		}
	})
}

// toBoolean gives null for a string that is not "true" or "false", so NOT of
// it is null and the row goes. Only "false" survives.
func TestBooleanOperand_NotOfNullFunctionResult(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	for i, s := range []string{"false", "true", "maybe"} {
		if _, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i + 1)), "s": storage.StringValue(s),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{"id": storage.IntValue(4)}); err != nil {
		t.Fatal(err)
	}
	rs, err := runWithParams(t, NewExecutor(gs), `MATCH (p) WHERE NOT toBoolean(p.s) RETURN p.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedIDs(t, rs); !reflect.DeepEqual(got, []int64{1}) {
		t.Errorf("ids = %v, want [1]", got)
	}
}

func TestBooleanOperand_OptionalMatchWhere(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	mk := func(id int64, props map[string]storage.Value) uint64 {
		props["id"] = storage.IntValue(id)
		n, err := gs.CreateNode([]string{"T"}, props)
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	a := mk(10, map[string]storage.Value{})
	b := mk(11, map[string]storage.Value{"b": storage.BoolValue(true)})
	c := mk(12, map[string]storage.Value{"b": storage.BoolValue(false)})
	for _, to := range []uint64{b, c} {
		if _, err := gs.CreateEdge(a, to, "KNOWS", nil, 1); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := runWithParams(t, NewExecutor(gs),
		`MATCH (a:T {id: 10}) OPTIONAL MATCH (a)-[:KNOWS]->(q) WHERE q.b RETURN q.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedIDs(t, rs); !reflect.DeepEqual(got, []int64{11}) {
		t.Errorf("ids = %v, want [11]", got)
	}
}
