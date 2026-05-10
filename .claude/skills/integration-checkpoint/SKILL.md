---
name: integration-checkpoint
description: Sync a long-running branch (or worktree) against latest main and re-verify the agent's working assumptions before merging or continuing. Catches drift when parallel agents have landed work that affects the current branch's premises. Use when an agent's branch is more than a few hours old, before any "I'm about to merge" decision, after a high-leverage architectural change lands on main (e.g., A4 affecting all storage work), or when the user asks "sync against main," "check assumptions," "integration check." Output is a clean rebase, re-run tests, and an advisor confirmation that the original framing still holds.
---

# Integration checkpoint

Periodic re-grounding for long-running parallel work. Catches drift before it becomes merge conflict or shipped-on-stale-assumptions.

## When to invoke

- Branch is more than ~4 hours old since branched off main.
- About to merge a PR that's been open while other PRs landed.
- A high-leverage change just landed on main (e.g., A4 affecting all subsequent storage work; observability surface changing affecting all api work).
- The user explicitly says "sync," "rebase against main," "integration check," "drift check."
- After `worktree-spawn` for any task expected to run >2 hours.

## Process

1. **Capture current state**:
   ```bash
   git rev-parse HEAD                         # current commit
   git log --oneline main..HEAD               # commits on this branch
   git log --oneline HEAD..origin/main        # commits on main since branched
   ```
   If `HEAD..origin/main` is empty, you're already up to date — skip the rebase, jump to step 4.
2. **Identify drift candidates** in `HEAD..origin/main`:
   - Did any PR touch files this branch also touches? `git diff --name-only main...HEAD` ∩ `git log --name-only HEAD..origin/main` (set intersection).
   - Did any PR change shared interfaces / types / lock conventions? Read PR titles for keywords: `feat(storage)`, `refactor`, `breaking`, `interface`, `lock-grain`, `partition`.
3. **Rebase** (or merge, if the repo prefers):
   ```bash
   git fetch origin
   git rebase origin/main
   ```
   If conflicts: stop, surface them to the user, do NOT silently auto-resolve. Conflicts during integration-checkpoint indicate real coordination work, not mechanical merging.
4. **Re-verify the build**:
   ```bash
   go build ./... && go vet ./...
   go test ./pkg/<area>/ -short -timeout 90s -count=1
   ```
5. **Re-verify race-cleanness if the branch touches concurrency**:
   ```bash
   go test -race ./pkg/<area>/ -count=3 -timeout 300s
   ```
   Background-run if long; notify when done.
6. **Re-verify lint at CI's surface**:
   ```bash
   golangci-lint run ./...
   ```
7. **Advisor call** if the branch's framing might have shifted. Frame the question as: "I'm working on <task ID>. Since I started, these PRs landed: <list>. Here's the current diff: <stats>. Do my original assumptions still hold, or has the ground moved?"
   - Skip the advisor call if the rebase was trivial (no shared file overlap, no architectural changes).
8. **Update the branch's PR description** if drift caused scope to shift. The PR body should reflect current intent, not stale planning notes.
9. **Report**:
   - Rebase outcome (clean / conflicts resolved / blocked)
   - Test/lint state
   - Drift summary (1-line: "no drift" / "<list of impacts and how addressed>")
   - Advisor recommendation if called

## What this skill does NOT do

- **Doesn't auto-resolve merge conflicts.** Conflicts during integration mean two agents made conflicting decisions; that's a coordination problem the human resolves (or the agent escalates to the user).
- **Doesn't squash or rewrite the branch's commits.** Rebase preserves the structural / lock-grain / bench split atomic-commit shape.
- **Doesn't update the planning doc.** That's `planning-doc-update`'s job, after the work merges.
- **Doesn't replace pre-merge `ci-status-triage`.** Run both: integration-checkpoint covers the LOCAL branch sanity; ci-status-triage covers the REMOTE PR's CI state.

## Edge cases

- **Rebase conflicts in shared infrastructure files** (e.g., `pkg/storage/storage_helpers.go`, `CLAUDE.md`, `NEXT_STEPS_<DATE>.md`). Surface immediately to the user — these are coordination smells, not mechanical merges. Resolution should align with the user's intent for the conflict, not just merge the lines.
- **Rebase succeeds but tests now fail.** Catastrophic drift. Don't try to fix in the integration checkpoint — surface to the user and let them decide whether to reset, fix, or escalate.
- **Branch is so far behind that rebase is impractical** (50+ commits to replay). Recommend abandoning the branch + restarting from main with the lessons-learned. Surface this honestly rather than forcing a multi-hour rebase.
- **Advisor call comes back with "your assumptions are no longer valid."** Don't proceed with the original plan. Surface the advisor's feedback and ask the user how to adjust scope.

## Pre-flight checks

- [ ] On a feature branch (not `main`).
- [ ] `git status --short` clean before starting (uncommitted changes should be committed or stashed first).
- [ ] You can fetch from origin (`git fetch --dry-run`).
