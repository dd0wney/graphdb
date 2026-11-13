package query

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A parameter inside a list literal ([1, $v]) used to reach the result as a
// *ParameterRef: the parser keeps the reference in the list and the evaluator
// returned the list as it was. The JSON of the response held {"Name":"v"}, and
// WHERE p.id IN [$v, 2] lost the row with id 1.

var listParamCases = []struct {
	name   string
	lit    string // <ANN> is the id of Ann
	par    string
	params map[string]any
}{
	{"RETURN one element", `RETURN [1, 1] AS l`, `RETURN [1, $v] AS l`, map[string]any{"v": int64(1)}},
	{"RETURN after WITH", `MATCH (a:Person {id: 7}) WITH a RETURN [1, 5] AS l`, `MATCH (a:Person {id: 7}) WITH a RETURN [1, $v] AS l`, map[string]any{"v": int64(5)}},
	{"RETURN in the second UNION part", `MATCH (z:Person {id: 7}) RETURN [0, 0] AS l UNION RETURN [1, 5] AS l`, `MATCH (z:Person {id: 7}) RETURN [0, 0] AS l UNION RETURN [1, $v] AS l`, map[string]any{"v": int64(5)}},
	{"RETURN two parameters", `RETURN [1, "x"] AS l`, `RETURN [$a, $b] AS l`, map[string]any{"a": int64(1), "b": "x"}},
	{"WHERE IN", `MATCH (p:Person) WHERE p.id IN [1, 2] RETURN p.name AS n`, `MATCH (p:Person) WHERE p.id IN [$v, 2] RETURN p.name AS n`, map[string]any{"v": int64(1)}},
	{"WHERE IN, only a parameter", `MATCH (p:Person) WHERE p.id IN [1] RETURN p.name AS n`, `MATCH (p:Person) WHERE p.id IN [$v] RETURN p.name AS n`, map[string]any{"v": int64(1)}},
	{"WHERE IN after WITH", `MATCH (a:Person {id: 7}) WITH a MATCH (p:Person) WHERE p.id IN [1] RETURN p.name AS n`, `MATCH (a:Person {id: 7}) WITH a MATCH (p:Person) WHERE p.id IN [$v] RETURN p.name AS n`, map[string]any{"v": int64(1)}},
	{"WHERE IN in the second UNION part", `MATCH (z:Person {id: 7}) RETURN z.name AS n UNION MATCH (p:Person) WHERE p.id IN [1] RETURN p.name AS n`, `MATCH (z:Person {id: 7}) RETURN z.name AS n UNION MATCH (p:Person) WHERE p.id IN [$v] RETURN p.name AS n`, map[string]any{"v": int64(1)}},
	{"WITH WHERE IN", `MATCH (p:Person) WITH p WHERE p.id IN [1, 2] RETURN p.name AS n`, `MATCH (p:Person) WITH p WHERE p.id IN [$v, 2] RETURN p.name AS n`, map[string]any{"v": int64(1)}},
	{"SET a list", `MATCH (p:Person {id: 2}) SET p.tags = ["a", "x"] RETURN p.tags AS n`, `MATCH (p:Person {id: 2}) SET p.tags = [$a, "x"] RETURN p.tags AS n`, map[string]any{"a": "a"}},
	{"CALL argument", `CALL algo.kHop(<ANN>, 1, "out", ["KNOWS"]) YIELD totalReachable RETURN totalReachable AS n`, `CALL algo.kHop(<ANN>, 1, "out", [$t]) YIELD totalReachable RETURN totalReachable AS n`, map[string]any{"t": "KNOWS"}},
}

func TestListParams_MatchTheLiteralForm(t *testing.T) {
	for _, tc := range listParamCases {
		t.Run(tc.name, func(t *testing.T) {
			exLit, ann := segmentStore(t)
			litText := strings.ReplaceAll(tc.lit, "<ANN>", fmt.Sprint(ann))
			litRes, err := exLit.Execute(parsePatternQuery(t, litText))
			if err != nil {
				t.Fatalf("the literal form has no baseline: %q: %v", litText, err)
			}
			if litRes.Count == 0 {
				t.Fatalf("%q returned no rows", litText)
			}

			exPar, ann := segmentStore(t)
			parText := strings.ReplaceAll(tc.par, "<ANN>", fmt.Sprint(ann))
			parRes, err := exPar.ExecuteWithParams(parsePatternQuery(t, parText), tc.params)
			if err != nil {
				t.Fatalf("ExecuteWithParams %q: %v", parText, err)
			}
			for _, row := range parRes.Rows {
				for col, v := range row {
					if containsParameterRef(v) {
						t.Errorf("column %s holds a *ParameterRef: %#v", col, v)
					}
				}
			}
			if got, want := rowSignature(parRes), rowSignature(litRes); got != want {
				t.Errorf("%q\n  parameter rows: %s\n  literal rows:   %s", parText, got, want)
			}
		})
	}
}

// A missing parameter inside a list is the same refusal, in each case.
func TestListParams_MissingParameterIsRefused(t *testing.T) {
	for _, tc := range listParamCases {
		t.Run(tc.name, func(t *testing.T) {
			ex, ann := segmentStore(t)
			text := strings.ReplaceAll(tc.par, "<ANN>", fmt.Sprint(ann))
			_, err := ex.ExecuteWithParams(parsePatternQuery(t, text), map[string]any{})
			// The refusal names the first missing parameter in the text. A map
			// has no fixed order, so the name is found by position: picking the
			// first key of tc.params made this test fail on some runs.
			var name string
			first := len(text)
			for k := range tc.params {
				if at := strings.Index(text, "$"+k); at >= 0 && at < first {
					name, first = k, at
				}
			}
			wantMissingParam(t, err, name)

			// And through an entry point with no parameters.
			_, err = ex.Execute(parsePatternQuery(t, text))
			wantMissingParam(t, err, name)
		})
	}
}

// The parsed query stays read only: the list in the AST still holds the
// reference after a run, and a second run with another value gives the second
// value.
func TestListParams_ParsedQueryIsNotWritten(t *testing.T) {
	ex := patternParamsStore(t)
	q := parsePatternQuery(t, `RETURN [1, $v] AS l`)
	for _, v := range []int64{5, 9} {
		res, err := ex.ExecuteWithParams(q, map[string]any{"v": v})
		if err != nil {
			t.Fatal(err)
		}
		want := []any{int64(1), v}
		if got := res.Rows[0]["l"]; !reflect.DeepEqual(got, want) {
			t.Errorf("v=%d: l = %#v, want %#v", v, got, want)
		}
	}
	lit := q.Return.Items[0].ValueExpr.(*LiteralExpression)
	if !containsParameterRef(lit.Value) {
		t.Errorf("the run wrote into the parsed query: %#v", lit.Value)
	}
}
