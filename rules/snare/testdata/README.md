# Snare benchmark corpus from graphdb defects

This directory is a labelled corpus for the [snare](https://github.com/dd0wney/snare) static
analyser. It follows the layout in snare's `docs/BENCHMARK.md`: `<language>/{vulnerable,safe}/`.
Each fixture is one `.go` file with `package fixture`. A fixture parses and passes `gofmt`.
A fixture does not compile. The Go tool ignores a directory named `testdata`, so the fixtures
never join `go build ./...`.

The code is copied verbatim from graphdb commits with `git show <commit>:<path>`. The header of
each fixture names the source file, the commit, the line range, the label and the reason. Set 2
adds the type declarations that the functions need. Set 3 adds the helper `respondError` from
`pkg/api/server_helpers.go` so that the sink resolves inside the fixture.

## Layout and naming

Each basename is unique across the corpus, because snare keys a finding by `<language>/<basename>`.

| Prefix | Directory | Meaning |
|---|---|---|
| `lock665_` | `vulnerable/` | A function from `b6fb6a4^`, before PR #665. |
| `lock665fixed_` | `safe/` | The same function at `b6fb6a4`, after PR #665. |
| `lock672_` | `vulnerable/` | `snapshotWithBoundary` at `b6fb6a4`. Its `gs.mu.RLock` section is the hazard that PR #672 (`d08c106`) fixed. |
| `lock672fixed_` | `safe/` | `snapshotWithBoundary`, `captureJSONSnapshot` and `captureMmapSnapshot` at `d08c106`. |
| `lock665helper_` | `safe/` | A helper that PR #665 added to hold the lock with a deferred unlock. |
| `lockmain_` | `safe/` | A function from `b9bc9d2` that a human labelled as not a hazard. |
| `lockorder_` and `lockorderfixed_` | `vulnerable/` and `safe/` | Set 2. |
| `errtext706_`, `errtext706fixed_` and `errtextlive_` | `vulnerable/` and `safe/` | Set 3. |

## Sets

### Set 1: `graphdb-lock-call-without-defer` (a rule exists)

- Vulnerable: 30 functions from `b6fb6a4^` that PR #665 (commit `b6fb6a4`) changed from an explicit
  unlock after a call to a deferred unlock. There are 39 annotations, one for each changed lock
  section. The annotation is above the line that takes the lock.
  - 20 functions use a mutex directly (`mu.Lock`, `mu.RLock` or `txWALBarrier.RLock`). This is the
    count of the snare author.
  - 10 more functions take a shard lock through the wrappers `lockShard` or `rlockShard`.
    PR #665 fixed these too. The rule cannot see a wrapper call, so these are known misses.
- Vulnerable, PR #672: `lock672_GraphStorage_snapshotWithBoundary.go` has one annotation, on the `gs.mu.RLock` line. The `gs.mu.Lock` section is not annotated because PR #665 already fixed it. `snapshotWithBoundary` is also in the PR #665 pair. Both pairs stay.
- Safe, fixed code: 29 of the same functions at `b6fb6a4`. `snapshotWithBoundary` has no `lock665fixed_` file, because that version still has the PR #672 hazard. 29 helpers that PR #665 added are also safe
  fixtures. They are the code that now holds each lock with `defer`.
- Safe, hard negatives: 20 functions from `b9bc9d2` with 21 findings that a human labelled. None is a
  real hazard. `LSMStorage.flush` carries two labelled findings (`lsm_workers.go:56` and `:82`),
  so it is one fixture. The labels are in `labels-main-b9bc9d2.tsv` in this directory.

### Set 2: `graphdb-lock-order-inversion` (no rule yet)

`lockorder_LSMStorage_stats.go` has `LSMStorage.GetStats`, `flush` and `compact` from `b9bc9d2`.
`GetStats` takes `lsm.stats.mu` and then `lsm.mu`. `flush` and `compact` take `lsm.mu` and then
`lsm.stats.mu`. The annotation is on the second lock in `GetStats`.
`lockorderfixed_LSMStorage_stats.go` has the same three functions from commit `0d1c650` on
`origin/fix/lsm-getstats-lock-order`.

### Set 3: `graphdb-error-text-in-response` (no rule yet)

- `errtext706_registerUniquenessRule.go`: the handler from `8c9f42e^`, before PR #706. It sends
  `err.Error()` of a `RegisterUniquenessRule` failure to the client. The text of a failed
  `rules.json` write carries the data-directory path.
- `errtext706fixed_registerUniquenessRule.go`: the handler at `8c9f42e` with the helper
  `registerRuleErrorResponse`, and `sanitizeError`.
- `errtextlive_handleBatchNodes.go`: `handleBatchNodes` and `batchCreateItemError` at `b9bc9d2`. A WAL
  wait failure returns an error that wraps `ErrWALWriteFailed` and the disk error (see
  `waitWALPending` in `pkg/storage/persistence_wal.go`). `batchCreateItemError` has no case for it
  and returns `err.Error()`. The handler puts that text in the per-item `errors` of the response.

## How the labels were made

The labels come from the git history, not from snare output.

- Set 1 vulnerable: `git diff b6fb6a4^ b6fb6a4 -- pkg/` plus a Go AST scan that lists each function
  whose count of explicit unlock statements falls, then a read of each function. The annotation goes
  on the lock line of each section that the commit changed, whatever line snare reports.
- Set 1 hard negatives (labels in `labels-main-b9bc9d2.tsv`): each finding on `b9bc9d2` was labelled by reading the call and its callee.
- Sets 2 and 3: the defect and the fix commit are named in the commit history.

Snare reports a finding at the first line of the matched block. For a function that is the `func`
line. The annotation is on the lock line. The two differ, so the line-exact score for set 1 is zero.
That difference is a result of the benchmark. It is not a change of label.

## Run

```bash
/path/to/snare bench rules/snare/testdata --rules rules/snare --format json
```

## Licence

Copied from graphdb (MIT licence, see LICENSE at the repository root). Each fixture carries the
same notice in its header.
