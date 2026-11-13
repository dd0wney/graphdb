package algorithms

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// BenchmarkPageRankForTenant measures the loop that gained a cancellation
// check, so a regression from the check shows as a number.
func BenchmarkPageRankForTenant(b *testing.B) {
	gs, err := storage.NewGraphStorage(b.TempDir())
	if err != nil {
		b.Fatalf("NewGraphStorage: %v", err)
	}
	defer gs.Close()
	const tid, n = "t-bench", 5000
	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		node, cErr := gs.CreateNodeWithTenant(tid, []string{"N"}, nil)
		if cErr != nil {
			b.Fatalf("node: %v", cErr)
		}
		ids = append(ids, node.ID)
	}
	for i := range ids {
		for _, step := range []int{1, 7, 31} {
			if _, eErr := gs.CreateEdgeWithTenant(tid, ids[i], ids[(i+step)%n], "L", nil, 1.0); eErr != nil {
				b.Fatalf("edge: %v", eErr)
			}
		}
	}
	opts := DefaultPageRankOptions()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PageRankForTenant(gs, opts, tid); err != nil {
			b.Fatal(err)
		}
	}
}
