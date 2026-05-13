# Session handoff — 2026-05-13 02:31 UTC

**Date**: 2026-05-13 (single continuous session, ~1h, picked up from `SESSION_HANDOFF_2026-05-13-0124Z.md`'s "Linux CI infra fix → C1" critical path).
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-13-0124Z.md` for §3–§8. That doc's record of PRs #155–#157 stays accurate; this doc extends through #159–#163.

## 1. TL;DR

This turn (post-#158 handoff) tested the Linux CI OOM hypothesis (#159, disproven by evidence), closed Track H5 (#160), unblocked the lint check repo-wide (#161), and pushed Track C from "design-complete" through C1.0 + C1.1 (#162 merged, #163 open). C1.1 surfaced — and includes the fix for — a real B+Tree correctness bug in the original archive code (post-split lookups for boundary keys returned not-found).

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #161 | `chore(deps): go mod tidy — promote golang.org/x/mod to direct require` | One-line fix for a `go.mod`-tidy regression introduced by #150's direct import of `golang.org/x/mod/semver`. Was failing `Check go.mod and go.sum` lint job on every PR. Landed first to unblock #159 + #160 + #162's same lint check. |
| #160 | `docs(claude.md): fold stacked-PR --delete-branch gotcha into Known pitfalls (H5)` | Closes Track H5. Promotes the user-private memory `feedback_stacked_pr_delete_branch_gotcha` into project-level CLAUDE.md so every agent (any machine, any user) sees it on the orient-first read. Timely given Track C will produce stacked PRs (C1.0 → C1.1 was the first instance). |
| #159 | `chore(ci): cap test-race package-parallelism at -p 2 (H/Linux CI)` | Hypothesis test: capped `go test -race`'s package-parallelism to `-p 2` to mitigate suspected OOM during race-detector instrumentation on Ubuntu runners. **Hypothesis disproven** by Go 1.24 ubuntu CI evidence (`make: *** test-race Terminated` + runner shutdown signal — external SIGTERM, not OOM). Merged anyway as a marginal improvement (slightly lower memory peak for the same wall-clock budget); doesn't fix the Linux CI tax. |
| #162 | `feat(btree): extract pkg/btree primitives from gemini archive (C1.0)` | Surgical extraction of `pkg/btree/{node,pager,tree}.go` from `origin/archive/gemini-bulk-2026-05-13^3` (~649 LOC, stdlib-only imports). Verified zero coupling. Three `TODO(C1.1)` comments document the spike-quality bits found during inspection: no btree-level tests, stub `Delete` (`Put(key, nil)` "for spike"), magic constant `20`. Acceptance changed from "tests pass" to "build/vet/lint clean — tests deferred to C1.1." |
| #163 | `feat(btree): tests + Delete contract + named constant + correctness fix (C1.1)` | **OPEN at handoff time.** 13 unit tests (10 in `tree_test.go`, 3 in `node_test.go`); Delete contract documented as tombstone with compaction TODO at top of `pager.go`; `20` → `maxKeysPerNode` with derivation comment. **Bonus**: tests surfaced a real navigation bug in archive — `findLeaf` and `insertNonFull` used `findKey` (`>=`) for internal-node descent, but the leaf-split convention requires `>` for child selection. Added `findChild` and routed both navigations through it. Without the fix, post-split `Get` for boundary keys returned not-found. |

**Session total**: 4 PRs merged (#159, #160, #161, #162) + 1 PR open (#163). Combined with previous session's #155–#157 + #158 handoff, the broader 2026-05-12/13 arc has produced 18 PRs.

## 3. Current state

- `origin/main` HEAD: `d16f58d feat(btree): extract pkg/btree primitives from gemini archive (C1.0) (#162)`
- **Open PRs from this session**: **#163** (C1.1). All required CI green (`golangci-lint`, `Check go.mod and go.sum`, macOS Test on Go 1.25); Linux infra tax pattern expected (tolerated). Mergeable as soon as you bless it.
- **Open PRs from prior/parallel work** (NOT this session — inherited): #136 `feat(search): switch LSA term weighting to log-entropy (A2)`, #137 `feat(search): quantize LSA doc vectors to int8 (C1)`, #138 `docs: rewrite PRODUCTION_QUICKSTART for single-node cmd/server (A8.1 step 4b)`, #139 `docs: update legacy-binary references after A8.1 (step 4c)`, #140 `refactor(metrics): delete replication-metric orphans (A8.1 step 4d)`. **Naming-collision warning**: #136/#137 use `A2`/`C1` tag-names that collide with the new planning doc's Track A/C semantics — that #136/#137 use the old (pre-2026-05-13) tag scheme. If they merge, PR descriptions or commit messages may need disambiguation.
- **Open local branches** (not all this session): `docs/coord-learning-skills`, `feat/c1.1-btree-tests-delete` (this session, current PR), `feat/h4.3-followup-snapshot-tenant-index`, `feat/h4.3-replay-tenant-index`, `feat/h4.4-rest-blite-mirror`, `feat/lsa-bigrams-logentropy`, `feat/lsa-persistence`, `feat/lsa-quantize-docvecs`. Most predate this session.
- **Open worktrees**: `/Users/darraghdowney/Workspace/github.com/graphdb-c1.1` — should be removed after #163 merges.
- **Uncommitted changes on main**: NONE (this handoff PR will be the last thing landing).
- **Test/lint state on main**: `go build ./...` clean (verified earlier this session); `pkg/btree` tests under `-race -count=3` pass on `feat/c1.1-btree-tests-delete` (verified pre-PR). Lint clean.

## 4. Artifacts that survive this session

### `pkg/btree/` (PR #162 + PR #163)

A new top-level Go package: `node.go` (159 LOC), `pager.go` (157 LOC), `tree.go` (333 LOC), plus C1.1's `tree_test.go` (235 LOC) and `node_test.go` (93 LOC). Stdlib-only imports. Zero internal coupling. Zero current consumers — C2 will be the first.

### `CLAUDE.md` § Known pitfalls (PR #160)

New bullet on the stacked-PR `--delete-branch` gotcha (auto-CLOSE behavior + recovery paths). Brings CLAUDE.md to 169 lines (cap is ~200).

### Diagnosis correction (PRs #159 + #163 descriptions)

The Linux CI failure is **external SIGTERM, not OOM and not internal `go test -timeout`**. PR #159's description has the full evidence write-up. Worth folding into the next planning doc / a CLAUDE.md tweak (see §6).

### Track C planning-doc shape (carried forward through #156-#157 + this session's #162/#163)

C1 is no longer a single PR. It's split into **C1.0 (extract, #162)** and **C1.1 (tests + Delete + constant + correctness fix, #163)**. Sequencing graph in `NEXT_STEPS_2026-05-13.md` updated accordingly. C2 is now the head of the critical path after #163 merges.

## 5. What's next

The ranked queue from `docs/NEXT_STEPS_2026-05-13.md`. Critical path top:

### Immediate: merge #163 (C1.1)

All required CI green; Linux infra tax tolerated. Single click. Closes the C1 sub-track and unblocks C2.

### Then: C2 — `pkg/storage/btree_storage.go`

- Files exist in archive at `origin/archive/gemini-bulk-2026-05-13^3 -- pkg/storage/btree_storage.go`, `btree_storage_test.go`, `btree_bench_test.go` (~818 LOC + ~200 LOC tests).
- **First external consumer of S1's `Storage` interface beyond `*GraphStorage`.** Validates whether S1's narrowing (PR #145) was correct.
- Per planning doc: "May need fixups: S1's narrowing omitted vector methods and `AddObserver`; B+Tree backend may require either trimmed-interface satisfaction or stub implementations of the omitted methods. Decide PR-locally; document the choice."
- **Surprise budget**: expect at least one S1 follow-up PR if the interface needs adjustment to fit the B+Tree backend.

### Then: C3..C6 — the rest of the Cypher engine

- C3: `pkg/query/physical_plan.go` (~1233 LOC, Volcano operators). May need to split.
- C4: `pkg/query/planner.go` (~329 LOC).
- C5: parser additions for CALL/CREATE/SET/DELETE/REMOVE/MERGE.
- C6: `pkg/query/procedures.go` — **DO NOT carry the `algo.shortestPath` stub** (replace with real wire-up to `pkg/algorithms`); **DO NOT carry `gnn.messagePass`** (S6 redesign required first); **audit `llm.generate`** for the mock-fallback issue.

### Off-path: R1 + R2 implementation (parallel-eligible)

- R1 spike (#156) and R2 spike (#157) each have a PR breakdown ready.
- R1 + R2 touch disjoint method sets on `Storage` — safe to run in parallel via `git worktree` + the `graphdb-coord` skills (sibling repo `dd0wney/graphdb-coord`).
- Don't start R1/R2 implementation in this checkout simultaneously without worktree isolation.

### New off-path candidate: Linux CI infra escalation

- This session disproved the OOM hypothesis. The remaining structural fixes per the advisor:
  - `concurrency: cancel-in-progress: true` on `test.yml` to free runner slots when superseded SHAs land.
  - Matrix-breadth reduction (drop Go 1.23 + 1.24, keep 1.25 only) — halves Linux runner pressure.
  - Move race tests to macOS-only (macOS finishes in ~3 min reliably).
- Single-PR each. Pick whichever the user prefers; this session didn't get user signal on the trade-off.

### Pre-existing open work to be aware of (not this session's responsibility)

Same set as the previous handoff's §5 — see #136–#140 disposition open question.

## 6. Stale assumptions to retire

This is the highest-leverage section. The next session should be able to update planning docs / refresh memory using only this list.

### `CLAUDE.md` line 93 — Linux CI exit-143 diagnosis

Currently:

> **CI Ubuntu `test-race` consistently exits 143.** This is `make test-race`'s 10-minute timeout against the runner's idle-timeout budget — runner cancellation, not a real test failure. macOS runs pass. Tolerated; don't re-investigate without new evidence.

**Corrected diagnosis** (PR #159 evidence): exit-143 is **external SIGTERM to the runner agent**, not `go test -timeout 10m` firing. Evidence:
- `make: *** [Makefile:59: test-race] Terminated` + `##[error]The runner has received a shutdown signal` in the log (Go 1.24 ubuntu, this session).
- Even non-race steps (e.g., `make test-verbose`) fail in the same exit-143 pattern; some PRs see the kill at 2:42, others at exactly 2821s (47:01 — a hard cap, not random preemption).
- Even **docs-only PRs** (#146 with zero code changes) hit Ubuntu fast-fails. Confirms the cause is infra, not workload.

**Suggested replacement bullet**: "CI Ubuntu jobs (test-verbose AND test-race) consistently exit 143 with `runner has received a shutdown signal`. The cause is external — likely account-level concurrent-job contention or runner-pool eviction, NOT internal `go test -timeout`, NOT race-detector OOM (PR #159 ruled out OOM). macOS runs pass. Tolerated; escalation candidates documented in NEXT_STEPS_2026-05-13.md § Track H 'Linux CI infra escalation.'"

### User auto-memory `project_ci_red_state_tolerated.md`

Currently characterizes exit-143 as "concurrency cancel-in-progress" — phrasing that suggests `concurrency.cancel-in-progress: true` clause in a workflow file. **`.github/workflows/test.yml` has no such clause.** The kill comes from elsewhere (account-level limits, runner preemption, or some interaction we haven't fully diagnosed). Suggest updating the memory's "Why" line to "external SIGTERM to runner agent; likely account-level concurrent-job contention" and noting that `test.yml` deliberately has no `concurrency:` clause.

### `NEXT_STEPS_2026-05-13.md § Track H "Linux CI infra tax"`

Currently says: "Two structural fixes: split race target across packages, or bump the runner timeout in `.github/workflows/`. Single small PR either way." **Both options are now known to be ineffective**:
- Bumping `go test -timeout` doesn't help because internal timeout never fires (external SIGTERM beats it).
- Splitting the race target into sequential steps in the same job doesn't help (advisor's catch — same per-job memory + wall clock + preemption exposure).
- `-p 2` (already shipped in #159) is now confirmed not to fix it either.

**Suggested replacement bullet**: list the three escalation candidates from §5 above (`concurrency: cancel-in-progress`, matrix-breadth reduction, macOS-only race) and note that the OOM hypothesis was empirically disproven.

### `NEXT_STEPS_2026-05-13.md § Track C "C1"`

This session's PR #162 + PR #163 split the C1 entry in the planning doc. The doc reflects the C1.0 ✅ + C1.1 ✅ status (post-merge). One implication for the next planning checkpoint: **C2's "May need fixups" framing is now load-bearing** — C1.0 + C1.1 demonstrated that the archive isn't always a clean lift-and-shift. Expect C2 to surface its own surprises.

## 7. Open questions for the user

1. **Linux CI escalation pick**: `concurrency: cancel-in-progress: true` (advisor's recommendation), matrix-breadth reduction, or move race to macOS-only? Each is a single small PR.
2. **PRs #136–#140 disposition** (carried forward from previous handoff — still unanswered): should the A8.1 step-4 cleanup (#138, #139, #140) merge, or are they superseded? Should the LSA improvements (#136, #137) coordinate naming with Track C of NEXT_STEPS_2026-05-13 (both use "C1" / "A2" tags meaning different things)?
3. **Worktree + branch cleanup**: this session left a worktree at `/Users/darraghdowney/Workspace/github.com/graphdb-c1.1` and several pre-existing local branches (the `feat/h4.3-*`, `feat/lsa-*`, etc). The `branch-cleanup` skill could clear the merged ones; the worktree needs `git worktree remove` after #163 merges.

## 8. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-13-0231Z.md

Then read (in order):
  docs/NEXT_STEPS_2026-05-13.md (especially § Track C "C2" + § Track H if you want to take on Linux CI escalation)
  CLAUDE.md § "Orient first" (auto-loaded; pointer is correct after PR #155)
  docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md § Subset 🟢 (only if working on C2 onward)

Default next action: merge PR #163 (C1.1) if not already, then start C2 — extract pkg/storage/btree_storage.go + btree_storage_test.go + btree_bench_test.go from origin/archive/gemini-bulk-2026-05-13^3. Per the planning doc: this is the first external consumer of S1's Storage interface beyond *GraphStorage; expect 1-2 S1 follow-up PRs if the interface needs adjustment for the B+Tree backend.

Validation angle: this session demonstrated that surgical extraction + test-writing surfaces real bugs (the C1.1 findKey/findChild fix). Carry that discipline into C2 — don't bulk-apply the archive's btree_storage code; reason about each method as you extract.

Pre-flight:
  - confirm `git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/storage/btree_storage.go pkg/storage/btree_storage_test.go pkg/storage/btree_bench_test.go` shows all three files (verified this session).
  - clean up the C1.1 worktree: `git worktree remove /Users/darraghdowney/Workspace/github.com/graphdb-c1.1` (after #163 merges).

Stale assumptions to retire BEFORE acting on Track H:
  - CLAUDE.md line 93's exit-143 diagnosis is wrong (see §6 of this handoff). PR #159 ruled out OOM. Real cause is external SIGTERM (likely account-level concurrent-job contention).
  - User-private memory `project_ci_red_state_tolerated` has the same wrong "concurrency cancel-in-progress" framing.
  - NEXT_STEPS_2026-05-13.md § Track H's "split-race-target vs bump-runner-timeout" framing is empirically disproven; both options miss the actual failure mode.

5 open PRs predate this session (#136–#140). Not this session's responsibility; surface to the user if their disposition affects your work.

End-of-session: write a session handoff per CLAUDE.md § "Preparing a new session (handoff convention)" via the session-handoff skill.
```

## 9. How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-05-13.md` (§ "Sequencing graph" → C2 is now top-of-queue once #163 merges; § "Appendix — extraction commands for Track C" is the operational reference for C2).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded; pointer is correct after PR #155 + the H5 fold from PR #160).
4. If picking up C2: also `git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/storage/btree_storage.go` to confirm files present, and `docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md § Subset 🟢` for context.
5. If picking up Linux CI escalation: read §6 of this doc FIRST so you don't try the disproven hypotheses again.
6. If you find the prior handoff (`SESSION_HANDOFF_2026-05-13-0124Z.md`) authoritative for anything in §3–§8, cross-check against this doc — the deltas in §6 above name what's been overtaken.
