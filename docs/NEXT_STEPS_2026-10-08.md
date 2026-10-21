# Plan: Next Steps (graphdb) — 2026-10-08

**Predecessor**: [`NEXT_STEPS_2026-06-18.md`](./NEXT_STEPS_2026-06-18.md). Its status updates run to
2026-07-17; everything after that lived in coord and in session handoffs only. This checkpoint
rebuilds the queue from coord (`coord status graphdb`) as of v1.7.0.

**Why a fresh doc**: four months and three releases (v1.5.0, v1.6.0, v1.7.0) since the predecessor,
and the 2026-10-08 session alone seeded about 30 coord tasks. Nothing ranked them as one queue.

**`main` HEAD at write time**: `26de5d2` (#660, the v1.7.0 release).

**Source of truth for task state is coord**, not this file: `coord status graphdb`, and
`coord insight list --task graphdb:<id>` before trusting a title written on 2026-10-08 (two titles
are corrected by insights). This file ranks and groups; it does not track status.

---

## Decision: roadmap themes are milestones, not version numbers (2026-10-08)

The roadmap's minor line named a theme per version (`v1.7.0 — Backup, DR & data protection`,
`v1.8.0 — Connectors & SDK completion`, ...). Releases stopped following it: v1.5.0 shipped the
"v1.4" work and v1.6.0/v1.7.0 shipped correctness fixes. The user decided to decouple them: a
theme is a **named milestone** that ships in whichever minor is next when it is ready, and a
version number says only what `docs/STABILITY_POLICY.md` requires (minor for additive or
behaviour-correcting changes, major for breaking ones). `docs/ROADMAP_post_1.0.md` is updated in
the same PR as this file.

## Releases since the predecessor

