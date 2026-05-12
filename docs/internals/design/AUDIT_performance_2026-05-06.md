# Performance Audit — graphdb

Date: 2026-05-06

---

## HIGH: GetNode clones every node on every read — K allocations per vector search

**File:** `pkg/storage/node_operations.go:106`

`GetNode` always returns `node.Clone()`, which allocates a new `Node`, a fresh `[]string` for labels, and a full copy of the `Properties` map on every call. In the vector search post-filter loop (`pkg/api/handlers_vectors.go:338`), this fires once per HNSW candidate — up to `ef` candidates (default `max(K*2, 50)`) — before tenant/label/property filters have a chance to reject any of them. For `K=50` and default `ef=100` that is 100 heap allocations and 100 full property-map copies on the hot query path.

The fix is a `getNodeRef` path (returns a pointer to the internal node, read-lock held by the caller) for internal post-filter loops. Public API callers that may escape the lock still need `Clone`; only the hot internal loop needs the zero-copy variant.

Impact: reduces allocations-per-search from O(ef) to O(results_kept). For a typical filter-heavy search where 80% of candidates are dropped, this is roughly a 5x reduction in per-request GC pressure.

---

## HIGH: Global `gs.mu.RLock()` serializes all node reads — shardLocks are only wired for edges

**Files:** `pkg/storage/node_operations.go:92`, `pkg/storage/storage_types.go:51-52`

`GraphStorage` declares 256 `shardLocks` and the infra code in `storage_helpers.go:162-168` exists to use them. However, `GetNode` takes `gs.mu.RLock()` (global), not `rlockShard`. `GetEdge` correctly uses shard locking (edge_operations.go:83). Node reads — the most frequent operation — are fully serialized by the global mutex. Under any concurrent request load (Cloudflare Tunnel will stripe concurrent reads from multiple browser sessions), all node-read goroutines queue behind this single lock.

Extending `rlockShard` to node reads requires that every writer that touches `gs.nodes` also acquires the per-shard write lock in addition to (or instead of) `gs.mu.Lock`. That is a correctness-sensitive refactor, not a line edit — worth a focused spike. Impact at concurrency ≥ 4 goroutines: proportional to lock-wait fraction; likely 2-4x throughput gain under read-heavy load.

---

## HIGH: Default WAL fsyncs to disk on every write — serializes all mutations

**Files:** `pkg/wal/wal.go:79-85`, `pkg/storage/storage.go:24`

`WAL.Append` calls `writer.Flush()` then `file.Sync()` before returning (lines 79-85). `CompressedWAL.Append` does the same (compressed_wal_io.go:46-52). The default `GraphStorage` config has `EnableBatching: false` and `EnableCompression: false`, so `NewGraphStorage` uses the plain `WAL` path. Every `CreateNode`, `UpdateNode`, and `DeleteNode` holds `gs.mu.Lock()` for the entire duration including the fsync. On `beelink-ser` (NVMe), a single fsync is ~50-200µs; that floor serializes all writes and sets the p99 write latency budget.

`BatchedWAL` exists and is wired — it is just not the default. Enabling `EnableBatching: true` with a 10ms flush interval in the production config would amortize fsyncs across concurrent writes with no durability regression beyond that window.

---

## MEDIUM: `parallel_aggregation.go` is sequential in practice

**File:** `pkg/query/parallel_aggregation.go:66-93`

`CountNodesByLabel` partitions the node ID space across `runtime.NumCPU()` workers. Each worker calls `pa.graph.GetNode(nodeID)` per node, which takes `gs.mu.RLock()` and allocates a `Clone`. Because `gs.mu.RLock` is a global reader-writer mutex, all worker goroutines proceed in parallel on the read side — this is legitimately parallel for reads. However, each worker also allocates a `Clone` per node. For a graph with 100k nodes this is 100k clones spread across workers, all racing on the GC.

