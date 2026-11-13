package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Every entry point that runs a query must refuse a parameter that the run
// does not hold, in an expression as well as in a pattern. Only the entry
// points with parameters did; the others returned no rows, or a null row, with
// no error.

var missingParamQueries = []struct{ name, text string }{
	{"WHERE", `MATCH (p:Person) WHERE p.id = $missing RETURN p.name AS n`},
	{"RETURN", `RETURN $missing AS x`},
	{"WITH WHERE", `MATCH (p:Person) WITH p WHERE p.id = $missing RETURN p.name AS n`},
	{"SET", `MATCH (p:Person {id: 2}) SET p.z = $missing RETURN p.z AS n`},
}

func wantMissingParam(t *testing.T, err error, name string) {
	t.Helper()
	if err == nil {
		t.Errorf("no error; want a refusal naming $%s", name)
		return
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Errorf("error is %T, want *RefusalError: %v", err, err)
	}
	if !strings.Contains(err.Error(), "$"+name) {
		t.Errorf("error does not name $%s: %v", name, err)
	}
}

func TestEntryPoints_RefuseAMissingParameterInAnExpression(t *testing.T) {
	entries := []struct {
		name string
		run  func(ex *Executor, q *Query, text string) (*ResultSet, error)
	}{
		{"Execute", func(ex *Executor, q *Query, _ string) (*ResultSet, error) { return ex.Execute(q) }},
		{"ExecuteWithContext", func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
			return ex.ExecuteWithContext(context.Background(), q)
		}},
		{"ExecuteWithOptions", func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
			return ex.ExecuteWithOptions(context.Background(), q, PathOptions{})
		}},
		{"ExecuteWithText", func(ex *Executor, q *Query, text string) (*ResultSet, error) {
			return ex.ExecuteWithText(text, q)
		}},
		{"ExecuteWithParams, empty map", func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
			return ex.ExecuteWithParams(q, map[string]any{})
		}},
		{"ExecuteWithParams, other parameter", func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
			return ex.ExecuteWithParams(q, map[string]any{"other": int64(1)})
		}},
		{"ExecuteWithParamsContext, nil map", func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
			return ex.ExecuteWithParamsContext(context.Background(), q, nil)
		}},
	}
	for _, en := range entries {
		for _, qc := range missingParamQueries {
			t.Run(en.name+"/"+qc.name, func(t *testing.T) {
				ex := patternParamsStore(t)
				res, err := en.run(ex, parsePatternQuery(t, qc.text), qc.text)
				if err == nil {
					t.Fatalf("%q returned %v with no error", qc.text, res.Rows)
				}
				wantMissingParam(t, err, "missing")
			})
		}
	}
}

// A refused SET changes nothing.
func TestEntryPoints_RefusedSetWritesNothing(t *testing.T) {
	ex := patternParamsStore(t)
	text := `MATCH (p:Person {id: 2}) SET p.z = $missing RETURN p.z AS n`
	if _, err := ex.Execute(parsePatternQuery(t, text)); err == nil {
		t.Fatal("no error")
	}
	res, err := ex.Execute(parsePatternQuery(t, `MATCH (p:Person {id: 2}) RETURN p.z AS n`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows[0]["n"] != nil {
		t.Errorf("p.z = %v after a refused SET", res.Rows[0]["n"])
	}
}

// A run reads only the parameters that its own entry point set. A nested call
// with the context of an outer parameterized run has none.
func TestEntryPoints_NestedRunDoesNotReadOuterParameters(t *testing.T) {
	ex := patternParamsStore(t)
	outer := withRunParams(context.Background(), map[string]any{"v": int64(1)})
	text := `MATCH (p:Person) WHERE p.id = $v RETURN p.name AS n`

	for name, run := range map[string]func(*Query) (*ResultSet, error){
		"ExecuteWithContext": func(q *Query) (*ResultSet, error) { return ex.ExecuteWithContext(outer, q) },
		"ExecuteWithOptions": func(q *Query) (*ResultSet, error) { return ex.ExecuteWithOptions(outer, q, PathOptions{}) },
	} {
		t.Run(name, func(t *testing.T) {
			res, err := run(parsePatternQuery(t, text))
			if err == nil {
				t.Fatalf("the nested run read the outer $v and returned %v", res.Rows)
			}
			wantMissingParam(t, err, "v")
		})
	}

	// The entry point with parameters replaces the outer values.
	res, err := ex.ExecuteWithParamsContext(outer, parsePatternQuery(t, text), map[string]any{"v": int64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowSignature(res); got != "map[n:Bob]" {
		t.Errorf("rows = %s, want Bob (the inner value, not the outer one)", got)
	}
}

// EXPLAIN builds a plan and needs no value, through every entry point, with
// and without values.
func TestExplain_ReturnsItsPlanThroughEveryEntryPoint(t *testing.T) {
	text := `EXPLAIN MATCH (p:Person {id: $v}) WHERE p.name = $w RETURN p.name AS n`
	both := map[string]any{"v": int64(2), "w": "Bob"}
	entries := map[string]func(ex *Executor, q *Query) (*ResultSet, error){
		"Execute": func(ex *Executor, q *Query) (*ResultSet, error) { return ex.Execute(q) },
		"ExecuteWithContext": func(ex *Executor, q *Query) (*ResultSet, error) {
			return ex.ExecuteWithContext(context.Background(), q)
		},
		"ExecuteWithOptions": func(ex *Executor, q *Query) (*ResultSet, error) {
			return ex.ExecuteWithOptions(context.Background(), q, PathOptions{})
		},
		"ExecuteWithText": func(ex *Executor, q *Query) (*ResultSet, error) { return ex.ExecuteWithText(text, q) },
		"ExecuteWithParams, no values": func(ex *Executor, q *Query) (*ResultSet, error) {
			return ex.ExecuteWithParams(q, map[string]any{})
		},
		"ExecuteWithParams, some values": func(ex *Executor, q *Query) (*ResultSet, error) {
			return ex.ExecuteWithParams(q, map[string]any{"v": int64(2)})
		},
		"ExecuteWithParams, all values": func(ex *Executor, q *Query) (*ResultSet, error) {
			return ex.ExecuteWithParams(q, both)
		},
		"ExecuteWithParamsContext, no values": func(ex *Executor, q *Query) (*ResultSet, error) {
			return ex.ExecuteWithParamsContext(context.Background(), q, nil)
		},
	}
	for name, run := range entries {
		t.Run(name, func(t *testing.T) {
			res, err := run(patternParamsStore(t), parsePatternQuery(t, text))
			if err != nil {
				t.Fatalf("EXPLAIN: %v", err)
			}
			if len(res.Columns) != 2 || res.Columns[0] != "step" || res.Count == 0 {
				t.Errorf("not a plan: columns=%v rows=%v", res.Columns, res.Rows)
			}
		})
	}
}

// ExecuteWithText used to run an EXPLAIN query and return its rows.
func TestExplain_ExecuteWithTextRunsNothing(t *testing.T) {
	ex := patternParamsStore(t)
	text := `EXPLAIN CREATE (p:Made {id: 1})`
	res, err := ex.ExecuteWithText(text, parsePatternQuery(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if res.Columns[0] != "step" {
		t.Errorf("not a plan: %v", res.Columns)
	}
	nodes, _ := ex.graph.GetNodesByLabelForTenant("default", "Made")
	if len(nodes) != 0 {
		t.Errorf("EXPLAIN created %d node(s)", len(nodes))
	}
}
