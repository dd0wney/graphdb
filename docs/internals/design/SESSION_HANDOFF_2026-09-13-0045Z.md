# Session handoff — 2026-09-13 00:45 UTC

**Date**: 2026-09-13 (single session, 10:10–10:45 AEST; one PR merged, two open and mid-flight, one PR in the coi-screen sibling repo)
**Outgoing model**: Claude Fable 5.1
**Delegation**: none. Every step ran in the main loop; the other live session (`graphdb-coord-89`, the coord repo's session) was a peer, not a subagent — see § Peer session.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The real-corpus coi-screen run (`graphdb:v1.1-coi-screen-real-corpus`, the planning doc's recommended track since June) is done: the real consumer ran end to end on the full 2.0M-node ICIJ corpus in mmap mode, decision B-1 is answered, and the run surfaced one graphdb defect — `Close` rewrites the snapshot on a session that wrote nothing — now seeded as `graphdb:v1.4-close-no-rewrite-when-clean`. The corpus had been on this machine the whole time the track was "deferred for lack of a local corpus".

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #596 | chore(coord): seed v1.4-close-no-rewrite-when-clean | Merged 00:44Z. Applied to :8090 first (tasks_created=1, tasks_skipped=9); Task node 438. |

Merged before this session but reconciled in it: #593 (v1.4-sdk-parity 3 of 3, from the reboot-lost session `graphdb-49`). Its coord claim was 43 h stale; released with `--pr 593` at 00:11Z and the task marked done.

In the sibling repo: **coi-screen PR #2** (`fix/graphdb-module-rename`, commits `fee1300` + `28a759d`) — module rename, ADR 0003 error return, `UseMmapSnapshot` in `graphload.Open`. Without it the consumer does not build against graphdb main. Pushed at the user's request; not merged.

## 3. Current state