| Release | Date | Headline |
|---|---|---|
| v1.5.0 | 2026-10-07 | GraphQL index paging, SDK parity, WAL durability arc, Cypher value conversion, Ulysses contracts |
| v1.6.0 | 2026-10-08 | Cypher silent results (index lookup, edge DELETE/SET), bulk-import WAL guard, mmap record-width refusal, decoder checks, readiness probe |
| v1.7.0 | 2026-10-08 | Cypher MATCH join and repeated clauses, per-row CREATE/MERGE with bound-node reuse and direction, plain-DELETE deprecation |
| v1.8.0 | 2026-10-21 | Cypher implicit grouping (#664), lock release on a panic in storage (#665) |

A new embedding consumer, `oit-cyber/mapper` (session `mapper-6b`), drove most of the 2026-10-08
findings. It pins v1.6.0 and moves to v1.7.0 next.

---

## The queue

Ranked within each track; tracks are ordered by the cost of their worst defect (silent wrong
data first, then hidden failures, then missing features). **D** marks an item that needs a design
or spec before code; **S** a spike.

### Track C — Cypher correctness

1. ~~`v1.4-cypher-implicit-grouping`~~ ✅ **DONE 2026-10-08 (#664)** — non-aggregate `RETURN`
   items are the grouping keys, null is its own group, and ORDER BY a non-key after aggregation
   and `sum`/`avg`/`min`/`max` of an entity are refused. Aggregates inside `WITH` remain:
   `v1.4-cypher-with-aggregates`.
2. `v1.4-cypher-with-undefined-variable` (seeded in #663) — `WITH` binds `nil` for an undefined
   variable (`pkg/query/executor.go`), so a misspelled name after `WITH` writes or matches nothing
   with no error.
3. `v1.4-query-sanitizer-skip-literals`, `v1.4-traverse-truncation-header-alias` — Ulysses asks.
4. `v1.4-cypher-merge-update-errors` — `_ =` in `pkg/query/physical_ops_mutate.go`, which only the
   unused `planner.go` reaches. Dead-code hygiene, not a live defect.
5. `v2.0-cypher-plain-delete-refuses-attached-node` — the v2.0 half of the #655 deprecation.

### Track S — storage robustness

1. ~~`v1.4-lock-panic-safety`~~ ✅ **DONE 2026-10-08 (#665)** — 37 critical sections now unlock
   by `defer`; also fixed a nil-map panic in `Transaction.Commit` and a held `txWALBarrier` after a
   panic in the transaction WAL append. One site is left: `snapshotWithBoundary` (item 1a).
1a. ~~`v1.4-snapshot-rlock-panic-safety`~~ ✅ **DONE 2026-10-21 (#672)** — `captureJSONSnapshot`
   and `captureMmapSnapshot` take the read lock with `defer`; an interleaved A/B benchmark showed no
   change in `Snapshot()` cost (−2.3% JSON, −0.3% mmap).
2. ~~`v1.4-batched-wal-flush-interval-validation`~~ ✅ **DONE 2026-10-21 (#670)** — a non-positive
   `FlushInterval` or a negative `BatchSize` is refused at open with `*wal.BatchConfigError`.
3. `v1.4-foreach-node-tenant-and-lock` — tenant-blind, holds `gs.mu.RLock` across the callback;
   `GetEdgesByTypeForTenant` skips a damaged record silently.
4. `v1.4-datadir-flock` — no lock between processes on one data directory. Short design (test
   sweep for double opens, Unix only, NFS).
5. `v1.4-tenant-quota-enforcement` — quotas are stored and never checked.
6. ~~`v1.4-telemetry-stop-waits`~~ ✅ **DONE 2026-10-21 (#671)** — `Stop` waits for the report
   loop; the tests' handler counters raced on `main` and are atomic now.
7. `v1.4-vector-index-backfill-on-create`, `v1.4-label-propagation-cache-neighbours` — Ulysses asks.
8. **D** `v1.4-subsecond-timestamps` — needs a new value type or format version.

### Track W — WAL throughput and memory

1. **S** `v1.4-wal-default-remeasure` — the per-write fsync default was justified by numbers that
   reproduce only on tmpfs; batching is about 7x faster on real disks with concurrency. Measure
   (single writer too), then the user decides the default.
2. `v1.4-bulk-create-group-commit` with `v1.4-storage-batch-atomic-commit` — one WAL group per bulk
   call; `Batch.Commit` keeps partial work on error.
3. **D** `v1.4-mmap-close-memory` — Close builds the snapshot in memory, about 0.75 KB per record.

### Track M — consumer primitives (mapper first)

1. ~~node upsert~~ ✅ **DONE 2026-10-21** — spec
   `docs/internals/design/SPEC_node_upsert_2026-10-21.md` (#676, approved by the user with its
   eleven recommended calls); storage `UpsertNodeForTenant` (#677), REST
   `POST /nodes[/batch]?on_conflict=merge` (#683, CC27–CC31), Go client `Nodes.Upsert` /
   `BatchUpsert` (#684). The builder check and a review of the first build each caught a gap
   before merge (D6; "every covering rule must be the key").
2. ~~`v1.4-rest-conflict-node-id`~~ ✅ **DONE 2026-10-21 (#675)** — the 409 carries
   `conflicting_node_id` (CC26); the Go client exposes `Error.ConflictingNodeID`.
3. `v1.4-unique-create-property-index` — size with a graphdb benchmark, not mapper's numbers
   (insight on the task).
4. `v1.4-mapper-consumer-contracts` — pin mapper's six storage behaviours.
5. `v1.4-rest-edge-upsert`, `v1.4-rest-property-index-routes`, `v1.4-rest-schema-introspection`,
   `v1.4-rest-idempotency-key`, `v1.4-rest-property-encoding-rule`, `v1.4-node-label-mutation`.
6. **D** `v1.4-optimistic-concurrency` — version field (format bump) plus `If-Match`/412.

### Track U — uniqueness-registry coverage (added 2026-10-21)

ADR 0001 named two enforcement surfaces, GraphQL and the REST single create. Three more write paths
skipped the registry and stored a duplicate of a registered key with no error — the failure class
the ADR's own risk section names. Ranked above Track S items 3+ because each is silent wrong data.

1. ~~`v1.4-rest-batch-create-uniqueness-rules`~~ ✅ **DONE 2026-10-21 (#674)** — each batch item
   goes through `CreateNodeWithUniquenessRulesForTenant`.
2. ~~`v1.4-patch-node-uniqueness-rules`~~ ✅ **DONE 2026-10-21 (#685)** — every write through
   `patchNode` (REST `PUT`, Cypher `SET`, GraphQL update) checks the rules; `PUT` answers 409 with
   `conflicting_node_id`.
3. ~~`v1.4-cypher-create-uniqueness-rules`~~ ✅ **DONE 2026-10-21 (#680)** — a refused `CREATE`
   stops the query; a `MERGE` whose create loses a race matches once more; `/query` answers 409.
   The concurrent-`MERGE` duplicate reproduced in 3 of 20 runs before the fix.
4. `v1.4-tx-batch-uniqueness-rules` (seeded in #686) — the storage `Batch` and
   `Transaction.Commit` skip the registry on create and update; the last write paths that do.

### Track E — encryption at rest

1. `v1.4-encryption-no-plaintext-test` — the acceptance check for the next item.
2. `v1.4-encrypt-uniqueness-rules-file` — `rules.json` is plaintext in an encrypted store.
3. `v1.4-encryption-envelope-test-cannot-fail` — searches raw text; values are base64.
4. `v1.4-encryption-key-zeroing` (seeded in #663) — no way to zero the engine key on Close.

### Critical path (proposed)

~~`implicit-grouping`~~ (#664) → ~~`lock-panic-safety`~~ (#665) → ~~release v1.8.0~~ (2026-10-21)
→ ~~node-upsert spec~~ (#676) → ~~node upsert~~ (#677, #683, #684) → ~~Track U items 2–3~~
(#685, #680) → Track U item 4 → `datadir-flock` → Track E → Track W.

Parallel-safe at any time: the small Track S items and the Ulysses asks.

---

## Unseeded gaps

None. #663 seeded all three that were listed here: `v1.4-cypher-with-undefined-variable`,
`v1.4-float-index-nan-negative-zero` and `v1.4-encryption-key-zeroing`.

## Corrections to older claims

- `pkg/algorithms/community_test.go` called `TestClusteringCoefficient_Triangle` a "known flake"
  caused by `sync.Pool` reuse and retried it three times. It was a defect in
  `ClusteringCoefficient`: each neighbour pair was checked in one direction, in map order, so a
  directed triangle scored 1.0 or 0 at random (about 16% of first attempts). Fixed in #682.

- `pkg/storage/storage.go` `DefaultStorageConfig` comment: "per-write fsync ... fastest on
  local/NVMe (~11µs)" reproduces only on tmpfs (`b.TempDir()` is `/tmp`). See
  `v1.4-wal-default-remeasure` and the 2026-10-08 handoff.
- `docs/CAPABILITIES_2026-05-10.md` lists `query` as mature with no Cypher gaps. Nine silent-result
  or dropped-clause defects were found on 2026-10-08; all are fixed (#641, #644, #646, #657,
  #659, and #664 for implicit grouping).
- `CLAUDE.md` gives the `pkg/storage` suite as ~120-170 s; it ran in 43-44 s throughout 2026-10-08.
- The branch-claim hook from #583 is not installed in every clone; claim coord tasks by hand.
