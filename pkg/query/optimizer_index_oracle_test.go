package query

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A property index is an access path, not a semantics change: every query must
// return the same rows whether or not an index exists on the property it
// filters on. Each case runs against two otherwise identical graphs, one with
// an index on "pid" and one without, and compares the rows.
func TestIndexSelection_SameRowsWithAndWithoutIndex(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"single node", "MATCH (a:P) WHERE a.pid = 1 RETURN a.name"},
		{"relationship expansion", "MATCH (a:P)-[:R]->(b:Q) WHERE a.pid = 1 RETURN a.name, b.name"},
		// At 5988924 the scan also leaves b unbound (graphdb:v1.4-cypher-multiple-match-clauses),
		// so this case cannot fail yet. It keeps the two paths together once that is fixed.
		{"second pattern", "MATCH (a:P), (b:Q) WHERE a.pid = 1 RETURN a.name, b.name"},
		{"inline property map", "MATCH (a:P {name: 'p1'}) WHERE a.pid = 1 RETURN a.name"},
		{"value of another type", "MATCH (a:P) WHERE a.pid = 'one' RETURN a.name"},
		{"label filter", "MATCH (a:Q) WHERE a.pid = 1 RETURN a.name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, wantErr := runIndexOracleQuery(t, false, tc.query)
			got, gotErr := runIndexOracleQuery(t, true, tc.query)
			if wantErr != nil {
				t.Fatalf("query fails without an index, so the case proves nothing: %v", wantErr)
			}
			if gotErr != nil {
				t.Fatalf("with index: %v; without index the query returns %v", gotErr, want)
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("rows differ\n  without index: %v\n  with index:    %v", want, got)
			}
		})
	}
}

// The oracle above passes when the index is never used at all, so it cannot
// tell a fix from disabling the optimization. This test pins which shapes keep
// the index lookup and which fall back to the scan.
func TestIndexSelection_UsesLookupOnlyForSingleNodePattern(t *testing.T) {
	cases := []struct {
		query      string
		wantLookup bool
	}{
		{"MATCH (a:P) WHERE a.pid = 1 RETURN a.name", true},
		{"MATCH (a) WHERE a.pid = 1 RETURN a.name", true},
		{"MATCH (a:P) WHERE a.pid = 1 AND a.name = 'p1' RETURN a.name", true},
		{"MATCH (a:P)-[:R]->(b:Q) WHERE a.pid = 1 RETURN b.name", false},
		{"MATCH (a:P), (b:Q) WHERE a.pid = 1 RETURN a.name, b.name", false},
		{"MATCH (a:P {name: 'p1'}) WHERE a.pid = 1 RETURN a.name", false},
		{"MATCH (a:P) WHERE a.pid = 'one' RETURN a.name", false},
	}

	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	if err := gs.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	executor := NewExecutor(gs)

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			parsed := parseCallInput(t, tc.query)
			plan := NewOptimizer(gs).Optimize(executor.buildExecutionPlan(parsed), parsed)

			gotLookup := false
			for _, step := range plan.Steps {
				if _, ok := step.(*IndexLookupStep); ok {
					gotLookup = true
				}
			}
			if gotLookup != tc.wantLookup {
				t.Errorf("IndexLookupStep in plan = %v, want %v", gotLookup, tc.wantLookup)
			}
		})
	}
}

// runIndexOracleQuery builds the fixture graph, optionally indexes "pid", runs
// query, and returns its rows rendered and sorted so row order cannot matter.
func runIndexOracleQuery(t *testing.T, withIndex bool, query string) ([]string, error) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)

	if withIndex {
		if err := gs.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
			t.Fatalf("CreatePropertyIndex: %v", err)
		}
	}

	// Two P nodes share pid 1 so the inline-map case can tell them apart; a Q
	// node also carries pid 1 so the label filter has something to reject.
	p1 := mustOracleNode(t, gs, "P", map[string]storage.Value{"pid": storage.IntValue(1), "name": storage.StringValue("p1")})
	mustOracleNode(t, gs, "P", map[string]storage.Value{"pid": storage.IntValue(1), "name": storage.StringValue("p2")})
	mustOracleNode(t, gs, "P", map[string]storage.Value{"pid": storage.IntValue(2), "name": storage.StringValue("p3")})
	q1 := mustOracleNode(t, gs, "Q", map[string]storage.Value{"pid": storage.IntValue(1), "name": storage.StringValue("q1")})
	if _, err := gs.CreateEdge(p1.ID, q1.ID, "R", map[string]storage.Value{}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	result, err := NewExecutor(gs).Execute(parseCallInput(t, query))
	if err != nil {
		return nil, err
	}

	rows := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		rows = append(rows, fmt.Sprint(row))
	}
	sort.Strings(rows)
	return rows, nil
}

func mustOracleNode(t *testing.T, gs *storage.GraphStorage, label string, props map[string]storage.Value) *storage.Node {
	t.Helper()
	n, err := gs.CreateNode([]string{label}, props)
	if err != nil {
		t.Fatalf("CreateNode(%s): %v", label, err)
	}
	return n
}
