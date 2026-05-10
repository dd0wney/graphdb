---
name: branch-cleanup
description: Find and force-delete local git branches whose PRs are squash-merged. Use when the user asks "clean up stale branches," "delete merged branches," "branch cleanup," or after substantive multi-PR work that left local branches behind. The repo's habit is `gh pr merge --delete-branch` which prevents accumulation, but historically older PRs landed without it; this skill catches up.
---

# Branch cleanup

Force-delete local branches whose PRs are confirmed-merged. Squash-merge breaks `git branch -d`'s reachability check (the squashed commit on main is content-equivalent but not ancestor-equivalent to the branch tip), so `-D` is required after PR-state verification.

## When to invoke

- After substantive multi-PR work where some PRs predated the `--delete-branch` habit.
- The user explicitly requests cleanup ("H3 cleanup," "branch cleanup," etc.).
- `git branch | wc -l` returns more than ~3 (heuristic; main + a couple in-flight is normal).

## Process

1. **Refresh remote-tracking** — prune deleted upstreams:
   ```bash
   git fetch --prune
   ```
2. **Inventory candidates**:
   ```bash
   git branch -vv
   ```
   Candidates are branches that:
   - Are NOT `main` (or whatever the default branch is — check `gh repo view --json defaultBranchRef`).
   - Are NOT currently checked out (the marker `*` in `git branch` output).
   - Match a project naming convention (`feat/*`, `chore/*`, `docs/*`, `fix/*`, `ci/*`) — skip ad-hoc local-only branches like `pr-NN` unless the user includes them.
3. **Verify each candidate** — for each branch name, confirm a merged PR exists:
   ```bash
   gh pr list --head <branch> --state merged --limit 1 --json number,mergedAt
   ```
   - If the response has a PR with a `mergedAt` timestamp → safe to delete.
   - If empty → SKIP (might be in-flight, abandoned, or remote-only).
   - For local-only branches with no upstream (`pr-NN`, `pr-NN-review`-style), look up the PR by number directly:
     ```bash
     gh pr view <NN> --json state,mergedAt
     ```
4. **Show the user the deletion list** before acting. Format:
   ```
   Verified merged (safe to -D):
     - <branch>  (PR #<N> merged <date>)
     - <branch>  (PR #<N> merged <date>)

   Skipped (no merged PR):
     - <branch>  (no PR found / PR open / not searched)

   Total: <X> to delete, <Y> skipped.
   ```
5. **Confirm with the user** before executing the bulk delete. This is a destructive operation; even though it's local-only and the work is on origin, agent-initiated bulk deletes deserve an explicit nod.
6. **Execute**:
   ```bash
   git branch -D <branch1> <branch2> ...
   ```
   Bulk delete via single command (one `git branch -D` with all names) for atomicity.
7. **Verify**:
   ```bash
   git branch
   ```
   Should show only `main` (or the small set of intentional in-flight branches).
8. **Report** the count deleted and the remaining branch list.

## What about remote branches?

Different concern. The skill targets LOCAL cleanup. If origin has stale remote branches (because old PRs merged without `--delete-branch`), those are a separate task:

```bash
# Inventory remote-only stale branches
git branch -r | grep -v 'origin/main' | grep -v 'origin/HEAD'
# Delete (per branch — destructive on remote, requires explicit user nod):
git push origin --delete <branch>
```

This skill should NOT touch remote branches without an explicit second confirmation from the user — remote deletion affects shared state.

## Edge cases

- **The currently-checked-out branch shows up as a candidate**. Skip it; can't delete the branch you're on. If the user wants to delete it, switch to `main` first.
- **A branch's tip commit is on `main`** (not squash-merged but actually merged with a merge commit). `git branch -d` would work, but `-D` works too. Use `-D` consistently for atomic-cleanup logic.
- **A branch has no upstream tracking** (`gh pr list --head` returns empty even though it's clearly a feature branch). Check if it was ever pushed (`git config branch.<name>.remote`). If never pushed and the user can't recall why it exists, ask before deleting — could be unfinished local work.
- **`gh pr merge --delete-branch` was used at merge time**. Then the local branch is already gone (the flag deletes both remote and local). Subsequent `git branch -D` will error with "branch not found" — that's expected, not a problem.

## What this skill does NOT do

- **Doesn't delete `main`** or whatever the default branch is. Hardcoded skip.
- **Doesn't delete remote branches** without an explicit second confirmation (separate concern).
- **Doesn't delete uncommitted local work**. If a branch has commits not on origin (`git rev-list <branch> ^origin/<branch>` non-empty), warn before deleting and ask the user.
- **Doesn't run `gh repo prune` or similar repo-side cleanup** — out of scope.

## Pre-flight checks

- [ ] You're on `main` (you can't delete the branch you're on).
- [ ] `git status --short` is clean.
- [ ] Default branch confirmed (`gh repo view --json defaultBranchRef`) — don't assume `main`.
- [ ] User has been shown the candidate list and confirmed before bulk delete.
