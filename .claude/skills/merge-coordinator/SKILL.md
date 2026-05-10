---
name: merge-coordinator
description: Given multiple ready-to-merge PRs, infer their dependency order from cross-references in PR bodies and recommend a merge sequence. Runs ci-status-triage on each, accounts for "follow-up to #N" / "depends on #M" relationships, and surfaces the recommended order — does NOT auto-merge. Use when ≥2 PRs are open and ready, or when the user asks "what order should I merge these," "merge sequence for #A #B #C," "any dependencies between open PRs." Most useful when parallel agents have produced multiple ready PRs with implicit ordering.
---

# Merge coordinator

Compute a safe merge order for multiple ready-to-merge PRs. Recommends; doesn't auto-execute.

**Speculative skill — retire if not earning its keep.** Most sessions have 1-2 PRs ready at a time and ordering is obvious. This skill earns its place when ≥3 PRs land near-simultaneously from parallel agents. If a quarter passes without invoking it for that scenario, delete this file.

## When to invoke

- ≥2 PRs are in `OPEN, MERGEABLE` state and the merge order isn't obvious.
- The user asks "what order should I merge these."
- After parallel agents have all closed their work and several PRs are queued.

If only one PR is ready, this skill is overkill — use `ci-status-triage` directly.

## Process

1. **Inventory open mergeable PRs**:
   ```bash
   gh pr list --state open --json number,title,body,mergeable,mergeStateStatus,headRefName \
     --jq '.[] | select(.mergeable == "MERGEABLE")'
   ```
   Filter to MERGEABLE only. PRs in `CONFLICTING` state need rebase first — surface as blocked, don't include in the merge plan.
2. **Extract dependency hints** from two sources:
   - **Coord instance** (preferred when available): traverse `:DEPENDS_ON` edges from the Task nodes the PRs claim. Each PR's matching Claim points to a Task; `(PR_task)-[:DEPENDS_ON]->(other_task)` means "the other task must close first." Query: `MATCH (t:Task {id: $task_id})-[:DEPENDS_ON]->(d:Task) RETURN d.id`. Authoritative — these dependencies were declared at planning time, not inferred from PR prose.
   - **PR body fallback** (when coord traversal returns nothing or coord is unavailable): scan for "follow-up to #N" / "depends on #M" / "blocked by #M" / "should land before #X" / "should land after #X" / file-path overlaps suggesting sequencing (two PRs both touching `pkg/storage/storage_types.go` will conflict; one needs to land before the other rebases).
3. **Build the dependency DAG**. Each PR is a node; "depends on" / "follow-up to" creates an edge. Cycles indicate human coordination needed — surface and abort.
4. **Topologically sort**. Tie-break ties by:
   - Smaller PR first (less risk per merge — get easy wins out)
   - Older PR first (FIFO discipline)
   - PRs blocking the most other PRs first (unblock the queue)
5. **Run `ci-status-triage` on each** in the planned order. Note any that recommend hold/investigate.
6. **Report** the recommended order as a numbered list:
   ```
   Recommended merge order:
     1. #N — <title> (CI: <triage result>)
     2. #M — <title> (depends on #N landing first; CI: <triage result>)
     3. #P — <title> (parallel-ok with #M; CI: <triage result>)

   Blocked / needs attention:
     - #Q — CONFLICTING with main; rebase required
     - #R — ci-status-triage flagged net-new failure (see <details>)

   Cycles detected: <none | description>
   ```
7. **Stop**. Do NOT auto-merge. The user merges (or directs the agent to) per the plan.

## Sequential merge with re-check (optional follow-up)

If the user says "go ahead, merge in this order," run sequentially:

1. For each PR in order:
   - `gh pr merge <#> --squash --delete-branch`
   - Wait for the next PR's `mergeable` state to recompute (`UNKNOWN` → `MERGEABLE` or `CONFLICTING`):
     ```bash
     until [ "$(gh pr view <next> --json mergeable --jq '.mergeable')" != "UNKNOWN" ]; do sleep 5; done
     ```
   - If the next PR's state is now `CONFLICTING`, abort the rest of the sequence — that PR needs the merging agent to rebase before continuing.
2. Report final state: how many merged, which (if any) are now blocked.

## What this skill does NOT do

- **Doesn't auto-merge without user confirmation.** Recommendation only by default; sequential merge requires an explicit "go ahead" from the user.
- **Doesn't resolve merge conflicts.** PRs that conflict with main need the owning agent to rebase.
- **Doesn't infer dependencies from code analysis.** Only reads PR bodies. If "follow-up to #N" isn't in the body, this skill won't catch the dependency. Augment by reading file-path overlaps when uncertain.
- **Doesn't split PRs.** If one PR is "too big" to merge cleanly, that's the owning agent's call to split, not this skill's call.
- **Doesn't replace `ci-status-triage`.** It calls that skill per PR; it's an orchestrator, not a replacement.

## Edge cases

- **Cyclical dependencies**: PR-A says "follow-up to PR-B," PR-B says "depends on PR-A." Cycle. Surface to user — this is a coordination problem the agents created and humans resolve.
- **PR descriptions don't mention dependencies but file overlaps suggest one**: surface as a heuristic warning, don't treat as definitive. Recommend the user clarify intent.
- **All PRs are independent** (no dependency graph): trivial topological sort returns any order. Recommend by FIFO + size tie-break.
- **One PR has been waiting much longer than others** (open >7 days): surface as a separate "stale PR — review and merge or close" note, don't auto-promote in the order.

## Coordination with other skills

- **`ci-status-triage`**: called per PR.
- **`work-claim`**: claims should already be released (or about to be released) by the merging PRs. If a PR's matching `:Claim` node in the coord instance is stale, surface as a follow-up (the stale-sweep task documented in `work-claim`'s body).
- **`planning-doc-update`**: not invoked by this skill, but the user typically runs it AFTER the merge sequence completes (one update covering all the closed tasks).

## Pre-flight checks

- [ ] At least 2 open MERGEABLE PRs (otherwise this skill is overkill).
- [ ] `gh` CLI authenticated.
- [ ] You're on `main` with a clean tree (the agent running this skill might want to pull post-merge to verify).
