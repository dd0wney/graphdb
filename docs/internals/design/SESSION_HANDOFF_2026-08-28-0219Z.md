# Session handoff — 2026-08-28 02:19 UTC

**Date**: 2026-08-28 (single session; 8 PRs merged, 7 of them authored in-session)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Amended**: 02:33 UTC, before merge. The session continued after the first draft — #474 (the mmap invariant checker, which the draft listed as the recommended next track) is now open, and #457 was rebased. Sections 4, 5, 6 and 8 reflect the later state. Recorded here rather than rewritten silently, because a handoff that quietly changes under a reader is worse than one that is out of date.

## TL;DR

The session compared graphdb against SQLite's 15-technique test regime
(sqlite.org/testing.html) and shipped the four highest-yield gaps. Three of the
four found a real defect on their first run: a file-descriptor leak in
`WAL.Close`, two metamorphic tests that had been asserting nothing, and a
coverage floor set from the wrong machine. A fifth PR closed a silent atomicity
bypass in `:Claim` uniqueness.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #465 | chore: remove Serena MCP references | Plugin was already disabled at user level; removed the repo-side wiring and a 7.5 MB untracked cache dir. |
| #466 | test(wal): I/O fault-injection seam + fix a descriptor leak it found | **Found a real defect on first run.** `WAL.Close` returned the first flush/fsync error and never closed the file, so each disk fault leaked one descriptor and the process later died on EMFILE far from the cause. `walFile` interface + `faultyFile` harness (SQLite's fail-once / fail-always / fail-at-N modes). |
| #467 | ci: run the 14 fuzz targets as fuzzers, nightly | The targets had **never been fuzzed** — `go test` only replays a target's seed corpus. Guard worth knowing: `go test -fuzz` exits **0** when the regex matches no target, so the run step greps for "no fuzz tests to fuzz" or a renamed target would leave the job green while fuzzing nothing. **First real run is 14:17 UTC daily — it has not executed yet.** |
| #468 | refactor(storage): promote the invariant checker out of the test binary | `CheckInvariants` is now ordinary code and **refuses** mmap-backed stores with `ErrInvariantsUnsupported`. **Found that `TestMetamorphic_NoDelete` / `_WithDelete` were asserting nothing**: their WAL driver recovered via `NewGraphStorage` (mmap default) while the rest used `crashRecoveryConfig` (JSON). Measured on a 5-node store: mmap reopen → shard count 0, JSON reopen → 5. Empty ground truth vs empty derived indexes reported "healthy". |
| #469 | ci: gate on the coverage number, and widen it to pkg/graphql | The number was decorative (`fail_ci_if_error: false`, nothing read it). **The gate failed on its own PR** because the floor came from a developer machine. See §6 — the machine-dependence is unexplained and is a live finding, not a footnote. |
| #470 | fix(claim): enforce :Claim uniqueness by label containment | Silent atomicity bypass: the rule fired only for `labels == ["Claim"]` exactly, so an agent could take a held task by passing `["Claim","Urgent"]` with **no error to either side**. Storage was always correct (`containsString` + check-and-insert under one `gs.mu.Lock`); the two callers had narrowed it. GraphQL + REST changed together. **One existing subtest was reversed, not deleted** — it pinned the bypass on purpose. |
| #471 | docs: reconcile two stale claims with the code | CLAUDE.md said mmap is "off by default"; #447 made it the default in v1.2. Planning decision **B-2 was already answered and shipped** but still listed as open. |
| #461 | fix(query): coerce numeric types in WHERE = / != | **Authored before this session** (opened 2026-07-20); merged here after confirming green and no overlap with in-session changes. |

## Current state

- **`origin/main`**: `79a6f31`
- **Open PRs**:
  - **#474** `feat(storage): invariant checker for the mmap representation` — **the recommended track in §5, now built and in review.** Opened after this handoff's first draft. See §5 for what it does and does not cover.
  - **#473** this handoff.
  - **#457** `fix(api): /query value decoding, batch per-item errors, ADMIN_PASSWORD warning` — **rebased**: `origin/main` was merged into its branch (not a rebase+force-push, so the PR history is intact and the squash-merge collapses the merge commit). Verified after merging rather than trusting a clean merge: #470's containment fix survives at `handlers_nodes.go:138`, the full `pkg/api` suite passes, claim-uniqueness tests pass. CI re-running against today's base.
  - **#472** dependabot actions bump — untouched by this session.
