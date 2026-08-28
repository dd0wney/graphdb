# Coupling and interference analysis

**Living document.** Update it in the same PR as any change that adds, removes
or alters a coupling.

**Last verified**: 2026-08-28, against `main` at `c39efab`.

## Why this exists, and why the SQLite scorecard is not enough

`SQLITE_TESTING_SCORECARD.md` tracks graphdb against SQLite's fifteen
techniques. That list is calibrated for **one component**: a single
`sqlite3.c`, where MC/DC over that component is nearly the whole story.

graphdb is 42 packages. Sorting the thirteen defects found on 2026-08-28 by
where they lived:

| Kind | Count | Examples |
|---|---|---|
| **Coupling** — an interface, shared data, or a caller narrowing a callee's guarantee | **9** | `:Claim` bypass, mmap decoder panic, `sections()` panic, zero-fill phantom entries, OOM LSN reuse, `ListSSTables` `%v`, nil SSTable driver, `WriteAt` on `O_APPEND`, metamorphic tests asserting nothing |
| Within one component | 1 | `WAL.Close` descriptor leak |
| Process or test contract | 3 | coverage floor from the wrong machine, the `-tags nng` job, the N-sweep invariant |

Nine of thirteen were coupling defects. **Not one would have been caught by
raising statement or branch coverage inside a package.** The scorecard was
measuring the wrong axis for most of what was actually wrong.

DO-178C treats this as a separate objective for the same reason — 6.4.4.d,
"test coverage of software structure (data coupling and control coupling) is
achieved", sits apart from structural coverage because unit coverage verifies
behaviour *inside* a component and cannot verify components interact correctly
once integrated.

## Definitions

From DO-248C, and used verbatim here because a looser definition makes the
enumeration below arbitrary:

- **Data coupling** — "The dependence of a software component on data not
  exclusively under the control of that software component."
- **Control coupling** — "The manner or degree by which one software component
  influences the execution of another software component."

## graphdb's couplings

### Control coupling

