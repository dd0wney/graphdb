# Plan: Next Steps (graphdb) — 2026-06-03

**Predecessor**: [`NEXT_STEPS_2026-05-15.md`](./NEXT_STEPS_2026-05-15.md). This doc supersedes it: that doc's Decision 9 commissioned the performance-under-SaaS-load audit, which earned **Track P**; Track P is now complete, so the critical path is a fresh question again — and this checkpoint resolves it.

**Why a fresh doc**: the 2026-05-15 doc was written at a "no earned critical path" moment after Track R closed. Decision 9 resolved it (commission audit → Track P). **Track P is now fully shipped** (see § State reconciliation), so we are back at the same junction — but this time with a clear, evidence-backed next track rather than a TBD. Per the repo convention, a new dated doc marks the new checkpoint.

**`main` HEAD at write time**: `b08bb70` (#275).

---

## State reconciliation

### Track P — shrink what `gs.mu` guards ✅ **CLOSED**

The 2026-06-02 perf-under-SaaS-load audit (`docs/internals/design/AUDIT_performance_saas_load_2026-06-02.md`) produced a backlog ordered by measured leverage. All of it shipped:

| Item | What | PRs |
|---|---|---|
| (1) WAL group-commit | release `gs.mu` during the fsync wait; **all** node + edge write paths (incl. `RemoveNodeProperties`) | #255, #256, #257, **#275** |
| (2) cross-tenant read scans (H4) | per-tenant `tenantNodeIDs`/`tenantEdgeIDs` enumeration indexes; index-level count (M1) | #259, #260, #261, #262 |
| (3) HNSW-out (H2) | lift the HNSW insert off `gs.mu` (plan-under-lock / apply-after-unlock; race-proven) | #266 |
| (4) M5 | pool the HNSW visited set (8× fewer bytes / 57% fewer allocs) | #269 |
| (4) M6 | cache cosine norms (3.66× per distance call, bit-identical) | #271 |
| (4) M4 | entry-point level index, `findNewEntryPoint` O(N)→O(log N) (~5000× at 50k) | #272 |

**What this gives the next session**: writes scale with tenant count (group commit), label-absent reads are O(tenant) not O(total-DB), and the HNSW hot paths no longer serialize behind `gs.mu` or recompute norms. The audit's MEASURED backlog is exhausted.

**Deferred tail (decision-laden — NOT abandoned, see § Off-path queue):** M3 (label-index O(N²) bulk delete) needs a snapshot-format version bump; M7 (drop the global `nodesByLabel`/`edgesByType` mirror) is a public-API deprecation, not the dead-code delete the audit framed. M7 is the precondition for a clean M3. Both carry decisions in `SESSION_HANDOFF_2026-06-03-0220Z.md` and memory `project_track_p_m3_m7_deferred`.

### Track R / Track H — ✅ CLOSED (carried, unchanged)

Per `NEXT_STEPS_2026-05-15.md` §§ Track R, Track H. The Track R verification gap (1a/1b/1c) is fully discharged. No change this checkpoint.

---

## Critical path — Track Q (NEW): consumer-driven correctness hardening

**SELECTED 2026-06-03** (Decision 10 below). Unlike the 2026-05-15 "TBD", this track is **earned by evidence**, not manufactured:

- The 2026-06-02 reconciliation in the prior doc records that exercising the vector path **over REST** (validating the `understand-graphdb` consumer) surfaced **two independent, pre-existing bugs that neither the planning doc nor the unit tests caught** — *because the existing vector tests assert only "N results with valid IDs," never nearest-neighbour correctness.* Dogfooding found what white-box tests structurally could not.
- Two real consumers exist and are mid-flight: **`understand-graphdb`** (code-understanding knowledge graph; Phase 1 ingest merged, Phases 2–3 planned) and **`coi-screen`** (COI screening; MVP built, not yet run on the real ICIJ corpus — "Milestone-1-proper").
- The pattern that keeps paying off this repo (audit → measured backlog) has a consumer-driven analogue: **drive the real consumers against `main`, treat every divergence as a graphdb bug + a missing regression assertion.**

### Initial item breakdown (refine via a short spike if scope is unclear)

- **Q1 — Close the correctness-assertion gap in the vector test surface. ✅ DONE (#283).** The unit tests assert result *count*, not *ranking*. Add nearest-neighbour / recall assertions (known-answer datasets) to `pkg/vector` and the storage/REST vector paths, so the class of bug the REST exercise found becomes a unit-level regression guard. *Highest leverage — it's the root cause the audit reconciliation named.* **Shipped:** `pkg/api` `TestVectorSearch_NearestNeighbourCorrectness` (REST identity assertions) + `pkg/storage` `TestVectorSearchForTenant_KnownAnswerOrdering` (k>1 identity + ordering), using well-separated planted clusters for a deterministic known answer.
- **Q2 — Drive `understand-graphdb` against current `main` end-to-end. ✅ DONE (#286 + consumer validation).** Drove the consumer (REST) against `main` via its own harness + a local deterministic embeddings server. **Shipped:** `pkg/api` `TestVectorSearch_RESTFloatArrayIngestionRoundTrip` — #246's float-array→vector coercion was pinned only at the storage layer, not the REST surface it was written for (the consumer's actual path); neuter-and-fail verified. Neural path validated end-to-end (ingest 121n/315e → correct NN top hit over REST); default FTS/LSA path assertion-grade green (consumer's `GRAPHDB_INTEGRATION=1` suite, 103 tests). Consumer-side: stale "neural blocked" docs corrected. *Remaining boundary: LSA semantic-dimensions need a real-LLM-summary run.*
- **Q3 — Run `coi-screen` Milestone-1-proper against `main`. ✅ DONE (#287 + #288; synthetic-corpus proof).** coi-screen consumes graphdb as an embedded library; driving it surfaced **two pre-existing storage *persistence* bugs**, both fixed + pinned: **(#287)** `Snapshot()` clears the plain adjacency maps after compaction and never serializes the compressed adjacency → ALL edge adjacency lost on reopen under the default `EnableEdgeCompression` (independently confirmed by the Stór consumer); **(#288)** the batch/bulk-import path (`import-icij`) never stamped `TenantID`/maintained the per-tenant indexes → bulk data invisible to every `*ForTenant` reader. End-to-end proof on a synthetic 50K-node ICIJ-shaped corpus: import → screen → flagged the planted 2-hop conflict in <1s (pre-fix: zero). *Real ~814K corpus run still pending (corpus absent locally) — synthetic was sufficient for the bugs.*
- **Q4 — Generalize: a consumer-contract regression harness. ⬜ REMAINING (only open Track-Q item).** Turn the recurring "consumer surfaced a bug" loop into standing contract tests in graphdb so future consumer breakage is caught in CI, not in the field. *Seed set = the four pins this session: #283 (NN identity), #286 (REST float-array ingestion), #287 (adjacency-survives-reopen), #288 (bulk-import tenant visibility).* Sequenced last — it generalizes what Q1–Q3 learned.

**New gap surfaced (Q3, not yet a task):** the batch executor's **delete/update** paths (`executeDeleteNode`/`executeUpdateNode`) have the same per-tenant-index omission #288 fixed for create, but are unexercised by any consumer — documented follow-up, fix when a consumer needs batch delete/update.

**Acceptance**: each consumer-surfaced divergence is (a) fixed in graphdb and (b) pinned by a graphdb-side test that fails against the pre-fix code. Q1–Q3 met this (#283/#286/#287/#288). The track closes when Q4's harness generalizes these into standing CI contracts.

**Q1 ✅ → Q2 ✅ → Q3 ✅ → Q4 (remaining).** Q1 was the root-cause assertion gap; Q2/Q3 drove the two live consumers and fixed every divergence in graphdb; Q4 generalizes. Track Q may also be closed at Q3 if the standing harness isn't wanted yet.

### Reconciliation 2026-06-03 — Transaction durability shipped (code-vs-doc discrepancy)

Before Track Q started, a code read surfaced that this doc's "all write paths are group-commit-converted" framing didn't extend to the `Transaction` API: `Transaction.Commit` applied buffered changes straight to the shard maps and **bypassed WAL durability, tenant indexes, vector/property indexes, stats, and observers** — committed transactions were neither crash-durable nor visible to any `*ForTenant` read. (Per CLAUDE.md "trust the code, surface the discrepancy"; `Transaction` had zero non-test callers — the durable production bulk path is `Batch` — so it was dormant, not a live regression.)

Completed as a real, durable, tenant-aware Go primitive (brainstorm-approved spec `docs/superpowers/specs/2026-06-03-transaction-durability-design.md`):

- **#277** spec; **#278** extract shared `persistNodeLocked`/`persistEdgeLocked` (single source of truth for "persist a node/edge"); **#279** `wal.AppendBatchAtomic` + `gs.appendWALBatch` (single-fsync all-or-none batch durability on both WAL modes); **#280** rewrite `Transaction.Commit` to route every buffered op through the persist helpers + one atomic batch fsync, add `BeginTransactionForTenant`, validate references all-or-none, apply vectors + dispatch observers off-lock.
- Scope: creates + updates, last-writer-wins, internal Go API (no client surface). **Deferred:** transaction deletes (`tx.DeleteNode`/cascade), conflict detection, and a client-facing transaction API — all documented in the spec § Out of scope.

This was a side-quest off the Track Q critical path; **Track Q remains selected but not yet started.**

---

## Decision points

### Decision 10 (NEW) — Critical-path selection after Track P

Track P is the second audit-driven track to complete (Track R via the 2026-05-06 audits; Track P via Decision 9's 2026-06-02 audit). The candidate angles considered:

- **(A) Consumer-driven correctness hardening** — **✅ SELECTED 2026-06-03.** Evidence-rich (dogfooding already found 2 bugs unit tests missed), needs no new audit ceremony, two live consumers ready to drive. Becomes **Track Q** above.
- **(B) Commission a security audit** — the least-recently-audited dimension (last 2026-05-06); vector/embedding side-channels + the auth/tenant surface. Deferred: still a strong *future* move, but (A) has standing evidence now and (B) would re-incur audit ceremony before any fix ships.
- **(C) Productization / operability** — onboarding/quickstart docs, single-node limitation framing, the deployment-ordering note (create indexes before traffic). Deferred to § Off-path; ships adoption value but isn't correctness-urgent.
- **(D) Finish the Track P tail (M3/M7)** — deferred to § Off-path; gated on the snapshot-format + API-deprecation decisions, not "proceed" work.

**Don't re-open (A)'s evidence as license to manufacture sub-tracks** — Q1–Q4 are bounded by what the consumers actually surface; let the divergences drive scope, not speculation.

### Carry-forward decisions still open

- **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` is synchronous; streaming is a future enhancement, not a launch question. Still open from 2026-05-14.
- **Batched-WAL default** — now that group commit works (Track P item 1, all paths), should `EnableBatching` default to true? Needs a FlushInterval latency-vs-throughput sweep first (unstarted). Latency-vs-throughput call.

---

## Off-path queue (deferred, with decisions teed up)

### Track P tail — M3 + M7 (decision-laden)

- **M7 first**: drop the global `nodesByLabel`/`edgesByType` mirror. NOT a dead-code delete — live tenant-blind readers (`FindNodesByLabel`/`FindEdgesByType` at `query_operations.go:145,215`; `node_adjacency.go:57`) + snapshot-persisted. **Decision needed:** is the tenant-blind `Find*` API still wanted? If not, migrate its callers to per-tenant indexes, then drop the mirror.
- **M3 after M7**: label-index O(K) removal → O(N²) bulk delete. The set fix needs the global mirror's persisted type changed → snapshot **format version bump**; doing M7 first removes the persisted global index entirely, making M3 a free per-tenant change. **Decision needed:** format bump vs sorted-slice (the latter doesn't fix the asymptote). See memory `project_track_p_m3_m7_deferred`.

### Carried follow-ups

- **Resolver-level index-level pagination** (Track P rec #2's deferred half): GraphQL edge resolver (`pagination_resolvers.go:123`) + REST `listNodes` (`handlers_nodes.go:80`) still materialize the full per-tenant slice then page in memory. Lower-leverage (scales with the tenant's own data, not total DB); resolver contract change (cursors are integer offsets).
- **Batched-WAL default sweep** — see Decision carry-forward above.
- **Productization / operability** (Decision 10 option C): onboarding docs, single-node framing, deployment-ordering note.
- **Update-driven auto-embedding** (R2.5a TODO) — gated on a ctx-passing-storage-methods decision; out of scope, carried from 2026-05-15.

### Inherited PRs — #240 / #241 (forcing function, now ~10 days)

`#240` (property-index lifecycle + general unique_property) and `#241` (node-label mutation over HTTP) have been carried open since 2026-05-24 across multiple sessions, untouched. The disposition is still unresolved: **adopt** (rebase, review, land) or **close** (declare superseded). This perennial carry-forward should be resolved at the start of Track Q (it's cheap and clears the open-PR list to just Track Q's work).

---

## Out of scope (carry-forward + new)

- **M3/M7 without their decisions** — do not implement either on a generic "proceed"; both need the format / API call (above).
- **Snapshot on-disk format changes without a version bump** — the snapshot file is customer-data-equivalent (CLAUDE.md § Snapshot format stability). M3's set fix is the live example.
- **New perf tracks** — the perf dimension has now had two audits (2026-05-06, 2026-06-02) and a fully-shipped backlog. Don't open a third perf track without a *new* measured bottleneck.
- **`coi-screen` redesign** — it's a BUILT MVP in a private repo; Track Q drives it as a consumer, it does not get re-architected here.

---

## How to use this document

1. **Track Q is done through Q3** — Q1 (#283), Q2 (#286), Q3 (#287 + #288). Only **Q4** (consumer-contract regression harness) remains, and it may be deferred if the standing harness isn't wanted yet. See the Track Q section above for the per-item outcomes.
2. **Resolve the inherited-PR disposition (#240/#241)** — cheap, clears the board (Decision: adopt or close); still open.
3. **If continuing Track Q**: Q4 generalizes the four pins (#283/#286/#287/#288) into standing CI contracts.
4. **Don't** re-open the perf dimension or manufacture sub-tracks beyond what the consumers surface.
5. If a consumer divergence turns out to be deep enough to need design, spike it (`/spike`) before implementing — but most will be bounded bugfixes.

The critical path is **earned, not TBD**: Track Q exists because dogfooding already proved the unit tests miss correctness bugs the consumers hit.
