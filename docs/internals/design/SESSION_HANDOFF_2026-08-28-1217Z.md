# Session handoff — 2026-08-28 12:17 UTC

**Date**: 2026-08-28 (third handoff of the day; supersedes `SESSION_HANDOFF_2026-08-28-0540Z.md`)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

17 PRs merged. The concurrent fault-injection plan is complete — all 11 tasks —
and `pkg/lsm` is the first package where a fault reaches a background worker
while a foreground reader runs. Then three testing regimes that did not exist
this morning: mutation testing, coupling coverage, and per-package coverage
floors. **Thirteen defects were found, and five existing tests turned out to be
decoration that passed for the wrong reason.**

> **Correction, 2026-08-29.** This line read "Nine defects were found, and five
> of them were in the instruments rather than in the system." Both halves
> misstated the source. The total is **13**, not 9 — the 9 is the *coupling*
> subset, as the #486 row below says ("9 of 13 defects were coupling defects").
> #486 sorts the 13 as 9 coupling, 1 inside one component, 3 process or the
> contract of a test. The **5** is a separate population, not a subset of the
> 13: § "How to use this handoff" records five *existing tests* that were
> decoration. Written as one sentence, the two counts read as a ratio, and a
> later session quoted "13 defects, 5 in the instruments", which is neither
> source.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #485 | `pkg/btree` and `pkg/lsm` on the vfs driver | The only failing check was `gofmt -s`, not `gofmt` — see §6 |
| #486 | Coupling and interference analysis | 9 of 13 defects were coupling defects |
| #487 | Session handoff, 05:40 UTC | Superseded by this file |
| #488 | Four `pkg/lsm` read-path defects | `Delete` had no effect on a flushed key; `Get` and `Scan` disagreed; the SSTable decoder allocated 1.8 GB on a 50-entry scan; a tombstone had no size so `Sync` wrote nothing |
| #489 | Concurrent fault-injection design + two plans | The read-path work split out as its own plan after peer review |
| #490 | Contract guard, red-first rule, `gofmt -s` gate | Found CC10, a contract unregistered since #412 |
| #491 | REST approval transcript + reference model | `testdata/rest_surface.golden` answers "what does this API do" on two screens |
| #492 | The concurrency harness, plan tasks 2–5 | `RoleFS`, `SweepRole`, the barrier, `Explore` |
| #493 | LSM invariant checker, tasks 6–7 | Five invariants, each with a test that corrupts a store the matching way |
| #494 | Five defects the sweeps reached, tasks 8–10 | **Two of the five came from repairing the other three** |
| #495 | Local lint gate | Nine of CI's eleven linters work here; only govet and staticcheck do not |
| #496 | C5 and D5 have evidence; D5 named the wrong structure | `BlockCache` is a record cache, not a block cache |
| #497 | Mutation testing | `pkg/vfs/vfstest` 64.71%, `pkg/lsm` 77.45% |
| #498 | Coupling coverage (DCCC) | 538/616 at the time |
| #499 | Per-package coverage floors | Eight floors from CI run 33159364069 |
| #500 | The two driver entry points nothing exercised | `pkg/vfs.Resolve` 0/6 → 6/6 |
| #501 | Coupling baseline re-measured | 563/616 = 91.4% |

## Current state

