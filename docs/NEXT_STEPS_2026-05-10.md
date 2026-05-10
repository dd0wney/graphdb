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

### Track A — Audit / tenancy isolation ✅ **CLOSED for original scope; A4 done 2026-05-10; one A4 follow-up + two A8 follow-ups**

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
| A9: GraphQL schema introspection isolation (4 commits) | ✅ Done | A9 spike `5c771fa`, impl `22ee7a2` `7b220d0`, regression `d3fa00d` |

**A4 closed (2026-05-10, PR #67)** — partitioned `gs.nodes` for race-cleanness, clone-elision in vector post-filter, concurrent-read bench. **One follow-up surfaced**: **A4-edges** (symmetric partition for `gs.edges` + transaction-layer coordination — three known surfaces of one bug class; details in the A4-edges section below and the in-code note at `pkg/storage/edge_operations.go` above `GetEdge`).

**A8 spike-defined follow-ups** (called out in `A8_REPLICATION_TENANCY_DESIGN.md` lines 232, 266 — not yet tracked):
- **A8.1**: Deprecate or rebuild standalone primary/replica binaries on top of `cmd/server` infrastructure. The current `GRAPHDB_LEGACY_BINARY` gate is a holding action, not a fix.
- **A8.2**: Replica's `/nodes` GET is an unauth'd cross-tenant dump. Separate from A8's write-path finding; the read path on the replica binary is independently exposed.

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

#### A4-edges. Symmetric partition for `gs.edges` + transaction-layer coordination

The same lock-disagreement bug class A4 closed for nodes has three known surfaces on edges and the transaction layer — see the in-code note at `pkg/storage/edge_operations.go` above `GetEdge`:

1. `GetEdge` (shard.RLock) vs `CreateEdge`/`UpdateEdge`/`DeleteEdge` (gs.mu.Lock) — same shared-map race A4 fixed for nodes.
2. `transaction_commit.applyNodeUpdates` mutates `node.Properties` under `shard.Lock` alone, while `UpdateNode` mutates the same `node.Properties` under `gs.mu.Lock` — transaction layer doesn't coordinate with global-lock writers.
3. `DeleteNode` cascade mutates `gs.edges` / adjacency under `gs.mu.Lock` while `GetEdge` reads under `shard.RLock` — symmetric to (1).

- [ ] Apply A4's partition shape to `gs.edges` → `edgeShards [256]map[uint64]*Edge`; mirror the helpers (`lookupEdgeShard` / `storeEdgeInShard` / `deleteEdgeShardEntry` / `edgeCount` / `forEachEdgeUnlocked` / `flattenEdgesForSnapshot` / `rebucketSnapshotEdges`).
- [ ] Migrate `GetEdge`/`CreateEdge`/`UpdateEdge`/`DeleteEdge` (and `*ForTenant` variants) onto per-shard locks; same writer pattern as A4 (`gs.mu.Lock` for global indexes — `edgesByType`, tenant indexes, adjacency lists — plus `lockShard(edgeID)` for the shard mutation).
- [ ] Audit `transaction_commit.applyNodeUpdates` and `DeleteNode` cascade lock acquisition; tighten coordination so transaction-layer writes don't race with global-lock readers (the same fix that closed surface 1 for nodes).
- [ ] Add the corresponding A4-style bench (`BenchmarkGetEdge_Uniform_PureReads_4` + Legacy baseline) — same access-pattern/contention axes; throughput claim framed correctness-first per A4's reframe.
- **Acceptance**: `go test -race ./pkg/storage/... -count=3` clean (closes all three surfaces); no new race-detector findings; existing concurrent-edge tests unchanged.
- **Why now (after A4)**: the partition idiom is fresh, the helper shape is established, the bench harness is reusable. Landing the symmetric edge fix while reviewer context is hot keeps the PR cheap to read.
- **Estimated scope**: similar to A4 — ~300-400 LOC, ~15 files; either one big PR or the same three-commit split A4 used (structural / lock-grain / bench).

#### A8.1. Rebuild standalone replication on `cmd/server` (nng-only after H1)
- [ ] Spike doc: with H1 having narrowed the surface to nng + the un-prefixed `cmd/graphdb-{primary,replica}` (in-process transport), the open question is: rebuild `cmd/graphdb-nng-{primary,replica}` to share `cmd/server`'s tenant middleware stack, or delete them and document migration to `cmd/server`'s built-in replication.
- [ ] Decision recorded as a go/no-go in the spike, then 1–2 implementation PRs.
- **Acceptance**: `GRAPHDB_LEGACY_BINARY` becomes either (a) a removed env var, or (b) gates only a thin shim that delegates to `cmd/server` plumbing.

#### A8.2. Replica `/nodes` GET unauth'd cross-tenant dump
- [ ] Single PR. Add `requireAuth + withTenant` to the replica binary's `/nodes` route, or remove the route entirely if the read API is only meant to live on the primary.
- [ ] Add an audit-regression row pinning the behavior.
- **Acceptance**: Cross-tenant request to replica `/nodes` returns 401/404 (matching primary); regression test passes.

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
        ├─→ A4 ✅ ──→ A4-edges ──→ A8.2 ──┐
H3 ─────┘                                  ├─→ F1.1-spike → F1.1-impl → ┐
                                           └─→ H2 ───────────────────── ├─→ F3 → A8.1 → S1
```

**Critical path**: ~~H1~~ → ~~A4~~ → **A4-edges** → A8.2 → F1.1-spike → F1.1-impl → F3 → A8.1 → S1.

Off-path parallel work: H3 (branches) and H2 (requireAdmin) anywhere there's a small gap.

**Why this ordering**:
- **H1 first** ✅ — broken `main` builds were creating false-positive CI signal across every other PR's matrix; closed via PRs #65 + #66.
- **A4 early** ✅ — the audit's HIGH-1/HIGH-2/CRIT-1 collapsed into one operation; closed via PR #67 (with the throughput-criterion reframe documented).
- **A4-edges next** because the partition idiom and helper shape are fresh from A4; reviewer context is hot. A4-edges closes the symmetric edge-side race plus the transaction-layer coordination gap that A4 surfaced but didn't fix.
- **A4-edges before A8.2 is the same deliberate call** A4 was originally given vs A8.2: A8.2 is a security finding, but the legacy-binary gate (`GRAPHDB_LEGACY_BINARY` fail-closed, PR #47) means the exposed replica route doesn't start unless an operator opts in. Exposure is mitigated-in-depth. Meanwhile A4-edges closes a real concurrency bug class that's already manifested as `TestGraphStorage_ConcurrentDeletion` flakes. If A8.2 turns out to be reachable via a path the legacy gate doesn't cover, swap the order — the dependency graph permits it.
- **A8.2 before F1.1** because once the security debt is closed, the rest of the audit track can be considered fully retired before new feature surface lands.
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

## How to use this document

This is a planning checkpoint, not a backlog. Work below the line is grouped by sequencing-graph dependency, not priority. When picking up the next PR:

1. Read the next item on the critical path (or any unblocked off-path item).
2. If the item has a "spike" sub-task, do the spike first and **stop** before implementation — the spike's recommendation may change scope.
3. After ~3–5 PRs land, this checkpoint should itself be revisited and superseded by a `NEXT_STEPS_<DATE>.md` for the next window. Starting fresh is a legitimate option then; this document does not need to live forever.

**Revisit triggers** (any one is sufficient to start a new checkpoint immediately, not after the 3–5 PR cadence):
- **S1 spike concludes** with a "schedule for Q3 as track-banner item" recommendation — that decision re-orders the next quarter, not just the next item.
- **A8.1 spike concludes** with "rebuild on `cmd/server`" rather than "delete" — rebuild is a multi-PR sub-track that wasn't budgeted in this checkpoint.
- **A8.2 turns out to be reachable** through a path the legacy-binary gate doesn't cover — promotes immediately to head of queue.
