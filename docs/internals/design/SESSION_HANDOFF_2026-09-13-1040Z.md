# Session handoff — 2026-09-13 10:40 UTC

**Date**: 2026-09-13 (second stage of one session, 18:11–20:40 AEST; supersedes `SESSION_HANDOFF_2026-09-13-0415Z.md`, which covers the first stage)
**Outgoing model**: Claude Fable 5.1
**Delegation**: after the user asked to use every tier, two sonnet agents ran in parallel from one written specification: `tester` wrote `wal_write_errors_test.go` (14 paths, 3 backends) and found the four resurrection failures; `general-purpose` implemented the contract across 29 sites and confirmed the resurrection on the pre-change code with its own probe. The main loop set the contract with the user, proved red against the sentinel commit itself, diagnosed and fixed the truncate defect, and ran every gate. An opus `reviewer` gave GO with two warnings and one nit, all applied. Peer session `graphdb-coord-89` was told before each seed apply and after the release; it corrected its own belief that task 438 was pending.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

A single-op write whose WAL append fails now returns `ErrWALWriteFailed` instead of success (#606, `a52ac9b`), with the in-memory change kept, the same contract `Transaction.Commit` had. Its tests found and fixed a second defect: `WAL.Truncate` flushed a sticky-error buffer, refused the truncate, and let the next open resurrect deletes. Two follow-ups are seeded; graphdb has **2 pending, 0 claimed** coord tasks.

## 2. What's done this session (this stage)

| PR | Title | Notes |
|---|---|---|
| #605 | chore(coord): seed v1.4-wal-write-errors-reach-the-caller | Node 475. |
| #606 | fix(storage,wal): a WAL append failure reaches the caller as ErrWALWriteFailed | `a52ac9b`. Contract chosen by the user from three options. 17 test cases seen red against `0d86b6a`; the four delete/drop cases seen red again against the contract with the old `Truncate`. Race ×3 clean on storage and wal, 57 packages ok, lint 0. Task released with lesson `lesson-1789295684-7fe7e3`. |
| #607 | chore(coord): seed the two follow-ups of v1.4-wal-write-errors-reach-the-caller | Nodes 485 and 486. **Open at handoff**, merge on green. |
| #608 | docs(planning): note the WAL write-error contract done (#606) and its two seeded follow-ups | **Open at handoff**, merge on green. |

Earlier this calendar day (first stage, see the 04:15Z handoff): #601, #602, #603, #604.

## 3. Current state

- `origin/main` HEAD: `a52ac9b` (#606), plus #607 and #608 when they merge.
- Open PRs: #607 (seed, 8 of 10 green at handoff), #608 (planning doc, CI started). Both docs/JSON only.
- Open local branches besides `main`: `coord/seed-wal-followups` (#607), `docs/planning-wal-write-errors-done` (#608), this handoff branch, and `docs/planning-sdk-parity-done` (one unpushed commit `6d3dfda` from 2026-09-11, no PR; the user has not yet said rebase or drop).
- Uncommitted changes: none.
- Test/lint state on `a52ac9b`: `-race -count=3` storage 149 s and wal 18 s ok; `go test ./pkg/... ./cmd/... -short` 57 packages ok; golangci-lint v2.13.2 zero issues; `gofmt -s` empty.
- Coord (graphdb): 30 tasks, 27 done, 1 cancelled, **2 pending** (`v1.4-api-wal-error-no-retry`, which depends on the done task 475, and `v1.4-snapshot-records-wal-boundary`), 0 claimed.
- Budget: task 475 spent its $10 seed before the review fixes. The first `coord budget grant … 40` was acknowledged by the daemon but the hook still read $10; the same command a second time took effect. Recorded in auto-memory `coord-budget-hook-blocks-all-tools`.

## 4. What's next

1. **`v1.4-snapshot-records-wal-boundary`** (node 486). A snapshot that lands while the WAL truncate fails for any other cause (disk error at exit) still leaves entries replay re-applies, and a create before a delete resurrects the entity. Additive field in the JSON snapshot and the mmap metadata tail (absent means 0, no version bump), replay skips `entry.LSN <= boundary`. Red-first: force the truncate to fail after a landed snapshot; a deleted node must stay deleted after reopen, both formats. This one is storage-only and closes the last known resurrection path.
2. **`v1.4-api-wal-error-no-retry`** (node 485). REST and GraphQL surface `ErrWALWriteFailed` as a generic error; a client that retries creates a second node. Handler change: status, body, and GraphQL extension that say "applied, do not retry" and carry the entity ID. One handler test per surface, red first.
3. **mmap clean-check cost** (`buildMmapMetadata` clones every property index per Close): unchanged from the 04:15Z handoff.
4. Off-path: consumer-drive SKIP policy, coi-screen M1 steps 3–5, GraphQL damage signal (ADR 0003), onboarding docs, the junk-test sweep.

## 5. Stale assumptions to retire

- Anything that says a single-op write "logs WAL errors and returns success" or names `writeToWAL`: the wrapper is gone, `waitWALPending` returns the error, and every public write path returns `ErrWALWriteFailed` on a failed append. `CLAUDE.md` does not describe this; nothing to fix there.
- Any test that calls `gs.Close()` after an injected WAL write fault and tolerates a "failed to flush WAL before truncate" error with `t.Logf`: that error no longer occurs. `TestJSONClose_FailedWALAppendRewritesSnapshot` and `wal_write_errors_test.go` both use `t.Fatalf` now. A new occurrence of that message is a regression.
- The 04:15Z handoff §4 item 3 framed this task as "the last place a caller is told a write was durable when it was not". The storage layer is closed; the API layer is not (node 485).
- The implementation agent's belief that `TestJSONClose_FailedWALAppendRewritesSnapshot` "pinned the old contract" was right and the test was updated, not kept.

## 6. Open questions for the user

- `docs/planning-sdk-parity-done` (one unpushed commit from 2026-09-11): rebase and open, or drop? Carried from the 04:15Z handoff.
- consumer-drive SKIP: fail or warn? Carried.
- The budget hook: the first grant of a pair not taking effect happened once today. If it recurs, the coord repo should look at whether a grant can race the hook's read.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first, then the 04:15Z handoff §4 for the longer queue.
2. If #607 or #608 is still open, merge on green, then `git checkout main && git pull --ff-only`.
3. `coord next --claim` gives node 486 or 485; both are claimable, 486 is the recommendation. Ask for the task budget early, and if the hook still refuses after a grant, ask for the same grant again.
