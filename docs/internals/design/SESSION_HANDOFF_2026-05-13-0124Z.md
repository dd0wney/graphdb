# Session handoff — 2026-05-13 01:24 UTC

**Date**: 2026-05-12 → 2026-05-13 (one continuous session, ~13h; supersedes intra-session handoff `SESSION_HANDOFF_2026-05-13-0014Z.md` for the queue/state — that handoff covered PRs #141–#153; this one extends through PRs #155–#157).
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-13-0014Z.md` for §3–§8. That doc's §3–§4 (the 10 Phase-1/Phase-2 merges) remain accurate; only the queue + open-questions sections went stale when this turn landed.

## 1. TL;DR

This turn (post-#154 handoff) landed the 2026-05-13 planning checkpoint and **both** R1/R2 redesign spikes as three independent PRs (#155, #156, #157), produced via parallel `Agent` subagent dispatch. Track C (Cypher engine extraction) and Track R (F4 + S11 redesigns) are now design-complete; next session starts implementation.

## 2. What's done this session

**Pre-this-turn** (10 PRs, detailed in `SESSION_HANDOFF_2026-05-13-0014Z.md` §2): #141 (docs reorg), #144 (Gemini track-claims audit), #145 (S1 narrowed), #146 (README scalability section), #147 (import-dimacs + integration-test dev binaries), #149 (pkg/updater audit), #150–#153 (pkg/updater Path C redesign + cmd/graphdb-upgrade deletion), #154 (intra-session handoff).

**This turn** (3 PRs, parallel-agent dispatch flow):

| PR | Title | Notes |
|---|---|---|
| #155 | docs: planning checkpoint — NEXT_STEPS_2026-05-13 | Single-file-shape: new planning doc + CLAUDE.md orient-first pointer update. Reconciles A8.1 closure + S1 narrowing + Path C against the May-10 plan. Introduces Tracks C (Cypher engine extraction) and R (F4 + S11 redesigns). |
| #156 | docs(spike): F4 — tenant-isolated vector ops redesign | Track R1 spike. Per-tenant HNSW recommended; per-tenant memory cost dominates filter cost below ~1000 tenants. 6 method signatures defined for S1 re-attachment in R3. 4-PR implementation breakdown. Drafted by Agent subagent (`architect`) with `advisor()` callout. |
| #157 | docs(spike): S11 — auto-embedder + NodeObserver redesign | Track R2 spike. Pluggable `Embedder` interface (Option A) + in-tree `LSAEmbedder` adapter recommended. Bounded worker pool replaces archive's `go func`. Critical discipline: misconfiguration must error, not mock-fallback. 5-PR implementation breakdown. Drafted by Agent subagent (`architect`) with `advisor()` callout. |

**Session total**: 13 PRs merged across the ~13h session.

## 3. Current state

- `origin/main` HEAD: `0f0a5e2 docs(spike): S11 — auto-embedder + NodeObserver redesign (#157)`
- **Open PRs from this session**: none. All three landed clean (no branch protection wait — docs-only PRs merged immediately via `gh pr merge --squash --delete-branch` despite pending CI; this is a deliberate pattern for `docs:`-prefixed PRs).
- **Open PRs from prior/parallel work** (NOT from this session, inherited state — flagged for next-session awareness): #136 `feat(search): switch LSA term weighting to log-entropy (A2)`, #137 `feat(search): quantize LSA doc vectors to int8 (C1)`, #138 `docs: rewrite PRODUCTION_QUICKSTART for single-node cmd/server (A8.1 step 4b)`, #139 `docs: update legacy-binary references after A8.1 (step 4c)`, #140 `refactor(metrics): delete replication-metric orphans (A8.1 step 4d)`. PRs #138–#140 are A8.1 step-4 cleanup that didn't merge alongside steps 1–3 (#127, #129, #130, #133); PRs #136–#137 are an LSA improvement track running independently of the new planning doc.
- Open local branches: none (all `--delete-branch`'d at merge).
- Uncommitted changes: **NONE** (this handoff PR will be the last thing landing).
- Build state on main: `git log` clean. Docs-only diffs this turn; no `go build`/`go vet`/lint re-run needed since #157.

## 4. Artifacts that survive this session

### `docs/NEXT_STEPS_2026-05-13.md` (PR #155)

The new planning checkpoint. Replaces `docs/NEXT_STEPS_2026-05-10.md` as the live queue. Introduces:
- **Track C** — Cypher engine extraction (6 atomic PRs from `origin/archive/gemini-bulk-2026-05-13^3`). Appendix has the exact extraction commands.
- **Track R** — F4 + S11 redesigns (with R3 closing the S1 surface afterwards).
- **Critical path**: Linux CI → C1 (btree) → C2 (btree_storage) → C3–C6 (Cypher) → R1 + R2 (parallel) → R3.
- **6-week horizon** (not 90 days), because Track C is bounded by archive contents, not calendar.

`CLAUDE.md`'s orient-first pointer now references this doc (PR #155).

### `docs/internals/design/F4_VECTOR_TENANT_REDESIGN.md` (PR #156)

R1 spike. 587 lines. Recommendation conditional on tenant-count assumption (Option A unless >1000 tenants with dense vectors). 6 method signatures for S1 re-attachment in R3.

### `docs/internals/design/S11_AUTO_EMBEDDER_REDESIGN.md` (PR #157)

R2 spike. 573 lines. Option A (pluggable interface + in-tree `LSAEmbedder`) — chosen after the advisor pointed out that Option B (LSA-as-default) collapses to A once cold-start is correctly accounted for. Five-PR breakdown.

### Parallel-agent dispatch pattern (validated this turn)

Two `Agent` subagents (`architect` type) drafted the F4 + S11 spikes in parallel from disjoint file paths. Each was briefed with: the audit doc as threat model, the canonical tenant-strict pattern, the archive ref for inspection, an existing spike doc as structural template, and an explicit `advisor()` callout. Both completed in under 10 minutes total wall clock; both advisor calls surfaced load-bearing refinements (F4: existence-leak via search-result content, not just error shape; S11: lock-ordering trap + unbounded-goroutine smell).

**Pattern is safe for read-only design work in a shared checkout** (disjoint file writes). Extending to implementation requires `git worktree` isolation per the user's auto-memory `feedback_parallel_agent_worktree_isolation`.

## 5. What's next

The ranked queue from `docs/NEXT_STEPS_2026-05-13.md`. Top of critical path:

### Highest-value (single-PR, unblocks signal): Linux CI infra fix

- May-10 plan flagged this; carried forward in NEXT_STEPS_2026-05-13 § Track H.
- `make test-race` consistently exits 143 on Ubuntu runners (runner-cancellation, not real regression).
- Two structural fixes: split race target across packages, or bump runner timeout in `.github/workflows/`.
- **Do before Track C starts** — Track C is 6 PRs each carrying race tests; noisy red checks would muddy review signal.

### Then: Track C — Cypher engine extraction (starts with C1 btree)

- **C1**: `git checkout origin/archive/gemini-bulk-2026-05-13^3 -- pkg/btree/node.go pkg/btree/pager.go pkg/btree/tree.go` — pure new package, lowest integration risk. Validates the surgical-extraction methodology before C2 adds S1-interface coupling.
- **C2**: `pkg/storage/btree_storage.go` — first external consumer of S1's `Storage` interface beyond `*GraphStorage`. If S1's narrowing (PR #145) was wrong somewhere, C2 surfaces it. Expect at least one S1 follow-up PR.
- C3–C6: physical_plan, planner, parser additions, procedures. C3 may need to split (1233 LOC).

### Off-path: R1 + R2 implementation (parallel-eligible)

- R1 spike (#156) and R2 spike (#157) each have a PR breakdown ready.
- R1 + R2 touch disjoint method sets on `Storage` — safe to run in parallel via `git worktree` + the `graphdb-coord` skills (sibling repo `dd0wney/graphdb-coord`).
- Don't start R1/R2 implementation in this checkout simultaneously without worktree isolation.

### Off-path: H5 — fold stacked-PR --delete-branch gotcha into CLAUDE.md

- Tracked in NEXT_STEPS_2026-05-13 Track H. Currently in user-private memory only (`feedback_stacked_pr_delete_branch_gotcha`).
- Single-file PR to add the pitfall to CLAUDE.md § "Known pitfalls".

### Pre-existing open work to be aware of (not this session's responsibility)

- **#136 / #137** — LSA improvements ("A2" log-entropy, "C1" int8 quantization). **Naming-collision risk**: these "C1" / "A2" tags collide with the new planning doc's Track C "C1" (btree extraction). If both land, the planning-doc semantics must take precedence; PR descriptions may need disambiguation.
- **#138 / #139 / #140** — A8.1 step-4 documentation/cleanup PRs that should have landed alongside #133. Worth asking the user: should these merge, or are they superseded by this session's work?

## 6. Stale assumptions to retire

- **`SESSION_HANDOFF_2026-05-13-0014Z.md` §5 ("What's next")** — recommended writing `NEXT_STEPS_2026-05-13.md`. **Done** by PR #155. The handoff's §5–§8 are now superseded by this doc; §2–§4 (PRs #141–#153) remain accurate as the Phase-1/Phase-2 record.
- **`SESSION_HANDOFF_2026-05-13-0014Z.md` §4** ("`origin/archive/gemini-bulk-2026-05-13` — single-commit archive of `stash@{0}`") — slightly imprecise. The archive is a git-stash merge commit with three parents; **Subset 🟢 lives at parent `^3`** (`d9417a9`), not the bare ref. `git ls-tree origin/archive/gemini-bulk-2026-05-13 -- pkg/btree/` returns empty because the bare ref points at the working-tree-stash commit. Use `origin/archive/gemini-bulk-2026-05-13^3 -- <path>` for extraction. NEXT_STEPS_2026-05-13's Appendix captures this correctly; cross-references to the prior handoff should be read with this correction in mind.
- **`SESSION_HANDOFF_2026-05-13-0014Z.md` §7 ("Open questions")** — all three answered: (1) NEXT_STEPS_2026-05-13.md was written fresh; (2) Subset 🟢 extraction starts with btree (NEXT_STEPS_2026-05-13 Track C ordering); (3) the stacked-PR gotcha fold is queued as H5 (not yet done).
- **`CLAUDE.md`'s orient-first pointer** — updated by PR #155 to reference `docs/NEXT_STEPS_2026-05-13.md`. Auto-loaded for Claude Code agents; next session sees the right pointer on first read.
- **User's auto-memory: `feedback_stacked_pr_delete_branch_gotcha.md`** continues to hold (this turn's three PRs all targeted `main` directly; no stacking; `--delete-branch` was safe on each). No update needed.
- **User's auto-memory: `project_ci_red_state_tolerated.md`** continues to hold — all CI was pending at merge time for #155/#156/#157, but docs-only PRs cannot cause real regressions. No update needed.
- **Potential new auto-memory candidate**: "Agent subagents for parallel spike/design-doc work in shared checkout is safe — disjoint file writes don't race." This turn validated the pattern (F4 + S11 spikes drafted in parallel, ~10min, both with advisor calls, both load-bearing). Worth the user's consideration as a new `feedback_parallel_agent_*` entry, complementing the existing worktree-isolation rule that covers the implementation case.

## 7. Open questions for the user

1. **PRs #136–#140 disposition**: should the A8.1 step-4 cleanup (#138, #139, #140) merge, or are they superseded? Should the LSA improvements (#136, #137) coordinate naming with Track C of NEXT_STEPS_2026-05-13 (both use "C1" / "A2" tags meaning different things)?
2. **CLAUDE.md fold of the stacked-PR gotcha (H5)** — single-file PR, ~5 minutes of work, ready any time. Currently user-private memory only. Should this land before Track C starts, or batched in later?
3. **Linux CI fix priority** — split-race-target vs. bump-runner-timeout. Both are <1h work; bump-timeout is the lower-risk single-line change but doesn't address the root cause.

## 8. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-13-0124Z.md

Then read (in order):
  docs/NEXT_STEPS_2026-05-13.md (§ Sequencing graph + § Track H "Linux CI infra tax")
  CLAUDE.md § "Orient first" (auto-loaded; pointer updated to 2026-05-13 by PR #155)
  docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md (only if working on Track C)

Default next action: Linux CI infra fix as a single-PR unblock for
Track C signal. NEXT_STEPS_2026-05-13 § Track H names this; choose
split-race-target (root cause) vs. bump-runner-timeout (lower-risk
single-line). User input welcome on the trade-off.

After Linux CI lands: C1 (pkg/btree extraction from
origin/archive/gemini-bulk-2026-05-13^3) is the head of the critical
path. The Appendix in NEXT_STEPS_2026-05-13 has the exact extraction
commands. C1 is a pure new package — zero coupling, lowest risk.

Validation angle: this session validated parallel `Agent` subagent
dispatch for design-doc work in a shared checkout. Next session could
validate the implementation-parallel flow: spawn two `git worktree`s
for R1 + R2, run a Claude Code session in each via the graphdb-coord
skills (`work-claim`, `worktree-spawn`, `merge-coordinator`).
Document what worked / what didn't.

Pre-flight: confirm `git branch -a` shows `origin/archive/gemini-bulk-2026-05-13`.
Run `git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/btree/`
to verify access — the `^3` suffix is essential.

5 open PRs predate this session (#136–#140). Not this session's
responsibility; surface to the user if their disposition affects
your work (e.g., #136/#137 use "C1"/"A2" tag names that collide with
the new planning doc's Track C/A).

End-of-session: write a session handoff per CLAUDE.md § "Preparing a
new session (handoff convention)" via the session-handoff skill.
```

## 9. How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-05-13.md` (§ "Sequencing graph" is the orient-fast view; § "Appendix — extraction commands for Track C" is the operational reference for the next ~6 PRs).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded; pointer is correct after PR #155).
4. If picking up Track C: also `git ls-tree origin/archive/gemini-bulk-2026-05-13^3` to confirm Subset 🟢 file presence, and `docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md` § "Subset A" for which files are substantive vs. facade.
5. If picking up R1 or R2 implementation: read the corresponding spike doc (`F4_VECTOR_TENANT_REDESIGN.md` or `S11_AUTO_EMBEDDER_REDESIGN.md`) end-to-end before opening the first PR. The spike's "Final method signatures" + "PR breakdown" sections are the implementation contract.
6. If you find the prior handoff (`SESSION_HANDOFF_2026-05-13-0014Z.md`) authoritative for anything in §3–§8, cross-check against this doc first — the deltas in §6 above name what's been overtaken.
