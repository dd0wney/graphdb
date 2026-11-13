package query

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A property index is an access path, not a semantics change. A write query
// that an index serves must change exactly the rows that the same query changes
// with no index. Before this fix applyFilterPushdown moved the FilterStep to
// the end of the plan whenever an IndexLookupStep replaced the MatchStep, so
// the write ran on every row the index returned and the WHERE ran after it.
//
// Each case runs on two stores that hold the same data, one with an index on
// "pid" and one without. The test compares the result rows, the affected count
// and the stored nodes and edges. It reads the store through the storage API,
// not through Cypher, so the check does not depend on the code under test.

type indexedWriteOutcome struct {
	rows     []string
	affected string
	nodes    []string
	edges    []string
	usedIdx  bool
	err      string
}

// seedIndexedWriteStore builds six :T nodes with pid 1..6 and k 1..6, one :U
// node that shares pid 1 (the label a query does not match), and one edge so
// DETACH DELETE has relationships to remove.
func seedIndexedWriteStore(t *testing.T, withIndex bool) *storage.GraphStorage {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	if withIndex {
		if err := gs.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
			t.Fatalf("CreatePropertyIndex: %v", err)
		}
	}
	var first, second *storage.Node
	for i := 1; i <= 6; i++ {
		name := "y"
		if i%2 == 1 {
			name = "x"
		}
		n, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{
			"pid":  storage.IntValue(int64(i)),
			"k":    storage.IntValue(int64(i)),
			"name": storage.StringValue(name),
		})
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		switch i {
		case 1:
			first = n
		case 2:
			second = n
		}
	}
	if _, err := gs.CreateNode([]string{"U"}, map[string]storage.Value{
		"pid": storage.IntValue(1), "k": storage.IntValue(1),
	}); err != nil {
		t.Fatalf("CreateNode U: %v", err)
	}
	if _, err := gs.CreateEdge(first.ID, second.ID, "R", map[string]storage.Value{}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	return gs
}

func renderProps(props map[string]storage.Value) string {
	parts := make([]string, 0, len(props))
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s=%d:%s", k, v.Type, v.String()))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func dumpStore(t *testing.T, gs *storage.GraphStorage) (nodes, edges []string) {
	t.Helper()
	for _, n := range gs.GetAllNodesAcrossTenants() {
		labels := append([]string(nil), n.Labels...)
		sort.Strings(labels)
		nodes = append(nodes, fmt.Sprintf("node %d %v {%s}", n.ID, labels, renderProps(n.Properties)))
	}
	sort.Strings(nodes)
	es, err := gs.GetAllEdgesForTenant("default")
	if err != nil {
		t.Fatalf("GetAllEdgesForTenant: %v", err)
	}
	for _, e := range es {
		edges = append(edges, fmt.Sprintf("edge %d %d-[%s]->%d {%s}", e.ID, e.FromNodeID, e.Type, e.ToNodeID, renderProps(e.Properties)))
	}
	sort.Strings(edges)
	return nodes, edges
}

func runIndexedWrite(t *testing.T, withIndex bool, text string) indexedWriteOutcome {
	t.Helper()
	gs := seedIndexedWriteStore(t, withIndex)
	ex := NewExecutor(gs)
	parsed := parseCallInput(t, text)

	var out indexedWriteOutcome
	plan := NewOptimizer(gs).Optimize(ex.buildExecutionPlan(parsed), parsed)
	for _, s := range plan.Steps {
		if _, ok := s.(*IndexLookupStep); ok {
			out.usedIdx = true
		}
	}

	res, err := ex.Execute(parseCallInput(t, text))
	if err != nil {
		out.err = err.Error()
	} else {
		for _, r := range res.Rows {
			if a, ok := r["affected"]; ok && len(res.Columns) == 1 && res.Columns[0] == "affected" {
				out.affected = fmt.Sprint(a)
				continue
			}
			out.rows = append(out.rows, fmt.Sprint(r))
		}
		sort.Strings(out.rows)
	}
	out.nodes, out.edges = dumpStore(t, gs)
	return out
}

