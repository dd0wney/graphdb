package retrieval

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/search"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// Audit F2 #6 (2026-05-08): latency benchmark for the retrieval
// package. Establishes the perf budget cited in
// docs/F2_GRAPHRAG_DESIGN.md.
//
// # Measured baseline (Apple M1, 2026-05-08, benchtime=2s)
//
//	BenchmarkRetrieve_TypicalQuery   p50= 44µs  p95= 82µs  p99=156µs
//	BenchmarkRetrieve_SeedsOnly      p50= 37µs  p95= 68µs  p99=125µs
//	BenchmarkRetrieve_ThreeHops      p50= 44µs  p95= 79µs  p99=148µs
//
// # Perf budget (regression alarm)
//
//	p95 ≤ 5ms for TypicalQuery on a 1000-node corpus
//
// 60× margin over the measured baseline — leaves headroom for
// (a) larger graphs (10–100× node count), (b) slower CI hardware,
// (c) future GC pauses. A regression breaching this budget should
// block ship; a regression that doubles latency but stays under the
// budget should file a perf follow-up.
//
// The spike (#28 §4) initially proposed "p95 < 500ms" — that target
// was sized for an HTTP+network round-trip, not the in-memory
// pipeline. The real budget is 100× tighter because we're measuring
// the retrieval primitive, not the wire-level latency a client sees.
//
// SeedsOnly and ThreeHops bracket the work: SeedsOnly is the
// search-only floor, ThreeHops shows that HardNodeCap=50 keeps even
// dense expansions bounded (3-hop and 2-hop are nearly identical).
//
// Reports custom metrics:
//
//	us/p50  microseconds, median per-call latency
//	us/p95  microseconds, 95th percentile
//	us/p99  microseconds, 99th percentile
//
// Run with: go test -bench=BenchmarkRetrieve_TypicalQuery -run=^$ ./pkg/retrieval/
//
// The default ns/op metric is the mean — useful for relative
// comparisons across changes, but for an LLM-facing API the tail
// matters more than the mean. The percentiles capture that.

const (
	benchNodes        = 1000
	benchEdgesPerNode = 5
	benchSeedKeyword  = "graph database"
	// benchSeedHits controls how many seeded nodes contain the
	// query keyword. Mirrors a realistic workload where ~5% of the
	// corpus matches a typical user query.
	benchSeedHits = 50
)

// setupBenchCorpus builds a single-tenant corpus of benchNodes nodes
// connected by benchNodes*benchEdgesPerNode random edges, with the
// first benchSeedHits nodes carrying the query keyword in their
// body. Indexes the corpus for FTS.
//
// Deterministic — uses a fixed PRNG seed so benchmark runs are
// comparable across invocations.
func setupBenchCorpus(b *testing.B) (*Retriever, func()) {
	b.Helper()

	gs, err := storage.NewGraphStorage(b.TempDir())
	if err != nil {
		b.Fatalf("storage: %v", err)
	}
	cleanup := func() { _ = gs.Close() }

	tenantID := "tenant-bench"
	nodeIDs := make([]uint64, 0, benchNodes)

	for i := 0; i < benchNodes; i++ {
		var body string
		if i < benchSeedHits {
			// Seed candidates: contain the query keyword plus some
			// distinguishing payload.
			body = fmt.Sprintf("%s reference document %d with example content", benchSeedKeyword, i)
		} else {
			// Non-matching nodes: traversal targets. Body content
			// avoids the query keyword.
			body = fmt.Sprintf("auxiliary node %d background notes filler text payload", i)
		}
		n, err := gs.CreateNodeWithTenant(tenantID, []string{"Doc"}, map[string]storage.Value{
			"body": storage.StringValue(body),
		})
		if err != nil {
			cleanup()
			b.Fatalf("seed node %d: %v", i, err)
		}
		nodeIDs = append(nodeIDs, n.ID)
	}

	// Random edges (deterministic via fixed PRNG seed). Each node
	// gets benchEdgesPerNode outgoing edges to random other nodes.
	rng := rand.New(rand.NewSource(42))
	for i, fromID := range nodeIDs {
		for j := 0; j < benchEdgesPerNode; j++ {
			toIdx := rng.Intn(len(nodeIDs))
			if toIdx == i {
				continue // skip self-loops
			}
			if _, err := gs.CreateEdgeWithTenant(tenantID, fromID, nodeIDs[toIdx], "REL", nil, 1.0); err != nil {
				cleanup()
				b.Fatalf("seed edge %d→%d: %v", i, toIdx, err)
			}
		}
	}

	searchIdx := search.NewTenantIndexes(gs)
	if err := searchIdx.IndexForTenant(tenantID, []string{"Doc"}, []string{"body"}); err != nil {
		cleanup()
		b.Fatalf("FTS index: %v", err)
	}
	lsaIdx := search.NewTenantLSAIndexes()

	return NewRetriever(gs, searchIdx, lsaIdx), cleanup
}

