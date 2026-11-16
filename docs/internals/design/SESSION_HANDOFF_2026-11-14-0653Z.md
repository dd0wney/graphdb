# Session handoff — 2026-11-14 06:53 UTC

**Date**: 2026-11-14 (a continuation of the session of 2026-11-13; 5 PRs merged, one patch release, one advisory)
**Outgoing model**: Claude Fable 5.2
**Delegation**:
- `reviewer` on Opus, six rounds on the v1.12.1 backport (#758). Every round found something real: a parameter defect after `WITH`, three more in `UNION` and later segments, a defect that my own third commit caused, two more pre-existing delete paths, and one regression in the fifth commit. The main loop used each report after reading the cited code. The reason for Opus was stated in the prompt: a data-loss fix on a code base that lacks 14 intermediate fixes.
- `worker` on Sonnet built the TCK harness (#759) in three passes. The main loop found that 128 of its 204 "pass" outcomes were error scenarios passing for a wrong reason (a weakness of the main loop's specification) and sent it back.
- `reviewer` on Sonnet reviewed the harness with 25 mutations; 11 survived, all closed in the third pass.
- `worker` on Sonnet built commits four and five of #758 from a main-loop specification. Used after reading the diff.
- No Haiku dispatch. The mechanical tails were short enough to do inline.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

v1.12.1 is released from a new branch `release/v1.12.x`, with advisory GHSA-fjm7-wf5c-2jhh: Cypher writes in v1.0.0 to v1.12.0 could delete or change data the query did not select. `main` now has an openCypher TCK subset as a measured oracle (76 of 619 scenarios pass with rows compared).

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #757 | docs(handoff): session handoff for the second Cypher correctness run | Merged by the user. |
| #759 | test(query): measure the Cypher parser and executor with an openCypher TCK subset | 34 feature files vendored (Apache 2.0, `THIRD_PARTY_NOTICES.md`). Golden file `pkg/queryutil/testdata/tck_baseline.tsv`; a change of any outcome in either direction fails `TestTCKBaseline`. Closes coord `v1.4-cypher-tck-known-gaps-table`. |
| #758 | fix(query): index writes honour WHERE, and a parameter has its value in each part of a query | **Base is `release/v1.12.x`, not `main`.** Seven commits, squashed to `2f8a8ad`. Commit 1 is the cherry-pick of #752. Commits 2 to 6 are parameter corrections that exist only on the release branch; `main` solves the same defects differently (#741, #754). |
| #760 | test(query): prove that a missing parameter is refused and writes nothing through each entry point | Tests only. Proven by switching the check off: 228 executor and 34 HTTP subtests failed. |
| #761 | chore(release): v1.12.1 changelog section, release pointers and chart appVersion | Merged while GitHub reported `mergeStateStatus` UNKNOWN straight after #760 merged; 12 of 12 checks were green on its head and the file sets are disjoint. |

Also done, not a PR:
- Tag `v1.12.1` on `2f8a8ad`. `Release` and `Docker Publish` succeeded; 11 assets, 6 signatures.
- Release notes carry the data-loss notice above the generated changelog.
- Advisory GHSA-fjm7-wf5c-2jhh published: Go module `github.com/dd0wney/graphdb`, `>= 1.0.0, <= 1.12.0`, patched `1.12.1`, severity High, no CVE requested. The user did not answer the severity and CVE question; the values are the main loop's proposal.

## Current state

- `origin/main` HEAD: `b2e8b52`.
- `origin/release/v1.12.x` HEAD: `2f8a8ad` (= tag `v1.12.1`). Keep this branch; it is the base of the 1.12 patch line.
- Open PRs: #735, #734, #711 (older, untouched), and this handoff.
- Local branches that should go: about 16 `worktree-agent-*`, `test/tck-harness`, `backport/v1.12.1-indexed-write` (remote deleted; a scratch worktree holds it). Run the `branch-cleanup` skill.
- Uncommitted changes: none.
- No coord claim held by this session.
- Gates on `main`: the 10 CI checks were green on #759, #760 and #761.

## What's next

1. **v1.13.0 from `main`.** Not started; needs the user's word. `main` holds 15 Cypher fixes with behaviour changes (new refusals, a total sort order, new column names, float division refused). It closes the first two "Not corrected" items of v1.12.1 — verified on `main` at `1042b72`: 384 of 416 executor runs and 40 of 40 HTTP runs with a missing parameter are refused with the store unchanged (the 32 others are `EXPLAIN`, which runs nothing, and an unknown function refused earlier).
2. **Parser gaps, now measured.** 376 of 619 TCK scenarios do not parse. The main causes: `UNWIND` of a literal list, a list inside a list (`v1.4-cypher-nested-list-literal`), a negative number in a list, a map literal in `RETURN`, `WITH ... ORDER BY` (`v1.4-cypher-with-order-by`). A fix moves rows of the golden file; update it with `GRAPHDB_TCK_UPDATE=1`.
3. **Product defects the 37 `fail` rows show**: list comparison with a null element, `percentileDisc`/`percentileCont`/`tail` missing, `+` on lists, an aggregate inside an `ORDER BY` expression, upper-case aggregate column names (`COUNT(r)`), `IN` on a non-list and `range()` with a wrong type not refused. None has a coord task yet.
4. The 11 tasks seeded by #756 that are still pending (for example `v1.4-cypher-bound-non-node-variable-rematched`, `v1.4-storage-create-node-keeps-caller-map`, `v1.4-cypher-parameter-row-cost`).

New gaps found this session, not yet in coord:
- `EXPLAIN` of a query with a missing parameter returns a plan and does not warn that the real run is refused.
- On `main`, a refused later segment: check that no earlier write stays (the probe saw none for one case; v1.12.1 has the defect).
- The SanitizeQuery line join lets a `//` comment swallow the rest of a query (TCK WithOrderBy4 [1], [2]).

## Stale assumptions to retire

- `docs/internals/design/SESSION_HANDOFF_2026-11-13-0412Z.md`, open question 1 ("a patch release v1.12.1 with the #752 fixes only"): done, but not "#752 only". The backport needed five more commits, because v1.12.0 has parameter defects that #752's own tests do not reach.
- Same file, "No release has the fix": v1.12.1 has it.
- Any note that says "the fix for the index defects is a clean cherry-pick": the text applies cleanly, the release was not safe with it alone.
- `NEXT_STEPS_2026-10-08.md` does not mention the TCK oracle, the release branch or v1.12.1. It needs a planning update.
- `CLAUDE.md` has no entry for `release/v1.12.x` or for the TCK baseline convention. Both deserve two lines.
- Auto-memory `graphdb-merge-on-green-standing-instruction`: still correct. This session added one data point: the user gave an explicit conditional instruction for the tag ("tag v1.12.1 when the review is clean and #758 is merged").

## Open questions for the user

1. v1.13.0: release `main` now, or after more parser work?
2. The advisory: keep severity High and no CVE, or change?
3. Should the two remaining v1.12.1 defects that `main` does not obviously cover (a refused later part keeps earlier writes; a parsed query run twice keeps a pattern-map value) get coord tasks after a probe on `main`?
4. Vendor the full TCK (needs a download, so it needs permission), or stay with the 34 files?
5. Carried over: the Relate binary download; commit trailers naming the actual model.

## Lessons worth keeping

- A count or a test can be green for a wrong reason. Twice this session the question "why did it pass?" found the problem (error scenarios passing because the parser refuses the clause; `CALL` cases passing because v1.12.0 ignores `CALL`).
- "The store did not change" needs a store that can change. An HTTP probe reported 40 of 40 on an empty store.
- For a backport, the review question is "does the fix depend on behaviour the old code lacks", and the instrument is a differential oracle over the full store, run on both trees.
