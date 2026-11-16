package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// SKIP and LIMIT written after WITH belong to the WITH. They were once read as
// the first token of the next segment, where only RETURN looked at them, so
// they were ignored and a write after them ran on every row.

// withLimitStore holds six :Person nodes, id 1 to 6. Persons 1 to 3 are in
// group "x" and persons 4 to 6 in group "y".
func withLimitStore(t *testing.T) *Executor {
	t.Helper()
	ex := unwindStore(t)
	for i := 1; i <= 6; i++ {
		g := "x"
		if i > 3 {
			g = "y"
		}
		if _, err := ex.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)), "g": storage.StringValue(g),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return ex
}

func personCount(t *testing.T, ex *Executor) int {
	t.Helper()
	nodes, err := ex.graph.FindNodesByLabelAcrossTenants("Person")
	if err != nil {
		t.Fatal(err)
	}
	return len(nodes)
}

func TestWithSkipLimit_DeleteProbe(t *testing.T) {
	ex := withLimitStore(t)
	if _, err := runUnwind(t, ex, `MATCH (a:Person) WITH a SKIP 1 DETACH DELETE a`, nil); err != nil {
		t.Fatal(err)
	}
	if got := personCount(t, ex); got != 1 {
		t.Errorf("Person nodes after WITH a SKIP 1 DETACH DELETE a = %d, want 1 (6 before)", got)
	}
}

func TestWithSkipLimit_LimitThenMatch(t *testing.T) {
	ex := withLimitStore(t)
	res, err := runUnwind(t, ex, `MATCH (p:Person) WITH p LIMIT 1 MATCH (q:Person) RETURN q.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 6 {
		t.Errorf("rows = %d, want 6 (one kept row times six persons)", len(res.Rows))
	}
}

func TestWithSkipLimit_SkipAndLimit(t *testing.T) {
	ex := withLimitStore(t)
	// A trailing MATCH multiplies the rows that the WITH keeps by six, and a
	// RETURN after the WITH could not hide a LIMIT that it applied itself.
	res, err := runUnwind(t, ex, `MATCH (a:Person) WITH a SKIP 1 LIMIT 2 MATCH (q:Person) RETURN q.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 12 {
		t.Errorf("rows = %d, want 12 (two kept rows times six persons)", len(res.Rows))
	}
	if _, err := runUnwind(t, ex, `MATCH (a:Person) WITH a SKIP 1 LIMIT 2 DETACH DELETE a`, nil); err != nil {
		t.Fatal(err)
	}
	if got := personCount(t, ex); got != 4 {
		t.Errorf("Person nodes after SKIP 1 LIMIT 2 DETACH DELETE = %d, want 4", got)
	}
}

func TestWithSkipLimit_Parameter(t *testing.T) {
	// RETURN takes a number only, so a parameter is refused in the WITH too,
	// and the refusal must leave the store as it was.
	for _, text := range []string{
		`MATCH (a:Person) WITH a LIMIT $n DETACH DELETE a`,
		`MATCH (a:Person) WITH a SKIP $n DETACH DELETE a`,
	} {
		ex := withLimitStore(t)
		wantRefusedCount(t, ex, text, 6)
	}
}

// wantRefusedCount parses and runs text under the run parameter n=2, and
// requires a refusal, either from the parser or from the executor, and that
// the Person count stays at want.
func wantRefusedCount(t *testing.T, ex *Executor, text string, want int) {
	t.Helper()
	tokens, err := NewLexer(text).Tokenize()
	if err == nil {
		var q *Query
		if q, err = NewParser(tokens).Parse(); err == nil {
			_, err = ex.ExecuteWithParams(q, map[string]any{"n": 2})
		}
	}
	if err == nil {
		t.Errorf("%q ran without an error", text)
	}
	if got := personCount(t, ex); got != want {
		t.Errorf("%q: Person nodes = %d, want %d", text, got, want)
	}
}

// WITH DISTINCT is not parsed at all, so there is no DISTINCT projection for
// SKIP or LIMIT to follow. This test records that the refusal is loud and the
// store does not change, so that adding DISTINCT later has to revisit it.
func TestWithSkipLimit_DistinctIsRefused(t *testing.T) {
	ex := withLimitStore(t)
	wantRefused(t, ex, `MATCH (a:Person) WITH DISTINCT a.g AS g LIMIT 1 DETACH DELETE a`, "DISTINCT")
}

func TestWithSkipLimit_Aggregate(t *testing.T) {
	ex := withLimitStore(t)
	res, err := runUnwind(t, ex, `MATCH (a:Person) WITH a.g AS g, count(a) AS c LIMIT 1 MATCH (q:Person) RETURN q.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 6 {
		t.Errorf("rows = %d, want 6 (two groups, LIMIT 1, times six persons)", len(res.Rows))
	}
	res, err = runUnwind(t, ex, `MATCH (a:Person) WITH a.g AS g, count(a) AS c SKIP 1 MATCH (q:Person) RETURN q.id AS id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 6 {
		t.Errorf("rows = %d, want 6 (two groups, SKIP 1, times six persons)", len(res.Rows))
	}
}

func TestWithSkipLimit_WhereIsRefused(t *testing.T) {
	for _, text := range []string{
		`MATCH (a:Person) WITH a LIMIT 1 WHERE a.id > 0 DETACH DELETE a`,
		`MATCH (a:Person) WITH a SKIP 1 WHERE a.id > 0 DETACH DELETE a`,
	} {
		ex := withLimitStore(t)
		wantRefused(t, ex, text, "WHERE with SKIP or LIMIT after WITH is not supported yet", "second WITH")
		if got := personCount(t, ex); got != 6 {
			t.Errorf("%q: Person nodes = %d, want 6", text, got)
		}
	}
}

func TestWithSkipLimit_AtSegmentStartIsRefused(t *testing.T) {
	ex := withLimitStore(t)
	wantRefused(t, ex, `MATCH (a:Person) WITH a LIMIT 1 LIMIT 2 DETACH DELETE a`, "LIMIT")
	if got := personCount(t, ex); got != 6 {
		t.Errorf("Person nodes = %d, want 6", got)
	}
	wantRefused(t, ex, `MATCH (a:Person) WITH a SKIP 1 SKIP 2 RETURN a`, "SKIP")
	wantRefused(t, ex, `LIMIT 1 MATCH (a:Person) RETURN a`, "LIMIT")
}

func TestWithSkipLimit_ReturnStillWorks(t *testing.T) {
	ex := withLimitStore(t)
	res, err := runUnwind(t, ex, `MATCH (a:Person) RETURN a.id AS id SKIP 2 LIMIT 3`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 3 {
		t.Errorf("rows = %d, want 3", len(res.Rows))
	}
}