- **`origin/main`**: `511c6ef`
- **Open PRs**: none.
- **Branches**: `main`, plus `main-prerebase-backup` (pre-existing, not this session's).
- **Uncommitted**: none.
- **Gates, all verified on `main` after the last merge**: `contract-guard` OK (10 contracts, 13 guarding tests), `dccc-selftest` 6/6, `mutation-selftest` both outcomes, `coverage-floors-selftest` 5/5, `gofmt -s` clean over `pkg` and `cmd`, build clean, `-race` clean on `pkg/lsm` and `pkg/vfs/...`.

## What's next

1. **ADR 0002 stage 4: `pkg/storage` on the driver.** 179 call sites, and a
   `syscall.Mmap` path that does not fit `vfs.File` at all. This is a track, not
   a commit. It gates everything below it.
2. **Concurrent fault injection for `pkg/storage`.** Its 28 concurrent test
   files still inject nothing. The role classifier cannot be derived from paths
   there — a flush and a reader touch one file — so `pkg/vfs` needs a declared
   role, added additively.
3. **Out-of-process fault injection for `syscall.Mmap`** (FUSE or `dm-flakey`).
   Design document first; nothing built until the approach is chosen.
4. **Tail-latency metrics for the interference table**, new open item 4 in
   `COUPLING_AND_INTERFERENCE.md`. `ReportMetric` already has precedent in
   `pkg/retrieval`, `pkg/search` and `pkg/storage`.

### New gaps this session surfaced, not yet on the planning doc

- **Thresholds for the two new measures.** Mutation and DCCC both ship with
  baselines and no gate, deliberately: the numbers came from a developer
  machine. They become gates once CI has measured them.
- **The mutation survivors in `sweep_role.go`.** `n > maxOps` and the
  `maxOps <= 0` default are documented failure paths that no test reaches.
- **Model-based tests beyond the node surface.** Edges and traversal are not
  modelled; `pkg/graphql` has no approval transcript.
- **Scorecard rows still open**: 8 (boundary values), 11 (the truncation fuzz
  target opens 1 of 209 inputs), 4 (OOM covers only the WAL record buffer).

## Stale assumptions to retire

1. **`SESSION_HANDOFF_2026-08-28-0540Z.md` is superseded.** It gives `main` as
   `f9c9654`, lists #485 and #486 as open, and names concurrent fault injection
   as the next track. All three are now wrong.
2. **"CI's lint is the only working linter"** — `CLAUDE.md` said this, and #495
   corrected it in place. Nine of eleven linters run locally via
   `make lint-local`; only `govet` and `staticcheck` trip the Go 1.26 stdlib
   typecheck.
3. **"`gofmt` is the format gate"** — it is `gofmt -s`. `.golangci.yml:174` sets
   `simplify`, so CI rejects an empty `import ()` that plain `gofmt` accepts.
   This cost #485 a round trip. Corrected in `CLAUDE.md` by #490.
4. **`COUPLING_AND_INTERFERENCE.md`'s D5 row said "LSM block cache"** —
   corrected by #496. It is keyed by the record key with no file or offset, so
   the hazard the name implies cannot occur.
5. **The same file's "gap, stated plainly" said every fault test is
   single-threaded** — true when written, false for `pkg/lsm` since #492–#494.
   Still true for `pkg/storage`.
6. **`SQLITE_TESTING_SCORECARD.md` gap 1, per-package coverage floors, said
   "not started"** — done in #499.
7. **`MUTATION_BASELINE.md` gives `pkg/lsm` at 2m29s on four workers.** The
   configured default is two. The scores do not change with worker count; the
   wall clock does, and that row says so. Four workers exhausted this machine's
   memory into swap.
8. **The `/handoff` slash command still says "save to Serena memory".** Serena
   was removed in #465. This was flagged in the previous handoff and is still
   true — the command's instruction is dead, and the `session-handoff` skill is
   the working path.

## Open questions for the user

1. **ADR 0001 appendix.** A peer session (`graphdb-coord-4b`) has its user's
   approval for graphdb to write the uniqueness-registry request and response
   shapes into ADR 0001 as an appendix. Not started. ADR 0001 is `proposed`, so
   an appendix is a design decision, and the peer's user's yes is not this
   user's yes.
2. **The credential for `declare`.** An API key holding `write`, or admin?
   Recommended the API key: requiring admin makes `.graphdb_admin_password` a
   per-run dependency of a bootstrap script that needs it once, and #456 exists
   because that credential already behaves differently on first run. The peer
   is unblocked either way.
3. **Re-measure `pkg/lsm` mutation at two workers**, when the machine is quiet.
4. **Set thresholds** for mutation and DCCC once CI has measured them.

## Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## How to use this handoff

1. Read this, then `SQLITE_TESTING_SCORECARD.md` (17 rows) and
   `COUPLING_AND_INTERFERENCE.md` (the C and D tables, and its open items).
2. Then `CLAUDE.md` § "Orient first".
3. If picking up stage 4, read `docs/adr/0002-testability-drivers.md` and
   `CONCURRENT_FAULT_INJECTION_PLAN_2026-08-28.md` — the plan's task 2 explains
   why a derived role classifier will not work for `pkg/storage`.
4. **Before trusting any new gate, run its self-test.** Five tests this session
   were decoration and passed for the wrong reason. Every gate here has a
   `*-selftest` target for exactly that.
