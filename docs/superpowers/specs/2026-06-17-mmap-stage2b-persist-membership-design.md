# Design: mmap reopen Stage 2b — persist membership indexes

**Date:** 2026-06-17
**Status:** Design approved; implementation not started.
**Ask:** graphdb ask #1 ("make reopen of a large persisted store cheap"), Stage 2b.
**Builds on:** Stage 2a (`docs/superpowers/specs/2026-06-17-mmap-stage2-derived-indexes-design.md`, branch `feat/mmap-stage2-derived-indexes` / PR #412). **Stacked on that branch** — rebase onto `main` once 2a merges.

## The problem

Stage 2a took mmap-mode store *open* to ~7ms by persisting adjacency as CSR and deferring
the membership-index build. The membership build (74% of the old residual, ~2s at consumer
scale) is now lazy — paid once, on the **first enumeration query** after reopen
(`GetNodesByLabelForTenant`, `GetAllNodesForTenant`, pagination, …). For a consumer that
enumerates immediately on reopen, that ~2s stall is just relocated, not removed. Stage 2b
persists the membership indexes as mmap-native sections so the first enumeration is also ~0.

## Approach (chosen)

Membership is an inverted index (key → sorted IDs), structurally identical to the CSR
adjacency Stage 2a already persists (nodeID → sorted edge IDs). Apply the same solution:
persist sorted-ID runs per membership key in `snapshot.mmap`, and have the enumeration
readers compute *base run − tombstones ∪ overlay* through accessor helpers — **replacing**
the 2a lazy build, whose only job was materializing those maps. Reads become stateless and
zero-allocation on the base, mirroring `getEdgeIDsForNode`.

Rejected alternatives: per-key lazy materialization with caching (adds cache-invalidation
complexity on post-cache overlay writes); serialized-blob eager deserialize at open (still
O(N) allocation at open — the very thing 2a/2b exist to avoid).

## Component 1 — Format v3 → v4: the membership section

A new section in `snapshot.mmap`, written after the CSR adjacency sections. Four
**per-tenant** inverted indexes, each a set of sorted-ID runs encoded with the existing
2a `appendCSRRun`/`readCSRRun` codec (length-prefixed `[]uint64`):

| Kind | Composite key | Run = sorted IDs of |
|---|---|---|
| 0 | tenant | all node IDs in tenant (enumeration set; includes unlabeled nodes) |
| 1 | tenant `␀` label | node IDs carrying that label in tenant |
| 2 | tenant | all edge IDs in tenant |
| 3 | tenant `␀` type | edge IDs of that type in tenant |

(`␀` = a 0x00 separator byte; tenant/label/type are the raw strings.)

**Membership directory:** `count(4)`, then per entry `keyLen(2) │ keyBytes(incl. leading kind byte) │
runOffset(8) │ runCount(8)`, sorted by full keyBytes (kind is the first byte of the key, not a
separate field) for binary search at read. The
header gains `membDirOffset(8)` and `membDataOffset(8)`; `computeCRC` extends to cover the
membership directory (the runs, like node/edge records and CSR runs, are paged in lazily
and excluded from the CRC). Version bump `mmapSnapshotVersion` 3 → 4.

**Global (across-tenant) queries are derived, not persisted.** `FindNodesByLabelAcrossTenants`
/ `FindEdgesByTypeAcrossTenants` iterate the persisted tenant list (the keys of
`metadata.TenantStats`, already persisted in 2a) and union each tenant's kind-1 / kind-3
run. This avoids a redundant global section (for the common single-tenant store, the global
index would duplicate the one tenant's index exactly).

**Size:** each node ID appears in ~2 runs (its tenant-all set + its tenant-label set); each
edge ID likewise. ~+30 MB at the 937k-node / 1.3M-edge consumer scale, on top of the ~21 MB
CSR — still far below the 456 MB JSON snapshot. Stated in release notes.

## Component 2 — Accessors + overlay/tombstone merge

Six accessor helpers replace the direct membership-map reads. Each returns a **sorted
`[]uint64`** (exactly what every reader collects today), encapsulating the JSON-vs-mmap
branch like `getEdgeIDsForNode`:

```
membershipNodeIDsForTenant(tid)        → kind 0
membershipNodeIDsByLabel(tid, label)   → kind 1
membershipEdgeIDsForTenant(tid)        → kind 2
membershipEdgeIDsByType(tid, etype)    → kind 3
membershipNodeIDsByLabelGlobal(label)  → ∪ kind 1 over the tenant list
membershipEdgeIDsByTypeGlobal(etype)   → ∪ kind 3 over the tenant list
```

Plus a small `membershipLabelsForTenant(tid)` / `membershipEdgeTypesForTenant(tid)` for the
key-only readers (`GetLabelsForTenant`, `GetEdgeTypesForTenant`): enumerate the directory
keys of the relevant kind for that tenant, unioned with the overlay map's keys.

**Merge (each ID accessor):** `(base run for key − tombstoned IDs) ∪ (overlay set for key)`,
then sort ascending. The base run is a zero-allocation slice decoded from the mapping; the
overlay is the existing in-memory map (now holding only post-open deltas); tombstones are the
Stage-1 `deletedNodes`/`deletedEdges` sets. Post-open IDs are disjoint from base IDs
(`> snapshot NextNodeID/NextEdgeID`), so the union needs no dedup. **JSON mode**
(`mmapSnap == nil`): returns the in-memory map's set directly — byte-identical to today.

The ~18 enumeration readers change from e.g. `gs.tenantNodesByLabel[tid][label]` (a
`map[uint64]struct{}` they range-and-sort) to `gs.membershipNodeIDsByLabel(tid, label)` (an
already-sorted `[]uint64`) — often *simplifying* the call site, since they already
collect-then-sort. Readers: `GetNodesByLabelForTenant`, `CountNodesByLabelForTenant`,
`GetEdgesByTypeForTenant`, `GetAllNodesForTenant`, `GetAllEdgesForTenant`,
`GetLabelsForTenant`, `GetEdgeTypesForTenant`, `ListTenants` (tenant list from
`tenantStats`), the four `pagination.go` pagers, `FindNodesByLabelAcrossTenants`,
`FindEdgesByTypeAcrossTenants`, `GetAllLabels` (global label keys = sticky keys ∪ overlay —
already works without a run accessor), and `CreateNodeWithUniquePropertyForTenant` (its
uniqueness scan reads `membershipNodeIDsByLabel(tid, uniqueLabel)`).

**Deletions (the 2a build is superseded):** remove `membership_lazy.go`
(`ensureMembershipBuilt`, `addNodeToTenantIndexNoCount`, `addEdgeToTenantIndexNoCount`), the
`membershipBuilt` field, and every `gs.ensureMembershipBuilt()` call site (replaced by the
accessor at the point of read).

**Write-path interaction (unchanged from today):** `addNodeToTenantIndex`/`addEdgeToTenantIndex`
still populate the in-memory maps for newly-created nodes/edges — those maps are now the
mmap-mode overlay. Deletes tombstone base entities (filtered from the run) and remove overlay
entries. Per-tenant counts stay on the 2a metadata-restore + write/replay-delta path; the
membership accessors never touch counts, so `CountNodesForTenant` is unaffected.

## Component 3 — Writer / persist / loader

- **Writer** (`mmap_snapshot_writer.go`): after the CSR sections, build the four inverted
  indexes by bucketing the already-sorted `nodes`/`edges` slices (each node → its tenant
  enumeration run + one run per label; each edge → tenant run + type run), emit the runs and
  the membership directory, set the header offsets, extend the CRC. Derived purely from the
  `nodes`/`edges` passed to `writeMmapSnapshotData`, so `Snapshot`/`Close` (the persist merge
  in `mmap_snapshot_persist.go`) round-trips it with no extra work — exactly as CSR does.
- **Loader** (`mmap_snapshot_loader.go`): no membership build. The in-memory membership maps
  start empty (overlay-only); sticky label/type keys are still registered at open (so
  `GetAllLabels` exposes a label whose last member was deleted). Drop all 2a build wiring.
- **Reader** (`mmap_snapshot_reader.go`): `membershipRun(kind byte, key []byte) []uint64`
  — binary-search the membership directory, decode the run via `readCSRRun`; plus a directory
  key-enumeration helper for the label/type-key accessors and the tenant-derived globals.

## Testing — parity gate vs JSON (extends `mmap_reopen_test.go`)

`checkGraphInvariants` remains incompatible with the lazy representation; correctness is
public-interface parity vs JSON.

- **Enumeration parity at open with ZERO prior calls** (the whole point): after an mmap
  reopen, `GetNodesByLabelForTenant`, `GetAllNodesForTenant`, `GetEdgesByTypeForTenant`,
  `GetAllEdgesForTenant`, and the four pagers return results identical to a JSON reopen
  **without** any preceding query — proving the data is served from the persisted section,
  not a build.
- **Overlay + tombstone parity:** after post-open create / delete / cascade, enumeration
  matches JSON — live and after a second reopen (overlay runs become base runs; tombstoned
  base IDs are gone from the rewritten file).
- **Global across-tenant** queries (`FindNodesByLabelAcrossTenants`) match JSON with
  multiple tenants.
- **Uniqueness consumer contract:** `TestMmapStage2_UniqueConstraintSurvivesReopen` (from 2a)
  must still pass with the accessor-based scan — duplicate rejected after reopen with no
  prior enumeration.
- **Counts** unaffected: `CountNodesForTenant` still correct at open (metadata path).
- Cross-tenant isolation; `-race`; full `-short` suite (JSON path unchanged).
- **Bench:** re-run `TestMmapReopen_EndToEnd` AND add a first-enumeration measurement
  (reopen, then time the first `GetAllNodesForTenant`) — confirm it drops from ~2s to ~0.

## Invariants preserved

Stable IDs across reopen; tenant-strict enumeration (no cross-tenant leak); the
membership accessors reproduce the *results* of the in-memory indexes (label/type membership,
per-tenant enumeration), asserted by parity. Counts remain correct without the build.

## Out of scope

- mmap + at-rest encryption; mmap + `UseDiskBackedEdges` (still fall back to JSON).
- The pre-existing `DeleteAllNodes` mmap-mode bug surfaced in the 2a final review (leaves the
  base mapped) — tracked separately; not introduced or addressed here.
- Persisting property/vector indexes (already restored from metadata; not a reopen cost).
