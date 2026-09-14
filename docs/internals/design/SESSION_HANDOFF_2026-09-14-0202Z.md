# Session handoff — 2026-09-14 02:02 UTC

**Date**: 2026-09-14 (single session, 09:51–12:02 AEST; four PRs merged, one open)
**Outgoing model**: Claude Fable 5.1
**Delegation**: the user asked for every tier and for contact with the other agent. Two sonnet `Explore` agents mapped the code for each task (reports used as read, one cited line drifted and is noted below). The main loop wrote one specification per task, chose the design (monotonic LSN, 202 status), reviewed every diff, ran the race gate and wrote the PR bodies. A sonnet `tester` wrote the snapshot tests in a scratch worktree and saw them red; a sonnet `general-purpose` implemented the snapshot change; a second sonnet `general-purpose` did tests-then-code for the API task. Two opus `reviewer` agents gave GO with findings, all applied by the same implementers (one finding was reworked at the storage layer after the main loop rejected an API-layer copy of the cascade). A sonnet `worker` rewrote both PR bodies to zero ASD-STE100 findings. Peer session `graphdb-coord-6f` was told before the claims and after each release; it verified two seed candidates against main and queued three for its user's approval.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The two follow-ups of the WAL write-error contract (#606) are done: a snapshot records the WAL boundary LSN and replay skips covered entries (#611), and REST/GraphQL tell a client that a write applied and must not be retried (#612). The WAL LSN is now monotonic for the life of the data directory. graphdb has **0 pending, 0 claimed** coord tasks until the coord session's three seed entries land.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #610 | docs(planning): mark SDK parity done (#591, #592, #593) | The stale local branch from 2026-09-11, rebased and merged. Resolves the open question carried since the 04:15Z handoff. |
| #611 | fix(storage,wal): a snapshot records the WAL boundary LSN and replay skips covered entries | `bfd9658`. Additive `WALBoundaryLSN` in both snapshot formats, no version bump. `Truncate` no longer resets the LSN on any backend; the constructor raises the counter to the recorded boundary (`RaiseLSNTo`) before replay — without that a clean close then a reopen hands the next write LSN 1 and a later replay drops it (T2 guards it, seen red with the raise removed). T1 seen red on `2222218` for json and mmap. `faultsim.WALRotateReopen` cannot reproduce the defect (fires after the real rename succeeded); the test refuses the real rename through `vfstest.RoleFS`. Opus review drove five extra paths on five backend shapes, no loss. Race ×3 clean. |
| #612 | feat(api,graphql): a write whose WAL append failed answers applied-not-durable, do not retry | `f24f598`. REST 202 with `applied/durable/retry` fields; the create bodies are a superset of the 201 body. GraphQL error extensions `WAL_WRITE_FAILED` + id; `pkg/graphql/http.go` forwards extensions (it dropped them before). `DeleteAllNodesForTenant` and `DeleteTenant` complete the cascade past a WAL failure and return `ErrWALWriteFailed` at the end (two storage tests seen red). `openapi.yaml` and `API.md` carry the 202. Both handler tests seen red on `2222218`. |
| #613 | docs(planning): mark the two WAL follow-ups done | **Open at handoff**, docs only, merge on green. |

## 3. Current state

