# Concurrent fault injection for `pkg/lsm`

**Status**: design, approved 2026-08-28. Not implemented.
**Depends on**: PR #485 (`pkg/lsm` and `pkg/btree` on the `pkg/vfs` driver).
**Closes**: open item 1 of `COUPLING_AND_INTERFERENCE.md`, and rows C5 and D5 of its
coupling tables, which read "None" today.

## The gap

Every fault, crash and sweep test built on 2026-08-28 is single-threaded. Measured:
zero goroutines across `wal_sweep_test.go`, `wal_vfs_test.go`, `wal_crash_test.go`,
`wal_oom_test.go`, `lsm_vfs_test.go` and `pager_vfs_test.go`. Meanwhile `pkg/storage`
has 28 test files that run concurrent workloads, and none of them injects a fault.

The two halves have never met. `-race` does not join them: it reports two goroutines
that touch one address with no happens-before edge, and it injects nothing. It cannot
report a wrong *order of committed effects* across a component boundary. A flush worker
that writes an SSTable, fails before the level list is updated, and discards the error
is perfectly race-free.

## Why `pkg/lsm` is the first target

`pkg/storage` is the larger prize and is not reachable yet: ADR 0002 stage 4 has 179
call sites, and its `syscall.Mmap` path does not fit the `vfs.File` interface.

`pkg/lsm` holds the background workers that C5 names. `pkg/lsm/lsm.go:38-39` starts
`flushWorker` and `compactionWorker`. After #485 the package takes its filesystem from
`LSMOptions.FS`, so a fault driver reaches the shipped path.

## The hypotheses this design must confirm or refute

### H1 — one failed flush stops every later flush, and `Sync` then lies

`flush()` moves `memTable` into `immutableTable`, unlocks, and calls `NewSSTable`
(`pkg/lsm/lsm_workers.go:55-81`). On an error it returns and leaves `immutableTable`
set. Every later `flush()` stops at the "Already flushing" arm (`lsm_workers.go:58-62`).

Three consequences, in increasing order of severity:

1. `Get` still reads `immutableTable` (`pkg/lsm/lsm.go:96`), so the store looks correct
   while the process runs.
2. The memtable is never flushed again and grows without a bound.
3. `Sync()` calls `flush()` (`lsm.go:178`) and therefore returns `nil` having written
   nothing. `Close()` takes the same path at line 228. The durability contract is broken
   with a clean error return.

`flushWorker` discards the original error into `log.Printf` (`lsm_workers.go:34-36`), so
no caller ever learns that the first flush failed.

### H2 — one failed cleanup makes deleted keys readable again

`compact()` replaces `levels`, then calls `CleanupOldSSTables` (`lsm_workers.go:185`).
On an error the old `.sst` files stay on the disk. `NewLSMStorage` rebuilds the levels
with `ListSSTables`, which matches `*.sst` and parses the level out of the name
(`pkg/lsm/compaction.go:172-190`). The superseded files load beside their compacted
replacement, so a key whose tombstone was dropped during compaction becomes readable.

### H3 — a tombstone never masks a flushed value (confirmed, single-threaded)

Confirmed by probe on 2026-08-28, before this design was written.

`MemTable.Get` returns `(nil, false)` for a tombstone (`pkg/lsm/memtable.go:78`), which
`lsm.Get` cannot distinguish from "absent". It therefore falls through to the SSTable
scan and returns the pre-delete value (`lsm.go:89-113`). `SSTable.Get` has the same
shape: a tombstone yields `(nil, false)` and the loop continues to the next, older
SSTable (`pkg/lsm/sstable.go:52-55`).

So `Delete` has no effect on any key that has been flushed.

Reachability: `pkg/storage/lsm_storage_nodes.go:81,139,146,152` and
`pkg/storage/lsm_storage_edges.go:86,89,94` delete nodes, edges, label index entries and
property index entries through this path. `LSMGraphStorage` is exported from
`pkg/storage`, and no shipped binary constructs it — only `cmd/benchmark-graph-storage`
and the tests do. `EdgeStore`, which `UseDiskBackedEdges` does wire into the production
path, never calls `lsm.Delete`. The defect is therefore live for a library consumer and
latent for `cmd/server`.

H3 is not a concurrency defect and does not need this harness. It is in this document
because **it masks H2**: an oracle that asserts "a deleted key is absent after a reopen"
fails for H3's reason in every run, so scenario S2 cannot attribute a failure. H3 is
fixed first, as stage 0.

## Non-goals

- **Deterministic simulation.** A seeded cooperative scheduler in the style of
  FoundationDB or TigerBeetle is the strongest answer and needs the LSM workers to accept
  an injected scheduler. That is a production restructure beyond ADR 0002.
- **`pkg/storage`.** Stage 4 of ADR 0002 first.
- **Widening `pkg/vfs`.** The published `FileSystem` and `File` interfaces do not change.
  ADR 0002 needs no successor.
- **Redesigning the workers' error reporting.** `log.Printf` in `flushWorker` is a defect
  contributor. It is changed only as far as a confirmed defect's fix requires.

