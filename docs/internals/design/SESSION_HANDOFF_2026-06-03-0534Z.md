# Session handoff — 2026-06-03 05:34 UTC

**Date**: 2026-06-03 (short continuation of the long arc — merged the docs PRs, shipped Track Q's first item (Q1), and resolved the deferred M3 structure question with the now-live DSA skills)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

Track Q is open and its first item is done: **Q1 (vector nearest-neighbour correctness assertions) merged (#283)**. The Transaction-durability docs reconciliation + prior handoff merged (#281/#282). The next Track Q step is **Q2 — drive the `understand-graphdb` consumer against `main`** (heavier, needs the consumer runnable). Separately, the deferred Track P **M3 structure question is now resolved** (hash set, via the `choosing-a-map-or-set` skill) — M3 is blocked only on the **M7 API-deprecation decision**.

---

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #281 | `docs(planning)`: record Transaction-durability completion | reconciliation |
| #282 | `docs`: session handoff — 04:11 UTC | superseded by this |
| #283 | `test(vector)`: pin nearest-neighbour correctness at REST + storage (Track Q / Q1) | **Q1 done** |

**Q1 detail:** the 2026-06-02 REST exercise found two vector bugs (#243 ranking, #246 ingestion) the suite missed because vector-search tests asserted *count*, never *which* nodes. #283 adds known-answer NN-identity assertions: `pkg/api` `TestVectorSearch_NearestNeighbourCorrectness` (REST handler — the layer with zero identity checks) + `pkg/storage` `TestVectorSearchForTenant_KnownAnswerOrdering` (k>1 identity + ordering; storage previously pinned only k=1). Well-separated planted clusters → deterministic answer (not the synthetic-uniform concentration trap).

**Non-PR outcome — M3 structure resolved.** The user's portable DSA skill-suite (`~/.claude/skills`, `dd0wney/claude-skills`) is now complete (all 14, fully tested) and is to be used for structure/algorithm decisions. Applied `choosing-a-map-or-set` to the deferred Track P **M3** (label-index removal): the answer is a **hash set `map[uint64]struct{}`**, decisively — sorted-slice ruled out (keeps O(K) removal, buys ordering nothing needs; deterministic pagination is a sort-on-read concern). Recorded in memory `project_track_p_m3_m7_deferred`. So M3's only open question is now the **M7 decision**, not the structure.

---

## Current state

- **`origin/main` HEAD**: `f570ddf` (#283).
- **Open PRs (inherited, NOT mine)**: `#240`, `#241` — carried since 2026-05-24; left per user decision.
- **Open branches**: `main` + stale non-mine locals. This session's branches were `--delete-branch`'d.
- **Uncommitted changes**: none tracked. Pre-existing untracked `.claude/scheduled_tasks.lock`, `docker-compose.override.yml` — leave them.
- **Build/test/lint**: `main` builds; `pkg/api` + `pkg/storage` + `pkg/vector` + `pkg/wal` suites green; `golangci-lint` 0; routine `UNSTABLE` per PR = benchmark comment-step only.

---

## What's next

**Track Q — consumer-driven correctness hardening** (`NEXT_STEPS_2026-06-03.md` § Critical path). Q1 done; remaining:

- **Q2 (next) — drive `understand-graphdb` against `main` end-to-end.** Both consumers are present locally (`../understand-graphdb`, `../coi-screen`). Re-run ingest + Phase 2–3 queries; every divergence → a graphdb bugfix + a graphdb-side regression test. **Pre-flight (ask the user / check the consumer README):** needs a running graphdb server + an embeddings backend (`GRAPHDB_EMBED_URL` / local Ollama per the 1c verification) — the consumer-driving is exploratory and service-dependent, which is why it was deferred to a fresh session.
- **Q3** — `coi-screen` Milestone-1-proper (real ICIJ corpus); same fix-and-pin loop.
- **Q4** — generalize into a consumer-contract regression harness.

### Off-path / deferred (decisions teed up)

- **Track P tail M3/M7.** M3 structure now resolved (hash set; sort-on-read). Sequence is **M7 then M3**: M7 = drop the global `nodesByLabel`/`edgesByType` mirror, which is NOT a dead-code delete (live tenant-blind `FindNodesByLabel`/`FindEdgesByType` readers + snapshot-persisted) → **needs the user's call: is that tenant-blind API still wanted?** Once M7 removes the serialized global mirror, M3 is a clean format-free set swap. Use `choosing-a-concurrent-structure` for M7's lock-grain question.
- **Transaction follow-ups** (spec § Out of scope): tx deletes (`tx.DeleteNode`/cascade — `deletedNodes`/`deletedEdges` are dead scaffolding), conflict detection, client-facing transaction API. Only when a caller needs them (no non-test callers yet).
- **Batched-WAL default sweep**; **resolver-level index-level pagination**; **productization/operability**; **security audit**.
- **Inherited #240/#241** — dispose or adopt.

---

## Stale assumptions to retire

1. **DSA skills are live + are to be used.** `~/.claude/skills` now has all 14 `choosing-*`/`applying-*` skills (fully tested). Standing instruction: invoke the relevant one before any structure/algorithm decision. M3 already used `choosing-a-map-or-set`; M7→`choosing-a-concurrent-structure`; HNSW→`choosing-a-priority-structure`/`choosing-a-graph-algorithm`; SIMD/goolang→`applying-hpc-techniques`.
2. **Memory `project_track_p_m3_m7_deferred`** now records the resolved M3 structure (hash set, not sorted-slice). Don't re-litigate the structure; the open item is the M7 API decision.
3. **`CLAUDE.md` § "Partitioned shard maps" (line ~69)** still lists `forEachNodeUnlocked` "(and edge variants)" — #261 deleted `forEachEdgeUnlocked`. Drop/qualify in a future CLAUDE.md touch. (Carried; small.)
4. **`NEXT_STEPS_2026-06-03.md`** is current (Track P closed, Transaction recorded, Track Q selected with Q1 now done — mark Q1 ✅ when reconciling next).

---

## Open questions for the user

1. **Q2 pre-flight** — how do you run `understand-graphdb` against a local graphdb (server launch + embeddings/`GRAPHDB_EMBED_URL`)? Needed before Q2 can start.
2. **M7 decision** — is the tenant-blind `FindNodesByLabel`/`FindEdgesByType` API still wanted? (Unblocks the whole M3/M7 tail; structure is already settled.)
3. **Transaction deletes** — implement next or wait for a caller?
4. Carried: batched-WAL default sweep; inherited #240/#241.

---

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

---

## How to use this handoff

1. Read this first.
2. Track Q / Q2: `NEXT_STEPS_2026-06-03.md` § Critical path; resolve open question 1 (run setup) with the user first.
3. M3/M7 tail: memory `project_track_p_m3_m7_deferred` (structure resolved; M7 decision pending).
4. Then `CLAUDE.md` § "Orient first" (auto-loaded). Invoke the relevant DSA skill before any structure/algorithm decision.
