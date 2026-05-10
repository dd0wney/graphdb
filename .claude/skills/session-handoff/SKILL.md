---
name: session-handoff
description: Write a session-handoff doc capturing what shifted, what's queued, and what to retire so the next Claude Code session can pick up cleanly. Use when the user asks to "prepare a session handoff," "write a handoff doc," "close out the session," "end of session," or after substantive multi-PR work (≥3 merged PRs in this session, or any state that's non-obvious for a fresh agent). Output is a single new markdown file at docs/SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md following a 7-section structure. Distinct from task-focused HANDOFF_<DATE>_<TOPIC>.md docs (those are implementation briefs for one deliverable).
---

# Session handoff

Write `docs/SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md` capturing session-end state for the next agent.

## Filename

- Format: `SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md` (UTC; explicit `Z` to avoid timezone ambiguity).
- Time component is required — multiple handoffs can land per calendar day.
- Pull the timestamp from `date -u +"%Y-%m-%d-%H%MZ"` if writing pre-commit, or from the most-recent commit's UTC time via `git log -1 --format="%aI"` then convert to UTC.

## Required state to gather (run these first, in parallel)

```bash
git log --oneline origin/main -<N>            # N = roughly this session's PR count + 2
git status --short                             # any uncommitted changes? should be none
git branch                                     # any non-main branches?
gh pr list --state open --limit 10            # any in-flight PRs?
gh pr list --state merged --limit 20 --search "merged:>=YYYY-MM-DD"  # this session's merges
```

If you can't determine which PRs belong to "this session" from `gh pr list` alone, ask the user for the start commit / time. Don't guess from PR titles alone — handoffs that misattribute work damage trust.

## 7-section structure

Write the doc with these sections in order. Each section earns its place by saving the next agent at least one bad turn.

### 1. Header

```markdown
# Session handoff — <YYYY-MM-DD> <HH:MM> UTC

**Date**: <YYYY-MM-DD> (<one-line session shape: "single session, ~9 PRs merged" / "two distinct stages" / etc.>)
**Outgoing model**: <model name from environment, e.g. "Claude Opus 4.7 (1M context)">
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
```

### 2. TL;DR

One or two sentences. What changed at the project level. The next agent reads this first; if their task is unrelated, they read no further.

### 3. What's done this session

A table of merged PRs, oldest first. Columns: `PR | Title | Notes`. The Notes column captures anything non-obvious — reframed criteria, unexpected findings, scope changes.

### 4. Current state

- `origin/main` HEAD commit (short SHA)
- Open PRs (if any) with one-line status — note known-infra-tolerated `UNSTABLE` state per `CLAUDE.md` § "Known infra patterns"
- Open branches (should be just `main` if `--delete-branch` discipline held)
- Uncommitted changes (should be none — flag explicitly if not)
- Test/lint state (race-clean? vet-clean? lint-clean?)

### 5. What's next

The ranked queue from `docs/NEXT_STEPS_<DATE>.md` (or successor), with notes for any items the session moved (e.g., "A4-edges is now done, removed from the queue"). Include both critical-path items and off-path-parallel options.

If the session surfaced new gaps not yet on the planning doc, list them in a sub-section so the next planning checkpoint can absorb them.

### 6. Stale assumptions to retire

This is the highest-leverage section. List anything in the user's auto-memory or in `NEXT_STEPS_<DATE>.md` that this session's work invalidated. **Be specific**:

- Name the file
- Quote the line range or claim
- State the corrected version

Example: "`NEXT_STEPS_2026-05-10.md` line 217-251 'Single-node assumption baked in' → should be 'no sharded write path; pkg/cluster/ substrate exists.'"

The next session should be able to update the planning doc / refresh memory using only this list. Don't make the next agent re-derive corrections.

### 7. Open questions for the user

Decisions that came up but weren't resolved. The next session opens by either resolving these or by acknowledging them and proceeding. If there are no open questions, omit this section — don't pad it.

### 8. (Optional) How to use this handoff

A short numbered list:

1. Read this first.
2. Then read `docs/NEXT_STEPS_<DATE>.md`.
3. Then read `CLAUDE.md` § "Orient first" (auto-loaded for Claude Code agents).
4. If picking up <recommended-task>, also read <relevant audit doc / source file>.

This section is optional because it's stable across handoffs — only include if there's something path-specific to flag.

## What this skill produces

1. The handoff markdown file at `docs/SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md`.
2. A new branch `docs/session-handoff-<YYYY-MM-DD>-<HHMM>Z` (use the same UTC suffix).
3. A commit on that branch.
4. A PR titled `docs: session handoff — <YYYY-MM-DD> <HH:MM> UTC` with the body summarizing the doc's TL;DR + open questions.
5. Stop before merging. Surface to user with merge prompt — handoffs are the literal close-out, the user should explicitly bless the merge as their session-end signal.

## What this skill does NOT do

- **Don't update `CLAUDE.md` or the planning doc** as part of the handoff. Those are separate PRs (with their own conventions). The handoff just lists what should be updated; the next session does the update.
- **Don't refresh the user's auto-memory directly**. The handoff lists stale memory items; the user's harness handles memory updates.
- **Don't bundle handoffs into task PRs**. Single-file diffs review fast and don't churn alongside code.
- **Don't update prior handoffs**. Each handoff is a one-shot snapshot. Stale handoffs are intentional historical record.

## Pre-flight checks before writing

- [ ] `git status --short` is clean (or you've explicitly noted uncommitted state in §4).
- [ ] You're on `main` (handoff is written *off* `main`, not on top of in-flight work).
- [ ] You've identified the session boundary (what counts as "this session's" PRs vs prior).
- [ ] You've confirmed `docs/CAPABILITIES_<DATE>.md` and `docs/NEXT_STEPS_<DATE>.md` are accurate enough to reference, or noted in §6 if they need correction.

## Edge cases

- **Session that produced no PRs but left non-trivial state** (e.g., partial design exploration, abandoned approach). Still write the handoff — §3 says "no PRs merged this session," §6 captures what was learned. Future agents benefit from "we tried X, it didn't work because Y" more than from silence.
- **Session that's mid-task at handoff time**. Note explicitly in §4 (open PRs / open branches / uncommitted changes). The next session knows to resume rather than start fresh.
- **Multiple handoffs same day**. Filename time disambiguates. The latest is the live handoff; prior same-day handoffs are historical (typically because the session restarted after a model swap or context reset).
