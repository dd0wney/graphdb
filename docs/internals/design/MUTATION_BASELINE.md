# Mutation testing baseline

**Living document.** Update it in the same PR as any change that moves a number.

**Measured**: 2026-08-28, against `main` at `c39efab` plus the concurrency work.

## Why this regime was added

Coverage says which lines ran. It does not say which lines anything checked.

On 2026-08-28 five tests in this repository ran the code they claimed to test
and constrained nothing:

| Test | Why it passed | Found by |
|---|---|---|
| The partial-SSTable test | It faulted the flush's `open`, so no file was ever created to leak | Reverting the repair |
| The record-cache scenario | Its reader started after the writes, so no cached value could go stale | Reverting the repair |
| `contract-guard` locale case | It compared the ambient locale with a UTF-8 one, and the ambient one was already UTF-8 | Reverting the repair |
| `lint-local` errcheck case, twice | It used `os.Remove`, which an exclusion preset covers | Reverting the repair |

Each was found by taking the repair out and watching the test stay green. That
is mutation testing performed by hand, one mutant at a time, and only for the
lines somebody thought to check.

## Baselines

Run with `make mutation`. Configuration and its rationale: `.gremlins.yaml`.

| Package | Killed | Lived | Not covered | Timed out | Efficacy | Mutator coverage | Wall clock |
|---|---|---|---|---|---|---|---|
| `pkg/vfs/vfstest` | 55 | 30 | 37 | 3 | 64.71% | 69.67% | 13s at 2 workers |
| `pkg/lsm` | 285 | 83 | 27 | 6 | 77.45% | 93.16% | **2m29s at 4 workers** |

**The `pkg/lsm` row was measured at four workers and must be re-measured at
two.** Four workers on that package reached 669% CPU and exhausted this
machine's memory into swap, which is why the configured default is now two. The
numbers themselves do not depend on the worker count; the wall clock does.

## No threshold yet, deliberately

A floor set before the number is understood is how the coverage floor was got
wrong in #469: it came from a developer machine, failed every pull request, and
had to be re-measured from CI.

These numbers were measured on a developer machine. A threshold goes in when
they have been measured where it will be enforced.

## What the survivors say

Not every surviving mutant is a missing test. Some are equivalent mutants, and
some are in timing code that no assertion should pin. These are the ones that
are neither, from the first run:

- **`sweep_role.go:44`** — the `n > maxOps` guard. `SweepRole`'s documentation
  says it "fails, rather than passing, on reaching `maxOps`", and no test makes
  it reach `maxOps`. The failure path is documented and unexercised.
- **`sweep_role.go:37`** — the `maxOps <= 0` default of 512. Never exercised.
- **`role.go:346`, `role.go:348`** — the jitter gate, `nap >= 0` and `nap > 0`.
  The sentinel that distinguishes "jitter off" from "a zero-length nap" has no
  test, which is exactly the distinction a comment in that file argues for.
- **`explore.go:39`** — the `seed < seeds` boundary.

The rest of `pkg/vfs/vfstest`'s survivors are in `crash.go` and `faults.go`,
which predate this work.

## How to use this

1. Moving a number means editing this file in the same PR.
2. A survivor that turns into a test goes in the section above, struck through
   rather than deleted — the point is which lines were unconstrained, not the
   final score.
3. `make mutation-selftest` proves the tool can report both outcomes. Run it
   before trusting any score: gremlins with a short timeout reports
   "Test efficacy: 100.00%" from a single evaluated mutant out of 88.
