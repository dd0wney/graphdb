package query

import (
	"errors"
	"strings"
	"testing"
)

// Each refusal that depends on the query text alone is a *RefusalError, so a
// server can answer it as a client error. The text must stay what it was.
func TestRefusalsAreRefusalErrors(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	cases := []struct{ name, text, want string }{
		{"undefined variable", `MATCH (p:Person) RETURN q.name`, "undefined variable: q (in RETURN)"},
		{"unknown function", `RETURN nosuchfn(1) AS x`, "unknown function: nosuchfn()"},
		{"nested aggregate", `MATCH (p:Person) RETURN size(collect(p.id)) AS x`, "unknown function: collect()"},
		{"range zero step", `RETURN range(1,3,0) AS r`, "step must not be 0"},
		{"range literal", `RETURN range('a',3) AS r`, "integers"},
		{"range arity", `RETURN range(1) AS r`, "2 or 3 arguments"},
		{"unknown procedure", `CALL algo.nope()`, "unknown procedure: algo.nope"},
		{"missing yield column", `CALL algo.pageRank() YIELD nodeId RETURN nodeId AS n`, `has no column "nodeId"`},
		{"column already bound", `MATCH (scores:Person) CALL algo.pageRank() YIELD scores RETURN scores AS s`, `column "scores" is already bound`},
		{"with key needs a name", `MATCH (p:Person) WITH p.id, count(p) AS c RETURN c AS c`, "WITH: a grouping key"},
		{"with aggregate needs a name", `MATCH (p:Person) WITH p.id AS id, count(p) RETURN id AS id`, "WITH: the aggregate"},
		{"with aggregate argument", `MATCH (p:Person) WITH count(1) AS c RETURN c AS c`, "WITH: count(...) AS c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCypher(t, ex, tc.text)
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("error %v (%T) is not a *RefusalError", err, err)
			}
			if !strings.Contains(refusal.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", refusal.Error(), tc.want)
			}
		})
	}
}

// The two run-time checks are behind the static check for query text, so a
// direct call reaches them.
func TestRuntimeTargetRefusalsAreRefusalErrors(t *testing.T) {
	binding := &BindingSet{bindings: map[string]any{}}

	_, err := resolvePropertyTarget(binding, "SET", "q")
	var refusal *RefusalError
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "SET q: variable is not defined by the query") {
		t.Errorf("resolvePropertyTarget: got %v (%T)", err, err)
	}

	err = newDeleteTargets().add(binding, "q")
	refusal = nil
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "DELETE q: variable is not defined by the query") {
		t.Errorf("deleteTargets.add: got %v (%T)", err, err)
	}
}

// A procedure's own error for its arguments is not a refusal of the query
// text, and an API must not show it.
func TestProcedureArgumentErrorIsNotARefusal(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	_, err := runCypher(t, ex, `CALL algo.kHop('x', 1)`)
	if err == nil {
		t.Fatal("want an error")
	}
	var refusal *RefusalError
	if errors.As(err, &refusal) {
		t.Errorf("a procedure argument error must not be a RefusalError: %v", err)
	}
}
