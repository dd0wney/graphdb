# Session handoff — 2026-10-22 01:54 UTC

**Date**: 2026-10-22 (addendum to `SESSION_HANDOFF_2026-10-22-0007Z.md`, same session; that file was
merged as #700 before the items below, and a merged handoff is not edited)
**Outgoing model**: Claude Opus 5.5
**Delegation**: none since the 00:07 handoff.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Everything the 00:07 handoff left pending is closed: v1.10.0 is released and Ulysses has the tag,
the stale worktrees and branches are gone, and `RegisterUniquenessRule` now refuses a second rule on
a held key (#703, the user's answer to the 00:07 open question 1). Read the 00:07 handoff for the
rest of the session; this file supersedes its "Current state", its open question 1 and its
next-session prompt.

## What's done since the 00:07 handoff

| PR / action | Notes |
|---|---|
| v1.10.0 tag (user) | on `1cdae6c`, before the release-docs PR #702 (explained in the 00:07 handoff, which was updated before #700 merged). Release `success`, Docker `success`, 11 assets, published 2026-10-22 00:44 UTC. CI on `main` for `1cdae6c` and `ecb0afb` all `success`; #702's checks all `SUCCESS` after its merge |
| Message to `ulysses-69` | tag `v1.10.0`, usage `-host 127.0.0.1` / `GRAPHDB_HOST`, and the two behaviour changes that can reach it (second open of a held data directory fails; `Batch`/`Transaction` refuse a held key). Asked it to confirm the macOS bind with its sidecar test; no reply yet |
| Branch cleanup (user-approved list) | 8 worktrees removed (7 `.claude/worktrees/agent-*`, `../graphdb-ulysses-contracts`), 16 branches deleted. Each was verified on origin through a merged PR first: `feat/ulysses-consumer-contracts`' 7 local-only commits are the first 7 of #631's 8; `pr674`'s tip is #674's merged head. Only `main` remains |
| #703 fix(storage): refuse a second uniqueness rule on a held key | `ErrUniquenessRuleKeyTaken`; REST 409. The loader still admits such a pair from an existing `rules.json` (a refusal at open would lock the operator out) — the one place the loader admits what Register refuses; ADR 0001 "Amendment (2026-10-22): one rule per key". `TestUniquenessRules_CountCap` now uses a key per rule; the #698 test now loads its pair from `rules.json` and was re-proven a gate |
| #704 docs(changelog): record #703 under Unreleased | merge-on-green watcher was running at this handoff |

## Current state

- `origin/main` HEAD: `55d4b6d` (#703), or #704's merge commit if it has landed.
- Open PRs: #704 (CHANGELOG) and this handoff's PR.
- Open branches: `main` only, plus the branches of those two PRs.
- Uncommitted changes: none.
- Coord: no claim held. No coord session was live all session; the two gaps below are still unseeded.

## What's next

1. **Ulysses**: if it reports that the macOS sidecar test still fails with `-host 127.0.0.1`, that is
   a graphdb defect in #701 — reproduce from its report before changing anything.
2. **Coord seeds** (needs a `graphdb-coord` session; see the 00:07 handoff's "New gaps" for
   file:line): `Snapshot()`/`CompactWAL()` refuse after `Close`; `graphdb-admin backup restore
   --into` takes the data-directory lock.
3. **Small fix**: `POST /admin/uniqueness-rules` answers 400 when the registry cannot persist to
   disk (`pkg/api/handlers_admin_uniqueness.go`, the `RegisterUniquenessRule` error branch). That
   is a server error and must be 500; only validation and `ErrUniquenessRuleKeyTaken` (409) are the
   client's fault. Read at `55d4b6d`.
4. Planning-doc update (`planning-doc-update` skill): Track U and Track S item 4 done; critical path
   head is Track E.
5. **Track E**: `v1.4-encryption-no-plaintext-test`, then `v1.4-encrypt-uniqueness-rules-file`.

## Stale assumptions to retire

- `SESSION_HANDOFF_2026-10-22-0007Z.md` "Current state" (open PRs, worktrees, "Release + Docker for
  `v1.10.0` were running") and open question 1 (duplicate rule keys) → closed as above.
- `CHANGELOG.md` `[1.10.0]` names no duplicate-key refusal: correct, it shipped after the tag and is
  under `[Unreleased]` (#704).

## Open questions for the user

1. Carried from 2026-10-21, still unanswered: the bare `RETURN n` column name `"n."`; and telling the
   syntopica-timer-spec owner that #680 can now give a 409 where it got a duplicate.

## Next-session prompt (paste-ready)

```
Read docs/internals/design/SESSION_HANDOFF_2026-10-22-0154Z.md, then SESSION_HANDOFF_2026-10-22-0007Z.md,
then docs/NEXT_STEPS_2026-10-08.md.

1. If #704 is open, check gh pr view 704 --json statusCheckRollup and ask the user to merge.
2. Run ListAgents. If a Ulysses session reports on the -host macOS check, act on it first. If a
   graphdb-coord session is live, hand it the two unseeded gaps (file:line in the 00:07 handoff).
3. Fix the admin register 500-vs-400 mapping (red first), then run the planning-doc-update skill.
4. Then Track E: claim graphdb:v1.4-encryption-no-plaintext-test; write the test so it can fail
   (base64 values defeat a raw-text search), see it red, then v1.4-encrypt-uniqueness-rules-file.
5. Gates as in CLAUDE.md; pkg/storage WITHOUT -short. End with the session-handoff skill.
```
