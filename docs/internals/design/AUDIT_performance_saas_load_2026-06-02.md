# Performance Audit — Multi-Tenant SaaS Load

Date: 2026-06-02
Scope: graphdb OSS core (`pkg/storage`, `pkg/vector`, `pkg/wal`, `pkg/query`, `pkg/graphql`, `pkg/intelligence`)
Extends: `docs/internals/design/AUDIT_performance_2026-05-06.md` (single-node, code-reading-only)

---

## Why this audit, and how it differs from 2026-05-06

The 2026-05-06 performance audit was a single-node, code-reading pass. Since
then the A4 / A4-edges sharding work landed and **Track R** added an in-server
auto-embed path (LSA → HNSW), verified searchable end-to-end (PR #251,
2026-06-02). With the Track R verification gap fully closed, the planning doc's
Decision 9 nominated a fresh audit as the next critical path; the chosen angle
is **performance under realistic multi-tenant SaaS load** (the angle the
single-node audit structurally could not see).

This audit is grounded in the **hot query shapes** of that workload, not a
feature list:

- **Write-dominated:** auto-embed node creation — `CreateNodeForTenant` →
  `createNodeLocked`, then (when `GRAPHDB_AUTO_EMBED_ENABLED=true`) an async
  write-back `UpdateNodeForTenant`. This is the Track-R production write path.
- **Read-dominated:** tenant-filtered vector search; tenant-scoped node
  lookups (`GetNodeForTenant`); labeled and unlabeled `MATCH`; GraphQL
  node/edge queries with pagination.

Findings are tagged **MEASURED** (a benchmark in this repo produced the number)
or **ANALYTICAL** (code-path inference). Three findings were corroborated
independently — they are noted as such.

### What 2026-05-06 flagged that is now RESOLVED (do not re-flag)

| 2026-05-06 finding | Current status |
|---|---|
| HIGH: global `gs.mu.RLock` serializes node reads | **FIXED** — `GetNode`/`GetNodeForTenant` now take `rlockShard(nodeID)` + `lookupNodeShard` (A4). Reads on different shards are genuinely parallel. |
| HIGH: `GetNode` clones every candidate in the vector post-filter loop | **FIXED** — `handlers_vectors.go` uses `WithNodeRefForTenant` (per-shard RLock, live pointer); `Clone()` fires only for survivors. Allocations O(ef) → O(kept). |
| MEDIUM: HNSW per-search `priorityQueue`/`candidateQueue` heap boxing | **FIXED** — value-semantics rewrite in `hnsw_types.go`. (The `visited` map alloc remains — see M5.) |
| (implied) per-tenant vector heap memory blowup | **VERIFIED OK** — Track R 1a measured ratio 1.000 across 100→1000 tenants. No fixed per-tenant overhead; per-tenant maps are lazy-initialized. |

---

## The one structural fact under every HIGH finding

graphdb serializes all writes — and several read paths — on the process-global
`gs.mu`. That serialization is **known and accepted**: `CLAUDE.md` states
verbatim *"Currently moot because `gs.mu.Lock` serializes writers."* The lock
itself is not the discovery.

The discovery is that the **SaaS-era workload has quietly loaded more work into
the sections `gs.mu` guards** than existed when "moot" was written:

- a synchronous **HNSW insert** now runs inside the write critical section
  (Track R auto-embed);
- a reflection-based **`json.Marshal`** of the full node — embedding vector
  included — runs inside it;
- the auto-embed write-back makes that critical section run **twice** per
  logical node create;
- unlabeled reads and **every GraphQL edge query** run **full cross-tenant
  scans** under `gs.mu.RLock`.

"Moot" was true when the critical section was a few map appends and an fsync.
It is no longer true. **The next critical path is to shrink what `gs.mu`
guards**, in the order the measured data dictates (see § Recommendations).

---

## Measured evidence

New benchmarks in `pkg/storage/bench_concurrent_write_test.go` (Apple M1, 8
cores, Go 1.25, APFS/NVMe). Each writer goroutine writes to its **own** tenant,
so per-shard locks and per-tenant indexes never contend — the only shared
contention point exercised is `gs.mu` **and the single shared WAL file**.

