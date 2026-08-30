# Session handoff — 2026-08-30 03:08 UTC

**Date**: 2026-08-30 (single session, 20 PRs merged, 2 open at close)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The absent-versus-refused arc is **complete end to end** — a damaged record is now
distinguishable from a missing one at every layer from `pkg/storage` to the REST
boundary, including the seven enumeration methods that ADR 0003 covers. Ten
findings surfaced that the plan did not contain; seven are fixed and three are
recorded. A durability defect nobody here had noticed — graphdb never fsynced a
parent directory after a publishing rename — was predicted by a peer session from
theory, confirmed by grep, and fixed.

## What is done this session

Twenty pull requests, oldest first. #510 predates this session.

| PR | Title | Notes |
|---|---|---|
| #514 | `docs(design)`: what PostgreSQL's cluster harness has that graphdb does not | Carried over, independent |
| #515 | `docs`: session handoff — 2026-08-29 12:03 UTC | Carried over |
| #511 | `refactor(storage)`: a reason code for the mmap read path (PR A of 3) | Stack bottom |
| #512 | `fix(storage)`: a damaged record stops reading as a missing one (PR B of 3) | Rebased onto main; **8 commits, not the 7 the prior handoff claimed** |
| #516 | `fix(ci)`: a pull request gets tests and lint whatever its base is | **The highest-leverage item.** See §6 |
| #517 | `fix(wal)`: a device error during recovery is not a torn tail | Agent **inverted the default**: data errors are a closed list, unknown = resource. Verified safe — corruption is caught by `break` inside `ReadAll` and never reaches the classifier |
| #518 | `fix(vfs)`: correct two vfstest SweepRole defects | Trace-shortening check + `opConstName` table test |
| #519 | `fix(dccc)`: refuse an ambiguous coupling symbol instead of picking one | Row C4 was measuring `BTreeGraphStorage`, not `GraphStorage`. Floor 14 → 20 |
| #513 | `test(api)`: a cross-principal equivalence sweep (PR C of 3) | Stack top. Doc-only conflicts; CC11/CC12 did not collide |
| #521 | `fix(storage)`: the snapshot write refuses a damaged node record | **The only finding that destroyed data.** Scoped to one file by mirroring the edge branch rather than changing `forEachNodeUnlocked` (10 call sites, 7 files) |
| #522 | `fix(storage)`: unique-property scan refuses on an unreadable record | Backs CC10 and graphdb-coord's atomic claim |
| #523 | `test(storage)`: prove release() balance across the mmap reader's 8 error paths | 8/8 paths reached; production file untouched |
| #524 | `chore`: ignore agent worktrees under `.claude/` | Found while running six parallel agents |
| #525 | `docs(adr)`: propose an error return for the seven enumeration methods | ADR 0003, approved by the user this session |
| #526 | `fix(api)`: GET reports an unreadable record to the tenant that owns it | 500 to the owner, 404 to a stranger. Agent edited the sweep test **against instruction** and was right to — see §6 |
| #527 | `fix(server)`: report a shutdown that could not write its snapshot | `cmd/server` discarded the error #521 exists to produce, and exited 0 |
| #528 | `fix(storage)`: a Close that refuses to snapshot still releases what it holds | Mapping + WAL handle leaked with no retry, because `closed` was already set |
| #529 | `docs(dccc)`: re-measure the coupling baseline, and record what it cannot tell you | 595/640 = 93.0%. Second commit records what a stable number cannot tell you |
| #530 | `fix(storage,wal)`: sync the parent directory after a publishing rename | Predicted by a peer session; ten rename sites, zero directory syncs |
| #531 | `feat(storage)!`: an error return for the seven enumeration methods (ADR 0003) | **Breaking.** 35 production + 99 test sites, atomic. Paginated lists serve the partial page with `X-Enumeration-Incomplete` |

**Open at close**: #532 (this handoff) and #533 (`feat(vfs)`: the `extfs` adapter — see §5.4).

**All nine findings from the 2026-08-29 handoff's §5.1 are now closed** — #521, #517,
#519, #522, #526, #518 (two of them), #523, and the `ste-check` rule 3.4 repair
(user-level, no PR).

## Current state

- **`origin/main`**: `62e53c0`
- **Open PRs**: #532 (this handoff) and #533 (`extfs` adapter, CI green-so-far, zero failures)
- **Open branches**: `main`, `main-prerebase-backup` (pre-existing), plus the two branches behind #532 and #533
- **Uncommitted**: none
- **Worktrees**: one (the primary checkout)
- **Gates on `62e53c0`**: `go build` 0, `go vet` 0, `gofmt -s -l` empty,
  `contract-guard` 14 contracts / 21 guarding tests
