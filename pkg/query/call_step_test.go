package query

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupCallGraph builds 6 Person nodes with KNOWS edges 0->1, 1->2, 2->0, 0->3.
// Each node carries its own storage ID as "uid" so a test can feed a MATCH-bound
// property to a procedure that wants a node ID.
func setupCallGraph(t *testing.T) (*Executor, []uint64, func()) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	ids := make([]uint64, 6)
	for i := range ids {
		n, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("P" + strconv.Itoa(i)),
		})
		if err != nil {
			cleanup()
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		ids[i] = n.ID
		if err := gs.UpdateNode(n.ID, map[string]storage.Value{"uid": storage.IntValue(int64(n.ID))}); err != nil {
			cleanup()
			t.Fatalf("UpdateNode %d: %v", i, err)
		}
	}
	for _, e := range [][2]int{{0, 1}, {1, 2}, {2, 0}, {0, 3}} {
		if _, err := gs.CreateEdge(ids[e[0]], ids[e[1]], "KNOWS", nil, 1.0); err != nil {
			cleanup()
			t.Fatalf("CreateEdge %v: %v", e, err)
		}
	}
	return NewExecutor(gs), ids, cleanup
}

// runCypher returns the error rather than failing, because most CALL tests
// assert on a refusal.
func runCypher(t *testing.T, ex *Executor, text string) (*ResultSet, error) {
	t.Helper()
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize %q: %v", text, err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("Parse %q: %v", text, err)
	}
	return ex.Execute(q)
}

func wantCallErr(t *testing.T, ex *Executor, text string, substrs ...string) {
	t.Helper()
	rs, err := runCypher(t, ex, text)
	if err == nil {
		t.Fatalf("%q: want error, got rows=%v", text, rs.Rows)
	}
	for _, s := range substrs {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%q: error %q does not contain %q", text, err, s)
		}
	}
}

func TestCallStep_UnknownProcedureRefused(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	wantCallErr(t, ex, `CALL algo.nope()`, "unknown procedure: algo.nope")
}

func TestCallStep_UnknownProcedureRefusedWithZeroInputRows(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	wantCallErr(t, ex, `MATCH (x:NoSuchLabel) CALL algo.nope()`, "unknown procedure: algo.nope")
}

func TestCallStep_BareCallReturnsProcedureColumns(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `CALL algo.pageRank()`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(rs.Columns, []string{"scores"}) {
		t.Fatalf("columns = %q, want [scores]", rs.Columns)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rs.Rows))
	}
	scores, ok := rs.Rows[0]["scores"].(map[uint64]float64)
	if !ok {
		t.Fatalf("scores = %#v (%T), want map[uint64]float64", rs.Rows[0]["scores"], rs.Rows[0]["scores"])
	}
	if len(scores) != 6 {
		t.Errorf("scores has %d entries, want 6", len(scores))
	}
}

func TestCallStep_YieldThenReturn(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `CALL algo.pageRank() YIELD scores RETURN scores AS scores`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0]["scores"] == nil {
		t.Fatalf("rows = %v, want one row with scores not nil", rs.Rows)
	}
}

func TestCallStep_YieldUnknownColumnRefused(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	wantCallErr(t, ex, `CALL algo.pageRank() YIELD nodeId RETURN nodeId AS nodeId`,
		`has no column "nodeId"`, "scores")
}

func TestCallStep_DetectCyclesColumnsSortedAndCounted(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `CALL algo.detectCycles()`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(rs.Columns, []string{"count", "cycles"}) {
		t.Fatalf("columns = %q, want [count cycles]", rs.Columns)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rs.Rows))
	}
	count, ok := rs.Rows[0]["count"].(int)
	if !ok || count < 1 {
		t.Errorf("count = %#v, want an int >= 1 for the cycle 0->1->2->0", rs.Rows[0]["count"])
	}
}

func TestCallStep_YieldNameCollidesWithBoundVariable(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	wantCallErr(t, ex, `MATCH (scores:Person) CALL algo.pageRank() YIELD scores RETURN scores AS scores`, "scores")
}

func TestCallStep_LiteralArguments(t *testing.T) {
	ex, ids, cleanup := setupCallGraph(t)
	defer cleanup()
	// Node IDs are not fixed by contract, so the literal is built from the store.
	text := `CALL algo.kHop(` + strconv.FormatUint(ids[0], 10) + `, 1) YIELD totalReachable RETURN totalReachable AS reached`
	rs, err := runCypher(t, ex, text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %v, want 1", rs.Rows)
	}
	// Node 0 reaches nodes 1 and 3 in one hop.
	if got := rs.Rows[0]["reached"]; got != 2 {
		t.Errorf("reached = %#v, want 2", got)
	}
}

func TestCallStep_ArgumentFromMatchBoundProperty(t *testing.T) {
	ex, ids, cleanup := setupCallGraph(t)
	defer cleanup()
	text := `MATCH (p:Person {uid: ` + strconv.FormatUint(ids[0], 10) + `}) CALL algo.kHop(p.uid, 1) YIELD totalReachable RETURN p.name AS name, totalReachable AS reached`
	rs, err := runCypher(t, ex, text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %v, want 1", rs.Rows)
	}
	if got := rs.Rows[0]["reached"]; got != 2 {
		t.Errorf("reached = %#v, want 2", got)
	}
	if got := rs.Rows[0]["name"]; got != "P0" {
		t.Errorf("name = %#v, want P0", got)
	}
}

func TestCallStep_WriteQueryStillReportsAffected(t *testing.T) {
	ex, _, cleanup := setupCallGraph(t)
	defer cleanup()
	rs, err := runCypher(t, ex, `CREATE (n:Person {name: "Zed"})`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(rs.Columns, []string{"affected"}) {
		t.Errorf("columns = %q, want [affected]", rs.Columns)
	}
}
