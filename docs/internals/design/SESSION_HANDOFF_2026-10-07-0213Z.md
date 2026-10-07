# Session handoff — 2026-10-07 02:13 UTC

**Date**: 2026-10-07 (single session, 10:44–13:45 AEDT; five PRs merged, driven by a request from the Ulysses session)
**Outgoing model**: Claude Opus 5.5
**Delegation**:
- opus `reviewer` on #631: one should-fix (request bodies encoded through the server structs, proved by a tag-rename mutation). The main loop re-read the test lines, fixed it, and repeated the mutation with a control. Used.
- sonnet `general-purpose` verifying the six Ulysses claims (items 2–7) against `58dd3e5`: used after the main loop opened the code for items 2, 3 and 4. Two claims came back partly wrong (5, 6), which went back to Ulysses.
- opus `reviewer` on #633 with eight numbered doubts: verdict commit; asked for three more tests and a downgrade note. Both added, each test proved by a mutation.
- opus `reviewer` on #634 with eight numbered doubts: one should-fix (the vector check ran after the in-memory change, so a refused patch half-applied). Reproduced red by the main loop before the fix.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

Ulysses' graphdb asks are triaged and queued: #631 merged (its blocker), six follow-ups seeded in its priority order. **Task 1 turned up a live durability defect: a node property removed after the last snapshot came back after a crash, in both snapshot modes (#633).** A probe then found a second silent-corruption defect: a Cypher write stores `null`, lists and maps as Go `%v` text (`"<nil>"`, `"[a b]"`), now seeded and not fixed.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #631 | `feat(api)`: register Ulysses as a consumer (CC17–CC22), tenant-scoped community algorithms | `ac4f79f`. Written by the Ulysses session; reviewed and merged here. Review fix `2777fd6`: the CC17/CC19/CC20 request bodies were built from the server structs, so a renamed request tag changed test and handler together. Old tests passed under the rename, new ones fail at `:57 :183 :205 :225`. |
| #632 | `chore(coord)`: seed the encoding rule and five Ulysses follow-up tasks | `7c6705c`. No coord session was running; the user approved seeding directly. |
| #633 | `fix(storage)`: keep a removed node property removed across WAL replay | `f0e553d`. See §6 first bullet. Red first in JSON and mmap; four mutations prove the other tests. |
| #634 | `feat(api)`: a JSON `null` in `PUT /nodes/{id}` and `/edges/{id}` removes the key | `42c2f1b`. CC23/CC24. Stacked on #633 and retargeted to main before #633 merged; then conflicted in `node_operations.go` (it rewrote #633's lines), resolved with a merge commit after checking main's copy equalled `f3d8a68`'s. coord `graphdb:v1.4-put-null-removes-property` released, done. |
| #635 | `chore(coord)`: seed the Cypher value conversion task | `b75b940`. |

## 3. Current state