The deeper problem: the ID-range partitioning approach visits nodes sequentially by ID regardless of whether IDs are contiguous (deletions leave gaps). Use `GetAllNodeIDs` or `ForEachNode` to iterate only live nodes. `ForEachNode` (`node_operations.go:294`) holds one RLock for the entire iteration and calls the closure on the raw pointer without cloning — that single function replaces the parallel worker loop with lower total allocation cost, though it loses actual parallelism.

If parallel execution is genuinely wanted, pre-collect IDs once (one RLock, O(N) iteration), partition that slice across workers, then batch-lookup per shard.

---

## MEDIUM: HNSW search allocates a new `map[uint64]bool` visited set per search call

**File:** `pkg/vector/hnsw_search.go:10-11, 75-76`

Both `searchLayer` and `searchLayerKNN` call `make(map[uint64]bool)` for `visited` at entry. For a graph with M=16 and ef=100, the visited map typically holds a few hundred entries. On a busy server handling N concurrent vector searches, this is N small-map allocations per search call that escape to the heap. A `sync.Pool` of pre-allocated maps (cleared between uses) would eliminate this allocation on the steady-state path.

The two `priorityQueue` allocations per search call (candidates + w) have the same fix. Together these add ~3 allocations to what should be a heap-based traversal with zero GC. Impact: moderate, visible mainly under sustained concurrent query load rather than single-request p99.

---

## MEDIUM: Cosine distance recomputes `||query||` for every neighbor comparison

**File:** `pkg/vector/distance.go:24-44`

`CosineSimilarity` computes `normA` and `normB` inline. In `searchLayerKNN`, `h.distance(query, friend.vector)` is called for every neighbor candidate (line 110 of hnsw_search.go). The query vector does not change within a single `Search` call. Computing `||query||` once before entering the search loop and passing it to a specialized `CosineDistanceWithQueryNorm(a, b []float32, normA float32)` would save one inner-loop pass (O(dims)) and one `math.Sqrt` per neighbor evaluation. At 1536 dims (OpenAI embedding size) and ef=100 candidates, that is 100 sqrt calls and 100 full vector passes that could be eliminated. This compounds across the power-iteration layers.

Storing `||v||` per `hnswNode` at insert time would extend the optimization to the stored vector side as well.

---

## MEDIUM: LSM `BlockCache.Get` takes a write lock, not a read lock

**File:** `pkg/lsm/cache.go:35-50`

`BlockCache.Get` calls `bc.mu.Lock()` (exclusive) despite being a read operation, because it calls `bc.lru.MoveToFront`. This means concurrent cache hits on the LSM path serialize through a single mutex. Under any read concurrency, all cache hits queue. A common alternative is to use a two-level strategy: `RLock` for the lookup, promote to `Lock` only on a miss or every N hits to update LRU order. Or replace `container/list`-based LRU with a clock-hand or segment-based cache (`github.com/hashicorp/golang-lru/v2` has a concurrent variant) that avoids per-hit exclusion. Impact scales linearly with LSM read concurrency.

---

## GOOD: PropertyFilter pre-conversion in handlers_vectors.go

**File:** `pkg/api/handlers_vectors.go:326-332, 409-419`

The `propertyPredicate` map is built once outside the candidate loop by converting each `any` JSON value to `storage.Value` before iteration. Inside the loop, `matchesPropertyFilter` compares using `bytes.Equal` on already-encoded byte slices — no reflection, no string formatting, no per-iteration type conversion. This is the correct pattern. The same `convertToValue` call placed inside the loop would have been a measurable regression for large candidate sets.

---

## Not pursued

- LSA SVD kernel (`lsaJacobi`, `lsaSparseMulDense`): these run once at index build time, not per-request. Not a latency target.
- Snappy overhead in `CompressedWAL`: dominated by the per-call fsync by orders of magnitude.
- Levenshtein in `SearchFuzzy`: iterates the full vocabulary, but fuzzy search is not the primary search path.
