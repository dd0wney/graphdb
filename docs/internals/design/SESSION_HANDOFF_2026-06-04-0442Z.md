# Session handoff — 2026-06-04 04:42 UTC

**Date**: 2026-06-04 (single continuation session: picked up the #305 merge from the prior handoff, then a net-new live-path bug, the C/D/E bundle, a capstone, and a two-phase **testing-infrastructure** build directly answering the user's "why so many silent bugs?")
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

The delete-path index-parity work is fully closed (replay, cascade, batch, remove-property all now maintain the per-tenant + vector indexes), and — prompted by the user asking whether insufficient testing was the root cause — graphdb gained a **parallel-invariant test harness**: a shard-derived consistency checker (proven to catch drift via a teeth-test) driven through the write-path × op matrix. This targets the *class* of bug behind #288/#298/#305/#307/#308, not just the instances.

---

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #305 | fix(storage): persist + rebuild the vector index across restart | The prior handoff's CRITICAL open finding. HNSW was lost on every restart; now rebuilt from the node set on load (defs persisted, graph derived — no format bump). Merged first this session. |
| #307 | fix(storage): cascade edge-delete removes edge from per-tenant index | **Net-new bug found while scoping C.** The shared cascade helpers skipped `removeEdgeFromTenantIndex` — a **live-path** tenant edge-count drift (wider blast radius than the replay-only pair), self-healed on reopen so invisible. Split out per "trust-the-code, surface the discrepancy." |
| #308 | fix(storage): delete-path replay + vector parity (C/D/E) | C: `replayDelete{Node,Edge}` now remove from the tenant index (crash-recovery drift). D: `RemoveNodeProperties` drops the removed prop's vector per-key (not wholesale). E: batch `executeDeleteNode` removes the node's vectors (mirrors #304's off-lock wiring). |
| #309 | test(storage): capstone — replay cascade-delete keeps both tenant counts | Cross-cutting regression guard for the C + #307 interaction (replay-delete of a node *with* edges) — needs both fixes, so neither PR could test it alone. |
| #310 | test(storage): parallel-invariant checker + retrofit (improved testing A) | `checkGraphInvariants`/`assertGraphInvariants` — derives ground truth from the authoritative shards, asserts every derived structure agrees. 8-case teeth-test proves it *fires* on drift; retrofitted into 6 known-green tests (zero false positives). |
| #311 | test(storage): write-path × op invariant matrix (improved testing B) | Drives the checker through live / batch (incl. cascade-delete) / transaction / WAL-replay paths, asserting invariants after every mutation. |

Also: opened/iterated the improved-testing plan in plan mode with the user (chose A+B now, count-only vector check, Phase C deferred).

---

## Current state

- **`origin/main` HEAD**: `25440c2` (#311).
- **Open PRs**:
  - **#306** — `docs: session handoff — 2026-06-04 03:04 UTC` (the **prior** session's handoff, still open/unmerged). Superseded by this handoff; recommend the user merge or close it. Its branch `docs/session-handoff-2026-06-04-0304Z` is still local.
  - **This handoff's PR** (about to open).
- **Open branches** (local, stale — cleanup candidates): `feat/expose-label-mutation`, `feat/expose-property-indexes-and-uniqueness` (prior handoff said these are now CLOSED PRs), `perf/int8-hnsw`, `docs/session-handoff-2026-06-04-0304Z`. Run `branch-cleanup`.
- **Uncommitted**: none (pre-existing untracked `.claude/scheduled_tasks.lock`, `docker-compose.override.yml`).
- **Test/lint**: `main` green. Every PR this session verified: full `pkg/storage` suite + `-race` ×2 + `go vet` + `golangci-lint run ./...` (0 issues); #308 also ran dependent suites (`pkg/api/... pkg/query/... pkg/vector/...`). Testing PRs are test-only (no production change).

---

## What's next

`NEXT_STEPS_2026-06-03.md` is the planning doc but is now well behind reality (see §6). Ranked, after this session:

**Highest-value, earned this session:**
- **Phase C — metamorphic equivalence test** (the documented testing follow-up). Apply the same op-script through live/batch/transaction/replay and assert observationally identical results — crucially **vector search-result equality**, which closes the count-only limitation (an in-place vector update re-indexed under the wrong value but right count currently slips through). New file `pkg/storage/invariant_metamorphic_test.go`. Plan: `~/.claude/plans/we-need-improved-testing-bubbly-wave.md`.

**Silent-bug-hunt backlog (from the prior handoff, still open):**
- **FTS index lost on restart (B)** — like the vector bug, but verify the API-server env-bootstrap path before treating as a bug.
- **`CreateVectorIndex` not WAL-logged** — an index created after the last snapshot is lost on crash. Narrow.
- **persist-HNSW escalation** — #305 rebuilds on load (O(N log N)); serialize the HNSW graph only if startup cost bites at very large N (e.g. the 814K ICIJ corpus). Measured follow-up.
- LSA stale after WAL-replay; partial-apply in `persist*Locked` if `idx.Insert` fails. Lower priority.

**New gaps surfaced this session (for the next planning checkpoint):**
- The invariant checker consciously **excludes `propertyIndexes`** and the **FTS index** — both can drift the same way. Extending the checker to them is a natural next increment (and would turn the FTS-on-restart question into a test).
- Vector check is **count-only**; Phase C is the exact-membership-via-search-results answer.

**Standing off-path candidates (no earned critical path once Phase C + backlog land):**
Productization (Python SDK + the 4 documented-but-unbuilt enterprise plugins + onboarding); a real ~814K ICIJ corpus run; a fresh audit in another dimension.

---

## Stale assumptions to retire

1. **`NEXT_STEPS_2026-06-03.md` knows nothing of**: the tenant-isolation sweep (F1/F2/F3, #300–#303), batch-path parity (#304), vector persistence (#305), the cascade live-path fix (#307), the delete-path bundle (#308), or the invariant-testing harness (#310/#311). It needs a refresh or a fresh `NEXT_STEPS_2026-06-04.md`. Use the `planning-doc-update` skill.
2. **"graphdb's silent bugs are an edge-case-coverage problem"** — explicitly retired this session. They're a **parallel-invariant** problem (N representations × M write paths). New memory `feedback_parallel_invariant_coverage` records the pattern + the checker/teeth-test/matrix remedy.
3. **The delete paths are now at full index parity** — replay, cascade, batch, remove-property all maintain the per-tenant + vector indexes. The prior handoff's "C/D/E bundle (next PR)" is DONE (#308); its "off-path batch delete/update tenant-index gap" was already done (#298).
4. **Memory `feedback_in_memory_index_drift_test_design`** still holds, but note its companion: crash-recovery tests MUST reopen (the load path itself drifts) — the inverse case, now codified in the replay tests + the matrix's WAL cell.
5. **PR #306 (prior handoff) is unmerged** — not lost, just open; this handoff supersedes its `NEXT_SESSION_PROMPT.md`.

---

## Open questions for the user

1. **Merge or close the stale prior-session handoff #306?** It's historical now; leaving two open handoff PRs is untidy.
2. **Phase C now or later?** It's the highest-value earned item (closes the count-only vector gap) but is the larger of the testing pieces. The user chose "A+B now, C follow-up" during the session — confirm whether the next session picks it up or pivots to the FTS/productization tracks.
3. **Extend the invariant checker to `propertyIndexes` + FTS?** Cheap increment, closes two more drift surfaces; competes with Phase C for "next testing increment."

---

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

---

## How to use this handoff

1. Read this first.
2. Refresh the planning doc (`NEXT_STEPS_2026-06-03.md` → new `2026-06-04`) from §6 before picking work — it's stale.
3. Then `CLAUDE.md` § "Orient first".
4. If picking up **Phase C**: read `~/.claude/plans/we-need-improved-testing-bubbly-wave.md` (§ Phase C) + `pkg/storage/invariants_test.go` + `pkg/storage/invariant_matrix_test.go`. The metamorphic test reuses `assertGraphInvariants` + `SearchForTenant` for result equality.
