# Session handoff — 2026-11-07 01:39 UTC

**Date**: 2026-11-07 (single session, 5 PRs merged plus the v1.11.0 documents PR, one release)
**Outgoing model**: Claude Opus 5.5
**Delegation**:
- `reviewer` (opus, high effort) on #714: GO, two warnings and three nits. Main loop took Warning 2 and Nits 2–3 as written, rejected Nit 1 with a reason (see #714 body), and seeded Warning 1 as a coord task.
- `reviewer` (opus, high effort) on #716: CONDITIONAL on one `gofmt -s` finding, which the main loop's own gate run also caught. Report used unchanged; three nits went into the PR body.
- No implementer subagents: every diff was small enough to write in the main loop.

**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

v1.11.1 shipped. `DeleteAllNodes` now resets the counts, the tenant ID sets and the vectors it
used to leave behind (`VectorSearch` returned deleted node IDs), and two write-path changes cut a
large WAL-less bulk create's peak RSS from 2,569 MB to 1,713 MB (-33%, snare's single run on
the release tag; base runs differed by 35 MB, so the PR-branch figure of -36% is within noise of it).

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #713 | chore(release): v1.11.0 changelog, release pointers and chart appVersion | Left open by the previous session; merged at the start of this one. |
| #714 | fix(storage): reset counters, tenant ID sets and vectors in DeleteAllNodes | The previous handoff named only the stats counters. `CheckInvariants` after the clear found three defects. Vector indexes keep their definitions (decision: a clear is for a reload). Property index definitions are still dropped — now inconsistent, seeded. |
| #715 | perf(storage): skip the WAL encode when no WAL sink is configured | Snare ask 4. 5 allocs → 0 per WAL write on an Ephemeral store. The marshal error it skips is unreachable: `validateEdgeWeight` refuses NaN/Inf first (#328), pinned by a new test. |
| #716 | perf(storage): clone in bulk creates only when an observer needs it | Snare ask 5. Clone under `gs.mu` only when `len(gs.observers) > 0`; edges never cloned. Behaviour change: an observer added between a batch's unlock and its notify loop misses that batch. |
| #717 | chore(coord): seed three DeleteAllNodes follow-ups from #714 | Applied to coord before the PR; no coord session was live. |
| #718 | chore(release): v1.11.1 changelog, release pointers and chart appVersion | Tag `v1.11.1` on `df59b61` (#716) first, per convention. Release: 11 assets, 6 signatures. |

## Current state

- `origin/main` HEAD: `f67c8d6` (#717). Tag `v1.11.1` → `df59b61`.
- Release, Docker Publish (tag and main), Tests, Benchmarks and Lint on `df59b61`: all success,
  read with `gh run view`.
- Open PRs not from this session: #708 (planning: close Track U item 4 and datadir-flock),
  #709 (encryption no-plaintext test), #710 (2026-10-22 11:06Z handoff, never merged), #711
  (dependabot gosec bump). None touched this session.
- Local branches not on main: `feat/encrypt-rules-file` (`93f354b`, rules.json sealing,
  committed locally, no PR seen this session), `test/encryption-no-plaintext` (#709),
  `docs/planning-close-track-u-flock` (#708), `docs/session-handoff-2026-10-22-1106Z` (#710).
- Uncommitted changes: none.
- Gates on each code PR before push: `gofmt -s` clean, vet clean, `pkg/storage` suite and
  `-race -count=3` green, golangci-lint v2.13.2 0 issues.

## What's next

Queue from `docs/NEXT_STEPS_2026-10-08.md` § Critical path: Track U item 4 → `datadir-flock`
→ Track E → Track W. #708 claims to close Track U item 4 and `datadir-flock`; if it merges,
Track E (encryption at rest) is the top, and `feat/encrypt-rules-file` plus #709 are already
Track E work.

### New gaps seeded this session (not yet in the planning doc)

- `graphdb:v1.4-clear-vector-insert-window` — a vector insert after the unlock can land in the
  freshly emptied index for a deleted node. Needs an epoch check or a lock change.
- `graphdb:v1.4-clear-property-index-definitions` — decide one rule for vector and property
  index definitions across `DeleteAllNodes`.
- `graphdb:v1.4-clear-disk-backed-edges` — `clearAllUnderLock` never touches `gs.edgeStore`;
  probe first, the defect is unconfirmed.

Track W in the planning doc should absorb #715 and #716 as done.

## Stale assumptions to retire

- The previous session's handoff (in `.remember/`, not the repo) said "clearAllUnderLock does
  not reset stats.NodeCount/EdgeCount, tenantNodeIDs/tenantEdgeIDs or the vector index". Fixed
  in #714. The comment at `pkg/storage/ephemeral_test.go:147` that repeated it was corrected in
  the same PR.
- `docs/NEXT_STEPS_2026-10-08.md` § Releases since the predecessor stops before v1.11.0 and
  v1.11.1. Add both.
- `docs/internals/design/NEXT_SESSION_PROMPT.md` pointed at the 2026-10-22 01:54Z handoff,
  skipping #710 and the v1.11.0 session. This handoff replaces it.

## Open questions for the user

1. **#710** (2026-10-22 11:06Z handoff): merge or close? It is three releases old, and this
   handoff supersedes its "what's next".
2. **Per-write fsync default**: carried from the previous session. Mapper and snare both pay
   for it (~137 µs per fsync on ext4), and the earlier justification was measured on tmpfs
   (auto-memory `go-bench-tempdir-is-tmpfs`). Coord has `v1.4-wal-default-remeasure`.
3. **`feat/encrypt-rules-file`**: a local commit with no PR. Open one, or is it waiting on #709?

## Next-session prompt (paste-ready)

```
Read docs/internals/design/SESSION_HANDOFF_2026-11-07-0139Z.md, then docs/NEXT_STEPS_2026-10-08.md.
First resolve the handoff's three open questions with the user (#710, the fsync default,
feat/encrypt-rules-file).
Then pick up Track E (encryption at rest) if #708 has merged, else Track U item 4.
Before any coord write that is not your own claim, run ListAgents and message the coord
session if one is live.
Every fix lands red-first: record the pre-fix output in the PR.
End via the session-handoff skill.
```
