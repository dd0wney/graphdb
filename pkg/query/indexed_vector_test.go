package query

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vector"
)

// A vector.similarity condition reads the "$q" binding. When a pid index also
// serves the query, the lookup used to drop that binding, so the similarity was
// unknown for every row: a write changed nothing and a read gave no row.
func TestIndexedVector_SameRowsAndEffectWithAndWithoutIndex(t *testing.T) {
	type outcome struct {
		rows     []string
		affected string
		nodes    []string
		usedIdx  bool
	}
	run := func(t *testing.T, withIndex bool, text string) outcome {
		t.Helper()
		gs, cleanup := setupExecutorTestGraph(t)
		t.Cleanup(cleanup)
		if err := gs.CreateVectorIndex("embedding", 3, 4, 50, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndex: %v", err)
		}
		if withIndex {
			if err := gs.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
				t.Fatalf("CreatePropertyIndex: %v", err)
			}
		}
		// Three concepts with pid 1 and similarity to [1,0,0] above 0.5, one
		// with pid 1 below it, and one with another pid.
		for i, e := range [][]float32{{0.9, 0.1, 0}, {0.7, 0.7, 0}, {0.8, 0.2, 0}, {0, 0, 1}, {0.9, 0.1, 0}} {
			pid := int64(1)
			if i == 4 {
				pid = 2
			}
			n, err := gs.CreateNode([]string{"C"}, map[string]storage.Value{
				"pid": storage.IntValue(pid), "embedding": storage.VectorValue(e),
			})
			if err != nil {
				t.Fatalf("CreateNode: %v", err)
			}
			if err := gs.UpdateNodeVectorIndexes(n); err != nil {
				t.Fatalf("UpdateNodeVectorIndexes: %v", err)
			}
		}
		ex := NewExecutor(gs)
		wireVectorSearch(t, gs, ex)

		var o outcome
		o.usedIdx = usesIndexLookup(gs, ex, parseCallInput(t, text))
		res, err := ex.ExecuteWithParams(parseCallInput(t, text), map[string]any{"q": []float32{1, 0, 0}})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for _, r := range res.Rows {
			if a, ok := r["affected"]; ok && len(res.Columns) == 1 && res.Columns[0] == "affected" {
				o.affected = fmt.Sprint(a)
				continue
			}
			o.rows = append(o.rows, fmt.Sprint(r))
		}
		sort.Strings(o.rows)
		o.nodes, _ = dumpStore(t, gs)
		return o
	}

	for _, text := range []string{
		"MATCH (c:C) WHERE vector.similarity(c.embedding, $q) > 0.5 AND c.pid = 1 SET c.hit = 1",
		"MATCH (c:C) WHERE c.pid = 1 AND vector.similarity(c.embedding, $q) > 0.5 SET c.hit = 1",
		"MATCH (c:C) WHERE vector.similarity(c.embedding, $q) > 0.5 AND c.pid = 1 DELETE c",
		"MATCH (c:C) WHERE vector.similarity(c.embedding, $q) > 0.5 AND c.pid = 1 RETURN c.pid",
		"MATCH (c:C) WHERE vector.similarity(c.embedding, $q) > 0.5 AND c.pid = 1 RETURN count(c) AS n",
	} {
		t.Run(text, func(t *testing.T) {
			want := run(t, false, text)
			got := run(t, true, text)
			if want.usedIdx || !got.usedIdx {
				t.Fatalf("index use: no-index store %v, indexed store %v; want false and true", want.usedIdx, got.usedIdx)
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
		})
	}
}