- `origin/main` HEAD: `1509243` (#596).
- **Open PRs (mid-flight at handoff)**:
  - **#594** `v1.1/v1.1-coi-screen-real-corpus` — the run itself: `scripts/icij-merge-nodes.py`, `docs/internals/design/COI_SCREEN_REAL_CORPUS_2026-09-13.md`, in-place correction of the July runbook. Six of nine CI jobs green; macOS tests + two benchmark jobs pending at 00:45Z. **The user asked for it to merge, then #595.** After it merges: `coord release --pr 594 graphdb:v1.1-coi-screen-real-corpus` (nothing else closes the task).
  - **#595** `docs/planning-coi-real-corpus` — planning-doc update (Shape A + B + C). Merge after #594 so the PR number it cites is merged work.
- Open branches: `v1.1/v1.1-coi-screen-real-corpus`, `docs/planning-coi-real-corpus`, this handoff branch. `--delete-branch` at merge time clears the first two.
- Uncommitted changes: none.
- Test/lint state: no Go code changed on any of these PRs. CI on #594 and #596 is green on lint, coverage, pure-Go and SIMD.
- Coord: `graphdb:v1.1-coi-screen-real-corpus` is **claimed by this session** (Claim#437, agent `claude:25dbae29-…`), budget $9.99 / $35.00 after one grant. `graphdb:v1.4-close-no-rewrite-when-clean` pending, unclaimed.
- Scratch state that outlives the session (all under `/tmp/claude-1000/…/scratchpad/`, not in the repo): the unzipped corpus + `all-nodes.csv`, the mmap (1.1 GB) and JSON (1.7 GB) data dirs, the `coi`/`import-icij`/`probe-bin` binaries, and `run/*.json` screen outputs. Rebuildable in ~2 min from the recipe in `COI_SCREEN_REAL_CORPUS_2026-09-13.md`.

## 4. What's next

From `docs/NEXT_STEPS_2026-06-18.md` as amended by #595:

1. **`graphdb:v1.4-close-no-rewrite-when-clean`** — the only pending graphdb coord task once #594 releases. Safety shape first (a read-only consumer rewrites customer data on exit; two exiting together rewrite it concurrently), performance second (8.3 s / 7.9 GB mmap, 25 s / 17.5 GB JSON on the ICIJ corpus). Acceptance in the task title and in NEXT_STEPS §D. Entry point: `Close` → `Snapshot` → `snapshotWithBoundary` in `pkg/storage/persistence.go`; the mmap branch merges overlay ∪ base − tombstones unconditionally. Red-first test: mtime + bytes unchanged after a read-only open/close, changed after one write.
2. **DoD Levers 2–3 lose their pull.** B-1 is answered "not on the consumer's read path"; don't pick them up on the old framing.
3. **coi-screen's own M1 steps 3–5** (its repo, its plan): hand-labelled precision, threshold calibration, the resolver index + DFS hub bound. The pair `CORIGLIANO FRANK J.` / `MCCLAMMY EDWARD G.` at `--max-hops 2` ran 13 min in `FindInterestPaths` before it was killed — that is the reproduction.
4. Off-path, unchanged: GraphQL `X-Enumeration-Incomplete` equivalent; predicate-aware storage page method; sorted-bucket cache; onboarding docs; CI hygiene.

### Gaps surfaced this session (not on the planning doc)

- **`scripts/consumer-drive.sh` coi step never fired.** It SKIPs when `../coi-screen` is absent, and it was absent everywhere. Had it run it would have failed three ways (module rename, #531 signature, mmap refusal). Whether a SKIP should fail CI is an open call; the `CLAUDE.md` red-first section is the argument for "yes".
- **1,139 ICIJ `node_id`s appear in two of the five node files.** `import-icij` creates a node per row and keeps the last mapping. Not investigated; noted in the results doc.
- **No library-level read of `GRAPHDB_STORAGE_MODE`.** Only `cmd/server`, `cmd/graphdb-admin`, `cmd/import-icij` read it. Every embedded consumer must set `UseMmapSnapshot` itself or hit the refusal. Worth one line in `docs/API.md` or the storage package doc.

## 5. Stale assumptions to retire

- `docs/NEXT_STEPS_2026-06-18.md` §D line "deferred for lack of a local corpus" and §B/§Decision points "B-1 open" → fixed by #595 (pending merge). If #595 is closed instead, redo those five edits.
- `docs/internals/design/SPIKE_COI_SCREEN_VALIDATION_2026-07-01.md` § Limitations "not present locally" and Runbook step 3 (`GRAPHDB_STORAGE_MODE=mmap go run ../coi-screen/cmd/coi`) → corrected in place by #594. The env line is right only with coi-screen `28a759d`.
- The 2026-09-11 handoff § 5: "the coord session is `graphdb-coord-a0`" → it was `graphdb-coord-89` this session. `ListAgents` each session; the name changes.
- Auto-memory `coord-budget-hook-blocks-all-tools`: still true, and sharper now — the first two user-run grants landed nowhere (the coord session verified a prefix-less grant is refused with exit 2 and one stderr line); the third, with the `task:` prefix, printed a success JSON and the limit moved. The user's terminal history had not flushed, so the exact failed text is unknown.
- Auto-memory (new this session) `coi-screen-and-icij-corpus-locations`: `../COI` is oit-cyber/coi, NOT the consumer; `../coi-screen` is; the corpus zip is at `/mnt/ssd2/Workspace/icij/offshoreleaks-data-packages/raw-data/full-oldb.LATEST.zip` (the `.LATEST` directory beside it is empty).
- July spike's "mmap reopen ~1370× cheaper" → 2,300× on the real corpus (25.7 s vs 11 ms). Same direction, bigger corpus.

## 6. Open questions for the user

- **Should a `consumer-drive.sh` SKIP fail?** Today it exits 0 with a loud stderr line. A gate that skipped for months is the thing `CLAUDE.md` § Red-first warns about.
- **Merge coi-screen #2?** It is the user's private repo; this session pushed on request but did not merge. Milestone-1-proper steps 3–5 need it in.
- **The first two budget grants**: the coord session wants the exact command text to close its investigation. Only the user's terminal has it.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## Peer session

This session was asked to "collaborate with the other agent". The peer was `graphdb-coord-89` (the graphdb-coord repo). Protocol that held: it owns coord writes other than my own claim/release; I message it before any seed write and it answers whether anything is in flight; it asks me to report the node id after. It was wrong once (named `../COI` as the consumer) and corrected its memory when shown the evidence. Its standing request: message it only for a coord client defect or before another write to :8090.

## 8. How to use this handoff

1. Read this first.
2. Check #594 / #595: merge #594, run `coord release --pr 594 graphdb:v1.1-coi-screen-real-corpus`, merge #595, then `git checkout main && git pull --ff-only`.
3. Then `docs/NEXT_STEPS_2026-06-18.md` §D (post-#595) and `COI_SCREEN_REAL_CORPUS_2026-09-13.md` § Findings 1 before claiming `v1.4-close-no-rewrite-when-clean`.