## Design

### Components

| File | New | Purpose |
|---|---|---|
| `pkg/vfs/vfstest/role.go` | yes | `RoleFS`: attributes each call to a role, arms faults per role, records a per-role trace. |
| `pkg/vfs/vfstest/pause.go` | yes | The barrier that parks one role's N-th operation until the test releases it. |
| `pkg/vfs/vfstest/role_test.go` | yes | Harness self-tests, including the positive controls in "Testing the harness". |
| `pkg/lsm/invariants.go` | yes | `CheckInvariants`, mirroring `pkg/storage/invariants.go` from #468. |
| `pkg/lsm/invariants_test.go` | yes | One test per invariant, each proved to fail on a corrupted store. |
| `pkg/lsm/lsm_concurrent_fault_test.go` | yes | Scenarios S1, S2 and S3, and the layered oracle. |

`vfstest` gains no knowledge of `pkg/lsm`. The classifier is a function the caller
supplies, and the LSM classifier lives in the LSM test file.

### Role attribution

```go
type Role string

type Op int // OpOpen, OpRemove, OpRename, OpStat, OpMkdirAll,
            // OpRead, OpWrite, OpSync, OpTruncate, OpClose

type Classifier func(op Op, name string, flag int) Role

func NewRoles(base vfs.FileSystem, name string, classify Classifier) *RoleFS
```

The classifier is called for `Open` and for the filesystem-level calls. A file takes the
role assigned at `Open` and keeps it for every `Read`, `Write`, `Sync`, `Truncate` and
`Close`, which are therefore never classified. This is correct for `pkg/lsm`, where one
actor opens a file and performs all of its I/O. `Op` still names those operations,
because the trace records them.

The LSM classifier reads the path and the open flags. `SSTablePath` writes
`L%d-%06d.sst` (`pkg/lsm/sstable_io.go:175`), so the level is in the name:

| Role | What the driver sees |
|---|---|
| `flush` | `Open` of `L0-*.sst` with `O_CREATE` |
| `compact` | `Open` of `L<n>-*.sst` with `O_CREATE` and `n >= 1`, and `Remove` of any `.sst` |
| `read` | `Open` of any `.sst` without `O_CREATE` |

The role is derived rather than declared, which costs no production change here. It does
not carry over to `pkg/storage`, where a flush and a reader touch one file. That case
needs a declared role, and `pkg/vfs` can gain one additively — a free function plus an
optional interface — without breaking an outside implementation.

### The soundness check

A sweep over "the role's N-th operation" is meaningful only if the role performs the same
sequence of operations on every run. Under concurrency that is an assumption.

`SweepRole` records the role's operation trace on each iteration and compares it with the
previous iteration's, up to the injection point. A divergence fails the test with the two
traces in the message. Without this the sweep still passes, still terminates, and proves
nothing — the failure mode the metamorphic tests had before #481.

### The barrier

```go
p := fs.PauseBeforeNthOpForRole(RoleFlush, 3)
// ... the scenario starts the flush worker ...
p.Wait(t, timeout)      // blocks the test until the worker reaches its 3rd operation
// the test now runs a reader against that exact suspended state
p.Release(vfstest.ErrInjected) // resume the worker, with or without an error
```

`Wait` takes a deadline and fails the test if the actor never arrives, because a worker
that never runs would otherwise hang the suite. `Release(nil)` resumes without a fault.
Every `Pause` must be released before the scenario ends, and the driver's `Close` fails
the test if one is outstanding, so a forgotten release cannot deadlock a later test.

### The sweep

```go
func SweepRole(t *testing.T, role Role, maxOps int,
    newFS func() *RoleFS,
    run func(fs vfs.FileSystem) error,
    check func(t *testing.T, n int, runErr error))
```

`newFS` builds a fresh driver, with its classifier, for each iteration. `Sweep` builds
its own driver inline because it needs no classifier; `SweepRole` cannot, so the caller
supplies the constructor.

N walks the role's operations from 1 upward. The loop ends when a run completes without
the fault firing, which means N has passed the end of that role's sequence. It fails,
rather than passing, on reaching `maxOps` or on a trace divergence. This is the existing
`Sweep` contract (`pkg/vfs/vfstest/sweep.go`) narrowed to one role.

### The oracle, in three layers

**Reopen.** The scenario records every key whose `Sync()` returned no error. After the
run it calls `Close()`, opens the directory again on `vfs.OS()`, and asserts that each
such key is present with its value and each deleted key is absent. This layer catches H1,
because H1's `Sync` returns `nil` and writes nothing.

**Live.** A reader goroutine samples a key set that the scenario never mutates. A key
that was visible must stay visible with the same value for the whole run. This layer
covers the visibility window that a compaction opens.

**Structural.** `lsm.CheckInvariants` asserts, at quiescence:

