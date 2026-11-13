package query

import (
	"math"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// POST /query decodes JSON without UseNumber, so the number 7 reaches the
// executor as float64(7). An inline pattern map {id: $v} must then match the
// stored integer 7 by the same rule WHERE p.id = $v uses.

func numericMatchStore(t *testing.T, id storage.Value) (*Executor, *storage.GraphStorage) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	if _, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{"id": id}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	return NewExecutor(gs), gs
}

func rowCount(t *testing.T, ex *Executor, q string, params map[string]any) int {
	t.Helper()
	return parseAndExecuteWithParams(t, ex, q, params).Count
}

func TestPatternMap_FloatParamMatchesStoredInt(t *testing.T) {
	ex, _ := numericMatchStore(t, storage.IntValue(7))
	if got := rowCount(t, ex, `MATCH (p:Person {id: $v}) RETURN p`, map[string]any{"v": float64(7)}); got != 1 {
		t.Errorf("float64(7) against stored int 7: rows = %d, want 1", got)
	}
}

func TestPatternMap_IntParamMatchesStoredFloat(t *testing.T) {
	ex, _ := numericMatchStore(t, storage.FloatValue(7.0))
	if got := rowCount(t, ex, `MATCH (p:Person {id: $v}) RETURN p`, map[string]any{"v": int64(7)}); got != 1 {
		t.Errorf("int64(7) against stored float 7.0: rows = %d, want 1", got)
	}
}

func TestPatternMap_OtherGoIntegerKinds(t *testing.T) {
	ex, _ := numericMatchStore(t, storage.IntValue(7))
	exF, _ := numericMatchStore(t, storage.FloatValue(7.0))
	for name, v := range map[string]any{"int": int(7), "int32": int32(7), "uint8": uint8(7)} {
		if got := rowCount(t, ex, `MATCH (p:Person {id: $v}) RETURN p`, map[string]any{"v": v}); got != 1 {
			t.Errorf("%s(7) against stored int 7: rows = %d, want 1", name, got)
		}
		if got := rowCount(t, exF, `MATCH (p:Person {id: $v}) RETURN p`, map[string]any{"v": v}); got != 1 {
			t.Errorf("%s(7) against stored float 7.0: rows = %d, want 1", name, got)
		}
	}
}

func TestPatternMap_NumericDoesNotMatchOtherTypes(t *testing.T) {
	cases := map[string]storage.Value{
		"string 7":  storage.StringValue("7"),
		"bool true": storage.BoolValue(true),
		// extractStorageValue yields Unix seconds for a timestamp, which is
		// a number; the pattern must still not match it.
		"timestamp": storage.TimestampValue(time.Unix(7, 0)),
	}
	for name, stored := range cases {
		ex, _ := numericMatchStore(t, stored)
		for _, v := range []any{float64(7), float64(1), int64(7), int64(1)} {
			if got := rowCount(t, ex, `MATCH (p:Person {id: $v}) RETURN p`, map[string]any{"v": v}); got != 0 {
				t.Errorf("%T(%v) matched stored %s: rows = %d, want 0", v, v, name, got)
			}
		}
	}
}

func TestPatternMap_NonIntegralAndNaNMatchNothing(t *testing.T) {
	ex, _ := numericMatchStore(t, storage.IntValue(7))
	exF, _ := numericMatchStore(t, storage.FloatValue(7.0))
	for name, v := range map[string]float64{"7.5": 7.5, "NaN": math.NaN()} {
		for _, e := range []*Executor{ex, exF} {
			if got := rowCount(t, e, `MATCH (p:Person {id: $v}) RETURN p`, map[string]any{"v": v}); got != 0 {
				t.Errorf("%s matched: rows = %d, want 0", name, got)
			}
		}
	}
}

