package query

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A parameter in an expression ($v in WHERE, RETURN, SET, ORDER BY or a CALL
// argument) must be readable in every segment of a WITH chain and in every
// part of a UNION. Before v1.4 only the first segment had the "$v" binding, so
// a later segment read null: the query returned no rows or partial rows.

// segmentPositions: <V> is the value (a literal or $v) and <ANN> the id of the
// node Ann. Each query returns one column, n.
var segmentPositions = []struct {
	name  string
	query string
	lit   string // the literal that <V> takes in the literal form
	param any
	// noValue marks a position whose value the executor never reads, so a
	// row comparison would pass with or without the fix.
	noValue string
}{
	{"WHERE", `MATCH (b:Person) WHERE b.id = <V> RETURN b.name AS n`, "1", int64(1), ""},
	{"RETURN", `MATCH (b:Person {id: 2}) RETURN b.id + <V> AS n`, "10", int64(10), ""},
	{"ORDER BY", `MATCH (b:Person) RETURN b.name AS n ORDER BY b.id * <V>`, "-1", int64(-1),
		"the executor ignores an ORDER BY expression that is not a bare property (orderByColumnName returns \"<expr>\"), so no row ever depends on its value"},
	{"SET", `MATCH (b:Person {id: 2}) SET b.score = <V> RETURN b.score AS n`, "5", int64(5), ""},
	{"CALL", `CALL algo.kHop(<ANN>, <V>) YIELD totalReachable RETURN totalReachable AS n`, "1", int64(1), ""},
}

// segmentContexts wrap a position's query. S is the position's query text.
var segmentContexts = []struct{ name, prefix string }{
	{"first segment", ``},
	{"after one WITH", `MATCH (a:Person {id: 1}) WITH a `},
	{"after two WITH", `MATCH (a:Person {id: 1}) WITH a MATCH (c:Person {id: 7}) WITH a, c `},
	{"second UNION part", `MATCH (z:Person {id: 7}) RETURN z.name AS n UNION `},
	{"third UNION ALL part", `MATCH (z:Person {id: 7}) RETURN z.name AS n UNION ALL MATCH (y:Person {id: 1}) RETURN y.name AS n UNION ALL `},
}

func segmentStore(t *testing.T) (*Executor, uint64) {
	t.Helper()
	ex := patternParamsStore(t)
	nodes, err := ex.graph.GetNodesByLabelForTenant("default", "Person")
	if err != nil {
		t.Fatalf("GetNodesByLabelForTenant: %v", err)
	}
	for _, n := range nodes {
		if v, ok := n.Properties["name"]; ok {
			if s, _ := v.AsString(); s == "Ann" {
				return ex, n.ID
			}
		}
	}
	t.Fatal("Ann not found")
	return nil, 0
}

func renderSegment(pos, prefix, value string, ann uint64) string {
	q := strings.NewReplacer("<V>", value, "<ANN>", fmt.Sprint(ann)).Replace(pos)
	return prefix + q
}

func TestSegmentParams_EachPositionInEachSegmentMatchesLiteral(t *testing.T) {
	for _, pos := range segmentPositions {
		for _, ctx := range segmentContexts {
			t.Run(pos.name+"/"+ctx.name, func(t *testing.T) {
				if pos.noValue != "" {
					t.Skip(pos.noValue)
				}
				exLit, ann := segmentStore(t)
				litText := renderSegment(pos.query, ctx.prefix, pos.lit, ann)
				litRes, err := exLit.Execute(parsePatternQuery(t, litText))
				if err != nil {
					t.Fatalf("the literal form has no baseline: %q: %v", litText, err)
				}
				if litRes.Count == 0 {
					t.Fatalf("%q returned no rows; the comparison would prove nothing", litText)
				}

				exPar, ann := segmentStore(t)
				parText := renderSegment(pos.query, ctx.prefix, "$v", ann)
				parRes, err := exPar.ExecuteWithParams(parsePatternQuery(t, parText), map[string]any{"v": pos.param})
				if err != nil {
					t.Fatalf("ExecuteWithParams %q: %v", parText, err)
				}
				if got, want := rowSignature(parRes), rowSignature(litRes); got != want {
					t.Errorf("%q\n  parameter rows: %s\n  literal rows:   %s  (%q)", parText, got, want, litText)
				}
			})
		}
	}
}

// WITH ... WHERE x = $v reads the parameter in the WITH itself and in the
// segment that follows.
func TestSegmentParams_WithWhere(t *testing.T) {
	for _, tc := range []struct{ name, lit, par string }{
		{"WITH WHERE", `MATCH (a:Person) WITH a WHERE a.id = 2 MATCH (b:Person {id: 1}) RETURN a.name AS n`,
			`MATCH (a:Person) WITH a WHERE a.id = $v MATCH (b:Person {id: 1}) RETURN a.name AS n`},
		{"WITH WHERE then WHERE", `MATCH (a:Person) WITH a WHERE a.id = 2 MATCH (b:Person) WHERE b.id = 7 RETURN a.name AS n`,
			`MATCH (a:Person) WITH a WHERE a.id = $v MATCH (b:Person) WHERE b.id = 7 RETURN a.name AS n`},
		{"WITH WHERE, parameter after", `MATCH (a:Person) WITH a WHERE a.id = 2 MATCH (b:Person) WHERE b.id = 2 RETURN a.name AS n`,
			`MATCH (a:Person) WITH a WHERE a.id = $v MATCH (b:Person) WHERE b.id = $v RETURN a.name AS n`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exLit, _ := segmentStore(t)
			litRes, err := exLit.Execute(parsePatternQuery(t, tc.lit))
			if err != nil || litRes.Count == 0 {
				t.Fatalf("literal baseline: rows=%v err=%v", litRes, err)
			}
			exPar, _ := segmentStore(t)
			parRes, err := exPar.ExecuteWithParams(parsePatternQuery(t, tc.par), map[string]any{"v": int64(2)})
			if err != nil {
				t.Fatalf("ExecuteWithParams: %v", err)
			}
			if got, want := rowSignature(parRes), rowSignature(litRes); got != want {
				t.Errorf("parameter rows: %s, literal rows: %s", got, want)
			}
		})
	}
}

