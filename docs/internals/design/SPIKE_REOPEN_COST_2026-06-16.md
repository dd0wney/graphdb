# Spike: cheap reopen of a large persisted store (graphdb ask #1)

**Date:** 2026-06-16
**Status:** Investigation complete — design decided, implementation not started.
**Branch artifact:** `pkg/storage/load_profile.go` + `pkg/storage/reopen_cost_bench_test.go`.

## The ask

Reopening an on-disk graph should cost far less than rebuilding it from scratch.
Today it doesn't: for a ~1M-node graph, `NewGraphStorage(dataDir)` takes about as
long as (slightly longer than) building the graph fresh, defeating the purpose of
persisting. General-purpose DB concern; benefits every client with a non-trivial
persisted graph.

**Acceptance criterion:** reopen of a persisted 937k-node / 1.3M-edge store is
materially faster than the cold build that produced it (reopen ≪ rebuild), not the
current ~1.0×.

## What was measured

Synthetic store of the consumer's reported shape — 936,908 nodes (label + two packed
int locations + a short string), 1,316,003 edges across 4 types, one tenant, edge
compression at its `NewGraphStorage` default. Built, snapshotted via `Close()`,
process state dropped, then `NewGraphStorage(dir)` timed. Reproduces the consumer's
numbers (see `TestReopenCost_Synthetic`):

| Phase | This spike | Consumer report |
|---|---|---|
| Cold build (from scratch) | 15.7s | 15.8s |
| `Close()` → snapshot to disk | 8.6s (456 MB) | 9.6s (514 MB) |
| Reopen (`NewGraphStorage`) | 14.2s | 16.4s |
| **reopen / rebuild ratio** | **0.90** | **1.04** |

