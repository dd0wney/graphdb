package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A MatchStep extends each row that enters it: the row's bindings, a WITH
// segment's variables and the "$name" parameters among them, stay, and a
// variable the row already binds must be that same node. An index is an access
// path, so an IndexLookupStep must give exactly the rows its MatchStep gives.
// Before graphdb:v1.4-cypher-indexed-write-ignores-where was closed, the lookup
// built its rows from the index alone and dropped every input row.
//
// Seed data (seedIndexedWriteStore): :T nodes with pid = k = 1..6, one :U node
// with pid 1 and k 1, and the edge (T1)-[:R]->(T2). The nodes with pid 1 are
// T1 and U1, so an unlabelled indexed MATCH on pid = 1 finds two nodes.
var indexedInputRowCases = []indexedCase{
	// The first segment gives no row, so nothing may be written or returned.
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 DELETE p"},
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 DETACH DELETE p"},
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 SET p.x = 1"},
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 REMOVE p.k"},
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 CREATE (:Made {from: 7})"},
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 MERGE (:M {id: 7})"},
	{query: "MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 RETURN p.pid"},

	// A new variable after a WITH: the result is a cross product. The first
	// segment gives 0, 1 and 3 rows and the index gives 2 nodes (T1 and U1).
	{query: "MATCH (a:T) WHERE a.k > 100 WITH a MATCH (p) WHERE p.pid = 1 RETURN a.pid, p.pid"},
	{query: "MATCH (a:T) WHERE a.k = 3 WITH a MATCH (p) WHERE p.pid = 1 RETURN a.pid, p.pid"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 RETURN a.pid, p.pid"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 RETURN count(p) AS c"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 RETURN DISTINCT p.pid"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a.k AS kk MATCH (p:T) WHERE p.pid = 1 RETURN kk, p.pid"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 OPTIONAL MATCH (p)-[:R]->(q) RETURN a.pid, p.pid, q.pid"},
	{query: "MATCH (a:T) WHERE a.k = 3 WITH a MATCH (p) WHERE p.pid = 1 SET p.x = 1"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 SET p.x = 1"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 REMOVE p.k"},
	{query: "MATCH (a:T) WHERE a.k = 3 WITH a MATCH (p) WHERE p.pid = 1 DELETE p"},
	{query: "MATCH (a:T) WHERE a.k = 3 WITH a MATCH (p) WHERE p.pid = 1 DETACH DELETE p"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 CREATE (:Made {from: 7})"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (p) WHERE p.pid = 1 MERGE (:M {id: 7})"},
	{query: "MATCH (a:T) WHERE a.k = 3 WITH a MATCH (p:T) WHERE p.pid = 2 AND p.k = 2 CREATE (p)-[:L]->(a)"},
	{query: "MATCH (a:T) WHERE a.k = 3 WITH a MATCH (p:T) WHERE p.pid = 2 AND p.k > 5 CREATE (p)-[:L]->(a)"},

	// A variable that the first segment already binds: the node must be the
	// same node. The bound node has the indexed value in some cases and not in
	// others, and a :U node shares pid 1 with the bound :T node.
	{query: "MATCH (a:T) WHERE a.pid = 1 WITH a MATCH (a) WHERE a.pid = 1 AND a.k > 5 SET a.k = 0"},
	{query: "MATCH (a:T) WHERE a.pid = 1 WITH a MATCH (a) WHERE a.pid = 1 SET a.k = 0"},
	{query: "MATCH (a:T) WHERE a.pid = 1 WITH a MATCH (a) WHERE a.pid = 1 RETURN a.pid, a.k"},
	{query: "MATCH (a:T) WHERE a.k = 4 WITH a MATCH (a) WHERE a.pid = 1 SET a.x = 1"},
	{query: "MATCH (a:T) WHERE a.k = 4 WITH a MATCH (a) WHERE a.pid = 1 RETURN a.pid"},
	{query: "MATCH (a:T) WHERE a.k = 4 WITH a MATCH (a) WHERE a.pid = 1 DELETE a"},
	{query: "MATCH (a:T) WHERE a.k <= 3 WITH a MATCH (a:U) WHERE a.pid = 1 RETURN a.pid"},

	// Two MATCH clauses in one segment.
	// The parser joins both clauses into one two-pattern MATCH, which the
	// optimizer does not index, so these plans keep the scan.
	{query: "MATCH (a:T) WHERE a.k <= 2 MATCH (p) WHERE p.pid = 1 RETURN a.pid, p.pid", noIndex: true},
	{query: "MATCH (a:T) MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 SET p.x = 1", noIndex: true},

	// CALL before MATCH does not parse ("graphdb runs every MATCH before
	// CALL"), and UNWIND needs a bound variable, so no MATCH can follow either
	// of them. The case stays so the oracle covers it if the parser allows it.
	{query: "CALL algo.pageRank() YIELD scores MATCH (p:T) WHERE p.pid = 1 RETURN p.pid"},

	// Parameters. $one is the second condition: the comparison reads the
	// binding "$one", so a lost binding makes it unknown and drops every row.
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one RETURN p.pid", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one SET p.x = 1", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one REMOVE p.k", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one DELETE p", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one DETACH DELETE p", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one CREATE (:Made {from: 7})", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one MERGE (:M {id: 7})", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one RETURN count(p) AS c", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one OPTIONAL MATCH (p)-[:R]->(q) RETURN p.pid, q.pid", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one RETURN p.pid + $one AS s", params: map[string]any{"one": int64(0)}},
	// $one selects no row, so no write may happen.
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > $one DELETE p", params: map[string]any{"one": int64(5)}},
	// The parameter is the SET value.
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 SET p.k = $v", params: map[string]any{"v": int64(9)}},
	{query: "MATCH (p:T) WHERE p.pid = 1 AND p.k > 0 SET p.x = $v", params: map[string]any{"v": "hello"}},
	// The parameter is in the equality. The optimizer indexes only a literal,
	// so these plans keep the scan; the oracle still compares the stores.
	{query: "MATCH (p:T) WHERE p.pid = $v AND p.k > 0 RETURN p.pid", params: map[string]any{"v": int64(1)}, noIndex: true},
	{query: "MATCH (p:T) WHERE p.pid = $v AND p.k > 0 SET p.x = 1", params: map[string]any{"v": int64(1)}, noIndex: true},
	// A parameter and a WITH chain together.
	{query: "MATCH (a:T) WHERE a.k <= 2 WITH a MATCH (p:T) WHERE p.pid = 1 AND p.k > $one RETURN a.pid, p.pid", params: map[string]any{"one": int64(0)}},
	{query: "MATCH (a:T) WHERE a.k <= 2 WITH a MATCH (p:T) WHERE p.pid = 1 AND p.k > $one SET p.x = a.k", params: map[string]any{"one": int64(0)}},
}

