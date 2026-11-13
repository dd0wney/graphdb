package query

import "testing"

// ExecuteWithText stores the optimized plan under the query text and reuses it.
// A cached plan must keep the WHERE before the write on the first run, which
// builds it, and on the second, which reads it back.
func TestExecuteWithText_CachedPlanKeepsWhereBeforeWrite(t *testing.T) {
	const text = "MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p"

	for _, withIndex := range []bool{false, true} {
		name := "scan"
		if withIndex {
			name = "index"
		}
		t.Run(name, func(t *testing.T) {
			gs := seedIndexedWriteStore(t, withIndex)
			ex := NewExecutor(gs)
			want, wantEdges := dumpStore(t, gs)

			for run := 1; run <= 2; run++ {
				res, err := ex.ExecuteWithText(text, parseCallInput(t, text))
				if err != nil {
					t.Fatalf("run %d: %v", run, err)
				}
				got, gotEdges := dumpStore(t, gs)
				if d := diffLines(want, got); d != "" {
					t.Errorf("run %d changed the stored nodes (- before, + after)\n%s", run, d)
				}
				if d := diffLines(wantEdges, gotEdges); d != "" {
					t.Errorf("run %d changed the stored edges (- before, + after)\n%s", run, d)
				}
				if len(res.Rows) != 1 || res.Rows[0]["affected"] != 0 {
					t.Errorf("run %d rows = %v, want affected 0", run, res.Rows)
				}
			}
		})
	}
}