(The spike's ratio is a touch lower than the consumer's because the synthetic edges
carry no properties; the consumer's likely do. Shape and conclusion are unaffected.)

### `loadFromDisk` phase breakdown (14.18s total, via `GRAPHDB_LOAD_PROFILE=1`)

| Phase | Time | % | Category |
|---|---|---|---|
| `os.ReadFile` | 116ms | 0.8% | blob decode (a) |
| **`json.Unmarshal`** | **10.5s** | **74.2%** | blob decode (b) |
| rebucket nodes+edges | 319ms | 2.3% | index rebuild (c) |
| edge adjacency rebuild | 782ms | 5.5% | index rebuild (d) |
| node label+tenant index loop | 1.09s | 7.7% | index rebuild (e) |
| edge type+tenant index loop | 1.34s | 9.5% | index rebuild (e) |

**Blob decode (a+b) = 75.0% · derived-index rebuild (c+d+e) = 25.0%.**

For the synthetic store the post-`loadFromDisk` phases (`replayWAL`,
`rebuildVectorIndexesFromNodes`) are ~0: the WAL is truncated on `Close()` and there
are no vector indexes. `loadFromDisk` is essentially all of reopen.

## What the split decides

The pre-spike hypothesis (in the ask) was: *"a faster blob decode alone will not
reach reopen ≪ rebuild — you almost certainly also need to persist the derived
indexes."* **The measurement contradicts this.** Derived-index rebuild is only 25%.
If blob decode is driven toward zero, reopen → ~3.5s against a 15.7s rebuild —
**ratio ~0.22, comfortably under the acceptance bar.** Attacking the decode alone
clears the criterion; persisting the derived indexes is the second-order win needed
for the consumer's full ~0.1s end-state, not for the bar.

### The sharper sub-question (decides the format approach)

The 10.5s `json.Unmarshal` is not purely parsing — it also allocates 936k
`map[string]Value` property bags, boxes every `Value`, and builds the flat
`map[uint64]*Node`/`map[uint64]*Edge`. A binary format decoded into the **same**
in-memory structures still pays most of that allocation cost. So the next cheap
experiment — splitting the 10.5s into *parse* vs *allocation* — decides between:

- **parse-dominated →** a binary/streaming/codegen decoder (e.g. a generated
  marshaler) into the existing structures is sufficient; or
- **allocation-dominated →** the representation must change: mmap the snapshot and
  materialize nodes/properties lazily so reopen is O(pages touched), not O(N).

mmap + lazy materialization is the safe bet that wins under either outcome, at the
cost of a larger change to the in-memory model.

### Follow-up experiment result: it is allocation-bound (2026-06-16)

`TestReopenParseVsAlloc_Synthetic` ran four decodes over the same 455.9 MB payload
at 936,908 nodes / 1,316,003 edges:

| Variant | Wall | Alloc | Mallocs | NumGC | GC pause |
|---|---|---|---|---|---|
| 1. `json.Valid` (scan only) | 2.33s | ~0 | 9 | 0 | 0 |
| 2. Unmarshal, props=`RawMessage` | 7.82s | 0.65 GB | 12.2M | 1 | 0 |
| 3. Unmarshal full (real types) | 10.76s | 1.07 GB | 25.3M | 1 | 0 |
| 4. Unmarshal full, **GC disabled** | 10.59s | 1.07 GB | 25.3M | 0 | 0 |

Decomposition of the 10.76s full decode:

- **Scan floor (1): 2.33s = 22%** — pure JSON lexing.
- **Allocation + tree-building (3−1): 8.43s = 78%** — everything beyond scanning.
- **Property-bag cost (3−2): 2.94s = 27%** — the `map[string]Value` + `Value` boxing.
- **Structs + flat maps (2−1): 5.49s = 51%** — node/edge structs, flat maps, slices.
- **GC overhead (3−4): 0.17s = 1.6% — negligible.**

**Conclusion: allocation-bound, not parse-bound, and not GC-bound.** 78% of the
decode is allocating the 25.3M heap objects that make up the graph (~11 allocations
per entity: the struct, `Labels` slice, the property `map`, and a `[]byte` per
`Value`). GC barely runs (one cycle) because those allocations are nearly all *live*
— the result graph — so there is little garbage to collect; the cost is `mallocgc`
itself (size-class, zeroing, pointer write-barriers). The GC-off run confirms it
(same wall time).

**Implication:** a faster *decoder* (binary/streaming/codegen) into the **same**
`map[uint64]*Node` + `map[string]Value` structures can only attack the 22% scan — it
cannot avoid the 25.3M allocations, so it yields a modest win at best. Reaching
reopen ≪ rebuild on the decode side requires **not allocating the graph up front**:
mmap the snapshot and materialize nodes/properties lazily on access, so a cold reopen
is O(pages touched), not O(N). The property-bag share (27%) is itself a clean lever —
defer `map[string]Value` construction until a property is read.

## Recommended staging

1. **Stage 1 — attack the 74% blob decode (clears acceptance).** The follow-up
   experiment shows this must be **mmap + lazy materialization**, not merely a binary
   decoder: 78% of the decode is allocation, which an eager decode into the same
   structures cannot avoid. Gets reopen toward ~0.22× rebuild (and lower as fewer
   nodes are touched on a given reopen).
2. **Stage 2 — eliminate the 25% O(N) index rebuild (reaches the ~0.1s dream).**
   Persist the derived indexes — compressed adjacency (`compressedOutgoing/Incoming`,
   currently *not* serialized), per-tenant indexes (H4.3), label/type membership — so
   they are loaded/mmapped rather than recomputed; or build them lazily on first use.

## Stage 1 prototype: mmap + lazy materialization — VALIDATED (2026-06-16)

A self-contained prototype (`mmap_proto_*.go`, exercised by `TestMmapReopen_Synthetic`)
writes the real `Node`/`Edge`/`Value` graph to a binary, mmap-able file with a dense
ID→offset directory, opens it by mapping the file + reading only the header, and
materializes nodes/edges lazily on access (property `Value.Data` aliases the mapping —
no copy, no JSON parse). Head-to-head at 936,908 nodes / 1,316,003 edges:

| Variant | Wall | Alloc | Mallocs |
|---|---|---|---|
| JSON `ReadFile`+`Unmarshal` (baseline) | 10.45s | 1.07 GB | 25.3M |
| **mmap open** | **~0 (sub-ms)** | **0** | **7** |
| mmap touch-all (every node+edge) | 0.63s | 0.70 GB | 14.6M |
| mmap random-10k reads | 7ms | 0.01 GB | 100k |

Snapshot size: **191.6 MB binary vs 455.9 MB JSON** (2.4× smaller — no base64/JSON
overhead). 1000 sampled nodes decoded via mmap matched a fresh JSON reopen exactly.

**Findings:**
- **Cold open is effectively free** — sub-millisecond, 7 allocations vs 25.3M. The graph
  is not built until touched. This is the result the spike predicted.
- **Even eager full materialization is ~16.5× faster** than JSON decode (0.63s vs 10.45s):
  binary records + aliased property bytes avoid the parse and the per-`Value` `[]byte`
  copies.
- **Lazy reads are ~free** — open + 10k random reads in 7ms.

**Scope / what this does NOT yet show (the productionization gap):**
- This is the **blob-decode 75% solved**. The derived-index rebuild (Stage 2, ~25% /
  ~3.7s) is *not* addressed and is still O(N) allocation. A production reopen that needs
  indexes would land at ≈3.7s + ~0 (lazy nodes) ≈ **0.24× rebuild** — clearing the bar,
  matching the prediction — and lower once indexes are persisted/lazy too.
- **Writes after reopen** need a copy-on-write overlay (mmap is read-only). Not built.
- **At-rest encryption** is incompatible with mapping the file as-is. Prototype is plaintext.
- **Dense-ID directory** assumes IDs dense from `minID` (true for a freshly built store; a
  `-1` sentinel handles gaps, but heavy deletion would want a sorted directory).
- `Value.Data` aliases the mapping → valid only while open; production must copy or pin.
- Platform: `syscall.Mmap` (unix; CI is Linux/macOS). `golang.org/x/exp/mmap` is the
  portable copying fallback.

**Conclusion:** the recommended direction is validated. Stage-1 productionization =
wire an mmap-backed read provider into reopen behind a CoW overlay for writes; Stage 2 =
persist/lazy the derived indexes.

## Stage 1 productionized — RESULT (2026-06-17)

The flag-gated mmap reopen mode (`StorageConfig.UseMmapSnapshot` /
`GRAPHDB_STORAGE_MODE=mmap`) is wired through the full `storage.Storage` interface:
`loadFromDiskMmap` maps `snapshot.mmap` and builds only the in-memory indexes via a
field-scan; reads resolve overlay → mmap base; writes promote into the shard overlay
(copy-on-write) or tombstone; `Snapshot`/`Close` write the merged `.mmap`. Encrypted
and disk-backed-edge stores fall back to the JSON path. Off by default.

**End-to-end reopen via the real `NewGraphStorage`, 936,908 nodes / 1,316,003 edges:**

| | wall | reopen/rebuild |
|---|---|---|
| cold build (JSON) | 16.41s | — |
| JSON reopen (baseline) | 14.42s | 0.88 |
| **mmap reopen** | **2.91s** | **0.18** |

**5.0× faster than the JSON reopen, 0.18× of the cold build** — clearing the acceptance
criterion (reopen ≪ rebuild), and matching the spike's ~0.24× prediction. The residual
2.9s is the eager index field-scan — exactly Stage 2's target (persist/lazy the indexes
→ approach ~0).

