package queryutil

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/query"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// Known-gap table: each row asserts the CORRECT Cypher behaviour that a spike
// found wrong or absent. No repo convention for such tests existed, so this is
// an expected-failure table with two modes:
//
//   - default: each row runs against a recorder. A row that still fails is
//     skipped with its coord task id. A row that PASSES fails the suite, so a
//     closed gap cannot stay hidden behind a skip: delete the row and move the
//     assertion to a real test in the PR that closes the gap.
//   - GRAPHDB_RUN_KNOWN_GAPS=1: each row runs against the real *testing.T and
//     fails with its message. Use it to read what a gap prints today.
// This file lives in pkg/queryutil, not pkg/query, because the server's
// executor wiring (WireCapabilities) imports pkg/query.

const knownGapsEnv = "GRAPHDB_RUN_KNOWN_GAPS"

// gapResult is the outcome of one query run through the production path.
type gapResult struct {
	rs  *query.ResultSet
	err error
}

// gapReporter is the part of *testing.T a check uses. The default run hands a
// check a recorder instead, so it can see whether the gap is still open
// without failing the suite.
type gapReporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// gapRecorder keeps the failures a check reported.
type gapRecorder struct{ failures []string }

func (*gapRecorder) Helper() {}
func (g *gapRecorder) Errorf(format string, args ...any) {
	g.failures = append(g.failures, fmt.Sprintf(format, args...))
}

type gapCase struct {
	id    string // coord task id, without the "graphdb:" prefix
	query string
	check func(t gapReporter, r gapResult)
}

