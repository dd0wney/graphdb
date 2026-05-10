---
name: planning-doc-update
description: Update docs/NEXT_STEPS_<DATE>.md to reflect newly-merged work — mark items done with PR refs, update the sequencing graph, surface new follow-ups. Use when the user asks to "update the planning doc," "mark task done," "reconcile the planning doc," "close out X in the roadmap," after a PR merges that closes a tracked task, or after substantive multi-PR work that shifted the critical path. Single-file PR shape (modifies only the planning doc); separate from the work PRs that actually closed the tasks.
---

# Planning doc update

Update the latest `docs/NEXT_STEPS_<YYYY-MM-DD>.md` to reflect work that just landed. Single-file PR; doesn't bundle into task PRs.

## When to invoke

- A PR just merged that closed a tracked task (e.g. A4, H1, F1.1-impl).
- A session ended with multiple closed tasks that haven't been reconciled.
- Net-new follow-ups surfaced (gaps, decisions, sub-tracks) that need to be on the queue.
- The user explicitly asks ("update the planning doc," "mark X done").

## What "the planning doc" is

The most recent `docs/NEXT_STEPS_<YYYY-MM-DD>.md` file. The header date is authoritative — find via `ls docs/NEXT_STEPS_*.md | sort | tail -1`. **Do not write to an older one** even if the user names it; the latest supersedes.

If no `NEXT_STEPS_<DATE>.md` exists, this skill is the wrong tool — use `/plan` to create one.

## Three update shapes (most edits fall into one of these)

### Shape A: mark a task done

Three places to update for each closed task:

1. **The reconciliation table** (early in the doc, "State reconciliation" section): change ❌ to ✅ Done with PR refs (commit short SHAs).
2. **The task's own section** (later in the doc, under "Track A / B / etc."): replace the unchecked checklist with a "✅ DONE <DATE> (PR #NN)" header + 2-3 line summary capturing key outcomes (especially any reframed acceptance criteria — see A4's throughput reframe in #67 for the canonical example).
3. **The sequencing graph**: strike-through the done item (`A4 ✅`); update the "Critical path" line; refresh "Why this ordering" bullets.

### Shape B: add a new follow-up

When work surfaced a new task (e.g., A4-edges from A4):

1. **The task section**: insert a new sub-section after the related parent task. Use the same shape (checklist + acceptance + why-now + estimated-scope).
2. **The sequencing graph**: insert into the dependency chain at the appropriate position.
3. **Cross-reference** from the parent task's "DONE" summary if relevant.

### Shape C: correct an over-broad claim

When a section's framing turned out to be wrong (e.g., #71's productization-gaps section corrected by #72's capabilities audit):

1. **Don't delete the original section** — it's part of the historical record.
2. **Add a "Correction" or "Reconciliation" sub-section** at the end of the affected section, pointing to the corrected source.
3. Update only the specific over-broad claims, not the whole section.

## Process

1. **Find the latest planning doc**: `ls docs/NEXT_STEPS_*.md | sort | tail -1`
2. **Read it fully** before editing — context matters for which sections need touching.
3. **Identify closed tasks since last update**:
   - Scan `git log --oneline origin/main` for PR merges since the doc's last modification (`git log -1 --format=%aI -- docs/NEXT_STEPS_*.md`)
   - For each merged PR, read its body to identify which tracked task it closed (PR descriptions reference task IDs like `A4`, `H1`)
4. **Decide which update shapes apply** (most edits are pure Shape A; Shape B fires when work surfaced new follow-ups; Shape C fires after audits or corrections).
5. **Apply edits** using the targeted-Edit pattern (don't rewrite whole sections; preserve historical structure).
6. **Verify the diff is small and focused** (`git diff --stat` should show one file with <100 line delta in most cases).
7. **Commit**: `docs(planning): close <task list>` or `docs(planning): mark <task> done` for Shape A; `docs(planning): add <new-task> follow-up` for Shape B; `docs(planning): reconcile <section> with <correction>` for Shape C.
8. **Push + open PR**: title matches commit; body summarizes what shifted and why.
9. **Stop before merge** — surface to user with merge prompt.

## What this skill does NOT do

- **Doesn't create new planning docs.** Use `/plan` for that. This skill only updates an existing one.
- **Doesn't bundle into task PRs.** The planning doc is updated *after* a task lands, in its own commit/PR. Bundling muddles "what code shipped" with "what we now think the future looks like."
- **Doesn't speculate about future work.** Only reflects what shipped + what shipped *surfaced* as a new finding. New aspirational tasks come from planning checkpoints, not from this skill.
- **Doesn't update auto-memory.** The session-handoff skill captures stale memory items; this skill stays narrowly on the planning doc.

## Examples from this repo

- PR #68 — Shape A (close A4 + H1, surface A4-edges as Shape B addition).
- PR #69 — Shape A (mark H3 done).
- PR #71 — Shape B (add productization-gaps section as net-new content).
- (Hypothetical) PR #72 should have triggered a Shape C correction note pointing the gaps section at the new capabilities audit; left as an open task in `SESSION_HANDOFF_2026-05-10-0208Z.md` open questions.

## Pre-flight checks

- [ ] You're on `main` (planning doc updates branch off `main`, not on top of in-flight work).
- [ ] `git status --short` is clean.
- [ ] You've identified the latest `NEXT_STEPS_<DATE>.md` (not an older one).
- [ ] You've identified which PRs map to which planning-doc tasks (don't guess from titles alone if ambiguous — ask the user).
