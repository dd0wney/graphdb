# Session handoff — 2026-11-12 11:22 UTC

**Date**: 2026-11-12 (single session; 5 PRs merged, one open)
**Outgoing model**: Claude Fable 5.2
**Delegation**:
- `Explore` (sonnet) produced code maps. The main loop used them to write the specs.
- `worker` (sonnet) built each change and wrote the pull request bodies.
- `reviewer` (opus) reviewed with a numbered list of doubts. Two Opus reviews of #739 each found a regression, and the main loop confirmed both in the code. #739's fourth commit had no independent review (the user ordered the merge without a third review).
- `worker` (haiku) ran the mechanical tail: gates, push, pull request, merge, seed, release. A 30-minute CI wait is longer than one agent command can hold, so this tail needs two dispatches: one to open the pull request, one after the CI result.

**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

A comparison spike against Relate by Neo4j ran on the graphdb side only, and it found that Cypher through `POST /query` had many correctness defects. Five fixes are merged (#736 to #740), a sixth (#741) is open, and an expected-failure table (`TestCypherKnownGaps`) now records the Cypher forms that are still wrong or refused.

## Why this session happened

The user sent the Neo4j blog post that announces Relate (an open-source embedded graph database in Rust, Apache-2.0, a Neo4j Labs project, v1.0.0 released 2026-11-11, repository `relatedb/relate`) and its hackathon (Devpost, submissions close 2026-12-11 18:00 Pacific). The user chose a comparison spike.

The auto mode classifier refused the download of the Relate binary ("Code from External"). The Relate half of the spike did not run. The graphdb half ran: a throwaway probe loaded 100,000 `Person` nodes, 500,000 `KNOWS` edges and 10,000 `Doc` nodes with 64-dimension vectors, then ran 34 Cypher queries through the same path as `handleQuery`. The probe and its data are not in the repository. They are in `/var/tmp/relate-spike/` (CSV files and graphdb stores) and in the session scratchpad.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #736 | test(query): record known Cypher gaps from the Relate spike as an expected-failure table | `pkg/queryutil/cypher_known_gaps_test.go`, `TestCypherKnownGaps`, plus 19 coord tasks in `coord/seed/graphdb.json`. Each row asserts the correct behaviour. The default run skips a row that still fails and fails the suite when a row passes ("known gap is closed"). `GRAPHDB_RUN_KNOWN_GAPS=1` shows each failure. **This squash commit also contains two commits that were only on the user's local `main`:** `20eb1cf` (e-means clustering: `pkg/vector/emeans.go`, `pkg/algorithms/emeans.go`, `algo.eMeans`) and `9eef8e1` (five procedures: `detectCycles`, `scc`, `labelPropagation`, `connectedComponents`, `betweennessCentrality`). The branch started from the local `main`. The pull request body has a correction. The user decided to keep the history and reset the local `main` to `origin/main`. |
| #737 | fix(query): compute aggregates inside WITH | `executeWithChain` groups by the non-aggregate items through `groupBindings`, which `RETURN` also uses. Refusals: an aggregate with no alias, a grouping key that is not a variable and has no alias, an aggregate whose argument is not a property or a variable. The check runs for the whole `WITH` chain before the first segment. |
| #738 | fix(query): run CALL through the step executor and refuse unknown procedures | A new `CallStep`. Before, `buildExecutionPlan` dropped `Query.Call` and each `CALL` answered `affected: 1`, so no procedure was usable through `/query`. Refusals: unknown procedure, a `YIELD` name the procedure does not return, a column name that is already bound, `MATCH` after `CALL`, `CALL` after a write clause or `UNWIND`. `algo.pageRank` refuses a damping value outside 0 to 1. |
| #740 | fix(query): give parser errors the token text, the line and the column | Each parser and lexer error is a `*query.SyntaxError` with `Message`, `Token`, `Line`, `Column`. The text ends in `at line L, column C`. No raw `Token(<number>)` in an error. |
| #739 | fix(query): give comparisons and NOT, AND, OR the openCypher null rules | `compareOrdered` returns an order and an `ok` flag. An ordering comparison with a null, or between types with no common order, is unknown, and `WHERE` removes the row. `NOT`, `AND`, `OR` use three-valued logic (`pkg/query/three_valued.go`). A bare boolean property is a three-valued operand. Behaviour changes are in `CHANGELOG.md` under `[Unreleased]`: `WHERE p.nosuch >= 0` returns no rows (it returned every node), `WHERE NOT n.flag` does not return a node that has no `flag`, `WHERE n.b AND n.c` over boolean properties now works, `RETURN` of an unordered comparison gives null, and `SET n.x = <comparison>` removes the property when the comparison is null. `=` and `<>` of plain values stay two-valued. |

The merge order on `origin/main` is #736, #737, #738, #740, #739.

## Current state

- `origin/main` HEAD: `9f1d516` (#739).
- Open PRs:
  - **#741** `v1.4/cypher-refuse-undefined-names`, worktree `.claude/worktrees/agent-a4bdee851d42bef5e`. It adds a static check (`checkQueryNames`) that refuses an undefined variable and an unknown function before any step runs. It adds `range()`. It adds `query.RefusalError`, so `POST /query` answers 400 with the message for a refused query. Before, it answered 500 "query execution failed", which also hid the refusals of #737 and #738. State recorded at 2026-11-12 11:21 UTC: `state` OPEN, `mergeStateStatus` UNSTABLE, 9 of 10 checks SUCCESS, and the `benchmark` check (workflow "Performance Benchmarks") IN_PROGRESS since 10:26:58Z. The cause of the long run is not known. The new check adds about 1 microsecond per query (`RETURN 1` went from 1,559 ns to 1,822 ns in a throwaway benchmark).
  - #735 (`chore(coord): seed v1.4-set-encryption-live-store`), #734 (`docs(design): spec an encryption key check for the data directory`) and #711 (dependabot gosec bump) are older and were not touched this session.
- Merge condition for #741: 10 checks with SUCCESS and state CLEAN. Steps after the merge:
  1. Delete the remote branch.
  2. `git pull --ff-only` on `main`, then check with `git status -sb`.
  3. `COORD_PROJECT=graphdb coord seed --seed-file coord/seed/graphdb.json`. It must create 9 tasks.
  4. `coord release graphdb:<id> --pr 741` for `v1.4-cypher-return-undefined-variable`, `v1.4-cypher-with-undefined-variable`, `v1.4-cypher-unknown-function-null`, `v1.4-cypher-range-returns-null`.
- Coord, read at 11:21 UTC:
  - `--track v1.4-cypher`: graphdb has 41 tasks in total (all tracks): 14 done, 5 in progress, 22 pending. Active claims: 5, all held by this session (`claude:096038e7-03ed-47c0-9cab-d70c7ee270f5`): `v1.4-cypher-with-alias-column-name` (6 h 01 min old, not started) and the four tasks of #741.
  - `--track v1.4-query`: 4 tasks: 1 done (`v1.4-query-parser-error-position`), 3 pending (`v1.4-query-error-hints`, `v1.4-query-row-limit-truncated`, `v1.4-query-sanitizer-skip-literals`). Active claims: 0.
  - This session seeded 37 tasks (19 in #736, 4 in #737, 6 in #738, 8 in #739) and released 6. The 9 tasks in the seed of #741 are not applied until it merges.
  - No `graphdb-coord` session was live, so this session applied the seeds after a dry run. This clone has no `.git/hooks/post-checkout`, so claims are made by hand.
- Uncommitted changes: none.
- The throwaway probe and its data are outside the repository (see above).

## What's next

Ranked. Task state lives in coord. `docs/NEXT_STEPS_2026-10-08.md` has the older ranking.

1. Merge #741 when green and do the steps above. If the `benchmark` check failed or is still in progress, say so and say that the cause is not known.
2. The column name with the trailing dot (`v1.4-cypher-with-alias-column-name`): `RETURN n` names its column `n.` and `RETURN count(n)` names it `COUNT(n.)`. The source is `buildColumnName` in `pkg/query/executor_results.go`, and `ComputeAggregates` in `pkg/query/aggregation.go` holds a second copy of the format. A code map is done. This waits for the user's decision (open question 1).
3. Arithmetic errors (`v1.4-cypher-arithmetic-error-null`): `1 / 0` and a string plus a number give null. The user decided to refuse both.
4. Refused Cypher forms, one task each: `count(*)`, `count(DISTINCT x)`, `UNWIND` of a list literal, a negative number in a pattern property map, a named path with `shortestPath`, and subqueries with the list comprehension (needs a design first).
5. `v1.4-query-error-hints` and `v1.4-query-row-limit-truncated` (the row limit stays off by default, decided by the user).
6. `v1.4-vector-index-rebuild-on-open`: an mmap store with 10,000 vectors of 64 dimensions took 11.7 s to 17.3 s to open (4 runs, btrfs on NVMe, warm cache) and 2 ms without the vectors. Measure open time against vector count first.
7. `v1.4-cypher-cycle-match-slow`: a three-hop cycle match on one start node took 17.8 s on 100,000 nodes and 500,000 edges. Profile first.
8. Follow-ups from the reviews that can lose or duplicate data. Rank these high:
   - `v1.4-cypher-pattern-map-float-parameter`: `MATCH (p {id: $v})` with a JSON number does not find an integer property, and `MERGE` then makes a second node.
   - `v1.4-cypher-three-valued-equality`
   - `v1.4-api-respond-json-status-before-encode`
   - `v1.4-procedures-ignore-context`
9. The Relate half of the spike: blocked until the user permits the binary.

## Stale assumptions to retire

- `CLAUDE.md` (Orient first) points at `docs/CAPABILITIES_2026-05-10.md`, which lists `query` as mature. Before this session no procedure was usable through Cypher `CALL`, aggregates in `WITH` were not computed, and `>=` with a null property passed the filter. `CAPABILITIES` needs a list of the Cypher forms that are still refused. The remaining rows of `TestCypherKnownGaps` are that list.
- `docs/NEXT_STEPS_2026-10-08.md` Track C lists `v1.4-cypher-with-aggregates` and `v1.4-cypher-with-undefined-variable` as open. The first is done (#737). The second is in #741.
- Commit `9eef8e1` ("expose detectCycles, scc, ... Cypher procedures") was not usable when it was written. `CALL` did not run until #738.
- A new convention exists and is not in `CLAUDE.md`: the expected-failure table in `pkg/queryutil/cypher_known_gaps_test.go`. The pull request that closes a gap deletes its row and adds real tests.
- `git pull --ff-only` prints "Already up to date" when the local branch is ahead of `origin`. It is not a check for equality. Use `git status -sb`.
- The REST layer hid every executor error as 500 until #741.

## Open questions for the user

1. **The column name with the trailing dot.** Options:
   - A: each row carries the new key and the old key until v2.0. The user first approved this, but it doubles the payload of `MATCH (n) RETURN n`, because `decodeResultRow` in `pkg/api` encodes each key.
   - B: change the name in the next minor release with a changelog entry. The session recommends this: `graphdb-coord` does not call `/query`, and no consumer contract fixes a column name.
   - C: two keys in the library and one key in REST.

   The user did not answer.
2. `WHERE NOT n.flag` no longer returns a node that has no `flag` property (#739, the openCypher rule). The user was asked if the old result must be kept and did not answer. The merge was ordered.
3. The Relate binary: permit the download to complete the spike, or stop with the documentation comparison.
4. #739's fourth commit (the bare boolean operand in `NOT`, `AND`, `OR`) had no independent review. Decide whether to review it now.

## How the session divided the work

Sonnet `Explore` for code maps, Fable for the spec and the decisions, Sonnet for the build and the pull request body, Opus for the review with a numbered list of doubts, Haiku for the mechanical tail. The user said twice to use each tier.

## Next-session prompt (paste-ready)

```
Read docs/internals/design/SESSION_HANDOFF_2026-11-12-1122Z.md, then docs/NEXT_STEPS_2026-10-08.md.
Check #741 with gh pr view 741 --json state,mergeStateStatus,statusCheckRollup. If it is green, merge it
and do the post-merge steps in the handoff (branch, pull, coord seed, four releases).
Ask the user the four open questions first (column name, NOT n.flag, Relate binary, review of #739's fourth commit).
Then pick up v1.4-cypher-with-alias-column-name once the user decides, or v1.4-cypher-arithmetic-error-null.
A fix closes a row of TestCypherKnownGaps: delete the row and add real tests, red-first.
Use sonnet Explore for maps, a spec from the main loop, sonnet to build, opus to review, haiku for the tail.
End via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then read `docs/NEXT_STEPS_2026-10-08.md`.
3. For any Cypher work, read `pkg/queryutil/cypher_known_gaps_test.go`.
