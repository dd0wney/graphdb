---
name: work-claim
description: Atomically claim a planning-doc task ID so multiple parallel agents don't start the same work. Uses a single source-of-truth ledger at docs/IN_FLIGHT.md committed via tiny PR — git's rebase/conflict semantics provide the serialization. Use when picking up any task from NEXT_STEPS_<DATE>.md before substantive work begins, or when the user says "claim X," "I'm taking X," "start X." Returns success (you own the task) or failure (someone else does — pick a different task or coordinate).
---

# Work claim

Claim a task before starting it. Prevents parallel agents from racing on the same scope.

## When to invoke

- Before starting any planning-doc task (`A8.2`, `H2`, `F1.1-spike`, etc.) when ≥2 agents may be active on this repo.
- The user explicitly says "claim X" or "I'm taking X."
- After `worktree-spawn` (which calls this skill internally) when adopting a task ID for the new worktree.

## How the claim works

A single file `docs/IN_FLIGHT.md` lives on `main` as the source of truth. Each row is one claim:

```markdown
| Task ID | Agent ID | Worktree / branch | Started (UTC) | Notes |
|---|---|---|---|---|
| A8.2 | agent-darragh-01 | feat/audit-a8.2 | 2026-05-10T03:14Z | Replica /nodes unauth fix |
```

Claiming is a tiny PR that adds a row. The PR is its own atomic unit — git's push-then-rebase semantics force one agent to retry if two race on the same task.

**Releasing a claim** = closing the PR (if claim never landed) or removing the row in the work PR's first commit (cleaner: same PR closes both work and claim).

## Process

1. **Read** `docs/IN_FLIGHT.md` from latest `main`. If the task ID already appears, report the conflict — do not proceed.
2. **Generate agent ID** if the user hasn't supplied one. Format: `agent-<user>-<short-uuid>` or `agent-<host>-<pid>`. Stable for the life of the agent's session.
3. **Branch** off `main`: `coord/claim-<task-id>` (short-lived; deletes on merge).
4. **Edit** `docs/IN_FLIGHT.md`: append the new row. Keep the table ordered by Started timestamp.
5. **Commit**: `chore(coord): claim <task-id> for <agent-id>`.
6. **Push**. If push fails on conflict: another agent claimed something else simultaneously; rebase + retry. If conflict is on the SAME task ID row: that agent claimed first; abort with the task already-claimed message.
7. **Open PR** titled `[CLAIM] <task-id>`. Body lists: task ID, agent ID, worktree/branch, planned scope (1-2 lines), expected duration.
8. **Merge immediately** via `gh pr merge --squash --delete-branch`. The claim PR is administrative; it doesn't need review. If branch protection requires review, surface that as a one-time setup task and degrade to a long-lived PR (`[CLAIM]` stays open until the work PR lands).
9. **Report** to user: "Claimed <task-id> as <agent-id>. Proceed."

## Releasing the claim

Pick one based on outcome:

- **Work PR opens** — in the work PR's first commit, remove the row from `docs/IN_FLIGHT.md`. Body of work PR references the closed claim by PR number.
- **Work abandoned** — separate single-line PR removing the row. Title: `chore(coord): release <task-id> claim (work abandoned: <reason>)`.
- **Work PR merges** — verify the row is gone. If the work PR forgot to remove it, file a follow-up.

**Stale claim cleanup**: rows older than ~24h with no associated work PR open should be flagged as stale (separate skill or manual sweep — out of scope for this skill).

## What this skill does NOT do

- **Doesn't enforce that agents respect the claim.** It's coordination, not access control. An agent that ignores `IN_FLIGHT.md` and starts the task anyway will produce a conflicting PR; the human resolves.
- **Doesn't claim sub-scopes.** Whole tasks only. If two agents want to split `A8.2` into `A8.2-server` and `A8.2-replica`, that's a planning-doc decomposition (`/plan`), not a claim split.
- **Doesn't expire claims automatically.** Stale-claim cleanup is a separate concern.
- **Doesn't lock files or branches.** Pure social-coordination via a shared ledger.

## Edge cases

- **No `docs/IN_FLIGHT.md` exists yet.** First-time use. The skill creates it with the table header in the same claim PR.
- **Branch protection blocks the immediate merge.** Degrade gracefully — leave the `[CLAIM]` PR open as the lock indicator. Other agents read open `[CLAIM]` PRs as well as the file.
- **Two agents push claims for different tasks at the same instant** — git rebase resolves cleanly (different rows, different lines). Both succeed eventually.
- **Two agents push claims for the SAME task** — second agent's push fails on rebase against the now-updated `IN_FLIGHT.md`. Skill detects the conflict on the row and reports the existing claim, including which agent ID owns it.

## Pre-flight checks

- [ ] Task ID exists in `docs/NEXT_STEPS_<DATE>.md`. Don't claim things that aren't on the planning doc — surface a planning-doc update instead.
- [ ] You're on `main` with a clean working tree.
- [ ] You can authenticate to GitHub (`gh auth status`) — claim merge needs `gh pr merge`.
