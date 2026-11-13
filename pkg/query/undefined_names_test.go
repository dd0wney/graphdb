package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// undefinedNamesStore builds 4 Person nodes (id 0..3) and KNOWS edges 0->1
// and 1->2, the shape the Relate spike used.
func undefinedNamesStore(t *testing.T) (*storage.GraphStorage, *Executor) {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	ids := make([]uint64, 4)
	for i := range ids {
		n, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"id":   storage.IntValue(int64(i)),
			"name": storage.StringValue(fmt.Sprintf("person-%d", i)),
		})
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		ids[i] = n.ID
	}
	for _, e := range [][2]int{{0, 1}, {1, 2}} {
		if _, err := gs.CreateEdge(ids[e[0]], ids[e[1]], "KNOWS", nil, 1); err != nil {
			t.Fatalf("CreateEdge %v: %v", e, err)
		}
	}
	return gs, NewExecutor(gs)
}

// cellsOf renders one column of a result as sorted strings, so that a test
// does not depend on row order or on the Go integer width.
func cellsOf(rs *ResultSet, col string) []string {
	out := make([]string, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		out = append(out, fmt.Sprint(row[col]))
	}
	sort.Strings(out)
	return out
}

func wantCells(t *testing.T, ex *Executor, text, col string, want ...string) {
	t.Helper()
	rs, err := runCypher(t, ex, text)
	if err != nil {
		t.Fatalf("%q: unexpected error: %v", text, err)
	}
	sort.Strings(want)
	got := cellsOf(rs, col)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%q: column %q = %v, want %v", text, col, got, want)
	}
}

func wantRefusal(t *testing.T, ex *Executor, text string, substrs ...string) {
	t.Helper()
	rs, err := runCypher(t, ex, text)
	if err == nil {
		t.Fatalf("%q: want an error, got rows=%v", text, rs.Rows)
	}
	for _, s := range substrs {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%q: error %q does not contain %q", text, err, s)
		}
	}
}

func TestUndefinedVariableRefused(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	cases := []struct {
		name, text string
		want       []string
	}{
		{"return", `MATCH (p:Person) RETURN q.name LIMIT 1`, []string{"undefined variable: q", "RETURN"}},
		{"return with zero rows", `MATCH (x:NoSuchLabel) RETURN q.name`, []string{"undefined variable: q", "RETURN"}},
		{"where", `MATCH (p:Person) WHERE q.age > 1 RETURN p.id AS id`, []string{"undefined variable: q", "WHERE"}},
		{"with item", `MATCH (p:Person) WITH x AS y RETURN y AS y`, []string{"undefined variable: x", "WITH"}},
		{"out of scope after with", `MATCH (p:Person) WITH p.id AS id RETURN p.name AS n`, []string{"undefined variable: p", "RETURN"}},
		{"set", `MATCH (p:Person) SET q.x = 1`, []string{"undefined variable: q", "SET"}},
		{"set value", `MATCH (p:Person) SET p.x = q.y`, []string{"undefined variable: q", "SET"}},
		{"delete", `MATCH (p:Person) DELETE q`, []string{"undefined variable: q", "DELETE"}},
		{"remove", `MATCH (p:Person) REMOVE q.x`, []string{"undefined variable: q", "REMOVE"}},
		{"order by", `MATCH (p:Person) RETURN p.id AS id ORDER BY q.name`, []string{"undefined variable: q", "ORDER BY"}},
		{"call argument", `CALL algo.labelPropagation(nosuch)`, []string{"undefined variable: nosuch", "CALL"}},
		{"unwind", `UNWIND q.list AS x RETURN x AS x`, []string{"undefined variable: q", "UNWIND"}},
		{"with where", `MATCH (p:Person) WITH p.id AS id WHERE q > 1 RETURN id AS id`, []string{"undefined variable: q", "WITH"}},
		{"second with segment", `MATCH (p:Person) WITH p WITH z AS z RETURN z AS z`, []string{"undefined variable: z", "WITH"}},
		{"function argument", `MATCH (p:Person) RETURN toUpper(q.name) AS x`, []string{"undefined variable: q"}},
		{"case result", `MATCH (p:Person) RETURN CASE WHEN p.id > 1 THEN q.name ELSE 'a' END AS x`, []string{"undefined variable: q"}},
		{"arithmetic", `MATCH (p:Person) RETURN p.id + q.id AS x`, []string{"undefined variable: q"}},
		{"optional match where", `MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b) WHERE q.id = 1 RETURN a.id AS a`, []string{"undefined variable: q"}},
		{"merge on create", `MERGE (m:M {k: 1}) ON CREATE SET m.c = q.x`, []string{"undefined variable: q", "SET"}},
		{"aggregate argument", `MATCH (p:Person) RETURN count(q) AS n`, []string{"undefined variable: q", "RETURN"}},
		{"group by", `MATCH (p:Person) RETURN p.id AS id, count(p) AS n GROUP BY q.id`, []string{"undefined variable: q"}},
		{"union second side", `RETURN 1 AS x UNION RETURN q.name AS x`, []string{"undefined variable: q"}},
		{"explain", `EXPLAIN MATCH (p:Person) RETURN q.name`, []string{"undefined variable: q"}},
		{"profile", `PROFILE MATCH (p:Person) RETURN q.name`, []string{"undefined variable: q"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantRefusal(t, ex, tc.text, tc.want...)
		})
	}
}