### Writes do not scale with tenant count (`BenchmarkConcurrentWrite_NoIndex`)

| goroutines (= tenants) | ns/op | aggregate throughput |
|---|---|---|
| 1  | 5.22 ms | ~192 writes/s |
| 4  | 5.93 ms | ~169 writes/s |
| 8  | 5.62 ms | ~178 writes/s |
| 16 | 6.27 ms | ~160 writes/s |

Per-op latency is **flat** as concurrency rises 1→16: 16 tenants writing
concurrently get roughly **one tenant's** aggregate throughput (~170 writes/s
*total*). This is the noisy-neighbor write ceiling, measured.

**Honest caveat:** the fsync happens *inside* `gs.mu`, and concurrent fsyncs to
the single shared WAL file serialize at the file level regardless of the lock.
So this benchmark demonstrates that writes are serialized and **fsync-dominated**
(~5.5 ms floor); it does **not** isolate `gs.mu` from shared-WAL fsync. The
practical consequence is the same — adding tenants does not add write
throughput — but the first lever to pull is the WAL path, not the lock (see
Recommendations).

### The auto-embed HNSW insert is masked today (`_WithIndex` vs `_NoIndex`)

| goroutines | NoIndex | WithIndex (pre-populated index) |
|---|---|---|
| 1  | 5.22 ms | 5.81 ms |
| 4  | 5.93 ms | 5.94 ms |
| 8  | 5.62 ms | 6.07 ms |
| 16 | 6.27 ms | 7.45 ms |

WithIndex tracks NoIndex within noise at low concurrency: the HNSW insert the
auto-embed path adds is **largely behind the ~5.5 ms fsync floor today**. The
gap widens only at g=16 (7.45 vs 6.27 ms ≈ +1.2 ms) where larger per-tenant
indexes and more accumulated serialized HNSW work begin to surface above the
floor — a preview of the ceiling H2 describes.

### The HNSW insert cost in isolation (`BenchmarkVectorIndexInsert`)

The remove+add pair `createNodeLocked` runs via `UpdateNodeVectorIndexes`,
measured without WAL or lock overhead, at 128 dims:

| index size | HNSW remove+add |
|---|---|
| empty       | 64 µs  |
| 1,000 vec   | 139 µs |
| 10,000 vec  | 176 µs |

~140 µs at 1k vectors is **~2.5% of the ~5.5 ms write critical section today**.
The cost grows ~O(log N) with index size. **This is the term that becomes the
dominant *serialized* cost once the fsync floor is amortized away** — the next
ceiling, not today's bottleneck.

### WAL batching is *worse*, not better (`_NoIndex_Batched`)

| goroutines | fsync-default (`_NoIndex`) | batched (`_NoIndex_Batched`) |
|---|---|---|
| 1  | 5.22 ms | 13.38 ms |
| 4  | 5.93 ms | 12.48 ms |
| 8  | 5.62 ms | 10.69 ms |
| 16 | 6.27 ms | 10.49 ms |

The 2026-05-06 audit recommended enabling `EnableBatching: true` to "amortize
fsyncs across concurrent writes." Measured, batching is **1.7–2.6× worse** and
pinned near the 10 ms `FlushInterval` — it never beats the fsync default, even
at g=16. See H5 for the structural reason (the global lock prevents the batch
from ever filling).

---

## HIGH findings

### H1 — Writes serialize globally; the critical section is fsync-dominated and does not scale with tenant count

**Files:** `pkg/storage/node_operations.go:27` (`gs.mu.Lock`),
`pkg/storage/persistence_wal.go` (WAL append+fsync inside the lock).
**Status:** KNOWN lock (CLAUDE.md), now **MEASURED**. **MEASURED.**

Aggregate write throughput is ~170/s *total* regardless of tenant count (table
above). Today this is fsync-dominated (~5.5 ms/write, `EnableBatching: false`
default at `storage.go:21`, unchanged since 2026-05-06). This is the frame for
everything below: the write path has one serialized throughput budget shared by
all tenants, and the SaaS workload keeps adding work to it.

### H2 — The Track-R auto-embed path put a synchronous HNSW insert inside the global write critical section

