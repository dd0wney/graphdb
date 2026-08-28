# SQLite testing scorecard

**Living document.** Update it in the same PR as any change that moves a row.
A tracker that is refreshed afterwards is a tracker that is wrong between times.

**Last verified**: 2026-08-28, against `main` at `99d8868` plus #483.

## Why this exists

graphdb spent 2026-08-28 working through SQLite's testing regime
(sqlite.org/testing.html, plus D. R. Hipp's 2026-07-16 talk). Eighteen PRs
merged. The programme was run off a list that existed only in a chat window, so:

- the planning doc recorded two of the eighteen,
- `docs/adr/0002` planned the drivers but not the techniques, and mentioned
  neither the N-sweep nor faultsim, both of which shipped,
- the state of the list was re-derived from memory each time it was reported.

That is precisely the drift these techniques exist to catch, so the programme
gets a tracked artifact.

## Scale, for context

| | SQLite | graphdb |
|---|---|---|
| Test code vs source | 590× | **1.47×** (130,961 / 88,895 LOC) |
| Fuzz targets | dbsqlfuzz, ~1e9 mutations/day | **16**, nightly |
| Fault-sim sites | 24 | **2** |
| assert() in shipped core | 6,754 | 0 (see row 12) |

The ratio is not a target. It is the reason graphdb should expect to keep
finding defects for some time: three of the four techniques applied this week
found one on first run.

## The fifteen techniques

| # | Technique | State | Evidence | Next |
|---|---|---|---|---|
| 1 | Four independent harnesses | ✗ | One (`go test`), plus 13 consumer-contract tests as a partial second view | Not planned. A second harness is not obviously worth it at this size |
| 2 | 100% branch + MC/DC coverage | ✗ | Statement coverage only, gated at a 74.0% floor; CI measures 75.5% (#469) | Per-package floors, per orca's `covcheck` design. **Unplanned — see Gaps** |
| 3 | Millions of test cases | ✗ | Fixed cases + fuzz seeds | Follows from 7, not pursued directly |
| 4 | Out-of-memory testing | ✗ | Go cannot substitute `malloc` | Allocation-limit driver for graphdb's own large buffers = ADR 0002 Driver 3. **Not built.** The double loop (fail once at N; fail everything from N) is the shape to copy |
| 5 | I/O error testing | ◐ | `pkg/vfs` + `vfstest.FaultFS` on the production path (#479); N-sweep (#481); `pkg/btree` and `pkg/lsm` migrated (#485). Found the `WAL.Close` descriptor leak (#466) and the LSM error-chain break (#485) | `pkg/storage` (179 sites) not migrated = ADR 0002 stage 4. Its `syscall.Mmap` path does not fit `File` at all |
| 6 | Crash and power-loss | ✓ | `vfstest.CrashFS` with LoseUnsynced / LosePartial / **ReorderAndLose** (#479); phantom-entry fix (#478); `SweepCrash` walks the cut through every operation with repeats per point (#483). `TestWAL_SweepEveryCrashPoint`: 9 cut points, 36 runs, 26 recovering entries | All three parts of the slide are now covered for `pkg/wal`. `pkg/lsm`, `pkg/btree` and `pkg/storage` inherit it once they migrate to the driver (ADR 0002 stages 3-4) |
| 7 | Fuzz testing | ✓ | 16 targets, nightly workflow (#467), first run verified green | Corpus is not persisted beyond the cache; consider committing high-value seeds |
| 8 | Boundary-value testing | ✗ | No `testcase()` equivalent | Unplanned. Go has no coverage-visible marker; needs thought |
| 9 | Disabled-optimization testing | ◐ | Two differential oracles: JSON↔mmap enumeration, SIMD↔scalar | No general switch. ADR 0002 Driver 4 (test control) |
| 10 | Regression testing | ✓ | 13 `CONSUMER CONTRACT:` tests + `scripts/consumer-drive.sh` | — |
| 11 | Malformed-database testing | ◐ | `FuzzMmapSnapshotCorruption` + `FuzzMmapSnapshotTruncation` (#477); two panics fixed. WAL phantom entries (#478) | Truncation target is near-vacuous (1 of 209 lengths opens) — a CRC-recomputing variant would reach the read paths |
| 12 | assert() and runtime checks | ◐ | `CheckInvariants` in production code for both representations (#468, #474) | No `never`/`always` equivalents, no production asserts = ADR 0002 Driver 4 |
| 13 | Valgrind | ✓ analogue | `-race` in CI | No `-msan`/`-asan`. Low value while row 14 holds |
| 14 | Undefined-behaviour checks | ✓ N/A, **gated** | Zero cgo, `CGO_ENABLED=0` in goreleaser and both Dockerfiles; CI job enforces it (#480) | Stays N/A only while graphdb is cgo-free. The gate is what makes that a fact rather than an assumption |
| 15 | Checklists | ◐ | Audits, ADRs 0001–0002, this scorecard | — |

Legend: ✓ done · ◐ partial · ✗ absent

Counts: **4 done, 5 partial, 6 absent.**

## Defects these techniques found

Recorded because the yield is the argument for the next stage.

| Defect | Found by | PR |
|---|---|---|
| `WAL.Close` leaked the descriptor when flush or fsync failed | I/O fault seam, first run | #466 |
| Two metamorphic tests asserted nothing (mmap ground truth empty) | Invariant checker refusing rather than answering | #468 |
| Coverage floor set from the wrong environment | The gate failing on its own PR | #469 |
| `:Claim` uniqueness bypassed by a second label | Reading the rule against the storage primitive | #470 |
| Decoder panic on a damaged mmap record | Fuzz seed corpus, before fuzzing | #477 |
| `sections()` panic at open on a corrupt offset | Fuzzing, 8.6M execs | #477 |
| Zero-filled corruption forges valid WAL entries, resetting the LSN | Hole-in-the-middle crash test | #478 |
| `WriteAt` impossible on an `O_APPEND` file | First real caller of `CrashFS` | #479 |
| `-tags nng` CI job compiled an identical file set | Measuring the tag's effect | #480 |
| Recovery may exceed acknowledged appends (test invariant wrong) | The N-sweep, at N=3 | #481 |

## Gaps in the plan itself

These are tracked nowhere else, which is the point of this section.

1. **Per-package coverage floors** (row 2). The single global floor lets a
   well-covered package mask a poor one — `pkg/query` at 62.9% hides behind
   `pkg/parallel` at 93.4%. orca's `tools/covcheck.py` has the design: per-file
   floors, three exit codes, and a self-test. Not started, not in any ADR.
2. **The coverage number moves ~4 points with the machine** (CI 75.5% vs
   developer 79.5%, reproduced). Unexplained. If timing-dependent paths are the
   cause, some statements are only ever covered on fast hardware.
3. **`syscall.Mmap` cannot be reached by any Go driver.** The honest fix is
   out-of-process (FUSE or `dm-flakey`), which is where C and Bazel fit — as a
   separate binary, never as a cgo dependency. Not designed.
4. **ADR 0002 covers drivers, not techniques.** The N-sweep and faultsim shipped
   without appearing in it. Either the ADR grows a techniques section or this
   scorecard is the authority for them; currently this scorecard is.

## How to use this

1. Moving a row means editing this file in the same PR.
2. A new defect found by one of these techniques goes in the defects table. The
   table is the evidence for spending more effort here.
3. When a row reaches ✓, say what the evidence is, not that it is done.
