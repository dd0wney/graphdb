---
name: worktree-spawn
description: Spin up an isolated git worktree for parallel agent work, with branch naming + setup boilerplate handled. Wraps EnterWorktree with the repo's branch convention, pulls latest main, runs `go mod download`, optionally calls work-claim for the task ID. Use when the user asks "spawn a worktree for X," "set up a parallel agent for X," "start parallel work on X," or when an agent needs to work in isolation from the main checkout. Output is a ready-to-work worktree at `../graphdb-<task-id>` on a fresh branch named per the repo's `<track>/<task-id>` convention.
---

# Worktree spawn

Set up an isolated worktree so a parallel agent can work without colliding with the main checkout.

## When to invoke

- The user wants to start parallel work on a task while another agent (or the user) is still mid-task in the main checkout.
- A long-running task would block the main checkout for hours; spawning a worktree lets quick fixes land in main while the long task progresses in the worktree.
- The user explicitly requests ("spawn a worktree," "set up parallel work on X").

## Process

1. **Pre-flight in main checkout**:
   - Verify `git status --short` is clean (don't carry uncommitted state into a new worktree).
   - Verify on `main`, pull latest: `git checkout main && git pull`.
   - Run `go build ./...` to confirm main is in a buildable state. If not, refuse to spawn — surface the broken-main as a blocker.
2. **Determine the bare task ID** from the user's request (e.g. `H4-PR3-skill-rewrite`). Look it up in `docs/NEXT_STEPS_<DATE>.md` to confirm it's a real task. If unclear, ask. Note: the coord daemon stores task IDs project-prefixed (`graphdb:H4-PR3-skill-rewrite`) — `work-claim` handles the prefix internally, so this skill passes the bare ID through.
3. **Optionally claim the task** via `work-claim` (recommend this when ≥2 agents are or might be active). `work-claim` auto-detects `COORD_PROJECT` from the git remote, prefixes the task ID, and uses the B-lite GraphQL `createNode` mutation to claim atomically — concurrent spawns racing the same task lose at the resolver. If the claim fails with a "unique constraint violation," abort spawn — another agent already owns it.
4. **Compute worktree path**: `../graphdb-<task-id>` (e.g., `../graphdb-H4-PR3-skill-rewrite`). If that directory exists, suggest a suffix (`../graphdb-H4-PR3-skill-rewrite-2`).
5. **Compute branch name**. Match the repo's existing convention from `git log --oneline | head -20` — typically `<track>/<task-id>` (e.g., `feat/coord-blite-claim-uniqueness-2026-05-10`). Fall back to `feat/<task-id>` if no clear convention.
6. **Use `EnterWorktree`** to set up the worktree. (`EnterWorktree` is a Claude Code tool — it creates the worktree, the branch off main, and switches the agent's working context. Do not shell out to `git worktree add` directly; the tool handles process state correctly.)
7. **Setup in the new worktree**:
   - `go mod download` (warm the module cache for the worktree).
   - `go build ./...` to confirm clean state.
   - `golangci-lint run ./... --fast 2>&1 | head -5` — quick smoke (fast mode); not required to be clean, just a baseline reading.
8. **Report** to user:
   - Worktree path
   - Branch name
   - Task ID (and claim status if claimed)
   - Recommended next step (typically: read `CLAUDE.md` § "Orient first," then start the task)

## Coordination with other skills

- **`work-claim`**: this skill calls it as step 3. If a claim already exists for the task, this skill aborts.
- **`session-handoff`**: when work in the worktree finishes, the agent that owns the worktree writes a handoff (or merges via the parent agent's session). Worktrees are per-task isolation; handoffs are per-session continuity.
- **`integration-checkpoint`**: long-running worktrees should periodically run integration-checkpoint (rebase against main, re-verify assumptions) to catch drift.

## What this skill does NOT do

- **Doesn't merge from the worktree.** Merges happen via PR from the worktree's branch; the main checkout's agent (or the user) handles the merge.
- **Doesn't run tests** in the new worktree beyond a smoke build. Test selection is the spawning agent's call after they've read the task.
- **Doesn't auto-spawn more than one worktree per call.** One task = one worktree. If you want N parallel workers on N tasks, call this skill N times.
- **Doesn't clean up worktrees automatically.** When work is done and the PR merges, the agent must `ExitWorktree` (and the worktree can be removed). Stale-worktree cleanup is a separate concern.

## Edge cases

- **`EnterWorktree` is unavailable** (older Claude Code or unsupported environment). Degrade to suggesting the user shell out to `git worktree add ../graphdb-<task-id> -b <branch>` manually, then open a fresh Claude Code session in that path.
- **Disk pressure**: each worktree is a full checkout. For very large repos, surface the disk cost. Not a concern here — this repo is not huge.
- **Worktree path collision**: if `../graphdb-<task-id>` exists but is a stale worktree from prior work, surface it and ask whether to reuse, rename, or delete.

## Pre-flight checks

- [ ] Main checkout is on `main`, clean, up-to-date with origin.
- [ ] `go build ./...` passes on main (don't fork from broken state).
- [ ] Task ID exists in the planning doc AND has been seeded into the coord daemon (via `scripts/coord-seed.sh`).
- [ ] If using `work-claim`, the coord daemon is reachable (`scripts/coord-bootstrap.sh` has run) and the claim succeeded.