func TestPatternMap_Merge_FloatParamDoesNotDuplicate(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	if _, err := gs.CreateNode([]string{"M"}, map[string]storage.Value{"id": storage.IntValue(7)}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	ex := NewExecutor(gs)
	for i := 1; i <= 2; i++ {
		parseAndExecuteWithParams(t, ex, `MERGE (p:M {id: $v})`, map[string]any{"v": float64(7)})
		nodes, err := gs.GetNodesByLabelForTenant("default", "M")
		if err != nil {
			t.Fatalf("GetNodesByLabel: %v", err)
		}
		if len(nodes) != 1 {
			t.Fatalf("after MERGE call %d: M nodes = %d, want 1", i, len(nodes))
		}
	}
}

func TestPatternMap_Merge_OtherGoKinds(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	if _, err := gs.CreateNode([]string{"M"}, map[string]storage.Value{"id": storage.IntValue(7)}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	ex := NewExecutor(gs)
	for name, v := range map[string]any{"int": int(7), "int32": int32(7), "int64": int64(7)} {
		parseAndExecuteWithParams(t, ex, `MERGE (p:M {id: $v})`, map[string]any{"v": v})
		nodes, _ := gs.GetNodesByLabelForTenant("default", "M")
		if len(nodes) != 1 {
			t.Fatalf("MERGE with %s(7): M nodes = %d, want 1", name, len(nodes))
		}
	}
}

// resolveParameters does not substitute parameters inside OPTIONAL MATCH
// patterns (a separate defect, recorded in the report), so this test resolves
// them by hand and runs the pattern comparison itself.
func TestPatternMap_OptionalMatch_FloatValueBinds(t *testing.T) {
	ex, _ := numericMatchStore(t, storage.IntValue(7))
	lexer := NewLexer(`MATCH (x:Person) OPTIONAL MATCH (p:Person {id: $v}) RETURN p.id AS pid`)
	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, om := range q.OptionalMatches {
		for _, pat := range om.Patterns {
			if err := resolvePatternParams(pat, map[string]any{"v": float64(7)}); err != nil {
				t.Fatalf("resolve: %v", err)
			}
		}
	}
	res, err := ex.Execute(q)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("rows = %d, want 1", res.Count)
	}
	for _, v := range res.Rows[0] {
		if v == nil {
			t.Errorf("OPTIONAL MATCH bound null for float64(7) against stored int 7: %+v", res.Rows[0])
		}
	}
}

func TestPatternMap_PathPattern_FloatParams(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	a, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{"id": storage.IntValue(1)})
	b, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{"id": storage.IntValue(2)})
	if _, err := gs.CreateEdge(a.ID, b.ID, "KNOWS", map[string]storage.Value{"w": storage.IntValue(3)}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	ex := NewExecutor(gs)
	params := map[string]any{"a": float64(1), "b": float64(2)}
	if got := rowCount(t, ex, `MATCH (a:Person {id: $a})-[:KNOWS]->(b:Person {id: $b}) RETURN a, b`, params); got != 1 {
		t.Errorf("path pattern rows = %d, want 1", got)
	}
	// The relationship property map goes through the same comparison.
	if got := rowCount(t, ex, `MATCH (a:Person)-[r:KNOWS {w: $w}]->(b:Person) RETURN a`, map[string]any{"w": float64(3)}); got != 1 {
		t.Errorf("relationship property map rows = %d, want 1", got)
	}
}

// The pattern map and WHERE must agree on a pair that float64 cannot tell
// apart: 2^53+1 stored as an integer, 2^53 as a float parameter.
func TestPatternMap_LargeIntegerAgreesWithWhere(t *testing.T) {
	ex, _ := numericMatchStore(t, storage.IntValue(9007199254740993))
	params := map[string]any{"v": float64(9007199254740992)}
	pattern := rowCount(t, ex, `MATCH (p:Person {id: $v}) RETURN p`, params)
	where := rowCount(t, ex, `MATCH (p:Person) WHERE p.id = $v RETURN p`, params)
	t.Logf("pattern rows = %d, WHERE rows = %d", pattern, where)
	if pattern != where {
		t.Errorf("pattern map rows = %d but WHERE rows = %d for the same pair", pattern, where)
	}
}

func TestCompareStorageValue_IntegerAndFloat(t *testing.T) {
	cases := []struct {
		name string
		a, b storage.Value
		want bool
	}{
		{"int 7 vs float 7.0", storage.IntValue(7), storage.FloatValue(7.0), true},
		{"float 7.0 vs int 7", storage.FloatValue(7.0), storage.IntValue(7), true},
		{"int 7 vs float 7.5", storage.IntValue(7), storage.FloatValue(7.5), false},
		{"int 7 vs int 7", storage.IntValue(7), storage.IntValue(7), true},
		{"int 7 vs string 7", storage.IntValue(7), storage.StringValue("7"), false},
		{"string a vs string a", storage.StringValue("a"), storage.StringValue("a"), true},
		{"int 7 vs timestamp 7", storage.IntValue(7), storage.TimestampValue(time.Unix(7, 0)), false},
	}
	for _, c := range cases {
		if got := compareStorageValue(c.a, c.b); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
