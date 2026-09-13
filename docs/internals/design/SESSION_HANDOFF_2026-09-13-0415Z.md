# Session handoff — 2026-09-13 04:15 UTC

**Date**: 2026-09-13 (single session, 13:04–14:20 AEST; supersedes `SESSION_HANDOFF_2026-09-13-0153Z.md`)
**Outgoing model**: Claude Fable 5.1
**Delegation**: two review subagents on the #602 diff. `reviewer` on opus (high effort, lock ordering and shared state): found one critical and one warning, both real, both fixed with a red-first test each; the main loop verified each finding against the code before acting. `security` on sonnet (low effort): no findings on the encryption and multi-process risks; report used unchanged. Peer session `graphdb-coord-89` was told before the seed apply and after the release; it had no writes of its own.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The JSON-mode half of the "Close must not rewrite when clean" defect is closed (#602, `fcb1b1a`): on the 2.0M-node ICIJ JSON store a read-only Close went from 25.4 s and 18.4 GB peak RSS to under 1 ms and 6.5 GB, file untouched. graphdb again has **no pending coord task**; the planning-doc row update is open as #603.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #601 | chore(coord): seed v1.4-json-close-no-rewrite-when-clean | Coord node 461. Seeded with `coord-task`, one create, ten skips. |
| #602 | fix(storage): Close skips the snapshot.json rewrite when the JSON session wrote nothing | `fcb1b1a`. Sync point (WAL boundary LSN + encryption engine) in `json_snapshot_clean.go`; `DeleteAllNodes` invalidates with an epoch; sticky flag on every WAL write error; clean check runs once for both formats ahead of edge compression (`snapshotAlreadyOnDisk`); JSON publish under `jsonPublishMu`. Twelve tests, each seen red (two on main as written, ten by removing their guard). Race ×3 clean three times, lint 0. Coord task released with it, lesson `lesson-1789272897-e52a7c` recorded. |

Open at handoff: **#603** (planning doc: mark the JSON half done) — docs-only, merge on green.

## 3. Current state

- `origin/main` HEAD: `fcb1b1a` (#602), plus #603 when it merges.
- Open PRs: #603. Its CI was still running at handoff (benchmark jobs take ~30 min).
- Open local branches besides `main`:
  - `docs/planning-json-close-no-rewrite-done` (#603, in flight).
  - `docs/planning-sdk-parity-done`: one **unpushed** commit `6d3dfda` from 2026-09-11 ("mark SDK parity done (#591, #592, #593)"), no PR. Main has moved 36 files since. Decide: rebase that one commit onto main and open its PR, or drop it if the SDK-parity row on main is already right.
  - `v1.4/v1.4-sdk-parity`: 0 commits ahead of main, no PR by that head name. Safe to `git branch -D`.
- Uncommitted changes: none (this handoff branch aside).
- Test/lint state on `fcb1b1a`: storage suite green (30 s), `-race -count=3` green (148 s), `go test ./pkg/... ./cmd/... -short` 57 packages ok, golangci-lint v2.13.2 zero issues, `gofmt -s` empty.
- Coord (graphdb): 28 tasks, 27 done, 1 cancelled, **0 pending, 0 claimed**. Task 461 ended near $10 of a $10 seed budget before the review fixes; the user granted $50 and the rest of the task fitted in it.
- Scratch state under this session's scratchpad: a `closebench` harness (opens a JSON store read-only, reads one node, times Close) in `harness-after/` and `harness-before/`, each a one-file module with a `replace` onto a graphdb tree. The previous session's ICIJ data dirs (`data/icij-json`, 1.7 GB; `data/icij-mmap`, 1.1 GB) are under the `25dbae29-…` scratchpad and are what the numbers above were measured on. All rebuildable from `COI_SCREEN_REAL_CORPUS_2026-09-13.md`.

## 4. What's next

Nothing is seeded. Candidates, in the order this session would pick them:

1. **mmap clean-check cost.** `mmapSnapshotCleanLocked` → `buildMmapMetadata` clones every property index once per Close. A digest kept in the header, or a compare that does not clone, makes a read-only mmap exit O(1). The JSON side has no such cost now (sync point compare is O(1)). Only worth it once a store with a large property index shows up in a profile; the ICIJ store has none.
2. **Pre-existing publish hazard, not introduced here, worth a task.** Two concurrent JSON `Snapshot()` calls could always clobber each other's `snapshot.json.tmp` and rename out of order; #602 serialises the publish under `jsonPublishMu`, which closes the temp-path clash. What it does not close: `CompactWAL` truncating WAL entries ≤ boundary_B after an older publish A renamed last. The security subagent noted the same gap across processes (no file lock). Scope: decide whether `Snapshot()`/`CompactWAL` should also refuse to rename over a newer file, or whether the fix is a data-dir lock.
3. **Fail-soft WAL writes.** `enqueueWAL` and `writeToWAL` log an append failure to stderr and return success to the API. #602 makes Close write in that case, so the node survives, but the caller was still told a durable write happened when it had not. The "loud refusal" objective says this should be an error. Big surface (every single-op write path), so seed it as its own task.
4. **consumer-drive SKIP policy** and **coi-screen M1 steps 3–5**: unchanged from the 01:53Z handoff §4 items 3–4.
5. Off-path, unchanged: GraphQL damage signal (ADR 0003), predicate-aware storage page method, sorted-bucket cache, onboarding docs, CI hygiene, the junk-test sweep.

## 5. Stale assumptions to retire

- `docs/NEXT_STEPS_2026-06-18.md` line 85 "**Still open**: JSON mode (25 s / 17.5 GB per exit) has no overlay to compare and needs a different proof" → fixed by #603 (pending). If #603 is closed instead, redo that one edit.
- `CLAUDE.md` § "Snapshot format stability" says nothing about Close skipping; a reader might assume `Close` always writes in JSON mode. It now skips when clean in **both** formats. Consider one sentence there next time that section is touched (not this PR — CLAUDE.md edits are their own change).
- The 01:53Z handoff §4 item 1 said the JSON proof would need "a write generation counter under `gs.mu`". It did not: a sync point on the WAL LSN plus an epoch and a sticky WAL-error flag covers it without a per-write counter. The reasoning is in the header comment of `pkg/storage/json_snapshot_clean.go`.
- Any belief that a `go test ./pkg/...` failure in `pkg/auth/oidc` (`TestOIDCHandler_Callback_Success`, "connection timed out" to 127.0.0.1) is a code defect: it failed once, for 136 s, while an 18 GB harness and the race suite ran together, and passes alone on main and on the branch. Load-induced.
- Auto-memory `coord-budget-hook-blocks-all-tools`: updated this session with two facts — a refused tool call ran nothing (edits inside a refused Bash heredoc were lost and had to be re-applied), and a $10 seed budget does not cover a storage task with a review round.

## 6. Open questions for the user

- The unpushed `docs/planning-sdk-parity-done` commit from 2026-09-11 (§3): rebase and open, or drop?
- §4 item 3: should fail-soft WAL writes become errors at the API? That changes the contract of every single-op write; the objective says yes, the blast radius says seed it and scope it first.
- consumer-drive SKIP: fail or warn? Carried from the previous handoff.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first.
2. If #603 is still open, merge it on green, then `git checkout main && git pull --ff-only`.
3. Nothing is claimable: pick from §4, seed it with the `coord-task` skill (dry run first; message `graphdb-coord-*` before the apply, find it with `ListAgents`), then `coord next --claim`.
4. Ask for the task budget early. The hook refuses every tool at 95%, and only the user can grant, with the `task:` prefix.
