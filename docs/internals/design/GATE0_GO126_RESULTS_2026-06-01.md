# Gate 0 — Go 1.26 adoption results (2026-06-01)

## Method
A/B: `go1.25.3` (baseline, Green Tea GC off) vs `go1.26.3` (Green Tea GC default on).
`go.mod` bumped 1.25.3 -> 1.26.0 between captures. Benchmarks unchanged. benchstat for deltas.
Primary signal: pkg/vector BenchmarkHNSWSearch (the SIMD-targeted kernel). Storage best-effort.

## Baseline (go1.25.3, Green Tea OFF)

### pkg/vector
```
goos: darwin
goarch: arm64
pkg: github.com/dd0wney/cluso-graphdb/pkg/vector
cpu: Apple M1
BenchmarkHNSWSearch-8   	   10000	    134393 ns/op	   12480 B/op	     204 allocs/op
BenchmarkHNSWSearch-8   	   10000	    120950 ns/op	   11168 B/op	     197 allocs/op
BenchmarkHNSWSearch-8   	   10000	    118962 ns/op	   11056 B/op	     191 allocs/op
BenchmarkHNSWSearch-8   	   10000	    130226 ns/op	   12448 B/op	     202 allocs/op
BenchmarkHNSWSearch-8   	    9943	    133336 ns/op	   12848 B/op	     220 allocs/op
BenchmarkHNSWSearch-8   	   10000	    114246 ns/op	   11584 B/op	     213 allocs/op
PASS
ok  	github.com/dd0wney/cluso-graphdb/pkg/vector	275.382s
```

### pkg/storage (best-effort)
Partial — storage benchmark ran for ~20 minutes and was killed while stuck on
`BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheMiss` (signal: terminated after
1224.246s). All benchmarks up to that point completed. Key partial results captured
at /tmp/baseline_storage.txt (281 lines). Representative samples:

```
BenchmarkGetEdge_Uniform_PureReads_4-8      	 3859474	       312.5 ns/op	     544 B/op	       3 allocs/op
BenchmarkGetEdge_Zipfian_PureReads_4-8      	 4115251	       306.8 ns/op	     544 B/op	       3 allocs/op
BenchmarkGetNode_Uniform_PureReads_4-8      	 2663668	       397.1 ns/op	     560 B/op	       4 allocs/op
BenchmarkGetNode_Zipfian_PureReads_4-8      	 3244636	       373.9 ns/op	     560 B/op	       4 allocs/op
BenchmarkStorage_GetNode_Memory-8           	(partial — see /tmp/baseline_storage.txt)
BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheHit-8 	 1335684	       892.7 ns/op	    1528 B/op	      24 allocs/op
BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheMiss-8	signal: terminated
FAIL	github.com/dd0wney/cluso-graphdb/pkg/storage	1224.246s
```

Note for the post-bump run: use `-bench=BenchmarkGet` or skip `BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheMiss`
to avoid the hang; or run storage benches with a shorter `-timeout` and accept partial.

## After (go1.26.3, Green Tea ON)

Post-bump `pkg/vector BenchmarkHNSWSearch`, `-count=6`, go1.26.3 (Green Tea GC
default on). Storage re-bench deliberately skipped — the baseline storage sweep
hung ~20 min on the `DiskBacked_CacheMiss` disk benchmark; vector is the
SIMD-targeted primary signal and the only metric worth a clean A/B here.

### benchstat: baseline (go1.25.3, Green Tea OFF) vs after (go1.26.3, Green Tea ON)
```
             │ baseline_vector.txt │      after_vector.txt        │
             │       sec/op        │    sec/op     vs base        │
HNSWSearch-8          125.6µ ± 9%   119.7µ ± 15%  ~ (p=0.589 n=6)

             │       B/op          │     B/op       vs base       │
HNSWSearch-8         11.73Ki ± 8%   11.06Ki ± 10%  ~ (p=0.589 n=6)

             │     allocs/op       │ allocs/op   vs base          │
HNSWSearch-8           203.0 ± 8%   205.0 ± 8%  ~ (p=1.000 n=6)
```

### Verdict (honest)
**No measurable Green Tea GC win on this benchmark/arch.** All three metrics are
statistically insignificant (`~`, p=0.589 sec/op, p=1.000 allocs/op): the
1.25→1.26 delta is within run-to-run noise on darwin/arm64. This is consistent
with the Go team's "~10% for most apps, up to 50% for complex memory layouts"
framing — HNSWSearch on arm64 simply isn't in the win zone, and after the
boxed-heap allocations were removed (separate `perf/hnsw-search-pooling` work),
there's little GC pressure left for Green Tea to reclaim here. The bump is still
worth keeping: it's the prerequisite for the SIMD path (amd64), and the GC
delta may differ on amd64 (not measured — this dev box is arm64).

## SIMD smoke result — DONE in commit 536de38 (PASS on amd64 via Docker)
