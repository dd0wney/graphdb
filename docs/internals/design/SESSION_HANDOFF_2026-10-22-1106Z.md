# Session handoff — 2026-10-22 11:06 UTC

**Date**: 2026-10-22 (single session, started from `SESSION_HANDOFF_2026-10-22-0154Z.md`; 2 PRs
merged, 2 green PRs wait for the user, 1 commit is local only)
**Outgoing model**: Claude Opus 5.5
**Delegation**: one `worker` subagent on sonnet built the sealed `rules.json` format (version 2).
The main loop did not use its result unchanged. It found two defects: a wrong-key open resealed a
version 1 file under the wrong key, and a helper comment made a false claim about open order. Both
are fixed in the local commit `93f354b` (see "Current state").
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The admin register endpoint now answers 500 for a persist failure (#706). Track E has a test that
can fail and proves an encrypted store writes no caller names or values in plaintext (#709, green,
not merged). The fix that seals `rules.json` in an encrypted store is done and gated, but exists
only as a local commit stacked on #709.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #706 | fix(api): answer 500, not 400, when the rule registry cannot persist | Allow-list mapping (`registerRuleErrorResponse`): `ErrUniquenessRuleKeyTaken` → 409, new sentinels `ErrInvalidUniquenessRule` and `ErrUniquenessRuleLimit` → 400, all else → 500 through `sanitizeError`. The user chose to keep the rule cap at 400. Red first: the persist test got 400, the cap test got 500 without the cap case. `vfstest.FaultFS.Clear()` resets `Fired()`, so read `Fired()` first |
| #707 | docs(changelog): record #706 under Unreleased | |

Open, all 10 checks green at 11:06 UTC, waiting for the user's merge approval:

| PR | Title | Notes |
|---|---|---|
| #708 | docs(planning): close Track U item 4 and datadir-flock | v1.9.0 and v1.10.0 rows, Track S item 4 and Track U item 4 done, new Track U item 5 (#703, #706), critical path head is Track E, "Unseeded gaps" lists the two gaps below |
| #709 | test(storage): prove an encrypted store writes no plaintext names or values | `pkg/storage/encryption_no_plaintext_test.go`. Seven random markers, four configs × closed/crash. Searches raw AND base64 (JSON stores `[]byte` as base64). A plaintext run is the positive control, and a reopen proves the files hold the data. Mutations M1 (no engine), M2 (no `rules.json` exception), M3 (raw search only) all go red. `ENCRYPTION_ARCHITECTURE.md` gets "What stays plaintext on disk (measured 2026-10-22)". In #709 `rules.json` is the one plaintext-by-design exception |

## Current state

- `origin/main` HEAD: `90d5660` (#707).
- Open PRs: #708, #709 (both green), and this handoff's PR.
- Local branches: `main`, `docs/planning-close-track-u-flock` (#708), `test/encryption-no-plaintext`
  (#709), `feat/encrypt-rules-file`, and this handoff's branch.
- **`feat/encrypt-rules-file` is NOT pushed.** It holds `93f354b feat(storage): seal rules.json in an
  encrypted store` on top of `ca96c3f` (#709's commit). Do not open its PR before #709 merges: a
  PR based on #709's branch closes when `--delete-branch` deletes that branch.
- Uncommitted changes: none.
- Gates on `feat/encrypt-rules-file` (`93f354b`): `gofmt -s -l` empty, `go vet` clean,
  golangci-lint v2.13.2 0 issues, full `pkg/storage` without `-short` ok (44 s), `-race -count=3` ok,
  `make test-local` exit 0.
- Coord: two claims held by this session, `graphdb:v1.4-encryption-no-plaintext-test` and
  `graphdb:v1.4-encrypt-uniqueness-rules-file`. Release each by hand after its PR merges.

### What `93f354b` does

- An encrypted store writes `rules.json` as `{"version":2,"sealed":"<base64>"}`. An unencrypted
  store still writes version 1, byte for byte as before.
- Open of version 2 with no engine fails and names `rules.json`. Open with the wrong key fails.
- A version 1 file in an encrypted store is sealed at the END of the constructor
  (`sealPlaintextRulesAfterOpen`), not at load. Reason: the registry loads before the snapshot. A
  reseal at load let a wrong-key open seal the file under that key before the snapshot refused the
  key. Red output of `TestUniquenessRules_WrongKeyOpenDoesNotResealVersion1` before the move:
  `a refused open rewrote rules.json: got {"version":2,...} want {"version":1,...}`.
- `plaintextByDesign` in the #709 test is now empty. ADR 0001 has "Amendment (2026-10-22): sealed
  file for an encrypted store". `CLAUDE.md` and `ENCRYPTION_ARCHITECTURE.md` each get one line.
- The user chose "encrypt it" for `rules.json`, and "not now" for a consumer-contract row for the
  mapper.

## What's next

1. With the user's approval: merge #708 and #709 (`--squash --delete-branch`).
2. Then `git checkout main && git pull --ff-only`, then
   `git rebase --onto origin/main ca96c3f feat/encrypt-rules-file`. Re-run the storage gates, push,
   and open the PR. The PR body must give the red outputs and the wrong-key defect above.
3. `coord release --pr 709 graphdb:v1.4-encryption-no-plaintext-test`.
4. After the `rules.json` PR merges: release `graphdb:v1.4-encrypt-uniqueness-rules-file` with its
   PR number, and open one CHANGELOG PR for #709 and that PR.
5. Then the rest of Track E in `docs/NEXT_STEPS_2026-10-08.md` (after #708 merges, Track E is the
   critical path head).

Off the critical path:

- Ulysses: no reply yet on the macOS `-host 127.0.0.1` check (asked in the 01:54 handoff's session).
- Coord seeds, when a `graphdb-coord` session is live: `Snapshot()`/`CompactWAL()` refuse after
  `Close`, and `graphdb-admin backup restore --into` takes the data-directory lock (file:line in
  `SESSION_HANDOFF_2026-10-22-0007Z.md`).

### New gaps found this session (not on the planning doc)

- The disk-backed edge store and the WAL frame header stay plaintext (IDs, sizes, types of frame,
  no caller names or values). `ENCRYPTION_ARCHITECTURE.md` (#709) records this as measured. No task
  exists. Seed one only if the user wants IDs and sizes hidden too.

## Stale assumptions to retire

- `ENCRYPTION_ARCHITECTURE.md` "What stays plaintext on disk" (added by #709) names `rules.json` as
  plaintext. That is true on `main` until the `rules.json` PR merges, and that PR corrects the
  bullet. Do not correct it by hand in a separate PR.
- `SESSION_HANDOFF_2026-10-22-0154Z.md` "What's next" item 3 (admin 400 vs 500) → done in #706.
  Item 4 (planning doc) → #708.

## Open questions for the user

1. Approve the merge of #708 and #709? Both are green. Nothing else in the queue can move until #709
   merges.
2. Carried from 2026-10-21, still unanswered: the bare `RETURN n` column name `"n."`, and whether
   to tell the syntopica-timer-spec owner that #680 can now give a 409 where it got a duplicate.

## Next-session prompt (paste-ready)

```
Read docs/internals/design/SESSION_HANDOFF_2026-10-22-1106Z.md, then docs/NEXT_STEPS_2026-10-08.md.

1. Ask the user to approve the merge of #708 and #709. Check statusCheckRollup first.
2. After #709 merges: pull main, then git rebase --onto origin/main ca96c3f feat/encrypt-rules-file.
3. Re-run the pkg/storage gates WITHOUT -short, push, and open the rules.json PR (body in STE,
   with the red outputs and the wrong-key defect from the handoff).
4. Release graphdb:v1.4-encryption-no-plaintext-test with --pr 709.
5. Run ListAgents: act on a Ulysses -host report, and hand a live coord session the two gaps.
6. After the rules.json PR merges: release its claim, open the CHANGELOG PR, continue Track E.
7. End with the session-handoff skill.
```
