package query

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A parameter in a pattern property map ({id: $v}) must be resolved wherever
// the grammar lets a pattern appear: each clause, each segment of a WITH
// chain, and each part of a UNION. Until v1.4 resolveParameters handled
// MATCH, CREATE and MERGE only, so an OPTIONAL MATCH kept its *ParameterRef,
// compared a stored value with it, never matched, and bound null with no error.

// patternParamsStore holds Ann (id 1), Bob (id 2) and Gus (id 7), and one
// edge Ann -[KNOWS {w: 3}]-> Bob.
func patternParamsStore(t *testing.T) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	ids := map[string]uint64{}
	for _, p := range []struct {
		name string
		id   int64
	}{{"Ann", 1}, {"Bob", 2}, {"Gus", 7}} {
		n, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"id":   storage.IntValue(p.id),
			"name": storage.StringValue(p.name),
		})
		if err != nil {
			t.Fatalf("CreateNode %s: %v", p.name, err)
		}
		ids[p.name] = n.ID
	}
	if _, err := gs.CreateEdge(ids["Ann"], ids["Bob"], "KNOWS",
		map[string]storage.Value{"w": storage.IntValue(3)}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	return NewExecutor(gs)
}

func parsePatternQuery(t *testing.T, text string) *Query {
	t.Helper()
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize %q: %v", text, err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse %q: %v", text, err)
	}
	return q
}

// rowSignature is the rows of a result as sorted strings, so two results
// compare equal whatever the row order.
func rowSignature(res *ResultSet) string {
	rows := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, fmt.Sprint(r))
	}
	sort.Strings(rows)
	return strings.Join(rows, "|")
}

// patternPlaces lists one query per place that can hold a pattern. <K> is the
// property key and <V> is the value: a literal in the literal form, $v in the
// parameter form.
var patternPlaces = []struct{ place, query string }{
	{"MATCH node", `MATCH (p:Person {<K>: <V>}) RETURN p.name AS n`},
	{"MATCH second pattern of one clause", `MATCH (a:Person {id: 1}), (b:Person {<K>: <V>}) RETURN a.name AS a, b.name AS b`},
	{"second MATCH clause", `MATCH (a:Person {id: 1}) MATCH (b:Person {<K>: <V>}) RETURN a.name AS a, b.name AS b`},
	{"OPTIONAL MATCH node", `OPTIONAL MATCH (p:Person {<K>: <V>}) RETURN p.name AS n`},
	{"OPTIONAL MATCH after MATCH", `MATCH (a:Person {id: 1}) OPTIONAL MATCH (b:Person {<K>: <V>}) RETURN a.name AS a, b.name AS b`},
	{"OPTIONAL MATCH second clause", `MATCH (a:Person {id: 1}) OPTIONAL MATCH (b:Person {id: 99}) OPTIONAL MATCH (c:Person {<K>: <V>}) RETURN a.name AS a, c.name AS c`},
	{"MERGE", `MERGE (p:Person {<K>: <V>}) RETURN p.name AS n`},
	{"CREATE", `CREATE (p:Made {<K>: <V>}) RETURN p.<K> AS n`},
	{"after WITH: MATCH", `MATCH (a:Person {id: 1}) WITH a MATCH (b:Person {<K>: <V>}) RETURN a.name AS a, b.name AS b`},
	{"after WITH: OPTIONAL MATCH", `MATCH (a:Person {id: 1}) WITH a OPTIONAL MATCH (b:Person {<K>: <V>}) RETURN a.name AS a, b.name AS b`},
	{"after two WITH", `MATCH (a:Person {id: 1}) WITH a MATCH (b:Person {id: 2}) WITH a, b MATCH (c:Person {<K>: <V>}) RETURN a.name AS a, c.name AS c`},
	{"UNION first part", `MATCH (p:Person {<K>: <V>}) RETURN p.name AS n UNION MATCH (q:Person {id: 7}) RETURN q.name AS n`},
	{"UNION second part", `MATCH (p:Person {id: 1}) RETURN p.name AS n UNION MATCH (q:Person {<K>: <V>}) RETURN q.name AS n`},
	{"UNION second part, OPTIONAL MATCH", `MATCH (p:Person {id: 1}) RETURN p.name AS n UNION OPTIONAL MATCH (q:Person {<K>: <V>}) RETURN q.name AS n`},
	{"UNION ALL third part", `MATCH (p:Person {id: 1}) RETURN p.name AS n UNION ALL MATCH (q:Person {id: 7}) RETURN q.name AS n UNION ALL MATCH (r:Person {<K>: <V>}) RETURN r.name AS n`},
}

