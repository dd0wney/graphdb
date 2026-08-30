# Coupling coverage baseline

**Living document.** Update it in the same PR as any change that moves a number.

**Measured**: 2026-08-28, `go test -short ./pkg/...` on a developer machine.
Re-measured after #500, which closed the two sites that were at zero.

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
| C4 | `pkg/storage.GraphStorage.CreateNodeWithTenant` | 20/20, 100.0% |
| C5 | `pkg/lsm.flush` | 84/91, 92.3% |
| C5 | `pkg/lsm.compact` | 52/55, 94.5% |
| D1 | `pkg/storage.openMmapSnapshot` | 26/33, 78.8% |
| D1 | `pkg/storage.sections` | 25/31, 80.6% |
| D2 | `pkg/wal.writeEntryTo` | 8/14, 57.1% |
| D2 | `pkg/wal.readEntry` | 28/28, 100.0% |
| D3 | `pkg/storage.CheckInvariants` | 227/242, 93.8% |
| D5 | `pkg/lsm.lookupEntry` | 8/10, 80.0% |
| D6 | `pkg/vfs.Resolve` | 6/6, 100.0% |
| D6 | `pkg/alloc.Bytes` | 19/20, 95.0% |

**563/616 statements = 91.4%.**

It was 538/616, 87.3%, when the measure was first written. The difference is
#500 and nothing else.

## Re-measured 2026-08-30 (`ef77e04`): 595/640 = 93.0% over 17 sites

Run with `make dccc-cover` then `make dccc`, both exit 0, after the
2026-08-30 fix wave landed.

**Most of the movement from 576/628 is a repair, not a gain.** Row C4 was
resolving to the wrong function. `scripts/dccc.sh` took the first file in the
package that declared the symbol and stopped, and `pkg/storage` declares
`CreateNodeWithTenant` twice — on `BTreeGraphStorage` in `btree_storage.go`
and on `GraphStorage` in `node_operations.go`. `btree_storage.go` sorts first,
so the row measured the BTree body, which contains no vector code, while the
row's note says node creation drives vector index maintenance.

So the old total carried a wrong numerator AND a wrong denominator for that
row: 12/14 against a body that was not the one named. #519 made the resolver
refuse an ambiguous symbol rather than pick one, and qualified the row as
`GraphStorage.CreateNodeWithTenant`. The floor moved 14 -> 20 because the
correct body is larger.

Found by the `github.com/dd0wney/fault` session, which read this repository's
coupling data and reported the mismatch.

**The rest did not move at all.** Six further pull requests landed between
#519's measurement and this one — two storage fixes, an api fix, a server
fix, a Close fix and a test-only addition — and the numerator and denominator
are unchanged. None of this session's changes touched one of the 17 named
functions. The measure was stable because it was measuring elsewhere, which
is a fair statement of both its value and its limit.

## The denominator is the claim, not the ratio

A stable number has two causes and they render identically.

Nineteen pull requests landed between #519's measurement and the 2026-08-30
re-measure without moving 595/640. The section above concludes that none of
them touched one of the 17 named functions. That is one explanation. The other
is that one of them CREATED a coupling site and no row exists for it.

Both print 595/640. In the second case the gate stays green while the measured
fraction of the real coupling surface gets smaller, and nothing in this
measurement can tell the two apart.

The cause is structural rather than a defect in the script. Coupling coverage
in the DO-178C 6.4.4.d sense measures the sites you DECLARED. `couplings.tsv`
is an INPUT. So the measure cannot report a coupling site that nobody wrote a
row for, and it cannot report that one is missing.

This is the same shape as every other instrument defect this repository has
found: a grep that printed nothing because it crashed on an unquoted glob, a
metamorphic test that passed while asserting nothing, a mutation run that
scored 1.00 because it produced no mutants, and #519's own resolver, which
picked the first of two matching symbols and measured the wrong function. A
gate reports on what it examined, and the dangerous case is always the one
where "examined nothing" and "found nothing wrong" are indistinguishable.

**What would close it** is a registry completeness check rather than a
coverage number — something that can say "this call crosses a package boundary
and has no row". Not built. Whether it is worth building for 17 sites is a
judgement, but the number should not be quoted outside this project until it
exists, because the denominator is the claim and only the ratio is measured.

Raised by the `github.com/dd0wney/fault` session, 2026-08-30, in response to
the re-measure above.

## Superseded: 2026-08-29 (#507): 576/628 = 91.7% over 17 sites

Three things moved, and only one of them is a coverage change.

1. **A coupling was hollowed out and the measure did not notice.** ADR 0002
   stage 4 extracted `openMmapSnapshot`'s body into `openMmapSnapshotWithFS`.
   The registered symbol still resolved and still reported coverage, of the one
   line left behind: D1 went **26/33 (78.8%) → 1/1 (100.0%)**, measured on the
   same profile with only the code changed. The score improved because the
   measure lost its subject. `couplings.tsv` now carries a statement-count
   floor per site and `dccc.sh` exits 3 below it.
2. **Three new sites.** C6 `pkg/vfs.MapFile` (6/6), D7
   `pkg/storage.writeFileWithFS` (5/7) and `readFileWithFS` (6/6) — the
   boundaries stage 4 created. `pkg/storage` had no driver coupling before it.
3. **The measure got its own profile.** It borrowed `COVER_PKGS`, which omits
   `pkg/api` by design, so 4 of 17 sites reported UNMATCHED. `make dccc-cover`
   covers every package the registry names.

616 → 628 reconciles exactly, and neither term is a coverage change:

    616  the 2026-08-28 total
     -7  the reader itself got smaller — one vfs.MapFile call replaced
         os.Open + f.Stat + syscall.Mmap, so D1 is 26 statements, not 33
    +19  three new sites: C6 (6), D7 writeFileWithFS (7), D7 readFileWithFS (6)
    ---
    628

Coverage of the boundaries is flat. What changed is how much of the boundary
the registry can see, and whether it can tell when that stops being true.

## ~~The two at zero~~ — closed by #500

This section is struck through rather than deleted. "How to use this" below
says why: the point of a baseline is which lines were unconstrained, not the
final score, and the reasoning here is the argument for the measure existing at
all.

~~**`pkg/vfs.Resolve` has no test at all**~~, and its own doc comment says why
that mattered:

> An unknown name is an error rather than a silent fallback, because falling
> back would run a test against the real filesystem while the test believed it
> had installed a fault driver — a pass that means nothing.

The function that existed to prevent a meaningless pass was the one nothing
exercised. `pkg/vfs` had no `_test.go` files; its drivers are tested from
`pkg/vfs/vfstest`, which never calls `Resolve`.

**`pkg/alloc.Bytes` was the same shape.** `pkg/alloc` had no package-level
tests; `alloctest` covers the fault driver and not the production entry point.

Both are D6, the process-wide installed drivers, and D6 is the row of the
coupling table whose evidence column read "the drivers' own tests" — which was
the problem stated exactly.

**#500 closed both.** `pkg/vfs` went from 0% to 74.3% and `pkg/alloc` from 0% to
96.9%. The measure found them; nothing else had.

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
