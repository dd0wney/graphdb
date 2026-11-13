package query

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// planOrderQueries covers reads and writes, with and without a WHERE, with one
// and with several conditions. Every one is parsed and planned on a store with
// an index on "pid" and on a store with none.
var planOrderQueries = []string{
	"MATCH (p:T) RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN p.pid",
	"MATCH (p:T) WHERE p.k > 5 AND p.pid = 1 RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 AND p.name = 'x' RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k = 1 RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN count(p) AS c",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN DISTINCT p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 RETURN p.pid ORDER BY p.pid LIMIT 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 OPTIONAL MATCH (p)-[:R]->(q) RETURN p.pid, q.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 WITH p RETURN p.pid",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 WITH p SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 CALL algo.kHop(p.pid, 1) YIELD totalReachable RETURN totalReachable AS r",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 CALL algo.pageRank() YIELD scores CREATE (:Made {from: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 SET p.x = p.k + 1",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 REMOVE p.k",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DETACH DELETE p",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 CREATE (:Made {from: 7})",
	"MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 MERGE (:M {id: 7})",
	"MATCH (p:T) WHERE p.pid = 1 SET p.x = 1",
	"MATCH (p:T) WHERE p.pid = 1 DELETE p",
	"MATCH (p:T) WHERE p.k > 5 SET p.x = 1",
	"MATCH (p:T) WHERE p.k > 5 DELETE p",
	"MATCH (p:T) SET p.x = 1",
	"MATCH (p:T) DELETE p",
	"MATCH (p:T)-[:R]->(q:T) WHERE p.pid = 1 AND q.k > 5 SET q.x = 1",
	"MATCH (p:T)-[:R]->(q:T) WHERE p.pid = 1 AND q.k > 5 DELETE q",
	"MATCH (p:T {k: 1}) WHERE p.pid = 1 AND p.k > 5 DELETE p",
	"MATCH (p:T), (q:T) WHERE p.pid = 1 AND q.k > 5 SET q.x = 1",
	"MATCH (p) WHERE p.pid = 1 AND p.k > 5 DELETE p",
	"MATCH (p:T) WHERE p.pid = 'one' AND p.k > 5 DELETE p",
	"CREATE (:Made {from: 7})",
	"CALL algo.pageRank() YIELD scores RETURN scores AS s",
}

// The optimizer must never move a FilterStep to a place after a step that came
// after it in the plan that buildExecutionPlan made. The test runs the passes
// without the guard in Optimize, so a bad pass shows here rather than being
// hidden by the fallback.
func TestOptimizerPasses_KeepFilterOrder(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		gs := seedIndexedWriteStore(t, withIndex)
		ex := NewExecutor(gs)
		for _, text := range planOrderQueries {
			name := text
			if withIndex {
				name = "index/" + text
			} else {
				name = "scan/" + text
			}
			t.Run(name, func(t *testing.T) {
				toks, lerr := NewLexer(text).Tokenize()
				if lerr != nil {
					t.Skipf("does not lex: %v", lerr)
				}
				parsed, perr := NewParser(toks).Parse()
				if perr != nil {
					t.Skipf("does not parse: %v", perr)
				}
				built := ex.buildExecutionPlan(parsed)
				optimized := NewOptimizer(gs).runPasses(built, parsed)
				if err := filterOrderViolation(built, optimized); err != nil {
					t.Errorf("%v\n  built:     %s\n  optimized: %s", err, planNames(built), planNames(optimized))
				}
			})
		}
	}
}

func planNames(p *ExecutionPlan) string {
	names := make([]string, 0, len(p.Steps))
	for _, s := range p.Steps {
		names = append(names, strings.TrimPrefix(fmt.Sprintf("%T", s), "*query."))
	}
	return strings.Join(names, " ")
}

// The built-in passes must leave the plan in the order buildExecutionPlan made
// it, step for step, apart from an access step that an index replaced.
func TestOptimizerPasses_VectorAndIndexKeepFilterBeforeWrite(t *testing.T) {
	gs := seedIndexedWriteStore(t, true)
	ex := NewExecutor(gs)
	ex.SetVectorSearch(
		func(a, b []float32) (float64, error) { s, err := vector.CosineSimilarity(a, b); return float64(s), err },
		func(string, []float32, int, int) ([]VectorSearchResult, error) { return nil, nil },
		func(p string) bool { return p == "embedding" },
		func(id uint64) (any, error) { return gs.GetNode(id) },
	)
	for _, text := range []string{
		"MATCH (p:T) WHERE vector.similarity(p.embedding, $q) > 0.8 AND p.k > 5 SET p.x = 1",
		"MATCH (p:T) WHERE vector.similarity(p.embedding, $q) > 0.8 AND p.pid = 1 AND p.k > 5 SET p.x = 1",
		"MATCH (p:T) WHERE vector.similarity(p.embedding, $q) > 0.8 AND p.pid = 1 AND p.k > 5 DELETE p",
	} {
		t.Run(text, func(t *testing.T) {
			parsed := parseCallInput(t, text)
			built := ex.buildExecutionPlan(parsed)
			optimized := ex.optimizer.runPasses(built, parsed)
			if err := filterOrderViolation(built, optimized); err != nil {
				t.Errorf("%v\n  built:     %s\n  optimized: %s", err, planNames(built), planNames(optimized))
			}
		})
	}
}

func TestFilterOrderViolation(t *testing.T) {
	match := &MatchStep{}
	filter := &FilterStep{}
	write := &SetStep{}
	del := &DeleteStep{}
	ret := &ReturnStep{}
	lookup := &IndexLookupStep{}
	vec := &VectorSearchStep{}
	input := &ExecutionPlan{Steps: []ExecutionStep{match, filter, write, ret}}

	cases := []struct {
		name    string
		after   []ExecutionStep
		wantErr string // empty means the plan is acceptable
	}{
		{"unchanged", []ExecutionStep{match, filter, write, ret}, ""},
		{"index replaces the match", []ExecutionStep{lookup, filter, write, ret}, ""},
		{"vector step and index replace the match", []ExecutionStep{vec, lookup, filter, write, ret}, ""},
		{"filter moved to the end", []ExecutionStep{lookup, write, ret, filter}, "came after the FilterStep"},
		{"filter moved after the write only", []ExecutionStep{lookup, write, filter, ret}, "came after the FilterStep"},
		{"filter moved before the access step", []ExecutionStep{filter, match, write, ret}, "came before the FilterStep"},
		{"filter dropped", []ExecutionStep{lookup, write, ret}, "missing"},
		{"write replaced by another step", []ExecutionStep{lookup, filter, del, ret}, "missing"},
		{"step added after the filter", []ExecutionStep{lookup, filter, vec, write, ret}, "added"},
		{"filter twice", []ExecutionStep{lookup, filter, filter, write, ret}, "twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := filterOrderViolation(input, &ExecutionPlan{Steps: tc.after})
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("rejected an acceptable plan: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("accepted a plan that breaks the order, want an error containing %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}

	// The row-neutral list holds ReturnStep and nothing else. A write that
	// swaps places with a filter changes which rows it writes, or which rows
	// the filter sees, so the guard must refuse it.
	t.Run("a write moved from before a filter to after it", func(t *testing.T) {
		in := &ExecutionPlan{Steps: []ExecutionStep{match, write, filter, ret}}
		err := filterOrderViolation(in, &ExecutionPlan{Steps: []ExecutionStep{match, filter, write, ret}})
		if err == nil || !strings.Contains(err.Error(), "came before the FilterStep") {
			t.Errorf("error = %v, want a refusal that the write came before the filter", err)
		}
	})

	t.Run("a pass lifts a filter above a SetStep", func(t *testing.T) {
		in := &ExecutionPlan{Steps: []ExecutionStep{match, write, filter}}
		opt := &Optimizer{}
		got := opt.applyFilterPushdown(in, &Query{})
		if err := filterOrderViolation(in, got); err != nil {
			t.Errorf("the pass broke the order: %v", err)
		}
		if got.Steps[1] != ExecutionStep(write) || got.Steps[2] != ExecutionStep(filter) {
			t.Errorf("applyFilterPushdown reordered %s, want the SetStep to stay before the FilterStep", planNames(got))
		}
	})

	// An optimizer may add or swap in an access step. It may not add anything
	// else, even before the first filter.
	t.Run("a write added before the first filter", func(t *testing.T) {
		err := filterOrderViolation(input, &ExecutionPlan{Steps: []ExecutionStep{del, match, filter, write, ret}})
		if err == nil || !strings.Contains(err.Error(), "not an access step") {
			t.Errorf("error = %v, want a refusal of a write that was added", err)
		}
	})

	t.Run("a write swapped in for the access step", func(t *testing.T) {
		err := filterOrderViolation(input, &ExecutionPlan{Steps: []ExecutionStep{del, filter, write, ret}})
		if err == nil || !strings.Contains(err.Error(), "not an access step") {
			t.Errorf("error = %v, want a refusal of a write put in place of the MatchStep", err)
		}
	})

	t.Run("a filter may be lifted above a ReturnStep", func(t *testing.T) {
		in := &ExecutionPlan{Steps: []ExecutionStep{match, ret, filter}}
		if err := filterOrderViolation(in, &ExecutionPlan{Steps: []ExecutionStep{match, filter, ret}}); err != nil {
			t.Errorf("rejected a lift above a ReturnStep: %v", err)
		}
	})

	t.Run("a plan with no filter has nothing to keep", func(t *testing.T) {
		in := &ExecutionPlan{Steps: []ExecutionStep{match, write}}
		if err := filterOrderViolation(in, &ExecutionPlan{Steps: []ExecutionStep{write, lookup}}); err != nil {
			t.Errorf("error for a plan with no filter: %v", err)
		}
	})
}

func moveFiltersToEnd(plan *ExecutionPlan, _ *Query) *ExecutionPlan {
	out := &ExecutionPlan{}
	var filters []ExecutionStep
	for _, s := range plan.Steps {
		if _, ok := s.(*FilterStep); ok {
			filters = append(filters, s)
			continue
		}
		out.Steps = append(out.Steps, s)
	}
	out.Steps = append(out.Steps, filters...)
	return out
}

// When a pass breaks the filter order, Optimize returns the plan it received
// and writes one log line that names the violation.
func TestOptimize_FallsBackWhenAPassBreaksFilterOrder(t *testing.T) {
	gs := seedIndexedWriteStore(t, false)
	ex := NewExecutor(gs)
	text := "MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p"
	parsed := parseCallInput(t, text)
	built := ex.buildExecutionPlan(parsed)

	opt := NewOptimizer(gs)
	opt.extraPasses = []optimizerPass{moveFiltersToEnd}

	var logs bytes.Buffer
	restore := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(restore) })

	got := opt.Optimize(built, parsed)

	if got != built {
		t.Fatalf("Optimize returned a different plan (%s), want the plan it received (%s)", planNames(got), planNames(built))
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "FilterStep") {
		t.Errorf("want one log line that names the FilterStep violation, got %q", logs.String())
	}
}

// The same fallback, seen through the executor: with a broken pass and an
// index, the DELETE must still leave the node that the WHERE rejects.
func TestExecute_BrokenPassStillRunsWhereBeforeWrite(t *testing.T) {
	gs := seedIndexedWriteStore(t, true)
	ex := NewExecutor(gs)
	ex.optimizer.extraPasses = []optimizerPass{moveFiltersToEnd}
	restore := log.Writer()
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(restore) })

	before, _ := dumpStore(t, gs)
	text := "MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p"
	if _, err := ex.Execute(parseCallInput(t, text)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	after, _ := dumpStore(t, gs)
	if d := diffLines(before, after); d != "" {
		t.Errorf("the store changed (- before, + after)\n%s", d)
	}
}