Correctness is gated by public-interface PARITY against JSON mode (the internal
invariant checker assumes shards hold every node, which the lazy representation breaks):
round-trip, build-vs-mmap parity, writes-after-reopen (node + edge update/delete/cascade/
create), batch parity, and crash-recovery (WAL replay) all assert identical results live
AND after a second reopen; cross-tenant isolation preserved; race-clean. Full
`pkg/storage` suite green in the default mode (every helper no-ops when `mmapSnap == nil`).

## Invariants any new format MUST preserve

- Stable node/edge IDs across reopen.
- Tenant-strict CRUD: `CreateNodeWithTenant`, `GetNodeForTenant`, cross-tenant reads
  returning a not-found-equivalent error (no existence leak).
- `Executor.ExecuteWithContext` honoring `tenant.WithTenant(ctx, …)`.
- `algorithms.ShortestPathForTenant` / `KHopNeighboursForTenant` over `storage.Storage`.
- Correct edge adjacency and per-tenant indexes after reopen — whatever replaces the
  rebuild passes must reproduce their *results* (compressed-adjacency + H4.3 fixes),
  not just their speed. The synthetic repro asserts node/edge counts survive; a format
  PR must extend this to adjacency + per-tenant query results.

## How to reproduce

```bash
# Full-scale (≈45s wall, ~456MB snapshot in a temp dir):
GRAPHDB_REOPEN_BENCH=1 \
  go test ./pkg/storage/ -run TestReopenCost_Synthetic -count=1 -timeout 600s -v

# Quick iteration at a smaller size:
GRAPHDB_REOPEN_BENCH=1 GRAPHDB_REOPEN_NODES=50000 GRAPHDB_REOPEN_EDGES=70000 \
  go test ./pkg/storage/ -run TestReopenCost_Synthetic -count=1 -v
```

`GRAPHDB_LOAD_PROFILE=1` enables the `loadFromDisk` phase breakdown on any reopen
(the test sets it automatically for the reopen leg); it is a zero-overhead env-gated
profiler safe to use against a real slow restart in production.

## Related, lower-priority asks (context, not blockers)

- **#3 Incremental durability:** `Close()` calls `Snapshot()` (full ~456MB rewrite)
  every time. A WAL-append mode with snapshot on a size/age threshold makes small
  mutations O(change). graphdb already has WAL + `CompactWAL`, so this is largely
  "don't force a full snapshot on `Close()`."
- **#2 Bulk delete-by-predicate** (`DeleteNodesByPropertyForTenant`): ergonomics
  complement to the bulk-create API; consumers can loop existing primitives meanwhile.
