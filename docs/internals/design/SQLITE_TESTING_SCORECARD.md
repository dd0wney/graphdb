# SQLite testing scorecard

**Living document.** Update it in the same PR as any change that moves a row.
A tracker that is refreshed afterwards is a tracker that is wrong between times.

**Last verified**: 2026-08-28, against `main` at `568adf3` plus #484.

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
| Fault-driver roles | n/a | **3** (`flush`, `compact`, `read`) |
| OOM loops | 2 | **2** |
| assert() in shipped core | 6,754 | 0 (see row 12) |

The ratio is not a target. It is the reason graphdb should expect to keep
finding defects for some time: three of the four techniques applied this week
found one on first run.

## The fifteen techniques

| # | Technique | State | Evidence | Next |
|---|---|---|---|---|
| 1 | Four independent harnesses | ✗ | One (`go test`), plus 13 consumer-contract tests as a partial second view | Not planned. A second harness is not obviously worth it at this size |
| 2 | 100% branch + MC/DC coverage | ✗ | Statement coverage only. Gated at a 74.0% global floor AND at per-package floors (`coverage-floors.tsv`, taken from CI run 33159364069 on main), so a well-covered package can no longer hide a poor one | Still statement coverage, not branch and not MC/DC. `pkg/query` at 63.0% is the lowest and its floor only stops it falling further |
| 3 | Millions of test cases | ✗ | Fixed cases + fuzz seeds | Follows from 7, not pursued directly |
| 4 | Out-of-memory testing | ◐ | `pkg/alloc` + `alloctest` with BOTH loops — fail-once-at-N and fail-all-from-N — swept to termination (#484). Gates graphdb's own length-driven buffers; Go's implicit allocations are out of reach and the package says so. Found the LSN-reuse defect below | Only `pkg/wal`'s record buffer is gated. Snapshot assembly, LSM blocks and query results are not |
| 5 | I/O error testing | ◐ | `pkg/vfs` + `vfstest.FaultFS` on the production path (#479); N-sweep (#481); `pkg/btree` and `pkg/lsm` migrated (#485). **Now runs against background workers while a foreground reader runs** — `RoleFS` attributes each operation to an actor and `SweepRole` walks the fault through one actor's operations, checking that the actor's sequence is stable rather than assuming it (#492). Found the `WAL.Close` descriptor leak (#466), the LSM error-chain break (#485), and five defects in the LSM flush and compaction paths (#494), two of which were introduced by repairing the other three | `pkg/storage` (179 sites) not migrated = ADR 0002 stage 4. Its `syscall.Mmap` path does not fit `File` at all, and its 28 concurrent test files still inject nothing |
| 6 | Crash and power-loss | ✓ | `vfstest.CrashFS` with LoseUnsynced / LosePartial / **ReorderAndLose** (#479); phantom-entry fix (#478); `SweepCrash` walks the cut through every operation with repeats per point (#483). `TestWAL_SweepEveryCrashPoint`: 9 cut points, 36 runs, 26 recovering entries | All three parts of the slide are now covered for `pkg/wal`. `pkg/lsm`, `pkg/btree` and `pkg/storage` inherit it once they migrate to the driver (ADR 0002 stages 3-4) |
| 7 | Fuzz testing | ✓ | 16 targets, nightly workflow (#467), first run verified green | Corpus is not persisted beyond the cache; consider committing high-value seeds |
| 8 | Boundary-value testing | ✗ | No `testcase()` equivalent | Unplanned. Go has no coverage-visible marker; needs thought |
| 9 | Disabled-optimization testing | ◐ | Two differential oracles: JSON↔mmap enumeration, SIMD↔scalar | No general switch. ADR 0002 Driver 4 (test control) |
| 10 | Regression testing | ✓ | 13 `CONSUMER CONTRACT:` tests + `scripts/consumer-drive.sh`, and `make contract-guard` now pins each one's body so a weakened assertion changes a tracked file (#490). An approval transcript of the REST surface and a reference-model test of the node surface (#491) | The contract guard found a tenth contract that had no registry row since #412 |
| 11 | Malformed-database testing | ◐ | `FuzzMmapSnapshotCorruption` + `FuzzMmapSnapshotTruncation` (#477); two panics fixed. WAL phantom entries (#478) | Truncation target is near-vacuous (1 of 209 lengths opens) — a CRC-recomputing variant would reach the read paths |
| 12 | assert() and runtime checks | ◐ | `CheckInvariants` in production code for both `pkg/storage` representations (#468, #474) and now for `pkg/lsm` — the level set against the files on the disk, and the record cache against the levels (#493). Each of its five invariants has a test that corrupts a store in the matching way | No `never`/`always` equivalents, no production asserts = ADR 0002 Driver 4. The `pkg/lsm` checker says nothing about the contents of an SSTable |
| 13 | Valgrind | ✓ analogue | `-race` in CI | No `-msan`/`-asan`. Low value while row 14 holds |
| 14 | Undefined-behaviour checks | ✓ N/A, **gated** | Zero cgo, `CGO_ENABLED=0` in goreleaser and both Dockerfiles; CI job enforces it (#480) | Stays N/A only while graphdb is cgo-free. The gate is what makes that a fact rather than an assumption |
| 15 | Checklists | ◐ | Audits, ADRs 0001–0002, this scorecard | — |

| 16 | **Coupling coverage (DCCC)** — not a SQLite technique | ◐ | Added because SQLite's list is calibrated for one component and graphdb is 42 packages. **9 of the 13 defects found on 2026-08-28 were coupling defects**, and none would have been caught by raising coverage inside a package. Analysis: `COUPLING_AND_INTERFERENCE.md` `docs/internals/design/couplings.tsv` names 14 sites and `make dccc` measures statement coverage restricted to them: **538/616 = 87.3%**, with two sites at zero. Baseline and method: `DCCC_BASELINE.md` | 14 sites, and D4 is not among them — no single symbol is the mmap overlay boundary. No threshold: these numbers came from a developer machine |

| 17 | **Mutation testing** — not a SQLite technique | ◐ | Added because coverage says which lines ran, not which lines anything checked. Five tests written on 2026-08-28 ran the code they claimed to test and constrained nothing, and each was found by reverting the repair by hand and watching the test stay green — this is that, automated. `gremlins` via `make mutation`, scoped to `pkg/vfs/vfstest` and `pkg/lsm`. Baselines in `MUTATION_BASELINE.md` | Two packages only. No threshold yet: a floor set before the number is understood is how the coverage floor was got wrong in #469 |

Legend: ✓ done · ◐ partial · ✗ absent

Counts: **4 done, 8 partial, 5 absent** (of 17 rows; rows 16 and 17 are not SQLite techniques).

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
| An allocation failure during WAL open truncated the LSN, so the next append reused an LSN already on disk — silently dropped by the next recovery | The OOM fail-once loop | #484 |
| `ListSSTables` flattened its cause with `%v`, so `errors.Is` could not tell an I/O failure from any other reason a table would not open | The LSM I/O sweep | #485 |
| SSTable constructors returned a nil driver, so the first Close or Remove would have panicked | Reading the constructors instead of trusting the build | #485 |
| Two scorecard rows were silently lost: a `str.replace()` anchored on a row that did not exist on the branch did nothing, and nothing checked | Noticing the merged table was short | #485 |

## Gaps in the plan itself

These are tracked nowhere else, which is the point of this section.

1. ~~**Per-package coverage floors** (row 2).~~ Done. `coverage-floors.tsv`
   holds a floor for each of the eight packages in `COVER_PKGS`, two points
   below what CI measured on main, and `make coverage-floors` enforces them in
   the coverage job. Three exit codes and a self-test, following
   `coverage-gate.sh`. The numbers came from CI run 33159364069, never from a
   developer machine — that distinction is what #469 was about.
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
