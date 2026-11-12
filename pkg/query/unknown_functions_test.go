package query

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUnknownFunctionRefused(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	wantRefusal(t, ex, `RETURN nosuchfn(1) AS x`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `MATCH (p:Person) WHERE nosuchfn(p.id) > 1 RETURN p.id AS id`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `MATCH (p:Person) RETURN toUpper(nosuchfn(p.name)) AS x`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `MATCH (p:Person) WITH nosuchfn(p.id) AS x RETURN x AS x`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `MATCH (p:Person) SET p.x = nosuchfn(1)`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `MATCH (p:Person) RETURN CASE WHEN nosuchfn(1) THEN 1 ELSE 2 END AS x`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `RETURN 1 AS x UNION RETURN nosuchfn(1) AS x`, "unknown function: nosuchfn()")
	wantRefusal(t, ex, `EXPLAIN RETURN nosuchfn(1) AS x`, "unknown function: nosuchfn()")
	// A MATCH with no rows is refused too.
	wantRefusal(t, ex, `MATCH (x:NoSuchLabel) RETURN nosuchfn(x.id) AS y`, "unknown function: nosuchfn()")
	// The registry is exact-match, and so is the refusal.
	wantRefusal(t, ex, `RETURN toupper('a') AS x`, "unknown function: toupper()")
	// An aggregate inside an expression is a call to a name that is not
	// registered, and gave null.
	wantRefusal(t, ex, `MATCH (p:Person) RETURN size(collect(p.id)) AS x`, "unknown function: collect()")
}

// A write that comes before the bad call must not run.
func TestUnknownFunctionRefusedBeforeAnyWrite(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	wantRefusal(t, ex, `CREATE (t:Tmp {v: 1}) WITH t MATCH (p:Person) RETURN nosuchfn(p.id) AS x`, "unknown function: nosuchfn()")
	wantCells(t, ex, `MATCH (t:Tmp) RETURN count(t) AS n`, "n", "0")
}

func TestExecuteWithTextRefusesUnknownFunction(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	text := `RETURN nosuchfn(1) AS x`
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ex.ExecuteWithText(text, q); err == nil {
		t.Errorf("%q: want an error from ExecuteWithText, got none", text)
	}
}

func TestFunctionsOutsideTheRegistryAreNotInvented(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	for _, text := range []string{
		`MATCH (p:Person) WHERE p.id = 1 RETURN id(p) AS a, labels(p) AS b, keys(p) AS c, properties(p) AS d`,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN type(r) AS t`,
		`MATCH (p:Person) RETURN size(p.name) AS s, toUpper(p.name) AS u, toString(p.id) AS i`,
		`MATCH (p:Person) RETURN count(p) AS c, collect(p.id) AS ids`,
		`MATCH (p:Person) WITH count(p) AS c RETURN c AS c`,
	} {
		if _, err := runCypher(t, ex, text); err != nil {
			t.Errorf("%q: unexpected error: %v", text, err)
		}
	}
}

// vector.similarity is registered by SetVectorSearch. A wired executor
// accepts it, and the query gives rows.
func TestVectorFunctionOnWiredExecutor(t *testing.T) {
	_, executor, cleanup := setupVectorConformanceGraph(t)
	defer cleanup()
	result := parseAndExecuteWithParams(t, executor,
		`MATCH (c:Concept) WHERE vector.similarity(c.embedding, $q) > 0.5 RETURN c.name`,
		map[string]any{"q": []float32{1.0, 0.0, 0.0}},
	)
	if len(result.Rows) == 0 {
		t.Fatal("want rows from a wired vector.similarity, got none")
	}
}

func TestRange(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	cases := []struct {
		query string
		want  string
	}{
		{`RETURN range(1,3) AS r`, "[1 2 3]"},
		{`RETURN range(0,10,5) AS r`, "[0 5 10]"},
		{`RETURN range(3,1,-1) AS r`, "[3 2 1]"},
		{`RETURN range(3,1) AS r`, "[]"},
		{`RETURN range(1,3,-1) AS r`, "[]"},
		{`RETURN range(2,2) AS r`, "[2]"},
		{`RETURN range(-2,0) AS r`, "[-2 -1 0]"},
		{`RETURN range(0,10,4) AS r`, "[0 4 8]"},
		{`RETURN size(range(1,5)) AS r`, "5"},
		{`RETURN size(range(1,0)) AS r`, "0"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			wantCells(t, ex, tc.query, "r", tc.want)
		})
	}
}

func TestRangeWorksWithInAndSize(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	wantCells(t, ex, `MATCH (p:Person) WHERE p.id IN range(1,2) RETURN p.id AS id`, "id", "1", "2")
}

func TestRangeRefusals(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	wantRefusal(t, ex, `RETURN range(1,3,0) AS r`, "step must not be 0")
	wantRefusal(t, ex, `RETURN range(1,3,-0) AS r`, "step must not be 0")
	wantRefusal(t, ex, `RETURN range(1.5,3) AS r`, "integers")
	wantRefusal(t, ex, `RETURN range('a',3) AS r`, "integers")
	wantRefusal(t, ex, `RETURN range(1) AS r`, "2 or 3 arguments")
	wantRefusal(t, ex, `RETURN range(1,2,3,4) AS r`, "2 or 3 arguments")
	wantRefusal(t, ex, `RETURN range(0,1000000) AS r`, "1000000")
	wantRefusal(t, ex, `RETURN range(0,100000000,1) AS r`, "limit")
	wantCells(t, ex, `RETURN size(range(1,1000000)) AS r`, "r", "1000000")
}

// The error of a function is discarded when the call is evaluated inside an
// expression (extractValue in ast_eval.go), so a fault that is known only at
// run time reaches the caller as null with no error. The static check refuses
// every literal fault; this test records what a parameter still does. When
// extractValue stops discarding errors this test must change to expect the
// error.
func TestRangeRuntimeErrorIsNullToday(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	tokens, err := NewLexer(`RETURN range(1, 3, $step) AS r`).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rs, err := ex.ExecuteWithParamsContext(ctx, q, map[string]any{"step": int64(0)})
	if err != nil {
		if strings.Contains(err.Error(), "step must not be 0") {
			t.Skip("extractValue now returns function errors: change this test to expect the error")
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0]["r"] != nil {
		t.Errorf("rows = %v, want one row with r null", rs.Rows)
	}
}