**Files:** `pkg/storage/node_operations.go:170` (`UpdateNodeVectorIndexes`
inside `createNodeLocked`), `pkg/storage/vector_operations.go:185-215`,
`pkg/vector/hnsw.go` (`Insert` → `searchLayerKNN` + `selectNeighborsHeuristic`).
**Status:** NEW. **MEASURED** (insert cost, isolated) + **ANALYTICAL** (in-path).
**Corroborated by all three audit dimensions independently.**

When a tenant has a vector index, `createNodeLocked` runs a real O(log N) HNSW
graph traversal (efConstruction candidates per layer, O(M²) neighbor pruning)
**while holding `gs.mu`**. This is the audit's novel contribution: pre-auto-embed
the critical section was index-appends + fsync; Track R added HNSW work to it.

**Precision (create vs update):** on the **create** path the preceding
`RemoveVectorForTenant` is a cheap map-miss (the node is not yet in the graph),
so create pays **one** real insert. On **update / auto-embed write-back** the
remove is real, so those pay remove+add (≈2× the insert). The microbench models
the create path (miss + add).

**Why it is the *next* ceiling, not today's bottleneck:** at ~140 µs it is 2.5%
of the fsync-dominated section now. It does not parallelize away (it is under
the global lock), so once the WAL floor is removed (H5 / Recommendation 1), this
becomes the dominant serialized term. `HNSWIndex` already owns its own `h.mu`
and is fully decoupled from `gs.mu` on the read side, so lifting
`UpdateNodeVectorIndexes` out of `gs.mu` (after the shard store that makes the
node visible) is low-risk — but it is **pre-positioning**, sequenced after the
WAL fix.

### H3 — Auto-embed doubles the write cost: two `gs.mu` acquisitions, two fsyncs, two HNSW ops per logical node create

**Files:** `pkg/intelligence/auto_embed_observer.go`,
`pkg/api/server_init.go:321` (`bootstrapAutoEmbedFromEnv`, env-wired in
production), `pkg/storage/node_operations.go:316` (`UpdateNodeForTenant`).
**Status:** NEW. **ANALYTICAL.** Scope: deployments with
`GRAPHDB_AUTO_EMBED_*` configured (the Track-R production path).

A logical "create a Doc node" under auto-embed is: (1) `CreateNodeWithTenant`
(lock + fsync + HNSW insert), then async (2) the observer computes the LSA
embedding off-lock (correct), then (3) `UpdateNodeForTenant` — a **second**
`gs.mu.Lock` + **second** fsync + a **real** HNSW remove+add (the node now
exists). The worker pool hides the embedding *latency* from the HTTP response
but does **not** reduce *contention*: the second critical section still competes
with all other tenants' writes. The realistic write-cost baseline for a
Track-R deployment is **2 global-lock acquisitions per node**, not 1 — size
write capacity accordingly.

### H4 — Unlabeled reads and every GraphQL edge query are full cross-tenant scans under `gs.mu.RLock`

**Files:** `pkg/storage/tenant_operations.go:188` (`GetAllNodesForTenant`),
`GetAllEdgesForTenant` → `forEachNodeUnlocked`/`forEachEdgeUnlocked` (walk all
256 shards across all tenants, filter by `TenantID`).
**Callers (verified):** `pkg/query/match_node.go:34`,
`pkg/query/physical_plan.go:63,868` (unlabeled `MATCH`);
`pkg/graphql/edges_resolvers.go:45`, `pagination_resolvers.go:123`,
`sorting_resolvers.go:49`, `aggregation_resolvers.go:111`,
`filtering_schema.go:193`, `limits.go:257` (GraphQL edge queries).
**Status:** NEW. **ANALYTICAL.**

`GetAllEdgesForTenant` scans the entire database (all tenants) and filters. The
GraphQL edge connection resolver (`pagination_resolvers.go:123`) and edge
resolver (`edges_resolvers.go:45`) then **paginate in memory** — a request for
10 edges first materializes a full filtered slice of *this tenant's* edges, but
only after touching *every* tenant's edges. Cost scales with **total** data,
not the requesting tenant's share — a read-side noisy-neighbor amplification.
Because the scan holds `gs.mu.RLock` for its full duration, it also **stalls all
writers** (which need `gs.mu.Lock`) for that window.

