# Session handoff — 2026-05-14 05:38 UTC

**Date**: 2026-05-14 (single session, 2 PRs merged — Track R verification component (1a) closed end-to-end across the verification doc AND the planning doc)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-14-0353Z.md` (`84a53ae`). The 0353Z handoff named "default next action: run count_scale_1000, append data, fix doc `-timeout` advice" — that action ran end-to-end this session, producing #212 + #213. 0353Z is historical from this point forward.

## 1. TL;DR

**Track R verification component (1a) is fully closed and reconciled across both the verification doc and the planning doc.** Per-tenant HNSW heap is **flat across the planning doc's full named tenant range (100 → 1000 tenants)** — ratio 1.000 to six significant figures. Decision 2's Option A bet (per-tenant HNSW in OSS) holds empirically. Two named follow-ups remain in `NEXT_STEPS_2026-05-15.md` § Verification gap: (1b) auto-embed observer load test, (1c) Docker/k8s deployment exercise — either is a valid next-session pick.

## 2. What's done this session

### Merged (2 PRs)

| PR | Title | Notes |
|---|---|---|
| #212 | `verify(storage): complete Track R 1a — count_scale_1000 ratio 1.000` | Merged as `2dde916`. +31/-19 single-file doc edit. Appended count_scale_1000 data point to `TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`: heap_bytes=3,463,237,704; per_tenant_bytes=3,463,237; ratio (1000/100) = 1.000 (raw 0.99994). Bumped reproduce-instruction `-timeout` advice from `1800s` → `3600s` (1800s was 83s below the actual total wall time on a fast machine). Two-commit PR squash-merged — the second commit fixed advisor-caught self-contradictions ("superlinear" header vs linear data; unverified "two machines" claim) before merge. |
| #213 | `docs(planning): mark Track R verification component (1a) done (#195/#209/#212)` | Merged as `9ac12ae`. +20/-10 single-file edit to `NEXT_STEPS_2026-05-15.md`. Applied the established line-95-99 "Reconciliation" pattern: kept historical framing intact, added "Reconciliation 2026-05-14 — component (1a) discharged" subsection citing all three PRs (#195 `d2172ae`, #209 `e718f87`, #212 `2dde916`). Labelled the three verification-gap bullets (1a)/(1b)/(1c) for cross-doc parity (labels existed in the verification doc + 0353Z handoff but not yet in this planning doc). Narrowed § Critical path option 1, § Decision 9 option A, § Risks, § Revisit triggers, and § How to use step 2 to point at (1b)/(1c) only. |

### Operational notes for next session

- **Trust-but-verify caught two near-misses this session.** (1) Spurious "Bash succeeded" notifications fired multiple times during the multi-minute bench wait — same pattern as 0353Z handoff documented. Discipline that works: `ps -ef | grep storage.test` + check log file size/contents; ignore the harness completion signal for backgrounded long-running tasks. (2) PR #195's merge SHA: I wrote `a0b6a98` from memory in the planning-doc draft; `gh pr view 195 --json mergeCommit.oid` returned `d2172ae`. Cross-checking before commit caught it. The general rule: when the doc names a specific commit SHA, run `gh pr view <N>` against the live source rather than typing from memory.
- **Advisor catch on the verification doc paid off.** First commit of #212 had a self-contradictory paragraph (header asserted "superlinear" while the body showed linear scaling 4.95× and 9.94× for 5× and 10× tenants) — carried over from the 0353Z handoff's extrapolation from a killed run. Advisor also flagged an unverified "two machines" claim (same user, same workspace; the cross-session ~50% wall-time variance is likely background-load, not separate hardware). Both fixed in a second commit before push. **General rule**: when you're about to ship empirical claims that were drafted from a stale source, re-verify against the data you just collected; advisor is well-placed to catch the carry-over.
- **The Reconciliation-subsection pattern is now load-bearing in `NEXT_STEPS_2026-05-15.md`.** Used three times in that doc (debt-discharge at line 95, R2.5b merge note at line 9, Track R (1a) at the new subsection). Pattern: keep the historical framing intact + add a small "Reconciliation YYYY-MM-DD" block citing the PRs that discharged it. The next session adding a (1b) or (1c) closure should follow the same shape; don't rewrite the original framing.
- **Bench wall-time is variable across sessions by ~50%** at the same scale. PR #209's session saw count_scale_500 ≈ 13 min; this session saw 8.9 min. Same user, same workspace, machine identity not separately tracked — likely background-load differences. Plan the `-timeout 3600s` conservatively for the same reason.

### Net new artifacts on main

- `docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`: +31/-19 (count_scale_1000 data + doc fixes via PR #212).
- `docs/NEXT_STEPS_2026-05-15.md`: +20/-10 (Reconciliation subsection + scoped narrowings via PR #213).
- `docs/internals/design/SESSION_HANDOFF_2026-05-14-0353Z.md`: prior handoff (historical from this point).
- `docs/internals/design/NEXT_SESSION_PROMPT.md`: will be overwritten by this handoff.

## 3. Current state

- `origin/main` HEAD: **`9ac12ae docs(planning): mark Track R verification component (1a) done (#195/#209/#212) (#213)`** — verified via `git log -1 origin/main`.
- **Open PRs** (1 total — pre-session carry-forward):
  - **#182** — `docs: session handoff — 2026-05-13 08:26 UTC`. Pre-session, untouched this session. Safe to leave or merge whenever.
- **Open local branches**:
  - `docs/session-handoff-2026-05-13-0826Z` (matches #182, intentional carry-forward)
  - This handoff's own branch will be added by the next commit.
  - This session's other branches (`verify/count-scale-1000-data-point`, `docs/planning-track-r-1a-closed`) auto-deleted on merge with `--delete-branch`.
- **Uncommitted changes on `main`**: none except `.claude/scheduled_tasks.lock` (runtime lock; ignored).
- **In-flight background work**: none. The bench completed cleanly in this session (1717.09s total wall, all four subtests PASS).
- **Test / lint state**: PRs #212 and #213 both passed all functional CI (golangci-lint, security scan, code coverage, go.mod check, tagged build with `-tags nng`, build binaries). macOS matrix tests were green for #212; for #213 they were still IN_PROGRESS at merge time (queued throttling) — accepted per the doc-only PR shape (no Go code changed). Benchmark workflows remained `UNSTABLE` per `CLAUDE.md` § "Known infra patterns" (benchmark comment-step permissions issue) — tolerated.

## 4. What's next

### (A) Track R verification gap closure — remaining components

Per `NEXT_STEPS_2026-05-15.md` § Verification gap (reconciled this session):

1. **(1b) auto-embed observer load test under production-shaped traffic.** O-1 structured logging shipped via PR #202; the bounded-pool drop path has never been exercised under sustained node-create load. Needs a harness that drives `POST /v1/nodes` with auto-embed enabled at a rate that exceeds the pool's drain rate. Lighter-touch than (1c) — single-process, no container.
2. **(1c) Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`.** End-to-end container build + env-driven bootstrap. The unit-test path through `pkg/api/server_init.go` (R2.5b) covers the bootstrap, but no real deployment exercises it. Larger scope than (1b).

Either is a valid pick. (1b) is the lighter follow-up; (1c) is the larger and more end-to-end.

### (B) Other live options from `NEXT_STEPS_2026-05-15.md` § Decision 9

- **(C) New audit angle** — performance under SaaS load (now correlated by Track R (1a) empirically across 100→1000 tenants), vector/embedding side-channels, productization audit for multi-node. Pick (C) only if (1b) and (1c) are explicitly deferred.

### New gaps surfaced this session (not yet on the planning doc)

- **None of substance.** This session executed the 0353Z handoff's named default and reconciled the planning doc. No new gaps surfaced; the bench wall-time variance observation is captured in the verification doc's "How to reproduce" section and doesn't warrant a planning-doc bullet.

## 5. Stale assumptions to retire

### `NEXT_STEPS_2026-05-15.md` — already reconciled this session via #213

Component (1a) was the only stale planning-doc framing as of the start of this session. PR #213 reconciled it. The next session should NOT need to update the planning doc for any (1a)-related claim — it's done.

### `TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md` — already updated this session via #212

The 0353Z handoff named two issues with this doc: (a) the count_scale_1000 row was marked `*(pending — see Limitations)*`, and (b) the "How to reproduce" section had `-timeout 1800s` which is what killed the prior session's bench. Both fixed by #212.

### `MEMORY.md` items (no change needed from this session)

The pre-existing items remain accurate. Nothing in this session contradicts any of them. The `project_ci_red_state_tolerated.md` and `project_ci_ubuntu_cancellation_pattern.md` items are still in their post-#181 state per the 0353Z handoff's §5 (no further narrowing needed this session).

## 6. Open questions for the user

None outstanding from this session. The 0353Z handoff's open questions either resolved during the work (Q1 settled on `-timeout 3600s`; Q2 settled on "keep 1000-tenant scenario, run manually") or remain low-urgency carry-forwards (Q3 spurious notifications — caught and worked around this session; Q4 emergent two-stage warmup — still emergent, didn't bite again).

## 7. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-14-0538Z.md

Then read (in order, only if relevant to your task):
  docs/NEXT_STEPS_2026-05-15.md § Verification gap
    (Component (1a) is discharged. Pick from (1b) or (1c) per § Critical path.)
  docs/internals/design/TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md
    (Reference for what (1a) closure looks like — useful template for
     (1b)/(1c) doc structure.)
  CLAUDE.md § "Orient first" + § "Known infra patterns" (auto-loaded)

Default next action — pick one:

  (1b) Auto-embed observer load test. Lighter-touch single-process harness.
       Drives POST /v1/nodes with auto-embed enabled at a rate that exceeds
       the bounded-pool drain rate; exercises O-1 logging (PR #202) under
       backpressure. ~1 session of work; outputs a verification doc plus
       optional test additions.

  (1c) Docker/k8s exercise of GRAPHDB_AUTO_EMBED_ENABLED. End-to-end
       container build + env-driven bootstrap. Larger scope; outputs a
       deployment recipe + verification doc.

Pre-flight:
  - confirm `gh pr list --state open` shows only #182 (intentional
    pre-session handoff carry-forward).
  - For (1b): expect to write a sustained-load harness; existing test
    file template at pkg/storage/vector_index_memory_test.go shows the
    structured-logging + skip-unless-env-set convention to follow.
  - For (1c): expect Dockerfile + compose work; check Makefile and the
    cmd/graphdb entrypoint for existing container hooks before
    designing.

Validation angle: cross-check any background-task "succeeded"
notifications against `ps` + the actual log file. The prior two sessions
both hit fabricated-completion notifications for long-running tasks.
Cheap; never skip.

End-of-session: write a session handoff per CLAUDE.md § "Preparing a new
session" via the session-handoff skill.
```

## 8. How to use this handoff

1. Read this first.
2. Then read `docs/NEXT_STEPS_2026-05-15.md` § Verification gap (already reconciled this session — quick read).
3. Then read `CLAUDE.md` § "Orient first" + § "Known infra patterns" (auto-loaded).
4. If picking up (1b): read `pkg/api/handlers_observer.go` (or the equivalent — `grep -r OnNodeCreated pkg/`) and PR #202's diff for the O-1 logging surface.
5. If picking up (1c): read `pkg/api/server_init.go` and the `R2.5b` PR (`gh pr view <NN>` once you've found the number — check `git log --grep=R2.5b`).
