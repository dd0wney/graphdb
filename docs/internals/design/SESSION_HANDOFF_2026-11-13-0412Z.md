# Session handoff — 2026-11-13 04:12 UTC

**Date**: 2026-11-13 (single session; 16 PRs merged, none open from this session)
**Outgoing model**: Claude Fable 5.2
**Delegation**:
- `Explore` (sonnet) produced two code maps at the start. The main loop wrote each spec from them. After that the builders did their own discovery from a spec, which was faster.
- `worker` (sonnet) built 11 fixes, each test-first with a quoted red result. The main loop read every source diff. Two builders corrected a test method that the main loop had specified wrongly (a deadline test that asserted only the error, and a countdown context that cannot isolate one check).
- `worker` (haiku) built one fix (`respondJSON`, one function, complete spec). Correct on the first attempt. It also did one push-and-open-PR tail.
- `reviewer` (opus) ran 12 reviews. 10 of them found a real defect. The main loop confirmed the first two in the code and after that relied on the reviewer's quoted runs. Each review had a positive control (the same probe on the parent commit).
- The main loop did the rebases, the trial merges, the PR bodies, the merges and the coord tail. A haiku tail dispatch cost more than it saved once the context was warm.

**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The Cypher correctness run that the Relate spike started went on: 15 fixes and one seed PR merged. Two of the defects found on the way lose data and are in every release from v1.0.0 to v1.12.0 (a write through a property index ignored the rest of its `WHERE`, and the index step discarded its input rows). They are fixed on `main` in #752 and **no release carries the fix yet**.

## The two data-loss defects (read this before any release work)

Both are in `pkg/query`, both need a property index, and a reviewer reproduced both by a run on the tags `v1.12.0` and `v1.0.0`.

1. **The filter ran after the write.** `applyFilterPushdown` put a `FilterStep` back only after a `*MatchStep`. When `applyIndexSelection` had replaced the `MatchStep` with an `IndexLookupStep`, the filter went to the end of the plan: `IndexLookupStep, DeleteStep, FilterStep`. `MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p` deleted the node where `k` is 1 and answered `affected: 0`. Same for `SET`, `REMOVE`, `CREATE`, `MERGE`. Reads were correct, because the rows are built after the last step.
2. **The index step discarded its input rows.** `IndexLookupStep.Execute` built its result from the index alone. The rows of an earlier `WITH` and the `$name` parameter bindings were lost. `MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 DELETE p` deleted nodes when no `:Nope` node exists. `WHERE p.pid = 1 AND p.k > $one RETURN p` through `POST /query` returned no rows: **a wrong read for a common request shape** (an indexed equality plus one more condition with a parameter).

The fix: `applyFilterPushdown` keeps step order and names no access step type; `filterOrderViolation` (`pkg/query/optimizer_guard.go`) checks the plan after the passes and `Optimize` falls back to the unoptimized plan on a violation; the index only supplies candidate nodes and `MatchStep` binds them. The oracle `TestIndexedWrite_SameEffectWithAndWithoutIndex` compares rows, `affected` and stored nodes and edges with and without an index. The old index oracle compared rows of reads only, which is why it never saw this.

## What's done this session

Merge order on `origin/main`. All are squash merges with 10 of 10 checks green.

