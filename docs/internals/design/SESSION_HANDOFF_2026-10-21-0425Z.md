# Session handoff — 2026-10-21 04:25 UTC

**Date**: 2026-10-21 (single session, 22 PRs merged, v1.8.0 released)
**Outgoing model**: Claude Opus 5.5
**Delegation**:
- 7 × worker (sonnet), each in its own worktree, each diff read in the main loop before its PR:
  - snapshot RLock panic safety (#672): used; its "+5%" benchmark was order bias — an interleaved A/B re-run gave −2.3% / −0.3%, and the PR quotes that.
  - 409 `conflicting_node_id` (#675): used; the main loop corrected the CC26 Origin column.
  - storage upsert (#677): used after a fix — the main loop found that `upsertKeyAllowed` accepted a key matching *one* of two covering rules (AG3 hole), wrote the red test and the fix as a third commit.
  - REST upsert (#683): used; rebased onto main onto a new branch (no force push), CHANGELOG conflict resolved by hand.
  - Cypher CREATE/MERGE uniqueness (#680): used; the main loop re-ran its gates (it skipped them after a last edit).
  - Go client upsert (#684): used; the main loop added `//nolint:errcheck` reasons for two lines the linter flags (one came from #675).
  - PUT/patch uniqueness (#685): used; its "silent `_ =` in MERGE SET" finding was checked and is dead code (`NewPlanner` has no caller).
- 1 × Explore (sonnet): upsert code map. Mostly right; its claim that Go `BatchCreate` "always returns an empty slice" was wrong under gc (a probe program returned 3) — the code is order-fragile, not broken.
- 1 × reviewer (opus): builder check of the upsert spec. Found three real gaps (D1 key vs rule, D6 rules on the stored node's other labels, batch WAL reporting) and four wrong claims; all went into the spec before approval.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

v1.8.0 shipped, node upsert landed in all three layers (storage, REST, Go client), and every
REST, GraphQL and Cypher node write path now enforces the uniqueness-rules registry (three bypasses
closed). Two CI "flakes" turned out to be real defects and were fixed (#682, #688). Nothing since
v1.8.0 is released.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #667 | docs: session handoff — 2026-10-08 12:43 UTC | the previous session's handoff |
| #668 | docs(planning): mark implicit grouping and lock panic safety done | |
| #669 | chore(release): v1.8.0 changelog, release pointers and chart appVersion | tag `v1.8.0` on `d3d2798`; Release + Docker workflows success; 11 assets, 6 signatures |
| #670 | fix(wal): refuse a batched WAL with no flush interval | `*wal.BatchConfigError`; the zero interval crashed the whole test binary (goroutine panic) |
| #671 | fix(licensing): wait for the telemetry loop in Stop | the planned 200× load test could not reproduce the flake on old code either — the deterministic test is the proof; also fixed a data race in the tests on main |
| #672 | fix(storage): release the snapshot read lock on a panic | `captureJSONSnapshot` / `captureMmapSnapshot` |
| #673 | chore(coord): seed three uniqueness-registry bypass tasks | batch, Cypher, PUT |
| #674 | fix(api): enforce uniqueness rules on POST /nodes/batch | it stored a duplicate per repeated key and counted it as created |
| #675 | feat(api): name the existing node ID in a uniqueness 409 | CC26 |
| #676 | docs(design): specify node upsert | approved by the user with its 11 recommended calls (§9) |
| #677 | feat(storage): add UpsertNodeForTenant | |
| #678 | docs(changelog): record #670 to #675 under Unreleased | |
| #680 | fix(query): enforce uniqueness rules on Cypher CREATE and MERGE | `/query` duplicate CREATE was 200, now 409; concurrent MERGE duplicate reproduced 3/20 before |
| #682 | fix(algorithms): count a clustering triangle in either edge direction | the "known flake" with 3 retries was a map-order defect, ~16% of first attempts |
| #683 | feat(api): upsert nodes on POST /nodes and /nodes/batch | CC27–CC31; replaces closed #681 (stacked draft) |
| #684 | feat(go-client): add node upsert | |
| #685 | fix(storage): enforce uniqueness rules on node property updates | every `patchNode` write; PUT 409 was 500 |
| #686 | chore(coord): seed the Batch and Transaction uniqueness-rules task | |
| #687 | docs(changelog): record #680, #682, #684 and #685 under Unreleased | |
| #688 | fix(query): stop ResultStream dropping nodes and panicking on Close | the second "flake": `StreamNodes` 1 of 2; also a send-on-closed-channel panic |
| #679 | docs(planning): record the upsert, Track U and the 2026-10-21 fixes | adds Track U to the planning doc |

Coord tasks released `done`: `v1.4-batched-wal-flush-interval-validation`, `v1.4-telemetry-stop-waits`,
`v1.4-snapshot-rlock-panic-safety`, `v1.4-rest-batch-create-uniqueness-rules`,
`v1.4-rest-conflict-node-id`, `v1.4-storage-upsert-node`, `v1.4-rest-upsert-node`,
`v1.4-go-client-upsert-node`, `v1.4-cypher-create-uniqueness-rules`,
`v1.4-patch-node-uniqueness-rules`.

## Current state

- `origin/main` HEAD: `f469cc6` (#679). Latest tag `v1.8.0` (`d3d2798`).
- Open PRs: this handoff, plus two that are green and wait for the user to merge (the auto-mode
  classifier refused both merges, "Merge Without Review"):
  - **#690** `chore(release): v1.9.0 changelog, release pointers and chart appVersion` — 12/12
    checks green. Moves `[Unreleased]` to `[1.9.0] - 2026-10-21` with a summary, adds the missing
    #677 and #688 entries, points README and ROADMAP at v1.9.0, chart 0.1.6 / appVersion 1.9.0.
  - **#691** `chore(coord): seed five gaps found on 2026-10-21` — 10/10 green. The five tasks are
    already applied to coord (`tasks_created=5`); the PR only records them in the seed file.
- **v1.9.0 is approved and not tagged.** The classifier refused the tag push ("Create Public
  Surface"). After #690 merges: `git tag -a v1.9.0 <#690 merge commit> -m v1.9.0`, push the tag,
  then watch the Release and Docker workflows with `gh run view --json conclusion`.
- #679 (planning doc: Track U, upsert, corrections) merged after a re-run of the macOS job that
  the #688 stream defect had failed.
- Local branches: 9 worktree branches, all merged or closed. Worktrees: `../graphdb-ulysses-contracts`
  (content in #631) and 7 under `.claude/worktrees/agent-*`. The auto-mode classifier refused
  `git worktree remove`; the user removes them.
- Uncommitted changes: none.
- Gates on the combined main after #683 + #685: `pkg/api`, `pkg/storage`, `pkg/query` short tests
  ok; `make contract-guard` OK — 31 contracts, 43 guarding tests.

## What's next

From `docs/NEXT_STEPS_2026-10-08.md` (updated in #679):

1. **Tag v1.9.0** once #690 merges (approved; see Current state).
2. `v1.4-tx-batch-uniqueness-rules` (#686) — the storage `Batch` and `Transaction.Commit` skip the
   registry on create and update; the last node write paths that do.
3. `v1.4-datadir-flock` — short design first.
4. Track E (encryption at rest), then Track W (fsync default re-measure).

### New gaps, seeded in #691

Coord IDs: `v1.4-stream-nodes-id-gaps`, `v1.4-batch-create-wal-error-text`,
`v1.4-upsert-wal-failure-202-test`, `v1.4-go-client-ci-lint`, `v1.4-go-client-return-eval-order`
(code read at `108a6c4`; the eval-order hazard is in `nodes.go:86` and `helpers.go:34`).

- `StreamNodes` (`pkg/query/stream_query.go`) scans IDs `1..NodeCount`, so it misses nodes after a
  delete leaves ID gaps. `ClusteringCoefficient` moved off the same pattern long ago.
- The plain batch create reports a WAL failure with the raw storage error text; the merge batch
  uses the fixed `walWriteFailedMessage` (#683 PR body).
- The single-upsert 202 path (WAL failure) has no test (#683).
- The `go-client` CI workflow runs no linter; `golangci-lint` on `clients/go` found two issues CI
  never saw (fixed in #684).
- Go client `BatchCreate` returns `out.Nodes, json.Unmarshal(…, &out)` — correct under gc, order
  not guaranteed by the spec.

## Stale assumptions to retire

- **`docs/adr/0001-uniqueness-rules-registry.md` lines ~27, ~58, ~138**: "Two surfaces enforce
  uniqueness" (GraphQL, REST create). Since #674, #680, #685 every REST, GraphQL and Cypher node
  write enforces it; the storage `Batch` and `Transaction` do not (#686). The ADR needs a
  consequences note.
- **`pkg/algorithms/community_test.go` "known flake … sync.Pool" comment**: removed in #682; the
  cause was the algorithm. Any memory or note that calls that test flaky is wrong.
- **`TestStreamingQuery_StreamNodes` as a flake**: it was `ResultStream.Close` (#688).
- **`SPEC_node_upsert_2026-10-21.md` D6 paragraph** says `PatchNodeForTenant` has the registry gap
  "today" — fixed in #685.
- **`docs/CONSUMER_CONTRACTS.md` CC25 row** still says `(this PR)` in Origin (pre-existing).
- **`.remember/remember.md`** is from 2026-09-10 and was delivered 9+ times; it describes the
  v1.4-graphql-index-pagination task as next. Replace it.

## Open questions for the user

1. ~~Release v1.9.0?~~ Approved 2026-10-21 ("proceed with recommendations"); #690 open, tag pending.
2. ~~Seed the five new gaps?~~ Approved; seeded, #691 open.
3. Carried, no answer recorded: the bare `RETURN n` column name `"n."` quirk, and telling the
   owner of syntopica-timer-spec about the CREATE stray nodes and the `DELETE r` change. Note:
   syntopica-timer-spec sends Cypher; #680 can now give it a 409 where it got a duplicate.
4. The stale coord claim on `graphdb:wiki-graph-export` (graphdb-coord #49 merged 2026-10-16) —
   the coord session must release it.

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (same text).

## How to use this handoff

1. Read this, then `docs/NEXT_STEPS_2026-10-08.md`.
2. For the upsert, read `docs/internals/design/SPEC_node_upsert_2026-10-21.md` §9 first.
3. Message `mapper-6b` (ListAgents) when it is live: v1.8.0, #670 (its FlushInterval ask), #675
   and the upsert (#677, #683, #684) are all its asks.
