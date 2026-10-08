# Session handoff — 2026-10-08 06:45 UTC

**Date**: 2026-10-08 (same session as `SESSION_HANDOFF_2026-10-08-0250Z.md`, continued; 13 more PRs merged, two releases cut that day in total)
**Outgoing model**: Claude Opus 5.5
**Delegation** (since the 02:50 handoff):
- 4 × reviewer (sonnet): #650 high, #655 low, #657 high, #659 high. Every report was re-checked in code; #657's review produced three real fixes (WHERE placement, OPTIONAL/MERGE ordering refusals, null and variable-length bound checks) and #659's found the CREATE direction defect.
- 1 × security (opus): encryption at rest for mapper, probes in a separate worktree. Used; two claims re-checked in code.
- 1 × general-purpose (sonnet): mapper's delete, Close-memory and bulk-read questions, probes in a separate worktree. Used; the FlushInterval panic and the single-WAL-entry node delete re-checked in code.
- 1 × worker (sonnet): STE pass on the #657 PR body. Used after reading it back; one count corrected by hand.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

v1.6.0 and v1.7.0 are both released. v1.7.0 finishes the Cypher correctness work: MATCH patterns join, repeated clauses are kept or refused instead of dropped, CREATE and MERGE act per row with bound-node reuse and relationship direction, and a plain DELETE of an attached node is deprecated ahead of a v2.0 refusal. A planning checkpoint (#661, open) rebuilds the queue and records that roadmap themes are milestones, not version numbers.

## What's done since the 02:50 handoff

| PR | Title | Notes |
|---|---|---|
| #650 | fix(storage): refuse a record the mmap snapshot cannot store | uint16 lengths wrapped silently; also nil-map guards in UpsertEdge and Batch.UpdateNode (the batch one hung a deferred Close under `gs.mu`). |
| #651 | chore(coord): seed three tasks from the encryption-at-rest review | rules.json plaintext, envelope test cannot fail, no whole-directory plaintext test |
| #652 | docs: session handoff — 2026-10-08 02:50 UTC | |
| #653 | chore(release): v1.6.0 | tagged `ae188bd`; Release and Docker workflows green, 11 signed assets |
| #654 | chore(coord): seed the v2.0 refusal of a plain DELETE of an attached node | |
| #655 | feat(query): deprecate a plain DELETE that removes a node's relationships | `ResultSet.Notices` (typed, stable `Code`), REST header `X-Cypher-Deprecation`, server log line. User chose deprecate-then-refuse (option C) under `STABILITY_POLICY.md`. |
| #656 | chore(coord): seed per-row Cypher CREATE and MERGE | |
| #657 | fix(query): join MATCH patterns and stop dropping repeated clauses | Parser kept one slot per clause; MatchStep unioned patterns; path targets ignored bound variables. `Query.Merge` became `Query.Merges`. |
| #658 | chore(coord): seed four storage and telemetry robustness tasks | conflicted with #656 in the seed file; resolved by merging main in, no force-push |
| #659 | fix(query): make Cypher CREATE and MERGE act per row, reuse bound nodes and honour direction | CREATE re-created bound nodes as unlabeled ones and ignored direction; MERGE matched with an empty binding. Carries syntopica's trust-cluster query as a test. |
| #660 | chore(release): v1.7.0 | tagged `26de5d2`; Release and Docker workflows green, 11 signed assets |

Coord tasks released `done` since 02:50: `v1.4-mmap-record-width-refusal`, `v1.4-cypher-delete-without-detach`, `v1.4-cypher-multiple-match-clauses`, `v1.4-cypher-create-merge-per-row` (plus the six listed in the 02:50 handoff).

## Current state

- `origin/main` HEAD: `26de5d2` (#660, v1.7.0). Tags `v1.6.0` (`ae188bd`) and `v1.7.0` (`26de5d2`) pushed and released.
- Open PRs: **#661** `docs(planning): checkpoint 2026-10-08 and roadmap themes as milestones` — awaiting the user's agreement on the queue order.
- Local branches: `main`, and `feat/ulysses-consumer-contracts` in the separate worktree `../graphdb-ulysses-contracts` (not this session's; leave it).
- Uncommitted changes: none.
- Gates on the last code PR (#659): `make test-local` 54 ok, `golangci-lint` v2.13.2 0 issues, `gofmt -s` clean, `contract-guard` OK.
- One intermittent test outside this work: `pkg/licensing` `TestTelemetryReporter_reportLoop_Cancellation` failed once under full-suite load; task `v1.4-telemetry-stop-waits` owns it (real cause: `Stop` does not wait for the loop).

## What's next

The ranked queue is in `docs/NEXT_STEPS_2026-10-08.md` (PR #661). Top of it:

1. `v1.4-cypher-implicit-grouping` — **a short design is drafted and awaiting the user's approval** (in the session transcript): implicit grouping keys are the non-aggregate RETURN items; group by typed value, by entity ID for a node or relationship, null as its own group; drop the lossy string key and `parseGroupKey`; keep `GROUP BY` working; ORDER BY/SKIP/LIMIT on grouped rows. Aggregates inside `WITH` (not computed today) are out of scope and need their own task.
2. `v1.4-lock-panic-safety`.
3. Node upsert — brainstorm and spec first (three layers, consumer-facing).
4. `v1.4-batched-wal-flush-interval-validation`, `v1.4-datadir-flock` (short design).

### Gaps surfaced but not seeded

- Aggregates inside `WITH` are not computed (found by the #657 review; seed with implicit grouping).
- `WITH` binds `nil` for an undefined variable.
- Float NaN and -0 in a property index (from the #641 review).
- No way to zero the encryption key on Close.

## Stale assumptions to retire

All of the 02:50 handoff's list still applies. New since then:

- **`docs/ROADMAP_post_1.0.md` minor line** named a theme per version (v1.7.0 = Backup & DR, ...). Superseded by #661: themes are milestones. Until #661 merges, do not read a roadmap version number as a commitment.
- **`docs/CAPABILITIES_2026-05-10.md`** lists `query` as mature. Nine silent-result or dropped-clause defects were found on 2026-10-08; eight are fixed in v1.6.0/v1.7.0; implicit grouping remains.
- **Cypher `CREATE` with a bound variable** made a new unlabeled node on every release before v1.7.0. Consumers that ran `MATCH ... CREATE (a)-[...]->(b)` (syntopica-timer-spec does) may hold stray unlabeled nodes; v1.7.0 does not remove them.
- **Cypher `DELETE r` and `SET r.x`** were no-ops before v1.6.0; syntopica's `fraud-detection.ts` `MATCH ()-[r:VOTES_WITH]->() DELETE r` starts deleting on upgrade.

## Open questions for the user

1. Approve the implicit-grouping design (§5 item 1), or change it.
2. Agree or change the queue order in #661, then merge it.
3. Seed the four gaps in §5?
4. Tell syntopica-timer-spec's owner about the CREATE stray nodes and the DELETE r behaviour change?

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (same text).

## How to use this handoff

1. Read this, then the 02:50 handoff's §6 (still valid), then `docs/NEXT_STEPS_2026-10-08.md` (or #661 if unmerged).
2. Before trusting a coord task title written on 2026-10-08: `coord insight list --task graphdb:<id>`.
3. Message `mapper-6b` (ListAgents) before changing anything mapper depends on (`v1.4-mapper-consumer-contracts` lists it).