// runGapQuery mirrors pkg/api handleQuery: sanitize, lex, parse, execute.
func runGapQuery(exec *query.Executor, q string) gapResult {
	sanitized, err := query.SanitizeQuery(q)
	if err != nil {
		return gapResult{err: fmt.Errorf("sanitize: %w", err)}
	}
	tokens, err := query.NewLexer(sanitized).Tokenize()
	if err != nil {
		return gapResult{err: fmt.Errorf("lex: %w", err)}
	}
	parsed, err := query.NewParser(tokens).Parse()
	if err != nil {
		return gapResult{err: fmt.Errorf("parse: %w", err)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rs, err := exec.ExecuteWithContext(ctx, parsed)
	return gapResult{rs: rs, err: err}
}

func buildGapGraph(t *testing.T) *query.Executor {
	t.Helper()
	g, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	ages := []int64{30, 30, 40, 40, 25, 50}
	ids := make([]uint64, len(ages))
	for i, age := range ages {
		n, err := g.CreateNode([]string{"Person"}, map[string]storage.Value{
			"id":   storage.IntValue(int64(i)),
			"age":  storage.IntValue(age),
			"name": storage.StringValue(fmt.Sprintf("person-%d", i)),
		})
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		ids[i] = n.ID
	}
	for _, e := range [][2]int{{0, 1}, {1, 2}, {2, 0}, {0, 3}} {
		if _, err := g.CreateEdge(ids[e[0]], ids[e[1]], "KNOWS", nil, 1); err != nil {
			t.Fatalf("CreateEdge %v: %v", e, err)
		}
	}
	exec, _ := WireCapabilities(query.NewExecutor(g), g)
	return exec
}

// --- check helpers: report with t.Errorf/t.Fatalf, never panic ---

func okResult(t gapReporter, r gapResult) bool {
	t.Helper()
	if r.err != nil {
		t.Errorf("unexpected error: %v", r.err)
		return false
	}
	if r.rs == nil {
		t.Errorf("nil result set and nil error")
		return false
	}
	return true
}

func wantErrContaining(sub string) func(gapReporter, gapResult) {
	return func(t gapReporter, r gapResult) {
		t.Helper()
		if r.err == nil {
			t.Errorf("want error containing %q, got none (rows=%v)", sub, rowsOf(r))
			return
		}
		if !strings.Contains(r.err.Error(), sub) {
			t.Errorf("want error containing %q, got %q", sub, r.err.Error())
		}
	}
}

func rowsOf(r gapResult) any {
	if r.rs == nil {
		return nil
	}
	return r.rs.Rows
}

// wantCell checks the first row's value under col equals want (numbers compared as float64).
func wantCell(col string, want any) func(gapReporter, gapResult) {
	return func(t gapReporter, r gapResult) {
		t.Helper()
		if !okResult(t, r) {
			return
		}
		if len(r.rs.Rows) < 1 {
			t.Errorf("want a row with %s=%v, got 0 rows", col, want)
			return
		}
		if got := r.rs.Rows[0][col]; !valuesEqual(got, want) {
			t.Errorf("column %q = %#v, want %#v (columns=%v)", col, got, want, r.rs.Columns)
		}
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func valuesEqual(got, want any) bool {
	if g, ok := toFloat(got); ok {
		w, ok := toFloat(want)
		return ok && g == w
	}
	return reflect.DeepEqual(got, want)
}

// listAsFloats converts a list value to []float64; ok is false for a scalar.
func listAsFloats(v any) ([]float64, bool) {
	rv := reflect.ValueOf(v)
	if v == nil || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]float64, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		f, ok := toFloat(rv.Index(i).Interface())
		if !ok {
			return nil, false
		}
		out = append(out, f)
	}
	return out, true
}

func wantListCell(col string, want []float64) func(gapReporter, gapResult) {
	return func(t gapReporter, r gapResult) {
		t.Helper()
		if !okResult(t, r) {
			return
		}
		if len(r.rs.Rows) < 1 {
			t.Errorf("want a row with list %s, got 0 rows", col)
			return
		}
		got, ok := listAsFloats(r.rs.Rows[0][col])
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("column %q = %#v, want list %v", col, r.rs.Rows[0][col], want)
		}
	}
}

func TestCypherKnownGaps(t *testing.T) {
	cases := []gapCase{
		{"v1.4-cypher-with-alias-column-name",
			`MATCH (p:Person {id: 0}) WITH p.age AS age RETURN age`,
			func(t gapReporter, r gapResult) {
				if !okResult(t, r) {
					return
				}
				if !reflect.DeepEqual(r.rs.Columns, []string{"age"}) {
					t.Errorf("columns = %q, want [\"age\"]", r.rs.Columns)
				}
			}},
		{"v1.4-cypher-unknown-function-null",
			`RETURN nosuchfn(1) AS x`,
			wantErrContaining("nosuchfn")},
		{"v1.4-cypher-range-returns-null",
			`RETURN range(1,3) AS r`,
			wantListCell("r", []float64{1, 2, 3})},
		{"v1.4-cypher-arithmetic-error-null/div-zero",
			`RETURN 1 / 0 AS x`,
			func(t gapReporter, r gapResult) {
				if r.err == nil {
					t.Errorf("want division error, got none (rows=%v)", rowsOf(r))
				}
			}},
		{"v1.4-cypher-arithmetic-error-null/type-mismatch",
			`MATCH (p:Person {id: 0}) RETURN p.name + 1 AS x`,
			func(t gapReporter, r gapResult) {
				if r.err == nil {
					t.Errorf("want type error, got none (rows=%v)", rowsOf(r))
				}
			}},
		{"v1.4-cypher-count-star",
			`MATCH (p:Person) RETURN count(*) AS n`,
			wantCell("n", 6)},
		{"v1.4-cypher-count-distinct",
			`MATCH (p:Person) RETURN count(DISTINCT p.age) AS n`,
			wantCell("n", 4)},
		{"v1.4-cypher-unwind-literal",
			`UNWIND [1,2,3] AS x RETURN x * 2 AS y`,
			func(t gapReporter, r gapResult) {
				if !okResult(t, r) {
					return
				}
				var got []float64
				for _, row := range r.rs.Rows {
					f, ok := toFloat(row["y"])
					if !ok {
						t.Errorf("y = %#v, want a number", row["y"])
						return
					}
					got = append(got, f)
				}
				if !reflect.DeepEqual(got, []float64{2, 4, 6}) {
					t.Errorf("y values = %v, want [2 4 6]", got)
				}
			}},
		{"v1.4-cypher-negative-literal-in-map",
			`OPTIONAL MATCH (p:Person {id: -1}) RETURN p`,
			func(t gapReporter, r gapResult) {
				if r.err != nil {
					t.Errorf("unexpected error: %v", r.err)
				}
			}},
		{"v1.4-cypher-named-path-shortest-path/named-path",
			`MATCH p = (a:Person {id: 0})-[:KNOWS]->(b) RETURN length(p) AS l LIMIT 1`,
			wantCell("l", 1)},
		{"v1.4-cypher-named-path-shortest-path/shortest-path",
			`MATCH p = shortestPath((a:Person {id: 0})-[:KNOWS*]-(b:Person {id: 2})) RETURN length(p) AS len`,
			wantCell("len", 1)},
		{"v1.4-cypher-subqueries-and-comprehension/call-subquery",
			`CALL { MATCH (p:Person) RETURN count(p) AS n } RETURN n`,
			wantCell("n", 6)},
		{"v1.4-cypher-subqueries-and-comprehension/exists-subquery",
			`MATCH (p:Person) WHERE EXISTS { (p)-[:KNOWS]->(:Person {id: 1}) } RETURN count(p) AS n`,
			wantCell("n", 1)},
		{"v1.4-cypher-subqueries-and-comprehension/list-comprehension",
			`RETURN [x IN range(1,5) WHERE x % 2 = 1 | x*x] AS odds`,
			wantListCell("odds", []float64{1, 9, 25})},
	}

	run := os.Getenv(knownGapsEnv) == "1"
	exec := buildGapGraph(t)
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			task := strings.SplitN(tc.id, "/", 2)[0]
			if run {
				tc.check(t, runGapQuery(exec, tc.query))
				return
			}
			rec := &gapRecorder{}
			tc.check(rec, runGapQuery(exec, tc.query))
			if len(rec.failures) == 0 {
				t.Fatalf("known gap is closed: coord graphdb:%s. Delete this row and move its assertion to a real test.", task)
			}
			t.Skipf("known gap: coord graphdb:%s (%s)", task, rec.failures[0])
		})
	}
}
