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

A new embedding consumer, `oit-cyber/mapper` (session `mapper-6b`), drove most of the 2026-10-08
findings. It pins v1.6.0 and moves to v1.7.0 next.

---

## The queue

Ranked within each track; tracks are ordered by the cost of their worst defect (silent wrong
data first, then hidden failures, then missing features). **D** marks an item that needs a design
or spec before code; **S** a spike.

### Track C — Cypher correctness

1. `v1.4-cypher-implicit-grouping` — `RETURN n.city, count(n)` returns one row per node with
   `count = null`; only the non-Cypher `GROUP BY` groups. Short in-chat design first (keep or
   deprecate `GROUP BY`; `DISTINCT`; aggregates inside `WITH`, which are not computed).
2. *(unseeded)* `WITH` binds `nil` for an undefined variable (`pkg/query/executor.go`), so a
   misspelled name after `WITH` writes or matches nothing with no error.
3. `v1.4-query-sanitizer-skip-literals`, `v1.4-traverse-truncation-header-alias` — Ulysses asks.
4. `v1.4-cypher-merge-update-errors` — `_ =` in `pkg/query/physical_ops_mutate.go`, which only the
   unused `planner.go` reaches. Dead-code hygiene, not a live defect.
5. `v2.0-cypher-plain-delete-refuses-attached-node` — the v2.0 half of the #655 deprecation.

### Track S — storage robustness

1. `v1.4-lock-panic-safety` — a write path that releases a lock without `defer` turns any panic
   into a hung store (the Cypher executor and net/http recover panics). #646 and #650 removed two
   panic sources, not the pattern.
2. `v1.4-batched-wal-flush-interval-validation` — `FlushInterval` 0 panics at open. Small; mapper asked.
3. `v1.4-foreach-node-tenant-and-lock` — tenant-blind, holds `gs.mu.RLock` across the callback;
   `GetEdgesByTypeForTenant` skips a damaged record silently.
4. `v1.4-datadir-flock` — no lock between processes on one data directory. Short design (test
   sweep for double opens, Unix only, NFS).
5. `v1.4-tenant-quota-enforcement` — quotas are stored and never checked.
6. `v1.4-telemetry-stop-waits` — `TelemetryReporter.Stop` does not wait; flaky under load.
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

1. **D** node upsert: `v1.4-storage-upsert-node` → `v1.4-rest-upsert-node` →
   `v1.4-go-client-upsert-node`. New public API in three layers; brainstorm and spec first.
   mapper's team mode waits for it.
2. `v1.4-rest-conflict-node-id` — small; helps team mode before upsert ships.
3. `v1.4-unique-create-property-index` — size with a graphdb benchmark, not mapper's numbers
   (insight on the task).
4. `v1.4-mapper-consumer-contracts` — pin mapper's six storage behaviours.
5. `v1.4-rest-edge-upsert`, `v1.4-rest-property-index-routes`, `v1.4-rest-schema-introspection`,
   `v1.4-rest-idempotency-key`, `v1.4-rest-property-encoding-rule`, `v1.4-node-label-mutation`.
6. **D** `v1.4-optimistic-concurrency` — version field (format bump) plus `If-Match`/412.

### Track E — encryption at rest

1. `v1.4-encryption-no-plaintext-test` — the acceptance check for the next item.
2. `v1.4-encrypt-uniqueness-rules-file` — `rules.json` is plaintext in an encrypted store.
3. `v1.4-encryption-envelope-test-cannot-fail` — searches raw text; values are base64.
4. *(unseeded)* no way to zero the engine key on Close.

### Critical path (proposed)

`implicit-grouping` → `lock-panic-safety` → node-upsert spec → node upsert (three layers) →
`flush-interval-validation`, `datadir-flock` → Track E → Track W.

Parallel-safe at any time: the small Track S items and the Ulysses asks.

---

## Unseeded gaps

- `WITH` binds `nil` for an undefined variable (Track C item 2).
- A float property index may compare NaN and -0 differently from the scan (raised in the #641
  review, not checked).
- No way to zero the encryption key in memory (Track E item 4).

## Corrections to older claims

- `pkg/storage/storage.go` `DefaultStorageConfig` comment: "per-write fsync ... fastest on
  local/NVMe (~11µs)" reproduces only on tmpfs (`b.TempDir()` is `/tmp`). See
  `v1.4-wal-default-remeasure` and the 2026-10-08 handoff.
- `docs/CAPABILITIES_2026-05-10.md` lists `query` as mature with no Cypher gaps. Nine silent-result
  or dropped-clause defects were found on 2026-10-08; all but implicit grouping are fixed (#641,
  #644, #646, #657, #659).
- `CLAUDE.md` gives the `pkg/storage` suite as ~120-170 s; it ran in 43-44 s throughout 2026-10-08.
- The branch-claim hook from #583 is not installed in every clone; claim coord tasks by hand.
