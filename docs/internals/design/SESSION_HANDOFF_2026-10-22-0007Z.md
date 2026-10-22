# Session handoff — 2026-10-22 00:07 UTC

**Date**: 2026-10-22 (single session, 13 PRs merged #689–#699, #701, #702, plus the `v1.9.0` and `v1.10.0` tags)
**Outgoing model**: Claude Opus 5.5
**Delegation**:
- `Explore`, sonnet — mapped every data-directory open path, `Close`, the crash helpers and the build targets for the flock design. Used after the main loop re-read the three load-bearing facts; one claim was wrong (it merged `testCrashableStorage`, 56 call sites, with `testCrashRecovery`, which has **0** callers) and was corrected before the design PR.
- `worker`, sonnet — swept the 45 in-process crash reopens in 16 test files onto `reopenAfterSimulatedCrash`. Used after a diff review of the two loop changes; it also caught a `gofmt -s` misalignment in the main loop's own `storage_types.go` edit.
- `reviewer`, opus — post-merge review of #692 and #695 against nine numbered doubts. Found one real defect (fixed in #698) and one risk (finding 5, below, not yet fixed). Its seven OK verdicts were not re-derived.

**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

v1.9.0 is released (Release and Docker green). Every node write path now enforces the uniqueness
rules — the storage `Batch` and `Transaction` were the last (#692, #698) — so Track U is closed. A
data directory is now locked against a second open (`flock` on `<DataDir>/LOCK`, #694/#695), and
`StreamNodes` no longer misses nodes after a delete or skips a damaged record (#697).

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #689 | docs: session handoff — 2026-10-21 04:25 UTC | previous session's handoff, merged by the user |
| #691 | chore(coord): seed five gaps found on 2026-10-21 | tasks were already live |
| #690 | chore(release): v1.9.0 changelog, release pointers and chart appVersion | user tagged `v1.9.0` on `3e0c8a6` (annotated, unsigned like v1.8.0); Release + Docker `success`, 11 assets |
| #692 | fix(storage): enforce uniqueness rules in Batch and Transaction | `Batch` checks per operation in order (keeps earlier ops, as every Batch error does); `Transaction` checks the **end state** in `validateLocked`, so a key swap commits and a refused commit applies nothing. New `uniquenessRegistry.createRule` is now the one create-time rule selection (create, upsert, Batch, Transaction); `planUniquenessEnforcement` removed. 10/13 tests red first |
| #693 | docs(changelog): record #692 under Unreleased | merged by the user before CI finished; CI on main went green after |
| #694 | docs(design): inter-process lock on the data directory | `DESIGN_datadir_flock_2026-10-22.md`; user said "proceed with recommendations", so §7's five calls are the author's |
| #695 | fix(storage): lock the data directory against a second open | `vfs.Locker` optional capability (beside `Mapper`), `flock(LOCK_EX\|LOCK_NB)` on unix, `ErrLockUnsupported` elsewhere; `*DataDirLockedError` / `ErrDataDirLocked`. Released last in `releaseAfterFailedOpen` and `Close`. Cross-process test re-execs the test binary and `SIGKILL`s the holder. Unexported `skipDataDirLock` for crash tests. `TestPartialSnapshot` discarded its reopen error and the first refusal panicked the whole suite after 0.3 s |
| #696 | docs(changelog): record #695 under Unreleased | |
| #697 | fix(query): stream every live node, and report a clean end as an end | `StreamNodes` enumerates `GetAllNodeIDs` (sorted) and ends on any error but `ErrNodeNotFound`. Second commit: `ResultStream.Next` returned `context.Canceled` for a **normal** end (Close cancels before it closes the channel); found only under `-race` (round 43 vs 2154). `Next` now waits on the channel alone |
| #698 | fix(storage): a transaction update does not conflict with its own node | opus review finding 1 on #692: two rule names on one `(Label, PropertyKey)` made a node conflict with itself in `Transaction.Commit` |
| #699 | docs(changelog): record #697 and #698 under Unreleased | |
| #701 | feat(server): bind one address with -host / GRAPHDB_HOST | asked by `ulysses-69` (macOS port sharing with a wildcard bind; LAN exposure). `GRAPHDB_HOST`, not `HOST` (zsh). Default unchanged. Red first: the server answered on the LAN address; binary smoke-tested with `ss` |
| #702 | chore(release): v1.10.0 changelog, release pointers and chart appVersion | merged after the user tagged `v1.10.0` on `1cdae6c`; also restored the CHANGELOG compare links, which stopped at v1.7.0 |

## Current state

- `origin/main` HEAD: `e623069` (#702). #699 and #701 were merged on the user's "merge in order"
  while their two benchmark jobs were still running; every other check was `SUCCESS`.
- **v1.10.0 is tagged on `1cdae6c`, the commit BEFORE the release PR #702** (`e623069`, merged
  after the tag). The binaries, the Docker image and the GitHub release notes (goreleaser uses the
  commit log) are complete: #702 changed only documents. In the tagged tree the CHANGELOG still
  lists these entries under `[Unreleased]`, README and the roadmap say v1.9.0, and the chart says
  `appVersion "1.9.0"`; `main` is correct. The tag was kept rather than moved, because moving it
  would delete a published release and overwrite the `1.10.0` Docker image. For the next release,
  merge the release PR before tagging. #702 was merged with 8 checks still running (none failed).
- Release + Docker for `v1.10.0` were running at handoff; confirm with
  `gh run list --branch v1.10.0 --json workflowName,conclusion`. When both are `success`, **send the
  tag to the Ulysses session** — it asked for `-host` (#701), will vendor the release and pass
  `-host 127.0.0.1`. The macOS port-sharing fix was not measured here; ask Ulysses to confirm.
- Open branches: local `main` only (plus the stale 2026-10-21 worktrees below). **Stale from 2026-10-21**: seven
  `.claude/worktrees/agent-*` worktrees (branches `feat/rest-upsert-node-main`,
  `feat/go-client-upsert-node`, `fix/patch-node-uniqueness-rules`, `feat/storage-upsert-node`,
  `fix/cypher-create-uniqueness-rules`, `fix/snapshot-rlock-panic-safety`,
  `feat/rest-conflict-node-id`), their `worktree-agent-*` branches, `pr674`, and the separate
  worktree `../graphdb-ulysses-contracts` (`feat/ulysses-consumer-contracts`). All their PRs are
  merged; run the `branch-cleanup` skill.
- Uncommitted changes: none.
- Gates on the last code PR trees (#697, #698): full `pkg/storage` suite without `-short`, `-race`
  on the touched tests, all 57 packages `-short`, golangci-lint v2.13.2 0 issues, `contract-guard`
  31 contracts. CI on `main` green through `ba0ef25`; later merges were green as PRs.
- Coord: `v1.4-tx-batch-uniqueness-rules`, `v1.4-datadir-flock`, `v1.4-stream-nodes-id-gaps` are
  `done`, each with a lesson. No claim held by this session.

## What's next

From `docs/NEXT_STEPS_2026-10-08.md`, critical path after this session:
~~Track U item 4~~ (#692) → ~~`datadir-flock`~~ (#695) → **Track E** → Track W.

1. **Track E — encryption at rest**: `v1.4-encryption-no-plaintext-test` (acceptance check) →
   `v1.4-encrypt-uniqueness-rules-file` → `v1.4-encryption-envelope-test-cannot-fail` →
   `v1.4-encryption-key-zeroing`.
2. Track W: `v1.4-wal-default-remeasure` (needs real disk, not tmpfs — see memory
   `go-bench-tempdir-is-tmpfs`), then `v1.4-bulk-create-group-commit` with
   `v1.4-storage-batch-atomic-commit`.
3. **Confirm the v1.10.0 Release + Docker runs**, then tell Ulysses the tag (see Current state).
4. Parallel-safe: the four remaining gaps from #691 (`v1.4-batch-create-wal-error-text`,
   `v1.4-upsert-wal-failure-202-test`, `v1.4-go-client-ci-lint`,
   `v1.4-go-client-return-eval-order`), Track S items 3, 5, 7.

### New gaps (not seeded — no coord session was live; the coord session owns seeds)

- **`Snapshot()` and `CompactWAL()` do not check `closed` at entry, and `Close` does not take
  `compactMu`** (opus review finding 5; the entry check verified by the main loop). A backup (`handlers_backup.go:39`) or tenant-compaction
  (`handlers_tenant.go:344`) handler still running after the server's 30 s shutdown timeout can
  write `snapshot.*` after `Close` released the directory lock, while a new process holds it.
  Older than #695, but #695 now promises one writer per directory. Read at `3203dae`.
- **`graphdb-admin backup restore --into` takes no directory lock.** It writes into a directory a
  running server can hold (`cmd/graphdb-admin/backup.go:146`, `backup.Extract`). It could take
  `vfs.LockFile` for the length of the extract. Read at `ba0ef25`.
- `testCrashRecovery` in `pkg/storage/testhelpers_test.go` has no callers (dead helper).

## Stale assumptions to retire

- `docs/NEXT_STEPS_2026-10-08.md` Track U item 4 (`v1.4-tx-batch-uniqueness-rules`) → done in #692
  (+ #698). Track U is complete.
- `docs/NEXT_STEPS_2026-10-08.md` Track S item 4 (`v1.4-datadir-flock`) → done in #694/#695.
- `docs/NEXT_STEPS_2026-10-08.md` critical path line ("→ Track U item 4 → `datadir-flock` → Track E
  → Track W") → Track E is now the head.
- `CHANGELOG.md` `[1.9.0]` intro says "The storage `Batch` and `Transaction.Commit` still skip the
  registry (`v1.4-tx-batch-uniqueness-rules`)" — true for 1.9.0, fixed in `[Unreleased]` (#692).
  Leave the released section; the `[Unreleased]` entry records the fix.
- Memory `auto-mode-git-blocks-in-graphdb.md`, the 2026-10-21 paragraph ("hand the merge and tag
  commands to the user") → on 2026-10-22, after the user said "merge in order on green", five
  single `gh pr merge` calls passed (#694–#698), three of them from a background script. The tag push was still done by the user. Keep "one merge per command, confirm by state".
- Planning-doc `NEXT_STEPS` has no row for the `ResultStream.Next` clean-end defect; it is fixed
  (#697), so nothing to add beyond a done note under the gap list from #691.
- `CLAUDE.md` has nothing on the directory lock. Worth one line under "Snapshot format stability"
  or "Known pitfalls": `<DataDir>/LOCK` exists, is never deleted, is not in a backup, and an
  in-process crash test must reopen with `reopenAfterSimulatedCrash`.

## Open questions for the user

1. **Duplicate rule keys.** `RegisterUniquenessRule` accepts two rule names on one
   `(Label, PropertyKey)`. Refuse the second, or permit it and say so in ADR 0001? #698 is correct
   either way; creates under two covering rules already refuse with `ErrMultipleUniquenessRules`.
2. **Seed the two new gaps** (Snapshot/CompactWAL after Close; restore lock) — needs the coord
   session.
3. Carried from 2026-10-21, no answer recorded: the bare `RETURN n` column name `"n."` quirk; and
   telling the syntopica-timer-spec owner that #680 can now give a 409 where it got a duplicate.

## Next-session prompt (paste-ready)

```
Read docs/internals/design/SESSION_HANDOFF_2026-10-22-0007Z.md first, then docs/NEXT_STEPS_2026-10-08.md.

1. Confirm the v1.10.0 Release and Docker runs: gh run list --branch v1.10.0 --json workflowName,conclusion.
   If both are success, send the tag v1.10.0 to the Ulysses session (it asked for -host, #701) and ask it to
   confirm the macOS bind. The tag sits on 1cdae6c, before the release-docs PR #702; see the handoff.
2. Run ListAgents. If a graphdb-coord session is live, send it the two unseeded gaps in the handoff's
   "New gaps" section (with their file:line) and ask it to seed them. Ask the user question 1
   (duplicate rule keys) before touching RegisterUniquenessRule.
3. Run the planning-doc-update skill for Track U item 4, Track S item 4 and the critical path.
4. Then Track E: claim graphdb:v1.4-encryption-no-plaintext-test. Write the test so it can fail
   (base64 values defeat a raw-text search — see v1.4-encryption-envelope-test-cannot-fail), see it
   red, then do v1.4-encrypt-uniqueness-rules-file against it.
5. Gates as in CLAUDE.md; run pkg/storage WITHOUT -short (CI does; -short skips most crash tests).
6. End with the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-10-08.md` — Track E.
3. For Track E, read `pkg/storage/uniqueness_rules.go` (`persistUniquenessRulesLocked`) and the
   encryption tests named in the queue before writing anything.
