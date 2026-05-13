# Plan: Next Steps (graphdb) — 2026-05-14

**Predecessor**: [`NEXT_STEPS_2026-05-13.md`](./NEXT_STEPS_2026-05-13.md). This document reconciles the prior plan against current `main` (through PR #181 + handoff #180/#182) after the 2026-05-13 session closed every remaining Track C task (C3.1, C4.1, C3.2, C6-prep, C6 — PRs #175–#179) and shipped the Linux CI escalation (PR #181 dropped `ubuntu-latest` from the `test` matrix).

**Why a fresh doc**: `NEXT_STEPS_2026-05-13.md` accumulated enough strike-throughs across Track A (closed off-session pre-window) + Track C (closed in-window) + Track H Linux CI (closed end-of-window) that it became hard to read. This doc is written from the post-Track-C, post-#181 baseline.

**Sources still load-bearing**:
- Audit synthesis: [`internals/design/AUDIT_synthesis_2026-05-06.md`](./internals/design/AUDIT_synthesis_2026-05-06.md) — original-scope closure remains valid; Track C now subsumes the Cypher engine landing question.
- Bulk-stash verdict matrix: [`internals/design/AUDIT_gemini_track_claims_2026-05-13.md`](./internals/design/AUDIT_gemini_track_claims_2026-05-13.md) — Subset 🟢 / 🟡 / 🔴 split. Subset 🟢 is now fully landed via Track C; Subset 🔴 is the threat model for Track R.
- R1 spike: [`internals/design/F4_VECTOR_TENANT_REDESIGN.md`](./internals/design/F4_VECTOR_TENANT_REDESIGN.md) — recommends **Option A (per-tenant HNSW)** with a tenant-count caveat.
- R2 spike: [`internals/design/S11_AUTO_EMBEDDER_REDESIGN.md`](./internals/design/S11_AUTO_EMBEDDER_REDESIGN.md) — recommends **Option A (pluggable Embedder interface) + in-tree LSAEmbedder adapter**.

---

## State reconciliation

### Tracks closed since 2026-05-13

| Track | Closed by | Status |
|---|---|---|
| Track C — Cypher engine extraction | PRs #160–#179 across the 2026-05-12/13 session arc | ✅ All subtasks landed: C1.0, C1.1, C2, C3.0, C3.1, C3.2, C4.0, C4.1, C5.0, C5.1, C6-prep, C6. `CALL algo.shortestPath(start, end) YIELD path` executes end-to-end on real BFS. |
| Track H — Linux CI infra tax | PR #181 (matrix `test` job moved to macOS-only) | ✅ One of three escalation candidates shipped; the other two (matrix-breadth reduction, race-to-macOS) are no longer load-bearing because the matrix job has moved. |
| Decision 6 — `algo.shortestPath` storage-type wiring | Resolved as **Option B** (widen `pkg/algorithms` signatures to `storage.Storage`) — implemented in PR #178 | ✅ Other backend-agnostic algorithms should follow Option B when exposed via procedures; Option D reserved for backend-specific dispatch (e.g., native vector indexes). |

### S1 surface — still narrowed (R3 closes it)

S1 (`Storage`, `StorageReader`, `StorageWriter`) shipped at 51 of 58 method signatures in PR #145. The 6 vector `*VectorIndexForTenant` methods + `AddObserver` remain omitted, gated on Track R below.

### Carry-forward (NOT touched in the 2026-05-13 session)

11 PRs still open from prior sessions. See § Inherited PRs disposition below for per-PR recommendations.

---

## Critical path

After Track C + Track H Linux CI closure, the critical path is bounded by Track R (R1 + R2 → R3) plus housekeeping (inherited-PR triage). Track R recommendation:

1. **R1 (F4 vector ops redesign)** — Decision 2 RESOLVED via tier-based split (2026-05-14): OSS ships Option A (per-tenant HNSW); enterprise plugin holds Option B (filtered HNSW). Implementation ready to start.
2. **R2 (S11 auto-embedder redesign)** — Decision 3 RESOLVED via tier-based split (2026-05-14): OSS ships Option A (pluggable Embedder + LSA adapter); enterprise plugin holds zero-config bundled-model embedder. Implementation ready to start.
3. **R3 (S1 surface re-closure)** — sequential after R1 + R2.

**Parallel-agent disposition (NEW)**: R1 and R2 touch disjoint surfaces (`pkg/vector/*` + `pkg/storage/vector_operations.go` vs. `pkg/storage/observer_*.go` + `pkg/intelligence/embedder.go`). They are parallel-eligible via the `graphdb-coord` sibling repo skills. **Default recommendation: sequential R1 → R2 with a single agent unless the user signals interest in exercising graphdb-coord on real work.** Reason: the user has not yet validated graphdb-coord against multi-agent real work; doing it here adds a coordination-correctness risk to a redesign that already carries threat-model risk. The graphdb-coord skills exist; testing them on a smaller, lower-risk PR pair first would be cheaper insurance.

---

## Track R — Redesign work (the queue)

### R1 — F4 tenant-isolated vector ops

- [x] Spike: [`F4_VECTOR_TENANT_REDESIGN.md`](./internals/design/F4_VECTOR_TENANT_REDESIGN.md) — recommends **Option A (per-tenant HNSW)**.
- [x] **Decision 2 RESOLVED (2026-05-14, tier-based)**: OSS = Option A (per-tenant HNSW); enterprise plugin = Option B (filtered HNSW) when thousands-of-tenants-with-dense-vectors is a customer requirement. The OSS `VectorIndex` interface accommodates both implementations without an API change. See § Decision points for the full resolution.
- [ ] **Implementation**: 6 `*VectorIndexForTenant` methods. Tenant-strict empty-tenant rejection (NOT silent route to "default"; diverges from `GetNodeForTenant`'s convention — see spike §1.3 for the justification). Return `ErrNodeNotFound` for cross-tenant existence-leak prevention.
- [ ] **Files to expect**: `pkg/storage/vector_operations.go` (rewrite 6 wrappers), `pkg/vector/hnsw_index.go` (data structure change: `map[string]*HNSWIndex` → `map[tenantID]map[property]*HNSWIndex`). Tests pin cross-tenant search returns `ErrNodeNotFound`-equivalent.
- [ ] **Sub-PRs expected**: R1.0 spike → already landed. R1.1 data-structure change. R1.2 the 6 methods + tests. R1.3 (if needed) snapshot format for persisted tenant-keyed HNSW.
- **Acceptance**: cross-tenant search closed; tests cover the existence-leak channel; bench shows per-tenant search latency `O(log N_tenant × ef)`, faster than shared-index baseline for sparse tenants.

### R2 — S11 auto-embedder + NodeObserver hook

- [x] Spike: [`S11_AUTO_EMBEDDER_REDESIGN.md`](./internals/design/S11_AUTO_EMBEDDER_REDESIGN.md) — recommends **Option A (pluggable `Embedder` interface, no default) + ship an in-tree `LSAEmbedder` adapter as the canonical first-party implementation**.
- [x] **Decision 3 RESOLVED (2026-05-14, tier-based)**: OSS = Option A (pluggable Embedder interface, no default, in-tree `LSAEmbedder` as canonical first-party adapter); enterprise plugin = zero-configuration auto-embed (bundled ONNX-runtime or hosted-API embedder) registers via the same `Embedder` interface from spike §7.1, no OSS interface change required. See § Decision points for the full resolution.
- [ ] **Implementation**: `Embedder` interface (`Embed(ctx, tenantID, text) ([]float32, error)`); `NodeObserver` interface (`OnNodeCreated` / `OnNodeUpdated` / `OnNodeDeleted` with `context.Context` and no error return); bounded worker pool for async dispatch (default 4 workers, 256 queue depth, shutdown drain, synchronous test mode); `LSAEmbedder` adapter that returns `ErrNoIndexForTenant{tenantID}` when the LSA index is absent; wiring in `server_init.go` similar to the existing `BuildLSAIndex` wiring.
- [ ] **Files to expect**: `pkg/storage/observer_*.go` (new), `pkg/intelligence/embedder.go` (rewrite), `pkg/api/server_init.go` (wire `AddObserver`), possibly `pkg/intelligence/lsa_embedder.go` or `pkg/search/lsa_embedder.go` (new adapter).
- [ ] **Sub-PRs expected**: R2.1 Observer interface + bounded pool. R2.2 Embedder interface + LSA adapter. R2.3 wiring + tests.
- **Acceptance**: a known input produces a known embedding (deterministic LSA output); auto-embed runs on `CreateNode*` via Observer; latency bounded (async path doesn't block create); tenant isolation end-to-end; misconfiguration produces a typed error, never a silent mock.

### R3 — S1 surface re-closure

- [ ] After R1 + R2 land, restore the 6 vector methods + `AddObserver` to S1's `Storage` / `StorageReader` / `StorageWriter` interfaces.
- [ ] Address the `Snapshot(ctx)` signature drift — pick a final shape, migrate call sites, document.
- [ ] One PR. The S1 interface becomes complete (~58/58).
- **Acceptance**: full-surface S1; the B+Tree backend (`pkg/storage/btree_storage.go`) implements the full surface (or has documented gaps if R1/R2's design implies KV-backed-only methods).

---

## Inherited PRs disposition (NEW — 11 PRs)

These PRs have carried forward across 3 session-handoffs. The 2026-05-13 0826Z handoff §5 explicitly asked for a written disposition. Here is one, conditional on green CI per PR.

**Status from bulk check (2026-05-14)**: all 11 PRs show `mergeable: MERGEABLE` or `mergeable: UNKNOWN` (GitHub recomputation pending). No required CI checks are reported on these branches (legacy from before the matrix was tightened). Per-PR mergeability must be re-confirmed at merge time.

### Group A — H4 storage-correctness fixes (#108, #109, #110)

| PR | Title | Disposition |
|---|---|---|
| #108 | `fix(storage): rebuild per-tenant label index in WAL replay (H4.3)` | **Rebase + merge if CI green.** Functional fix; still topical. Per-tenant label index restoration on replay is correctness, not optimization. |
| #109 | `fix(api): mirror B-lite claim-uniqueness in REST POST /nodes (H4.4)` | **Rebase + merge if CI green.** Mirrors the B-lite atomic-uniqueness primitive into REST. The `:Claim`/`for_task` resolver special-case (CLAUDE.md "Parallel-agent coordination workflow") relies on this primitive. |
| #110 | `fix(storage): rebuild per-tenant label index on snapshot load (H4.3-followup)` | **Rebase + merge if CI green.** Same shape as #108 for snapshot path; landed together they fully close the H4.3 thread. |

**Risk**: these touch `pkg/storage`. Verify they don't conflict with anything that landed in the Track C arc (specifically the A4 / A4-edges partitioned shard-map idiom). Quick check: re-rebase against current main and re-run `go test -race -count=3 ./pkg/storage/`.

### Group B — Docs-only (#131)

| PR | Title | Disposition |
|---|---|---|
| #131 | `docs(skills): add coord-lesson, coord-insight, coord-dream` | **Merge if CI green.** Skills already live in `.claude/skills/` and are listed in the available-skills set; the PR is the doc capture. |

### Group C — A8.1 step-4 cleanup (#134, #138, #139, #140)

A8.1 closed off-session at the start of the 2026-05-13 window (PRs #127–#133, retiring `cmd/graphdb-primary` / `cmd/graphdb-replica` / `cmd/graphdb-nng-{primary,replica}` and the `GRAPHDB_LEGACY_BINARY` gate). These four are the doc + metrics cleanup that follows.

| PR | Title | Disposition |
|---|---|---|
| #134 | `docs: delete legacy UPGRADE_GUIDE.md (A8.1 step 4a)` | **Merge if CI green.** Pure deletion; the guide describes a deleted orchestration flow. |
| #138 | `docs: rewrite PRODUCTION_QUICKSTART for single-node cmd/server (A8.1 step 4b)` | **Rebase + merge if CI green.** Important user-facing doc; verify it doesn't contradict the README "Scalability & Limitations" section landed in #146. |
| #139 | `docs: update legacy-binary references after A8.1 (step 4c)` | **Merge if CI green.** Mechanical reference update. |
| #140 | `refactor(metrics): delete replication-metric orphans (A8.1 step 4d)` | **Rebase + merge if CI green.** Code change (not docs). Removes metrics that no longer have producers. Risk: low — the metrics are dead code, deleting them is safe; verify no dashboard references them. |

### Group D — LSA search improvements (#135, #136, #137 — STACKED)

The LSA stack is **PR #135 → #136 → #137** (each branch is based on its predecessor's branch). PR #136 carries an `A2` tag in its commit message; PR #137 carries a `C1` tag. **These tags collide with the new Track A / Track C semantics** (A=tenancy isolation, C=Cypher engine extraction) — a code review reader hitting `git log` after merge would be confused.

| PR | Title | Disposition |
|---|---|---|
| #135 | `feat(search): persist per-tenant LSA indexes to disk (B1)` | **Rebase + merge if CI green** (base = `main`). **Before merging**: per CLAUDE.md "Stacked-PR --delete-branch gotcha", retarget #136's base to `main` BEFORE merging #135, OR merge #135 without `--delete-branch` and clean the stale branch later. Either avoids GitHub auto-closing #136. |
| #136 | `feat(search): switch LSA term weighting to log-entropy (A2)` | **Rebase + retag commit subject** to remove the `A2` collision (suggest: `feat(search): switch LSA term weighting to log-entropy`); merge if CI green. Same stacked-merge discipline applies to #137. |
| #137 | `feat(search): quantize LSA doc vectors to int8 (C1)` | **Rebase + retag commit subject** to remove the `C1` collision (suggest: `feat(search): quantize LSA doc vectors to int8`); merge if CI green. |

**Stacked-merge order** (per CLAUDE.md):
1. `gh pr edit 136 --base main` (retarget before parent merges).
2. `gh pr merge 135 --squash` (no `--delete-branch`).
3. Rebase #136 onto current main; force-push.
4. `gh pr edit 137 --base main`.
5. `gh pr merge 136 --squash`.
6. Rebase #137 onto current main; force-push.
7. `gh pr merge 137 --squash --delete-branch` (last one; safe to delete-branch since no dependents).
8. Local cleanup: `git branch -D feat/lsa-persistence feat/lsa-bigrams-logentropy feat/lsa-quantize-docvecs`.

**Alternative**: if the LSA stack is no longer load-bearing (i.e., the user has decided LSA-as-default-embedder will be replaced by R2's pluggable architecture and these improvements don't compose with R2), close all three with a comment pointing at the R2 spike. **Recommendation**: keep them merged — R2's `LSAEmbedder` adapter is the natural consumer of #136 (log-entropy weighting) and #137 (int8 quantization). Persisting LSA indexes to disk (#135) is also useful for R2 because the adapter needs to load a tenant's LSA index at observer-startup time.

### Bulk merge command (after retags + rebases)

```bash
for pr in 131 134 138 139 140 108 110 109; do
  gh pr checks $pr && gh pr merge $pr --squash --delete-branch
done
# LSA stack: handled separately per the stacked-merge order above.
```

Run after rebasing each onto current main. The `gh pr checks` gate prevents merging an empirically-red PR even if `mergeStateStatus: UNSTABLE` is the normal state in this repo.

---

## Off-path (independent, opportunistic)

These do not block Track R. Pick up if there is bandwidth between R1/R2/R3 sub-PRs.

### Track C tail (deferred from 2026-05-13 session)

The 0805Z handoff §5 listed four follow-ups deferred from Track C. They are not blocking and can be picked up opportunistically:

1. **Planner-level unit tests for `CALL → CallOperator` emission** — deferred from #176.
2. **CallOperator-level unit tests** — deferred from #175 (`TestShortestPathProcedure_HappyPath` in #179 covers the procedure-side but not the operator-stack side).
3. **CallOperator integration test** — exercises the full operator stack (planner → operator → registry → result) end-to-end.
4. **`pkg/algorithms` uniform widening** — PR #178 widened only `shortest_path.go`. Other algorithm files (centrality, pagerank, triangles, scc, topology, cycle_detection, link_prediction, node_similarity, khop, community_*) use the same `*storage.GraphStorage` pattern. Mechanical sweep PR, ~30 signature changes; same Decision-6=Option-B logic. Worth doing **when another algorithm gets exposed as a procedure**, not before.

### CLAUDE.md stale-bullet cleanup (single doc-update PR)

The 0826Z handoff §6 identified three stale or partially-stale CLAUDE.md bullets after PR #181 (matrix-test moved to macOS-only):

1. § "Known infra patterns" — Linux exit-143 bullet is now **partial** (matrix `test` job no longer hits it; non-matrix Linux jobs theoretically still could but historically don't). Replace with the 0826Z handoff's suggested wording.
2. § "Known infra patterns" — `mergeStateStatus: UNSTABLE` bullet — cause set shrunk to "benchmark comment-step permissions only" post-#181. Update accordingly.
3. User-private memory `project_ci_red_state_tolerated.md` — needs an update from "matrix-test red state is tolerated" to "matrix-test red state should NOT happen now; if it does, that's net-new and worth investigating."

**Single-file PR** (CLAUDE.md only); user-private memory is a separate, agent-side update.

### OAuth account-rename bullet (only if it bites again)

0826Z handoff §6 captured the `darragh-downey` → `dd0wney` GitHub-rename context that surfaced when pushing the macOS-only CI branch. Recipe in the handoff. **Don't add to CLAUDE.md yet** — it hasn't bitten three times. If it does, add a single-line bullet to § "Known pitfalls" with the `gh auth logout` + `gh auth login -s workflow` fix.

---

## Sequencing graph

```
R1 spike ✅ ─→ Decision 2 ✅ (tier-split) ─→ R1.1 (data-struct) ─→ R1.2 (6 methods + tests) ─┐
                                                                                              ├─→ R3 (S1 close)
R2 spike ✅ ─→ Decision 3 ✅ (tier-split) ─→ R2.1 (Observer + pool) ─→ R2.2 (Embedder + LSA) ┘
                                                                    └─→ R2.3 (wire + tests)

Off-path (parallel-eligible, NOT blocking R-track):
  Inherited PRs (#108-#140) — see § Inherited PRs disposition
  C-track tail (4 items) — see § Off-path
  CLAUDE.md stale-bullet cleanup — single doc-update PR
```

**Critical path**: R1 (3 sub-PRs) → R2 (3 sub-PRs) → R3 (1 PR). Decisions 2 and 3 resolved 2026-05-14 (tier-based split). **Capacity estimate** at ~2–3 PRs/week: 7 PRs total ≈ 3–4 weeks if sequential. Inherited-PR triage adds another ~1 session of work; the CLAUDE.md cleanup is a single PR.

---

## Decision points (current)

These need user weigh-in before implementation, not just inheritance from the prior planning doc.

### Decision 2 — R1 (F4): per-tenant HNSW (Option A) — RESOLVED (2026-05-14, tier-based)

User framing (2026-05-14): **"it will depend on whether its an enterprise or not."** Resolution maps to the open-core split (CLAUDE.md § "Open-core: a sibling private repo exists"):

- **OSS (this repo)**: ship **Option A — per-tenant HNSW** as the in-tree implementation. Fits the operator-managed, low-hundreds-tenant baseline. Memory footprint (~3.2 GB at 100 tenants × 10k vectors × 768 dims) is within typical cloud-instance budgets.
- **Enterprise (`graphdb-enterprise` `.so` plugin)**: the filtered-HNSW (Option B, B1 variant) is an enterprise-plugin extension when thousands-of-tenants-with-dense-vectors deployment is a customer requirement. The OSS `VectorIndex` interface must accommodate the replacement without an API change — the spike's per-tenant `map[tenantID]map[property]*HNSWIndex` is already a pluggable shape.

**What this means for R1.1**: the OSS data-structure change is the per-tenant map. The interface signatures in F4 spike §6 are the contract; both the OSS implementation and any future enterprise filtered-HNSW plugin satisfy the same contract.

### Decision 3 — R2 (S11): pluggable Embedder + LSA adapter (Option A) — RESOLVED (2026-05-14, tier-based)

Same tier-based framing as Decision 2:

- **OSS (this repo)**: ship **Option A — pluggable `Embedder` interface (no default), in-tree `LSAEmbedder` as the first NodeObserver implementation**. The operator builds the per-tenant LSA corpus before auto-embed produces results; `LSAEmbedder` returns `ErrNoIndexForTenant{tenantID}` until they do.
- **Enterprise (`graphdb-enterprise` `.so` plugin)**: the zero-configuration auto-embed experience ("create a node, get a vector, no admin steps") is an enterprise-plugin extension. Implementation candidates: bundled ONNX-runtime embedder, hosted-API `RemoteAPIEmbedder` adapter, or a model-package distribution mechanism. **All satisfy the same `Embedder` interface from spike §7.1** — no OSS interface change required.

**What this means for R2.1/R2.2**: the OSS pluggable interface is the contract; `LSAEmbedder` is the canonical in-tree adapter. Enterprise plugins register additional `Embedder` implementations at startup via the same `AddObserver` mechanism.

### Decision 5 — GNN procedure (S6): defer indefinitely — UNCHANGED

`pkg/gnn` does not exist on OSS main; archive's `procedures.go` would have registered `gnn.messagePass` but the import is unresolved here. Stays deferred unless a customer drives it.

### Decision 7 (NEW) — Parallel-agent coordination for R1+R2

**Default recommendation**: sequential R1 → R2 with a single agent. The `graphdb-coord` sibling repo skills exist; testing them here adds coordination risk on top of redesign risk. Decision belongs to user — the option remains open.

### Decision 8 (NEW) — Inherited-PR disposition

The default in § Inherited PRs disposition is "rebase + merge if green" for all 11. The user can override with "close all 11" or "park indefinitely" (`gh pr edit --add-label parked`) per the 0805Z handoff §7.3.

---

## Carry-forward decision points (still open from prior plans)

1. **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` is synchronous. SSE/WebSocket streaming is a future enhancement; not a launch question. Still open.

---

## Risks specific to this window

- **OSS / enterprise interface contract is load-bearing**: Decisions 2 + 3 resolved tier-based — OSS ships Option A; enterprise plugins extend. The OSS interfaces (`VectorIndex`, `Embedder`, `NodeObserver`) must remain stable enough that an enterprise `.so` plugin can swap in Option B for R1 (filtered HNSW backend) or a bundled-model embedder for R2 without an interface change. Treat the spike-doc signatures (F4 §6, S11 §7) as the contract, not as one tier's implementation detail. If R1/R2 implementation surfaces a signature change, evaluate against both tier consumers, not just OSS.
- **R1 data-structure change is the load-bearing PR.** Once `VectorIndex` switches from `map[string]*HNSWIndex` to `map[tenantID]map[property]*HNSWIndex`, every consumer (`UpdateNodeVectorIndexes`, `RemoveNodeFromVectorIndexes`, snapshot/replay paths) must route through `node.TenantID`. Single-PR shape; review surface is wide. Expect at least one fix-forward PR.
- **R2 Observer dispatch placement is correctness-critical.** Spike §7.4 requires the observer dispatch to run AFTER all shard and global mutex locks are released, NOT inside them. The archive's pattern (RLock over the slice copy, dispatch under that lock) is wrong and must be fixed structurally in R2.1, not deferred.
- **Inherited-PR rebases may surface drift.** The 11 PRs sat across the Track C arc (#160–#179). Group A (H4 storage fixes) touches `pkg/storage`, which absorbed most of the partitioned-shard-map churn. Group C (A8.1 step 4) touches docs and `pkg/metrics`. Expect at least one merge conflict per group at rebase time.
- **Bundle risk for the LSA stack (#135–#137)**: the stacked-merge order above relies on careful base-retargeting. Mis-merging triggers the GitHub auto-close-of-dependents pitfall (CLAUDE.md § "Known pitfalls") that bit the 2026-05-13 session twice. Follow the 8-step recipe above exactly.

---

## Out of scope (carry-forward + new)

- **GQL / non-Cypher query languages** — defer.
- **Geospatial / temporal data-model features** — still deferred; no new triggering signal.
- **Performance tracks B2/B3/B4** — opportunistic only.
- **Code-quality May-10-lettering tracks (C1/C2/C3/C4)** — opportunistic. Naming collision with this doc's Track A/C/R is documented in the LSA #136/#137 retag note above.
- **Mobile / `gomobile` / `pkg/mobile`** — Syntopica-v2 ruled out; unchanged.
- **S6 GNN as native kernel** — defer unless customer-driven.
- **S10b multi-statement ACID transactions** — Subset 🔴; redesign is multi-quarter; deferred indefinitely.
- **`-tags zmq` replication variant** — deleted (PR #65). Stays deleted; nng-only.
- **Bundled ONNX-runtime embedding model** — out of scope for R2; would be its own track if zero-config auto-embed becomes a launch requirement.

---

## Known limitations + productization gaps (delta from 2026-05-13 doc)

- **Single-node ceiling — documented**: README "Scalability & Limitations" closed this in PR #146. Unchanged.
- **Storage interface extraction (S1)**: partially closed by PR #145 at 51/58 methods. **R3 closes the remaining 7** (6 vector methods + AddObserver). Tied to R1 + R2.
- **Linux CI infra tax**: closed for matrix-test job by PR #181 (macOS-only). Non-matrix Linux jobs theoretically still vulnerable; if `coverage` / `benchmarks` / `build` / `tagged-build-nng` start showing exit-143, escalate. Unmonitored does not mean fixed.
- **Auto-embedder + vector tenancy facade risk**: the original gemini-bulk archive's mock embedder and silent tenant-fallback are documented in the spike docs. **R1 + R2 implementations are the structural prevention**. Until they ship, do not register the archive's `pkg/intelligence/embedder.go` or import the archive's `vector_operations.go` wrappers — they fail-silent in production.

---

## How to use this document

This is a planning checkpoint, not a backlog. When picking up the next PR:

1. **Decisions 2 + 3 are resolved (tier-based, 2026-05-14)** — see § Decision points. R1 and R2 implementations can begin without user re-confirmation. Treat the spike-doc interface signatures (F4 §6, S11 §7) as the OSS contract that enterprise plugins also satisfy.
2. If picking R1: re-read the spike doc § 6 (Final Method Signatures) before writing.
3. If picking R2: re-read the spike doc § 7 (Final Interface Signatures) before writing.
4. If picking inherited-PR triage: § Inherited PRs disposition has the recipe; the LSA stack needs the 8-step careful-merge order.
5. **After ~3–5 PRs land, this checkpoint should itself be revisited and superseded** by `NEXT_STEPS_<DATE>.md` for the next window.

**Revisit triggers** (any one is sufficient to start a new checkpoint immediately, not after the 3–5 PR cadence):
- **R1 implementation surfaces a `pkg/vector` design issue not anticipated by the spike** — e.g., HNSW persistence-format constraints that would require a `VectorIndex` interface change (which would also affect the enterprise-plugin extension point).
- **R2 implementation surfaces an `LSAEmbedder` dimension-mismatch issue** with existing HNSW indexes — the spike anticipates this but the resolution may change scope.
- **A customer-driven priority lands on the queue** — re-plan in the customer's terms.
- **The inherited-PR triage closes** — that resolves a 3-session carry-forward; re-evaluating the remaining queue with that decking cleared may shift priorities.

---

## Appendix — file-level validation discipline

The 2026-05-13 session's most useful insight (captured in the 0805Z handoff §6): **planning-doc sequencing claims should be cross-checked against file contents before acting**. The prior doc claimed "C4.1 first" was the right order; file-level analysis showed C4.1's q.Call block references `CallOperator{}` which doesn't exist on main without C3.1 landing first. Correct order was C3.1 → C4.1 → C6-prep → C6.

This discipline applies double for Track R, which is **redesign** not extraction: the spike docs describe a target shape, but the implementation must verify each cited symbol exists, each cited file path is correct, and each cited error type is reachable. Specific things to verify before each Track R sub-PR:

- **R1.1**: `pkg/vector/hnsw_index.go` currently holds `indexes map[string]*HNSWIndex` — confirm before refactoring.
- **R1.2**: the spike's "ErrNodeNotFound for cross-tenant" relies on `errors.Is(err, ErrNodeNotFound)` behaving on a wrapped error — confirm by reading the existing `*ForTenant` error-wrapping pattern in `pkg/storage/node_operations.go`.
- **R2.1**: spike §7.4 says observer dispatch runs AFTER mutex release — confirm by reading the current `pkg/storage/node_operations.go:CreateNodeForTenant` lock pattern (`gs.mu.Lock` + per-shard `lockShard`).
- **R2.2**: the LSA adapter references `TenantLSAIndexes.Get(tenantID)` — confirm this exists in `pkg/search` (or wherever LSA indexes live post-#135) before writing the adapter.

Don't trust the spike doc to be a literal blueprint; treat it as a target shape and verify against the code.
