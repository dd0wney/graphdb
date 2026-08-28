# Session handoff — 2026-08-28 05:40 UTC

**Date**: 2026-08-28 (second handoff of the day; supersedes `SESSION_HANDOFF_2026-08-28-0219Z.md`)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

22 PRs merged. The session worked through SQLite's testing regime and then
past it: the fault-injection seams were rebuilt as production drivers so tests
exercise the shipped path, and a coupling analysis showed that **9 of the 13
defects found were interface defects**, not within-component ones — an axis the
SQLite scorecard does not measure.

## What's done

`SESSION_HANDOFF_2026-08-28-0219Z.md` covers #465–#469. Since then:

| PR | What | Notes |
|---|---|---|
| #470 | `:Claim` uniqueness by label containment | Closed a silent atomicity bypass; storage was always correct, two callers had narrowed it |
| #471, #476, #482 | Doc reconciliation | CLAUDE.md said mmap was off by default; planning decision B-2 was already shipped |
| #473 | First handoff | Amended before merge when the session continued past it |
| #474 | mmap invariant checker | The default storage path had none |
| #475 | ADR 0001 — uniqueness registry | `proposed`; design settled across two sessions |
| #477 | Two panics on a malformed mmap snapshot | Fuzzing; one found on the **seed corpus** |
| #478 | Zero-fill forges valid WAL entries, resetting the LSN | Hole-in-the-middle crash test |
| #479 | `pkg/vfs` testability drivers + ADR 0002 | Fault injection on the production path |
| #480 | cgo-free CI gate | Replaced a `-tags nng` job that compiled an identical file set |
| #481 | N-sweep + `pkg/faultsim` | Found a wrong invariant in its own test at N=3 |
| #483 | Crash sweep | Cut at every operation, repeats per point |
| #484 | OOM double loop | Found LSN reuse after an allocation failure at open — silent data loss |

## Current state

- **`origin/main`**: `f9c9654`
- **Open PRs**:
  - **#485** `feat/vfs-btree-lsm` — migrates `pkg/btree` and `pkg/lsm` to the driver. A lint fix is pushed; re-running. Found two defects (a broken error chain, a nil driver in the SSTable constructors).
  - **#486** `docs/coupling-interference` — the coupling analysis. Docs only.
- **Branches**: the two above, plus `main-prerebase-backup` (pre-existing, not this session's).
- **Uncommitted**: none.
- **Local gotcha, costly if forgotten**: `golangci-lint` on this machine fails typecheck on a Go 1.26 stdlib file, and that failure **suppresses the analysers**, so it reports a clean-looking environment error and nothing about the code. It missed real findings on #479 and #485. **CI's lint is the only working linter.**

## What's next

Ranked, from `SQLITE_TESTING_SCORECARD.md` and `COUPLING_AND_INTERFERENCE.md`:

1. **Concurrent fault injection.** Every fault, crash and sweep test built today
   is single-threaded — measured, zero goroutines across six files — while
   `pkg/storage`'s 28 concurrent test files inject no faults. The two halves
   have never met, and `-race` does not close it. This is the highest residual
   risk and the graphdb analogue of A(M)C 20-193's MCP_Software_2.
2. **`pkg/storage` driver migration** (ADR 0002 stage 4, 179 sites). Its
   `syscall.Mmap` path does not fit the `File` interface and needs separate
   treatment.
3. **Per-package coverage floors.** The global 74% floor lets `pkg/query` at
   62.9% hide behind `pkg/parallel` at 93.4%. orca's `tools/covcheck.py` has
   the design, including a self-test.
4. **A DCCC coverage measure** over the interfaces enumerated in
   `COUPLING_AND_INTERFERENCE.md`.
5. **The uniqueness-rules registry** — ADR 0001, `proposed`, not started.

## Stale assumptions to retire

1. **`SESSION_HANDOFF_2026-08-28-0219Z.md` is superseded.** It says main is
   `79a6f31` and lists the mmap invariant checker as the next track; that
   shipped in #474.
2. **Serena is gone** (#465). The `/handoff` slash command still instructs
   "save to Serena memory" — that path no longer exists; this file is the
   handoff.
3. **"The coverage number is ~79%"** — that is a developer-machine number. CI
   measures 75.5%, reproduced twice. The floor lives in `Makefile:COVERAGE_MIN`
   and must be read from CI.
4. **"The CI-versus-laptop coverage gap is core count"** — **falsified**.
   `GOMAXPROCS=4 -p 2` on this machine gave 80.0%, identical to unconstrained,
   with the constraint verified to apply. Still unexplained.
5. **`CheckInvariants` refuses mmap stores** — false since #474; it inspects
   them, and `ErrInvariantsUnsupported` now means one thing only, a snapshot
   with no membership directory.

## Open questions for the user

1. **Is concurrent fault injection worth a track?** It is where the residual
   risk sits, and it is the largest remaining piece.
2. **The `:Claim` registry (ADR 0001) is `proposed`, not `accepted`.**
   graphdb-coord is holding its client half pending that decision.
3. **The `syscall.Mmap` gap needs an out-of-process approach** — FUSE or
   `dm-flakey`. This is the one place C and Bazel are the right tools, as a
   separate binary rather than a cgo dependency.

## Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## How to use this handoff

1. Read this, then `SQLITE_TESTING_SCORECARD.md` (16 rows, state and evidence)
   and `COUPLING_AND_INTERFERENCE.md` (why 9 of 13 defects were interface
   defects).
2. Then `CLAUDE.md` § "Orient first".
3. Check the nightly fuzz run: `gh run list --workflow Fuzz`. It ran
   successfully for the first time on 2026-08-28.
4. Do not trust the local `golangci-lint`. See Current state.
