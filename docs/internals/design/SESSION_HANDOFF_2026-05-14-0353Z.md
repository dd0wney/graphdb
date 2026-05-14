# Session handoff — 2026-05-14 03:53 UTC

**Date**: 2026-05-14 (focused session continuation — Track R verification gap (1a) closed at 100→500 scale; 1000-tenant data point pending due to in-flight bench timeout)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-14-0311Z.md` (now on main at `711f367` via PR #210). 0311Z is historical from this point forward — it was authored mid-session with PRs still open; this 0353Z handoff captures the closed state and incorporates the bench timeout finding from after the session-handoff-PR was opened.

## 1. TL;DR

**Track R verification gap (1a) is partially closed and now on main.** PR #209 landed: per-tenant bytes are **identical to six significant figures** between 100 → 500 tenants (3,463,306 → 3,463,305 = ratio 1.000000), validating Option A's count-scaling assumption at the tested scale. The 1000-tenant scenario hit a 30-min Go test timeout in the *trailing GC after inserts completed* — count_scale_1000 is committed in the test code but its empirical byte count is still pending. Recommended fix: re-run with `-timeout 2700s` (45 min).

## 2. What's done this session

### Merged (2 PRs)

| PR | Title | Notes |
|---|---|---|
| #209 | `verify(storage): per-tenant HNSW count scaling — 100→500 ratio 1.000 (Track R 1a)` | Merged as `e718f87`. +132 LOC across 2 files. Extends `pkg/storage/vector_index_memory_test.go` with `count_scale_100/500/1000` scenarios + `count_scale_linearity` subtest (1.5× threshold). Adds verification doc at `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`. Empirical 100→500 ratio = 1.000000. count_scale_1000 data pending. |
| #210 | `docs: session handoff — 2026-05-14 03:11 UTC` | Merged as `711f367`. Captures the verification-PR findings, the trust-but-verify near-miss with fabricated-data harness notifications, and the bench timeout finding (added after the PR was opened, before merge). Two-commit PR squash-merged. |

### Operational notes for next session

- **Trust-but-verify caught a fabricated-data near-miss this session.** Spurious "Bash succeeded" notifications and synthetic "Monitor event" injections arrived alongside real ones during a multi-minute background test. Cross-checking against `ps` and the actual log file revealed the documented numbers I was about to commit didn't exist. The discipline is: never commit numbers without verifying against the underlying file/process. Cost: ~30 sec per check. Avoided cost: fabricated empirical claims in a verification doc.
- **Convention "extend existing test, don't proliferate cmd/benchmark-* binaries" held.** The 0242Z handoff suggested possibly creating `cmd/benchmark-hnsw-tenants` (~250 LOC); reading PR #195's existing test first revealed the methodology was already there and the right move was a +41 LOC extension.
- **macOS CI matrix tests are queue-throttled on GitHub-hosted runners.** Multi-PR sessions hit this — #210's matrix tests took ~25 min to *start* on the second commit even though all other functional checks passed in ~1-2 min. Plan total CI wall time at ~30 min per PR if merging back-to-back; budget for it explicitly rather than assuming the 7-min execution time.
- **The "Bash succeeded" notification stream is unreliable for long-running tasks.** False-positive completion notifications fired many times during the bench wait. Defensive practice: cross-check `ps` and the task's output file before trusting any single signal.

### Net new artifacts on main

- `pkg/storage/vector_index_memory_test.go`: +41 LOC (count_scale scenarios + linearity assertion)
- `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`: new design doc
- `docs/internals/design/SESSION_HANDOFF_2026-05-14-0311Z.md`: prior handoff (historical from this point)
- `docs/internals/design/NEXT_SESSION_PROMPT.md`: overwritten with the 0311Z prompt (will be overwritten again by this 0353Z handoff)

## 3. Current state

- `origin/main` HEAD: **`711f367 docs: session handoff — 2026-05-14 03:11 UTC (#210)`** — verified via `git log -1 origin/main`.
- **Open PRs** (1 total — pre-session carry-forward):
  - **#182** — old handoff. Unchanged. Safe to leave or merge whenever.
- **Open local branches**:
  - `docs/session-handoff-2026-05-14-0353Z` (this branch — about to PR)
  - `docs/session-handoff-2026-05-13-0826Z` (matches #182, intentional)
  - This session's other two branches (`verify/track-r-hnsw-count-scaling`, `docs/session-handoff-2026-05-14-0311Z`) auto-deleted on merge with `--delete-branch`.
- **Uncommitted changes on main**: none except `.claude/scheduled_tasks.lock` (runtime lock; ignored).
- **In-flight background work**: none. The `GRAPHDB_BENCH_LARGE=1 go test` that was running earlier hit the 30-min Go test timeout and was killed by the runtime. Stack trace placed the kill in the post-insertion `runtime.GC()` at `measureVectorIndexHeapDelta:172`, meaning the 1M inserts completed but the final GC drain pushed total wall time over 1800s. No data persisted from that run.
- **Test / lint state**: PR #209 + PR #210 both passed all functional CI checks (matrix tests on Go 1.23/1.24/1.25 macOS, lint, security, coverage, tagged build, Build Binaries). `benchmark`/`Benchmarks` workflows remained pending/UNSTABLE per `CLAUDE.md` § "Known infra patterns" (benchmark comment-step permissions issue) — tolerated.

## 4. What's next

### (A) Track R verification gap closure — remaining axes

Same three components as the 0311Z handoff named, but (1a) now narrows to a specific single-PR follow-up:

1. **`count_scale_1000` data point + reproduce-instructions fix.** Single follow-up PR: (a) re-run `GRAPHDB_BENCH_LARGE=1 go test -v -run 'TestVectorIndex_PerTenantMemoryFootprint/count_scale' -timeout 2700s ./pkg/storage/` (≥45 min wall), (b) append the 1000-tenant number to `TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md` (~3 lines: table row, remove "pending" caveat, update conclusion), (c) fix the "How to reproduce" section in the same doc — it currently instructs `-timeout 1800s`, which is the timeout that just killed the bench. Single-file PR if no test-code changes needed; minimal-diff PR if also tightening the `-timeout` advice in the test docstring.
2. **Auto-embed observer load test under production-shaped traffic** (component 1b). O-1 structured logging shipped via #202; never exercised under load. Needs a sustained node-create harness.
3. **Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`** (component 1c). Largest scope — end-to-end container build + env-driven bootstrap.

### (B) O-1 metrics dimension — still open

Per prior handoffs: Prometheus counters for `auto_embed_drops_total`, `auto_embed_errors_total{category}`, `pool_panics_total`. Defer until product priorities warrant.

### (C) New audit angle — slightly more attractive after this session

Performance under SaaS load (now partly correlated by this measurement) is the natural next audit. Vector/embedding side-channels security, productization audit for multi-node remain alternatives.

### New gaps surfaced this session (not yet in planning doc)

- **Bench wall time scales worse than linear with tenant count.** count_scale_100 ≈ 78s; count_scale_500 ≈ 13 min (vs naive 5× extrapolation of 6.5 min). Likely GC pressure as heap grows. count_scale_1000 needed >30 min just for inserts + trailing GC. Implication: the verification doc's "How to reproduce" estimate was wrong; the 1.5× linearity check works on per-tenant *memory* (which is flat) but the *runtime* growth makes the test harder to fit in a single CI run. Worth a note in the planning doc that "Option A is memory-linear but wall-time superlinear at 1000+ tenants" — relevant for any future continuous-bench planning.

## 5. Stale assumptions to retire

### `NEXT_STEPS_2026-05-15.md` § "What's NOT yet verified in production"

Same as 0311Z — needs the same edit (mark per-tenant cost + 100→500 count scaling as discharged via #195 and #209; flag 1000-tenant data point as pending). The 0311Z handoff already named this; the next session can land the planning-doc update via the `planning-doc-update` skill.

### `TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md` § "How to reproduce"

**New stale assumption from this session.** The doc currently says `-timeout 1800s` (30 min). That's the timeout that just killed count_scale_1000 in the trailing GC. Recommend updating to `-timeout 2700s` (45 min) or `-timeout 3600s` (60 min) for safety. This is what the next session's first PR should fix alongside appending the empirical 1000-tenant number.

### `MEMORY.md` items (carry-forward from 0242Z, still applicable)

- `project_ci_red_state_tolerated.md` and `project_ci_ubuntu_cancellation_pattern.md` still describe pre-PR-#181 state. Both should be narrowed per the 0242Z handoff's §5.

## 6. Open questions for the user

1. **Should the verification doc's `-timeout` advice include a per-scenario breakdown?** Options: (a) Single global `-timeout 3600s` recommendation (simplest, conservative); (b) Per-scenario breakdown — small ~10s, medium ~10s, spike_estimate ~15m, count_scale_100 ~2m, count_scale_500 ~15m, count_scale_1000 ~45m. The breakdown is more useful but introduces drift maintenance. Recommend (a) for now; the test code itself doesn't enforce per-scenario timeouts.

2. **Should `count_scale_1000` be reduced to a smaller scenario** (e.g., 700 tenants) that fits in 30 minutes? Trade-off: the 1000-tenant *target* is what the user explicitly named in the handoff, but if it takes 45+ min to run, it'll never run in CI and exists only as a manual-bench scenario. Two reasonable answers: (a) keep at 1000, accept that it's only run manually; (b) reduce to 700 to fit in 30 min, document the smaller upper bound. Recommend (a) since the count_scale_500 already shows ratio 1.000 — the 1000-tenant data is "completeness" not "different signal."

3. **Spurious harness notifications during long-running background jobs.** This session received many false "Bash succeeded" notifications and synthetic "Monitor event" injections. Worth flagging to the harness maintainer if a feedback channel exists. Defensive workaround: always cross-check `ps` + log file. No urgency for fixing in the repo.

4. **Carry-overs from 0242Z**: (a) `allow_auto_merge` setting still off (recommend re-asking only if a future session has a stacked cohort); (b) two-stage warm-up pattern still emergent rather than codified (this session didn't need it, supporting "leave emergent until it bites the third time").

## 7. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-14-0353Z.md

Then read (in order, only if relevant to your task):
  docs/NEXT_STEPS_2026-05-15.md § Critical path
    (note: verification gap (1a) is partially closed — 100+500 measured via PR #209;
     1000-tenant data point and the verification-doc reproduce-instructions are
     the named first task. (1b) and (1c) remain open.)
  docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md
    (the just-merged verification doc; the "How to reproduce" needs the
     -timeout fix described in §5 of the handoff)
  CLAUDE.md § "Orient first" + § "Known pitfalls" (auto-loaded)

Default next action — single-PR follow-up to close (1a) fully:

  (0) Re-run count_scale_1000 with -timeout 2700s. Append the 1000-tenant
      data point to TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md
      (single-line table row + remove the "pending" caveat) AND fix the
      doc's "How to reproduce" section, which currently has -timeout 1800s
      (the timeout that just killed the bench in trailing GC). Expect
      ≥45 min wall time for the re-run; ~10 min implementation.

After (0) — pick from:

  (1b) Auto-embed observer load test. Needs a sustained node-create harness
       to exercise the O-1 logging shipped via #202.

  (1c) Docker/k8s exercise of GRAPHDB_AUTO_EMBED_ENABLED. End-to-end
       container build + env-driven bootstrap. Largest scope.

Pre-flight:
  - confirm `gh pr list --state open` shows only #182 (intentional handoff
    carry-forward).
  - Plan ~30 min CI wall time per PR if merging back-to-back — macOS Go
    matrix tests are queue-throttled on GitHub-hosted runners.

Validation angle: cross-check any background-task "succeeded" notifications
against `ps` + the actual log file. The prior session caught a fabricated-
data near-miss this way. Cheap; never skip.

End-of-session: write a session handoff per CLAUDE.md § "Preparing a
new session" via the session-handoff skill.
```

## 8. How to use this handoff

1. Read this first.
2. If picking up (0): the test code on main already includes `count_scale_1000`; the only changes needed are (a) running the bench, (b) appending the data, (c) fixing the doc's `-timeout` instruction. No new test scenarios needed.
3. Read `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md` (the verification doc from PR #209) for context on the methodology before touching the doc.
4. Read `CLAUDE.md` § "Orient first" + § "Known pitfalls" (auto-loaded).
