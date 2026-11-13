package query

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// orderByStore creates six Person nodes in an order that is neither the name
// order nor the age order. Bob and Carol share an age, so a stable sort keeps
// Bob first. zed has no age; it is last in match order, where compareValues
// (null equals everything) leaves it.
func orderByStore(t *testing.T) *Executor {
	t.Helper()
	gs, ex := newGroupingGraph(t)
	people := []struct {
		name string
		age  int64 // -1 means the node has no age
	}{{"dave", 41}, {"Bob", 20}, {"alice", 30}, {"Carol", 20}, {"eve", 35}, {"zed", -1}}
	for _, p := range people {
		props := map[string]storage.Value{"name": storage.StringValue(p.name)}
		if p.age >= 0 {
			props["age"] = storage.IntValue(p.age)
		}
		person(t, gs, props)
	}
	return ex
}

func columnOf(rows []map[string]any, col string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprint(r[col])
	}
	return strings.Join(out, ",")
}

const matchOrder = "dave,Bob,alice,Carol,eve,zed"

func TestOrderByExpression_Sorts(t *testing.T) {
	ex := orderByStore(t)

	// A sort test that passes on unsorted input by chance is not a gate.
	if got := columnOf(runRows(t, ex, `MATCH (p:Person) RETURN p.name AS n`), "n"); got != matchOrder {
		t.Fatalf("match order = %s, want %s; the fixture no longer proves a sort", got, matchOrder)
	}

	cases := []struct{ name, text, col, want string }{
		{"plus one", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age + 1`, "n", "Bob,Carol,alice,eve,dave,zed"},
		{"plain property for reference", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age`, "n", "Bob,Carol,alice,eve,dave,zed"},
		{"negation", `MATCH (p:Person) RETURN p.name AS n ORDER BY -p.age`, "n", "dave,eve,alice,Bob,Carol,zed"},
		{"modulo desc then name", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age % 2 DESC, p.name`, "n", "dave,eve,Bob,Carol,alice,zed"},
		{"function", `MATCH (p:Person) RETURN p.name AS n ORDER BY toUpper(p.name)`, "n", "alice,Bob,Carol,dave,eve,zed"},
		{"case", `MATCH (p:Person) RETURN p.name AS n ORDER BY CASE WHEN p.age > 30 THEN 0 ELSE 1 END, p.name`, "n", "dave,eve,Bob,Carol,alice,zed"},
		{"expression on a column not returned", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age * -1`, "n", "dave,eve,alice,Bob,Carol,zed"},
		{"alias in expression", `MATCH (p:Person) RETURN p.age AS a ORDER BY a + 1`, "a", "20,20,30,35,41,<nil>"},
		{"alias shadows variable", `MATCH (p:Person) RETURN p.age AS p ORDER BY p + 1`, "p", "20,20,30,35,41,<nil>"},
		{"skip and limit after the sort", `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age + 1 SKIP 1 LIMIT 2`, "n", "Carol,alice"},
		{"union all, sorted segment", `MATCH (p:Person) RETURN p.name AS n UNION ALL MATCH (p:Person) RETURN p.name AS n ORDER BY toUpper(p.name)`, "n", matchOrder + ",alice,Bob,Carol,dave,eve,zed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := columnOf(runRows(t, ex, tc.text), tc.col); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.text, got, tc.want)
			}
		})
	}
}

// The result carries the RETURN columns and nothing else: the value that the
// sort used must not reach the caller.
func TestOrderByExpression_NoHiddenColumn(t *testing.T) {
	ex := orderByStore(t)
	rs, err := runCypher(t, ex, `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age * -1, toUpper(p.name)`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fmt.Sprint(rs.Columns) != "[n]" {
		t.Errorf("Columns = %v, want [n]", rs.Columns)
	}
	for _, r := range rs.Rows {
		if len(r) != 1 {
			t.Fatalf("row %v has keys beyond the RETURN column", r)
		}
	}
}

func TestOrderByExpression_Refused(t *testing.T) {
	ex := orderByStore(t)
	cases := []struct{ name, text, want string }{
		{"division by zero", `MATCH (p:Person) RETURN p.name ORDER BY p.age / 0`, "division by zero"},
		{"string plus integer", `MATCH (p:Person) RETURN p.name ORDER BY p.name + 1`, "cannot apply +"},
		{"undefined variable", `MATCH (p:Person) RETURN p.name ORDER BY q.age + 1`, "undefined variable: q"},
		{"unknown function", `MATCH (p:Person) RETURN p.name ORDER BY nosuchfn(p.age)`, "unknown function: nosuchfn"},
		{"after an aggregate", `MATCH (p:Person) RETURN p.name AS n, count(p) AS c ORDER BY c + 1`, "not supported after an aggregate"},
		{"after a lone aggregate", `MATCH (p:Person) RETURN count(p) AS c ORDER BY c + 1`, "not supported after an aggregate"},
		{"with DISTINCT", `MATCH (p:Person) RETURN DISTINCT p.name ORDER BY p.age + 1`, "not supported with DISTINCT"},
		{"aggregate inside ORDER BY", `MATCH (p:Person) RETURN p.name AS n, count(p) AS c ORDER BY count(p)`, "unknown function: count()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := runCypher(t, ex, tc.text)
			if err == nil {
				t.Fatalf("%s: want a RefusalError, got rows %v", tc.text, rs.Rows)
			}
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("%s: error %T %q is not a *RefusalError", tc.text, err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: error %q does not contain %q", tc.text, err, tc.want)
			}
		})
	}
}

// WITH takes no ORDER BY: the parser refuses it, so no expression can be
// dropped there. This test fails if the parser starts to accept it without the
// executor evaluating it.
func TestOrderByExpression_WithRefused(t *testing.T) {
	tokens, err := NewLexer(`MATCH (p:Person) WITH p ORDER BY p.age + 1 RETURN p.name AS n`).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	if _, err := NewParser(tokens).Parse(); err == nil {
		t.Fatal("WITH ... ORDER BY parsed; the executor has no sort for it")
	}
}
