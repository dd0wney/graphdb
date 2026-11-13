package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// The run's parameters go into each projected row as it is built. A second
// pass that copied every row to add them cost 4 allocations for each row of a
// WITH chain (1.30 million against 1.70 million at 100,000 rows).
func TestWithChainParams_AllocationsStayNearTheLiteralForm(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()
	for i := 0; i < 2000; i++ {
		if _, err := gs.CreateNode([]string{"P"}, map[string]storage.Value{"k": storage.IntValue(int64(i))}); err != nil {
			t.Fatal(err)
		}
	}
	ex := NewExecutor(gs)
	qPar := parsePatternQuery(t, `MATCH (a:P) WITH a WHERE a.k > $v RETURN count(a) AS c`)
	qLit := parsePatternQuery(t, `MATCH (a:P) WITH a WHERE a.k > 1000 RETURN count(a) AS c`)
	params := map[string]any{"v": int64(1000)}

	// The two forms must also agree on the answer, or the comparison is moot.
	resPar, err := ex.ExecuteWithParams(qPar, params)
	if err != nil {
		t.Fatal(err)
	}
	resLit, err := ex.Execute(qLit)
	if err != nil {
		t.Fatal(err)
	}
	if rowSignature(resPar) != rowSignature(resLit) || resLit.Count != 1 {
		t.Fatalf("parameter rows %v, literal rows %v", resPar.Rows, resLit.Rows)
	}

	par := testing.AllocsPerRun(5, func() { _, _ = ex.ExecuteWithParams(qPar, params) })
	lit := testing.AllocsPerRun(5, func() { _, _ = ex.Execute(qLit) })
	if par > lit*1.02 {
		t.Errorf("allocations: parameter %.0f, literal %.0f (%.1f%% more); want at most 2%% more",
			par, lit, 100*(par-lit)/lit)
	}
}
