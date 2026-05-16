# Session handoff — 2026-05-13 08:26 UTC

**Date**: 2026-05-13 (~20 min coda after `SESSION_HANDOFF_2026-05-13-0805Z.md`; one CI-config PR landed + the OAuth scope dance that unblocked it).
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-13-0805Z.md` (#180, merged at `ba83283`). That doc remains accurate for the Track C completion arc; this one extends with the #181 close-out and the OAuth-scope context the next agent should know.

## 1. TL;DR

The macOS-only CI matrix change (Track H Linux escalation candidate) is **live on main** as of `fdc048f`. Required a one-time OAuth scope fix: the user's `gh` CLI was logged in under a stale account name (`darragh-downey` → renamed to `dd0wney`) and lacked the `workflow` scope needed to push to `.github/workflows/test.yml`. Resolved via `gh auth logout` + `gh auth login -s workflow` (matching the post-rename handle in both CLI and browser). The 0805Z handoff captured everything else.

## 2. What's done this turn

| PR | Title | Notes |
|---|---|---|
| #180 | `docs: session handoff — 2026-05-13 08:05 UTC` | Merged at `ba83283`. Closed out the Track C completion arc. |
| #181 | `chore(ci): drop ubuntu-latest from test matrix; macOS-only (H/Linux CI)` | The local-only commit `6788ee6` finally landed (as `fdc048f`) after the OAuth scope fix below. |

## 3. Current state

- `origin/main` HEAD: `fdc048f chore(ci): drop ubuntu-latest from test matrix; macOS-only (H/Linux CI) (#181)`.
- **Open PRs from this turn**: NONE.
- **Local-only commits**: NONE (the macOS branch pushed + merged + deleted).
- **Open PRs predating the C-track arc** (still NOT touched): **11 PRs** — #108, #109, #110, #131, #134, #135, #136, #137, #138, #139, #140. Disposition still unresolved across three session-handoffs now. Third handoff to surface this; recommendation in §5 below.
- **Open local branches** (besides this handoff's): `docs/coord-learning-skills`, `feat/h4.3-followup-snapshot-tenant-index`, `feat/h4.3-replay-tenant-index`, `feat/h4.4-rest-blite-mirror`, `feat/lsa-bigrams-logentropy`, `feat/lsa-persistence`, `feat/lsa-quantize-docvecs` — all correspond to inherited open PRs.
- **Open worktrees**: only the main checkout.
- **Uncommitted changes on main**: none (except `.claude/scheduled_tasks.lock`).

## 4. Artifacts that survive this turn

### macOS-only test matrix (PR #181, `fdc048f`)

`.github/workflows/test.yml` lines 14-21 now show:

```yaml
matrix:
  go-version: ['1.23', '1.24', '1.25']
  # Ubuntu runners hit external-SIGTERM exit-143 on test-verbose/test-race
  # consistently (CLAUDE.md § "Known infra patterns"; NEXT_STEPS_2026-05-13.md
  # § Track H). macOS-only matrix until the contention root cause is addressed.
  # Build/tagged-build jobs below stay on ubuntu-latest (short-running; produce
  # Linux artifacts; not subject to the SIGTERM pattern).
  os: [macos-latest]
```

**Coverage / benchmarks / build / tagged-build-nng jobs still run on `ubuntu-latest`** — they don't hit the SIGTERM pattern (short-running, no matrix pressure). Build job specifically produces the `binaries-linux` artifact; cross-building from macOS would lie about the artifact name.

**CI signal post-merge**: future PRs should no longer see "Test on Go X / ubuntu-latest" red. The `mergeStateStatus: UNSTABLE` pattern documented in `CLAUDE.md` § "Known infra patterns" was driven by:

1. Linux runner exit-143 SIGTERM (NOW gone for the matrix `test` job; still possible in theory for the `coverage`/`benchmarks`/`build`/`tagged-build` jobs but they haven't historically hit it)
2. Benchmark comment-step permissions (UNCHANGED — still a tolerated red)

`UNSTABLE` may still appear if benchmarks fail, but the failure set should shrink substantially. CLAUDE.md § "Known infra patterns" bullets are now partially stale — see §6.

## 5. What's next

Carry-forward from the 0805Z handoff's §5, with no changes except:

- **Linux CI escalation** — DONE (this turn). The other two candidates listed in NEXT_STEPS_2026-05-13.md § Track H (matrix-breadth reduction, race-to-macOS) can be skipped or stacked if exit-143 reappears in the non-matrix jobs.
- Everything else carries forward verbatim.

The critical-path queue (next session):
- **Track R** — R1 (F4 vector ops redesign), R2 (S11 auto-embedder redesign), R3 (S1 closure). R1 and R2 are parallel-eligible.
- **Fresh planning checkpoint** — `NEXT_STEPS_2026-05-13.md` is now heavy with strike-throughs from Track C completion + this CI change. The 0805Z handoff §7.4 already flagged this; still recommended.
- **11 inherited PRs disposition** — third handoff asking. Concrete recommendation below.

### Concrete recommendation for the 11 inherited PRs

Three carry-forward cycles is enough. Suggested ladder for the next session:

1. **Bulk-check CI/mergeability**: `for pr in 108 109 110 131 134 135 136 137 138 139 140; do gh pr view $pr --json title,mergeable,mergeStateStatus --jq '"\(.title) | \(.mergeable) | \(.mergeStateStatus)"'; done`. Sort into "still-mergeable" vs "needs-rebase" vs "stale-broken".
2. **For still-mergeable + still-relevant**: a session-end batch merge wave. Most are docs/cleanup PRs (A8.1 step-4 series #134/#138/#139/#140) — low risk.
3. **For "still-mergeable but stale-name" (#136, #137)**: the `A2`/`C1` tag collision with the new planning doc's Track A/C — needs rename in commit message OR a comment acknowledging the collision before merge.
4. **For "stale-broken"** (if any): close with a comment explaining the stale state. Don't let them sit forever.

This is a single-session task (~30-60 min) that retires a long-standing carry-forward debt. The 0805Z handoff §7.3's "park indefinitely" recommendation is still valid as a fallback if the bulk-triage is too costly.

## 6. Stale assumptions to retire

### `CLAUDE.md` § "Known infra patterns" — Linux exit-143 bullet

Currently:

> **CI Ubuntu jobs (both `test-verbose` AND `test-race`) consistently exit 143 with `runner has received a shutdown signal`.** [...]

After #181 the `test-verbose` and `test-race` steps **only run on macOS** (the matrix-job context). The bullet should be updated to reflect that the matrix `test` job no longer hits this pattern, but the non-matrix Linux jobs (`coverage`, `benchmarks`, `build`, `tagged-build-nng`) still could in theory.

Suggested replacement (single-PR doc update):

> **Historical: CI Ubuntu matrix-test jobs (`test-verbose`, `test-race`) consistently exited 143 with `runner has received a shutdown signal`.** Closed 2026-05-13 by PR #181 moving the `test` job's matrix to macOS-only. The cause was external SIGTERM (NOT internal `go test -timeout`, NOT race-detector OOM — PR #159 ruled out OOM); likely account-level concurrent-job contention. **Non-matrix Linux jobs (coverage, benchmarks, build, tagged-build-nng) still run on ubuntu-latest** and could theoretically hit the same pattern under heavy contention, but historically haven't because they're short-running and unparalleled. Re-investigate if they start failing.

### `CLAUDE.md` § "Known infra patterns" — UNSTABLE state bullet

Currently:

> **`mergeStateStatus: UNSTABLE`** is the normal state for green PRs in this repo (because of the two known-infra failures above). Verify the failure set matches the expected pattern before merging; net-new failures need investigation.

After #181, UNSTABLE should be RARER. Update suggestion:

> **`mergeStateStatus: UNSTABLE`** can still happen for green PRs in this repo because of the benchmark comment-step permissions issue (still open). The matrix-test Linux exit-143 SIGTERM cause is now closed (PR #181). Verify the failure set matches the expected pattern before merging.

### User-private memory `project_ci_red_state_tolerated.md`

The 0531Z handoff §6 already flagged that this memory has the wrong "concurrency cancel-in-progress" framing. After #181, the memory needs a further update: matrix-test Linux red state should no longer be tolerated as "infra pattern" — it shouldn't happen now.

### OAuth account-rename context (NEW — load-bearing for any future agent dealing with auth)

The user's GitHub account was renamed `darragh-downey` → `dd0wney` at some prior point. The local `gh` CLI had cached the old login name. Symptoms:

- `gh auth status` showed `darragh-downey` even though the user identifies as `dd0wney`
- `gh auth refresh -s workflow` failed with: `error refreshing credentials for darragh-downey, received credentials for dd0wney, did you use the correct account in the browser?`
- Token lacked `workflow` scope by default

**Fix that worked**: `gh auth logout` + `gh auth login -s workflow` (the `-s workflow` is essential; default `gh auth login` doesn't include it). After: `gh auth status` shows `dd0wney` with `workflow` in scopes.

**The agent's OAuth still lacks workflow scope** — that's separate from the user's `gh` CLI. Workflow-file changes (anything under `.github/workflows/`) still require a user push from their shell.

Worth adding to CLAUDE.md § "Known pitfalls" as a single-line bullet (if it bites a third time) — something like:

> Workflow-file edits require `workflow` OAuth scope, which the agent's token lacks by design. User pushes from their own shell. If their `gh` CLI also lacks workflow scope: `gh auth refresh -s workflow` (re-auth their CLI). If their stored login name is stale from a GitHub username rename: `gh auth logout` + `gh auth login -s workflow`.

Not yet at "third time" so the bullet isn't load-bearing yet — but the next handoff captures it so the cost of re-deriving stays small.

### `NEXT_STEPS_2026-05-13.md` § Track H Linux CI infra tax

This section now describes a closed problem. The three escalation candidates are reduced to one shipped (macOS-only) and two still-on-the-table-if-needed (matrix-breadth reduction, race-to-macOS). Update at the next planning checkpoint.

## 7. Open questions for the user

Same set as the 0805Z handoff §7 minus the now-closed items:

1. ~~Confirming #179 already merged~~ — DONE in 0805Z window.
2. ~~Push the macOS-only branch~~ — DONE this turn (#181).
3. **Disposition of 11 inherited PRs** (#108–#140) — third session asking. See §5 for concrete recommendation.
4. **Next planning checkpoint** — fresh `NEXT_STEPS_<NEXT-DATE>.md`, or skip and start R1/R2?
5. **Track R + graphdb-coord parallel-agent coordination** — testing time?

## 8. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-13-0826Z.md

Then read (in order, only if relevant to your task):
  docs/internals/design/SESSION_HANDOFF_2026-05-13-0805Z.md (the precursor; Track C completion arc)
  docs/NEXT_STEPS_2026-05-13.md (Track C all-closed; Track R is the next critical path; § Track H Linux CI now closed via #181)
  CLAUDE.md § "Orient first" + § "Known pitfalls" + § "Known infra patterns" (auto-loaded; the latter two have stale bullets — see §6 of this handoff)
  docs/internals/design/F4_VECTOR_TENANT_REDESIGN.md (if picking up R1)
  docs/internals/design/S11_AUTO_EMBEDDER_REDESIGN.md (if picking up R2)

Default next action — THREE PATHS (ranked by leverage):

  (A) Fresh planning checkpoint. NEXT_STEPS_2026-05-13.md is now heavy with
  strike-throughs from Track C + #181. Open NEXT_STEPS_2026-05-XX.md from
  scratch covering Track R (R1/R2/R3), what closed (Linux CI), and the 11
  inherited PRs disposition decision. Single-file PR. Clears the decking.

  (B) Triage the 11 inherited PRs (#108-#140). Third session carrying these
  forward; bulk-check mergeability, sort into batch-merge vs close-stale.
  See §5 of this handoff for the concrete ladder. ~30-60 min.

  (C) Start R1 or R2 directly. Both spike docs landed (#156, #157);
  implementation is ready. R1 and R2 touch disjoint surfaces, so they're
  parallel-eligible via the graphdb-coord sibling repo skills. R3 (S1
  closure) waits on both.

Pre-flight (regardless of path):
  - confirm `git ls-tree HEAD .github/workflows/test.yml` shows the macOS-only
    matrix landed (the matrix `os` line should be `[macos-latest]`).
  - if picking R1 or R2: read the corresponding spike doc FIRST and resolve the
    "decide" decision points (per-tenant HNSW vs shared+filter for R1; default
    embedder backend for R2) before implementing.
  - if picking the 11-PR triage: the LSA PRs #136/#137 use A2/C1 tags that
    collide with the new Track A/C semantics; rename or comment before merge.

Stale assumptions to retire BEFORE acting on CI / infra:
  - CLAUDE.md § "Known infra patterns" Linux exit-143 bullet is now PARTIAL —
    matrix test job no longer hits it (PR #181); non-matrix Linux jobs still
    could in theory. See §6 of this handoff for suggested replacement.
  - CLAUDE.md § "Known infra patterns" UNSTABLE bullet — the cause set shrunk
    to "benchmark comment-step permissions only" post-#181.
  - User-private memory project_ci_red_state_tolerated needs an update —
    matrix-test red state should no longer be tolerated.

End-of-session: write a session handoff per CLAUDE.md § "Preparing a new session"
via the session-handoff skill.
```

## 9. How to use this handoff

1. Read this first.
2. Read 0805Z handoff (precursor) only if you need the Track C completion context.
3. If picking up planning checkpoint: §5 + §6 + §7 of this doc are the source material for the fresh NEXT_STEPS doc.
4. If picking up the 11-PR triage: §5's ladder + the bulk-check command are your starting point.
5. If picking up R1/R2: read the spike doc; the implementation is independent of this handoff.
6. If the OAuth account-rename bites again: §6's "Fix that worked" sub-section is the recipe.