- `origin/main` HEAD: `f24f598` (#612), plus #613 when it merges.
- Open PRs: #613 (planning doc, CI running at handoff).
- Open local branches besides `main`: `docs/planning-wal-followups-done` (#613) and this handoff branch. The two task worktrees are removed and their branches deleted.
- Uncommitted changes: none.
- Test/lint state on `f24f598`: #611 ran `-race -count=3` storage 148 s and wal 18 s ok; both PRs ran golangci-lint v2.13.2 zero issues, `gofmt -s` empty, `make contract-guard` OK (16 contracts, 24 guarding tests); CI green on both before merge, #612 re-run after a rebase on #611.
- Coord (graphdb): 32 tasks, 29 done, 1 cancelled, 0 pending, 0 claimed. Three lessons recorded this session (`lesson-1789349184-e3eb0d`, `lesson-1789349196-71cd12`, `lesson-1789351275-bbb16d`).
- Budget window at session start: 64 %. No task-level cap was hit.

## 4. What's next

The coord session (`graphdb-coord-6f`) holds three graphdb seed entries pending its user's approval. When they exist, `coord next --claim` gives one of them:

1. **`v1.4-boundary-lsn-downgrade-guard`** (depends on the done 486). A binary at or before `2222218` resets the LSN to 0 on truncate. Measured loss: this binary closes cleanly at boundary 3, the old binary opens, writes LSN 1 and crashes, this binary opens and skips LSN 1. Guard: refuse the open when `0 < recovered WAL LSN < recorded boundary`. A failed truncate gives `recovered == boundary` and never fires it. Open decision: a torn WAL tail can read low and would refuse a valid store.
2. **`go-client-no-retry-on-post`** (depends on the done 485). `clients/go/transport.go:151` retries every 5xx with no method check (TS and Python exclude POST, audit M-11). Also read the 202 body and return a typed not-durable error with the id.
3. **`gate-private-netns-graphdb`**. Portmaster drops loopback SYNs in the local `go test ./pkg/...` gate (`pkg/auth/oidc` timed out at 136 s); reuse `in_private_netns` from graphdb-coord `scripts/lib/netns.sh`.
4. Off-path, unchanged: mmap clean-check cost (`buildMmapMetadata` clones every property index per Close), consumer-drive SKIP policy, coi-screen M1 steps 3–5, GraphQL damage signal (ADR 0003), onboarding docs, the junk-test sweep.

## 5. Stale assumptions to retire

- Anything that says the WAL LSN restarts at 0 after `Truncate` or after `DeleteAllNodes`. `pkg/wal` keeps the counter; the constructor raises it to the snapshot boundary. Four `pkg/wal` tests and the comments in `json_snapshot_clean.go`, `node_operations.go`, `truncate_upto.go` and `json_close_clean_test.go` were corrected in #611. `CLAUDE.md` § "Snapshot format stability" does not mention the boundary field; a one-line addition ("both formats carry an additive `WALBoundaryLSN`; replay skips entries at or below it; the WAL LSN is monotonic") is due but was not made here (handoffs do not edit `CLAUDE.md`).
- The `DeleteAllNodes` sync-point discard in `json_snapshot_clean.go` is now load-bearing, not defensive: with a monotonic LSN the boundary after the truncate equals the one the sync point recorded.
- The spec's claim that `faultsim.WALRotateReopen` forces a truncate failure. It only fakes the error after the real rename; lesson `lesson-1789349196-71cd12`.
- Anything that says REST answers 500 for `ErrWALWriteFailed`, or that GraphQL errors carry no extensions. `WriteNotDurableResponse`, `NodeNotDurableResponse`, `EdgeNotDurableResponse` in `pkg/api/types.go`; `walWriteFailedError` in `pkg/graphql/wal_errors.go`.
- The 10:40Z handoff §4 item 2 said "one handler test per surface". #612 has eight REST rows and two GraphQL cases, one on the production schema builder.
- The Explore report for the API task cited `clients/python/_retry.py`; the file is `clients/python/src/graphdb_client/_retry.py` (the coord session corrected it).
- A wrong snapshot boundary now costs a dropped write where it cost a duplicate before. `walLSNBarrieredLocked` is the only defence for the `Transaction.Commit` window.

## 6. Open questions for the user

- The downgrade guard's policy for a torn WAL tail that reads low: refuse (loud, may block a valid store) or warn and replay (silent, may resurrect). The reviewer leaned to refuse.
- Whether the Go client's 202 handling (task 2 above) should also stop retrying PATCH and PUT, matching TS, or only POST.
- consumer-drive SKIP: fail or warn? Carried.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first, then `docs/NEXT_STEPS_2026-06-18.md` (the #608/#613 row under track D carries the WAL work).
2. If #613 is still open, merge on green, then `git checkout main && git pull --ff-only`.
3. Ask `graphdb-coord-6f` (or its successor) whether the three seed entries exist before you claim; do not seed them yourself.
4. If picking up the downgrade guard, read `pkg/storage/storage.go` around `raiseWALLSNToSnapshotBoundary`, `pkg/wal/wal.go` `recoverLSN`, and `pkg/storage/snapshot_boundary_test.go`.
