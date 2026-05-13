# Plan: Next Steps (graphdb) — 2026-05-15

**Predecessor**: [`NEXT_STEPS_2026-05-14.md`](./NEXT_STEPS_2026-05-14.md). This doc reconciles that plan against current `main` after the 2026-05-13/14 session arc that closed Track R (the F4 vector tenant redesign + S11 auto-embedder redesign + S1 interface re-closure).

**Why a fresh doc**: the 2026-05-14 doc identified Track R as the critical path and named R3 as its closer. R3 merged. Every R-track sub-PR shipped (8 merged, 1 in review — see § State reconciliation). The 2026-05-14 doc explicitly named "after Track R closes, next checkpoint should write a fresh `NEXT_STEPS_<DATE>.md` reflecting the empirically-discovered next problem" — this doc is that checkpoint.

**Outstanding open PR at write time**: R2.5b (#193) is the last Track R wiring sub-PR and is still in review. The state-reconciliation below treats Track R as 8/9 merged with one PR closing the gap. When #193 merges, the only change to this doc is striking out the "in review" qualifier on R2.5b — every other claim holds independently.

---

## State reconciliation

### Track R — Redesign work ✅ **CLOSED (modulo R2.5b in review)**

The 2026-05-14 doc's three sub-tracks all shipped:

| Sub-track | PRs | Status |
|---|---|---|
| **R1.x — F4 vector tenant redesign** | R1.1 #184 + R1.2 #185 | ✅ merged |
| **R2.x — S11 auto-embedder + NodeObserver** | R2.1 #186, R2.2 #187, R2.3 #188, R2.4 #189, R2.5a #190 | ✅ merged |
| **R2.5b — server_init.go env-driven wiring** | #193 | 🟡 open |
| **R3 — S1 interface re-closure** | #191 | ✅ merged |

**Decisions 2 + 3 (resolved 2026-05-14, tier-based)** are realized in code:
- **OSS** = per-tenant HNSW (R1) + pluggable Embedder interface + in-tree LSAEmbedder adapter (R2.x).
- **Enterprise plugin** = filtered-HNSW (R1 alternative) + bundled-model embedder (R2.x alternative); both implement the same `Storage` / `Embedder` interfaces R3 closed.

**What this gives the next session**: a complete OSS implementation of tenant-isolated vectors + opt-in auto-embedding, with the enterprise extension points formally surfaced via the `Storage` interface.

### Track H — Linux CI infra tax ✅ **CLOSED (PR #181 + #192 cleanup)**

PR #181 moved the matrix `test` job to macOS-only, closing the exit-143 SIGTERM pattern for that job. PR #192 retired the stale CLAUDE.md bullets that described the pre-#181 state. The non-matrix Linux jobs (`coverage`, `benchmarks`, `build`, `tagged-build-nng`) could theoretically hit the same pattern under heavy contention — re-investigate if they start failing.

### What's NOT yet verified in production (verification gap)

**Track R has shipped but never run in a real deployment.** The OSS implementation is correct per the unit + integration tests, but:

- The per-tenant HNSW memory footprint at realistic tenant counts has not been benchmarked. Decision 2's spike picked Option A (per-tenant HNSW) on the assumption of low-hundreds tenants × ~10k vectors × 768 dims (≈3.2 GB). **Reality check needed before the next architectural decision rests on this assumption.**
- The auto-embed observer's bounded-pool backpressure has not been exercised under sustained node-create load. The pool drops on full; the metric exists; nobody has yet observed it firing in production-shaped traffic.
- The `pkg/api/server_init.go` env-driven wiring (R2.5b once merged) has not been exercised in a deployment. The end-to-end test in R2.5b covers the bootstrap path, but a Docker / k8s deployment that exercises `GRAPHDB_AUTO_EMBED_ENABLED=true` in production-shaped traffic doesn't exist.

**This is anchored as the next session's first task** in § How to use this document.

---

## Critical path

**TBD.** No new spike-grounded track exists. The 2026-05-14 doc had Track R because F4 + S11 spikes had landed and demanded implementation; after R2.5b there's no equivalent.

The next session should pick from one of:

1. **Run the verification gap above.** A deployment + benchmarks closes Track R *empirically* (not just *structurally*). This is the highest-leverage choice — it can either validate the Option A bet (no further action needed) or surface a real constraint that the enterprise filtered-HNSW plugin would need to satisfy.

2. **Resolve the inherited-PR carry-forward debt.** Four sessions of "decide later" needs to end. See § Inherited PRs forcing function below.

3. **Commission a new audit.** The May 2026 audit synthesis docs (`docs/internals/design/AUDIT_*_2026-05-06.md`) drove Track R via F4/S11. Six months on, a fresh audit pass may surface the next priority. Three candidate angles:
   - Performance under realistic SaaS load (correlates with the verification gap above).
   - Security: vector / embedding side-channels not covered by the F4 spike's tenant-strict guard.
   - Productization: the README "Scalability & Limitations" section (#146) named single-node as a limitation; that has not changed. A "what's needed for multi-node" audit would re-open scope.

**Don't manufacture a "Track S" or "Track T" without one of these three.** A made-up track risks the next session working a critical path that wasn't earned by evidence.

---

## Inherited PRs — forcing function (FOUR sessions carry-forward)

The 11 inherited PRs (#108, #109, #110, #131, #134, #135, #136, #137, #138, #139, #140) have been open since 2026-05-10/11/12. The 2026-05-13 0805Z handoff proposed "merge if green" or "park indefinitely." Neither happened. The 2026-05-13 0826Z handoff repeated the proposal. Neither happened. The 2026-05-14 doc captured per-PR disposition recommendations. Neither happened.

**Continuing this loop a fifth time means the carry-forward is no longer load-bearing.** Two paths from here:

### Path 1 — Act on the disposition

Per `NEXT_STEPS_2026-05-14.md` § Inherited PRs disposition, the recommended actions:

- **Group A (H4 storage fixes: #108, #109, #110)** — rebase + merge if CI green. These are functional fixes, not feature work.
- **Group B (docs: #131)** — merge if CI green.
- **Group C (A8.1 step-4 cleanup: #134, #138, #139, #140)** — rebase + merge if CI green.
- **Group D (LSA stack: #135 → #136 → #137)** — use the stacked-merge recipe in `NEXT_STEPS_2026-05-14.md`. Retag #136 (`A2`) and #137 (`C1`) before merge to avoid the Track A/C semantic collision.

If acted on, this is ~30-60 min of work. The 2026-05-14 doc has the full recipe.

### Path 2 — Remove from tracking

If they're not being acted on, they're not load-bearing. **The next planning doc (after this one) should remove them from the carry-forward set entirely.** Either:

- Close all 11 with a comment ("parked indefinitely; reopen if relevant"), OR
- Add a label like `triage:parked` and stop listing them in planning docs.

Either way, the carry-forward debt actually retires. The forcing function: **if not merged or explicitly closed by 2026-05-22 (one week from this doc), they get bulk-closed at the next planning checkpoint.**

This deadline is the new artifact this doc adds. The next session reads "if 2026-05-22 has passed and the 11 are still open, close them all with `gh pr close --comment 'parked indefinitely per NEXT_STEPS_2026-05-15.md forcing function'`" and acts.

---

## Off-path queue

### Track C tail — split into individual items

The 2026-05-14 doc aggregated four deferred items as "C-track tail." After ~10 sessions they're still all aggregated. Splitting:

1. **Planner-level CALL test** — `q.Call` → `CallOperator` emission. Single-test PR. ~30 lines. Useful when next agent touches planner. Acceptance: one table-driven test in `pkg/query/planner_test.go` (or similar).
2. **CallOperator unit tests** — exercise `CallOperator.Next()` directly with a stub procedure registry. Single-file PR. ~80 lines. Acceptance: 4-6 sub-tests covering arg dispatch, registry miss, YIELD bind shape.
3. **CallOperator integration test** — planner → CallOperator → registry → result with a real query. ~60 lines. Useful as a regression pin for future CALL-clause work. Acceptance: parses `CALL algo.shortestPath(...) YIELD path`, plans, executes, returns expected path.
4. **`pkg/algorithms` uniform widening** — the rest of the algorithm files (centrality, pagerank, triangles, scc, topology, cycle_detection, link_prediction, node_similarity, khop, community_*) take `*storage.GraphStorage`. Widen to `storage.Storage`. ~30 signature changes; mechanical. Acceptance: same Decision-6=B pattern as `shortest_path.go` (PR #178). Worth doing only when the next algorithm gets exposed as a procedure.

**(1)–(3) are each individually trackable. (4) is opportunistic — pick up when triggered by a procedure-exposing change.** All four are independent and can be picked up in any order or by different agents.

### OAuth account-rename CLAUDE.md bullet

Still flagged as "only if it bites again" from the 2026-05-13 0826Z handoff. Hasn't bitten since. Stays deferred.

### Update-driven auto-embedding (deferred per R2.5a TODO)

R2.5a's `OnNodeUpdated` is a no-op with a TODO: activating it requires a re-entry guard (spike §7.2) — either ctx-passing storage methods (a separate track) or sentinel property keys (leaks internal state). Until that's resolved, users who mutate a node's source text must delete+recreate (or call `/v1/embeddings` manually) to refresh the vector. **Out of scope for this doc; gates on a ctx-passing-storage-methods decision.**

---

## Decision points

### Decision 9 (NEW) — Critical-path selection for the next session

Choose one:
- **(A) Verification gap closure** — bench + deployment exercise of Track R.
- **(B) Inherited-PR triage** — execute the disposition (or bulk-close per the forcing function).
- **(C) New audit** — performance, security, or productization angle (see § Critical path option 3).

**Default if no answer**: (A) verification gap. Reason: it directly tests whether the Track R bet (Option A per-tenant HNSW) holds at realistic scale. If the answer is "no, memory is prohibitive at 1000 tenants," that surfaces enterprise-plugin work as the next track. If the answer is "yes, fits," Track R is empirically closed and (B) or (C) becomes the natural next.

### Carry-forward decisions still open

- **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` is synchronous. SSE/WebSocket streaming is a future enhancement; not a launch question. Still open from 2026-05-14.

---

## Risks specific to this window

- **The verification gap is silent until exercised.** Track R is unit-tested + integration-tested but never run in deployment. The risk of NOT running the verification is that an enterprise customer hits a real constraint and the OSS-tier decision (Option A) gets re-litigated under pressure. The risk of running it is one session of bench + Docker work that may surface "Option A is fine" (no further action) — that's the better failure mode.

- **The inherited-PR carry-forward debt is now load-bearing.** Four sessions of inaction means there's no consensus on whether these PRs matter. The forcing function above retires the debt one way or another. If neither happens by 2026-05-22, this planning rhythm starts losing credibility — future "merge or close by X" deadlines won't bind either.

- **No new critical path is a feature, not a bug.** The 2026-05-14 doc had Track R because two spikes demanded it. Track R is now done. "TBD critical path" is the honest state. **The risk is the next session manufactures a track to fill the gap rather than picking from the three honest options above.** Re-read § Critical path before declaring a new track exists.

---

## Out of scope (carry-forward + new)

Unchanged from 2026-05-14 except where noted:

- **GQL / non-Cypher query languages** — defer.
- **Geospatial / temporal data-model features** — still deferred.
- **Performance tracks B2/B3/B4** — opportunistic only. **(Subsumed if (A) verification-gap closure surfaces perf as the next track.)**
- **Code-quality May-10-lettering tracks** — opportunistic.
- **Mobile / `gomobile` / `pkg/mobile`** — Syntopica-v2 ruled out; unchanged.
- **S6 GNN as native kernel** — defer unless customer-driven.
- **S10b multi-statement ACID transactions** — Subset 🔴; deferred indefinitely.
- **`-tags zmq` replication variant** — deleted (PR #65). Stays deleted.
- **Bundled ONNX-runtime embedding model** — enterprise-plugin scope per Decision 3.

---

## How to use this document

This is a planning checkpoint, not a backlog. When picking up the next PR:

1. **Confirm R2.5b (#193) merged.** If not, merge it first — the only outstanding Track R PR.
2. **Pick a critical-path option from § Decision 9.** Default is (A) verification gap. If you pick (B) or (C), document why in the PR description.
3. **Address the inherited-PR forcing function** if 2026-05-22 has passed. Either merge per the disposition recipe in `NEXT_STEPS_2026-05-14.md`, or `gh pr close --comment "parked indefinitely per NEXT_STEPS_2026-05-15.md forcing function"` on all 11.
4. **After 1-3 PRs land**, this checkpoint should be revisited. Trigger: any of the three critical-path options being picked and at least one PR landed against it.

**Revisit triggers** (any one is sufficient to start a new checkpoint immediately):
- **Verification gap exercise surfaces a real constraint** — e.g., per-tenant HNSW memory blows up at 500 tenants. That changes the OSS vs enterprise architectural assumption and warrants its own track.
- **A customer-driven priority lands on the queue** — re-plan in the customer's terms.
- **Inherited-PR forcing function deadline passes (2026-05-22)** — the next checkpoint must record the outcome (bulk-merged, bulk-closed, or one-by-one acted on) and stop carrying them forward.

---

## Appendix — what the next agent does differently because of this doc

The previous planning docs accumulated content over time. This doc tries to add only things that change the next session's behavior:

- **§ Inherited PRs forcing function**: the deadline of 2026-05-22 is the new artifact. Without it, the next session reads "carry forward" and inherits the same indecision.
- **§ Verification gap as the default critical path**: the doc nominates an action rather than re-asking. Default-(A) lets the next session start without re-deciding.
- **§ Track C tail split into individual items**: trackable separately; each has a clear acceptance criterion so the next session can pick one off without designing scope.
- **§ Risks "No new critical path is a feature, not a bug"**: explicit caution against the manufacture-a-track failure mode.

Everything else in this doc is reconciliation of merged state — read it once, internalize, don't re-read on each PR.