// A refusal must come before any step runs, because an earlier segment can
// write to the graph.
func TestUndefinedVariableRefusedBeforeAnyWrite(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	wantRefusal(t, ex, `CREATE (t:Tmp {v: 1}) WITH t MATCH (p:Person) RETURN q.name`, "undefined variable: q")
	wantCells(t, ex, `MATCH (t:Tmp) RETURN count(t) AS n`, "n", "0")

	wantRefusal(t, ex, `CREATE (t:Tmp {v: 1}) RETURN q.name`, "undefined variable: q")
	wantCells(t, ex, `MATCH (t:Tmp) RETURN count(t) AS n`, "n", "0")

	// The first side of a UNION writes, the second is wrong.
	wantRefusal(t, ex, `CREATE (t:Tmp {v: 1}) RETURN t.v AS x UNION RETURN q.name AS x`, "undefined variable: q")
	wantCells(t, ex, `MATCH (t:Tmp) RETURN count(t) AS n`, "n", "0")
}

func TestNamesStillBoundAreNotRefused(t *testing.T) {
	_, ex := undefinedNamesStore(t)

	t.Run("order by a bare variable alias", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) RETURN p.id AS id ORDER BY id`, "id", "0", "1", "2", "3")
	})
	t.Run("with where sees the with names", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) WITH p.id AS id WHERE id > 0 RETURN id AS id`, "id", "1", "2", "3")
	})
	t.Run("with passes a variable", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) WITH p RETURN p.id AS id`, "id", "0", "1", "2", "3")
	})
	t.Run("relationship variable", func(t *testing.T) {
		wantCells(t, ex, `MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN a.id AS a, type(r) AS t, b.id AS b`, "t", "KNOWS", "KNOWS")
	})
	t.Run("optional match binds b", func(t *testing.T) {
		rs, err := runCypher(t, ex, `MATCH (a:Person) OPTIONAL MATCH (a)-[:KNOWS]->(b) RETURN a.id AS a, b.id AS b`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rs.Rows) != 4 {
			t.Fatalf("rows = %d, want 4", len(rs.Rows))
		}
		nulls := 0
		for _, row := range rs.Rows {
			if row["b"] == nil {
				nulls++
			}
		}
		if nulls != 2 { // persons 2 and 3 know nobody
			t.Errorf("rows with b null = %d, want 2", nulls)
		}
	})
	t.Run("unwind over a collected list", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) WITH collect(p.id) AS ids UNWIND ids AS x RETURN x AS x`, "x", "0", "1", "2", "3")
	})
	t.Run("merge with on create", func(t *testing.T) {
		wantCells(t, ex, `MERGE (m:M {k: 1}) ON CREATE SET m.created = true RETURN m.k AS k`, "k", "1")
	})
	t.Run("call with yield", func(t *testing.T) {
		rs, err := runCypher(t, ex, `CALL algo.pageRank() YIELD scores RETURN scores AS s`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rs.Rows) != 1 {
			t.Errorf("rows = %d, want 1", len(rs.Rows))
		}
	})
	t.Run("bare call", func(t *testing.T) {
		if _, err := runCypher(t, ex, `CALL algo.detectCycles()`); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("bare call then a return of a procedure column", func(t *testing.T) {
		// The columns of a bare CALL are not known before it runs, so the
		// name is not refused. The refusal in this case would be a guess.
		if _, err := runCypher(t, ex, `CALL algo.pageRank() RETURN scores AS s`); err != nil && strings.Contains(err.Error(), "undefined variable") {
			t.Fatalf("a name after a bare CALL must not be refused: %v", err)
		}
	})
	t.Run("case", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) RETURN CASE WHEN p.id > 1 THEN 'big' ELSE 'small' END AS k`, "k", "big", "big", "small", "small")
	})
	t.Run("aggregate", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) RETURN count(p) AS n`, "n", "4")
	})
	t.Run("aggregate in with", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) WITH count(p) AS c RETURN c AS c`, "c", "4")
	})
	t.Run("two with segments", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) WITH p.id AS id WITH id AS j RETURN j AS j`, "j", "0", "1", "2", "3")
	})
	t.Run("union", func(t *testing.T) {
		wantCells(t, ex, `RETURN 1 AS x UNION RETURN 2 AS x`, "x", "1", "2")
	})
	t.Run("explain", func(t *testing.T) {
		if _, err := runCypher(t, ex, `EXPLAIN MATCH (p:Person) RETURN p.id AS id`); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("functions outside the registry", func(t *testing.T) {
		wantCells(t, ex, `MATCH (p:Person) WHERE p.id = 1 RETURN id(p) > 0 AS x, labels(p) AS l, keys(p) AS k, size(p.name) AS c`, "c", "8")
	})
}

func TestParameterIsNotAnUndefinedVariable(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	tokens, err := NewLexer(`MATCH (p:Person) WHERE p.id = $id RETURN p.id AS id`).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rs, err := ex.ExecuteWithParamsContext(ctx, q, map[string]any{"id": int64(2)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cellsOf(rs, "id"); len(got) != 1 || got[0] != "2" {
		t.Errorf("id = %v, want [2]", got)
	}

	// A parameter does not hide an unbound variable.
	tokens, err = NewLexer(`MATCH (p:Person) WHERE p.id = $id RETURN q.id AS id`).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	q, err = NewParser(tokens).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ex.ExecuteWithParamsContext(ctx, q, map[string]any{"id": int64(2)}); err == nil || !strings.Contains(err.Error(), "undefined variable: q") {
		t.Errorf("want undefined variable: q, got %v", err)
	}
}

// ExecuteWithText does not go through ExecuteWithOptions, so it needs the
// check of its own.
func TestExecuteWithTextRefusesUndefinedNames(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	for _, text := range []string{
		`MATCH (p:Person) RETURN q.name LIMIT 1`,
	} {
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
}

// The caller can hand the first segment bindings of its own. Their names are
// in scope. An empty list says the names are not known, so nothing is refused.
func TestInitialBindingsScope(t *testing.T) {
	_, ex := undefinedNamesStore(t)
	parse := func(text string) *Query {
		t.Helper()
		tokens, err := NewLexer(text).Tokenize()
		if err != nil {
			t.Fatal(err)
		}
		q, err := NewParser(tokens).Parse()
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	ctx := context.Background()

	q := parse(`RETURN seeded AS s`)
	q.InitialBindings = []*BindingSet{{bindings: map[string]any{"seeded": int64(7), "$param": int64(1)}}}
	rs, err := ex.ExecuteWithContext(ctx, q)
	if err != nil {
		t.Fatalf("a name in InitialBindings was refused: %v", err)
	}
	if got := cellsOf(rs, "s"); len(got) != 1 || got[0] != "7" {
		t.Errorf("s = %v, want [7]", got)
	}

	q = parse(`RETURN other AS s`)
	q.InitialBindings = []*BindingSet{{bindings: map[string]any{"seeded": int64(7)}}}
	if _, err := ex.ExecuteWithContext(ctx, q); err == nil || !strings.Contains(err.Error(), "undefined variable: other") {
		t.Errorf("want undefined variable: other, got %v", err)
	}

	// A key that starts with $ is a parameter. It is not a variable named $x.
	q = parse(`RETURN x AS s`)
	q.InitialBindings = []*BindingSet{{bindings: map[string]any{"$x": int64(1)}}}
	if _, err := ex.ExecuteWithContext(ctx, q); err == nil || !strings.Contains(err.Error(), "undefined variable: x") {
		t.Errorf("a parameter must not bind the variable x: got %v", err)
	}

	q = parse(`RETURN anything AS s`)
	q.InitialBindings = []*BindingSet{}
	if _, err := ex.ExecuteWithContext(ctx, q); err != nil && strings.Contains(err.Error(), "undefined variable") {
		t.Errorf("an empty InitialBindings must not refuse a name: %v", err)
	}
}
