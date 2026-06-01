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

## After (go1.26.3, Green Tea ON) — filled by a later task

## SIMD smoke result — DONE in commit 536de38 (PASS on amd64 via Docker)
