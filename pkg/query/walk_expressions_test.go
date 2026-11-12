package query

import (
	"fmt"
	"testing"
)

// The walker must reach every expression slot. Each query has an expression
// in one clause, and the walker must report that clause.
func TestWalkQueryExpressionsReachesEachClause(t *testing.T) {
	cases := map[string]string{
		"WHERE":                `MATCH (p:Person) WHERE p.id > 0 RETURN p.id AS id`,
		"OPTIONAL MATCH WHERE": `MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b) WHERE b.id > 0 RETURN a.id AS a`,
		"WITH":                 `MATCH (p:Person) WITH p.id AS id RETURN id AS id`,
		"WITH WHERE":           `MATCH (p:Person) WITH p.id AS id WHERE id > 0 RETURN id AS id`,
		"UNWIND":               `MATCH (p:Person) UNWIND p.tags AS t RETURN t AS t`,
		"SET":                  `MATCH (p:Person) SET p.x = p.id + 1`,
		"REMOVE":               `MATCH (p:Person) REMOVE p.x`,
		"DELETE":               `MATCH (p:Person) DELETE p`,
		"ON CREATE SET":        `MERGE (m:M {k: 1}) ON CREATE SET m.c = m.k + 1`,
		"ON MATCH SET":         `MERGE (m:M {k: 1}) ON MATCH SET m.c = m.k + 1`,
		"RETURN":               `MATCH (p:Person) RETURN p.id AS id`,
		"GROUP BY":             `MATCH (p:Person) RETURN p.id AS id, count(p) AS n GROUP BY p.id`,
		"ORDER BY":             `MATCH (p:Person) RETURN p.id AS id ORDER BY p.name`,
		"CALL":                 `CALL algo.pageRank(0.85, 10)`,
		"MATCH":                `MATCH (p:Person {id: $id}) RETURN p.id AS id`,
		"CREATE":               `CREATE (t:Tmp {v: $v})`,
		"MERGE":                `MERGE (m:M {k: $k})`,
	}
	for clause, text := range cases {
		t.Run(clause, func(t *testing.T) {
			tokens, err := NewLexer(text).Tokenize()
			if err != nil {
				t.Fatal(err)
			}
			q, err := NewParser(tokens).Parse()
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			walkQueryExpressions(q, func(c string, _ Expression) { seen[c] = true })
			if !seen[clause] {
				t.Errorf("walker never visited clause %q; saw %v", clause, seen)
			}
		})
	}
}

// The walker must follow the WITH chain and both sides of a UNION.
func TestWalkQueryExpressionsFollowsChainAndUnion(t *testing.T) {
	for _, text := range []string{
		`MATCH (p:Person) WITH p RETURN zzz.name`,
		`RETURN 1 AS x UNION RETURN zzz.name AS x`,
		`MATCH (p:Person) WITH p RETURN p.id AS x UNION MATCH (p:Person) WITH p RETURN zzz.name AS x`,
	} {
		tokens, err := NewLexer(text).Tokenize()
		if err != nil {
			t.Fatal(err)
		}
		q, err := NewParser(tokens).Parse()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		walkQueryExpressions(q, func(_ string, e Expression) {
			if pe, ok := e.(*PropertyExpression); ok && pe.Variable == "zzz" {
				found = true
			}
		})
		if !found {
			t.Errorf("%q: walker did not reach zzz", text)
		}
	}
}

// Each Expression implementation, nested inside the others, must be visited
// down to its leaves. A type the walker skips would hide a name from both
// checks.
func TestWalkExpressionsCoversEachImplementation(t *testing.T) {
	leaf := func(name string) Expression { return &PropertyExpression{Variable: name} }
	tree := &CaseExpression{
		Operand: &UnaryExpression{Operator: "-", Operand: leaf("in_unary")},
		WhenClauses: []CaseWhen{{
			Condition: &BinaryExpression{Left: leaf("in_binary_l"), Operator: "=",
				Right: &ArithmeticExpression{Left: leaf("in_arith_l"), Operator: "+", Right: leaf("in_arith_r")}},
			Result: &FunctionCallExpression{Name: "toUpper", Args: []Expression{leaf("in_call")}},
		}},
		ElseResult: &LiteralExpression{Value: []any{leaf("in_literal"), &ParameterRef{Name: "p"}}},
	}
	seenVars := map[string]bool{}
	seenTypes := map[string]bool{}
	walkExpression("RETURN", tree, func(_ string, e Expression) {
		seenTypes[fmt.Sprintf("%T", e)] = true
		if pe, ok := e.(*PropertyExpression); ok {
			seenVars[pe.Variable] = true
		}
	})
	for _, v := range []string{"in_unary", "in_binary_l", "in_arith_l", "in_arith_r", "in_call", "in_literal"} {
		if !seenVars[v] {
			t.Errorf("walker did not reach %s", v)
		}
	}
	for _, typ := range []string{
		"*query.CaseExpression", "*query.UnaryExpression", "*query.BinaryExpression",
		"*query.ArithmeticExpression", "*query.FunctionCallExpression",
		"*query.LiteralExpression", "*query.PropertyExpression", "*query.ParameterExpression",
	} {
		if !seenTypes[typ] {
			t.Errorf("walker did not visit a %s", typ)
		}
	}
}
