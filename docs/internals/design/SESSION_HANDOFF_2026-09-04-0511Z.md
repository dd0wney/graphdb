# Session handoff — 2026-09-04 05:11 UTC

**Date**: 2026-09-04 (single session, 10 PRs merged: the 7 queued by the 08-31 session plus 3 new)
**Outgoing model**: Claude Fable 5.1
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The Go 1.27 floor (#555) and the six PRs queued behind it are on `main`. Three follow-ons landed
on top: the four `govulncheck` dependency advisories are closed (#556), `pkg/backup` reads and
writes through `pkg/vfs` (#558), and the V1-spike design document exists (#557). The design review
found **four pre-existing correctness defects** in `pkg/query` and `pkg/api` (§5.1) that need a
decision before the V1 implementation is worth starting.

## 2. What is done this session

| PR | Title | Notes |
|---|---|---|
| #549 | docs(query): say what a zero MaxResults means | Queued from 08-31. |
| #550 | docs(storage): record the H-3 toggle-hygiene crash-scan result | Queued from 08-31. |
| #551 | fix(query): bare-star variable-length path means one-or-more hops | Behaviour change on the REST surface, recorded in `CHANGELOG.md`. Read before merge, as instructed. |
| #552 | fix(mutation): guard the working tree around gremlins' mutations | Queued from 08-31. |
| #554 | chore(deps): gosec 2.28.0 → 2.29.0 | Dependabot. |
| #555 | chore(go): raise the toolchain floor to 1.27.0 | All 11 checks green before merge. `main` CI on `f4dcfd4` green on all six workflows. |
| #553 | docs: session handoff — 2026-08-31 | macOS `TestClusteringCoefficient_Triangle` failed once on a docs-only diff, passed on rerun. Flaky, not caused by the PR. |
| #556 | chore(deps): close four govulncheck advisories | pgx v5.9.2, otel v1.44.0 (seven otel modules moved together), grpc v1.82.1, x/text v0.39.0. `govulncheck` 4 → 0 with the same invocation. Six transitive bumps from `go mod tidy`, not audited separately. |
| #557 | docs(query): V1-spike design | 725-line design document, ten decisions D1–D10, three alternatives rejected per capability. Main-loop architecture review added two notes (per-start-node scope of the visited set, and the `ExecuteWithText` path that bypasses the proposed entry point). |
| #558 | feat(backup): thread pkg/vfs through archive write and extract | Resumed the 08-31 branch. Golden data independently re-derived from pre-change code (10/10 members match), fixture extended with a `.tmp`-named directory and an empty directory, equivalence helper proven to report a difference. Two fix rounds removed comments that cited evidence outside the repository. |

## 3. Current state

- **`origin/main`**: `d6bcaae`.
- **Open PRs**: none at write time, except the PR that carries this file.
- **CI on `main`**: `f4dcfd4` (#555) green on all six workflows. `8be5497` (#556) and `d6bcaae`
  (#558) were still running when this was written. **Check them first.**
- **Local branches**: 12 stale, all merged: `chore/deps-govulncheck-2026-09`,
  `docs/h3-toggle-hygiene-crash-sweep`, `docs/spike-v1-deep-path-traversal-design`,
  `docs/v1-spike-deep-path-traversal`, `feat/backup-vfs`, `fix/bare-star-variable-length-path`,
  `fix/maxresults-honesty`, `fix/mutation-working-tree-guard`, and five `worktree-agent-*`
  branches. `main-prerebase-backup` is pre-existing.
- **Worktrees**: six under `.claude/worktrees/agent-*`, every one on a merged branch. The auto-mode
  permission classifier refused `git worktree remove` this session, so they remain. Use the
  `branch-cleanup` skill or remove them by hand.
- **Uncommitted changes**: none.
- **Lint and vulnerability state**: `golangci-lint` v2.13.2 reported 0 issues on every PR.
  `govulncheck ./pkg/... ./cmd/...` reports 0 on `8be5497` and later.
- **Coord daemon**: not reachable at session start. `coord-next` fell back to the handoff and
  the open PR list.

## 4. What is next

### 4.1 Four correctness defects found by the design review — decide first

All four are pre-existing, verified by reading the code on `f4dcfd4`, and all four are the class
this repository refuses: a limit or a filter that acts and does not tell the caller.

| id | Defect | Where | Recommended fix |
|---|---|---|---|
| F-a | A cancelled traversal returns partial rows with a **nil** error, so a timed-out query claims completeness. | `pkg/query/match_path.go:148-150` | Return the rows **with** a wrapped cancellation error, in the #546 shape. |
| F-b | `PROFILE` returns a nil error, and the first segment of a `WITH` chain drops `ErrTraversalTruncated`. | `pkg/query/executor_explain.go:111`, `pkg/query/executor.go:200` | Same PR as F-a: propagate the signal on both paths. |
| F-c | The REST handler maps `ErrTraversalTruncated` to **HTTP 500** and discards the rows. No file outside `pkg/query` references the sentinel. | `pkg/api/server_handlers.go:165-174` | Separate PR: `errors.Is` check, answer 200 with the rows plus a header, in the CC14 `X-Enumeration-Incomplete` shape. |
| F-d | Relationship-pattern properties are never read on the match path. `MATCH ()-[r:T {k: v}]->()` returns every `T` edge and ignores `{k: v}` silently. The only readers of `RelationshipPattern.Properties` are the parser, the parameter injector, and `CreateStep`. | `pkg/query/match_path.go` (no reader), `pkg/query/ast.go:54` | Separate PR: either honour the properties in `getEdges`, or refuse the pattern with a named error. Honouring is the Cypher-correct answer and is small. |

Each needs a red-first test and the failing output in the PR body. F-c changes the REST surface
and belongs in `CHANGELOG.md`.

### 4.2 V1-spike — decisions, then implementation

`docs/internals/design/SPIKE_V1_DEEP_PATH_TRAVERSAL_2026-09-04.md` asks ten decisions in §7
and recommends an answer for each. The ones that change the shape of the work: **D2** (refuse
`MinHops >= 2` in node-visited mode), **D3** (a new `ExecuteWithOptions` entry point), **D5**
(the filter receives the loaded node), and **D7/D8** (fix the truncation holes first, which are
F-a to F-c above). Size estimate in §8: about 190 LOC of production code and 450 LOC of tests.
No implementation is approved until the user answers.

### 4.3 Tell the `fault` session

The 08-31 handoff asked that the `github.com/dd0wney/fault` session be told when `pkg/backup`
moved onto `pkg/vfs`. #558 is merged. No session with that name was reachable from
`ListAgents` this session. The message has not been delivered.

### 4.4 Carried from the 08-31 handoff, unchanged

- `docs/CAPABILITIES_2026-05-10.md:175` contradicts `docs/STABILITY_POLICY.md:46` (library import
  "supported" versus "not a versioned API"). One-row fix.
- `pkg/wal/durable_rename_test.go` is blind to the rename it covers. #545 made the rename
  observable; the test was not strengthened.
- `TestMmapReaderRejectsACorruptSnapshotFromTheDriver` flips a byte at the fixture midpoint, and
  the midpoint lands in a CRC-covered region only by accident of fixture size.
- `computeCRC` covers directories and metadata only. Detection of record corruption is unmeasured.
- `CLAUDE.md` § "Known pitfalls" should gain: `gh pr merge` merges server-side, and a later
  `git fetch` moves `origin/main` while the local `main` ref stays behind.

### 4.5 Planning-doc items still open

From `docs/NEXT_STEPS_2026-06-18.md`: the mmap invariant gap (vector index and adjacency lists
have no mmap ground truth), and ADR 0001 (uniqueness-rules registry, design settled, not built).

## 5. Stale assumptions to retire

- **`docs/NEXT_STEPS_2026-06-18.md:89-93`**, the V1-spike item. It cites `match_path.go:94`,
  `:97-99`, `:158-163` and `:182-210`. On `d6bcaae` those are `:107`, `:111-112`, `:219-223`
  and `:249-277`. Its status line "Spike" should now point at the merged design document
  (#557) and say that no implementation is approved.
- **`docs/internals/design/NEXT_SESSION_PROMPT.md`** as written by the 08-31 handoff: all three
  of its directives are done (#549–#552 merged, `pkg/backup` merged as #558, V1-spike designed).
  This handoff overwrites it.
- **`SESSION_HANDOFF_2026-08-31-1010Z.md` §3** "zero commits and no PR" for `feat/backup-vfs`:
  the branch had one commit (`2ad4044`) and was already pushed to `origin` before this session
  started. It is now merged as #558.
- **`docs/internals/design/GATE0_GO127_RESULTS_2026-09-03.md`** "Four dependency advisories …
  follow-up: one `go get` PR": done, #556.
- **The design-spike brief's own premise** "no push, no PR" for `feat/backup-vfs` was wrong on
  the push half. Recorded so the next brief-writer checks `git rev-parse origin/<branch>` first.

## 6. Open questions for the user

1. **F-a to F-d (§4.1)**: fix now as three small red-first PRs, or park? The recommendation is
   fix now, because F-a and F-c make a security-shaped answer quietly incomplete, and F-d makes a
   pattern filter silently inert.
2. **V1-spike D1–D10**: accept the document's recommendations, or change any? D2, D3, D5 and
   D7/D8 shape the implementation.
3. **Cleanup**: 12 merged local branches and 6 agent worktrees. The classifier refused
   `git worktree remove` in auto mode. Approve `branch-cleanup`, or remove them by hand.
4. **`CAPABILITIES:175`** row correction: still not made (carried from 08-31).

## 7. Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## 8. How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-06-18.md`.
3. If picking up §4.1 or §4.2, read `docs/internals/design/SPIKE_V1_DEEP_PATH_TRAVERSAL_2026-09-04.md`
   §2 and §7 — every claim there carries a `file:line` from `f4dcfd4`, and `pkg/query` did not
   change after that commit this session.
