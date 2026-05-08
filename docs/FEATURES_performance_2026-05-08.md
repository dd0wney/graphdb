# Killer-Features: Performance Analysis
**Date**: 2026-05-08
**Scope**: `github.com/dd0wney/cluso-graphdb` — top 3 perf wins reproducible on a buyer's bench

---

## Realistic scale ceiling

Before scoring any candidate: `pkg/search/lsa.go:545-573` (`TopKByVector`) is a single-threaded O(D×k) full dot-product scan — no SIMD, no parallelism (no parallel variant in `lsa_test.go`). At 200-dim embeddings that bounds LSA-involving queries to roughly 100K–500K documents before P99 crosses 100 ms on a single Beelink SER core. `lsa.go:22-24` also states the model is not incremental: any vocab shift forces a full rebuild. This is the hard ceiling on any claim involving LSA at "10M nodes." The three features below stay inside this ceiling and are honest.

---

## Feature 1: Graph-bounded k-NN (traversal-predicated vector search)

**Headline metric**: k-NN among nodes within N hops of a seed node, P50 < 20 ms at 500K-vector index, 2-hop radius.

**Current state**: `pkg/api/handlers_vectors.go:286-326` performs HNSW search globally, then post-filters by tenant and label. `pkg/query/executor_vector_step.go:35-78` does the same inside the query engine: HNSW-first, label-match after. Neither path accepts a candidate-ID constraint before HNSW entry. Graph traversal (`pkg/query/parallel_traversal.go:47-131`) and vector search are entirely decoupled subsystems with no shared call path.

**What closes the gap**: Two options, both scoped to existing files.
- Option A (preferred for large hop sets): add a `SearchWithCandidates(candidateIDs []uint64, query []float32, k int) ([]SearchResult, error)` method to `HNSWIndex` (`pkg/vector/hnsw.go`) that skips the HNSW walk entirely and runs a brute-force dot-product over the pre-filtered candidate set. When the hop-bounded set is small (< ~10K nodes, typical for 2-hop from a single seed), brute-force over a known set beats HNSW's approximate traversal of the full index. The caller is `VectorSearchStep.Execute` in `pkg/query/executor_vector_step.go`.
- Option B: run `ParallelTraversal.Execute` first, collect node IDs, then call Option A.

`FindNodesByProperty` (`pkg/storage/query_operations.go:96-114`) confirms there is no property index — full-graph property scans are O(N). The bounded candidate set is therefore essential: brute-force over 2K nodes beats O(N) scan over 500K nodes.

**Effort**: M. New method on HNSWIndex + wiring in executor_vector_step.go + a benchmark.

**Competitor gap**: Weaviate and Qdrant have no traversal-bounded ANN. Memgraph has both HNSW and graph traversal but does not fuse them into a single query step with a shared candidate set. This is the only feature in the field that lets a user write "nearest 10 documents co-authored with Alice's 2-hop network" as a single declarative query.

---

## Feature 2: End-to-end GraphRAG retrieval latency (P50/P99 on ICIJ corpus)

**Headline metric**: query → FTS+LSA hybrid → 1-hop neighborhood expansion → context-packed result, P50 < 50 ms on the ICIJ Offshore Leaks corpus (~800K entities).

**Current state**: `pkg/api/handlers_hybrid_search.go:60-258` implements the full RRF merge of FTS and LSA today. The `TookMs` field is already in the response body (line 254). Missing: (a) an expansion step that fetches 1-hop neighbors of top-K results and includes them in the response, (b) a context-pack serializer that formats the result as an LLM prompt window. The ICIJ corpus is already referenced in `docs/ICIJ_OFFSHORE_LEAKS_BENCHMARK.md`, giving a buyer a public, reproducible dataset.

**What closes the gap**: Add an `expand` parameter to `HybridSearchRequest` (1-hop BFS from each result node using `GetOutgoingEdges`). The expansion is bounded by `Limit * fan-out`, which is controllable. Wire `GetOutgoingEdges` calls in `handleHybridSearch` after pagination. The context-pack output is a second response field — no schema break for existing callers. Total new code: ~80 lines in `handlers_hybrid_search.go`.

**Effort**: S. The retrieval pipeline exists; expansion and serialization are additive.

**Competitor gap**: Weaviate ships `nearText` + `GraphQL` for retrieval but has no native graph hop expansion in the retrieval response. Neo4j and Memgraph have hop expansion but no built-in semantic ranking. graphdb is the only system in the sub-$1K/month deployment range that ships all four: FTS, LSA, HNSW, and traversal — fused at query time. A benchmark against Weaviate's hybrid search on ICIJ (Weaviate needs an external embedding model; graphdb uses built-in LSA) is a reproducible apples-to-apples comparison.

---

## Feature 3: Multi-tenant query isolation throughput at scale

**Headline metric**: P99 read latency for tenant A does not increase by more than 5% when tenants 2–100 are added, at 10K nodes per tenant.

**Current state**: `pkg/search/search_scenarios_bench_test.go:201-233` (`BenchmarkScenario_MultiTenantIsolation`) already scaffolds the benchmark with two tenants and alternating queries. `pkg/search/tenant_indexes.go` isolates FTS indexes per tenant via `RWMutex`-protected map. `pkg/search/lsa.go:32` explicitly states LSA is not tenant-scoped — a multi-tenant deployment today shares one LSA model across all tenants, meaning tenant B's documents influence tenant A's semantic results.

**What closes the gap**: (a) Promote the existing bench from 2 to 100 tenants and record the P99 number — this alone is publishable since no competitor in this weight class ships a reproducible multi-tenant isolation benchmark. (b) Introduce per-tenant LSA indexes using the pattern already established in `tenant_indexes.go` for FTS. The rebuild cost per tenant is bounded by per-tenant corpus size, not global corpus size — a concrete advantage over single-model systems.

**Effort**: S (bench promotion + headline number), M (per-tenant LSA).

**Competitor gap**: SaaS graph DB vendors (Neptune, TigerGraph Cloud) do not publish per-tenant query isolation benchmarks because their shared-infrastructure model makes the numbers unflattering. graphdb on a single Linux box with process-level isolation per tenant is a differentiator for regulated buyers (SOC2, HIPAA-adjacent).

---

## The undersold subsystem

`pkg/query/parallel_traversal.go` is the most competitively underexposed subsystem in the codebase. It is NumCPU-aware (`line 51`), uses `sync.Map` for lock-free visited tracking (`line 56`), imposes a per-level goroutine semaphore (`line 193`), and propagates context cancellation cleanly through all goroutines (`lines 155-159`, `199-203`). Most graph databases in this weight class run BFS single-threaded. A clean published benchmark — "k-hop expansion of M seed nodes, wall-clock time vs. single-threaded baseline, 8-core commodity hardware" — would be a first-of-kind number for the Go embedded-graph-database category. It also anchors Feature 1 (graph-bounded k-NN) and Feature 2 (1-hop expansion in GraphRAG). It is being used internally but not benchmarked or documented as a selling point anywhere in `docs/`.
