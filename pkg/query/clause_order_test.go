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