- `origin/main` HEAD: `42c2f1b` (#634).
- **Open PRs**: this handoff only.
- **Open branches**: this handoff branch, and `feat/ulysses-consumer-contracts`, which is the **Ulysses session's** local branch in `../graphdb-ulysses-contracts` (its remote is deleted; leave it to that session).
- **Worktrees**: `../graphdb-ulysses-contracts` (Ulysses') only.
- **Uncommitted changes**: none.
- **Gates on #634's tree**: `make test-local` 54 packages ok; `-race -count=3` storage ok (150 s); golangci-lint v2.13.2 0 issues; contract-guard 24 contracts / 35 tests pinned.
- **Coord (graphdb)**: 21 tasks — 15 done, 6 pending, none in progress.

## 4. What's next

1. **A release that contains #634.** Ulysses (`ulysses-d4`) waits for a version number to switch its mappers to send `null` for a cleared field. It needs no change before then (it checked: its one PUT null is document `metadata`, which it reads back as `{}`).
2. **`graphdb:v1.4-cypher-value-conversion`** — silent data corruption, highest severity in the queue. The `null` decision is open (§6).
3. The Ulysses follow-ups, in Ulysses' order: `v1.4-label-propagation-cache-neighbours`, `v1.4-vector-index-backfill-on-create`, `v1.4-traverse-truncation-header-alias`, `v1.4-query-sanitizer-skip-literals`. None blocks a Ulysses release.
4. `v1.4-rest-property-encoding-rule` — user chose "decide later". Ulysses must hear before any change.
5. Carried: benchmark CI step (premise corrected in §5), `clients/go` tag scheme, and the 2026-09-15 off-path list.

### New gaps for the next planning checkpoint

None of the seven tasks seeded today is on `docs/NEXT_STEPS_2026-06-18.md`. A `planning-doc-update` should add them, plus #631/#633/#634 as done.

## 5. Stale assumptions to retire

- **2026-09-15 handoff §5, "the benchmark step is on track to fail at Go's 10-minute default per package": FALSE.** `go test` injects `-test.timeout=10m`, but the testing package stops that alarm before benchmarks run (`testing/testing.go:2598-2616` in go1.27.0: `startAlarm` → tests and examples → `stopAlarm` → `runBenchmarks`). `pkg/storage` ran 1470 s and passed on 2026-10-06. The real gaps: the job has no `timeout-minutes` (a hang holds a runner for GitHub's 360-minute default), and `-bench=.` has no `-run='^$'`, so the step runs the unit tests again first.
- **Anything saying Cypher `REMOVE n.prop` is durable before `f0e553d` is FALSE.** `RemoveNodeProperties` logged `OpUpdateNode` with the remaining properties and `replayUpdateNode` merges, so a post-snapshot removal reappeared on recovery, and a removed vector property became searchable again. Fixed in #633 with an additive `omitempty` `Removed` field. **Downgrade limit**: a pre-#633 binary ignores `Removed`; close cleanly before downgrading.
- **"`PUT` merges and no REST request can remove a property"** — false since `42c2f1b` (#634): a top-level JSON `null` removes the key. A `null` on POST is still stored (CC24). GraphQL updates and Cypher `SET n.x = null` do not follow the PUT rule.
- **2026-09-15 `NEXT_SESSION_PROMPT.md` item 2, "Do not touch PR #625"** — #625 merged 2026-09-23.
- **"No graphdb coord task is pending"** (2026-09-15 handoff §3) — seven seeded today.
- **Ulysses' 2026-10-07 request, item 5 ("a client cannot remove a property") and item 6 ("no backfill")** — both partly wrong at `58dd3e5`: Cypher `REMOVE` existed (nodes only), and every open runs `rebuildVectorIndexesFromNodes`. Ulysses was told.
- **`CLAUDE.md` § "Atomic-commit convention", "PRs are squash-merged"** — #625 and #630 landed as merge commits on 2026-09-23. Today's PRs were squash-merged per the doc. Worth confirming which the user wants.

## 6. Open questions for the user

- **Cypher `SET n.x = null`**: remove the key (Cypher semantics; matches PUT after #634; the session's lean) or store a JSON null (matches REST POST)? Recorded in the task title.
- **Release** containing #634, and its version, for Ulysses.
- Carried: benchmark CI step (seed a small fix: job `timeout-minutes` plus `-run='^$'`?), `clients/go` tag scheme (`clients/go/vX.Y.Z` was the recommendation), the encoding rule (deferred by choice).

Answered this session, recorded so they are not re-asked:

- WAL removal record → additive `Removed` field on `OpUpdateNode`, not a new op type (an older binary skips an unknown op entirely). Two PRs, not one.
- `null` on create → **keep storing null**.
- Encoding rule → **decide later**, seeded.
- No coord session running → **seed directly**, with a PR per seed change.

## 7. Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (generated from this handoff).

## 8. How to use this handoff

1. Read this first, then `docs/NEXT_STEPS_2026-06-18.md` (stale on today's tasks, see §4).
2. `git checkout main && git pull --ff-only` before reading or branching from main.
3. Message `ulysses-d4` (if `ListAgents` shows it) before any change to the REST property encoding or the `X-Truncated` header.
4. Run `make test-local` for the package gate, and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./pkg/... ./cmd/...` for CI's linter.
