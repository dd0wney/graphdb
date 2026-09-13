# Session handoff — 2026-09-13 01:53 UTC

**Date**: 2026-09-13 (single session, 10:10–12:00 AEST; supersedes `SESSION_HANDOFF_2026-09-13-0045Z.md`, written mid-session)
**Outgoing model**: Claude Fable 5.1
**Delegation**: none. Peer session `graphdb-coord-89` (the coord repo) answered coord questions; see the 00:45Z handoff § Peer session.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

Two coord tasks closed end to end: the real-corpus coi-screen run (#594, B-1 answered) and the defect it surfaced, `Close` rewriting the snapshot on a read-only mmap session (#598: 8.3 s → 9 ms, 7.9 GB → 1.0 GB on the 2.0M-node ICIJ store). graphdb has **no pending coord task**; the next one has to be seeded.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #596 | chore(coord): seed v1.4-close-no-rewrite-when-clean | Task node 438 |
| #594 | docs: real-corpus coi-screen run on the 2.0M-node ICIJ package | `scripts/icij-merge-nodes.py`, results doc, runbook correction. Coord task released with it. |
| #595 | docs(planning): close the real-corpus track, resolve B-1, add the Close follow-up | |
| #597 | docs: session handoff — 2026-09-13 00:45 UTC | Mid-session; this one supersedes it |
| #598 | fix(storage): Close skips the snapshot rewrite when the mmap session wrote nothing | `4a4213a`. Five tests, each seen red; race ×3 clean; lint 0. Coord task released with it. |
| coi-screen #2 | fix: build against graphdb main | Sibling repo, `889f547`. Module rename, ADR 0003 error return, `UseMmapSnapshot` in `graphload.Open`. |

Open at handoff: **#599** (planning doc: mark the Close row done) — docs-only, merge on green.

## 3. Current state

- `origin/main` HEAD: `4a4213a` (#598), plus #599 when it merges.
- Open PRs: #599. Open branches: `docs/planning-close-no-rewrite-done`, this handoff branch.
- Uncommitted changes: none.
- Test/lint state on #598: storage suite green, `-race -count=3` green (140 s), golangci-lint v2.13.2 zero issues, `gofmt -s` empty.
- Coord (graphdb): 11 tasks, all done or cancelled, **0 pending, 0 claimed**. Budget on the Close task ended at about $12 of $35 after one grant.
- Scratch state under `/tmp/claude-1000/…/scratchpad/` (corpus, two data dirs, binaries) is rebuildable from `COI_SCREEN_REAL_CORPUS_2026-09-13.md`.

## 4. What's next

Nothing is seeded. Candidates, in the order this session would pick them:

1. **JSON-mode clean Close.** Same defect, other representation: 25 s / 17.5 GB per read-only exit. No overlay/base split to compare, so it needs a different proof — most likely "WAL LSN unchanged since open AND a write generation counter under `gs.mu` unchanged", with BulkImportMode (no WAL) as the case that must not slip through. Seed it before starting.
2. **Clean-check cost.** `mmapSnapshotCleanLocked` → `buildMmapMetadata` clones every property index once per Close. A metadata *digest* kept in the header, or a compare that does not clone, makes a read-only exit O(1). Only worth it once a store with a large property index shows up in a profile.
3. **consumer-drive SKIP policy.** The coi step skipped for months and looked green. Decide whether an absent consumer fails CI (see #594 finding 4). With coi-screen #2 merged the step now runs when the sibling is present.
4. **coi-screen M1 steps 3–5** (its repo): precision on hand-labelled pairs, threshold calibration, resolver index + DFS hub bound. Reproduction for the last: `CORIGLIANO FRANK J.` / `MCCLAMMY EDWARD G.` at `--max-hops 2`, 13 min, killed.
5. Off-path, unchanged: GraphQL damage signal (ADR 0003), predicate-aware storage page method, sorted-bucket cache, onboarding docs, CI hygiene, the junk-test sweep (gates that skip silently; tests with no assertion).

## 5. Stale assumptions to retire

- `docs/NEXT_STEPS_2026-06-18.md` §D Close row says "not yet a coord task" → fixed by #599 (pending). If #599 is closed instead, redo that one edit.
- `SESSION_HANDOFF_2026-09-13-0045Z.md` §3–§4: #594/#595 "mid-flight" and the Close task "pending, unclaimed" → all merged and done. `NEXT_SESSION_PROMPT.md` from that handoff told the next session to claim the Close task; this handoff's prompt replaces it.
- Any belief that `Close` on an mmap store always writes: it skips when clean (#598). `Snapshot()` and `CompactWAL` still always write.
- Auto-memory `coi-screen-and-icij-corpus-locations`: says the coi-screen branch is unpushed → merged as coi-screen #2 (`889f547`), branch gone.

## 6. Open questions for the user

- consumer-drive SKIP: fail or warn? (§4 item 3)
- The first two budget grants of the day landed nowhere; the coord session asked for the exact command text and only the user's terminal has it.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first.
2. If #599 is still open, merge it, then `git checkout main && git pull --ff-only`.
3. Nothing is claimable: pick from §4, seed it with the `coord-task` skill (dry run first; message the coord session before the apply), then `coord next --claim`.
