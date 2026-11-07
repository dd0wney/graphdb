# graphdb rules for snare

[snare](https://github.com/dd0wney/snare) is a graph-native static analyser. The rules here
encode defect classes graphdb has already shipped and fixed, so each rule has a known positive
control: the commit before its fix. They live in this repo, not in snare's rule packs, because
they depend on graphdb's history.

Run them with `SNARE=/path/to/snare make snare`. `make snare-selftest` proves the scan can
report a finding, a clean tree, and a scan that did not run. Neither target is a CI gate yet.

`scripts/snare-scan.sh` exits 0 with no finding, 1 with findings, and 2 when the scan did not run
or did not finish. `_test.go` files are left out; test-support packages such as
`pkg/vfs/vfstest` are scanned.

## Rules

### `lock-call-without-defer.star` — `graphdb-lock-call-without-defer`

A mutex section that calls a function between `Lock` and an explicit `Unlock`. A panic in the
call leaves the lock held; the Cypher executor and `net/http` recover panics, so every later
caller then blocks. #665 moved such sections to `defer`.

Written by the snare session on 2026-11-07 and measured against `git archive` copies of `pkg/`
with `_test.go` files removed, compared by function name:

| Revision | Functions flagged | Of the 20 functions #665 fixed |
|---|---|---|
| `b6fb6a4^` (before #665) | 37 | 18 flagged |
| `b9bc9d2` (main) | 20 | 0 flagged |

The two misses (`CreateNodesWithTenant`, `CreateEdgesWithTenant`) call inside a `for` loop
between `Lock` and `Unlock`. The rule sees only a call that is a statement in the same block as
the `Lock`; a call in a nested block or inside a larger expression is a known limit.

The 20 functions flagged on main are not yet labelled real or intentional. Nine are in
`pkg/vfs/vfstest`.
