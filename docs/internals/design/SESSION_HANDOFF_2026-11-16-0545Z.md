# Session handoff — 2026-11-16 05:45 UTC

**Date**: 2026-11-16 (single session; 10 PRs merged, none open from this session)
**Outgoing model**: Claude Opus 5.5
**Delegation**:
- `worker` (sonnet) ran 5 builds: UNWIND part 1, the clause-order refusals, the shutdown fix (plus a 20-run data-loss probe and a second fix), `WITH … SKIP/LIMIT`, and pattern-map row expressions. The main loop read each source diff and added a commit to 3 of the 5 builds (typed-slice UNWIND, SKIP/LIMIT before RETURN, the negative-number literal), each found by reading the diff or a builder's open doubt.
- `reviewer` (opus) ran 9 reviews. 8 of them found a real defect or an incorrect CHANGELOG claim. Before it acted on a finding, the main loop confirmed each one: in the code, by a probe, or with the red test of the repair.
- The main loop did the omnigraph comparison, the #766 version fix, the arm64 Dockerfile fix, the rebases and merges of `main` into PR branches, the PR bodies, the merges and the coord tail. No haiku dispatch: each tail was short.

**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Two GitHub issues from a client were worked: #766 is closed, and #765 is done except for the form without `WITH`. The work behind them found two data-loss defects in Cypher (`WITH … SKIP/LIMIT` and `LIMIT` before a write both deleted every node). It also showed that the server never wrote a snapshot on SIGTERM. All three are fixed on `main`. **No release carries any of it yet.**

## Read this before any release work

| Defect | Releases | Effect | Fixed in |
|---|---|---|---|
| `MATCH (a) WITH a SKIP 1 DETACH DELETE a` ignored the SKIP | v0.4.0 to v1.12.1 | deleted every node, not all but one | #773 |
| `MATCH (p) LIMIT 1 DETACH DELETE p` ignored the LIMIT | v0.1.0 to v1.12.1 | deleted every node; now refused | #773 |
| SIGTERM or SIGINT closed storage twice and exited mid-snapshot | v1.0.0 to v1.12.1 | no `snapshot.mmap` in 10 of 10 runs; WAL never truncated, grows on every restart; no acknowledged write lost (WAL replay, 5 of 5 nodes in every run) | #772 |
| `/health` reported `1.0.0`; update status `dev`; Docker images an empty version | all | clients could not detect the release | #768 |
| arm64 Docker image held amd64 binaries (not run; shown from the `Dockerfile`) | all multi-arch images | probably `exec format error` on arm64 | #771 |

The CHANGELOG `[Unreleased]` section names each one, with the release range and what a user must check.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #764 | seed two test tasks from the omnigraph review | From a review of ModernRelay/omnigraph: a seeded model test for `GraphStorage` with a crash contract, and a guard test for `faultsim` sites. Not started. |
| #767 | seed tasks for #765 and #766 | |
| #768 | report the build version in /health and the update status | New `pkg/buildinfo`. The review found that goreleaser's `1.12.1` (no `v`) made `updater.IsNewer` always false, so the release server never saw an update. The second commit adds the `v`. |
| #769 | UNWIND over a list parameter or literal | Three review passes. Allowing `UNWIND $xs` exposed the fixed plan order, so MATCH, OPTIONAL MATCH and MERGE after UNWIND, and UNWIND after a write, are now refused (**breaking**). The PR body closed #765 by mistake ("does not close #765" matches the keyword); the issue was reopened with a comment. |
| #770 | seed five defects | arm64, shutdown double close, three clause-order defects. All five are done. |
| #771 | compile for the target arch in the arm64 image | `FROM --platform=$BUILDPLATFORM`, `GOARCH=${TARGETARCH}`. No image was built (no local base images, and a download needs approval). The first `docker-publish.yml` run after merge is the first real build. |
| #773 | apply SKIP and LIMIT after WITH; refuse out-of-order clauses | 4 coord tasks. **Breaking**: OPTIONAL MATCH after a write, writes against plan order, MERGE after a write hidden by an earlier MERGE, a clause after RETURN, SKIP/LIMIT not after RETURN, WITH with SKIP/LIMIT and WHERE. |
| #774 | seed eight Cypher follow-ups | See "What's next". |
| #772 | write the snapshot on SIGTERM by closing storage once | `serve()` in `cmd/server/shutdown.go` owns shutdown. The review found an early-SIGTERM defect (a signal before `Start` stored the server was lost, and later SIGTERMs were ignored); `api.Server` now records the request. |
| #775 | per-row expressions in pattern property maps | The batch upsert for #765: `UNWIND $items AS item WITH item MERGE (n:L {id: item.id}) SET …`. Also closes `v1.4-cypher-negative-literal-in-map`. A row value uses no property index (a coord task). |

## Current state

- `origin/main` = `050499b`. Local `main` equals it.
- Open PRs, none from this session: #735 and #734 (encryption seed and spec), #711 (dependabot). The previous handoff merged as #762, before this one, on the user's word.
- Local branches: `main`, plus the branches of #735 and #734, plus `feat/encryption-key-check`, which is checked out in the worktree `.claude/worktrees/keycheck` and holds local work. The session deleted 24 merged branches and 7 scratch worktrees, with the user's approval.
- Uncommitted changes: none.
- Coord: every claim of this session is released. GitHub #766 is closed. **#765 is open on purpose**: the form without `WITH item` waits for `v1.4-cypher-merge-after-unwind-auto-split`.
- Gates: each merged PR passed all 10 CI checks. **Local golangci-lint does not run at the moment** (see "Stale assumptions").

