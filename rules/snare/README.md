# graphdb rules for snare

[snare](https://github.com/dd0wney/snare) is a graph-native static analyser. The rules here
encode defect classes graphdb has already shipped and fixed, so each rule has a known positive
control: the commit before its fix. They live in this repo, not in snare's rule packs, because
they depend on graphdb's history.

Run them with `SNARE=/path/to/snare make snare`. The rules need snare `0895313` or later: `snare.lockorder` and `--exclude` arrived there, and an older binary exits 2. `make snare-selftest` proves the scan can
report a finding, a clean tree, and a scan that did not run. Neither target is a CI gate yet.

`scripts/snare-scan.sh` exits 0 with no finding, 1 with findings, and 2 when the scan did not run
or did not finish. `_test.go` files are left out; test-support packages such as
`pkg/vfs/vfstest` are scanned.

## Rules

Rules are under `go/`, the `<rules>/<language>/` layout that `snare bench` expects.

### `go/lock-call-without-defer.star` — `graphdb-lock-call-without-defer`

A mutex section that calls a function between `Lock` and an explicit `Unlock`. A panic in the
call leaves the lock held; the Cypher executor and `net/http` recover panics, so every later
caller then blocks. #665 moved such sections to `defer`.

Written by the snare session on 2026-11-07 and measured against `git archive` copies of `pkg/`
with `_test.go` files removed, compared by function name:

| Revision | Functions flagged | Of the 20 functions #665 fixed |
|---|---|---|
| `b6fb6a4^` (before #665) | 35 | 17 flagged |
| `b9bc9d2` (main) | 19 | 0 flagged |

These numbers are for the current rule, which does not count a `Lock`, `Unlock`, `RLock` or
`RUnlock` as the call. The first version counted them, and it flagged 37 and 20 functions and 18
of the 20. One of those 18, `Transaction.Commit`, matched only because an `Unlock` counted as
the call: its real calls are inside `if` and `for` blocks.

The three misses (`CreateNodesWithTenant`, `CreateEdgesWithTenant`, `Transaction.Commit`) all
call inside a nested block between `Lock` and `Unlock`. The rule sees only a call that is a statement in the same block as
the `Lock`; a call in a nested block or inside a larger expression is a known limit.

#### Labels on main (`b9bc9d2`, 21 findings in 20 functions, 2026-11-07)

Each finding was labelled by reading the call and its callee. **None is a real hazard**, so on
main the rule is a regression gate, not a defect finder:

| Label | Findings | Notes |
|---|---|---|
| Safe | 10 | The call cannot panic in practice: an atomic add, a field assignment, a map read, a `close` behind `sync.Once`, or a constructor before the store is shared. |
| Test support, safe | 9 | `pkg/vfs/vfstest`, which only `_test.go` files import. |
| Rule false positive | 2 | `lsm.go:229` counts `lsm.mu.RLock` of a second mutex as the call; `lsm_workers.go:56` pairs a `Lock` with an `Unlock` from a later section. |

The current rule no longer reports the two false positives, so it flags 19 findings on main, and
none of them is a real hazard.

Three `VectorIndex` findings (`AddVectorForTenant`, `SearchForTenant`, `RemoveVectorForTenant`)
release the read lock before the slow HNSW call on purpose. A `defer` there would hold the lock
across the search.

Rule defects reported to snare: a `Lock`/`Unlock` call counted as the call (fixed here); the
`...` gaps let a `Lock` pair with an `Unlock` from a later section; the reported line is the
start of the block, not the `Lock`; a call in a nested block is not seen.