- **Known-red, pre-existing, NOT this session's**: `go test ./pkg/... -short` exits 1
  on `pkg/licensing`. See F10 in §5.1 — it is a real data race and it predates this
  work. Do not treat a green `pkg/...` run as the baseline until it is fixed.

## What is next

Nothing is forced. The planning doc's queue is unchanged except where this session
closed items; the live candidates are the findings below plus ADR 0003's step 5.

### 5.1 Findings recorded this session, still open

| # | Where | What |
|---|---|---|
| **F5** | `pkg/graphql` (5 sites) | No `sanitizeError` equivalent. Resolvers return the raw storage error, which now carries a snapshot byte offset. `pkg/api` deliberately withholds it. Not a cross-tenant leak — the storage guard holds — but the two surfaces disagree on the same rule |
| **F6** | `pkg/api` PUT/DELETE/POST | Already answer 500 for a damaged record, but the body says `update node failed` and does not name it. The equivalence sweep cannot see this, because 500-vs-404 already differs |
| **F9** | `pkg/storage/persistence.go:162`, `vfs_helpers.go:20` | The JSON snapshot publish syncs **neither** the temp file nor the directory. `writeFileWithFS`'s comment justifies this with "the durability of the rename is what matters" — wrong on both halves. #530 fixed only the mmap path, so the two are now asymmetric. JSON is not the default since v1.2 but encrypted stores, `UseDiskBackedEdges` and `GRAPHDB_STORAGE_MODE=json` all use it |
| **F10** | `pkg/licensing` + `Makefile:75` | A genuine data race in `TestTelemetryReporter_Start_Success` (`callCount` unsynchronised). Invisible because `RACE_PKGS` covers 8 of ~42 packages. **Two separate fixes**: the race, and the gate's scope. Widening the scope will likely surface more races and needs its own budget |

### 5.2 Carried out of #531

- **ADR 0003 step 5, the fault sweep over the seven methods, is not written.** It is
  additive. Without it the change bought a signature and not a gate — the whole
  argument for the ADR was that no sweep could reach those paths.
- **`GetEdgesByTypeForTenant` keeps the silent skip.** Same defect, not one of the
  seven, deliberately left rather than widening the change.
- **`CountNodesForTenant` / `CountEdgesForTenant` have no error return.** On
  `BTreeGraphStorage` they are now documented as a lower bound.

### 5.3 A registry completeness check for DCCC

`couplings.tsv` is an **input**, so the measure cannot report a coupling site nobody
declared, and cannot report that one is missing. Recorded in `DCCC_BASELINE.md`.
Not built. Until it exists, 93.0% should not be quoted outside the project, because
the denominator is the claim and only the ratio is measured.

### 5.4 `pkg/vfs/extfs` and the crash-simulation collaboration (#533)

A peer session (`github.com/dd0wney/fault`) is building crash and power-loss
simulation. graphdb is its candidate first consumer, approved by the user
conditionally: *"only when the library is demonstrably awesome"* and *"it must
solve more problems than it creates"*.

#533 adds `pkg/vfs/extfs`, which adapts a filesystem graphdb does not own onto
`pkg/vfs.FileSystem`. The interface is DECLARED there rather than imported, so
graphdb gains no dependency. `Seek`, `ReadAt` and `WriteAt` refuse with a named
error rather than approximating, because a recorder that tracks offsets by
addition would place a header backpatch at the end of a file and every state it
generated would be fiction.

**Three measured facts it produced, each now pinned by a test:**

    pkg/storage  1 positional call   mmap_snapshot_writer.go:184  WriteAt(hdr, 0)
    pkg/wal      2 seeks             wal.go:138, wal.go:217
    pkg/lsm     19 positional calls

So the mmap publish needs `WriteAt` and the WAL paths need `Seek` before an
external non-positional filesystem can drive them. **The JSON publish path needs
neither** and runs end to end today — that is the one path a crash sweep can
reach with no change on either side, and it is pinned by
`TestJSONPublishIsExpressibleOnANonPositionalFilesystem`.

**Open, and the user's to answer**: a ~15-line wrapper binding a specific
library's `FS` to `extfs.FS` must import both packages, because Go's structural
typing is **not covariant in interface-valued returns** (verified by compiling
the minimal case). As a *test-only* import of a zero-dependency library it is a
small question, but it is a dependency question and graphdb is open-core.