var patternValues = []struct {
	kind  string
	key   string
	lit   string
	param any
}{
	{"integer", "id", "2", int64(2)},
	{"string", "name", `"Bob"`, "Bob"},
}

func renderPlace(query, key, value string) string {
	return strings.NewReplacer("<K>", key, "<V>", value).Replace(query)
}

// A parameter gives the rows that the same literal gives.
func TestPatternParams_EachPlaceMatchesLiteral(t *testing.T) {
	for _, pl := range patternPlaces {
		for _, val := range patternValues {
			t.Run(pl.place+"/"+val.kind, func(t *testing.T) {
				litText := renderPlace(pl.query, val.key, val.lit)
				parText := renderPlace(pl.query, val.key, "$v")

				litRes, err := patternParamsStore(t).Execute(parsePatternQuery(t, litText))
				if err != nil {
					t.Fatalf("the literal form has no baseline: %q: %v", litText, err)
				}
				parRes, err := patternParamsStore(t).ExecuteWithParams(
					parsePatternQuery(t, parText), map[string]any{"v": val.param})
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

// Every place must also give a real row, or "equal to the literal" could be
// two empty results.
func TestPatternParams_EachPlaceLiteralBaselineIsNotEmpty(t *testing.T) {
	for _, pl := range patternPlaces {
		t.Run(pl.place, func(t *testing.T) {
			litText := renderPlace(pl.query, "id", "2")
			res, err := patternParamsStore(t).Execute(parsePatternQuery(t, litText))
			if err != nil {
				t.Fatalf("literal form: %v", err)
			}
			if res.Count == 0 {
				t.Errorf("%q returned no rows; the comparison above would prove nothing", litText)
			}
		})
	}
}

// A parameter that the caller did not supply is an error that names it, in
// each place.
func TestPatternParams_MissingParameterNamesIt(t *testing.T) {
	for _, pl := range patternPlaces {
		t.Run(pl.place, func(t *testing.T) {
			text := renderPlace(pl.query, "id", "$v")
			_, err := patternParamsStore(t).ExecuteWithParams(parsePatternQuery(t, text), map[string]any{})
			if err == nil {
				t.Fatalf("%q ran with no parameter v and returned no error", text)
			}
			if !strings.Contains(err.Error(), "$v") {
				t.Errorf("error does not name the parameter: %v", err)
			}
		})
	}
}

// Execute has no parameters at all. A pattern parameter then stays a
// *ParameterRef, which matches nothing, so the query must refuse and not
// return no rows.
func TestPatternParams_ExecuteWithoutParametersRefuses(t *testing.T) {
	for _, pl := range patternPlaces {
		t.Run(pl.place, func(t *testing.T) {
			text := renderPlace(pl.query, "id", "$v")
			res, err := patternParamsStore(t).Execute(parsePatternQuery(t, text))
			if err == nil {
				t.Fatalf("%q ran through Execute and returned %d rows with no error", text, res.Count)
			}
			if !strings.Contains(err.Error(), "$v") {
				t.Errorf("error does not name the parameter: %v", err)
			}
		})
	}
}

func TestPatternParams_OptionalMatchBindsOrGivesNull(t *testing.T) {
	ex := patternParamsStore(t)
	text := `OPTIONAL MATCH (p:Person {id: $v}) RETURN p.name AS n`

	res, err := ex.ExecuteWithParams(parsePatternQuery(t, text), map[string]any{"v": int64(7)})
	if err != nil {
		t.Fatalf("existing node: %v", err)
	}
	if res.Count != 1 || res.Rows[0]["n"] != "Gus" {
		t.Errorf("existing node: rows = %v, want one row with Gus", res.Rows)
	}

	res, err = ex.ExecuteWithParams(parsePatternQuery(t, text), map[string]any{"v": int64(99)})
	if err != nil {
		t.Fatalf("absent node: %v", err)
	}
	if res.Count != 1 || res.Rows[0]["n"] != nil {
		t.Errorf("absent node: rows = %v, want one row with null", res.Rows)
	}
}

func TestPatternParams_OptionalMatchAfterMatch(t *testing.T) {
	ex := patternParamsStore(t)
	q := `MATCH (a:Person {id: $a}) OPTIONAL MATCH (a)-[:KNOWS]->(b:Person {name: $n}) RETURN a.name AS a, b.name AS b`

	res, err := ex.ExecuteWithParams(parsePatternQuery(t, q), map[string]any{"a": int64(1), "n": "Bob"})
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if res.Count != 1 || res.Rows[0]["a"] != "Ann" || res.Rows[0]["b"] != "Bob" {
		t.Errorf("hit: rows = %v, want Ann/Bob", res.Rows)
	}

	res, err = ex.ExecuteWithParams(parsePatternQuery(t, q), map[string]any{"a": int64(1), "n": "Gus"})
	if err != nil {
		t.Fatalf("miss: %v", err)
	}
	if res.Count != 1 || res.Rows[0]["a"] != "Ann" || res.Rows[0]["b"] != nil {
		t.Errorf("miss: rows = %v, want Ann/null", res.Rows)
	}
}

func TestPatternParams_AfterWith(t *testing.T) {
	ex := patternParamsStore(t)
	res, err := ex.ExecuteWithParams(
		parsePatternQuery(t, `MATCH (a:Person) WITH a MATCH (b:Person {id: $v}) RETURN b.name AS n`),
		map[string]any{"v": int64(2)})
	if err != nil {
		t.Fatalf("ExecuteWithParams: %v", err)
	}
	if res.Count == 0 {
		t.Fatalf("a parameter after WITH matched nothing")
	}
	for _, r := range res.Rows {
		if r["n"] != "Bob" {
			t.Errorf("row = %v, want Bob", r)
		}
	}
}

func TestPatternParams_UnionSecondPart(t *testing.T) {
	ex := patternParamsStore(t)
	res, err := ex.ExecuteWithParams(
		parsePatternQuery(t, `MATCH (p:Person {id: 1}) RETURN p.name AS n UNION MATCH (q:Person {id: $v}) RETURN q.name AS n`),
		map[string]any{"v": int64(7)})
	if err != nil {
		t.Fatalf("ExecuteWithParams: %v", err)
	}
	if got := rowSignature(res); got != "map[n:Ann]|map[n:Gus]" {
		t.Errorf("rows = %s, want Ann and Gus", got)
	}
}

// resolvePatternParams used to write the values into the parsed Query, so a
// second run of the same *Query used the first run's values.
func TestPatternParams_ParsedQueryRunsTwiceWithDifferentValues(t *testing.T) {
	for _, text := range []string{
		`MATCH (p:Person {id: $v}) RETURN p.name AS n`,
		`OPTIONAL MATCH (p:Person {id: $v}) RETURN p.name AS n`,
		`MATCH (a:Person {id: 1}) WITH a MATCH (p:Person {id: $v}) RETURN p.name AS n`,
		`MATCH (a:Person {id: 1}) RETURN a.name AS n UNION MATCH (p:Person {id: $v}) RETURN p.name AS n`,
	} {
		t.Run(text, func(t *testing.T) {
			ex := patternParamsStore(t)
			q := parsePatternQuery(t, text)
			for i, c := range []struct {
				v    int64
				want string
			}{{1, "Ann"}, {2, "Bob"}} {
				res, err := ex.ExecuteWithParams(q, map[string]any{"v": c.v})
				if err != nil {
					t.Fatalf("run %d: %v", i+1, err)
				}
				found := false
				for _, r := range res.Rows {
					if r["n"] == c.want {
						found = true
					}
				}
				if !found {
					t.Errorf("run %d with v=%d: rows = %v, want a row with %s", i+1, c.v, res.Rows, c.want)
				}
			}
		})
	}
}