| PR | Title | Notes |
|---|---|---|
| #742 | docs: session handoff (previous session) | Merged on the user's word. |
| #741 | refuse an undefined variable and an unknown function | Carried over. Its `benchmark` failure was a runner event: that step benchmarks `pkg/storage`, `pkg/lsm`, `pkg/wal`, which import nothing the PR touched (`go list -test -deps`, with a control). The rerun passed. |
| #743 | match a numeric pattern property across integer and float | `MATCH (p {id: $v})` with a JSON number missed a stored integer, and `MERGE` then duplicated the node. |
| #744 | refuse an arithmetic error where the query returned null | `extractValue` (24 call sites) discarded every error. Now `evalOperand` returns `(any, error)` and carries a `RefusalError`. The three `WHERE` filters pass a refusal through in every mode. A write that precedes a refusal is kept (no rollback): recorded by a test and the changelog. |
| #745 | refuse a non-boolean operand, evaluate a bare boolean in each predicate position | From the review of #739's unreviewed fourth commit. `coerceToBool` is deleted. `evalPredicate` is the one function for a predicate position. `evalTri` errors on an unknown expression type (it was a stack overflow). |
| #747 | encode the JSON response before the status line | `respondJSON`: a value JSON cannot encode answered 200 with a cut body. |
| #746 | name the column of a bare variable without a trailing dot | Option B (change in place). `RETURN n` is `n`, `count(n)` is `COUNT(n)`. Breaks a client that read `n.`. |
| #748 | give `=` and `<>` the openCypher null rule | One condition in `equalityTri`. `WHERE p.x = null` returns no rows now. Checked against TCK `Comparison1`. The coord task text was wrong on one point (`'a' <> 1` is true, not null). |
| #750 | evaluate an `ORDER BY` expression | It was ignored in silence. Refused after an aggregate and with `DISTINCT`. Also cut sort allocations by 79 percent (the comparator built a key string per comparison). |
| #751 | list and map equality and `IN`, openCypher null rule | `equalTri` (`pkg/query/equal_tri.go`). 66 TCK rows pass at function level. A stored float32 vector compares at float32 precision. |
| #749 | stop a procedure when the query context ends | `...Context` variants in `pkg/algorithms` and `(*vector.Emeans).TrainContext`; no exported signature changed. 19 cancellation checks, each with a test that fails without it. Owner decision: no cap on `maxIterations`. |
| #753 | one total order for `ORDER BY`, `MIN`, `MAX` | `compareTotal` (`pkg/query/order_total.go`), TCK `ReturnOrderBy1` type order. Null last for `ASC`. `MIN` over a list and numbers now returns the list (TCK `Aggregation2` [12]). |
| #754 | a parameter has its value in each place; the parsed query is read-only | Grew through three reviews. Pattern parameters in `OPTIONAL MATCH`, after `WITH`, in `UNION` parts; expression parameters in later segments; list-literal parameters; a missing parameter is a 400 on every entry point; one parsed query is safe from many goroutines; `PROFILE` runs the whole query; `EXPLAIN` via `ExecuteWithText` no longer runs it. |
| #752 | run the `WHERE` filter before a write when an index selects the rows | The two data-loss defects above. Four commits, two reviews. |
| #755 | make a refusal independent of the plan and of operand order | Two rules in `evalTri` and `evalPredicate`. A reviewer enumerated every 2- and 3-operand combination: 0 violations in 1,408 cases against 720 on the parent. |
| #756 | chore(coord): seed twelve follow-up tasks | Applied: 12 created. |

## Current state

