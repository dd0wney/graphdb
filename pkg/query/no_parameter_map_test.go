package query

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// A query that names a "$name" parameter must be refused when the caller gives
// no value for it, through each entry point, before any part of the query runs.
// That includes a run with no parameter map at all. On v1.12.0 such a run read
// the parameter as null, and "$v IS NULL DELETE p" deleted the node.
//
// Each form is a write, or has a write before the place of the parameter, so
// the store comparison can fail. The forms put the parameter in each place
// where the parser accepts one.
var noMapParameterForms = []string{
	"MATCH (p:T) WHERE p.pid = 1 AND $v IS NULL DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT ($v IS NOT NULL) DETACH DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k = $v DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k IN $v DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k IN [1, $v] DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k < $v + 1 SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 SET p.k = $v",
	"MATCH (p:T) WHERE p.pid = 1 SET p.k = $v + 1",
	"MATCH (p:T) WHERE p.pid = 1 SET p.l = [1, $v]",
	"MATCH (p:T) WHERE p.pid = 1 WITH p, $v AS w WHERE w IS NULL DETACH DELETE p",
	"MATCH (p:T {pid: $v}) DELETE p",
	"CREATE (:Made {v: $v})",
	"MERGE (m:M {id: $v})",
	"MERGE (m:M {id: 1}) ON CREATE SET m.v = $v",
	"MATCH (p:T) WHERE p.pid = 1 OPTIONAL MATCH (p)-[:R]->(q {pid: $v}) WITH p, q WHERE q IS NULL DETACH DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 OPTIONAL MATCH (p)-[:R]->(q) WHERE q.k = $v WITH p, q WHERE q IS NULL DETACH DELETE p",
	"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 2 AND $v IS NULL DELETE p",
	"MATCH (a:T) WHERE a.k = 1 SET a.x = 1 WITH a MATCH (p:T) WHERE p.pid = 2 AND $v IS NULL DELETE p",
	"MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 2 AND $v IS NULL DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = 2 AND $v IS NULL DETACH DELETE q RETURN q.pid AS r",
	"MATCH (p:T) WHERE p.pid = 1 DELETE p RETURN $v AS x",
	"MATCH (p:T) WHERE p.k < 3 SET p.x = 1 RETURN p.pid ORDER BY $v",
	"MATCH (p:T) WHERE p.pid = 1 AND CASE WHEN $v IS NULL THEN true ELSE false END DELETE p",
	"PROFILE MATCH (p:T) WHERE p.pid = 1 AND $v IS NULL DELETE p",
}

// noMapEntryPoints are the ways to run a parsed query with no value for "$v".
var noMapEntryPoints = map[string]func(ex *Executor, q *Query, text string) (*ResultSet, error){
	"Execute": func(ex *Executor, q *Query, _ string) (*ResultSet, error) { return ex.Execute(q) },
	"ExecuteWithContext": func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
		return ex.ExecuteWithContext(context.Background(), q)
	},
	"ExecuteWithOptions": func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
		return ex.ExecuteWithOptions(context.Background(), q, PathOptions{})
	},
	"ExecuteWithText": func(ex *Executor, q *Query, text string) (*ResultSet, error) {
		return ex.ExecuteWithText(text, q)
	},
	// The second run reads the plan cache.
	"ExecuteWithText second run": func(ex *Executor, q *Query, text string) (*ResultSet, error) {
		_, _ = ex.ExecuteWithText(text, q)
		return ex.ExecuteWithText(text, q)
	},
	"ExecuteWithParams nil map": func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
		return ex.ExecuteWithParams(q, nil)
	},
	"ExecuteWithParams empty map": func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
		return ex.ExecuteWithParams(q, map[string]any{})
	},
	"ExecuteWithParams other name": func(ex *Executor, q *Query, _ string) (*ResultSet, error) {
		return ex.ExecuteWithParams(q, map[string]any{"w": int64(1)})
	},
}

func TestNoParameterMap_EachEntryPointRefusesAndWritesNothing(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		for _, form := range noMapParameterForms {
			for entry, run := range noMapEntryPoints {
				name := entry + "/" + form
				if withIndex {
					name += " [index]"
				}
				t.Run(name, func(t *testing.T) {
					gs := seedIndexedWriteStore(t, withIndex)
					nodesBefore, edgesBefore := dumpStore(t, gs)
					_, err := run(NewExecutor(gs), parseCallInput(t, form), form)
					if err == nil || !strings.Contains(err.Error(), "missing parameter: $v") {
						t.Errorf("want the refusal \"missing parameter: $v\", got error %v", err)
					}
					nodesAfter, edgesAfter := dumpStore(t, gs)
					if !reflect.DeepEqual(nodesBefore, nodesAfter) || !reflect.DeepEqual(edgesBefore, edgesAfter) {
						t.Errorf("the query changed the store\nbefore: %v %v\nafter:  %v %v", nodesBefore, edgesBefore, nodesAfter, edgesAfter)
					}
				})
			}
		}
	}
}

// The store comparison above is a negative result, so it needs a control: the
// same forms with a value for the parameter must change the store in some
// cases. If no form writes, "writes nothing" proves nothing.
func TestNoParameterMap_ControlTheFormsCanWrite(t *testing.T) {
	changed := 0
	for _, form := range noMapParameterForms {
		gs := seedIndexedWriteStore(t, false)
		nodesBefore, edgesBefore := dumpStore(t, gs)
		_, _ = NewExecutor(gs).ExecuteWithParams(parseCallInput(t, form), map[string]any{"v": int64(2)})
		nodesAfter, edgesAfter := dumpStore(t, gs)
		if !reflect.DeepEqual(nodesBefore, nodesAfter) || !reflect.DeepEqual(edgesBefore, edgesAfter) {
			changed++
		}
	}
	if changed < len(noMapParameterForms)/2 {
		t.Fatalf("only %d of %d forms change the store when the parameter has a value", changed, len(noMapParameterForms))
	}
}

// EXPLAIN runs nothing, so it returns its plan and does not refuse the missing
// parameter. The store must not change.
func TestNoParameterMap_ExplainRunsNothing(t *testing.T) {
	const form = "EXPLAIN MATCH (p:T) WHERE p.pid = 1 AND $v IS NULL DELETE p"
	for entry, run := range noMapEntryPoints {
		t.Run(entry, func(t *testing.T) {
			gs := seedIndexedWriteStore(t, true)
			nodesBefore, edgesBefore := dumpStore(t, gs)
			if _, err := run(NewExecutor(gs), parseCallInput(t, form), form); err != nil {
				t.Errorf("EXPLAIN gave an error: %v", err)
			}
			nodesAfter, edgesAfter := dumpStore(t, gs)
			if !reflect.DeepEqual(nodesBefore, nodesAfter) || !reflect.DeepEqual(edgesBefore, edgesAfter) {
				t.Errorf("EXPLAIN changed the store")
			}
		})
	}
}
