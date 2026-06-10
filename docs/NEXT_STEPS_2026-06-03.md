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

**Deferred tail — ✅ NOW CLOSED (2026-06-03, PR #294, see § Off-path queue):** M3 (label-index O(N²) bulk delete) and M7 (the global `nodesByLabel`/`edgesByType` mirror) shipped — both reframed by the code: M3 needed **no** snapshot-format bump (rebuild-on-load dissolved the premise), and M7 was a **rename** for explicit cross-tenant scope (`*AcrossTenants`), not a mirror-drop. Memory `project_track_p_m3_m7_deferred` updated.

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
- **Q4 — Generalize: a consumer-contract regression harness. ✅ DONE (merged `63c6c38`, local squash-merge — no PR).** Shipped via brainstorm→spec→plan→subagent-driven execution: a new pin **CC5** (label-filtered vector search on the REST float-array path), greppable `// CONSUMER CONTRACT:` tags on the four existing pins (#283/#286/#287/#288 → CC1–CC4), `docs/CONSUMER_CONTRACTS.md` (catalogue + growth rule), and `scripts/consumer-drive.sh` + committed deterministic embedder/synthetic-corpus generator (the on-demand drill, key-free, ran green end-to-end against both consumers). Live-consumer CI is deferred future work — blocked because `understand-graphdb` has no remote and `coi-screen` is private; the drill is structured so promotion is "run it from CI," not a rewrite. Spec: `docs/superpowers/specs/2026-06-03-consumer-contract-regression-harness-design.md`.

**New gap surfaced (Q3) — ✅ FIXED 2026-06-03 (PR #298, CC6):** the batch executor's **delete** path had the per-tenant-index omission #288 fixed for create — `executeDeleteNode`/`executeDeleteEdge` updated the global indexes but never called `removeNodeFromTenantIndex`/`removeEdgeFromTenantIndex`, so per-tenant counts kept the deleted node/edge. (Originally framed as "delete/update"; corrected to **delete-only** — batch `UpdateNode` is properties-only and already consistent with non-batch update.) Fixed + pinned by CC6 contract tests (assert per-tenant *counts*, not list membership, and in-memory without reopen — both reads and reopen self-heal the gap). ~~Residual, lower-priority index-hygiene follow-up: the node-delete cascade still leaves stale global `edgesByType` buckets + the opposite endpoint's adjacency.~~ — ✅ **CLOSED (stale note, corrected 2026-06-06).** This was already fixed: the non-batch cascade (`node_adjacency.go` `cascadeDeleteOutgoingEdge`/`cascadeDeleteIncomingEdge`) removes from `edgesByType` and the opposite adjacency, and the batch cascade was fixed by **PR #304** (+ per-tenant edge index by **#307**); `checkGraphInvariants` asserts `edgesByType`/adjacency consistency. No residual.

**Acceptance**: each consumer-surfaced divergence is (a) fixed in graphdb and (b) pinned by a graphdb-side test that fails against the pre-fix code. Q1–Q3 met this (#283/#286/#287/#288). The track closes when Q4's harness generalizes these into standing CI contracts.

**Q1 ✅ → Q2 ✅ → Q3 ✅ → Q4 ✅ — TRACK Q CLOSED.** Q1 was the root-cause assertion gap; Q2/Q3 drove the two live consumers and fixed every divergence in graphdb (incl. two storage persistence bugs, #287/#288); Q4 generalized the pins into a standing consumer-contract harness. graphdb is now back at a "no earned critical path" junction — the next track is a planning-checkpoint decision (the deferred follow-ups below are candidates, none yet promoted).

### Reconciliation 2026-06-03 — Transaction durability shipped (code-vs-doc discrepancy)

Before Track Q started, a code read surfaced that this doc's "all write paths are group-commit-converted" framing didn't extend to the `Transaction` API: `Transaction.Commit` applied buffered changes straight to the shard maps and **bypassed WAL durability, tenant indexes, vector/property indexes, stats, and observers** — committed transactions were neither crash-durable nor visible to any `*ForTenant` read. (Per CLAUDE.md "trust the code, surface the discrepancy"; `Transaction` had zero non-test callers — the durable production bulk path is `Batch` — so it was dormant, not a live regression.)

Completed as a real, durable, tenant-aware Go primitive (brainstorm-approved spec `docs/superpowers/specs/2026-06-03-transaction-durability-design.md`):

- **#277** spec; **#278** extract shared `persistNodeLocked`/`persistEdgeLocked` (single source of truth for "persist a node/edge"); **#279** `wal.AppendBatchAtomic` + `gs.appendWALBatch` (single-fsync all-or-none batch durability on both WAL modes); **#280** rewrite `Transaction.Commit` to route every buffered op through the persist helpers + one atomic batch fsync, add `BeginTransactionForTenant`, validate references all-or-none, apply vectors + dispatch observers off-lock.
- Scope: creates + updates, last-writer-wins, internal Go API (no client surface). **Deferred:** transaction deletes (`tx.DeleteNode`/cascade), conflict detection, and a client-facing transaction API — all documented in the spec § Out of scope.

This was a side-quest off the Track Q critical path; **Track Q remains selected but not yet started.**

**Correction 2026-06-04 (cross-repo, stór-confirmed — IMPORTANT):** the "`Transaction` had zero non-test callers … dormant" claim above is **graphdb-internal only and WRONG at the ecosystem level.** The stór consumer's core writes — `Register` / `LinkRealisation` in `graphstore.go` — are **live `Transaction.Commit` callers** on the realisation hot path (primary source: `BeginTransaction → tx.CreateNode/tx.CreateEdge → tx.Commit`). **Do NOT deprecate, remove, or refactor the `Transaction` path on a "no callers" assumption** — it would break stór; any migration to `Batch` is a coordinated interface change to flag, not a unilateral cleanup. (stór's transactions are **create-only** — no `tx.UpdateNode` — which is why the `#316` property-index drift on *existing-node update* had no consumer impact. That's the reason, not "dormant.")

---

## State reconciliation 2026-06-04 — post-Track-Q hardening wave

Track Q closed at "no earned critical path." The session that followed did **not** open a new audit-driven track; it ran a tenant-isolation sweep, brought the batch write path to full parity, executed a silent-bug hunt on the delete/persistence paths, and — answering the user's "why so many silent bugs?" — built a parallel-invariant test harness. All merged to `main` (HEAD `e49d7f6`).

### Tenant-isolation sweep (F1–F3) ✅
`#300` gate `/api/metrics` admin-only (cross-tenant stats leak); `#301` scope `/api/v1/tenants/{id}` with `withTenant`; `#302` rename orphaned tenant-blind footguns to `*AcrossTenants`; `#303` audit doc (`AUDIT_tenant_isolation_2026-06-04.md`). Live posture confirmed strong, findings small. Memory `project_tenant_isolation_sweep_2026_06_04` — don't re-commission without new evidence.

### Batch-path parity ✅ (`#304`)
Batch create/update now index vectors; create/update/delete dispatch observers; delete cascade cleans the global edge index + opposite adjacency — mirrors `Transaction.Commit`.

### Delete-path silent-bug hunt ✅ (`#305`, `#307`, `#308`, `#309`)
A systematic hunt for "write path updates index A, forgets index B" found and fixed:
- **`#305` (CRITICAL)** — the HNSW vector index was lost on every restart; now rebuilt from the node set on load (defs persisted, graph derived; **no format bump**).
- **`#307`** — the shared cascade helpers skipped `removeEdgeFromTenantIndex`: a **live-path** tenant edge-count drift, self-healed on reopen so invisible.
- **`#308` (C/D/E)** — `replayDelete{Node,Edge}` skipped the tenant index (crash-recovery drift); `RemoveNodeProperties` left a stale vector; batch `executeDeleteNode` never removed the node's vectors (the gap `#304` missed).
- **`#309`** — capstone regression guard for the C + `#307` interaction (replay-delete of a node *with* edges).

The delete paths (live / replay / cascade / batch / remove-property) are now at full per-tenant + vector index parity.

### Improved testing — parallel-invariant harness ✅ (`#310`, `#311`, `#314`)
Root cause of the silent-bug streak named explicitly: **N parallel representations × M write paths**, with tests asserting only one projection (the global `GetNode`/`GetEdge` view, always correct). The harness (`pkg/storage/invariants_test.go`) derives ground truth from the authoritative shards and asserts every derived structure agrees; an 8-case teeth-test proves it *fires* on drift; a write-path × op matrix (`#311`) drives it across live / batch / transaction / WAL-replay. **Phase C — metamorphic cross-path equivalence (`#314`)** then closed the harness's one conscious blind spot: its vector check is *count-only*, so `#314` drives one op-script through all four paths and asserts observationally identical results — crucially identical `VectorSearchForTenant` top-k — catching a vector re-indexed under the wrong *value* but right *count*. Memory `feedback_parallel_invariant_coverage`.

### Surfaced follow-ups (candidates — none promoted to critical path yet)
- **Phase C — metamorphic equivalence test ✅ DONE 2026-06-04 (`#314`).** Same op-script through every path (live / batch / transaction / WAL-replay), asserting vector **search-result** equality (`VectorSearchForTenant` top-k) — closed the count-only limitation the `#310`/`#311` checker can't see. A non-vacuity teeth-test proves the op-script's vector update moves the ranking, so the comparison can't pass for the wrong reason; a teeth-mutation confirmed the search assertion fires while count checks stay green. Test-only (`pkg/storage/invariant_metamorphic_test.go`), no production change. Spec: `~/.claude/plans/we-need-improved-testing-bubbly-wave.md` § Phase C.
- **Extend the invariant checker to `propertyIndexes` ✅ DONE 2026-06-04 (`#316`).** The global property index is now asserted by `checkGraphInvariants` (exact membership, no empty buckets, no duplicates) + 4 teeth cases + a property-index matrix across live / batch / WAL-replay / transaction. **Found and fixed a real bug:** `Transaction.Commit`'s existing-node update re-indexed vectors but skipped `updatePropertyIndexes`, drifting the property index (the per-tenant-index/#288 class). **FTS reframed — NOT a storage-checker target:** the FTS index is API-layer (`pkg/search`, owned by `pkg/api`), admin-rebuilt (`POST /search/index`), non-persisted, and stale-by-design between rebuilds; `GraphStorage` has zero references to it. It has no must-agree-with-shards invariant, so it does not belong in `checkGraphInvariants` — any FTS test is a separate API-layer rebuild-postcondition test.
- **~~FTS index lost on restart?~~ — RESOLVED (by-design, not a bug).** Confirmed during `#316`: the FTS index is built only by the admin `POST /search/index` rebuild and is not persisted, so restart-loss is expected behaviour (rebuild to repopulate), not drift. The open question is only whether a deployment auto-rebuilds at API bootstrap — an ops/bootstrap concern, not a storage invariant.
- **~~LSA stale after WAL-replay?~~ — RESOLVED (by-design, not a storage bug) 2026-06-04.** Same family as the FTS resolution above. The LSA registry (`search.TenantLSAIndexes`) hangs off `pkg/api.Server`, constructed *after* storage init — there is no symmetric place for a `rebuildLSAIndexesFromNodes` analog without `pkg/storage` importing the search layer (a dependency inversion; the vector-index rebuild only works because `gs.vectorIndex` is a storage member). LSA is also documented non-incremental (`pkg/search/lsa.go`: "Not incremental … callers must rebuild") and built only by an explicit admin/bootstrap action, so it is **equally stale after any live write** — "after WAL-replay" introduces no new staleness and is a red herring. The only real residual is API-layer: should bootstrap warn/refuse when a loaded `.lsa` snapshot has diverged from storage? — a product decision, not a storage invariant. **The actionable half of the original bullet was a different, real bug:** `persistNodeLocked` left a node half-committed when a type-mismatched value hit a property index — **FIXED in `#321`** (skip type-mismatched inserts, matching the build/replay paths). **Update/delete-path follow-up ✅ DONE 2026-06-04 (`#324`):** the siblings (`updatePropertyIndexes` — live `UpdateNode` + `Transaction.Commit` + `replayUpdateNode` —, `removeNodeFromPropertyIndexes`, `RemoveNodeProperties`, and the batch executor's inline update + delete copies) shared the root cause via an un-gated `idx.Remove` (`Remove("not found")` on a never-indexed mismatched value → spurious error + partial apply). Gated **both** Remove and Insert on `value.Type == idx.indexType` at every site, completing the create/update/delete × {live, transaction, replay, batch} property-index matrix (create cell was `#321`). 5 teeth tests, each RED pre-fix via its own mechanism.
- **`CreateVectorIndex` not WAL-logged ✅ FIXED 2026-06-04 (`#320`).** A vector index definition created after the last snapshot was never WAL-logged (unlike `CreatePropertyIndex`), so it was lost on crash and its vectors went un-indexed on recovery; the drop path had the mirror resurrection bug. Fixed with `OpCreateVectorIndex`/`OpDropVectorIndex` + replay handlers (definition-only; population stays with the post-replay rebuild) + two crash-recovery teeth tests.
- **persist-HNSW escalation** — `#305` rebuilds on load (O(N log N)); serialize the graph only if startup cost bites at very large N (e.g. the 814K ICIJ corpus). Measured follow-up.

---

## Track S (NEW) — security audit + hardening (2026-06-10)

Commissioned per **Decision 10 option B** (the security dimension was last audited 2026-05-06; the surface had grown substantially since — admin `mint-token`, the Value⇄JSON converter, Cypher param substitution, edge mutation, pagination, tenant cascade delete, WAL/transaction durability, the Python SDK). Full-surface re-audit doc: **`docs/internals/design/AUDIT_security_2026-06-10.md`** (six parallel specialist passes + gosec/govulncheck). **Verdict: no live cross-tenant exposure** — the 2026-05-06 FAIL areas (multi-tenancy) are now the strongest part; the findings are hardening-defaults, data-remanence, and client-library polish. **11 High / 16 Medium / 10 Low**, organized into a 3-wave backlog.

### Wave 1 — server hardening ✅ CLOSED (2026-06-10)
All six server-side Highs + all five targeted Mediums shipped, each TDD-pinned (RED→GREEN):
- **#372** H-1 (suspended tenants kept access — `withTenant` now uses `GetActive`) + M-5 (tenant-ID override validation / log-injection)
- **#373** H-7 (HNSW `M`/`ef` caps) + H-8 (`/traverse` node cap + `X-Truncated`)
- **#374** H-9 (toolchain → go1.26.4; govulncheck 2 reachable → 0)
- **#375** H-2 (at-rest files 0600/0700 across wal/storage/search/lsm) + H-4 (WAL record-size cap → no 4 GiB restart-loop OOM)
- **#376** M-3 (GraphQL depth limit wired) + M-4 (pre-auth body cap) + M-16 (audit-log endpoint caps)
- **#377** M-6 (mint-token defaults to viewer)
- **#378** M-13 (SDK `CreatedAPIKey.key` `repr=False`)

### Wave 2 — client release ✅ SHIPPED (2026-06-10)
- **#379** Python SDK: H-10 (path-segment encoding — `quote_segment` at all 20 sites; the experimentally-confirmed `../admin` traversal), M-10 (cache namespace), M-11 (429 not retried on POST), M-12 (`trust_env=False`), L-10 (cache-error logging), L-9 (explicit `follow_redirects=False`)
- **#380** TS Workers client: H-11 (cache identity namespace), M-11 (idempotent-only retries), L-9 (`redirect: 'manual'`)
- **Follow-up:** cut a versioned Python/TS client release bundling #379/#380; decide the PyPI-publish open question.

### Wave 3 — partially shipped (2026-06-10); the executable items are done, the design-gated remainder is queued

**Shipped (no design decision needed):**
- **H-6** ✅ (#385) — `context.Context` threaded through betweenness/edge-betweenness/scc/triangles/node-similarity-all loops (interface change, Option A approved); handler maps deadline → 408.
- **H-5** ✅ (#387) — rate limiting *activated* (`InitRateLimiterFromEnv` was never called → both limiters nil → no protection, incl. auth brute-force) + general limiter on-by-default with `RATE_LIMIT_ENABLED=false` opt-out.
- **M-8** ✅ (#383) — user-management handlers route through the composite JWT+OIDC validator (OIDC admins can manage users) + OIDC `nbf` check.
- **M-2** ✅ (#384) — tenant-delete fails (500) instead of orphaning the deleted tenant's `.lsa` content file.

**With H-5, all 11 Highs in the audit are addressed.**

**M-7 ✅ shipped (#390)** — token revocation via a per-user generation counter embedded as a JWT `gen` claim; `requireAuth` rejects stale-generation tokens. Bumped on explicit revoke / password change / role change (the role-change bump also closes AUTH-7). Turned out to be the standard pattern with no real design fork, so it was implementable directly. (An explicit admin revoke HTTP endpoint is a small follow-up — the `RevokeUserTokens` method is in place.)

**M-1 ⏳ spiked, awaiting decision (#391)** — design doc `DESIGN_m1_wal_remanence_2026-06-10.md`. The naive snapshot+truncate loses concurrent writes; the proper fix is **Option A: a `TruncateUpTo(lsn)` checkpoint** (capture boundary LSN under the snapshot RLock, truncate only ≤ N) across all 3 WAL backends, with **Option C** (document the window + defer to Close) as an interim. **Decision requested: A (durability-layer change) or C (interim).** Gates H-3 and pairs with L-1/L-4.

**Remaining — each needs a decision/spike/cross-repo (NOT started):**
- **H-3** encrypt WAL payloads — **entangled with M-1 + M-14** (enabling encryption mid-life orphans old plaintext WAL entries → needs clean-WAL-on-toggle (M-1) + an entry marker (M-14)). Sequence after the M-1 decision.
- **M-14** snapshot magic-header + version — snapshot-format version-bump discipline (decision).
- **M-15** enterprise `.so` plugin hash/signature verification — cross-repo with graphdb-enterprise (coordination).

**Also ready (pending your call):** cut a versioned **client release** bundling #379/#380 (semver judgment — the TS retry change is arguably breaking at 1.x — + the PyPI-publish decision).

### L-tier (10 Lows) — ✅ DISPOSITIONED (2026-06-10)
**L-5, L-7 fixed** (#386 — Cypher sanitizer SQL false-positives; float→int corruption near MaxInt64); **L-9, L-10 fixed** in the client release (#379/#380); **L-1/L-2/L-3/L-4/L-6/L-8 accept-risk or deferred** with rationale recorded in `AUDIT_security_2026-06-10.md` § L-tier disposition (L-1/L-4 pair with the M-1 spike; L-6 is by-design for coord). Original batch-or-accept-risk note below is superseded.

### L-tier (original note — superseded by the disposition above)
Each is a one-liner or a documented accepted-risk (edge TOCTOU, adjacency timing channel, ID-gap inference, zombie tenant, Cypher SQL-pattern false positives, 409 node-ID disclosure, float→int near MaxInt64, usage-endpoint admin-check placement, SDK redirect docs, cache-POST-invalidation). Bundle into one cleanup PR or close as won't-fix with rationale.

**`AUDIT_security_2026-06-10.md` § "Consolidated confirmed-clean" is the starting point for any future security audit — don't re-derive it.** Memory: `project_security_audit_2026_06_10`.

---

## Decision points

### Decision 10 (NEW) — Critical-path selection after Track P

Track P is the second audit-driven track to complete (Track R via the 2026-05-06 audits; Track P via Decision 9's 2026-06-02 audit). The candidate angles considered:

- **(A) Consumer-driven correctness hardening** — **✅ SELECTED 2026-06-03.** Evidence-rich (dogfooding already found 2 bugs unit tests missed), needs no new audit ceremony, two live consumers ready to drive. Becomes **Track Q** above.
- **(B) Commission a security audit** — the least-recently-audited dimension (last 2026-05-06); vector/embedding side-channels + the auth/tenant surface. Deferred: still a strong *future* move, but (A) has standing evidence now and (B) would re-incur audit ceremony before any fix ships.
- **(C) Productization / operability** — onboarding/quickstart docs, single-node limitation framing, the deployment-ordering note (create indexes before traffic). Deferred to § Off-path; ships adoption value but isn't correctness-urgent.
- **(D) Finish the Track P tail (M3/M7)** — ✅ DONE 2026-06-03 (PR #294); see § Off-path queue. The gating "decisions" dissolved on closer reading: M3 needed no format bump, and M7 was a rename, not a mirror-drop.

**Don't re-open (A)'s evidence as license to manufacture sub-tracks** — Q1–Q4 are bounded by what the consumers actually surface; let the divergences drive scope, not speculation.

### Carry-forward decisions still open

- **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` is synchronous; streaming is a future enhancement, not a launch question. Still open from 2026-05-14.
- **Batched-WAL default** — now that group commit works (Track P item 1, all paths), should `EnableBatching` default to true? Needs a FlushInterval latency-vs-throughput sweep first (unstarted). Latency-vs-throughput call.

---

## Off-path queue (deferred, with decisions teed up)

### Track P tail — M3 + M7 — ✅ DONE 2026-06-03 (PR #294, in review)

Both shipped on branch `perf/label-index-set-m3` (PR #294). The "decision-laden" framing below was resolved by trusting the code over the audit — both fixes turned out smaller *and* safer than the audit implied:

- **M3 — label-index O(K)→O(1) removal. ✅** Global + per-tenant label/type indexes are now a set (`map[string]map[uint64]struct{}`); bulk delete's label-index cost goes O(N²)→O(N). **No snapshot format bump** — the audit's blocking premise was false: the global mirror is the union of the per-tenant indexes, already rebuilt-on-load from the flat node set (the edge-adjacency idiom), so its in-memory type is free to change while the on-disk JSON shape stays identical. Behavior preserved (sticky labels, deterministic sorted reads); benchmark-backed (`BenchmarkLabelIndexRemoval`: flat ~35 ns vs O(K) slice).
- **M7 — `Find*` → `*AcrossTenants` rename, NOT mirror-drop. ✅** The audit's "drop the dead mirror" was wrong: the mirror serves genuinely cross-tenant callers (constraint validation, schema sampling, full-text index, query cardinality). The real defect was only the misleading tenant-blind *name*; the fix renames to the audit-A3b `*AcrossTenants` convention (cf. `GetAllNodesAcrossTenants`) and keeps the mirror. Enterprise repo confirmed 0 references.
- **Surfaced follow-up — PR #295 (stacked draft):** the M7 rename made visible a pre-existing **latent** cross-tenant leak in GraphQL aggregate-schema generation (property-key *names* sampled cross-tenant). Not live-exploitable — the production `limits.go` schema path uses static node types and never sampled; the aggregation generator is test-only — but fixed as hardening (per-tenant sampler + regression test). See memory `project_track_p_m3_m7_deferred`.

**Track P tail is complete; Track P is fully closed end-to-end.**

### Carried follow-ups

- **Resolver-level index-level pagination** (Track P rec #2's deferred half) — ✅ **REST DONE 2026-06-06 (PR #366); GraphQL deferred.** REST `listNodes`/`listEdges` now seek to the cursor in the sorted ID set and clone only the page (~`limit`) instead of cloning the whole per-tenant set — new `pkg/storage` `*PageForTenant` methods (`NodesPage`/`NodesByLabelPage`/`EdgesPage`/`EdgesByTypePage`) sharing a generic `pageFromSortedIDs[T]`. ~98× fewer allocs/page at 10K nodes (307 vs 30,012 at limit=100). **No contract change** — REST cursor was already ID-based, so CC8 / the Python SDK `nodes.list` walk is unaffected (existing cursor tests pass unmodified). `?from=`/`?to=` adjacency edges keep the materialize+paginate path (small + composition post-filter). **Still deferred:** the GraphQL resolvers (`pagination_resolvers.go`) clone the full set too but use opaque *offset* cursors — going index-level there is the resolver-contract change (offset→ID cursor migration + backward `last`/`before` seek). `GetAll*ForTenant` left untouched (GraphQL + exports use it).
- **Batched-WAL default sweep** — see Decision carry-forward above.
- **Productization — Python SDK ✅ ROADMAP COMPLETE M1→M4 (2026-06-04→06-06).** First-party `clients/python/` client (sync + async, `httpx`-only core, uv-managed; core facade anchored to consumer contracts CC1–CC9). All milestones shipped: **M1** core (#326/#327), **M2a** search/query (#358), **M2b** admin (#359), **M3** async `AsyncGraphDBClient` (#360), **M4a** retry/backoff (#361), **M4b** pluggable response caching (#362), **M4c** LangChain adapters behind a `[langchain]` extra (#363). Suite 168 passed / 2 skipped. Specs + plans: `docs/superpowers/{specs,plans}/2026-06-0{4,5,6}-python-sdk-*.md`; memory `project_python_sdk`. Further SDK work is consumer-driven, not roadmap. Other operability (onboarding docs, single-node framing, deployment-ordering note) still open.
- **Update-driven auto-embedding** (R2.5a TODO) — gated on a ctx-passing-storage-methods decision; out of scope, carried from 2026-05-15.

### Inherited PRs — #240 / #241 — ✅ RESOLVED 2026-06-04 (CLOSED)

`#240` (property-index lifecycle + general unique_property) and `#241` (node-label mutation over HTTP) were **closed** — verified not on `main`, no consumer need (disposition resolved per the 2026-06-04 03:04 handoff). The open-PR list is clear of them. The local branches (`feat/expose-property-indexes-and-uniqueness`, `feat/expose-label-mutation`) are stale cleanup candidates (`branch-cleanup`).

---

## Out of scope (carry-forward + new)

- ~~**M3/M7 without their decisions**~~ — RESOLVED 2026-06-03 (PR #294): the decisions were taken deliberately — M3 shipped with no format bump, M7 as a rename. This guard no longer applies.
- **Snapshot on-disk format changes without a version bump** — the snapshot file is customer-data-equivalent (CLAUDE.md § Snapshot format stability). (M3 was expected to be the live example but shipped *without* a format change — it kept the on-disk shape and rebuilds the label index from the flat node set on load.)
- **New perf tracks** — the perf dimension has now had two audits (2026-05-06, 2026-06-02) and a fully-shipped backlog. Don't open a third perf track without a *new* measured bottleneck.
- **`coi-screen` redesign** — it's a BUILT MVP in a private repo; Track Q drives it as a consumer, it does not get re-architected here.

---

## How to use this document

1. **Read § State reconciliation 2026-06-04 first** — Track Q is CLOSED, and the hardening wave after it (tenant sweep, batch parity, the delete-path silent-bug hunt, the parallel-invariant test harness, Phase C metamorphic equivalence, and the propertyIndexes checker `#316`) is the current state. `main` HEAD `cf51aff` (`#316`). **Released as `v0.4.0` (2026-06-04)** from this HEAD — consumers can drop `replace => ../graphdb` and pin the tag.
2. **No critical path is forced.** Phase C (`#314`) and the propertyIndexes checker (`#316`) are done; FTS is reframed out of the storage checker (API-layer, by-design). The earlier *earned* testing candidates are now closed: `CreateVectorIndex`-not-WAL-logged (`#320`), and the LSA-stale-after-replay bullet (dispositioned by-design + its actionable partial-apply half fixed in `#321`) — see the surfaced-follow-ups list above. The property-index partial-apply matrix is fully closed (create `#321`, update/delete `#324`). **Productization wave since 2026-06-04:** Python SDK **M1 shipped** (`#326`/`#327`); edge update/delete `PUT`/`DELETE /edges/{id}` (`#330`/`#332`, incl. an `OpUpdateEdge` replay-gap fix) + non-finite edge-weight rejection (`#328`); the Go **module was renamed** `cluso-graphdb` → `github.com/dd0wney/graphdb` (`#335`) and **`v0.4.1` cut** (v0.4.0's go.mod carried the old path — Go consumers pin v0.4.1 + update imports). Both API-correctness issues are now closed: **#329** OpenAPI `uint64`→`int64` (`#341` — 18 integer-ID schemas switched to the standard `format: int64`) and **#331** `/traverse` now honors `direction` (`outgoing`/`incoming`/`both`, invalid→400) + `edge_types` (`#342` — previously decoded and silently ignored). **Integrator-bug wave (2026-06-05):** **#224** property values now serialize as proper JSON, not Go `%v` (`null`→`null`, `{}`→`{}`, nested/arrays preserved) via a new `storage.TypeJSON` + the shared `ValueFromJSON`⇄`ValueToJSON` converter pair the REST and GraphQL paths now both use (`#344` storage/REST, `#345` GraphQL — 5 divergent serializers unified); **#237** Cypher `CREATE`/`MERGE` now substitutes `$params` instead of storing `"&{name}"` (`#347` — handler wired to `ExecuteWithParamsContext` + `convertCreateProperty` fails loud on unresolved refs); **#226** `graphdb-admin` gained `login` + `mint-token` (`#348`); **#248** HNSW construction cost reframed as data-dependent with a realistic clustered benchmark (`#349`). **CI gap found + fixed (`#346`):** the `make test` allowlist excluded `pkg/api`/`pkg/graphql`, so CI never tested the REST/GraphQL surfaces — that's how `#344` briefly merged with a red `pkg/api` test under green CI; both packages are now in the test job. **Still-open CI hygiene** (handoff §): `cmd/...` packages remain outside the CI test allowlist (so `#348`'s tests run locally only), and the repo's `golangci-lint` config does not flag `gofmt` violations (caught `#348` only at CI's `go fmt` step). **Productization wave (2026-06-06):** the Python SDK roadmap **completed M1→M4** — M2a/M2b facades (`#358`/`#359`), M3 async `AsyncGraphDBClient` (`#360`), M4a retry/backoff (`#361`), M4b pluggable caching (`#362`), M4c LangChain adapters (`#363`). The SDK is feature-complete (suite 168/2-skip); further work is consumer-driven. No critical path is forced.
3. **Inherited PRs #240/#241 are CLOSED** — board is clear (no longer a forcing function).
4. **Don't** re-open the perf dimension or manufacture sub-tracks beyond what the consumers / invariant harness surface.
5. If a follow-up turns out deep enough to need design, spike it (`/spike`) first — but most are bounded.
6. **Current state (2026-06-10): Track S (security audit) is the active track — Waves 1 + 2 done, Wave 3's executable items done.** Read § Track S. The audit (`AUDIT_security_2026-06-10.md`) found no live cross-tenant exposure. Waves 1 (server, #372–378) + 2 (clients, #379/#380) shipped; Wave 3's no-decision items shipped (H-6 #385, H-5 #387, M-8 #383, M-2 #384, **M-7 #390** — **all 11 Highs addressed**); the L-tier is dispositioned (L-5/L-7 #386, rest accept-risk). **M-1 is spiked and awaiting an A/C decision (#391 design doc).** **Remaining: the M-1 decision, then H-3 (gated on M-1), M-14 (format bump), M-15 (cross-repo) + cut a versioned client release.** Don't re-commission a security audit without new evidence.

The critical path is **earned, not TBD**: the testing harness exists because this session proved the silent bugs are a *parallel-invariant coverage* gap, not an edge-case gap; Track S then closed the long-deferred security dimension with measured findings.
