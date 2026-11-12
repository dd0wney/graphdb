package query

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/tenant"
)

func TestCallStep_ProfileCallReturnsProcedureColumns(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `PROFILE CALL algo.pageRank()`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(rs.Columns, []string{"scores"}) {
		t.Errorf("columns = %q, want [scores]", rs.Columns)
	}
	if len(rs.Profile) == 0 {
		t.Errorf("profile is empty, want at least one step")
	}
}

func TestCallStep_BareCallRefusesBoundColumnName(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	wantCallErr(t, ex, `MATCH (count:Person) CALL algo.detectCycles() RETURN count AS count`,
		"already bound", "count")
}

func TestCallStep_NoRowsStillHasNonNilColumns(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `MATCH (x:NoSuch) CALL algo.pageRank()`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.Columns == nil {
		t.Errorf("columns is nil, want an empty non-nil slice")
	}
}

func TestCallStep_PageRankRefusesBadArguments(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	wantCallErr(t, ex, `CALL algo.pageRank(-5.0, 10)`, "damping", "0")
	wantCallErr(t, ex, `CALL algo.pageRank(1.5, 10)`, "damping", "1")
	wantCallErr(t, ex, `CALL algo.pageRank(0.85, 0)`, "maxIterations")
	rs, err := runCypher(t, ex, `CALL algo.pageRank(0.85, 10)`)
	if err != nil {
		t.Fatalf("valid arguments refused: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Errorf("rows = %v, want 1", rs.Rows)
	}
}

func TestCallStep_ClauseOrderRefused(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"match-after-call",
			`CALL algo.kHop(1, 1) YIELD totalReachable MATCH (n:Person) WHERE n.uid = totalReachable RETURN n.uid AS u`,
			"put the MATCH before CALL"},
		{"optional-match-after-call",
			`CALL algo.pageRank() YIELD scores OPTIONAL MATCH (n:Person) RETURN scores AS s`,
			"put the MATCH before CALL"},
		{"call-after-create",
			`CREATE (n:Person {name:"late2"}) CALL algo.pageRank() YIELD scores RETURN scores AS s`,
			"put the CALL before CREATE"},
		{"call-after-unwind",
			`MATCH (p:Person) UNWIND p.tags AS t CALL algo.pageRank()`,
			"put the CALL before UNWIND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := NewLexer(tc.text).Tokenize()
			if err != nil {
				t.Fatalf("lex: %v", err)
			}
			_, err = NewParser(tokens).Parse()
			if err == nil {
				t.Fatalf("want parse error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestCallStep_PermittedClauseOrders(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	for _, text := range []string{
		`MATCH (p:Person) CALL algo.pageRank() YIELD scores RETURN scores AS s`,
		`CALL algo.pageRank() YIELD scores CREATE (n:Made)`,
		`CALL algo.pageRank() YIELD scores WITH scores RETURN scores AS s`,
		`MATCH (p:Person) WITH p CALL algo.pageRank()`,
	} {
		if _, err := runCypher(t, ex, text); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	// CALL ... UNWIND: UNWIND needs a property reference, so only parsing is checked.
	tokens, err := NewLexer(`CALL algo.pageRank() YIELD scores UNWIND scores.x AS s RETURN s AS s`).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if _, err := NewParser(tokens).Parse(); err != nil {
		t.Errorf("CALL ... UNWIND refused: %v", err)
	}
}

func TestCallStep_CallInFirstSegmentOfWithChain(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `CALL algo.pageRank() YIELD scores WITH scores RETURN scores AS s`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0]["s"] == nil {
		t.Errorf("rows = %v, want one row with s not nil", rs.Rows)
	}
}

func TestCallStep_ScopedToTenant(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	for tenantID, n := range map[string]int{"tenant-a": 2, "tenant-b": 5} {
		for i := 0; i < n; i++ {
			if _, err := gs.CreateNodeWithTenant(tenantID, []string{"Person"}, nil); err != nil {
				t.Fatalf("CreateNodeWithTenant: %v", err)
			}
		}
	}
	ex := NewExecutor(gs)

	tokens, err := NewLexer(`CALL algo.pageRank()`).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rs, err := ex.ExecuteWithContext(tenant.WithTenant(context.Background(), "tenant-a"), q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scores, ok := rs.Rows[0]["scores"].(map[uint64]float64)
	if !ok {
		t.Fatalf("scores = %T, want map[uint64]float64", rs.Rows[0]["scores"])
	}
	if len(scores) != 2 {
		t.Errorf("scores has %d entries, want 2 (tenant-a only)", len(scores))
	}
}