- `origin/main` HEAD: `48c13f5` (#756). Local `main` equals it.
- Open PRs: #735, #734, #711. All older than this session and not touched.
- Open branches: 15 local `worktree-agent-*` branches left by agent worktrees, plus `chore/seed-set-encryption-live-store`, `docs/spec-encryption-key-check`, `feat/encryption-key-check` (worktree `.claude/worktrees/keycheck`). None is from an unmerged fix of this session. Run the `branch-cleanup` skill.
- Uncommitted changes: none.
- Test state on `main` after the last code merge: `go build ./pkg/... ./cmd/...` and `go test ./pkg/query/ ./pkg/queryutil/ -short` pass. `pkg/query` passed 25 consecutive runs on the #754 branch and 20 on the #752 branch. The race detector ran on `pkg/query`, `pkg/algorithms`, `pkg/vector`.
- Coord: no claim held by this session. 21 tasks released, 22 seeded. No `graphdb-coord` session was live, so each seed was applied after a dry run.
- `CHANGELOG.md` `[Unreleased]` holds every behaviour change of the last two sessions. It is long. Read it before a release.

## What's next

Task state lives in coord. Ranked:

1. **Decide the patch release v1.12.1.** The two index fixes are on `main` mixed with behaviour changes that need a minor release. A patch release needs a backport of #752 onto the v1.12.0 code, a review of the backport, and a tag. Not started: it needs the owner's word (open question 1).
2. `v1.4-cypher-bound-non-node-variable-rematched`: `WITH 1 AS p MATCH (p:T) ... SET p.z = 1` binds `p` again and writes. A wrong write.
3. `v1.4-cypher-index-equality-int-float`: an int index does not hold a node whose value is the float 1.0, so an indexed write touches fewer nodes than the scan. `TestIndexedEquality_IntAgainstFloatKnownGap` records it and fails when the plans agree.
4. `v1.4-storage-create-node-keeps-caller-map`: `createLiveNodeLocked` stores the caller's properties map and labels slice with no copy and no ownership rule. Read `pkg/storage/node_operations.go:265-310` first; the v1.11.1 clone-skip work may be why.
5. `v1.4-cypher-with-order-by` (blocks 292 TCK scenarios) and `v1.4-cypher-nested-list-literal` (blocks 27). `v1.4-cypher-unwind-literal` blocks most of the rest.
6. `v1.4-cypher-tck-known-gaps-table`: make the TCK the source of `TestCypherKnownGaps`. A copy of the TCK feature files is only in the session scratchpad, so fetch it again (`gh api repos/opencypher/openCypher/contents/tck/features/...`, revision `1a5aa1031a`).
7. The rest of the previous handoff's queue that this session did not reach: `count(*)`, `count(DISTINCT x)`, a negative literal in a pattern map, named paths, `v1.4-query-error-hints`, `v1.4-query-row-limit-truncated`, `v1.4-vector-index-rebuild-on-open`, `v1.4-cypher-cycle-match-slow`.
8. Documents: `CAPABILITIES` needs the list of Cypher forms that are refused or that differ from openCypher on purpose (float division by zero is refused; string plus number is refused; list concatenation is refused). `CLAUDE.md` needs the known-gaps table convention. `NEXT_STEPS_2026-10-08.md` Track C is far behind.

Lower: `v1.4-cypher-vector-step-refusal-depends-on-plan`, `v1.4-cypher-explain-union-runs-second-part`, `v1.4-query-execute-with-text-ignores-with-and-union`, `v1.4-cypher-aggregate-in-order-by`, `v1.4-cypher-parameter-row-cost`, `v1.4-cypher-set-list-json-number-type`, `v1.4-cypher-index-uses-first-equality-only`, `v1.4-cypher-no-rollback-after-refusal` (needs a design), `v1.4-cypher-numeric-equality-above-2-53`, `v1.4-cypher-list-concatenation`.

## Stale assumptions to retire

- **`CLAUDE.md` "Orient first" and `docs/CAPABILITIES_2026-05-10.md` call `query` mature.** This session alone found more than 20 silent wrong results in Cypher, two of them data loss since v1.0.0, and the session before it found more. Treat any Cypher path with no differential test as unverified.
- **"The index is an access path, not a semantics change"** (the comment on `indexReplaceableNode` in `pkg/query/optimizer.go`) was false until #752. It is true now and `TestIndexedWrite_SameEffectWithAndWithoutIndex` holds it.
- **`extractValue` no longer exists.** Any doc, coord title or memory that names it means `evalOperand` (`pkg/query/ast_eval.go`), which returns an error. Coord task `v1.4-cypher-function-error-becomes-null` still names it: non-refusal errors of functions and `CASE` still read as null in `nestedValue`.
- **`compareValues` and `coerceToBool` no longer exist.** `compareTotal` is the sort order; `compareOrdered` is the comparison for `<` and `>`; `triFromValue` is the rule for a value used as a boolean.
- **`executeWithProfiling` no longer exists.** `PROFILE` uses the normal path with a mark on the run context.
- **The executor no longer writes into the parsed `*Query`.** `Query.Next.InitialBindings` is not written at run time; segment rows are an argument, and parameters are in the run context (`pkg/query/run_params.go`).
- **Previous handoff, open question 1** (column name A/B/C) is closed: B, in #746. **Open question 2** (`WHERE NOT n.flag`) is closed: the openCypher rule stays. **Open question 4** (review #739's fourth commit) is closed: reviewed, and #745 is the result.
- **Coord task `v1.4-cypher-three-valued-equality`** said `p.s <> 1` with a string `s` gives null. openCypher gives true; only a null operand gives null (TCK `Comparison1` [8], [9]).
- **A PR benchmark timeout is not evidence about the PR** unless the benchmarked packages import what the PR changed. Check with `go list -test -deps` first.
- **Auto-memory "session start: tiers"** says haiku for the tail. With a warm main-loop context the tail (rebase, trial merge, push, PR, merge, coord) was cheaper inline.

## Lessons that cost something this session

- **A push must depend on the test result.** A `;` between `go test` and `git push` opened #754 on a branch with a failing test. Use `&&`.
- **A test that passes 3 runs in 4 is not a gate.** `TestListParams_MissingParameterIsRefused` took the first key of a map as the expected name. It passed for the builder and three reviewers. `-count=200` found it in 4 seconds. Run a new test file with `-count=25` before a push.
- **Do not run a trial merge in a worktree an agent is using.** A `git merge --abort` there reset a reviewer's tree mid-run. Use a scratch worktree (`git worktree add --detach`).
- **"No conflict" from git is not "it compiles".** #751 and #753 each defined `asList` in different files. Build and test after every rebase, and `main` after every group of merges.
- **Give each builder a changelog position.** Every builder adds its entry at the top of the same section, so every pair of open PRs conflicts. Say "after the entry that starts with X".
- **Tell a reviewer to use its own `mktemp -d` directory.** One reviewer extracted its commit copies over another agent's scratch directories.
- **Each review round on a wide change found another defect** (#754: three rounds; #752: two). Budget for it, and stop when a round returns only pre-existing findings.

## Open questions for the user

1. **Patch release v1.12.1** with the #752 fixes only, backported to the v1.12.0 code. The session recommends it: the fix on `main` is mixed with behaviour changes, and a user of v1.12.0 cannot take it alone. Not started.
2. **A notice to users** about the two index defects. An affected write answered `affected: 0`, so no user could see it. The changelog entry of #752 says what to check; a separate advisory is the owner's call.
3. **`MIN` and `MAX` over a list.** #753 made `MIN` over a list and numbers return the list, as the TCK requires. The session flagged it and got no answer. Keep, or make `MIN`/`MAX` ignore a list as before.
4. **Parameter cost.** A `WITH` chain on 100,000 rows takes about 17 percent longer with a parameter than with a literal. The session merged #754 as it is and seeded `v1.4-cypher-parameter-row-cost`. Confirm, or rank that task up.
5. **The Relate binary** (carried over): permit the download to finish the comparison spike, or close the spike.
6. **Commit trailers.** Builders used the `Co-Authored-By: Claude Fable 5.2` line from the spec although Sonnet or Haiku wrote the code. Squash merges take the PR text, so `main` is unaffected. Say if each commit should name its own model.

## Next-session prompt (paste-ready)

```
Read docs/internals/design/SESSION_HANDOFF_2026-11-13-0412Z.md, then CHANGELOG.md [Unreleased].
Ask the user the open questions first, mainly question 1: a patch release v1.12.1 with the #752
index fixes backported to the v1.12.0 code. Two data-loss defects are in v1.0.0 to v1.12.0.
If the user says yes: branch from the v1.12.0 tag, backport #752 (163ebd7) test-first with the
oracle TestIndexedWrite_SameEffectWithAndWithoutIndex, get an opus review of the backport, then tag.
If not: pick up v1.4-cypher-bound-non-node-variable-rematched, then v1.4-cypher-index-equality-int-float.
Each fix: spec from the main loop, sonnet builds test-first with the red text quoted, the main loop
reads the diff, opus reviews a wide change with a positive control on the parent commit.
Before each push: build, vet, go test -count=25 on new tests, gofmt -s, joined with &&.
Trial merges go in a scratch worktree, never in a worktree an agent uses.
Run the branch-cleanup skill: 15 worktree-agent-* branches are left.
End via the session-handoff skill.
```

## How to use this handoff

1. Read this first, mainly "The two data-loss defects".
2. Then read `CHANGELOG.md` `[Unreleased]`. It is the most accurate record of what changed.
3. For any Cypher work, read `pkg/queryutil/cypher_known_gaps_test.go` and the oracle tests named above.
