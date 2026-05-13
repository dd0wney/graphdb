# Session handoff — 2026-05-13 05:33 UTC

**Date**: 2026-05-13 (single continuous session, ~3 hrs, picked up from `SESSION_HANDOFF_2026-05-13-0231Z.md` per its NEXT_SESSION_PROMPT).
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-13-0231Z.md` for §3–§8. The prior doc's record of PRs #155–#163 stays accurate; this doc extends through #165–#168.

## 1. TL;DR

This session executed the planning doc's **C2 → C3.0 → C4.0** critical path in one continuous arc, plus a docs-correction PR (#166) that fixed the misdiagnosed Linux CI exit-143 framing in CLAUDE.md + the planning doc. Pattern that emerged: **surgical defer of forward-refs** at extraction boundaries (CallOperator dropped from C3.0; `q.Call` block dropped from C4.0) — both will reinstate when C5 (parser CallClause) + C6 (procedureRegistry) co-land.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #165 | `feat(storage): BTreeGraphStorage as second Storage implementor (C2)` | First non-`*GraphStorage` implementor of S1's narrowed Storage interface (#145). Drift fixes: `Snapshot(ctx)` → `Snapshot()`, `RemoveNodeFromVectorIndexes(id, tenantID)` → `(id)`. Stub policy: typed `errBTreeBackendUnsupported` sentinel for every unimplemented write — silent `(nil, nil)` is forbidden because `CreateNodeWithUniquePropertyForTenant` is the B-lite primitive `graphdb-coord` depends on. Three archive deps were gone from current tree (`vector.NewPersistentHNSWIndex`, `KVStore`, `NodeObserver`) — caught by per-method review. |
| #166 | `docs: correct Linux CI exit-143 diagnosis (CLAUDE.md + Track H)` | The "concurrency cancel-in-progress" framing was empirically wrong (`test.yml` has no concurrency clause). The kill is **external SIGTERM to the runner agent** — likely account-level concurrent-job contention. Corrects CLAUDE.md line 93 (auto-loaded for every agent) + Track H planning doc. Used immediately on PR #165's same-pattern Ubuntu Go 1.24 failure to prevent a regression false-alarm. |
| #167 | `feat(query): Volcano physical operators (C3.0) — 16/17 lifted, CallOperator deferred` | 1192 LOC extraction. **Net-new architecture pattern** (Volcano pull-based `Open/Next/Close`) coexisting with the existing Step-based materializing executor. Drift fixes: archive's `EvalValue` was on concrete types not the interface — added private `valueEvaluator` interface for type assertion + `extractValue` fallback (matches existing `executor_steps.go` / `executor_results.go` pattern). One `revive` lint catch (indent-error-flow on MergeOperator). **CallOperator deferred** — references `procedureRegistry` (C6 territory). Net-new dep: `go.opentelemetry.io/otel@v1.43.0` + 5 transitive (committed separately for reviewable diff). |
| #168 | `feat(query): logical→physical Planner (C4.0)` | **OPEN at handoff time.** 350 LOC. Net-new code (no current planner-equivalent in `pkg/query`). Same forward-ref pattern as C3.0: drop `if q.Call != nil` block (Query.Call field doesn't exist; CallClause is C5 territory). All 3 required checks GREEN; failures match exit-143 / benchmark-permissions known patterns. Mergeable per #166's corrected diagnosis. |

**Session total**: 3 PRs merged (#165, #166, #167) + 1 PR open (#168). Combined with previous session's #155-#163 + handoff #164, the broader 2026-05-12/13 arc has produced 22 PRs.

## 3. Current state

- `origin/main` HEAD: `dc9a209 feat(query): Volcano physical operators (C3.0) — 16/17 lifted, CallOperator deferred (#167)`
- **Open PRs from this session**: **#168** (C4.0). All 3 required CI green (`golangci-lint`, `Check go.mod and go.sum`, `Test on Go 1.25 / macos-latest`); 4 failures all match the known infra patterns (exit-143 SIGTERM on 3 Ubuntu runners + benchmark comment-step permissions). `mergeable=MERGEABLE` `mergeStateStatus=UNSTABLE` — the normal green-but-noisy state per CLAUDE.md "Known infra patterns." Mergeable as soon as you bless it.
- **Open PRs from prior/parallel work** (NOT this session — inherited): **11 PRs**, larger than the prior handoff's framing acknowledged:
  - **#108** `fix(storage): rebuild per-tenant label index in WAL replay (H4.3)` (2026-05-10)
  - **#109** `fix(api): mirror B-lite claim-uniqueness in REST POST /nodes (H4.4)` (2026-05-11)
  - **#110** `fix(storage): rebuild per-tenant label index on snapshot load (H4.3-followup)` (2026-05-11)
  - **#131** `docs(skills): add coord-lesson, coord-insight, coord-dream` (2026-05-12)
  - **#134** `docs: delete legacy UPGRADE_GUIDE.md (A8.1 step 4a)` (2026-05-12)
  - **#135** `feat(search): persist per-tenant LSA indexes to disk (B1)` (2026-05-12)
  - **#136** `feat(search): switch LSA term weighting to log-entropy (A2)` (2026-05-12)
  - **#137** `feat(search): quantize LSA doc vectors to int8 (C1)` (2026-05-12)
  - **#138** `docs: rewrite PRODUCTION_QUICKSTART for single-node cmd/server (A8.1 step 4b)` (2026-05-12)
  - **#139** `docs: update legacy-binary references after A8.1 (step 4c)` (2026-05-12)
  - **#140** `refactor(metrics): delete replication-metric orphans (A8.1 step 4d)` (2026-05-12)
- **Open local branches** (not all this session): `docs/coord-learning-skills` (#131), `feat/c4-planner` (this session, current PR), `feat/h4.3-followup-snapshot-tenant-index` (#110), `feat/h4.3-replay-tenant-index` (#108), `feat/h4.4-rest-blite-mirror` (#109), `feat/lsa-bigrams-logentropy` (#136), `feat/lsa-persistence` (#135), `feat/lsa-quantize-docvecs` (#137). Most predate this session.
- **Open worktrees**: `/Users/darraghdowney/Workspace/github.com/graphdb-c4` — should be removed after #168 merges.
- **Uncommitted changes on main**: NONE (this handoff PR will be the last thing landing).
- **Test/lint state on main**: `go build ./...` clean; `go vet ./...` clean; `golangci-lint run ./...` 0 issues; `pkg/query` tests pass (~21s on the C4 worktree, equivalent on main).

## 4. Artifacts that survive this session

### `pkg/storage/btree_storage.go` + tests + bench (PR #165)

A new B+Tree-backed implementation of `Storage`. C2-PARTIAL: 30% real (CRUD + label/type indexes + adjacency + statistics + persistence + DeleteNodeForTenant); 70% stubs that route through `errBTreeBackendUnsupported` typed sentinel. Tests pin both the real surface and the stub policy. Benches show counterintuitive single-thread numbers (BTree 25× faster on point-read GetNode than `*GraphStorage` because A4 per-shard rlocks + Clone() are real costs the BTree skips); asymptotics flip on multi-thread / large-tree.

### `pkg/query/physical_plan.go` (PR #167)

1192 LOC, 16 of 17 Volcano operators (NodeScan, IndexSeek, Expand, Filter, Project, Create, Set, Delete, Remove, Merge, Unwind, Union, OptionalMatch, Aggregate, NestedLoopJoin, HashJoin). CallOperator deferred. Per-operator OTEL spans on Open(). Private `valueEvaluator` interface in this file (not exported) bridges archive's `EvalValue` calls to the public `Expression` interface that only requires `Eval(...) (bool, error)`.

### `pkg/query/planner.go` (PR #168, in flight)

350 LOC. Translates Query AST → tree of PhysicalOperators. `q.Call` block stripped pending C5 + C6. All 11 other clause types (MATCH, OPTIONAL MATCH, UNWIND, WHERE, CREATE, SET, DELETE, REMOVE, MERGE, RETURN/Aggregate-or-Project, WITH+Next, UNION) handled.

### `CLAUDE.md` § "Known infra patterns" line 93 + `NEXT_STEPS_2026-05-13.md` § Track H (PR #166)

Corrected exit-143 diagnosis. The auto-loaded CLAUDE.md text is now factually accurate; the planning doc's escalation-candidates list now reflects the disproven "split race / bump timeout" framing. **Used in this session** to triage PR #165's Go 1.24 Ubuntu failure (exit-143 SIGTERM) without misclassifying as a regression.

### Surgical-defer pattern (PRs #167 + #168 combined)

Two consecutive PRs demonstrated the pattern: when an extracted file forward-refs a not-yet-extracted symbol (procedureRegistry from C6, Query.Call field from C5), drop the forward-referencing block + leave a marker comment + document in the PR body. Same agent + same reviewer can re-extract the missing block when prerequisites land. Cleaner than stubbing the missing symbol or blocking the entire extraction.

## 5. What's next

The ranked queue from `docs/NEXT_STEPS_2026-05-13.md`. Critical path top:

### Immediate: merge #168 (C4.0)

All 3 required checks green; failures match known infra patterns. Single merge. Closes Track C "C4."

### Then: C5 — Cypher parser additions (CALL / CREATE / SET / DELETE / REMOVE / MERGE)

- Archive source: `pkg/query/parser_clauses.go` diff in `origin/archive/gemini-bulk-2026-05-13^3`.
- **This is the bottleneck** for several queued reinstatements: the q.Call block in planner.go can come back once `CallClause` exists, AND C6's procedureRegistry can be wired through CallOperator once both lands.
- Will likely need its own per-symbol drift triage. Apply the same surgical-extraction discipline.

### Then: C6 — `pkg/query/procedures.go`

- Per planning doc: DO NOT carry `algo.shortestPath` stub (replace with real wire-up to `pkg/algorithms`). DO NOT carry `gnn.messagePass`. AUDIT `llm.generate` for mock-fallback.
- **Co-lands the deferred CallOperator** (from C3.0) + the `q.Call` block (from C4.0). PR will be more substantial than C3.0/C4.0 individually because it backfills two prior deferrals.

### Then: C3.1 + C4.1 — back-fill operator + planner unit tests

- Pattern matches C1.0/C1.1 split. C1.1's tests surfaced a real navigation bug (`findKey` vs `findChild`); C3.1/C4.1 may surface similar drift.
- Order question: stack on C5/C6 (timing-sensitive — see #7.3 below) or land independently after #168 merges.

### Off-path (parallel-eligible):

- **R1 (F4 vector tenant) implementation** — spike doc landed prior session.
- **R2 (S11 auto-embedder) implementation** — spike doc landed prior session.
- **Linux CI infra escalation** (still §7.1 of prior handoff, now also §7.2 here): pick one of `concurrency: cancel-in-progress: true`, matrix-breadth reduction, or macOS-only race. Single-PR each.

### Pre-existing open work to be aware of

11 inherited open PRs (full list in §3). The prior handoff only flagged #136-#140; this session uncovered #108, #109, #110, #131, #134, #135 also still open. Disposition needed (see §7.1).

## 6. Stale assumptions to retire

### `NEXT_STEPS_2026-05-13.md` § Track C "C3" — Decision points #1

Currently says: "C3 split or single PR? `physical_plan.go` is ~1233 LOC. Single PR is faster but heavy review; 3-way split (scan/index, mutation, join/aggregate) is reviewable but slower. Recommendation: start as single PR; split if review surfaces it."

**Outcome**: started as single PR (#167). Review didn't surface a split request. Resolved → single-PR worked. Update the planning doc to record the outcome, OR fold this resolved decision-point into a "what worked" note for the next session's planning checkpoint.

### `NEXT_STEPS_2026-05-13.md` § Track C "C3" — Acceptance criterion

Currently says: "OTEL spans visible in `pkg/telemetry/` exporter integration test."

**Problem**: `pkg/telemetry/` does not exist in the current tree. Acceptance criterion is unverifiable as written. PR #167 lowered the bar to "OTEL spans compile + tracer wired" and surfaced this in the PR body. The planning doc still has the old wording. Either:
- (a) Update the doc to lower the bar (+ note the deferral),
- (b) Add a new track entry to extract `pkg/telemetry/` from somewhere (archive? from-scratch? find existing?),
- (c) Both.

### `NEXT_STEPS_2026-05-13.md` § Track C "C4" / "C6"

The planning doc treats C4 and C6 as sequenced. **In practice**, C4.0 deferred its `q.Call` block AND C3.0 deferred CallOperator — both reinstate at C6. So C6's PR scope is **larger** than the planning doc's "~102 LOC + audit work" framing implies. The reinstatements add ~95 LOC across two existing files (planner.go's q.Call block + the CallOperator class). Update C6's scope estimate.

### `SESSION_HANDOFF_2026-05-13-0231Z.md` §3 / §5

The prior handoff said "5 open PRs predate this session (#136-#140)." **Actually 11**: #108, #109, #110, #131, #134, #135, #136, #137, #138, #139, #140. The prior agent missed 6 of them. If they all need disposition, the surface area is bigger.

### NEW gap not yet on planning doc

**The `valueEvaluator` private interface in `pkg/query/physical_plan.go` is a workaround.** Long-term, `EvalValue` should be on the public `Expression` interface (would force adding it to all concrete implementations: PropertyExpression, BinaryExpression, ComparisonExpression, etc — wider refactor). Currently the workaround is local. Worth tracking as a future cleanup task once C-track lands; surface to next planning checkpoint.

## 7. Open questions for the user

1. **PRs #108-#140 disposition (11 inherited open PRs)**: most are 5+ days old. Some carry obvious value (#108-#110 are H4 storage fixes; #135-#137 are LSA improvements). Some are the A8.1 cleanup (#134, #138, #139, #140) which may be superseded. Some may be abandoned. Triage decision: which merge as-is, which need rebase against current main (likely all of them), which retire? **Carries forward unresolved from prior handoff §7.2.**

2. **Linux CI escalation pick** (still §7.1 from prior handoff): `concurrency: cancel-in-progress: true` (advisor's pick), matrix-breadth reduction, or macOS-only race? Single small PR each.

3. **C3.1 + C4.1 timing**: write them stacked on C5/C6 (cleanest because C5/C6 land more code that tests should anchor), or land independently after #168 merges (faster, but less cumulative test coverage)? Stacked-PR `--delete-branch` gotcha applies to either choice.

4. **`pkg/telemetry/` resolution** (new from §6): ignore, extract from archive, build from scratch?

## 8. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-13-0533Z.md

Then read (in order):
  docs/NEXT_STEPS_2026-05-13.md (especially § Track C "C5" + "C6")
  CLAUDE.md § "Orient first" (auto-loaded; pointer is correct)
  CLAUDE.md § "Known infra patterns" (corrected by #166 — line 93)

Default next action: merge PR #168 (C4.0) if not already, then start C5 — extract Cypher parser additions for CALL/CREATE/SET/DELETE/REMOVE/MERGE from origin/archive/gemini-bulk-2026-05-13^3 (probably pkg/query/parser_clauses.go diff, but verify the path). C5 unblocks BOTH the q.Call block in planner.go AND CallOperator (the two surgical-defer reinstatements queued from this session).

Validation angle: this session demonstrated the surgical-defer pattern twice (C3.0 dropped CallOperator; C4.0 dropped q.Call block). C5 will likely surface its own forward-ref to C6 (CallClause type definition that procedureRegistry consumes). Apply the same defer discipline if so. Consult advisor BEFORE writing if the symbol surface is bigger than ~5 forward-refs.

Pre-flight:
  - confirm `git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/query/parser_clauses.go pkg/query/cypher_spike_test.go` (the test surface for C5 acceptance per planning doc)
  - clean up the C4.0 worktree: `git worktree remove /Users/darraghdowney/Workspace/github.com/graphdb-c4` (after #168 merges)

Stale assumptions to retire (§6 of this handoff):
  - NEXT_STEPS_2026-05-13.md § Track C "C3" decision-point #1 → C3 single-PR recommendation worked; resolved
  - NEXT_STEPS_2026-05-13.md § Track C "C3" acceptance bar refers to nonexistent pkg/telemetry/
  - NEXT_STEPS_2026-05-13.md § Track C "C6" scope is larger than written (must absorb C3.0's CallOperator + C4.0's q.Call block reinstatements)
  - prior handoff said "5 open PRs from prior session" — actually 11

Open questions for the user (§7) — at minimum acknowledge before C5:
  1. Disposition of 11 inherited open PRs (#108-#140)
  2. Linux CI escalation pick
  3. C3.1 + C4.1 timing (stacked on C5/C6 or independent)
  4. pkg/telemetry/ resolution

End-of-session: write a session handoff per CLAUDE.md § "Preparing a new session (handoff convention)" via the session-handoff skill.
```

## 9. How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-05-13.md` (§ "Sequencing graph" → C5 is now top-of-queue once #168 merges; § "Decision points" — note #1 is now resolved per §6 of this handoff).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded; line 93's exit-143 diagnosis is correct after #166).
4. If picking up C5: also `git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/query/parser_clauses.go pkg/query/cypher_spike_test.go` to confirm files present.
5. If picking up the 11 inherited open PRs: triage by tag prefix (H4.x = storage fixes, A8.1 = old cleanup, B1/A2/C1 = LSA — note these collide with new Track C "C1" semantics).
6. The prior handoff (`SESSION_HANDOFF_2026-05-13-0231Z.md`) §3-§8 are superseded by this doc; §6's diagnosis-correction recommendations were executed by PR #166.
