# Session handoff — 2026-10-08 12:43 UTC

**Date**: 2026-10-08 (same session as the 02:50 and 06:45 handoffs, continued; 4 more PRs merged, no release since v1.7.0)
**Outgoing model**: Claude Opus 5.5
**Delegation** (since the 06:45 handoff):
- 1 × Explore (sonnet): audit of the 61 lock sites in `pkg/storage` with no defer. Used; its "confirmed bug" (nil-map write in `Transaction.Commit`) was re-checked in code and reproduced red before it was fixed.
- 1 × worker (sonnet): converted 29 of the 39 non-trivial sites, one file per commit, with a mutation red proof per test. Used after reading every diff; two of its files (`node_adjacency.go`, `edgestore.go`) were re-proved red against the real pre-fix code from `efbe8ee` (6 subtests failed).
- 1 × reviewer (opus): whole-branch review of #665. GO, no critical finding; its three nits were applied before merge.
- #664 (implicit grouping): see its PR body.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The last Cypher silent-result defect from the gap sweep is fixed (#664, implicit grouping), and `pkg/storage` no longer leaves a lock held after a recovered panic (#665). Both are on `main` and **not released**: v1.7.0 is still the latest tag.

## What's done since the 06:45 handoff

| PR | Title | Notes |
|---|---|---|
| #663 | chore(coord): seed four Cypher, index and encryption gaps | the four "gaps surfaced but not seeded" from the 06:45 handoff |
| #664 | fix(query): group Cypher aggregates as openCypher does | non-aggregate RETURN items are grouping keys; typed keys; null its own group; ORDER BY a non-key after aggregation is refused; sum/avg/min/max of an entity refused. `buildResultSet` now returns an error. No CHANGELOG entry yet. |
| #665 | fix(storage): release locks on a panic | 37 critical sections moved into methods with a deferred unlock; work that runs off-lock on purpose stays off-lock. Also fixes a real panic: a transaction update of a node created with nil properties hit "assignment to entry in nil map" under `gs.mu` (`transaction_commit.go:147` at `efbe8ee`). A panic in the transaction WAL append also left `txWALBarrier` held, so the next `Close` hung. Test-only hook `panicPoint` (`lock_panic_hook.go`); harness `assertPanicReleasesLock` (`lock_panic_test.go`). No CHANGELOG entry yet. |
| #666 | chore(coord): seed the snapshot read-lock panic-safety task | the one site #665 left out, with a proposed design |

Coord tasks released `done` since 06:45: `v1.4-cypher-implicit-grouping`, `v1.4-lock-panic-safety`.

## Current state

- `origin/main` HEAD: `68af820` (#666). Latest tag `v1.7.0` (`26de5d2`).
- Unreleased on main: #664 and #665. The `[Unreleased]` CHANGELOG section has no entries for them.
- Open PRs: this handoff only.
- Local branches: `main`, and `feat/ulysses-consumer-contracts` in another worktree (not this session's; leave it).
- Uncommitted changes: none.
- Gates on #665: CI 10/10 green; locally `golangci-lint` v2.13.2 0 issues, `gofmt -s` clean, `contract-guard` OK, `go test -race ./pkg/storage/ -count=3` ok (149 s). `make test-local` had one failure, the known `pkg/licensing` `TestTelemetryReporter_reportLoop_Cancellation` flake, under load from the concurrent race run; alone it passed 20/20 on the branch and on main, and `pkg/licensing` does not import `pkg/storage`.
- mapper-6b was told that #665 is on main and unreleased.

## What's next

From `docs/NEXT_STEPS_2026-10-08.md`; items 1 and 2 of its queue (implicit grouping, lock-panic-safety) are now done.

1. **Release v1.8.0** — CHANGELOG entries for #664 and #665, README/ROADMAP pointers, chart version and appVersion, then the usual tag-and-watch procedure. Both fix behaviour a user can hit silently (wrong aggregate rows; a hung store), so they should not wait on the next feature.
2. **Node upsert — brainstorm and spec first** (`v1.4-storage-upsert-node` → `v1.4-rest-upsert-node` → `v1.4-go-client-upsert-node`). New public API in three layers; needs the user's approval of a written spec before code.
3. `v1.4-snapshot-rlock-panic-safety` (new, #666) — the design is in the task title; benchmark the RLock hold time before and after.
4. `v1.4-batched-wal-flush-interval-validation`, `v1.4-datadir-flock`, `v1.4-telemetry-stop-waits`, then the rest of the planning-doc queue.

## Stale assumptions to retire

The 02:50 and 06:45 lists still apply, except where corrected here.

- **`docs/NEXT_STEPS_2026-10-08.md` lines 49 and 61** (read at `68af820`; do not navigate by these numbers after the next edit) list `v1.4-cypher-implicit-grouping` and `v1.4-lock-panic-safety` as the top of the queue. Both are done (#664, #665). The sequencing line near line 105 should start at the node-upsert spec.
- **06:45 handoff §6, `CAPABILITIES_2026-05-10.md` claim**: "eight are fixed in v1.6.0/v1.7.0; implicit grouping remains". Implicit grouping is now fixed on main (#664), unreleased.
- **"A panic under a storage lock hangs the store"** (mapper-6b's original report, and the `v1.4-lock-panic-safety` task title): true before #665 for every site; after #665 only the `snapshotWithBoundary` read lock (task `v1.4-snapshot-rlock-panic-safety`) and the constructor's lock (no caller ever gets that store) release by hand.
- **"`patchNode` has the only nil-map write under a lock"**: no — `Transaction.Commit` had one too; fixed in #665.

## Open questions for the user

1. Cut v1.8.0 now with #664 and #665 (recommended)?
2. Start the node-upsert brainstorm next?
3. Carried from earlier, no answer recorded: seed a task for the bare `RETURN n` column name `"n."` quirk, with a deprecation path?
4. Carried from the 06:45 handoff, no answer recorded: tell syntopica-timer-spec's owner about the CREATE stray nodes and the `DELETE r` behaviour change?

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (same text).

## How to use this handoff

1. Read this, then the 06:45 and 02:50 handoffs' §6 (still valid except as corrected above), then `docs/NEXT_STEPS_2026-10-08.md`.
2. Before trusting a coord task title written on 2026-10-08: `coord insight list --task graphdb:<id>`.
3. Message `mapper-6b` (ListAgents) before changing anything mapper depends on (`v1.4-mapper-consumer-contracts` lists it).