// IndexLookupStep.Execute must start from the rows it receives, as a MatchStep
// does, and so give no row when it receives none.
func TestIndexLookupStep_NoInputRowsGiveNoRows(t *testing.T) {
	gs := seedIndexedWriteStore(t, true)
	step := &IndexLookupStep{
		propertyKey: "pid",
		value:       storage.IntValue(1),
		variable:    "p",
		labels:      []string{"T"},
	}
	ec := newExecutionContext(context.Background(), gs, PathOptions{})
	ec.results = nil
	if err := step.Execute(ec); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ec.results) != 0 {
		t.Errorf("got %d rows from no input rows, want 0", len(ec.results))
	}
}

// A node with pid stored as the float 1.0 is not in an int index, so the
// indexed plan misses a row that the scan finds. The two plans disagree on a
// value of another type; this is not the filter or input-row defect. The test
// records the gap and starts failing when it closes, so the skip is removed
// with the task.
func TestIndexedEquality_IntAgainstFloatKnownGap(t *testing.T) {
	const query = "MATCH (p) WHERE p.pid = 1 RETURN p.name"
	run := func(withIndex bool) []string {
		gs, cleanup := setupExecutorTestGraph(t)
		t.Cleanup(cleanup)
		if withIndex {
			if err := gs.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
				t.Fatalf("CreatePropertyIndex: %v", err)
			}
		}
		for name, v := range map[string]storage.Value{"int": storage.IntValue(1), "float": storage.FloatValue(1.0)} {
			if _, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{"pid": v, "name": storage.StringValue(name)}); err != nil {
				t.Skipf("the store refuses a %s pid beside an int index: %v", name, err)
			}
		}
		res, err := NewExecutor(gs).Execute(parseCallInput(t, query))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		var rows []string
		for _, r := range res.Rows {
			rows = append(rows, fmt.Sprint(r))
		}
		return rows
	}
	scan, indexed := run(false), run(true)
	if fmt.Sprint(scan) == fmt.Sprint(indexed) {
		t.Fatalf("scan and index agree (%v): the gap v1.4-cypher-index-equality-int-float is closed, so remove this test and the task", scan)
	}
	t.Skipf("known gap v1.4-cypher-index-equality-int-float: scan rows %v, index rows %v", scan, indexed)
}