## What's next

Ranked. The planning doc is still `docs/NEXT_STEPS_2026-10-08.md`; this session did not update it.

1. **Release decision (user).** `main` carries the five fixes in the table above and two breaking parser changes. The previous handoff already recommended v1.13.0 from `main`.
2. **Small Cypher correctness tasks from #774**, each red-first:
   - `v1.4-cypher-return-limit-zero`: `RETURN … LIMIT 0` keeps every row.
   - `v1.4-cypher-repeated-skip-limit`: `SKIP 1 SKIP 2` keeps the last value with no error.
   - `v1.4-cypher-merge-null-property`: MERGE with a null literal or null parameter is not refused.
3. **`v1.4-storage-seeded-model-test`** (#764): the largest durability gap that the omnigraph comparison found. Then `v1.4-faultsim-site-guard` (about one day).
4. **`v1.4-cypher-merge-after-unwind-auto-split`**: the last part of #765. It is a design task: an implicit WITH that carries every bound variable. Done well, it could replace the clause-order refusals of #769 and #773.
5. The rest of #774: `v1.4-cypher-with-distinct`, `v1.4-cypher-skip-limit-parameter`, `v1.4-cypher-pattern-map-same-pattern-variable`, `v1.4-cypher-pattern-map-row-value-index`.
6. From the previous handoff (`SESSION_HANDOFF_2026-11-14-0653Z.md`, merged as #762): the 37 TCK `fail` rows and the probes for the two defects that v1.12.1 leaves.

### New gaps not yet on the planning doc

- Cypher writes are not transactional. An error in a later row keeps the writes of earlier rows (`v1.4-cypher-no-rollback-after-refusal` exists).
- The physical planner (`pkg/query/planner.go`) has no production caller, but it still holds UNWIND and pattern code that each change must keep in step. A decision to delete it would remove that cost.

## Stale assumptions to retire

- **`CLAUDE.md` § "Build, test, lint at CI's surface"** says that a golangci-lint built with Go 1.27 or later "typechecks the whole module, so all eleven CI linters run locally". At 14:53 AEDT (03:53 UTC) on 2026-11-16, something outside the session replaced `/usr/local/go` with go1.27.4 (ctime of `/usr/local/go/VERSION`). After that, `golangci-lint@v2.13.2` failed with `could not load export data … export data version 5 is greater than maximum supported version 4`, and `staticcheck` (built with go1.25.3) refused the module. The module cache holds no Go 1.27 toolchain, so `GOTOOLCHAIN=go1.27.0` needs a download. Until that is resolved, CI is the only lint gate. The paragraph should say which local Go the pinned linter can read.
- **`CLAUDE.md` § "Known pitfalls"** has no line for the closing keyword: "does not close #N" in a PR body still closes issue N. The auto-memory now has `github-closing-keyword-ignores-negation`. This repository should have the line too.
- **`CLAUDE.md` § "Snapshot format stability"** describes when the snapshot is written, but it does not say that a signal-stopped `cmd/server` never wrote one before #772. Any doc or benchmark that measured "reopen from a snapshot" after a SIGTERM stop on v1.12.1 or earlier measured a WAL replay.
- **The previous handoff (#762)** lists "branch cleanup: about 18 local branches". That is done (24 deleted).

## Open questions for the user

1. **Resolved:** the user merged #762 first, then this handoff. `NEXT_SESSION_PROMPT.md` is this handoff's version.
2. **v1.13.0 from `main`?** The data-loss and snapshot fixes argue for it. It is breaking for Cypher queries in the refused classes, so it is a minor version, not a patch.
3. **Local lint:** approve a download of a Go 1.27.0 toolchain or a newer golangci-lint, or accept CI as the only lint gate?
4. **`WITH … SKIP/LIMIT … WHERE` is refused** because the openCypher order of WHERE and SKIP/LIMIT is not confirmed from a local source. The full TCK (a download) would confirm it. Approve that download, or keep the refusal?
5. **Option B for #765** (an automatic split of the query part) is a larger parser change. Should it be the next design task, or should #765 close with the `WITH item` form?

## Next-session prompt (paste-ready)

```text
Read docs/internals/design/SESSION_HANDOFF_2026-11-16-0545Z.md first.
Resolve its open questions 2 and 3 with the user before other work (v1.13.0, local lint).
Then work the Cypher tasks in "What's next" item 2, red-first, one PR each:
v1.4-cypher-return-limit-zero, v1.4-cypher-repeated-skip-limit, v1.4-cypher-merge-null-property.
Claim each with `coord claim graphdb:<id>`; release with `--pr N` after merge.
Run `pkg/queryutil` in every Cypher gate: it holds the TCK baseline and the known-gap table.
Local golangci-lint may not run (go1.27.4); if it fails with "export data version 5", CI is the lint gate. Do not download a toolchain without approval.
Do not write "close", "fix" or "resolve" directly before an issue number in a PR body unless the PR closes it.
End with the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then read `docs/NEXT_STEPS_2026-10-08.md` and `SESSION_HANDOFF_2026-11-14-0653Z.md`.
3. For any Cypher work, read the `[Unreleased]` section of `CHANGELOG.md`: it lists every clause order that the parser now refuses, and why.
