# Plan: Next Steps (graphdb) — 2026-05-15

**Predecessor**: [`NEXT_STEPS_2026-05-14.md`](./NEXT_STEPS_2026-05-14.md). This doc reconciles that plan against current `main` after the 2026-05-13/14 session arc that closed Track R (the F4 vector tenant redesign + S11 auto-embedder redesign + S1 interface re-closure).

**Why a fresh doc**: the 2026-05-14 doc identified Track R as the critical path and named R3 as its closer. R3 merged. Every R-track sub-PR shipped (8 merged, 1 in review — see § State reconciliation). The 2026-05-14 doc explicitly named "after Track R closes, next checkpoint should write a fresh `NEXT_STEPS_<DATE>.md` reflecting the empirically-discovered next problem" — this doc is that checkpoint.

**Outstanding open PR at write time**: R2.5b (#193) is the last Track R wiring sub-PR and is still in review. The state-reconciliation below treats Track R as 8/9 merged with one PR closing the gap. When #193 merges, the only change to this doc is striking out the "in review" qualifier on R2.5b — every other claim holds independently.

> **Reconciliation 2026-05-14**: #193 merged at 2026-05-13T13:36Z (`39247af`). Track R is now 9/9 merged with no qualifier. See updated row in § Track R below.

---

## State reconciliation

### Track R — Redesign work ✅ **CLOSED**

The 2026-05-14 doc's three sub-tracks all shipped:

| Sub-track | PRs | Status |
|---|---|---|
| **R1.x — F4 vector tenant redesign** | R1.1 #184 + R1.2 #185 | ✅ merged |
| **R2.x — S11 auto-embedder + NodeObserver** | R2.1 #186, R2.2 #187, R2.3 #188, R2.4 #189, R2.5a #190 | ✅ merged |
| **R2.5b — server_init.go env-driven wiring** | #193 | ✅ merged (`39247af`, 2026-05-13) |
| **R3 — S1 interface re-closure** | #191 | ✅ merged |

**Decisions 2 + 3 (resolved 2026-05-14, tier-based)** are realized in code:
- **OSS** = per-tenant HNSW (R1) + pluggable Embedder interface + in-tree LSAEmbedder adapter (R2.x).
- **Enterprise plugin** = filtered-HNSW (R1 alternative) + bundled-model embedder (R2.x alternative); both implement the same `Storage` / `Embedder` interfaces R3 closed.

**What this gives the next session**: a complete OSS implementation of tenant-isolated vectors + opt-in auto-embedding, with the enterprise extension points formally surfaced via the `Storage` interface.

### Track H — Linux CI infra tax ✅ **CLOSED (PR #181 + #192 cleanup)**

PR #181 moved the matrix `test` job to macOS-only, closing the exit-143 SIGTERM pattern for that job. PR #192 retired the stale CLAUDE.md bullets that described the pre-#181 state. The non-matrix Linux jobs (`coverage`, `benchmarks`, `build`, `tagged-build-nng`) could theoretically hit the same pattern under heavy contention — re-investigate if they start failing.

### What's NOT yet verified in production (verification gap)

**Track R has shipped but never run in a real deployment.** The OSS implementation is correct per the unit + integration tests, but:

- **(1a)** ~~The per-tenant HNSW memory footprint at realistic tenant counts has not been benchmarked. Decision 2's spike picked Option A (per-tenant HNSW) on the assumption of low-hundreds tenants × ~10k vectors × 768 dims (≈3.2 GB). **Reality check needed before the next architectural decision rests on this assumption.**~~ ✅ **Discharged 2026-05-14** via PRs #195, #209, #212 — see § Reconciliation 2026-05-14 below.
- **(1b)** ~~The auto-embed observer's bounded-pool backpressure has not been exercised under sustained node-create load. The pool drops on full; the metric exists; nobody has yet observed it firing in production-shaped traffic.~~ ✅ **Discharged 2026-05-14** via PRs #196, #202, #215 — see § Reconciliation 2026-05-14 — component (1b) discharged below.
- **(1c)** ~~The `pkg/api/server_init.go` env-driven wiring (R2.5b once merged) has not been exercised in a deployment. The end-to-end test in R2.5b covers the bootstrap path, but a Docker / k8s deployment that exercises `GRAPHDB_AUTO_EMBED_ENABLED=true` in production-shaped traffic doesn't exist.~~ ✅ **Discharged 2026-06-02** via a containerized exercise of the in-server LSA auto-embed path — see § Reconciliation 2026-06-02 — component (1c) discharged below.

**The Track R verification gap (1a + 1b + 1c) is now fully closed.** The next session picks a *new* critical-path option (default (C) — commission a new audit) per § How to use this document.

#### Reconciliation 2026-05-14 — component (1a) discharged

The per-tenant HNSW memory footprint at scale was the load-bearing question for Decision 2's Option A bet. **Per-tenant heap is flat across the planning doc's full named tenant range (100 → 1000).** Three PRs closed this:

- **PR #195** (`d2172ae`): per-tenant HNSW cost at the documented Option A scale (100 tenants × 10k vectors × 768 dims = 3.46 GB heap, +8% delta vs the 3.2 GB spike estimate).
- **PR #209** (`e718f87`): count-scaling extension — `count_scale_100/500` scenarios + `count_scale_linearity` subtest with 1.5× threshold. 100→500 ratio = 1.000 (six significant figures).
- **PR #212** (`2dde916`): 1000-tenant data point appended; reproduce-instruction `-timeout` advice corrected from 1800s to 3600s (the 1800s killed PR #209's session in trailing GC). 1000/100 ratio = 1.000.

Empirical per-tenant bytes: 3,463,428 → 3,463,209 → 3,463,237 across 100 → 500 → 1000 tenants. Reference doc: `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`. **Decision 2's Option A bet (per-tenant HNSW in OSS) holds empirically.** The enterprise filtered-HNSW plugin remains a premium-tier offering, not a correctness prerequisite.

#### Reconciliation 2026-05-14 — component (1b) discharged

The auto-embed observer's bounded-pool backpressure has now been exercised across all four surface combinations (Go × HTTP × burst × sustained × erroring × non-erroring). **S11 spike §7.5's drop-on-full design holds empirically.** Three PRs closed this:

- **PR #196** (`11bf734`): Go-direct burst load test — 400-node burst against a 2-worker / 10-queue pool with `slowFakeEmbedder`. Pins that the drop path fires + CreateNode latency stays bounded (~3.67ms) + no goroutine leak.
- **PR #202** (`2e22885`): O-1 structured error logging in the auto-embed worker — M-1 sanitized embedder-error log, source-property type-mismatch log, writeback log, panic-recovery log. Unit-level coverage of all four log sites.
- **PR #215** (`6dcef1c`): the three remaining angles — `TestAutoEmbedObserver_SustainedLoadDropsContinue` (steady-state drop accumulation), `TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad` (O-1 logs fire from pool-dispatched tasks under load + log volume bounded by drain count, not submit count), `TestAutoEmbedObserver_HTTPCreateNodeBackpressure` (HTTP-surface bookend; 400 POST /nodes all return 201 under 83% drop rate; HTTP latency bounded).

Reference doc: `docs/internals/design/TRACK_R_AUTO_EMBED_HTTP_LOAD_VERIFICATION_2026-05-14.md`. ~~**Component (1c) Docker/k8s exercise is the one remaining verification component.**~~ Component (1c) discharged 2026-06-02 — see below.

#### Reconciliation 2026-06-02 — the vector path was end-to-end broken over REST; now fixed (and (1c) is newly meaningful)

Track R shipped a *structurally* correct per-tenant vector path, but exercising it over REST against released `main` (while validating the sibling `understand-graphdb` consumer) surfaced **two independent, pre-existing bugs neither this doc nor the R-track tests caught** — because the existing vector tests assert only "N results with valid IDs," never nearest-neighbour correctness. Both are now fixed on `main`:

- **REST vector ingestion was a silent dead-end** — fixed by **PR #246** (`761dc9a`). JSON number arrays decode to `TypeFloatArray`, but `UpdateNodeVectorIndexes` only indexed `TypeVector`, so a REST/GraphQL client could create + query a vector index but never fill it. #246 indexes a `TypeFloatArray` property as a vector when a vector index already exists for that property name (declared index = intent; zero new API surface).
- **HNSW search recall was 0.0 at scale** — fixed by **PR #243** (`07e75a7`, parallel-authored, reviewed + validated end-to-end here). Three bugs: min-heap/max-heap inversion in candidate selection, k-farthest result extraction, and `pruneConnections` dropping bridge links (replaced with Malkov–Yashunin Algorithm 4). recall@10 0.0 → 1.0.
- **Footprint bench gated** — **PR #247** (`39b6344`). `TestVectorIndex_PerTenantMemoryFootprint/medium` was fast only while search was broken (early termination skipped insert work); with #243's correct search it timed out the suite. Gated behind `GRAPHDB_BENCH_LARGE` like the other large scenarios.

**Consequence for the verification gap:** component (1c) is now *more* valuable than when filed. Before #246/#243, a Docker/k8s `GRAPHDB_AUTO_EMBED_ENABLED=true` run produced vectors that couldn't be ingested or ranked correctly — so (1c) would have validated only "the observer fired." Now the full auto-embed → ingest → search path works end-to-end, so (1c) validates a real searchable result. Validated locally with fully-local Ollama (gemma3:4b summaries → nomic-embed-text embeddings); semantic ranking is correct and well-discriminated.

#### Reconciliation 2026-06-02 — component (1c) discharged

The in-server LSA auto-embed path was exercised in a **container deployment** (`GRAPHDB_AUTO_EMBED_ENABLED=true`, built from the repo `Dockerfile`) under node-create traffic sufficient to exercise the path, and proven **searchable end-to-end with correctly-ordered nearest neighbours** — not just "the observer fired." `POST /nodes` → `AutoEmbedObserver` → LSA embed → `TypeVector` writeback (`UpdateNodeForTenant`) → per-tenant HNSW → `/vector-search` returns the right cluster on top. Two lexically-distinct clusters (ocean / finance); each in-cluster query ranked its own cluster across all of the top 3, with a large in-cluster vs off-cluster margin. Stable across native-arm64 (local binary) and amd64 (container) runs; absolute cosine scores differ by architecture (Halko SVD float rounding) but the **ranking is invariant** — which is why the assertion targets ordering, not score thresholds.

**Scope precision (do not overclaim):** (1c) validates the deployment *wiring* returns correctly-**ordered** neighbours end-to-end. It does **not** re-exercise **#243's recall-at-scale** property — the corpus is 6 searchable vectors, a trivially-navigable HNSW below the scale where #243's collapse bit; recall@10 stays owned by #243's own test. (The ranking assertion *would* catch #243's ordering bugs — heap-inversion, k-farthest — at any scale; it just doesn't prove the at-scale recall fix.) It also does **not** exercise **#246** (`TypeFloatArray` REST ingest) — the observer writes `VectorValue` (`TypeVector`) directly; #246's path is the *external* neural one, validated separately in `understand-graphdb`.

Artifacts: `scripts/verify-track-r-1c-autoembed.sh` (driver; `--docker` mode builds + ups + asserts + tears down) + `docker-compose.track-r-1c.yml`. Reference doc: `docs/internals/design/TRACK_R_AUTO_EMBED_DEPLOYMENT_VERIFICATION_2026-06-02.md`. **The Track R verification gap (1a + 1b + 1c) is now fully closed.** One operational follow-up surfaced: a customer-facing deployment-ordering note (create the vector + LSA indexes *before* the traffic you expect to be searchable) — a productization-doc item, not a code change.

**New optional follow-up (not on critical path):** issue **#248** — HNSW construction worst-case guard. Investigated and reframed: construction cost is governed by data *intrinsic dimensionality*, not a fixed asymptote. Real/clustered embeddings build O(N log N); only high-dim near-uniform or zero synthetic vectors hit O(N²) (concentration of measure). The footprint bench's zero-vectors are that worst case, which is why #247 gates it. Lever (only if a real high-intrinsic-dim workload appears): an `efConstruction` knob + optional visited-node budget cap in `searchLayer`, both trading recall. **Do not benchmark HNSW construction with synthetic uniform/zero vectors.**

---

## Critical path

**TBD.** No new spike-grounded track exists. The 2026-05-14 doc had Track R because F4 + S11 spikes had landed and demanded implementation; after R2.5b there's no equivalent.

The next session should pick from one of:

1. ~~**Run the remaining verification component.**~~ ✅ **Done 2026-06-02.** Components (1a), (1b), and (1c) are all discharged (Option A validated 100 → 1000 tenants; backpressure validated Go × HTTP × burst × sustained × erroring; auto-embed deployment validated searchable end-to-end in a container — see the three § Reconciliation subsections above). **Track R is verified empirically across its full surface.** This option is no longer live; pick (2) or (3).

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

### Reconciliation 2026-05-14 — debt discharged

The 11 inherited PRs were fully resolved by 2026-05-14T00:34Z (8 days ahead of the 2026-05-22 forcing-function deadline). The disposition was **hybrid**, not Path 1 or Path 2:

- **7 merged** (Groups A + B + D, plus the LSA stack): #108 (H4.3 WAL replay), #109 (H4.4 REST claim-uniqueness), #110 (H4.3 snapshot followup), #131 (coord-* skill docs), #135 (LSA B1 persist), #136 (LSA L2 log-entropy), #137 (LSA L3 int8 quantize). The LSA stack used the retag-folded-with-rebase recipe (see `SESSION_HANDOFF_2026-05-14-0034Z.md` § 2).
- **4 closed without merge** (Group C, A8.1 step-4 cleanup): #134 (delete UPGRADE_GUIDE), #138 (PRODUCTION_QUICKSTART rewrite), #139 (legacy-binary refs), #140 (replication-metric orphans). These were doc-cleanup items that lost relevance during the carry-forward window; closing-without-merge was the correct disposition.

The hybrid pattern (some merged, some closed) is the more honest resolution shape than Path 1 / Path 2 anticipated. Future "forcing function" sections in planning docs should allow hybrid as a third path.

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
- **(A) Verification gap closure** — bench + deployment exercise of Track R. ✅ **Fully discharged**: (1a) PRs #195/#209/#212; (1b) PRs #196/#202/#215 (both 2026-05-14); (1c) the containerized auto-embed deployment exercise (2026-06-02 — see § Reconciliation 2026-06-02 — component (1c) discharged). **This option is no longer live.**
- **(B) Inherited-PR triage** — execute the disposition (or bulk-close per the forcing function). **✅ DISCHARGED 2026-05-14** via hybrid disposition (7 merged, 4 closed); see § Inherited PRs § Reconciliation. No longer a live option.
- **(C) New audit** — performance, security, or productization angle (see § Critical path option 3). **✅ SELECTED + EXECUTED 2026-06-02** — performance-under-SaaS-load angle. See § Reconciliation 2026-06-02 — audit (C) commissioned (perf under SaaS load) below.

**Default if no answer**: (C) commission a new audit. Reason: (A) verification gap is now fully closed — (1a) "Option A fits at 1000 tenants, ratio 1.000", (1b) "drop-on-full backpressure holds across Go × HTTP × burst × sustained × erroring", and (1c) "auto-embed is searchable end-to-end in a container deployment" are all answered. With Track R verified across its full surface and no new spike-grounded track, a fresh audit pass is the natural way to earn the next critical path (perf under SaaS load, vector/embedding side-channels, or multi-node scope).

#### Reconciliation 2026-06-02 — audit (C) commissioned (perf under SaaS load)

Decision 9 resolved to **(C)**, angle **performance under realistic multi-tenant SaaS load** (the angle the single-node 2026-05-06 audit structurally could not see, and the one the planning doc flagged as correlating with the just-closed verification gap). Output: **`docs/internals/design/AUDIT_performance_saas_load_2026-06-02.md`** + a new concurrent-multi-tenant write benchmark (`pkg/storage/bench_concurrent_write_test.go`).

**The earned critical path — shrink what `gs.mu` guards (call it Track P).** The audit's synthesis: the global `gs.mu` write/scan serialization is *known and accepted* (CLAUDE.md), but the SaaS-era workload (Track-R auto-embed writes + GraphQL/unlabeled reads) has quietly loaded more work into the sections it guards. Recommendations are **ordered by measured leverage** (the bench dictates the order — leading with the novel HNSW finding would headline a 2.5%-today win):

1. ~~**Fix the WAL group-commit path** (audit H5 → H1).~~ **✅ DONE 2026-06-02 (#255 create, #256 update/delete, #257 edges).** `BatchedWAL.Append` parked on its flush channel *while holding `gs.mu`*, so batching could never amortize fsync — measured **1.7–2.6× worse** than the fsync default, correcting the 2026-05-06 "enable batching" recommendation. Fix: assign the WAL sequence under `gs.mu`, release `gs.mu` before the flush wait, so concurrent tenants share one fsync. All hot node + edge write paths converted; batched bench g=16 **10.49ms → 0.68ms**. *Remainder (minor, not yet done):* `RemoveNodeProperties` + admin index ops (`CreatePropertyIndex`/`DropPropertyIndex`) still take the synchronous path — byte-identical for the non-batched default; convert for completeness when convenient. *Open (deferred):* whether to flip `EnableBatching` default — needs a latency-vs-throughput FlushInterval sweep first (user decision 2026-06-02).
2. ~~**Stop full-cross-tenant scans on label-absent reads** (H4 → M1).~~ **✅ DONE 2026-06-02 (#259 edge-index restart prerequisite, #260 node enumeration, #261 edge enumeration, #262 M1 count).** Unlabeled `MATCH` and *every* GraphQL edge resolver called `GetAllNodes/EdgesForTenant`, which scanned all 256 shards across every tenant and filtered/paginated in memory — O(total-DB), holding `gs.mu.RLock` across the clone loop. Fix: per-tenant `tenantNodeIDs`/`tenantEdgeIDs` enumeration sets (the only index that captures unlabeled nodes / all edges), collect-under-RLock → sort → release → per-shard-RLock clone (A4 pattern); `countNodes` reads `len(index)` (M1). Bench `GetAllNodesForTenant` noisy-neighbor: indexed **flat ~1.7µs** vs legacy **157µs at 10k background (92×)**. #259 first fixed a latent pre-existing bug — `tenantEdgesByType` was never rebuilt on restart, so `GetEdgesByTypeForTenant` returned empty after every reload — which the edge enumeration index then depended on. Also fixed a latent `NodeCount`/`EdgeCount` drift on unlabeled/empty-type deletes. **Scope — what this did NOT do:** the *cross-tenant* fetch-all is gone, but Recommendation #2's *index-level pagination* half is unbuilt. The GraphQL edge connection resolver (`pkg/graphql/pagination_resolvers.go:123`) and REST `listNodes` (`pkg/api/handlers_nodes.go:80`) still call `GetAllEdges/NodesForTenant` then slice in memory, so a 10-of-1M request still clones all of *that tenant's* matching rows. That within-tenant over-materialization is a separate, lower-leverage follow-up (scales with the tenant's own data, not total DB) — sits below item (3).
3. **Pre-position: lift the HNSW insert out of `gs.mu`** (H2) and budget the auto-embed 2× (H3) — **now the next critical-path step.** With (1)'s fsync floor amortized, the ~140 µs serialized HNSW insert is the dominant write term (paid twice per node under auto-embed). `HNSWIndex` already has its own `h.mu`, so the lift is low-risk; sequence it now that (1) and (2) have landed.
4. Index-structure hygiene (M3/M4/M7) + vector-search read hygiene (M5/M6).

Empirically grounded: writes do not scale with tenant count (~170 writes/s aggregate flat across 1→16 concurrent tenants — fsync-dominated; see the audit's honest caveat that the bench does not isolate `gs.mu` from shared-WAL fsync). **Items (1) and (2) are now done (2026-06-02); the next session picks Track P item (3) — lift the HNSW insert out of `gs.mu` (H2) + budget the auto-embed 2× (H3).**

### Carry-forward decisions still open

- **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` is synchronous. SSE/WebSocket streaming is a future enhancement; not a launch question. Still open from 2026-05-14.

---

## Risks specific to this window

- **The verification gap is now fully closed.** (1a) and (1b) discharged 2026-05-14 (per-tenant HNSW heap ratio 1.000 across 100→1000 tenants; backpressure across Go × HTTP × burst × sustained × erroring); (1c) discharged 2026-06-02 (containerized auto-embed exercise, searchable end-to-end with correct ranking). The "enterprise customer hits an env-driven-bootstrap constraint" risk is retired — the deployment-ordering constraint it surfaced (create indexes before traffic) is a productization-doc note, not a code gap.

- **The inherited-PR carry-forward debt is now load-bearing.** Four sessions of inaction means there's no consensus on whether these PRs matter. The forcing function above retires the debt one way or another. If neither happens by 2026-05-22, this planning rhythm starts losing credibility — future "merge or close by X" deadlines won't bind either.

- **~~No new critical path is a feature, not a bug.~~ Resolved 2026-06-02: there is now an earned critical path (Track P).** The 2026-05-14 doc had Track R because two spikes demanded it; after Track R closed, "TBD critical path" was the honest state and the risk was manufacturing a track to fill the gap. That risk is retired the right way: audit (C) was commissioned and produced a measured, evidence-grounded backlog — **Track P (shrink what `gs.mu` guards)**, ordered by bench leverage (WAL group-commit → cross-tenant scans → HNSW-out). See § Decision 9 Reconciliation 2026-06-02. **Items (1) WAL group-commit and (2) cross-tenant scans are now done (2026-06-02, #255–#257 and #259–#262); the critical path advances to item (3) HNSW-out (H2/H3).**

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

1. ~~**Confirm R2.5b (#193) merged.**~~ ✅ Merged 2026-05-13 (`39247af`). No action needed.
2. **Pick a critical-path option from § Decision 9.** Default is now (C) commission a new audit — the (A) verification gap is fully closed (1a/1b discharged 2026-05-14; 1c discharged 2026-06-02), and option (B) is no longer live (discharged). Document the chosen audit angle in the PR description.
3. ~~**Address the inherited-PR forcing function** if 2026-05-22 has passed.~~ ✅ Discharged 2026-05-14 via hybrid disposition; see § Inherited PRs § Reconciliation. No action needed.
4. **After 1-3 PRs land**, this checkpoint should be revisited. Trigger: any of the live critical-path options being picked and at least one PR landed against it.

**Revisit triggers** (any one is sufficient to start a new checkpoint immediately):
- ~~**Verification gap exercise surfaces a real constraint** — e.g., per-tenant HNSW memory blows up at 500 tenants. That changes the OSS vs enterprise architectural assumption and warrants its own track.~~ — Fully discharged: (1a)/(1b) 2026-05-14, (1c) 2026-06-02. Per-tenant HNSW heap flat across 100 → 1000 tenants (ratio 1.000); backpressure holds across Go × HTTP × burst × sustained × erroring; auto-embed searchable end-to-end in a container with correct ranking. The OSS-vs-enterprise architectural assumption is empirically validated; no constraint surfaced (only a deployment-ordering doc note). This revisit trigger is retired.
- **A customer-driven priority lands on the queue** — re-plan in the customer's terms.
- ~~**Inherited-PR forcing function deadline passes (2026-05-22)**~~ — ✅ Discharged 2026-05-14 via hybrid disposition (7 merged, 4 closed). No longer a revisit trigger.

---

## Appendix — what the next agent does differently because of this doc

The previous planning docs accumulated content over time. This doc tries to add only things that change the next session's behavior:

- **§ Inherited PRs forcing function**: the deadline of 2026-05-22 is the new artifact. Without it, the next session reads "carry forward" and inherits the same indecision.
- **§ Verification gap as the default critical path**: the doc nominates an action rather than re-asking. (As of 2026-06-02 the gap is fully closed — (1a)/(1b)/(1c) all discharged — so the default has shifted to (C) commission a new audit.)
- **§ Track C tail split into individual items**: trackable separately; each has a clear acceptance criterion so the next session can pick one off without designing scope.
- **§ Risks "No new critical path is a feature, not a bug"**: explicit caution against the manufacture-a-track failure mode.

Everything else in this doc is reconciliation of merged state — read it once, internalize, don't re-read on each PR.
