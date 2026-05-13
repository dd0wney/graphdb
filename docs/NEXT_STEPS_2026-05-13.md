# Plan: Next Steps (graphdb) — 2026-05-13

**Predecessor**: [`NEXT_STEPS_2026-05-10.md`](./NEXT_STEPS_2026-05-10.md). This document reconciles that plan against current `main` (through PR #153 + handoff #154) after the 2026-05-12/13 session's A8.1 closure (off-session, PRs #127–#133), Gemini-bulk-stash triage (Phase 1: #141, #144, #145, #146, #147), and `pkg/updater` redesign (Phase 2: #149–#153).

**Sources still load-bearing**:
- Audit synthesis: [`internals/design/AUDIT_synthesis_2026-05-06.md`](./internals/design/AUDIT_synthesis_2026-05-06.md) — five cross-cutting findings; original-scope closure remains valid.
- Killer-features synthesis: [`internals/design/FEATURES_synthesis_2026-05-08.md`](./internals/design/FEATURES_synthesis_2026-05-08.md) — F1/F2/F3 closed; storage-interface unlock thesis now partly realized via S1.
- **Bulk-stash verdict matrix**: [`internals/design/AUDIT_gemini_track_claims_2026-05-13.md`](./internals/design/AUDIT_gemini_track_claims_2026-05-13.md) — Subset 🟢 / 🟡 / 🔴 split; the keystone doc for Track C below.
- **pkg/updater threat model**: [`internals/design/AUDIT_pkg_updater_2026-05-13.md`](./internals/design/AUDIT_pkg_updater_2026-05-13.md) — Path C selected and shipped; doc remains the threat model for any future updater work.
- Archive branch: `origin/archive/gemini-bulk-2026-05-13`. The git-stash structure has three parents; Subset 🟢 files (Cypher engine, B+Tree) live at **parent `^3`** (`d9417a9`), not the bare ref. Extraction commands in Track C below assume `^3`.

---

## State reconciliation

### Track A — Audit / tenancy isolation ✅ **CLOSED (A8.1 done off-session 2026-05-12)**

A8.1 closed across four PRs on 2026-05-12 (pre-this-session), retiring the legacy primary/replica binaries and the multi-node orchestrator that A8 had only fail-closed-gated:

| PR | Title | Step |
|---|---|---|
| #127 | `docs(spike): A8.1 — replication standalone-binary disposition` | Spike — selected "delete legacy binaries, keep `cmd/server`'s in-process replication" over rebuild. |
| #129 | `refactor(admin): delete pkg/admin/upgrade* orphan (A8.1 step 1)` | Removed dead admin-CLI plumbing for the orchestrated upgrade flow. |
| #130 | `refactor(cmd): delete legacy replication binaries (A8.1 step 2)` | Removed `cmd/graphdb-primary`, `cmd/graphdb-replica`, `cmd/graphdb-nng-{primary,replica}` and the `GRAPHDB_LEGACY_BINARY` gate. |
| #133 | `refactor(replication): lift apply gate to pkg/wal/apply, delete the rest (A8.1 step 3)` | Salvaged the WAL-apply gate; deleted the rest of `pkg/replication` orchestration. |

**Acceptance met**: `GRAPHDB_LEGACY_BINARY` no longer exists; the in-process replication wired to `cmd/server`'s tenant middleware is the only path. Re-validates the "single-node by design" framing the README's Scalability & Limitations section (PR #146) now states publicly.

### S1 — Storage interface extraction 🟡 **NARROWED LANDING (PR #145)**

May-10's plan scheduled S1 as a *spike*; this session landed the *implementation* — narrowed:

- **Landed**: 51 of 58 method signatures from the Gemini bulk-stash interface (`Storage`, `StorageReader`, `StorageWriter`). Tenant-aware throughout (codifies the `*ForTenant` convention at the contract layer). PR #145.
- **Deliberately omitted**: the 6 vector `*ForTenant` methods (F4-coupled), the `AddObserver` NodeObserver hook (S11-coupled), and the `Snapshot(ctx)` drift. Reason: those surfaces require redesign, not just extraction — see Track R below.

**What this means for the plan**: S1 is no longer the *end* of the queue. It's the *middle*. Track C consumes it; Track R expands it; full-surface S1 closure rolls into the next planning checkpoint (after Track R lands).

### Phase 1 bulk-stash triage (this session, 2026-05-12/13)

| PR | Title | Disposition |
|---|---|---|
| #141 | `docs: reorg internal docs into docs/internals/` | Mechanical, 101 renames + 16 reference updates. |
| #144 | `docs(audit): score Gemini 2026-05-12 track-closure claims against substance` | 19-row verdict matrix. Now the keystone for Track C. |
| #145 | `refactor(storage): extract Storage/StorageReader/StorageWriter (S1, narrowed)` | See above. |
| #146 | `docs: add Scalability & Limitations section to README` | The honest part of the bulk-stash README rewrite. Closes May-10 "single-node ceiling — silent vs. documented" gap. |
| #147 | `feat(cmd): add import-dimacs + integration-test dev binaries` | DIMACS road-network importer + Phase-2 storage exerciser. First external consumers of S1. |

### Phase 2 pkg/updater redesign (Path C, this session)

| PR | Title | Notes |
|---|---|---|
| #149 | `docs(audit): score pkg/updater/ substance against single-node graphdb needs` | Threat model. Path C selected. |
| #150 | `feat(updater): redesign pkg/updater with security + correctness fixes` | `VerifyChecksum` wired, `golang.org/x/mod/semver`, `Version` injected via `-ldflags`, 22 test cases covering audit issues 1–4. Race-clean. |
| #151 | `feat(graphdb-admin): add `update` subcommand using new pkg/updater` | Bridges `main.Version` → `updater.Version`. |
| #152 | `feat(api): HTTP update endpoints with proper job/status tracking` | `/admin/update/check`, `/admin/update/apply` (202 + job ID), `/admin/update/jobs/{id}` (poll). Replaces audit issue 5 (fire-and-forget + `os.Exit(0)`). |
| #153 | `refactor(cmd): delete cmd/graphdb-upgrade — replaced by graphdb-admin update` | Removes 393-LOC dead orchestrator post-A8.1. |

**Acceptance met**: the threat model in `AUDIT_pkg_updater_2026-05-13.md` is closed at all five issues; `cmd/graphdb-upgrade` is gone; the new updater is testable end-to-end via the HTTP job/status API. The threat-model doc stays in-repo as a permanent reference for future updater changes.

---

## The next ~6 weeks

Capacity assumption: **~2–3 PRs/week** (per May-10 calibration). 6 weeks ≈ 12–18 PRs. Plan below totals ~10 PRs, leaving slack for the Cypher engine's surgical-extraction surprises (each Subset 🟢 file may surface drift from current `main`).

**Why 6 weeks, not 90 days**: Track C is the dominant scope-driver and is bounded by the archive's contents, not the calendar. Once Track C + Track R close, the next checkpoint should write `NEXT_STEPS_<DATE>.md` reflecting whatever the empirically-discovered next problem is — pre-committing 90 days now risks reading wrong before the Cypher engine's integration cost is known.

Sequencing principles carried forward:
- One logical commit per task; one PR per task.
- Spike → ~4 PRs → audit-regression-row pattern for any new sub-track touching tenant-scoped code paths.
- **Surgical extraction discipline (NEW)**: for Track C, every PR explicitly `git checkout origin/archive/gemini-bulk-2026-05-13^3 -- <path>` for the specific file(s), then manual review. Do NOT bulk-apply the branch — that would drag in Subset 🔴 (mockEmbedding, "ACID" without isolation, tenant-strict-violating wrappers).

### Track C — Cypher engine extraction (NEW)

Subset 🟢 from `AUDIT_gemini_track_claims_2026-05-13.md` lands as a series of atomic PRs from the archive. Six PRs proposed; order chosen for **decreasing isolation** and **increasing integration risk**:

#### C1. `pkg/btree/{node,pager,tree}.go` — B+Tree primitive

Split into C1.0 + C1.1 after archive inspection (2026-05-13) found three issues that prevent a clean lift-and-shift: archive contains zero btree-level tests (only integration tests in `pkg/storage/btree_storage_test.go`, which is C2 scope); `Tree.Delete` is a stub `Put(key, nil)` with `// for spike` comment; and `isNodeFull` uses literal `20` for max keys per node.

##### C1.0 — Lift-and-shift extraction (this PR)

- [ ] Extract `pkg/btree/node.go`, `pager.go`, `tree.go` from `origin/archive/gemini-bulk-2026-05-13^3` (~649 LOC).
- [ ] Apply `gofmt` (archive files have stale formatting).
- [ ] Fix two staticcheck S1009 nil-checks (`val == nil || len(val) == 0` → `len(val) == 0`).
- [ ] Add `TODO(C1.1)` comments documenting the three deferred issues (no tests, stub Delete, magic-20).
- [ ] No consumers yet — pure new package; verified zero coupling (stdlib-only imports).
- **Acceptance**: package builds, `go vet` clean, `golangci-lint run ./pkg/btree/...` clean. **No test acceptance** — `pkg/btree/` ships without unit tests in this PR; deferred to C1.1.

##### C1.1 — Tests + Delete contract + named constant ✅ **DONE (this PR)**

- [x] Write btree-level unit tests: `Put`/`Get` round-trip; `Delete` semantics (whether tombstone or real removal); cursor `Next()` skipping zero-length values; pager close+reopen persistence; split/merge boundary conditions. (10 tests in `tree_test.go`, 3 in `node_test.go`.)
- [x] Decide `Delete` behavior: kept tombstone semantics, documented in `Delete` docstring as the intended contract. Compaction TODO added in `pager.go` for the higher-layer concern.
- [x] Replace literal `20` with named `maxKeysPerNode` constant and add a comment explaining the heuristic derivation.
- [x] **Bonus correctness fix surfaced by tests**: `findLeaf` and `insertNonFull` were using `findKey` (`>=`) for internal-node descent, but the leaf-split convention (where `splitKey` lives in the *right* leaf) requires strict `>` for child-selection. Added `findChild` and routed both navigations through it. Without the fix, queries for keys exactly at a split boundary returned not-found.
- **Acceptance met**: btree-level tests pass under `-race -count=3`; `Delete` behavior is documented as tombstone; no magic numbers.

#### C2. `pkg/storage/btree_storage.go` — B+Tree as a `Storage` backend

- [ ] Extract `pkg/storage/btree_storage.go` + `btree_storage_test.go` + `btree_bench_test.go` (~818 LOC + ~200 LOC tests).
- [ ] **First external consumer of S1's `Storage` interface beyond `*GraphStorage`.** Validates the interface holds against a second backend.
- [ ] May need fixups: S1's narrowing omitted vector methods and `AddObserver`; B+Tree backend may require either trimmed-interface satisfaction or stub implementations of the omitted methods. Decide PR-locally; document the choice.
- **Acceptance**: backend satisfies S1's `Storage` interface (or trimmed variant); tests pass; bench tests run.

#### C3. `pkg/query/physical_plan.go` — Volcano operators

- [ ] Extract `physical_plan.go` (~1233 LOC) defining 17 operators (NodeScan, IndexSeek, Expand, Filter, Project, Call, Create, Set, Delete, Remove, Merge, Unwind, Union, OptionalMatch, Aggregate, NestedLoopJoin, HashJoin).
- [ ] **May need to split** if the LOC count makes review impractical. Suggested split axis: scan/index ops in one PR, mutation ops (Create/Set/Delete/Remove/Merge) in a second, join/aggregate ops in a third.
- [ ] **Carries S7 OTEL spans per operator** — per the audit verdict matrix, the per-operator span wiring is real. Land alongside the operators (don't strip it).
- **Acceptance**: operators implement the existing physical-operator interface; tests pass; OTEL spans visible in `pkg/telemetry/` exporter integration test.

#### C4. `pkg/query/planner.go` — logical→physical mapping

- [ ] Extract `planner.go` (~329 LOC). Consumes C3's operators.
- **Acceptance**: planner emits valid physical plans for the Cypher AST shapes C3 supports; tests pass.

#### C5. Cypher parser additions — CALL / CREATE / SET / DELETE / REMOVE / MERGE

- [ ] Extract the parser-clauses additions for the six mutation/procedure verbs from the archive's `pkg/query/parser_clauses.go` diff.
- [ ] **Tied to C3's `CreateOperator` / `SetOperator` / `DeleteOperator` / `RemoveOperator` / `MergeOperator` / `CallOperator`** — land after C3 so the operators exist to wire to.
- **Acceptance**: cypher_spike_test cases (extracted from `pkg/query/cypher_spike_test.go`) pass end-to-end through parser → planner → executor.

#### C6. `pkg/query/procedures.go` — procedure registry

- [ ] Extract `procedures.go` (~102 LOC) defining the `procedureRegistry` with three entries.
- [ ] **DO NOT carry the `algo.shortestPath` stub.** The archive's version has `// Stub for now - in real life this calls pkg/algorithms` and returns fake `[srcID, dstID]` path data. Replace with a real wire-up to `pkg/algorithms` shortest-path. Single-tenant-scoping must round-trip through the existing `*ForTenant` algorithm signatures.
- [ ] **DO NOT carry the `gnn.messagePass` procedure** unless S6 redesign (Track R) is done first — the archive's `pkg/gnn` is spike-quality (BFS-as-message-pass) and shouldn't reach users via Cypher.
- [ ] **Audit `llm.generate` for the mock-fallback issue** flagged in `AUDIT_gemini_track_claims_2026-05-13.md` (Subset 🟡, `pkg/intelligence/llm.go:28`). The handler silently returns `[MOCK RESPONSE...]` when `APIKey == ""`. Either gate the procedure on a populated key (return an error, don't mock), or drop the procedure for now.
- **Acceptance**: procedure registry has only real implementations; mock fallbacks return errors instead of silently lying.

### Track R — Redesign work (NEW)

The bulk-stash had usable architectural skeletons for three features whose *outputs* were facade-quality (Subset 🔴 in the audit). The next planning window should redesign each from first principles using the audit doc as threat model — same pattern Path C used for `pkg/updater`.

#### R1. F4 — Tenant-isolated vector ops (redesign)

- [x] Spike doc: [`docs/internals/design/F4_VECTOR_TENANT_REDESIGN.md`](./internals/design/F4_VECTOR_TENANT_REDESIGN.md) **(landed this session)**. The archive's wrappers had `if tenantID == "" { tenantID = "default" }` — violates tenant-strict semantics (CLAUDE.md "Tenant scoping" + `pkg/storage/node_operations.go:GetNodeForTenant` canonical pattern). The redesign must return `ErrNodeNotFound` for empty/unknown tenant, not silently route to "default."
- [ ] Decide: per-tenant HNSW index (full isolation, higher memory) vs. shared index with tenant-keyed filter at search time (lower memory, tenant-tagged vectors). Trade-off question for the spike.
- [ ] Implementation: 6 `*VectorIndexForTenant` methods that match the existing `*ForTenant` error-shape convention.
- [ ] Wires the 6 omitted methods back into the S1 `Storage` interface.
- **Acceptance**: cross-tenant vector search returns `ErrNodeNotFound`-equivalent; tests pin the existence-leak channel closed; bench shows the chosen design's perf characteristics.

#### R2. S11 — Auto-embedder + NodeObserver hook (redesign)

- [x] Spike doc: [`docs/internals/design/S11_AUTO_EMBEDDER_REDESIGN.md`](./internals/design/S11_AUTO_EMBEDDER_REDESIGN.md) **(landed this session)**. The archive's `pkg/intelligence/embedder.go` returns `[float32(len(content))/100, ...]` — a 3-float mock. Self-comment: `// Mock embedding for spike`.
- [ ] Decide: which embedder backend ships as default — pluggable interface with no default? Existing LSA (deterministic, in-tree)? External API (OpenAI / Anthropic-compat via the existing `/v1/embeddings` shape)?
- [ ] Implementation: real embedder + real NodeObserver wiring. The Observer pattern in the archive is reasonable; the *embedding* is the only thing to replace.
- [ ] Wires `AddObserver` back into the S1 `Storage` interface.
- **Acceptance**: embeddings are real (a known input produces a known embedding); auto-embed runs on `CreateNode` via Observer; latency is bounded (async path doesn't block the create); tenant isolation holds end-to-end.

#### R3. S1 surface re-closure

- [ ] After R1 + R2 land, restore the 6 vector methods + `AddObserver` to S1's interface set.
- [ ] Address the `Snapshot(ctx)` signature drift — pick a final shape, migrate call sites, document.
- [ ] One PR. The S1 interface becomes complete.
- **Acceptance**: 58/58 originally-declared methods on `Storage` (or whatever the post-R1/R2 final count is); no remaining `*ForTenant` gaps; the C2 B+Tree backend (or a successor) implements the full surface.

### Track H — Housekeeping (carry-forward)

#### H5. Fold stacked-PR `--delete-branch` gotcha into in-repo CLAUDE.md ✅ **DONE (this PR)**

- [x] The user-private memory `feedback_stacked_pr_delete_branch_gotcha.md` captures a pitfall hit twice this session: merging a stack-bottom PR with `--delete-branch` auto-closes dependent PRs that GitHub then refuses to reopen. Retarget the base before merging parent, or skip `--delete-branch` on non-leaf PRs.
- [x] Single-file PR adding to CLAUDE.md § "Known pitfalls". Makes it apply on any machine/user.
- **Acceptance**: CLAUDE.md updated; user-private memory can stay (or be retired) at the user's option.

#### Linux CI infra tax (carry-forward from May-10)

- [ ] May-10 §"Known limitations" item still open. `make test-race` consistently exits 143 on Linux runners. Two structural fixes: split race target across packages, or bump the runner timeout in `.github/workflows/`. Single small PR either way.
- [ ] Worth doing **before** Track C starts — Subset 🟢 PRs will each carry race-test runs, and noisy red checks would obscure real failures.

---

## Sequencing graph

```
A8.1 ✅ ──→ S1 (narrowed) ✅ ──→ Linux CI ─→ C1.0 (btree extract) ─→ C1.1 (btree tests + Delete) ─→ C2 (btree_storage) ──┐
                                                                                                                          ├─→ R1 (F4 redesign) ──┐
Path C ✅ (#149-#153) ──→ R2 (S11 redesign) ───────────────────────────────────────────────────────────────────────────  ┤                       ├─→ R3 (S1 close)
                                                                                                                          └─→ C3-C6 (cypher) ────┘
```

**Critical path**: ~~A8.1~~ → ~~S1 narrowed~~ → Linux CI → **C1.0 → C1.1 → C2** → C3..C6 (Cypher engine) → R1 + R2 (parallel) → R3 (S1 closure).

Off-path: ~~H5 (CLAUDE.md fold)~~ ✅ closed by #160.

**Why this ordering**:
- **Linux CI before Track C** — Track C is 6 PRs each carrying race tests; noisy infra failures would muddy review signal. The May-10 plan already flagged this gap.
- **C1 split into C1.0 + C1.1** — archive inspection found the btree files have no btree-level tests, a stub `Delete`, and a magic `20`. C1.0 lifts the files cleanly with TODOs marking the gaps; C1.1 closes them. Splitting keeps each PR's review surface bounded and lets C2 wait on C1.1's test surface rather than C1.0's untested extraction.
- **C1 before C2 (btree_storage)** — btree is a pure new package, zero coupling. Lowest-risk way to validate the extraction methodology before we add an S1-interface integration in C2.
- **C2 before C3..C6** — C2 is the first external consumer of S1. If S1's narrowing was wrong somewhere, C2 will surface it before the larger Cypher engine drops in.
- **C3..C6 before R1/R2** — the Cypher engine wires to S1 as-narrowed (which excludes the F4 and S11 surfaces). Landing Cypher first proves the narrowed S1 is sufficient for query work, then R1/R2 expand for vector/observer work without query-engine coupling.
- **R1 and R2 parallel** — they touch disjoint surfaces (vector ops vs. observer hooks). Two agents (or one agent across two work sessions) can hold them.
- **R3 last** — the S1 closure depends on R1 + R2 having shipped their respective method sets.

---

## Decision points

These are explicit questions the user should weigh in on rather than decisions baked into the plan.

1. **C3 split or single PR?** `physical_plan.go` is ~1233 LOC. Single PR is faster but heavy review; 3-way split (scan/index, mutation, join/aggregate) is reviewable but slower. Recommendation: start as single PR; split if review surfaces it.
2. **R1 (F4) design** — per-tenant HNSW index (full isolation, higher memory) vs. shared index + tenant-keyed filter (lower memory, tenant-tagged vectors). Spike must decide.
3. **R2 (S11) default embedder** — pluggable interface with no default? LSA as default (deterministic, in-tree, already shipped)? External API only (BYO embeddings)? Spike must decide.
4. **C6 `llm.generate` procedure** — drop entirely, or gate on populated `APIKey` and return errors instead of `[MOCK RESPONSE...]`?
5. **Should the GNN procedure (S6) get a Track R entry**, or is it deferred indefinitely as a "research kernel" with no ship date? Current plan defers; revisit if a GNN customer appears.

---

## Carry-forward decision points from May-10 plan (still open)

1. **GraphRAG SSE vs. WebSocket** — `/v1/retrieve` is synchronous. SSE/WebSocket streaming is a future enhancement, not a launch question. Still open.
2. **Cypher revisit timing** — RESOLVED: Track C is the answer.

---

## Risks specific to this 6-week window

- **Surgical-extraction discipline is the load-bearing assumption for Track C.** Every C-track PR must explicitly `git checkout ...^3 -- <specific path>` and review the file as if writing it fresh. Bulk-apply temptations should be resisted; if a reviewer notices a file with a `// for the spike` comment in production-path code, that's a sign the discipline slipped.
- **S1's narrowing may be wrong somewhere.** PR #145 is 51 of 58 methods. C2 (btree_storage) is the first external consumer. If C2 needs methods that S1 lacks, the right move is to expand S1 *as part of C2* (or before it) rather than work around. Expect at least one S1 follow-up PR.
- **R1/R2 redesign cost is unestimated.** Path C took ~5 PRs and one session. R1 and R2 may each take similar effort. If the 6-week plan extends because of redesign cost, that's fine — the alternative (landing facade features) is worse.
- **Subset 🟢 has integration drift.** The archive is parented to pre-Path-C `main` (`3c27aaf`). Files that touch `pkg/api/handlers_*.go`, `pkg/storage/vector_*.go`, or `pkg/wal/apply/` may need rebase work — what compiled against `3c27aaf` won't necessarily compile against today's `main`. Plan for one fixup commit per extraction.

---

## Out of scope (carry-forward + new)

- **GQL / non-Cypher query languages** — defer. Track C is the Cypher engine landing; GQL revisit is a post-Track-C question.
- **Geospatial / temporal data-model features** — still deferred; no new triggering signal.
- **Performance tracks B2/B3/B4** — opportunistic only.
- **Code-quality tracks C1/C2/C3/C4** (May-10 lettering) — opportunistic. Note the lettering collision with Track C (Cypher) above; future docs should rename.
- **Mobile / `gomobile` / `pkg/mobile`** — Syntopica-v2 ruled out (April 2026). Unchanged.
- **S6 GNN as native kernel** — the archive's version is spike-quality. Defer unless a customer drives it; would warrant its own track-banner spike.
- **S10b multi-statement ACID transactions** — Subset 🔴 in the audit. The HTTP endpoints exist in the archive but with no isolation. Redesign from first principles is multi-quarter; deferred indefinitely.
- **`-tags zmq` replication variant** — deleted (PR #65, May-10). Stays deleted; nng-only.

---

## Known limitations + productization gaps (carry-forward)

Most items from May-10's "Known limitations + productization gaps" §  remain open. Three updates:

- **~~Single-node ceiling — silent vs. documented~~** — CLOSED 2026-05-13 by PR #146 (README "Scalability & Limitations" section).
- **~~Storage interface extraction (S1)~~** — PARTIALLY CLOSED 2026-05-13 by PR #145. Track R closes the rest.
- **Linux CI infra tax** — STILL OPEN. Should be a one-PR fix; move into the queue before Track C.

The commercial-offering question (open-source-first / dual-license / hosted) is unchanged from May-10. Worth its own founder-led discussion, not a technical-track decision.

---

## How to use this document

This is a planning checkpoint, not a backlog. When picking up the next PR:

1. Read the next item on the critical path (or any unblocked off-path item).
2. If the item has a "spike" sub-task, do the spike first and **stop** before implementation — the spike's recommendation may change scope.
3. After ~3–5 PRs land, this checkpoint should itself be revisited and superseded by `NEXT_STEPS_<DATE>.md` for the next window.

**Revisit triggers** (any one is sufficient to start a new checkpoint immediately, not after the 3–5 PR cadence):
- **C2 (btree_storage) surfaces an S1 narrowing-gap** — expand S1 mid-track and re-plan.
- **R1 (F4) or R2 (S11) spike concludes "redesign is bigger than expected"** — the 6-week window extends, and the next checkpoint scopes the redesign as its own track.
- **A customer-driven priority lands on the queue** — re-plan in the customer's terms, not the audit's.

---

## Appendix — extraction commands for Track C

For each Track C PR, the canonical extraction is:

```bash
# Confirm the archive branch is fetched
git fetch origin archive/gemini-bulk-2026-05-13

# Verify Subset 🟢 file presence at parent ^3
git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/btree/ pkg/storage/btree_storage.go pkg/query/

# Extract specific files (do NOT bulk-apply)
git checkout origin/archive/gemini-bulk-2026-05-13^3 -- pkg/btree/node.go pkg/btree/pager.go pkg/btree/tree.go

# Review every line as if writing fresh — watch for "// spike", "// Mock", "// TODO" markers
grep -nE '// (spike|Mock|MOCK|TODO|FIXME|stub|for now)' pkg/btree/*.go

# Run tests under the race detector
go test -race -count=3 ./pkg/btree/

# Lint at CI's surface
golangci-lint run ./...
```

The `^3` parent reference is essential — `origin/archive/gemini-bulk-2026-05-13` (the bare ref) points at the working-tree stash commit, which does NOT contain the untracked files. The untracked-files stash is the third parent (`d9417a9`).