| # | Interface | What one component does to another's execution | Evidence today |
|---|---|---|---|
| C1 | `pkg/wal` → `pkg/storage` replay | `persistence_replay.go:47` switches on `wal.OpType` and dispatches into a different storage mutation per arm. The WAL decides which storage path runs | Replay tests per op type |
| C2 | `pkg/wal/apply` → storage | `apply.go:108` switches on `op.Type`, the fail-closed apply gate | `apply_test.go` |
| C3 | `pkg/graphql` and `pkg/api` → `pkg/storage` uniqueness | Two callers decide whether a write takes the atomic unique path or the ordinary one. **This is where #470 lived**: both callers narrowed a guarantee storage made correctly | Tests on both surfaces since #470 |
| C4 | `pkg/storage` → `pkg/vector` | Node create/update/delete drive vector index maintenance (5 sites in `node_operations.go`) | Metamorphic tests |
| C5 | Background flush and compaction → foreground readers | A worker decides when a memtable becomes an SSTable, changing which read path a concurrent reader takes | `TestFlushWorkerUnderFaultSweep`, `TestCompactionUnderFaultSweep` (#494) |

### Data coupling

| # | Shared data | Not under whose exclusive control | Evidence today |
|---|---|---|---|
| D1 | `snapshot.mmap` format | Written by 5 production files, read by others; the reader trusts a length the writer wrote. **#477's two panics lived here** | `FuzzMmapSnapshotCorruption`, the JSON↔mmap oracle |
| D2 | WAL record format | The WAL writes, storage replay reads. **#478's phantom entries and #484's LSN reuse lived here** | Crash sweep, OOM sweeps |
| D3 | Derived indexes vs shard ground truth | Every write path must update several representations | `CheckInvariants`, 35 checks |
| D4 | The mmap CoW overlay | A reader materialises from the base while a writer promotes into the shard | `CheckInvariants` mmap path (#474) |
| D5 | LSM record cache | Shared LRU across readers; one workload evicts another's entries. **Named a block cache here until 2026-08-28, and it is not one**: `BlockCache` is keyed by the record key and holds that record's value (`pkg/lsm/lsm.go`), with no file and no offset in the key. So the hazard the name suggests — a reader serving data from a file compaction removed — cannot occur, because no entry names a file | `TestRecordCacheUnderFaultedCompaction` (#494); `CheckInvariants` I5 (#493) |
| D6 | `vfs.FileSystem` / `alloc.Allocator` | Process-wide installed drivers | The drivers' own tests |

## Interference

A(M)C 20-193 defines an interference channel as "a platform property that may
cause interference between software applications or tasks", and distinguishes
**direct** (competition for a shared resource) from **indirect** (hardware
unpredictability changing behaviour). A typical hardware platform has 20-250.

graphdb is software, so the hardware channels are not its concern directly.
The concept transfers to its own shared resources:

| Channel | Kind | Mitigated? |
|---|---|---|
| Global `gs.mu` | Direct | Partially — the A4 work moved readers to per-shard locks |
| 256 shard locks | Direct, partitioned | **Partitioned by design, and the mitigation is not measured.** `bench_concurrent_read_test.go` has 12 benchmark functions and no `ReportMetric`, no percentile and no maximum, so it reports Go's default mean. A(M)C 20-193 asks for no observable impact on performance, and a mean is not even a high-water mark. See open item 5 |
| LSM record cache (D5) | Direct | No. The eviction claim is still unmeasured — see open item 4 |
| Background flush/compaction vs foreground I/O | Direct | No |
| Go GC and scheduler | Indirect | Not analysable from inside |

The paper's definition is worth adopting: a channel is **mitigated** when there
is no observable impact from it on the system's performance. graphdb can
measure that for the ones it owns, and `bench_concurrent_read_test.go` already
does for the shard locks.

## The gap, and what is left of it

**As first written, this section said every fault, crash and sweep test in the
repository was single-threaded, and that `pkg/storage`'s 28 concurrent test
files injected no fault.** That was measured and it was true. It is no longer
true of `pkg/lsm`.

`TestFlushWorkerUnderFaultSweep` walks a fault through every I/O operation the
shipped flush worker performs while a foreground reader runs.
`TestCompactionUnderFaultSweep` does the same for a compaction, including the
removals of its cleanup. `TestRecordCacheUnderFaultedCompaction` runs readers
against a store whose compaction is failing underneath them. C5 and D5 have
evidence for the first time.

**What is left**: `pkg/storage`. Its 28 concurrent test files still inject no
fault, and they cannot until ADR 0002 stage 4 puts the package on the driver —
179 call sites, and a `syscall.Mmap` path that does not fit `vfs.File` at all.
D4, the mmap copy-on-write overlay, is inside that.

`-race` does not close any of it. It finds unsynchronised access. It does not
find wrong *sequencing* across an interface, and it injects nothing. Every
defect in the table below is race-free.

## What the concurrency track found, and a category this document lacked

Five defects, and only two of them were the ones the design predicted:

| Defect | Kind | Predicted |
|---|---|---|
| `flush` strands `immutableTable`, so flushing stops for the life of the store | Control coupling, C5 | Yes, H1 |
| `NewSSTableWithFS` leaves its half-written file, so one failed flush makes the store unopenable | Data coupling, D2-like: a writer leaves a file a reader trusts | No |
| The repair for H1 discarded writes `Put` had acknowledged | **Introduced by a repair** | No |
| `compact` leaves superseded tables, so a reopen resurrects a deleted key | Data coupling, C5 | Yes, H2 |
| `CleanupOldSSTables` counted an absent file as a failure, so no retry could work | **Introduced by a repair** | No |

**Two of the five came out of repairing the other three**, and the first table
in this document has no row for that. It is the argument for a sweep rather
than a single test: a sweep re-runs after every change, so a repair that breaks
something else is caught by the same instrument that found the original.

It is also the argument for the red-first rule. Two tests in that track were
decoration when first written, and each looked correct. The partial-file test
faulted the flush's *open*, so no file was ever created to leak. The cache
scenario started its reader *after* the writes, so nothing it cached could go
stale. Both were found by removing the repair and watching the test stay
green — not by reading them.

## What DCCC coverage would mean here

The mechanically useful part of DO-178C's guidance: define the interfaces well
enough to write test cases against them, then measure coverage of "the relevant
read/write and procedure call statements" — that is, statement coverage
restricted to the statements that cross a component boundary.

For graphdb that is tractable: the tables above name the interfaces, and the
call sites are identifiable. A coverage report filtered to those sites says
what fraction of the couplings any test run actually exercises.

`make dccc` computes it (#498). `couplings.tsv` names the sites, and the
measure is statement coverage restricted to them. The baseline is 563/616 =
91.4% (#501), recorded in `DCCC_BASELINE.md`. There is no threshold, because
the number came from a developer machine — see the same caveat on the mutation
baseline.

## Open items

1. ~~**Concurrent fault injection.**~~ Done for `pkg/lsm` (#492, #493, #494).
   Open for `pkg/storage`, which needs ADR 0002 stage 4 first. That is where
   the residual risk now sits, and it is the larger half.
2. ~~**A DCCC coverage measure** over the interfaces above.~~ Done. `make dccc`
   (#498), baseline 563/616 = 91.4% (#501). What is left is a threshold, which
   waits on a CI measurement rather than a developer-machine one.
3. ~~**D5 and C5 have no evidence at all.**~~ Both have evidence now. D5's
   *description* was also wrong, which is worth keeping in view: the incorrect
   name generated a plausible correctness hazard that cannot occur, and it was
   only caught by reading the code the row describes.
4. **Nothing here measures a maximum.** The mitigation claims in the
   interference table rest on benchmarks that report a mean. Rapita's account
   of worst-case execution time draws the distinction that matters: a
   measurement gives a high-water mark, not a WCET, and a mean gives neither.
   graphdb has no real-time deadline, so the certification use does not carry
   over, but the measurement discipline does — an interference channel is
   visible in the tail, not in the average. `b.ReportMetric` is the whole cost.

5. **The CI-versus-developer coverage gap is still unexplained.** The leading
   hypothesis — interference from a 4-core runner suppressing timing-dependent
   paths — was **tested and falsified** on 2026-08-28: constraining this
   machine to `GOMAXPROCS=4 -p 2` produced 80.0%, identical to unconstrained,
   against CI's 75.5% measured twice. The constraint was verified to apply
   (`runtime.GOMAXPROCS` reported 4). One hypothesis eliminated; the gap stands.

## References

- DO-248C definitions of data and control coupling, via Rapita MC-WP-011 §6.9.2
- DO-178C objective A-7 #8 (6.4.4.d), required with independence at DAL A/B
- A(M)C 20-193 MCP_Software_2 — DCCC of a multicore system; MCP_Resource_Usage_3
  — identify interference channels
- Rapita MC-WP-015, *Mitigation of interference in multicore processors*
- Rapita, *Worst-case execution time* — the measurement-based, static and
  hybrid methods, and why a measurement gives a high-water mark and not a WCET.
  The source for open item 4
- `SQLITE_TESTING_SCORECARD.md` — the per-component axis this document complements
