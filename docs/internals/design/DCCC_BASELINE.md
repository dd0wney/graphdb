# Coupling coverage baseline

**Living document.** Update it in the same PR as any change that moves a number.

**Measured**: 2026-08-28, `go test -short ./pkg/...` on a developer machine.

## What this measures, and what it does not

Nine of the thirteen defects found on 2026-08-28 were coupling defects, and not
one would have been caught by raising statement or branch coverage inside a
package. DO-178C separates the two for that reason: 6.4.4.d asks for coverage
of the interfaces between components, because unit coverage verifies behaviour
inside one and cannot verify that two interact correctly once integrated.

The mechanically useful part of that guidance is narrow, and this implements
exactly it: define the interfaces well enough to write test cases against them,
then measure statement coverage **restricted to the statements that cross a
boundary**. `couplings.tsv` is the definition. `make dccc` is the measure.

It is not MC/DC, it is not branch coverage, and a high number here does not say
the couplings are correct. It says how much of them a test run reaches.

## Baseline

| Coupling | Site | Covered |
|---|---|---|
| C1 | `pkg/storage.replayEntry` | 19/20, 95.0% |
| C2 | `pkg/wal/apply.ApplyWriteOperation` | 13/14, 92.9% |
| C3 | `pkg/api.createNode` | 36/38, 94.7% |
| C4 | `pkg/storage.CreateNodeWithTenant` | 12/14, 85.7% |
| C5 | `pkg/lsm.flush` | 84/91, 92.3% |
| C5 | `pkg/lsm.compact` | 52/55, 94.5% |
| D1 | `pkg/storage.openMmapSnapshot` | 26/33, 78.8% |
| D1 | `pkg/storage.sections` | 25/31, 80.6% |
| D2 | `pkg/wal.writeEntryTo` | 8/14, 57.1% |
| D2 | `pkg/wal.readEntry` | 28/28, 100.0% |
| D3 | `pkg/storage.CheckInvariants` | 227/242, 93.8% |
| D5 | `pkg/lsm.lookupEntry` | 8/10, 80.0% |
| D6 | `pkg/vfs.Resolve` | **0/6, 0.0%** |
| D6 | `pkg/alloc.Bytes` | **0/20, 0.0%** |

**538/616 statements = 87.3%.**

## The two at zero

**`pkg/vfs.Resolve` has no test at all**, and its own doc comment says why that
matters:

> An unknown name is an error rather than a silent fallback, because falling
> back would run a test against the real filesystem while the test believed it
> had installed a fault driver — a pass that means nothing.

The function that exists to prevent a meaningless pass is the one nothing
exercises. `pkg/vfs` has no `_test.go` files; its drivers are tested from
`pkg/vfs/vfstest`, which never calls `Resolve`.

**`pkg/alloc.Bytes` is the same shape.** `pkg/alloc` has no package-level tests;
`alloctest` covers the fault driver and not the production entry point.

Both are D6, the process-wide installed drivers, and D6 is the row of the
coupling table whose evidence column reads "the drivers' own tests".

## No threshold, deliberately

A floor set before the number is understood is how the coverage floor was got
wrong in #469: it came from a developer machine, failed every pull request, and
had to be re-measured from CI. This number came from a developer machine.

## What the measure refuses to do

Two refusals, and each one was added after it fired:

1. **A symbol the registry names and the code does not define is a refusal.** A
   measure that silently drops a renamed function reports a number for fewer
   couplings than it claims.
2. **A site that matches no statement in the profile is a refusal.** Every real
   function has statements, so zero means the profile does not cover that
   package — or it was read while it was still being written. That happened:
   `pkg/api.createNode` reported `0/0` beside thirteen real percentages,
   because the coverage job had not finished. It read as a result.

`make dccc-selftest` proves both, plus the defect that the first working
version had: matching profile blocks by package and line range without the file
counted every block at the same line numbers in every other file of the
package, and reported a 20-statement function as 634 statements.

## Not enumerated

**D4, the mmap copy-on-write overlay.** A reader materialises from the base
while a writer promotes into the shard, and no single symbol is the boundary.
Naming the wrong one would be worse than the gap, so the gap is recorded in
`couplings.tsv` instead.
