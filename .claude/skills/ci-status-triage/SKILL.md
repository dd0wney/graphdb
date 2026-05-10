---
name: ci-status-triage
description: Triage a PR's CI failures into known-infra-tolerated vs net-new-needs-investigation, and recommend merge/hold/investigate. Use when the user asks "is this PR mergeable," "check CI status on #N," "should I merge despite UNSTABLE," "triage CI failures," or before any merge in this repo (the repo's normal merge-state for green PRs is UNSTABLE because of two known-infra failures, so manual eyeballing is required for every merge). Outputs a categorized failure list and a merge recommendation; doesn't modify anything.
---

# CI status triage

For each failing check on a PR: classify as known-infra-tolerated or net-new-needs-investigation. Recommend merge / hold / investigate based on the classification.

## When to invoke

- Before merging any PR (this repo's `mergeStateStatus: UNSTABLE` is the normal state — see `CLAUDE.md` § "Known infra patterns").
- When CI shows red and the user wants to know if it's a real regression.
- When a check has been in-progress for a long time and the user wonders if it's hung.

## Known infra patterns (the tolerated set)

These failure patterns are documented in `CLAUDE.md` § "Known infra patterns" and have been tolerated across PRs #65–#76:

| Pattern | Signal | Cause | Action |
|---|---|---|---|
| Ubuntu `Test on Go <ver> / ubuntu-latest` exit 143 | "runner has received a shutdown signal" in logs; `make: *** [Makefile:57: test-race] Terminated` | `make test-race`'s 10-minute timeout against the runner's idle-timeout budget; macOS runs pass | Tolerate. Don't re-investigate without new evidence. |
| `benchmark` (Performance Benchmarks workflow) failure | Comment-step failure, not benchmark itself | Workflow permission scope for posting PR comments | Tolerate. The benchmark itself ran. |

If a PR's failure set is **exactly** these two patterns, recommend merge. If there's a net-new failure (a different check name, or a different failure mode for these checks), recommend investigate.

## Process

1. **Fetch state**:
   ```bash
   gh pr view <#> --json mergeable,mergeStateStatus,statusCheckRollup
   ```
2. **Categorise each failure**:
   - For each check with `conclusion: FAILURE`:
     - Match against the known-infra patterns above.
     - If match → tag as `tolerated-infra`.
     - If no match → tag as `needs-investigation`.
3. **Check in-progress count** — if checks are still in flight, decide whether to wait (block until they settle if the answer matters before merging, otherwise report current state).
4. **Spot-check the log** for at least one new-this-PR Ubuntu failure to confirm it's the exit-143 pattern (don't assume — patterns can shift). Use:
   ```bash
   gh run view <run-id> --log-failed --job <job-id> | tail -30
   ```
   Look for the "shutdown signal" / "exit code 143" / "make: *** [Makefile:57: test-race] Terminated" markers.
5. **Recommend**:
   - All failures tolerated, no in-progress: **merge**.
   - All failures tolerated, in-progress checks won't change the picture: **merge** (note in-progress checks).
   - Any net-new failure: **investigate** (don't merge until classified).
   - Any in-progress check whose outcome would matter: **wait** (block on it via `until` loop).
6. **Report** to the user as a compact table + one-line recommendation. Don't modify anything.

## Wait pattern (for in-progress checks)

If checks are still running and the answer matters, use a background poll:

```bash
until [ "$(gh pr view <#> --json statusCheckRollup --jq '[.statusCheckRollup[] | select(.status == "IN_PROGRESS")] | length')" = "0" ]; do sleep 30; done
```

Run via `Bash` with `run_in_background: true` so you get a single notification when checks settle. Don't poll in foreground.

## Output format

```
PR #<N> CI status

Successes: <count>
Failures (categorized):
  tolerated-infra:
    - <check name> (matches: <pattern>)
    - <check name> (matches: <pattern>)
  needs-investigation:
    - <check name> (no match — <one-line reason or "log review pending">)
In-progress: <count> (<list>)

Recommendation: <merge | hold | investigate | wait>
Reason: <one line>
```

## What this skill does NOT do

- **Doesn't merge.** Recommendation only. The user explicitly merges (or directs the agent to).
- **Doesn't modify CI config.** If a known-infra pattern keeps biting, that's a separate task — surface it for next planning checkpoint, don't fix it as part of triage.
- **Doesn't expand the tolerated-infra set.** New patterns get tolerated only after the user explicitly says so AND the pattern gets added to `CLAUDE.md`. Don't silently expand the toleration list.
- **Doesn't replace `/preflight`.** That covers pre-PR local checks (build, test, lint, format). This covers post-PR-open CI status interpretation.

## Pre-flight checks

- [ ] PR number provided or inferable from context.
- [ ] `gh` CLI authenticated (try `gh auth status` if uncertain).
- [ ] You've read `CLAUDE.md` § "Known infra patterns" recently — if the toleration list there has changed, update this skill.
