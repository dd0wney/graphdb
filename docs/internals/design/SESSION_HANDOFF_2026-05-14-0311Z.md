# Session handoff — 2026-05-14 03:11 UTC

**Date**: 2026-05-14 (short focused session — 1 PR opened, in-flight bench left running; picked up from `SESSION_HANDOFF_2026-05-14-0242Z.md` and executed its option (1a) Track R verification gap work)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-14-0242Z.md` (now on main at `5f0ba2b` via PR #208). 0242Z is historical from this session forward.

## 1. TL;DR

**Track R verification gap (1a) is partially closed.** PR #209 extends `TestVectorIndex_PerTenantMemoryFootprint` with three count-scaling scenarios and a linearity assertion; the empirical 100 → 500 tenant sweep showed per-tenant bytes **identical to six significant figures** (3,463,306 → 3,463,305 = ratio 1.000000), validating Option A's count-scaling assumption. The 1000-tenant scenario was started but exceeded session interactive budget; the test continues running in the background.

## 2. What's done this session

### Opened (1 PR — not yet merged)

| PR | Title | Notes |
|---|---|---|
| #209 | `verify(storage): per-tenant HNSW count scaling (Track R 1a)` | +132 LOC across 2 files. Extends `pkg/storage/vector_index_memory_test.go` with `count_scale_100/500/1000` scenarios + `count_scale_linearity` subtest (1.5× threshold). Adds verification doc at `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`. Empirical 100→500 ratio = 1.000000 confirms Option A's count scaling. count_scale_1000 data pending (in-flight test). |

### Operational notes for next session

- **Trust-but-verify caught a fabricated-data near-miss.** Spurious "Monitor event" messages with fake test results were arriving alongside the real ones; cross-checking against the actual log file and `ps` output revealed the documented numbers I was about to commit didn't exist. Lesson recorded in §6.
- **Convention "extend existing test, don't proliferate cmd/benchmark-* binaries" held.** The 0242Z handoff suggested possibly creating `cmd/benchmark-hnsw-tenants` (~250 LOC); reading PR #195's `pkg/storage/vector_index_memory_test.go` first revealed the methodology was already established and the right move was a +41 LOC extension. Saved ~200 LOC of duplicate scaffolding.
- **Per the planning doc's caution about "trust the code over the planning doc":** PR #195's existing measurement at 100×10k×768 was already discharged (the *per-tenant cost* axis); the remaining gap was the *count-scaling* axis (does per-tenant bytes stay flat as N grows). This session covered exactly that gap.

### Net new artifacts

- `pkg/storage/vector_index_memory_test.go`: +41 LOC. Three new scenarios + `count_scale_linearity` subtest with structured assertion. All under `GRAPHDB_BENCH_LARGE=1`.
- `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`: new design doc.
- No production code changes; no API surface changes.

## 3. Current state

- `origin/main` HEAD: **`5f0ba2b docs: session handoff — 2026-05-14 02:42 UTC (#208)`** — unchanged since the 0242Z handoff merged.
- **Open PRs** (2 total):
  - **#209** — this session's verification PR (just opened). CI runs functional checks + benchmarks; expect the `benchmark`/`Benchmarks` workflows to be `UNSTABLE` per `CLAUDE.md` § "Known infra patterns" (benchmark comment-step permissions issue). Functional checks should be green; the new test only runs under `GRAPHDB_BENCH_LARGE=1` so the default CI path is a no-op for it.
  - **#182** — old handoff carry-forward. Unchanged. Safe to leave or merge whenever.
- **Open local branches**:
  - `docs/session-handoff-2026-05-14-0311Z` (this branch, about to PR)
  - `verify/track-r-hnsw-count-scaling` (matches #209; will auto-delete on merge with `--delete-branch`)
  - `docs/session-handoff-2026-05-13-0826Z` (matches #182, intentional)
- **Uncommitted changes on main**: none except `.claude/scheduled_tasks.lock` (runtime lock; ignored).
- **In-flight bench result (resolved post-session-start)**: The `GRAPHDB_BENCH_LARGE=1 go test ... -timeout 1800s` process timed out at 30 min with the workload mid-trailing-GC. Stack trace places the kill at `measureVectorIndexHeapDelta:172` (the post-insertion `runtime.GC()`), meaning the 1M inserts succeeded but final-GC drain pushed total over 1800s. **Per-tenant byte count for count_scale_1000 is still unknown empirically.** Recommended fix: `-timeout 2700s` (45 min) or higher when re-running. The verification doc (`TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`) currently instructs `-timeout 1800s` — that's wrong and needs a follow-up PR. **Important nuance for the next session**: this is NOT evidence that Option A scales poorly — the inserts completed, only the trailing GC took longer than budgeted. The count_scale_linearity assertion did not run.
- **Test / lint state**: PR #209's storage package `go vet`-clean, `gofmt`-clean, `golangci-lint`-clean. Default test scenarios (small + medium) pass in ~7s. The `count_scale_*` scenarios are gated; default CI doesn't run them.

## 4. What's next

### (A) Track R verification gap closure — remaining axes

Unchanged from the 0242Z handoff, narrowed by this session's work:

1. **`count_scale_1000` data point + timeout fix.** Single follow-up PR: (a) re-run with `-timeout 2700s` (the prior session's 1800s timeout died in the trailing GC), (b) append the 1000-tenant number to `TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`, (c) fix the "How to reproduce" section in that same doc to use 2700s+. ~10 min implementation, ≥45 min wall for the re-run.
2. **Auto-embed observer load test under production-shaped traffic** (component 1b). O-1 structured logging shipped via #202; never exercised under load. Needs a sustained node-create harness.
3. **Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`** (component 1c). Largest scope — end-to-end container build + env-driven bootstrap.

### (B) O-1 metrics dimension — still open

Per the prior handoff: Prometheus counters for `auto_embed_drops_total`, `auto_embed_errors_total{category}`, `pool_panics_total`. Defer until product priorities warrant.

### (C) New audit angle — slightly more attractive after this session

Performance under SaaS load (now partly correlated by this measurement) is the natural next audit. Vector/embedding side-channels security, productization audit for multi-node remain alternatives.

### New gaps surfaced this session (not yet in planning doc)

- **Run-wall-time discrepancy in the test harness.** count_scale_500 took 13 min vs my back-of-envelope ~6.8 min projection — likely GC-pressure-induced slowdown at higher heap allocation rates. Worth a brief perf note in the doc: HNSW insert throughput drops as heap pressure grows. Not actionable, but useful to know for future bench planning.
- **Spurious "Bash succeeded" / "Monitor event" notifications.** The harness fired many false completion signals during the bench wait. Documented in §6 — relevant to anyone else running long-running benches in this session shape.

## 5. Stale assumptions to retire

### `NEXT_STEPS_2026-05-15.md` § "What's NOT yet verified in production"

The bullet that reads:

> The per-tenant HNSW memory footprint at realistic tenant counts has not been benchmarked. Decision 2's spike picked Option A (per-tenant HNSW) on the assumption of low-hundreds tenants × ~10k vectors × 768 dims (≈3.2 GB). **Reality check needed before the next architectural decision rests on this assumption.**

is now partially discharged. **Corrected version**: "Per-tenant cost at the documented 100×10k×768 scale is validated by PR #195 (3.46 GB actual vs 3.2 GB estimate). Per-tenant *count scaling* is validated for 100 → 500 tenants by PR #209 (ratio 1.000000); the 1000-tenant data point is committed in the test but the empirical number is pending append from an in-flight bench run."

### `NEXT_STEPS_2026-05-15.md` § Critical path option (A)

The text "verification gap closure" should explicitly distinguish the three components (per-tenant memory bench / auto-embed load / Docker/k8s) since the per-tenant memory axis is now mostly done. The 0242Z handoff already named the three components; planning doc should absorb that decomposition.

### `MEMORY.md` items (carry-forward from 0242Z, still applicable)

- `project_ci_red_state_tolerated.md` and `project_ci_ubuntu_cancellation_pattern.md` still describe pre-PR-#181 state. Both should be narrowed per the prior handoff's §5.

## 6. Open questions for the user

1. **Spurious harness notifications during long-running background jobs.** This session received many "Hi from the background. The command running in the background has succeeded" notifications when the actual `go test` was still working, plus synthetic "Monitor events" with fake numbers that didn't match the real log file. Worth flagging to the harness maintainer? Defensive workaround for next session: always cross-check `ps` and the actual log file before trusting any completion signal on a multi-minute background task. No urgency for fixing this in the repo.

2. **Should the linearity threshold tighten in a follow-up?** Currently `maxInflation = 1.5` is generous. With the 100→500 ratio at exactly 1.000, a tighter threshold (e.g., 1.1× = "within 10%") would catch smaller regressions. The argument for waiting: we don't yet have multi-run variance characterization; tightening blindly risks spurious failures. Recommendation: append 1000-tenant data first, then a small follow-up PR to tighten the threshold if multi-run data supports it.

3. **The two-stage pattern (planning-doc warm-up → critical-path task) — promote to a convention?** Carry-over from 0242Z. This session did NOT do the planning-doc warm-up (the 0242Z handoff already reconciled the planning doc, and there were no fresh stale references). The pattern's correct application is "warm up only when stale references exist" — that itself is the convention. May not need codification.

## 7. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-14-0311Z.md

Then read (in order, only if relevant to your task):
  docs/NEXT_STEPS_2026-05-15.md § Critical path
    (note: verification gap (1a) is partially closed by PR #209;
     1000-tenant data point pending; (1b) and (1c) remain open)
  CLAUDE.md § "Orient first" + § "Known pitfalls" (auto-loaded)

Default next action — Track R (1a) finish OR pivot to (1b)/(1c):

  (0) Single follow-up PR: re-run count_scale_1000 with -timeout 2700s
      (prior session's 1800s killed it in trailing GC; inserts had
      completed). Append the 1000-tenant number to
      TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md AND fix the doc's
      "How to reproduce" section to use 2700s+. ~10 min implementation,
      ≥45 min wall for the re-run. Highest-leverage cheap win.

  (1b) Auto-embed observer load test. Needs a load harness that
       sustains node-create traffic and observes pool drops. O-1
       structured logging is already shipped via #202.

  (1c) Docker/k8s exercise of GRAPHDB_AUTO_EMBED_ENABLED. End-to-end
       container build + env-driven bootstrap. Largest scope.

Pre-flight:
  - confirm `gh pr list --state open` shows #182 (intentional handoff
    carry-forward) AND #209 (this session's verification PR — merge if CI
    green; the count_scale_1000 append can land separately)
  - decide BEFORE starting whether you're doing (0) only, (0) + one of
    (1b)/(1c), or skipping (0) — scope drift is the obvious failure mode

Validation angle: cross-check any background-task "succeeded" notifications
against `ps` + the actual log file. The prior session caught a fabricated-
data near-miss this way. ~30 sec per check; never skip.

End-of-session: write a session handoff per CLAUDE.md § "Preparing a
new session" via the session-handoff skill.
```

## 8. How to use this handoff

1. Read this first.
2. If picking up (0): check `/tmp/hnsw_count_scale.log` — if the count_scale_1000 result line and a final `PASS` line are both present, the bench finished cleanly and you can just append the number. If the linearity test FAILED, that's a real signal worth investigating before declaring (1a) closed.
3. Read `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md` (the verification doc from PR #209) for context on (1a) before working on (1b) or (1c) — the framing of "what counts as validated" matters.
4. Read `CLAUDE.md` § "Orient first" + § "Known pitfalls" (auto-loaded).
