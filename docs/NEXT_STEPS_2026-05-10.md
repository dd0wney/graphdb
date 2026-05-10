# Plan: Next Steps (graphdb) — 2026-05-10

**Predecessor**: [`NEXT_STEPS_2026-05-08.md`](./NEXT_STEPS_2026-05-08.md). This document reconciles that plan against current `main` (through PR #64) and projects the next 90 days.

**Sources still load-bearing**:
- Audit synthesis: [`AUDIT_synthesis_2026-05-06.md`](./AUDIT_synthesis_2026-05-06.md) — five cross-cutting findings; closure tracked below.
- Killer-features synthesis: [`FEATURES_synthesis_2026-05-08.md`](./FEATURES_synthesis_2026-05-08.md) — three lead candidates, storage-interface unlock thesis.
- A8 / A9 design spikes: [`A8_REPLICATION_TENANCY_DESIGN.md`](./A8_REPLICATION_TENANCY_DESIGN.md), [`A9_SCHEMA_ISOLATION_DESIGN.md`](./A9_SCHEMA_ISOLATION_DESIGN.md).
- F2 design spike: [`F2_GRAPHRAG_DESIGN.md`](./F2_GRAPHRAG_DESIGN.md) — captures the `/v1/retrieve` endpoint-shape decision (the May 8 plan named `expand_hops` on `/hybrid-search`; the spike re-scoped it).

---

## State reconciliation

### Track M — Merge in-flight PRs ✅ **CLOSED**

All M1–M6 items shipped. PRs #2, #3, #4, #5, #15, #16 merged into `main`.

### Track A — Audit / tenancy isolation ✅ **CLOSED — original scope + A4-edges + A8.2 done 2026-05-10; one A8 follow-up (A8.1) remains, off critical path**

| Original task | Status | Evidence |
|---|---|---|
| A1: `pkg/tenantid` leaf package | ✅ Done | PR #5 (`bd50589`) |
| A2: `JWT_SECRET` fail-closed | ✅ Done | PR #4 (`95657fa`) |
| A3a: additive `*ForTenant` variants | ✅ Done | PR #15 (`9f1f5e1`) |
| A3b: enforce `matchesTenant` | ✅ Done | PR #16 (`54b44e4`) |
| A4: shard locks + clone-elision on `GetNode` | ✅ Done | PR #67 (`f44d01c` partition, `89dcbcf` clone-elision, `1e92e21` bench, `eb7026a` matchesTenant cleanup); throughput criterion reframed — see A4 section below |
| A5: `withTenant` middleware on remaining REST routes | ✅ Done | `4afa405`, hardened by `9a442ef` |
| A6a: `/nodes` `/edges` `*ForTenant` migration | ✅ Done | `85a0835`, follow-up `d561281` |
| A6b: `/traverse` `/shortest-path` tenant scoping | ✅ Done | `5c9ac3c` |
| A6c: storage iter / GraphQL ctx / GraphQL resolvers / query / algorithms | ✅ Done | `cd2266c`, `6b960c4`, `c0c1a6c`, `6397dd4`, `974df44` |
| A7: cross-tenant regression suite | ✅ Done | `793bc8f` (`pkg/api/audit_regression_test.go`) |

**New sub-tracks added after May 8** — not in the original plan:

| Sub-track | Status | Evidence |
|---|---|---|
| A8: replication tenancy (5 commits) | ✅ Done | A8 spike `d32d30d`, impl `ade06c6` `fbea625`, regression `c0e90fb`, legacy gate `1051a34`, banner `0fe7425` |
| A8.2: replica `/nodes` GET unauth'd cross-tenant dump | ✅ Done | PR #81 (`c42f9b6`) — removed route from both replica binaries, refactored to private mux, audit-regression row added |
| A9: GraphQL schema introspection isolation (4 commits) | ✅ Done | A9 spike `5c771fa`, impl `22ee7a2` `7b220d0`, regression `d3fa00d` |

**A4 closed (2026-05-10, PR #67)** — partitioned `gs.nodes` for race-cleanness, clone-elision in vector post-filter, concurrent-read bench. **A4-edges follow-up closed (2026-05-10, PR #70)** — symmetric partition for `gs.edges` + transaction-layer coordination collapsed three surfaces to one (Commit() holds gs.mu around apply* calls, neutralizing surfaces 2+3); race-clean under `-count=3`.

**A8 spike-defined follow-ups** (called out in `A8_REPLICATION_TENANCY_DESIGN.md` lines 232, 266):
- **A8.1**: Deprecate or rebuild standalone primary/replica binaries on top of `cmd/server` infrastructure. The current `GRAPHDB_LEGACY_BINARY` gate is a holding action, not a fix. **Off critical path.**
- **A8.2**: Replica's `/nodes` GET unauth'd cross-tenant dump. **✅ Done 2026-05-10 (PR #81)** — removed route from both `cmd/graphdb-replica` (real leak via `GetAllNodesAcrossTenants()`) and `cmd/graphdb-nng-replica` (empty-stub, removed for symmetry). See A8.2 section below for the remove-vs-add-auth decision.

### Track F — Killer features

| Original task | Status | Evidence |
|---|---|---|
| F1: `/v1/embeddings` OpenAI-compat endpoint | ✅ Done | PR #7 (`09d527b`) |
| F1.1: per-tenant LSA (named as follow-up) | ❌ Not started | The multi-tenant caveat documented in F1 is still live |
| F2: GraphRAG retrieval — **shipped as `/v1/retrieve` (design pivot)** | ✅ Done | F2 #1–#7: spike `b2943d3`, factor `079a3c6`, package `039e8d1`, handler `01afe54`, regression `3a84cdf`, bench `80bbdc9`, docs `0f673b6` |
| F3: Compliance API package | ❌ Not started | `pkg/masking/`, per-tenant property filter, audit log all live; integration surface absent |

**F2 design pivot** is documented in `F2_GRAPHRAG_DESIGN.md` and rationalized in commit `b2943d3`: LangChain BaseRetriever shape (not OpenAI-compat — OpenAI defines no retrieval endpoint), new `POST /v1/retrieve` (not `expand_hops` on `/hybrid-search`), avoid "graphrag" in the URL. The May 8 plan's F2 task description is superseded; the spike doc is authoritative going forward.

### Cross-cutting cleanup since May 8 (not tracked in original plan)

- **Lint sweep**: batches 1, 2A, 2B-correctness/handlers/logger, 2C-mechanical, 2D, 2E, 2F all merged (PRs #50, #52–#58, plus prior #14, #12). Cadence has noticeably slowed — the residual is small.
- **Test-flake roster** (May 2026): WAL race (#59), query Linux failure (#60), perf-regression skip (#61), 5M-node cache assertion tier (#62), parser cleanup (#64). CI is now reliably green on PRs; remaining red is overwhelmingly Linux-runner cancellation (exit 143), not real regressions.
- **Coord-deploy track** ✅ **CLOSED 2026-05-10**: gap-spike → operational MVP → multi-project schema → B-lite atomic claim resolver → skill rewrite. PRs #85–#93. Daemon now runs locally with real atomic uniqueness; skills exercise the live surface.

---

## The next 90 days

Capacity assumption: **~2–3 PRs/week** (calibrated against PRs #50–#64 cadence). 12 weeks ≈ 24–36 PRs. Plan below totals ~20 PRs, leaving real slack for the inevitable interrupt-driven work (lint findings, CI hiccups, customer-driven fixes).

Sequencing principles carried forward from the May 8 plan:
- One logical commit per task; one PR per task.
- Track-letter naming preserved (M / A / F); two new letters introduced (**H** housekeeping; **S** scoping spike).
- Spike → ~4 PRs → audit-regression-row pattern (the shape A8/A9 used) is the default for any new sub-track that touches tenant-scoped code paths.

### Track A — Audit closure

#### A4. Shard locks + clone-elision on `GetNode` ✅ DONE 2026-05-10 (PR #67)

Landed as four commits on `feat/audit-a4-shard-partition`:
- `feat(storage): partition gs.nodes for race-cleanness` (`f44d01c`) — `gs.nodes` becomes `nodeShards [256]map[uint64]*Node`; readers/writers migrated onto per-shard locks; `closed` becomes `atomic.Bool`.
- `feat(api): clone-elision in vector post-filter` (`89dcbcf`) — vector handler uses storage's `WithNodeRefForTenant` callback so the filter sees a live ref; `Clone()` runs only for survivors. Allocations on the hot path drop from O(ef) to O(survivors). Closes audit HIGH-1.
- `test(storage): concurrent-read benchmark + lock-grain ratio` (`1e92e21`) — three access-pattern axes × two contention axes × Legacy global-RLock baseline isolating the lock-grain delta.
- `chore(api): drop unused matchesTenant helper` (`eb7026a`) — orphaned by the clone-elision change; removed per the codebase's "delete unused" convention.

**Acceptance reframed**: the original "≥2× throughput at 4 readers" criterion did NOT hold empirically on M1 (lock-grain ratio 1.02×/1.08×/1.15× at 4/16/32 readers). Reason: Go's `RWMutex.RLock` doesn't contend with other RLockers; pure-reader workloads measure cache-line / atomic-op cost, not the lock-wait fraction the audit's projection assumed. The value delivered is **structural correctness** (race-detector clean under `-count=3` — closes the latent shared-map race that `transaction_commit.applyNodeUpdates` was creating against global readers) plus the named clone-elision. The empirical finding is documented in the bench file's header so future readers see both the spec and the outcome.

#### A4-edges. Symmetric partition for `gs.edges` + transaction-layer coordination ✅ DONE 2026-05-10 (PR #70)

Landed as PR #70 — partitioned `gs.edges → edgeShards [256]map[uint64]*Edge`, mirrored A4's helpers, migrated `GetEdge`/`CreateEdge`/`UpdateEdge`/`DeleteEdge` (and `*ForTenant` variants) onto per-shard locks, added `BenchmarkGetEdge_Uniform_PureReads_4` + Legacy baseline.

**Three-surfaces-to-one collapse**: the in-code note above `GetEdge` originally enumerated three lock-disagreement surfaces. Investigation found surfaces 2+3 (transaction-layer writes, DeleteNode cascade) are already neutralized because `Commit()` holds `gs.mu` around the `apply*` calls — only surface 1 (`GetEdge` shard.RLock vs writer gs.mu.Lock) needed the fix. Race-clean under `go test -race ./pkg/storage/... -count=3`.

#### A8.1. Rebuild standalone replication on `cmd/server` (nng-only after H1)
- [ ] Spike doc: with H1 having narrowed the surface to nng + the un-prefixed `cmd/graphdb-{primary,replica}` (in-process transport), the open question is: rebuild `cmd/graphdb-nng-{primary,replica}` to share `cmd/server`'s tenant middleware stack, or delete them and document migration to `cmd/server`'s built-in replication.
- [ ] Decision recorded as a go/no-go in the spike, then 1–2 implementation PRs.
- **Acceptance**: `GRAPHDB_LEGACY_BINARY` becomes either (a) a removed env var, or (b) gates only a thin shim that delegates to `cmd/server` plumbing.

#### A8.2. Replica `/nodes` GET unauth'd cross-tenant dump ✅ DONE 2026-05-10 (PR #81)

Landed as PR #81 (`c42f9b6`) — chose **remove** over **add-auth** because (1) replication uses the WAL stream not HTTP so the route was inspection-only, (2) wiring `requireAuth + withTenant` would re-implement middleware in binaries A8.1 wants to retire, (3) future replica read-API should ride `cmd/server`'s middleware stack. `cmd/graphdb-replica`'s leak (`graph.GetAllNodesAcrossTenants()`) and `cmd/graphdb-nng-replica`'s empty-stub were both removed for symmetry.

Side change: each binary's `startHTTPServer` now uses a private `*http.ServeMux` via `buildHTTPHandler` (required for the regression tests; also marginal hardening — no default-mux pollution can collide). Audit-regression row added at `pkg/api/audit_regression_test.go` reference map; per-binary regression tests at `cmd/graphdb-{,nng-}replica/server_test.go` (`TestBuildHTTPHandler_A82_NoNodesRoute`).

### Track F — Features

#### F1.1. Per-tenant LSA
- [ ] **Spike** (1 PR): design doc `docs/F1_1_PER_TENANT_LSA_DESIGN.md`. Specify per-tenant LSA model build trigger (lazy on first semantic-search request? eager on tenant create?), storage cost (N tenants × 200-dim × vocabulary), migration path for existing single-LSA deployments. Explicit go/no-go recommendation at the end.
- [ ] If go: 2–3 implementation PRs adapting `pkg/search/tenant_indexes.go`'s pattern to `pkg/search/lsa.go`.
- [ ] Cross-tenant test: writes to tenant B do not change tenant A's embedding output for the same input.
- [ ] Update `/v1/embeddings` to route per-tenant; remove the multi-tenant caveat from `docs/API.md`.
- **Acceptance**: Per-tenant cross-tenant embedding test passes; bench shows per-tenant build cost is bounded by per-tenant corpus size.

#### F3. Compliance API package
- [ ] New `pkg/api/handlers_compliance.go`. Endpoints: `GET /v1/compliance/audit-log` (paginated, filtered), `POST /v1/compliance/masking-policy`, `GET /v1/compliance/masking-policy/{tenant}`.
- [ ] Swagger annotations.
- [ ] Reference SOC2/GDPR integration guide in new `docs/COMPLIANCE.md`.
- [ ] Audit-regression row: cross-tenant policy access denied.
- **Acceptance**: Audit log returns tenant's events in append-only order; masking policy applies to all read paths (Get/List/Search/Vector) — pinned by the existing per-tenant property-filter test surface.

### Track H — Housekeeping (new)

#### H1. Delete the `-tags zmq` replication variant ✅ DONE 2026-05-10 (PR #65 + #66)

Landed as PR #65 (`a356e1f`) — removed `pkg/replication/zmq_*.go` (5 files), `cmd/graphdb-zmq-{primary,replica}/`, `test-zmq-replication.sh`. `go mod tidy` removed `github.com/pebbe/zmq4`, eliminating the only CGO dependency in the replication tree. Tagged-build CI for the surviving `nng` variant landed as PR #66 (`508acb5`); workflow-scope token meant it had to split out from #65.

#### H2. Consolidate `requireAdmin` to a single helper
- [ ] `handleTenantEndpoint` asserts admin claims that handlers re-fetch — ~12 call sites. One PR introducing a `requireAdmin` middleware/helper, migrating sites, and pinning behavior with a regression row.
- **Acceptance**: All 12 sites converted; behavior unchanged (verified by existing auth tests); claim-double-check pattern eliminated.

#### H3. Local branch cleanup ✅ DONE 2026-05-10

Force-deleted 21 stale branches whose PRs were squash-merged. Squash-merge breaks `git branch -d`'s reachability check (the squashed commit on main is content-equivalent but not ancestor-equivalent to the branch tip), so `-D` was required after verifying each branch had a merged PR via `gh pr list --head <branch> --state merged`. `git branch | grep -c audit` = 0; only `main` remains. No remote cleanup attempted — the remote branches still exist on origin but no longer affect local state. Future PRs that use `--delete-branch` at merge time avoid recreating this debt.

#### H4. Coord-deploy gap ✅ DONE 2026-05-10 (PRs #85, #86, #87, #89, #91, #93)

Surfaced by PR #82 (`docs/COORD_GAP_2026-05-10.md`) when the 2026-05-10 02:36Z session attempted to deploy the parallel-agent coord instance per `docs/COORD_SETUP.md`. Pre-flight found the deploy commands and skill bash blocks referenced an API surface that doesn't exist. Resolution chose **B-lite** (resolver-side `:Claim` uniqueness) per spike recommendation; rolled out across six PRs over 2026-05-10.

**What landed:**

- [x] Spike doc: `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` (PR #85). Recommends B-lite.
- [x] Operational MVP: coord daemon running, `scripts/coord-bootstrap.sh` + `scripts/coord-seed.sh`, `docs/COORD_SETUP.md` rewritten (PRs #86, #87).
- [x] Multi-project schema: `:Project` nodes, `:IN_PROJECT` edges, project-prefixed Task IDs (`graphdb:H4-PR1-blite`), conflict-guard against `COORD_PROJECT` mismatch, multi-project safety verified (PR #89).
- [x] **B-lite resolver**: atomic `:Claim` uniqueness via new `storage.CreateNodeWithUniquePropertyForTenant` + special-case in `createNodeMutationResolver`. Two agents racing for the same task get exactly one winner, with a structured "unique constraint violation" error naming the conflicting node (PR #91).
- [x] **H4.2 wiring** (closed as part of #91): `cmd/server`'s GraphQL had no Mutation type — `pkg/graphql/limits.go` was queries-only. Extracted shared `buildMutationType` and mounted on both schema generators. B-lite is now reachable end-to-end from cmd/server's `/graphql`.
- [x] **Skill rewrite**: `.claude/skills/work-claim/`, `worktree-spawn/`, `merge-coordinator/` rewritten against real REST + GraphQL surface. `work-claim` uses GraphQL `createNode` for the Claim (REST `POST /nodes` bypasses B-lite — explicitly noted), HOLDS+FOR via REST `/edges`. Live-verified end-to-end (PR #93).

**Strategic framing held**: graphdb coordinates its own development with **real atomic claim semantics** (verified live: 10-way concurrent claims for the same task → 1 success, 9 structured conflicts citing the same winner ID). The dogfood claim now lands without footnote — see memory `project_graphdb_dogfoods_coord.md`.

**Off-track follow-ups discovered, not closed under H4** (each is independent of H4's deliverables; promote to top-level next planning round):

- **H4.1** *(open)*: REST `/nodes` GET base64-encodes string properties (`pkg/api/handlers_nodes.go:34` — `props[k] = v.Data` where `Value.Data` is `[]byte`). All read-side coord scripts and skill bash blocks work around this with Python decode helpers. Type-aware decoding before `respondJSON` is a single-PR cleanup; affects every REST `/nodes` consumer, not just coord.
- **H4.3** *(net-new)*: snapshot-replay drops the per-tenant label index. `replayCreateNode` populates global `nodesByLabel` but not `tenantNodesByLabel`, so per-label GraphQL queries (`{ tasks { id } }`) return "Cannot query field" after restart until the next write. Skill bash blocks already work around this with REST `/nodes` + client-side label filter; the proper fix is to mirror the tenant-index population in `persistence_replay.go`. Single-PR cleanup.
- **H4.4** *(net-new)*: REST `POST /nodes` doesn't enforce B-lite uniqueness. The check is GraphQL-resolver-only. Skills route Claim creation through GraphQL explicitly, but a future caller using REST would silently bypass. Mirroring the check in `pkg/api/handlers_nodes.go` would close this; ~30-50 LOC.

### Track S — Scoping spike (new)

#### S1. Storage interface extraction — spike with binary go/no-go
- [ ] Design doc `docs/S1_STORAGE_INTERFACE_DESIGN.md`.
- [ ] Section 1: Proposed interface signatures (subset for read path; full surface for write path; tenant-aware throughout — A3a/A3b's `*ForTenant` shape codified at the contract layer).
- [ ] Section 2: Migration plan as a PR-by-PR breakdown — explicit number of PRs and ordering, just like A8/A9 spikes specified.
- [ ] Section 3: Risk register (lock semantics, performance regression risk vs. the shard-lock work A4 lands, plugin-system implications).
- [ ] Section 4: **Explicit go/no-go recommendation** — the spike must conclude with either "schedule for Q3 as track-banner item" or "defer further; here's the trigger condition."
- **Acceptance**: One reviewer + advisor call sign off on the spike. Decision committed under "Decision points" in the next planning checkpoint.

---

## Sequencing graph

```
H1 ✅ ──┐
        ├─→ A4 ✅ ──→ A4-edges ✅ ──→ A8.2 ✅ ──┐
H3 ✅ ──┘                                       ├─→ F1.1-spike → F1.1-impl → ┐
                                                └─→ H2 ────────────────────── ├─→ F3 → A8.1 → S1
```

**Critical path**: ~~H1~~ → ~~A4~~ → ~~A4-edges~~ → ~~A8.2~~ → **F1.1-spike** → F1.1-impl → F3 → A8.1 → S1.

Off-path parallel work: ~~H3~~ ✅ (branches), ~~H4~~ ✅ (coord-deploy gap — closed via B-lite + skill rewrite + multi-project, PRs #85–#93), H2 (requireAdmin) anywhere there's a small gap. The H4.x net-new sub-tracks (H4.1 base64, H4.3 replay-tenant-index, H4.4 REST-uniqueness-mirror) are each single-PR cleanups suitable as small parallel work — none are blocking.

**Why this ordering**:
- **H1 first** ✅ — broken `main` builds were creating false-positive CI signal across every other PR's matrix; closed via PRs #65 + #66.
- **A4 early** ✅ — the audit's HIGH-1/HIGH-2/CRIT-1 collapsed into one operation; closed via PR #67 (with the throughput-criterion reframe documented).
- **A4-edges after A4** ✅ — the partition idiom and helper shape were fresh from A4; landed PR #70 with the three-surfaces-to-one collapse documented (Commit() holds gs.mu around apply* calls, neutralizing surfaces 2+3).
- **A8.2 after A4-edges** ✅ — A8.2 is a security finding but the legacy-binary gate (`GRAPHDB_LEGACY_BINARY` fail-closed, PR #47) meant exposure was mitigated-in-depth, so A4-edges (real concurrency bug class manifesting as `TestGraphStorage_ConcurrentDeletion` flakes) went first. PR #81 closed A8.2 via removal rather than auth-wrap.
- **F1.1-spike now at head of queue** because the audit track is fully retired (A4 + A4-edges + A8.2 done; A8.1 remains but is off critical path). New feature surface (F1.1) rides on a clean audit track.
- **F1.1 before F3** because the multi-tenant LSA caveat is a documented hole in a *shipped* feature (`/v1/embeddings`); F3 introduces *new* surface and customer-facing claims, so it should ride on a clean F1.
- **A8.1 late** because it's an architectural cleanup of binaries that are already gated by `GRAPHDB_LEGACY_BINARY`; the urgency is lower.
- **S1 last** because its output is the **input to the next planning checkpoint**, not work for this one.

---

## Decision points

These are explicit questions the user should weigh in on rather than decisions baked into the plan.

1. **Storage interface extraction — promote to next quarter, or defer further?**
   This plan schedules S1 (the spike) at the end of the 90 days. The spike's go/no-go is the binary deliverable. Do not pre-commit either way.
2. **F1.1 vs. F3 ordering** — currently F1.1 first (fix the documented multi-tenant LSA caveat in shipped F1) before F3 (new market surface). If a customer signal pulls F3 forward, swap the order; the dependency graph allows it.
3. **A8.1's spike conclusion** — could land as either "delete the standalone binaries" or "rebuild on `cmd/server`". The spike decides; the decision is not pre-baked.
4. **Per-tenant LSA build trigger** (lazy vs. eager) — answered inside the F1.1 spike.

---

## Carry-forward decision points from May 8 plan (still open)

1. **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` shipped synchronously per F2 spike. SSE/WebSocket streaming is now a *future* enhancement on the existing handler, not a launch question. Reframe in next planning round.
2. **Compliance API: REST-only or also GraphQL?** — folds into F3 scoping; defer until F3 starts.
3. **Cypher revisit timing** — still gated on storage interface extraction. S1's go/no-go is the trigger.

---

## Risks specific to this 90-day window

- **A4 is the riskiest scheduled item**. Lock-ordering refactors are advisor-call territory by the original plan's own admission. Underestimating it shifts the entire critical path right. Mitigate by getting the bench numbers first (read-only — baseline doesn't need any code change) so the *value* of the change is quantified before the change lands.
- **F1.1 storage cost may invalidate per-tenant LSA**. N tenants × 200-dim × vocabulary memory could push small-deployment users over a footprint they accepted in F1. The spike must produce a memory model, not a hand-wave. If the model says "no go," document it and remove the multi-tenant LSA caveat from F1's docs by stating the design constraint explicitly rather than promising a fix.
- **S1's spike could conclude "do this NOW"**. If the design doc determines that storage interface extraction must happen before F3 ships safely, the 90-day plan needs to be re-ordered, not extended. That outcome is a feature, not a failure — but plan for the possibility of mid-quarter re-planning.
- **Replication-binary deprecation (A8.1) may surface customer dependencies** invisible in the codebase. The spike should poll `cmd/graphdb-primary` and `cmd/graphdb-replica` users (if any are reachable) before deciding to delete vs. rebuild.

---

## Out of scope (carry forward + new)

- **Cypher / GQL** — gated on S1 outcome.
- **Geospatial / temporal data-model features** — defer until F1.1 + F3 ship and a real perf bench tells us where headroom is.
- **Performance tracks B2/B3/B4** (HNSW visited-set `sync.Pool`, cosine norm hoisting, LSM cache lock) — opportunistic only, no urgency, do not promote to scheduled work without a new perf signal.
- **Code-quality tracks C1/C2/C3/C4** — opportunistic.
- **Audit lint discrepancy investigation (D3)** — superseded; the lint sweep batches resolved most of the surfaced findings. Close the open question rather than carrying it forward indefinitely.
- **Mobile / `gomobile` / `pkg/mobile`** — Syntopica-v2 ruled this out (April 2026 decision). Do not propose without a new triggering signal.

---

## Known limitations + productization gaps

Surfaced from a session-end audit of "what would block a serious enterprise customer." Some items overlap with existing roadmap entries (linked); others are net-new gaps that aren't sequenced into the 90-day plan above. **This list is the input to the next planning checkpoint, not the plan itself** — each new gap is a track-banner-sized commitment.

### Already tracked elsewhere in this doc

- **S1** — storage interface extraction. Without it, the codebase is one big package and the plugin/extension story is undefined. Spike scheduled at the end of the 90-day window; if its go/no-go says "do this NOW" the rest of the plan re-orders. The largest unlock for "third-party storage backends" or "embedded engine for other products."
- **F1.1** — per-tenant LSA. Cross-tenant embedding leakage is documented as a caveat in shipped F1, but the documented hole is reachable today by any customer running multi-tenant `/v1/embeddings`. Spike + impl scheduled mid-window.
- **F3** — Compliance API package. SOC2/GDPR integration is named as a deliverable but not built; the underlying primitives (`pkg/masking/`, per-tenant property filter, `pkg/audit/`) all exist, but the customer-facing surface tying them together is absent.
- **A8.1** — standalone-binary architectural cleanup. The `GRAPHDB_LEGACY_BINARY` fail-closed gate (PR #47) is a holding action, not a fix; the legacy binaries still exist and an operator that opts in still gets the old code path.

### Architectural ceilings (net-new — not on the 90-day plan)

- **Single-node assumption baked in.** Replication is primary/replica (A8 design), not sharded — write throughput can't scale beyond one box. Any horizontal-scale story (sharded write path, consensus, distributed query) is a multi-quarter scope. Either commit to the work as a track-banner item or explicitly document "single-node by design; horizontal scale is a customer's responsibility (e.g., per-tenant deployments behind a router)" at the README/positioning level. Currently undocumented.
- **LSA scale ceiling (~100K-500K docs at 200 dims).** Documented in F1 internal docs but not at the README/positioning layer. For commercial corpora the customer would swap LSA for a real embedding model (OpenAI text-embedding-3, Anthropic, BGE, etc.). Not blocked — `/v1/embeddings` is OpenAI-shape compatible and BYO embeddings work via the same surface — but the OOTB story is limited and the workaround isn't documented.

### Operational / observability (net-new)

- **No production-grade observability narrative beyond `pkg/metrics`.** No tracing (no OpenTelemetry / Jaeger / Honeycomb integration), no SLO/SLI document, no dashboards-as-code, no runbook surface. A serious enterprise eval would ask for these before signing — usually as part of the "ops readiness" checklist. Scope: ~1-2 PRs for OpenTelemetry tracing wiring, plus a `docs/SLO.md` with target latency / error budget per endpoint, plus example Grafana JSON.
- **Linux CI infra tax.** `make test-race` consistently exits 143 (runner-cancellation) on every PR; tolerated per session-memory framing as known infra, not regression. A customer running CI against the upstream repo would see red checks and ask why. Two structural fixes possible: split the race target across packages (so each batch fits the runner's idle-timeout budget), or bump the runner timeout in `.github/workflows/`. Single small PR either way; worth doing before any external-developer onboarding pass.

### Commercial / docs (net-new)

- **Documentation surface is internal-audit-shaped, not customer-shaped.** `docs/AUDIT_*.md`, `NEXT_STEPS_*.md`, design spikes (`A8_REPLICATION_TENANCY_DESIGN.md`, `A9_SCHEMA_ISOLATION_DESIGN.md`, `F2_GRAPHRAG_DESIGN.md`) dominate the docs tree; getting-started, integration guides, "5-minute quickstart," language-client examples, deployment recipes are thin. A README-funnel pass would surface this — the work is mostly reorganization plus one or two new guides, not a research project.
- **No stated commercial offering.** No pricing, no support model, no SLA, no licensing story. `pkg/licensing/` exists as a code module but isn't the same as a market offering. Decision the technical work assumes but hasn't articulated: open-source-first with paid support? Dual-license (AGPL + commercial)? Hosted-only? Each implies different architectural priorities — a hosted offering pulls A8.1 and observability forward; open-source-first pulls customer docs forward. **Worth its own founder-led discussion, not a technical-track decision.**

### Suggested triage for next checkpoint

When this document is superseded by `NEXT_STEPS_<DATE>.md` for the next window:

1. **Decide the commercial-offering question first** — it shapes which gaps are urgent vs. deferred.
2. **Promote the Linux CI infra tax to a small in-cycle PR** regardless of commercial direction — it's a 1-2 hour fix that pays back in every subsequent PR's review surface.
3. **Bundle observability + customer docs into a single "ops readiness" track** if the commercial answer says "hosted" or "enterprise self-serve."
4. **Surface the single-node ceiling as a README positioning paragraph** even if you don't commit to the horizontal-scale work — silent ceilings are worse than documented ones.

---

## How to use this document

This is a planning checkpoint, not a backlog. Work below the line is grouped by sequencing-graph dependency, not priority. When picking up the next PR:

1. Read the next item on the critical path (or any unblocked off-path item).
2. If the item has a "spike" sub-task, do the spike first and **stop** before implementation — the spike's recommendation may change scope.
3. After ~3–5 PRs land, this checkpoint should itself be revisited and superseded by a `NEXT_STEPS_<DATE>.md` for the next window. Starting fresh is a legitimate option then; this document does not need to live forever.

**Revisit triggers** (any one is sufficient to start a new checkpoint immediately, not after the 3–5 PR cadence):
- **S1 spike concludes** with a "schedule for Q3 as track-banner item" recommendation — that decision re-orders the next quarter, not just the next item.
- **A8.1 spike concludes** with "rebuild on `cmd/server`" rather than "delete" — rebuild is a multi-PR sub-track that wasn't budgeted in this checkpoint.
- ~~**A8.2 turns out to be reachable** through a path the legacy-binary gate doesn't cover — promotes immediately to head of queue.~~ ✅ Closed 2026-05-10 by PR #81 (route removed; cross-tenant request now returns 404 on both replica binaries).
