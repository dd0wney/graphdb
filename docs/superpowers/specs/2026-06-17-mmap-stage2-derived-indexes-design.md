# Design: mmap reopen Stage 2 — persist/lazy the derived indexes

**Date:** 2026-06-17
**Status:** Design approved; implementation not started.
**Ask:** graphdb ask #1 ("make reopen of a large persisted store cheap"), Stage 2.
**Predecessor:** `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` (Stage 1 productionized — mmap reopen at 0.18× rebuild). Stage 1 PRs: #408/#409/#410.

## The problem

Stage 1's mmap reopen mode (`GRAPHDB_STORAGE_MODE=mmap`) already clears the acceptance
bar — reopen of a 936,908-node / 1,316,003-edge store dropped 14.4s → 2.9s (0.18×
rebuild). The residual ~2.9s is the **eager derived-index field-scan** in
`loadFromDiskMmap`: two loops over every node/edge ID that rebuild the in-memory
membership indexes and the adjacency maps. Stage 2 removes that residual, taking
reopen toward ~0 (the consumer's ~0.1s dream).

## The measurement that shaped this design (2026-06-17)

The Stage-1 `loadFromDisk` profiler was JSON-path only. I wired the same zero-overhead
`loadProfiler` into `loadFromDiskMmap` with phase marks (membership vs adjacency split
via a diagnostic two-pass), and ran `TestMmapReopen_EndToEnd` at full consumer scale
(`GRAPHDB_REOPEN_BENCH=1 GRAPHDB_LOAD_PROFILE=1`). The 2.8s residual splits:

| Phase | Time | Share | Nature |
|---|---|---|---|
| mmap open + CRC | 3ms | 0.1% | already ~0 |
| node membership (`nodesByLabel`, tenant indexes) | 934ms | 33% | map-set inserts |
| edge membership (`edgesByType`, tenant indexes) | 1,144ms | 41% | map-set inserts |
| edge adjacency (`outgoingEdges`/`incomingEdges`) | 724ms | 26% | slice appends |
| property/vector/stats | ~0 | 0% | tiny metadata |

**Membership indexing is 74% of the residual; adjacency is only 26%** — inverting the
intuition that adjacency's slice-append churn dominates. Measure-before-design (the
spike idiom) changed the plan: optimizing adjacency first would have chased the smaller
quarter. The instrumentation was reverted after measuring; re-adding clean single-loop
profiler marks is the first implementation step.

## The structural asymmetry

The two halves of the residual want **opposite** treatments:

| | Adjacency (26%) | Membership (74%) |
|---|---|---|
| Read sites | **1 chokepoint** — `getEdgeIDsForNode` (already branches disk/compressed/uncompressed) | **~20 diffuse** — pagination.go, query_operations.go, tenant_operations.go; many iterate whole maps |
| Lazily rebuildable? | **No** — finding one node's edges needs a full edge scan | **Yes** — derived from a node/edge scan |
| Stage 2 treatment | **Persist** as CSR; add one mmap-base branch | **Defer** — build lazily on first enumeration |

## Scope and staging

- **Stage 2a (this work):** persist adjacency as CSR + lazy-build membership. Target:
  mmap reopen **2.8s → ~3ms** for the open-then-traverse path; the first enumeration
  query (if any) pays the membership scan once, attributed to that query.
- **Stage 2b (documented follow-up, NOT built now):** persist membership as
  mmap-native sections so first-enumeration is also ~0. Deferred by explicit user
  decision (the likely consumer pattern is open-then-traverse, not enumerate-on-open).
  Mirrors the Stage 1 Phase 0/1 split.

Off-by-default discipline and the "no-op when `mmapSnap == nil`" rule are preserved
exactly; the JSON path stays byte-for-byte unchanged. The correctness gate stays
public-interface parity vs JSON (`checkGraphInvariants` does not work in mmap mode —
the lazy representation breaks its "shards hold every node" assumption).

## Component 1 — Adjacency: persisted CSR, immutable base + tombstone-filter + overlay-append

### On disk (format v3)

Two new sections in `snapshot.mmap`, written after the edge records:

- **Outgoing CSR:** per node (ascending ID), a run of its outgoing edge IDs.
- **Incoming CSR:** per node, a run of its incoming edge IDs.
- A dense directory `nodeID → (outOffset, outLen, inOffset, inLen)` reusing the
  `minNodeID`-based dense-directory idiom already in the format (`dirAbsent` sentinel
  for nodes with no edges on a side).

### The read model (reuses Stage 1 tombstones — no copy-on-write)

The base CSR is **immutable**. `getEdgeIDsForNode` in mmap mode returns:

> **(base CSR run for the node, minus tombstoned edge IDs) ∪ (overlay `outgoingEdges[node]`
> of edges created since open, minus tombstoned)**

- **Deletes** are already handled by Stage 1: `DeleteEdge` tombstones the edge
  (`deletedEdges` set). Adjacency read filters the base run by that set. No CoW, no
  empty-entry-shadowing bug.
- **Creates** append the new edge ID to the overlay map at the existing write site
  (`edge_operations.go`, `batch_executor.go`) — unchanged. New edge IDs are disjoint
  from base IDs (fresh IDs > base max), so the union needs no dedup.
- Per-read cost is O(degree) decode + tombstone-filter — paid only when a node is
  traversed, never at open.

This is **one new branch in `getEdgeIDsForNode`** (the single adjacency chokepoint) and
**zero changes to the ~34 adjacency write sites** — they keep mutating the overlay maps,
which now hold only post-open deltas. `node_adjacency.go`'s
`delete(gs.outgoingEdges, nodeID)` cleanup stays correct because base adjacency never
lived in that map.