// reportPercentiles sorts the timings and reports p50/p95/p99 as
// custom benchmark metrics (in microseconds). Mean ns/op is reported
// automatically by the benchmark framework.
func reportPercentiles(b *testing.B, timings []time.Duration) {
	if len(timings) == 0 {
		return
	}
	sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
	// Index into sorted timings; use n*p/100 with floor.
	pAt := func(p int) time.Duration {
		idx := len(timings) * p / 100
		if idx >= len(timings) {
			idx = len(timings) - 1
		}
		return timings[idx]
	}
	b.ReportMetric(float64(pAt(50).Microseconds()), "us/p50")
	b.ReportMetric(float64(pAt(95).Microseconds()), "us/p95")
	b.ReportMetric(float64(pAt(99).Microseconds()), "us/p99")
}

// BenchmarkRetrieve_TypicalQuery measures the budget-relevant
// scenario: K=10, MaxHops=2, against a 1000-node / ~5000-edge
// single-tenant corpus. This is the "5-seed × 2-hop" target from F2
// spike (#28) §4 sized up to a realistic corpus.
func BenchmarkRetrieve_TypicalQuery(b *testing.B) {
	r, cleanup := setupBenchCorpus(b)
	defer cleanup()

	ctx := context.Background()
	opts := Options{K: 10, MaxHops: 2}

	timings := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_, err := r.Retrieve(ctx, benchSeedKeyword, "tenant-bench", opts)
		if err != nil {
			b.Fatalf("retrieve: %v", err)
		}
		timings = append(timings, time.Since(start))
	}
	b.StopTimer()
	reportPercentiles(b, timings)
}

// BenchmarkRetrieve_SeedsOnly is the floor: MaxHops=0 skips BFS
// expansion entirely, isolating the seed-retrieval cost. Useful for
// attributing latency between search and traversal when the
// TypicalQuery budget moves.
func BenchmarkRetrieve_SeedsOnly(b *testing.B) {
	r, cleanup := setupBenchCorpus(b)
	defer cleanup()

	ctx := context.Background()
	opts := Options{K: 10, MaxHops: 0}

	timings := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_, err := r.Retrieve(ctx, benchSeedKeyword, "tenant-bench", opts)
		if err != nil {
			b.Fatalf("retrieve: %v", err)
		}
		timings = append(timings, time.Since(start))
	}
	b.StopTimer()
	reportPercentiles(b, timings)
}

// BenchmarkRetrieve_ThreeHops measures the upper edge of the v1
// design space: MaxHops=3 typically hits the HardNodeCap (50 nodes)
// in dense graphs, which is the worst-case wall-clock under v1
// constraints. Useful for sizing v2 work (e.g., centrality caching).
func BenchmarkRetrieve_ThreeHops(b *testing.B) {
	r, cleanup := setupBenchCorpus(b)
	defer cleanup()

	ctx := context.Background()
	opts := Options{K: 10, MaxHops: 3}

	timings := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_, err := r.Retrieve(ctx, benchSeedKeyword, "tenant-bench", opts)
		if err != nil {
			b.Fatalf("retrieve: %v", err)
		}
		timings = append(timings, time.Since(start))
	}
	b.StopTimer()
	reportPercentiles(b, timings)
}