Nothing in `extfs` depends on that answer.

## Stale assumptions to retire

1. **`docs/NEXT_STEPS_2026-06-18.md`, item 4 of the 2026-08-28 follow-ups**: "the
   enumeration half ... This is PR C, not started". **Shipped as #531.** All seven
   methods now return an error.
2. **Same item**: "26 production call sites move across five packages". **Wrong.**
   Measured on `ef77e04`: **35 production sites across six packages** (graphql 14,
   api 8, query 6, storage 5, search 1, algorithms 1), plus **99 test sites**.
3. **Same item**: "`GetEdgesByTypeForTenant`" is not in the seven and still skips
   silently. Do not assume the enumeration half is wholly closed.
4. **`docs/NEXT_STEPS_2026-06-18.md:169`**: "DCCC baseline 563/616 → 576/628 over 17
   sites". Accurate as history of #507, but the **live** baseline is now
   **595/640 = 93.0%** (`DCCC_BASELINE.md`, re-measured on `ef77e04`).
5. **`docs/adr/0003-...md` as originally merged in #525** claimed a five-step
   independent migration, and that `pkg/algorithms` takes `storage.Storage` in
   "seven functions" with one struct field. All three were wrong and are corrected
   in #531's second commit: the migration is **atomic** (compile-time assertions at
   `interface.go:156` and `btree_storage.go:665`), the real count is **32 non-test
   functions**, and there are **two** struct fields (`view.go:48` and `:84`).
6. **Any belief that `make test-race` covers the repository.** It covers eight
   packages. See F10.
7. **The user's auto-memory `mmap-json-mode-switch-loses-data`** is unchanged by this
   session and still needs the rewrite the 2026-08-29 handoff described.

## Open questions for the user

1. **A version call for #531.** It is `feat(storage)!` — a breaking change to a public
   interface with two implementations. Major or minor is unresolved. Not blocking
   anything until a release is cut.
2. **Whether graphdb becomes the first consumer of `github.com/dd0wney/fault`'s
   crash-simulation library.** The user said yes, conditionally: *"only when the
   library is demonstrably awesome"*, and later *"it must solve more problems than it
   creates"*. Eight acceptance criteria were relayed to that session. The blocking
   one on our side is now resolved — `pkg/vfs` **can** already fsync a directory, so
   no interface change is needed and adoption is one decision, not two.

## Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## How to use this handoff

1. Read this, then `docs/NEXT_STEPS_2026-06-18.md`, then `CLAUDE.md` § "Orient first".
2. **Before running `go test ./pkg/...`, read F10.** A red `pkg/licensing` is
   pre-existing and not yours.
3. If picking up F9, read `pkg/storage/vfs_helpers.go:15-30` first — the comment
   there explains a decision whose premise is false, and the fix is a behaviour
   change to every caller of that helper, not only the snapshot publish.
4. If picking up ADR 0003 step 5, read `pkg/vfs/vfstest/sweep_role.go` and
   `pkg/storage/mmap_release_balance_test.go` (#523) for the sweep pattern.

## A note on method, because it produced most of the findings

Seven of the ten findings came from checking an instrument rather than reading code:
a grep that printed nothing because it crashed, a test that passed while asserting
nothing, a mutation run that scored 1.00 by producing no mutants, a resolver that
measured the wrong function, a coverage number whose denominator is an input, a race
gate covering 8 of 42 packages, and a CI workflow that ran **zero** checks on a
stacked PR while showing a green tick.

The shape is always the same: **"examined nothing" and "found nothing wrong" render
identically.** Before trusting a gate, make it fail on purpose.

Two of the ten were the inverse — a recorded limitation that was never true. This
session claimed `pkg/vfs` could not fsync a directory, which was false and was about
to cost the user an interface-change approval they did not need. A one-minute probe
disproved it. Re-running a number catches drift; nothing re-runs a sentence.

A third shape appeared late, and it has the cheapest countermeasure of the three.
This session ran `grep -rnE '\.(Seek|ReadAt|WriteAt)\(' pkg/ | head -15`, concluded
`pkg/storage` had no positional calls, and told a collaborator so. `pkg/lsm` has 19
matches; they filled the window and `pkg/storage`'s single one fell off the bottom.
**`grep -c` before `grep | head`. If the count exceeds the window, the window is
lying to you.** The error was caught not by re-reading the grep but by building
`extfs` and watching a real store fail on the call the grep had hidden.
