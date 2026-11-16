package query

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// graphdb runs one query segment as a fixed sequence of steps: MATCH, WHERE,
// OPTIONAL MATCH, CALL, MERGE, UNWIND, CREATE, SET, REMOVE, DELETE, RETURN.
// A query that writes two clauses in the opposite order gave a wrong result
// with no error. The parser refuses each such order and names the WITH split.

// orderStore holds one :A node (id 1, k 1) and one :B node (k 2).
func orderStore(t *testing.T) *Executor {
	t.Helper()
	ex := unwindStore(t)
	if _, err := ex.graph.CreateNode([]string{"A"}, map[string]storage.Value{
		"id": storage.IntValue(1), "k": storage.IntValue(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.graph.CreateNode([]string{"B"}, map[string]storage.Value{"k": storage.IntValue(2)}); err != nil {
		t.Fatal(err)
	}
	return ex
}

// storeSnapshot lists every node of the labels the tests use, so that a test
// can show that a refused write left the store as it was.
func storeSnapshot(t *testing.T, ex *Executor) string {
	t.Helper()
	var lines []string
	for _, label := range []string{"A", "B", "L", "Z"} {
		nodes, err := ex.graph.FindNodesByLabelAcrossTenants(label)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			lines = append(lines, fmt.Sprintf("%s %v", label, n.Properties))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// wantRefused parses text and checks that the parser refuses it with a message
// that holds each of wants. When the parse succeeds, it runs the query, to put
// the damage in the failure line, and then fails.
func wantRefused(t *testing.T, ex *Executor, text string, wants ...string) {
	t.Helper()
	before := storeSnapshot(t, ex)
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize %q: %v", text, err)
	}
	q, perr := NewParser(tokens).Parse()
	if perr == nil {
		res, xerr := ex.Execute(q)
		after := storeSnapshot(t, ex)
		t.Fatalf("%q parsed and ran (err=%v, result=%v, store changed=%v); it must be refused", text, xerr, res, before != after)
	}
	for _, want := range wants {
		if !strings.Contains(perr.Error(), want) {
			t.Fatalf("%q: error = %q, want it to contain %q", text, perr, want)
		}
	}
	if after := storeSnapshot(t, ex); after != before {
		t.Fatalf("%q was refused but the store changed:\nbefore:\n%s\nafter:\n%s", text, before, after)
	}
}

// Task 1: the plan runs every OPTIONAL MATCH before any write.
func TestClauseOrder_OptionalMatchAfterWriteIsRefused(t *testing.T) {
	queries := map[string]string{
		"CREATE": `CREATE (n:L {k: 1}) OPTIONAL MATCH (m:L) RETURN m.k`,
		"MERGE":  `MERGE (n:L {k: 1}) OPTIONAL MATCH (m:L) RETURN m.k`,
		"SET":    `MATCH (a:A) SET a.k = 5 OPTIONAL MATCH (m:A {k: 5}) RETURN m.k`,
		"REMOVE": `MATCH (a:A) REMOVE a.k OPTIONAL MATCH (m:A) RETURN m.k`,
		"DELETE": `MATCH (a:A) DELETE a OPTIONAL MATCH (m:A) RETURN m.k`,
		"DETACH": `MATCH (a:A) DETACH DELETE a OPTIONAL MATCH (m:A) RETURN m.k`,
	}
	for name, text := range queries {
		t.Run(name, func(t *testing.T) {
			wantRefused(t, orderStore(t), text, "OPTIONAL MATCH after ", "WITH")
		})
	}
}

// The refusal names WITH, so the WITH form must give the openCypher result.
func TestClauseOrder_OptionalMatchAfterWriteWithForm(t *testing.T) {
	ex := orderStore(t)
	res, err := runUnwind(t, ex, `CREATE (n:L {k: 1}) WITH n OPTIONAL MATCH (m:L) RETURN m.k AS k`, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, res, "map[k:1]")

	res, err = runUnwind(t, ex, `MATCH (a:A) SET a.k = 5 WITH a OPTIONAL MATCH (m:A {k: 5}) RETURN m.k AS k`, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, res, "map[k:5]")
}

// Task 2: among CREATE, SET, REMOVE and DELETE the plan order is that list.
// Each text order that is the opposite of it is refused: six pairs, and
// DETACH DELETE counts as DELETE. A repeated CREATE was refused already.
func TestClauseOrder_WriteAgainstPlanOrderIsRefused(t *testing.T) {
	queries := map[string]struct{ text, want string }{
		"SET then CREATE":           {`MATCH (a:A) SET a.k = 9 CREATE (n:Z {v: 1})`, "CREATE after SET"},
		"REMOVE then CREATE":        {`MATCH (a:A) REMOVE a.k CREATE (n:Z {v: 1})`, "CREATE after REMOVE"},
		"DELETE then CREATE":        {`MATCH (a:A) DELETE a CREATE (n:Z)`, "CREATE after DELETE"},
		"DETACH DELETE then CREATE": {`MATCH (a:A) DETACH DELETE a CREATE (n:Z)`, "CREATE after DELETE"},
		"REMOVE then SET":           {`MATCH (a:A) REMOVE a.id SET a.id = 5 RETURN a.id`, "SET after REMOVE"},
		"DELETE then SET":           {`MATCH (a:A), (b:B) DELETE a SET b.k = 7`, "SET after DELETE"},
		"DETACH DELETE then SET":    {`MATCH (a:A), (b:B) DETACH DELETE a SET b.k = 7`, "SET after DELETE"},
		"DELETE then REMOVE":        {`MATCH (a:A), (b:B) DELETE a REMOVE b.k`, "REMOVE after DELETE"},
		"DETACH DELETE then REMOVE": {`MATCH (a:A), (b:B) DETACH DELETE a REMOVE b.k`, "REMOVE after DELETE"},
		"SET REMOVE SET":            {`MATCH (a:A) SET a.k = 1 REMOVE a.id SET a.id = 5`, "SET after REMOVE"},
	}
	for name, c := range queries {
		t.Run(name, func(t *testing.T) {
			wantRefused(t, orderStore(t), c.text, c.want, "WITH")
		})
	}
}

// Text order that is the plan order must keep working.
func TestClauseOrder_WriteInPlanOrderStillRuns(t *testing.T) {
	ex := orderStore(t)
	res, err := runUnwind(t, ex, `MATCH (a:A) SET a.k = 9 REMOVE a.id RETURN a.k AS k, a.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, res, "map[id:<nil> k:9]")
	if _, err := runUnwind(t, ex, `MATCH (a:A) CREATE (n:Z {v: 1}) SET n.w = 1 REMOVE n.v`, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runUnwind(t, ex, `MATCH (a:A) SET a.k = 2 DELETE a`, nil); err != nil {
		t.Fatal(err)
	}
}

// Each refusal of the test above names WITH, so each WITH form must give the
// openCypher result.
func TestClauseOrder_WriteAgainstPlanOrderWithForm(t *testing.T) {
	t.Run("REMOVE then SET", func(t *testing.T) {
		ex := orderStore(t)
		res, err := runUnwind(t, ex, `MATCH (a:A) REMOVE a.id WITH a SET a.id = 5 RETURN a.id AS id`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, "map[id:5]")
	})
	t.Run("SET then CREATE", func(t *testing.T) {
		ex := orderStore(t)
		res, err := runUnwind(t, ex, `MATCH (a:A) SET a.k = 9 WITH a CREATE (n:Z {v: 1}) RETURN a.k AS k, n.v AS v`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, "map[k:9 v:1]")
	})
	t.Run("REMOVE then CREATE", func(t *testing.T) {
		ex := orderStore(t)
		res, err := runUnwind(t, ex, `MATCH (a:A) REMOVE a.k WITH a CREATE (n:Z {v: 1}) RETURN a.k AS k, n.v AS v`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, "map[k:<nil> v:1]")
	})
	t.Run("DELETE then CREATE", func(t *testing.T) {
		ex := orderStore(t)
		if _, err := runUnwind(t, ex, `MATCH (a:A) DELETE a WITH 1 AS one CREATE (n:Z)`, nil); err != nil {
			t.Fatal(err)
		}
		if got := countLabel(t, ex, "A"); got != 0 {
			t.Fatalf("%d :A nodes left, want 0", got)
		}
		if got := countLabel(t, ex, "Z"); got != 1 {
			t.Fatalf("%d :Z nodes, want 1", got)
		}
	})
	t.Run("DETACH DELETE then CREATE", func(t *testing.T) {
		ex := orderStore(t)
		if _, err := runUnwind(t, ex, `MATCH (a:A) DETACH DELETE a WITH 1 AS one CREATE (n:Z)`, nil); err != nil {
			t.Fatal(err)
		}
		if got := countLabel(t, ex, "A"); got != 0 {
			t.Fatalf("%d :A nodes left, want 0", got)
		}
		if got := countLabel(t, ex, "Z"); got != 1 {
			t.Fatalf("%d :Z nodes, want 1", got)
		}
	})
	t.Run("DELETE then SET", func(t *testing.T) {
		ex := orderStore(t)
		res, err := runUnwind(t, ex, `MATCH (a:A), (b:B) DELETE a WITH b SET b.k = 7 RETURN b.k AS k`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, "map[k:7]")
		if got := countLabel(t, ex, "A"); got != 0 {
			t.Fatalf("%d :A nodes left, want 0", got)
		}
	})
	t.Run("DELETE then REMOVE", func(t *testing.T) {
		ex := orderStore(t)
		res, err := runUnwind(t, ex, `MATCH (a:A), (b:B) DELETE a WITH b REMOVE b.k RETURN b.k AS k`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, "map[k:<nil>]")
		if got := countLabel(t, ex, "A"); got != 0 {
			t.Fatalf("%d :A nodes left, want 0", got)
		}
	})
}