### Reader additions (`mmap_snapshot_reader.go`)

`outgoingCSR(id)` / `incomingCSR(id)` return a freshly-decoded `[]uint64` (copy-on-read,
consistent with `readProps`). Zero edges on a side → directory `len==0` → return nil
(must not alias the mapping).

## Component 2 — Membership: lazy build, decoupled stats

### At open

Register sticky label/type keys only (as today). Do **not** scan for membership. A flag
`membershipBuilt bool` (guarded by `gs.mu`) starts false.

### `ensureMembershipBuilt()`

Guarded, runs at most once, called at the top of each enumeration entry point:
`GetNodesByLabel*`, `GetAllNodes*`, `GetAllLabels`, the four `pagination.go` readers, and
the `tenant_operations.go` enumerators. It field-scans the base and, for each base
node/edge ID, **skips any that is shadowed (present in the shard overlay) or tombstoned**,
folding the rest into the membership maps.

**Why skip-shadowed-or-tombstoned is correct against post-open writes:** a base node
relabeled by `UpdateNode` is already in the overlay shard *and* already re-indexed under
its new labels (write-time maintenance, unchanged from Stage 1). The build skips it, so
it is not re-added under stale base labels. New nodes (`CreateNode`) got fresh IDs
disjoint from base and were indexed at write time. So overlay entities are handled at
write; the build only fills in untouched base entities — no double-count, no stale labels.

### Stats decoupling

Per-tenant counts (`tenantStats`) move into the persisted `mmapMetadata` (tiny — one row
per tenant). `CountNodesForTenant` / `GetTenantStats` read them directly, so **counts are
correct at open without triggering the build**. Consequently `ensureMembershipBuilt`
populates only the label/ID-set maps (`nodesByLabel`, `tenantNodesByLabel`,
`tenantNodeIDs`, and edge equivalents) via a **build-only insert** that does NOT call the
count-incrementing path (`incrementTenantNodeCount`/`incrementTenantEdgeCount`).

## Component 3 — Format v3 + writer / persist changes

- **`mmap_snapshot_format.go`:** version 2→3; add to the header the offsets for the two
  CSR data sections (outgoing, incoming) plus the **single combined adjacency directory**
  (`(outOffset, outLen, inOffset, inLen)` per node, dense by `nodeID − minNodeID`); add
  `TenantStats map[string]TenantStats` to `mmapMetadata`. Extend `computeCRC` to cover the
  new directory.
- **`mmap_snapshot_writer.go`:** after the edge records, emit the two CSR sections by
  bucketing edge IDs per `FromNodeID` / `ToNodeID` (sorted) with a dense directory.
  `buildMmapMetadata` gathers `tenantStats`.
- **`mmap_snapshot_persist.go`** (`Snapshot`/`Close` merge): the merged write already
  enumerates live nodes via `forEachNodeUnlocked` and merges live edges; CSR is derived
  from that same live edge set, so the snapshot round-trips. No on-disk format downgrade —
  v3 is written only in mmap mode; the JSON path is untouched.
- **`mmap_snapshot_loader.go`:** drop the two eager index loops; keep sticky-key
  registration, property/vector defs, nextIDs, stats; load `tenantStats` from metadata;
  re-add clean single-loop profiler marks (no membership/adjacency rebuild left to mark).

## Testing — public-interface parity gate (extends `mmap_reopen_test.go`)

- **Adjacency parity vs JSON after reopen** for: untouched base node; base node with a
  post-open edge added; base node with a base edge deleted (tombstone-filter); a node
  with both; a brand-new node — asserted live AND after a second reopen.
- **Membership parity:** `GetNodesByLabel` / `GetAllNodesForTenant` / pagination identical
  to JSON, asserted **both before and after** the first enumeration call (proves the lazy
  build triggers and is correct), including after relabel / delete / create.
- **Stats decoupling:** `CountNodesForTenant` correct **without** any enumeration call.
- Cross-tenant isolation preserved; `-race` clean on the `ensureMembershipBuilt` guard
  (concurrent first-enumeration).
- Re-run `TestMmapReopen_EndToEnd` to confirm the new open time and that the profiler
  shows membership and adjacency gone from open.

## Risks

1. **`ensureMembershipBuilt` race** — concurrent first-enumeration readers. Guard with
   `gs.mu` write-lock; double-check the flag inside. Once per process; acceptable.
2. **Zero-edge CSR node** — directory `len==0`, return nil; must not alias the mapping.
3. **CSR size growth** — adds ~2 × edgeCount × 8 bytes (~21 MB at consumer scale) to the
   file; still far below the JSON snapshot. State in release notes.
4. **Tombstone filter on every adjacency read** — O(degree) set-membership check per
   read. Negligible, but confirm with a quick traversal bench that the hot path does not
   regress.

## Invariants preserved (from the spike's list)

Stable IDs across reopen; tenant-strict CRUD with no existence-leak; tenant-context
honoring in the executor; correct adjacency + per-tenant query *results* (not just speed)
after reopen, asserted via parity. The synthetic repro asserts counts survive; this work
extends it to adjacency + per-tenant enumeration results.

## Out of scope (explicitly)

- Persisting membership as mmap-native sections (Stage 2b).
- mmap + at-rest encryption; mmap + `UseDiskBackedEdges` (both still fall back to JSON).
- Incremental durability (#3) and bulk delete-by-predicate (#2) — separate asks.
