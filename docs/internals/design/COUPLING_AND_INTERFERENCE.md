# Coupling and interference analysis

**Living document.** Update it in the same PR as any change that adds, removes
or alters a coupling.

**Last verified**: 2026-08-28, against `main` at `f9c9654`.

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
| C5 | Background flush and compaction → foreground readers | A worker decides when a memtable becomes an SSTable, changing which read path a concurrent reader takes | **None under fault injection** |

### Data coupling

| # | Shared data | Not under whose exclusive control | Evidence today |
|---|---|---|---|
| D1 | `snapshot.mmap` format | Written by 5 production files, read by others; the reader trusts a length the writer wrote. **#477's two panics lived here** | `FuzzMmapSnapshotCorruption`, the JSON↔mmap oracle |
| D2 | WAL record format | The WAL writes, storage replay reads. **#478's phantom entries and #484's LSN reuse lived here** | Crash sweep, OOM sweeps |
| D3 | Derived indexes vs shard ground truth | Every write path must update several representations | `CheckInvariants`, 35 checks |
| D4 | The mmap CoW overlay | A reader materialises from the base while a writer promotes into the shard | `CheckInvariants` mmap path (#474) |
| D5 | LSM block cache | Shared LRU across readers; one workload evicts another's entries | **None** |
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
| 256 shard locks | Direct, partitioned | **Yes, by design.** Partitioning a contended resource is the software analogue of hardware partitioning, and `bench_concurrent_read_test.go` measures the effect |
| LSM block cache (D5) | Direct | No |
| Background flush/compaction vs foreground I/O | Direct | No |
| Go GC and scheduler | Indirect | Not analysable from inside |

The paper's definition is worth adopting: a channel is **mitigated** when there
is no observable impact from it on the system's performance. graphdb can
measure that for the ones it owns, and `bench_concurrent_read_test.go` already
does for the shard locks.

## The gap, stated plainly

**Every fault, crash and sweep test built on 2026-08-28 is single-threaded.**
Measured: zero of the tests in `wal_sweep_test.go`, `wal_vfs_test.go`,
`wal_crash_test.go`, `wal_oom_test.go`, `lsm_vfs_test.go` and
`pager_vfs_test.go` start a goroutine. Meanwhile `pkg/storage` has 28 test
files that do run concurrent workloads, and none of them inject a fault.

So the two halves have never met: faults are injected sequentially, and
concurrency is tested without faults. C5, D4 and D5 are exercised by neither.

`-race` does not close this. It finds unsynchronised access; it does not find
wrong *sequencing* across an interface, and it injects nothing.

## What DCCC coverage would mean here

The mechanically useful part of DO-178C's guidance: define the interfaces well
enough to write test cases against them, then measure coverage of "the relevant
read/write and procedure call statements" — that is, statement coverage
restricted to the statements that cross a component boundary.

For graphdb that is tractable: the tables above name the interfaces, and the
call sites are identifiable. A coverage report filtered to those sites would
say what fraction of the couplings any test run actually exercises. Nothing
computes that today.

## Open items

1. **Concurrent fault injection.** Inject a fault while a background flush is
   mid-write and a reader is mid-scan, and sweep that point. This is the
   graphdb analogue of MCP_Software_2, and it is where the residual risk is.
2. **A DCCC coverage measure** over the interfaces above.
3. **D5 and C5 have no evidence at all.**
4. **The CI-versus-developer coverage gap is still unexplained.** The leading
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
- `SQLITE_TESTING_SCORECARD.md` — the per-component axis this document complements
