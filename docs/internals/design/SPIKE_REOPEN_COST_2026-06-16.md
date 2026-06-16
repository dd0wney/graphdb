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

## Recommended staging

1. **Stage 1 — attack the 74% blob decode (clears acceptance).** Replace the JSON
   snapshot with a binary layout (and/or mmap + lazy materialization, pending the
   parse-vs-alloc result). Biggest single win; gets reopen to ~0.22× rebuild.
2. **Stage 2 — eliminate the 25% O(N) index rebuild (reaches the ~0.1s dream).**
   Persist the derived indexes — compressed adjacency (`compressedOutgoing/Incoming`,
   currently *not* serialized), per-tenant indexes (H4.3), label/type membership — so
   they are loaded/mmapped rather than recomputed; or build them lazily on first use.

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