This is partly residue of a correctness fix: the comment at
`edges_resolvers.go:42` shows this path *replaced* a worse "iterate
`1..stats.EdgeCount`" pattern that leaked edges across tenants (A6c). The fix
chose tenant-isolation correctness; the scan cost is the unpaid remainder.
**Labeled paths are fine** — `GetNodesByLabelForTenant` uses the per-tenant
index. The defect is label-absent paths only.

### H5 — WAL batching is structurally defeated by the global lock; the 2026-05-06 recommendation to enable it is invalid

**Files:** `pkg/wal/batched_wal.go:54-83` (`Append` parks on `<-doneCh` at :76),
`:86-126` (`backgroundFlusher` ticks every `FlushInterval`),
`pkg/storage/persistence_wal.go` (`writeToWAL` called under `gs.mu`).
**Status:** NEW — corrects 2026-05-06 (lines 35-36). **MEASURED** + **ANALYTICAL.**

`BatchedWAL.Append` enqueues its entry then blocks on a done-channel **while the
caller still holds `gs.mu.Lock`**. Because the global lock prevents any *second*
writer from entering `createNodeLocked` to enqueue, the batch buffer never
exceeds one entry, `shouldFlush` (`len >= batchSize`) never fires, and every
write waits the full `FlushInterval` (10 ms default) **plus** the fsync — worse
than plain WAL's one-fsync floor. Batching's amortization premise ("multiple
concurrent writers fill the batch before the flush") is structurally
impossible under the global lock. The fix (assign a sequence number under
`gs.mu`, then release `gs.mu` **before** parking on `doneCh`) is the same
unlock-the-critical-section move H2/H3 need, and is the **first-order write
scaling lever** (see Recommendations).

---

## MEDIUM findings

### M1 — `GetNodesByLabelForTenant` holds the global `gs.mu.RLock` across its full clone loop; `countNodes` clones a whole bucket just to take `len()`

**Files:** `pkg/storage/tenant_operations.go:135-158`; callers
`pkg/api/handlers_nodes.go:45` (`countNodes`), `:78` (`listNodes`).
**Status:** NEW. **ANALYTICAL.**

The label lookup is O(1), but the loop clones every node in the bucket under
`gs.mu.RLock`. For a 50k-node label, writers stall behind the RLock for 50k
clones. `countNodes` is the worst case: it materializes and clones the entire
bucket only to return `len()`. Fixes: count via `len(tenantNodesByLabel[tid][label])`
(no clones); list via collect-IDs-under-RLock → release → clone via per-shard
RLocks (the A4 pattern already proven for `GetNode`). This is the labeled-path
sibling of H4 — milder, but on the same live `GET /nodes?label=` surface.

### M2 — `json.Marshal` of the full node (embedding vector included) runs under `gs.mu`

**File:** `pkg/storage/persistence_wal.go:26` (called from `createNodeLocked:176`).
**Status:** NEW. **ANALYTICAL.**

`writeToWAL` reflection-marshals the whole node — including a ~12 KB
1536-dim embedding — to JSON inside the critical section. The bytes depend only
on the (already-constructed) node, not on global state, so the marshal can move
*before* lock acquisition. For embedding-heavy write traffic this is meaningful
CPU on the serialized path. Pairs with H5: a corrected WAL path that assigns the
sequence number under the lock and writes pre-encoded bytes after release fixes
both.

### M3 — Label-index removal is an O(K) linear scan, performed twice (global + per-tenant), under `gs.mu` on every `DeleteNode`

**Files:** `pkg/storage/node_indexing.go:51-61` (global),
`pkg/storage/tenant_operations.go:57-68` (per-tenant).
**Status:** NEW. **ANALYTICAL.** *Corroborated (write + scaling dimensions).*

`map[string][]uint64` with no reverse index means removal scans the slice to
find the ID. A popular label (100k nodes) makes each delete a 100k-element scan
under the global lock, and it runs twice (the tenant-blind global index is
maintained in parallel — see M7). Bulk delete / tenant offboarding is O(N²).
Fix: `map[uint64]struct{}` or sorted slice (binary-search removal).

### M4 — `DeleteNode` → `findNewEntryPoint` is an O(N) scan of the tenant's whole HNSW under `gs.mu`

**Files:** `pkg/vector/hnsw_graph.go:107-123`,
`pkg/storage/node_operations.go:582`.
**Status:** NEW. **ANALYTICAL.**

When the deleted node was the HNSW entry point, `findNewEntryPoint` iterates
every vector in the tenant's index to find the new max-level node — O(N) under
`gs.mu`. Infrequent for random workloads (only the top-layer node triggers it)
but unbounded, and a long-running tenant pruning a large index will hit it.
Fix: maintain a max-layer candidate set updated on insert/delete (O(1)
replacement).

### M5 — HNSW search allocates a fresh `map[uint64]bool` visited set per layer per search; no pool

**File:** `pkg/vector/hnsw_search.go:9,67`.
**Status:** STILL OPEN from 2026-05-06 (the co-flagged heap boxing is fixed; the
map alloc is not). **ANALYTICAL.**

`Search` allocates `maxLayer+1` visited maps (each O(ef) entries, always
heap-escaping) per call. Under N concurrent searches, GC pressure scales with
concurrency × index depth. Fix: `sync.Pool` of cleared maps (safe — `Search`
holds `h.mu.RLock` for its full duration, no re-entrancy).

### M6 — Cosine distance recomputes `‖query‖` and `‖stored‖` on every neighbor evaluation

**File:** `pkg/vector/distance.go:29-35`; `hnswNode` carries no cached norm.
**Status:** STILL OPEN from 2026-05-06. **ANALYTICAL.**

The query norm is constant within a `Search`; recomputing it per neighbor is an
O(dims) pass + `sqrt` wasted each time. The stored-vector norm could be cached
on `hnswNode` at insert. At 1536 dims, ef=100, that is ~100 redundant full
passes + 200 redundant `sqrt` per search — pure CPU waste that bites when cores
are saturated serving concurrent searches.

### M7 — Global `nodesByLabel` / `edgesByType` are maintained in parallel with the tenant indexes, as an unbounded memory sink serving only dead code

**Files:** `pkg/storage/storage_types.go:41-42` (decls),
`pkg/storage/node_operations.go:149-153` (dual append on every create).
**Status:** NEW. **ANALYTICAL.**

Every node create appends to both the global and the per-tenant label index.
The only live reader of the tenant-blind global index is the query optimizer's
`estimateCardinality`, which is **dead code** (none of the five live optimizer
passes call it; `applyJoinOrdering` stubs cardinality out). At 1000 tenants ×
10k "User" nodes, `nodesByLabel["User"]` holds 10M entries, is serialized into
every snapshot (bloating file size and load time), and pins node IDs against GC.
Fix: drop the global index (or gate it behind a feature that actually uses it).

---

## LOW findings

- **L1** — `ListTenants` iterates three tenant maps under `gs.mu.RLock`
  (`tenant_operations.go:247`); bounded but write-blocking if scraped at high
  frequency. **ANALYTICAL.**
- **L2** — `time.Now()` syscall (VDSO, ~20 ns) per tenant-stat update under
  `gs.mu` (`tenant_operations.go:284,297,307,320`). Noise today. **ANALYTICAL.**
- **L3** — `parallel_aggregation.go` `CountNodesByLabel`/`AggregateProperty`
  are dead code (no non-test callers) **and** carry a correctness bug:
  partitioning `1..NodeCount` undercounts nodes whose IDs exceed `NodeCount`
  after deletions. Track for deletion or fix-if-revived. **ANALYTICAL.**
- **L4** — Vector search has a benign TOCTOU between `vi.mu.RUnlock` and
  `h.mu.RLock` (a concurrent `DropIndex` yields stale-but-not-wrong results).
  Memory-safe; documented as benign at `vector_operations.go:118`.
  **ANALYTICAL.**

---

## Explicitly refuted / verified-OK (so a later pass doesn't re-litigate)

- **`vi.mu` is NOT a cross-tenant contention point.** `AddVectorForTenant` /
  `SearchForTenant` hold `vi.mu.RLock` only to look up the `*HNSWIndex` pointer,
  then operate under per-index `h.mu`. Cross-tenant inserts/searches are fully
  parallel. `vi.mu.Lock` is taken only by create/drop-index (admin). **OK.**
- **No fixed per-tenant overhead.** The 256-shard arrays are global; per-tenant
  maps are lazy-initialized on first write. Consistent with Track R 1a's
  ratio-1.000 heap proportionality. **OK.**
- **Vector-search tenant isolation is structural**, not per-candidate filtering;
  the `WithNodeRefForTenant` post-filter is documented defense-in-depth. **OK.**
- **`GetOutgoingEdgesForTenant` fetch-then-filter is not cross-tenant
  amplification** — A6a forbids cross-tenant edge endpoints, so a node's
  adjacency list is already single-tenant. **OK.**

---

## Recommendations — ordered by *measured* leverage

The bench dictates the order. HNSW-in-the-critical-section is the novel finding,
but it is 2.5% of the write cost *today*; leading with it would headline a
near-zero immediate win. Lead with the WAL floor.

1. **Fix the WAL path so group-commit actually amortizes fsync (H5, then H1).**
   Assign the WAL sequence number under `gs.mu`, then release `gs.mu` before
   parking on the flush — so concurrent writers from different tenants fill one
   batch and share one fsync. This is the first-order write-scaling lever: it is
   what makes write throughput rise with tenant count at all. M2 (marshal-before-
   lock) folds into the same change. *Highest measured leverage.*

2. **Stop full-cross-tenant-scanning on label-absent reads (H4, then M1).**
   Back unlabeled `MATCH` and the GraphQL edge resolvers with per-tenant
   enumeration + index-level pagination instead of fetch-all-then-slice. Fix
   `countNodes` to read `len(index)` rather than clone a bucket. Removes a read
   noisy-neighbor *and* a writer-stall source. *Second — it scales with total
   data and is on live request paths.*

3. **Pre-position by lifting the HNSW insert out of `gs.mu` (H2), and budget for
   the auto-embed 2× (H3).** Once (1) lands and the fsync floor is gone, the
   ~140 µs serialized HNSW insert becomes the dominant write term — and under
   auto-embed it is paid twice per node. `HNSWIndex` already has its own mutex,
   so the lift is low-risk; sequence it *after* (1) so the win is real, not
   notional. *Third — the next ceiling, pre-positioned.*

4. **Index-structure hygiene (M3, M4, M7).** Replace O(K) label-slice removal
   with a set/sorted-slice; give `findNewEntryPoint` an O(1) candidate set; drop
   the dead global `nodesByLabel`/`edgesByType` mirror. Each shrinks the
   critical section and/or the memory/snapshot footprint at scale.

5. **Vector-search read hygiene (M5, M6).** `sync.Pool` the HNSW visited set;
   cache query/stored norms. Independent of the write-path work; bites under
   sustained concurrent search.

### Suggested instrumentation (cheap, high-signal)

- Per-write histogram of **time-under-`gs.mu`**, tagged by op. A p99 spike
  without a p50 spike is the fingerprint of HNSW-under-lock (H2) or a label-scan
  delete (M3).
- Counter + duration for `GetAllNodesForTenant`/`GetAllEdgesForTenant`, tagged
  by label-present. Makes H4 observable in production.
- `len(nodesByLabel[label])` for high-cardinality labels at snapshot time —
  detects the M7 unbounded growth.

---

## What this audit does *not* claim

- It does **not** re-measure Track R 1a/1b/1c (per-tenant heap proportionality,
  backpressure, auto-embed searchability) — those are closed.
- The concurrency bench shows write serialization is real and fsync-dominated;
  it does **not** isolate `gs.mu` from shared-WAL fsync. Both point to the same
  fix order (WAL first), but the distinction is preserved deliberately.
- Findings are tagged MEASURED vs ANALYTICAL; most are analytical (code-path
  inference). The headline write-serialization shape, the HNSW insert cost, and
  the batching pathology are MEASURED.