var indexedWriteCases = []string{
	// The WHERE is false for the one node the index returns: no write may happen.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 SET p.x = p.k + 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 REMOVE p.k",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DETACH DELETE p",
	// The WHERE is true: the write must happen on both plans.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 SET p.x = p.k + 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 REMOVE p.k",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 DETACH DELETE p",
	// The write changes the property that the WHERE reads.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 SET p.k = 9",
	// The indexed equality is on the right.
	"MATCH (p:T) WHERE p.k > 5 AND p.pid = 1 SET p.x = 1",
	"MATCH (p:T) WHERE p.k > 5 AND p.pid = 1 DELETE p",
	// Three conditions, and a negation.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 AND p.name = 'x' DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 AND p.name = 'x' SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k = 1 DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k = 2 DELETE p",
	// Other write clauses after a MATCH.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 CREATE (:Made {from: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 CREATE (:Made {from: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 MERGE (:M {id: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 MERGE (:M {id: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 WITH p SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 WITH p SET p.x = 1",
	// CALL runs before the filter in the unoptimised plan order too, so a write
	// after it shows whether the filter kept its place.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 CALL algo.pageRank() YIELD scores CREATE (:Made {from: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 CALL algo.pageRank() YIELD scores CREATE (:Made {from: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 CALL algo.kHop(p.pid, 1) YIELD totalReachable RETURN totalReachable AS r",
	// Reads: the rows must not depend on the index either.
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN count(p) AS c",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN DISTINCT p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN p.pid ORDER BY p.pid LIMIT 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 OPTIONAL MATCH (p)-[:R]->(q) RETURN p.pid, q.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k = 1 OPTIONAL MATCH (p)-[:R]->(q) RETURN p.pid, q.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 WITH p RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 WITH count(p) AS c RETURN c",
}

func TestIndexedWrite_SameEffectWithAndWithoutIndex(t *testing.T) {
	for _, text := range indexedWriteCases {
		t.Run(text, func(t *testing.T) {
			// A form that does not parse proves nothing; say so instead of passing.
			if toks, lerr := NewLexer(text).Tokenize(); lerr != nil {
				t.Skipf("does not lex: %v", lerr)
			} else if _, perr := NewParser(toks).Parse(); perr != nil {
				t.Skipf("does not parse: %v", perr)
			}

			want := runIndexedWrite(t, false, text)
			got := runIndexedWrite(t, true, text)

			if want.usedIdx {
				t.Fatalf("the store with no index used an IndexLookupStep")
			}
			if !got.usedIdx {
				t.Fatalf("the indexed store did not use an IndexLookupStep, so the case proves nothing")
			}
			if want.err != "" {
				t.Fatalf("the query fails with no index, so the case proves nothing: %s", want.err)
			}
			if got.err != "" {
				t.Fatalf("the query fails with the index: %s", got.err)
			}
			if fmt.Sprint(got.rows) != fmt.Sprint(want.rows) {
				t.Errorf("rows differ\n  no index: %v\n  index:    %v", want.rows, got.rows)
			}
			if got.affected != want.affected {
				t.Errorf("affected differs: no index %q, index %q", want.affected, got.affected)
			}
			if d := diffLines(want.nodes, got.nodes); d != "" {
				t.Errorf("stored nodes differ (- no index, + index)\n%s", d)
			}
			if d := diffLines(want.edges, got.edges); d != "" {
				t.Errorf("stored edges differ (- no index, + index)\n%s", d)
			}
		})
	}
}

func diffLines(want, got []string) string {
	count := map[string]int{}
	for _, l := range want {
		count[l]++
	}
	for _, l := range got {
		count[l]--
	}
	var b strings.Builder
	for _, l := range want {
		if count[l] > 0 {
			fmt.Fprintf(&b, "  - %s\n", l)
		}
	}
	for _, l := range got {
		if count[l] < 0 {
			fmt.Fprintf(&b, "  + %s\n", l)
		}
	}
	return b.String()
}