| # | Invariant |
|---|---|
| I1 | The set of paths in `levels` equals the set of `L<n>-<id>.sst` files in `dataDir`. |
| I2 | No path appears in two levels, or twice in one level. |
| I3 | An SSTable held at `levels[i]` has `i` as the level in its file name. |
| I4 | After `Sync()` returns `nil` and the workers are stopped, `immutableTable` is nil and the memtable is empty. |
| I5 | Every block-cache entry equals what a cache-bypassing lookup returns for that key. |

I4 states H1 as an invariant rather than as a symptom. I5 is D5, the block cache, and
needs a small unexported `getUncached` helper in `pkg/lsm`.

`CheckInvariants` is production code in `pkg/lsm`, not a test helper, for the reason
#468 promoted the storage one: a consumer that suspects a corrupt store must be able to
ask.

### Scenarios

| # | Coupling | What it does | Expected |
|---|---|---|---|
| S1 | C5 | Fill the memtable so `Put` calls `triggerFlush` (`lsm.go:67`), let the shipped `flushWorker` run, sweep the `flush` role. | Confirms H1 |
| S2 | C5, D2 | Build several L0 tables, force a compaction, sweep the `compact` role including the `Remove` calls of cleanup. | Confirms H2 |
| S3 | D5 | Concurrent readers and writers over overlapping keys while the `compact` role is faulted. | No hypothesis; this row has no evidence today |

All three open the store with `EnableAutoCompaction: true` and steer the shipped workers
through `triggerFlush` and `triggerCompaction`. No scenario waits on the 1-second or
10-second ticker. Testing the worker loop matters because the loop is where the flush
error is discarded.

Each confirmed defect gets two commits: the failing test, then the fix. This is the shape
#478 and #484 used.

## Testing the harness

A gate that cannot report the negative is not a gate. `role_test.go` must prove:

1. The classifier assigns the expected role for each path and flag combination in the
   table above. It must include a `dataDir` whose own name contains `L0-`, which a
   substring match against the full path would misclassify as a flush; the classifier
   must parse `filepath.Base`.
2. `FailNthOpForRole` fails exactly the N-th operation of that role and no operation of
   any other role.
3. The barrier really blocks: a goroutine parked by `PauseBeforeNthOpForRole` does not
   proceed until `Release`, proved by a state change that the test observes while it is
   parked, not by a sleep.
4. `Wait` fails the test, rather than hanging, when the actor never arrives.
5. The soundness check fails on a scenario built to be nondeterministic.

`invariants_test.go` must prove each of I1 to I5 fails on a store corrupted in the
matching way. An invariant that has never failed has not been shown to work.

## CI cost

Sweeps multiply runs, and a sweep of a compaction is not cheap. The narrow scenarios run
in the normal suite. The wide sweeps are skipped under `-short`, which is what
`pkg/storage` already does for its long tests. If the wide sweeps exceed the storage
suite's 300-second budget they move to the nightly `Fuzz` workflow, which ran
successfully for the first time on 2026-08-28.

## Order of work

| Stage | Work | Blocked by |
|---|---|---|
| 0 | Fix H3: `Get` must stop at a tombstone in the memtable, in the immutable table, and in each SSTable. Failing test first. | Nothing. Independent of #485. |
| 1 | Merge #485. Its only failing check is `gofmt` on `pkg/lsm/compaction_types.go`. | — |
| 2 | `RoleFS`, the barrier and `SweepRole`, with the self-tests above. | 1 |
| 3 | `pkg/lsm/invariants.go` and its tests. | Nothing, but it is easier to write after stage 0. |
| 4 | S1, and the fix for H1 if S1 confirms it. | 2, 3 |
| 5 | S2, and the fix for H2 if S2 confirms it. | 0, 4 |
| 6 | S3. | 4 |
| 7 | Update `COUPLING_AND_INTERFERENCE.md` rows C5 and D5, and `SQLITE_TESTING_SCORECARD.md`. | 4, 5, 6 |

Stage 0 precedes stage 5 because H3 masks H2 in the oracle.

## Risks

- **The local `golangci-lint` is broken.** It fails typecheck on a Go 1.26 standard
  library file, which suppresses the analysers and reports a clean-looking environment
  error. It missed real findings on #479 and #485. CI's lint is the only reading that
  counts.
- **The scenarios may not be deterministic per role.** The soundness check turns that
  from a silent unsound pass into a visible failure, but it does not fix it. If a role's
  trace will not settle, the barrier is the fallback: park the actor and inject at a named
  point instead of sweeping.
- **`Put` triggers a flush only when the memtable is full.** The scenarios set
  `MemTableSize` small enough that a handful of writes fills it, rather than writing 4 MB.

## References

- `COUPLING_AND_INTERFERENCE.md` — C5, D2, D5 and the open items this closes
- `SQLITE_TESTING_SCORECARD.md` — the per-component axis this complements
- `docs/adr/0002-testability-drivers.md` — why the drivers ship as ordinary code
- A(M)C 20-193 MCP_Software_2 — data and control coupling coverage of a multicore system
- DO-178C 6.4.4.d — why coupling coverage is an objective separate from structural coverage
