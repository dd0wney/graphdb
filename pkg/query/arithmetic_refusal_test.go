package query

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// arithmeticStore builds Person nodes whose properties make an arithmetic
// operand wrong on purpose: name is a string, zero is the integer 0, and one
// node has age 0 so a CASE guard has something to guard.
func arithmeticStore(t *testing.T) *Executor {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	var ids []uint64
	for i, age := range []int64{0, 5} {
		n, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"id":   storage.IntValue(int64(i)),
			"name": storage.StringValue(fmt.Sprintf("secret-name-%d", i)),
			"zero": storage.IntValue(0),
			"age":  storage.IntValue(age),
		})
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		ids = append(ids, n.ID)
	}
	if _, err := gs.CreateEdge(ids[0], ids[1], "KNOWS", nil, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	return NewExecutor(gs)
}

func TestArithmeticErrorRefused(t *testing.T) {
	ex := arithmeticStore(t)
	cases := []struct {
		name, text string
		want       []string
	}{
		{"integer division by zero", `RETURN 1 / 0 AS x`, []string{"division by zero"}},
		{"modulo by zero", `RETURN 1 % 0 AS x`, []string{"modulo by zero"}},
		{"float division by zero", `RETURN 1.0 / 0 AS x`, []string{"division by zero"}},
		{"string plus integer", `RETURN 'a' + 1 AS x`, []string{"+", "string", "integer"}},
		{"integer plus string", `RETURN 1 + 'a' AS x`, []string{"+", "integer", "string"}},
		{"negate a string", `RETURN -'a' AS x`, []string{"-", "string"}},
		{"property string plus integer", `MATCH (p:Person) RETURN p.name + 1 AS x`, []string{"+", "string", "integer"}},
		{"integer over zero property", `MATCH (p:Person) RETURN 10 / p.zero AS x`, []string{"division by zero"}},
		{"nested", `RETURN (1 / 0) + 1 AS x`, []string{"division by zero"}},
		{"in WHERE equality", `MATCH (p:Person) WHERE 1 / 0 = 1 RETURN p`, []string{"division by zero"}},
		{"in WHERE ordering", `MATCH (p:Person) WHERE p.age / 0 > 1 RETURN p`, []string{"division by zero"}},
		{"in WITH", `WITH 1 / 0 AS x RETURN x`, []string{"division by zero"}},
		{"function argument", `RETURN toString(1 / 0) AS x`, []string{"division by zero"}},
		{"procedure argument", `CALL algo.pageRank(1 / 0) YIELD scores RETURN scores`, []string{"division by zero"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := runCypher(t, ex, tc.text)
			if err == nil {
				t.Fatalf("%q: want a RefusalError, got rows=%v", tc.text, rs.Rows)
			}
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("%q: error %T %q is not a *RefusalError", tc.text, err, err)
			}
			for _, s := range tc.want {
				if !strings.Contains(refusal.Message, s) {
					t.Errorf("%q: message %q does not contain %q", tc.text, refusal.Message, s)
				}
			}
		})
	}
}

func TestArithmeticNotRefused(t *testing.T) {
	ex := arithmeticStore(t)
	cases := []struct {
		name, text, col string
		want            []string
	}{
		{"null operand plus", `RETURN 1 + null AS x`, "x", []string{"<nil>"}},
		{"null divided by zero", `RETURN null / 0 AS x`, "x", []string{"<nil>"}},
		{"string concatenation", `RETURN 'a' + 'b' AS x`, "x", []string{"ab"}},
		{"integer division", `RETURN 7 / 2 AS x`, "x", []string{"3"}},
		{"float division", `RETURN 7.0 / 2 AS x`, "x", []string{"3.5"}},
		{"missing property", `MATCH (p:Person) RETURN p.missing + 1 AS x`, "x", []string{"<nil>", "<nil>"}},
		{"null from OPTIONAL MATCH", `MATCH (p:Person {id: 1}) OPTIONAL MATCH (p)-[:KNOWS]->(q) RETURN q.age + 1 AS x`, "x", []string{"<nil>"}},
		// The CASE branch that divides is never evaluated for age 0.
		{"CASE guards the division", `MATCH (p:Person) RETURN CASE WHEN p.age = 0 THEN 0 ELSE 100 / p.age END AS x`, "x", []string{"0", "20"}},
		// Recorded, not designed: AND skips its right operand when the left is
		// false, and OR skips it when the left is true.
		{"false AND skips the right operand", `RETURN false AND (1 / 0 = 1) AS x`, "x", []string{"false"}},
		{"true OR skips the right operand", `RETURN true OR (1 / 0 = 1) AS x`, "x", []string{"true"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantCells(t, ex, tc.text, tc.col, tc.want...)
		})
	}
}

func TestArithmeticRefusalMessageHasNoGraphValue(t *testing.T) {
	ex := arithmeticStore(t)
	for _, text := range []string{
		`MATCH (p:Person) RETURN p.name + 1 AS x`,
		`MATCH (p:Person) RETURN -p.name AS x`,
	} {
		_, err := runCypher(t, ex, text)
		if err == nil {
			t.Fatalf("%q: want an error", text)
		}
		for _, bad := range []string{"secret-name", "int64", "float64", "string)"} {
			if strings.Contains(err.Error(), bad) {
				t.Errorf("%q: message %q contains %q", text, err, bad)
			}
		}
	}
}

// A WHERE filter drops the row of an error that is not a refusal. A refusal
// must stop the query at each of the three places that filter.
func TestWhereRefusalReachesTheCaller(t *testing.T) {
	ex := arithmeticStore(t)
	cases := []struct{ name, text string }{
		{"MATCH WHERE", `MATCH (p:Person) WHERE p.age / 0 > 1 RETURN p`},
		{"WITH WHERE", `MATCH (p:Person) WITH p.age AS a WHERE a / 0 > 1 RETURN a`},
		{"OPTIONAL MATCH WHERE", `MATCH (a:Person {id: 0}) OPTIONAL MATCH (a)-[:KNOWS]->(b) WHERE b.age / 0 > 1 RETURN b`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := runCypher(t, ex, tc.text)
			var refusal *RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("%q: want a *RefusalError, got err=%v rows=%v", tc.text, err, rs)
			}
		})
	}
}