// A parameter that the caller did not supply is an error that names it, in
// every position of every segment, and it is a RefusalError (HTTP 400).
func TestSegmentParams_MissingParameterNamesIt(t *testing.T) {
	cases := []struct{ name, text string }{
		{"WITH WHERE", `MATCH (a:Person) WITH a WHERE a.id = $v RETURN a.name AS n`},
	}
	for _, pos := range segmentPositions {
		for _, ctx := range segmentContexts {
			cases = append(cases, struct{ name, text string }{
				pos.name + "/" + ctx.name, renderSegment(pos.query, ctx.prefix, "$v", 1)})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ex, _ := segmentStore(t)
			_, err := ex.ExecuteWithParams(parsePatternQuery(t, tc.text), map[string]any{})
			if err == nil {
				t.Fatalf("%q ran with no parameter v and returned no error", tc.text)
			}
			if !strings.Contains(err.Error(), "$v") {
				t.Errorf("error does not name the parameter: %v", err)
			}
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Errorf("error is %T, want *RefusalError so a server answers 400: %v", err, err)
			}
		})
	}
}

// PROFILE with the parameter supplied reads the parameter in its expressions.
func TestProfile_ReadsParameters(t *testing.T) {
	res, err := patternParamsStore(t).ExecuteWithParams(parsePatternQuery(t,
		`PROFILE MATCH (p:Person) WHERE p.id = $v RETURN p.name AS n`), map[string]any{"v": int64(2)})
	if err != nil {
		t.Fatalf("PROFILE: %v", err)
	}
	if got := rowSignature(res); got != "map[n:Bob]" {
		t.Errorf("rows = %s, want Bob", got)
	}
}

// One RefusalError type for a missing parameter, whichever clause holds it.
func TestMissingParameter_IsARefusalEverywhere(t *testing.T) {
	for _, text := range []string{
		`MATCH (p:Person {id: $v}) RETURN p.name AS n`,
		`MATCH (p:Person) WHERE p.id = $v RETURN p.name AS n`,
		`MATCH (p:Person) RETURN p.name AS n, p.id + $v AS m`,
	} {
		_, err := patternParamsStore(t).ExecuteWithParams(parsePatternQuery(t, text), map[string]any{})
		var refusal *RefusalError
		if !errors.As(err, &refusal) {
			t.Errorf("%q: error is %T, want *RefusalError: %v", text, err, err)
		}
	}
}