- **Open branches**: `feat/mmap-invariant-checker` (#474), `fix/api-query-batch-restore` (#457), `docs/session-handoff-2026-08-28-0219Z` (#473), `main-prerebase-backup` (pre-existing, not this session's).
- **Worktree note**: `fix/api-query-batch-restore` was registered to a worktree at `/data/Workspace/github.com/graphdb-fix-api` whose directory no longer exists, so `git switch` refused the branch. `git worktree prune` cleared it. If another branch refuses to check out for the same reason, that is the cause.
- **Uncommitted changes**: none.
- **Test/lint state on main**: `go build ./pkg/... ./cmd/...` clean; `go vet` clean; `go test ./pkg/storage/` and `-race` clean; coverage gate clears (79.8% local / 75.5% CI vs a 74.0 floor).
- **Local-only gotcha**: `golangci-lint` on this machine panics on `pkg/graphql` and `pkg/api` with "file requires newer Go version go1.27 (application built with go1.26)", and reports a stdlib `splice_linux.go` typecheck error on every package. Both reproduce on `origin/main` without any local change. **CI's Lint job is the real gate and is green.** Don't chase these locally.

## What's next

Ranked. The planning doc (`NEXT_STEPS_2026-06-18.md`) now carries these in its
"Follow-ups opened 2026-08-28" section.

1. **Rebase and land #457** — cheap, and it overlaps a file that moved today.
2. ~~**An invariant checker for the mmap representation.**~~ **BUILT — in review as #474.** Ground truth is `shard records ∪ (mmap base records − tombstones)`, shard winning, built from raw records rather than the membership helpers (those fuse base and overlay and are the thing under test; comparing them against themselves would pass unconditionally). **What remains uncovered and is the real next item: the vector index and the adjacency lists have no mmap ground truth.** A clean result on the mmap path is therefore a weaker statement than a clean result on the JSON path, and `CheckInvariants`' doc comment says so. Closing that is the follow-on track.
3. **Explain the coverage machine-dependence** (see §6). Cheap to investigate, and if the hypothesis holds it means some statements are only ever tested on fast hardware.
4. **The uniqueness-rules registry** (`pkg/graphql/mutations_resolvers.go:25`, existing TODO). #470 made the `:Claim` rule correct but kept the label hardcoded. The registry is what removes coord-domain knowledge from graphdb. User's directional steer, 2026-08-28: **ACID over eventual consistency** for this class of constraint.
5. **Off-path, from the same comparison, not started**: I/O fault injection for `pkg/lsm` and `pkg/btree` (no filesystem seam — `pkg/storage` calls the OS directly at ~171 sites); crash/power-loss simulation with torn writes and write reordering; `pkg/api` coverage (excluded from the number for the runner reason `test-cover` documents, so the HTTP surface is outside it).
6. **Unchanged from the prior checkpoint**: decision **B-1** (is full-graph enumeration-on-reopen a consumer hot path?) is still open and still gates DoD Levers 2–3. The real-corpus coi-screen track remains the planning doc's recommendation for it.

## Stale assumptions to retire

Most were corrected in #471, but these are worth carrying forward:

1. **`CLAUDE.md` § snapshot format, "Off by default"** → corrected in #471 to "On by default since v1.2 (#447)". If any memory or doc still says mmap is opt-in, it is wrong.
2. **`CLAUDE.md` § snapshot format, "`checkGraphInvariants` does NOT work in mmap mode"** → the function is now `CheckInvariants`, it lives in `pkg/storage/invariants.go` (not a `_test.go`), and it **refuses** rather than silently passing. Corrected in #471.
3. **`NEXT_STEPS_2026-06-18.md` decision B-2 ("should mmap mode become a default?")** → answered and shipped in #447. Marked resolved in #471. The "Recommended next track" section still lists "explicit precondition for B-2" as one of three reasons to pick that track; **that reason is spent**, the other two (B-1 evidence, real-consumer validation) still stand.
4. **"The coverage number is ~79%"** → that is a developer-machine number. **CI measures 75.5%**, reproduced exactly across two runs. The floor lives in `Makefile:COVERAGE_MIN` and must be set from CI. Anyone raising the ratchet should read the CI number, not run `make test-cover` locally.
5. **"`CheckInvariants` refuses mmap-backed stores"** → true on `main` today, false the moment **#474** merges. After that it inspects them, and `ErrInvariantsUnsupported` narrows to one case: a snapshot with no membership directory. `CLAUDE.md` § snapshot format was updated by #471 to describe the refusal and **will need a second edit when #474 lands** — it is the third correction to that same paragraph today, which is itself a signal that the paragraph tracks fast-moving code.
6. **"`:Claim` uniqueness is advisory / a second label opts out"** → false since #470 on both REST and GraphQL. graphdb-coord's `docs/COORD_DEPLOY_SPIKE_2026-05-10.md:213` still specifies the exact-match rule as the intended design; that session was told and is correcting it.

## Open questions for the user

1. **Does the coverage machine-dependence matter enough to chase?** The evidence: CI 75.5% vs local 79.5%, reproduced; every package with timing or concurrency is lower on the runner, while `wal/apply` (neither) is 92.9% on both. Unverified: local runs show zero skips in `wal` and `graphql`, and skips were **not** measured on CI. If the hypothesis holds, the fix is not "raise coverage" but "make those paths deterministic".
2. **Should the mmap invariant checker be a track now, or does B-1 come first?** They compete for the same storage-area attention.
3. **`pkg/api` is outside the coverage number.** Leaving it out keeps the ubuntu runner load down; including it puts the HTTP surface under the gate. Not resolved.

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` — same content, standalone.

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-06-18.md`, especially "Follow-ups opened 2026-08-28".
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If picking up the mmap invariant checker: read `pkg/storage/invariants.go` (the refusal and why), `pkg/storage/mmap_reopen_test.go` (`fingerprintTenant` / `assertFingerprintEqual` — the only gate that path has), and `pkg/storage/mmap_snapshot_loader.go` (the membership section that mmap-aware ground truth would be built from).
5. **Check the nightly fuzz run first** (`gh run list --workflow Fuzz`). Its first execution is 14:17 UTC 2026-08-28; nobody has seen it run.
