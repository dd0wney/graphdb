# Session handoff — 2026-06-03 02:20 UTC

**Date**: 2026-06-02→03 (long continuation — completed Track P items (2) and (3), then the clean portion of (4); this handoff supersedes both the 0844Z handoff and the concurrent 1241Z `coi-screen` handoff for graphdb-core purposes)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

**Track P is now done through the clean portion of item (4).** Items (1) WAL group-commit, (2) cross-tenant read scans, (3) HNSW-out, and item (4)'s M5/M6/M4 are all merged. What remains of the whole audit backlog is the **decision-laden tail: M3 (snapshot-format-invasive) and M7 (mis-framed — a reframe, not a delete)**. Neither is a drop-in; both need a design call. The next session should either take a M3/M7 decision or pick fresh work.

---

## What's done this session (graphdb-core, #259 onward)

| PR | Title | Notes |
|---|---|---|
| #259 | rebuild tenant edge index on snapshot+replay | item (2) prerequisite + latent restart bug |
| #260 | per-tenant node-ID enumeration index | item (2) node half (92× noisy-neighbor) |
| #261 | per-tenant edge-ID enumeration index | item (2) edge half |
| #262 | index-level `CountNodesByLabelForTenant` | item (2) M1 |
| #263, #264, #265 | planning reconciliation, CLAUDE.md fix, handoff | docs |
| #266 | lift HNSW insert out of `gs.mu` | item (3) / H2; plan-under-lock/apply-after-unlock; race-proven |
| #267, #268 | planning reconciliation, handoff (0844Z) | docs |
| #269 | pool HNSW visited set | item (4) M5; 8× fewer bytes / 57% fewer allocs |
| #271 | cache cosine norms | item (4) M6; 3.66× per-call, bit-identical |
| #272 | entry-point level index | item (4) M4; O(N)→O(log N), ~5000× at 50k nodes |
| #273 | planning reconciliation (item 4 clean) | docs — **open at handoff** |

(Not mine: **#270**, the 12:41Z `coi-screen` session — built a separate sibling tool, **did not touch graphdb core**. It advanced nothing here; its only graphdb artifact was overwriting `NEXT_SESSION_PROMPT`, now superseded by this handoff.)

**Method notes worth keeping:**
- **The advisor caught the two highest-value design errors before code** (the #259 restart gap; #266's memory-safety reframe). Call it before committing to an approach.
- **M6's CI hit a macOS-runner flake** (generic "exit code 2", no FAIL annotation). Confirmed flake by reproducing the full surface green locally on darwin + a clean job re-run, then merged. The matrix test job (macOS-only since #181) is better than the old Linux exit-143 but not immune — `gh run rerun <id> --failed` after local confirmation is the play.
- **Benchmark hygiene**: measure variants on one shared fixture / isolate at the right layer. M6's HNSW-search microbench was noisy (~6%); the real win showed cleanly at the distance level (3.66×). Saved as auto-memory `feedback_benchmark_shared_fixture_not_per_size`.
- **Background-shell discipline**: avoid `until…do sleep…done` wait-loops — they pile up as tracked background shells. Lean on task-completion notifications.

---

## Current state

- **`origin/main` HEAD**: `ffb8d58` (#272).
- **Open PRs (mine)**: **#273** — planning reconciliation for item (4)'s clean portion. Docs-only; merge when CI green.
- **Open PRs (inherited, NOT mine)**: `#240`, `#241` — carried since 2026-05-24; left per user decision.
- **Open branches**: `main` + `docs/planning-track-p-item-4-clean-done` (#273) + stale non-mine locals. This session's code branches were `--delete-branch`'d.
- **Uncommitted changes**: none tracked. Two pre-existing untracked files (`.claude/scheduled_tasks.lock`, `docker-compose.override.yml`) — leave them.
- **Build/test/lint**: `main` builds; `pkg/vector` + `pkg/storage` suites green; recall + bit-identity tests green; `-race -count=2/3` clean on vector/storage hot paths; `golangci-lint` 0. Routine `UNSTABLE` per PR = benchmark comment-step only.

---

## What's next

Track P's first-order backlog is exhausted except the decision-laden tail:

1. **M3 — label-index O(K) removal (deferred, needs a design call).** `removeFromLabelIndex` (`node_indexing.go:51`) + `removeNodeFromTenantIndex` (`tenant_operations.go:57`) do an O(K) linear scan, twice per `DeleteNode`, under `gs.mu`; bulk delete / tenant offboarding is O(N²). The audit's `map[uint64]struct{}` fix changes the **persisted** global index's value type → **snapshot format version bump**. A sorted-slice (binary-search find, O(N) shift) avoids the format change. **Decision needed:** format bump vs sorted-slice compromise.
2. **M7 — drop the global mirror (deferred, REFRAMED).** NOT a dead-code delete: `nodesByLabel`/`edgesByType` have live tenant-blind readers (`FindNodesByLabel`/`FindEdgesByType` at `query_operations.go:145,215`; `node_adjacency.go:57`) and are snapshot-persisted. Real shape: deprecate/migrate the tenant-blind `Find*` API to per-tenant indexes, *then* drop the mirror. **Decision needed:** is the tenant-blind `Find*` API still wanted?
3. **If neither appeals**, Track P's measured-leverage work is effectively complete — the next critical path is a fresh question (a new audit angle, or one of the carried follow-ups below). Worth a planning checkpoint / new `NEXT_STEPS_<DATE>.md`.

### Carried follow-ups (lower-leverage, below the tail)

- **Resolver-level index-level pagination** (rec #2's deferred half): GraphQL edge resolver (`pagination_resolvers.go:123`) + REST `listNodes` (`handlers_nodes.go:80`) still materialize the full per-tenant slice then page in memory. Scales with the tenant's own data, not total DB. Resolver contract change (cursors are integer offsets).
- **Batched-WAL default** — deferred pending a FlushInterval latency-vs-throughput sweep (unstarted).
- **Item-(1) remainder** — `RemoveNodeProperties` + admin index ops to the group-commit pattern.

---

## Stale assumptions to retire

1. **`NEXT_STEPS_2026-05-15.md` § Decision 9** — item (4)'s clean portion reconciled to ✅ in **#273** (open). Until it merges, the live doc lags by these three PRs.
2. **`NEXT_SESSION_PROMPT.md`** currently points at the 1241Z `coi-screen` handoff and says graphdb is "advanced only by #269." Stale — M6 (#271) + M4 (#272) landed after. This handoff regenerates it.
3. **`CLAUDE.md` § "Partitioned shard maps" (line ~69)** still lists `forEachNodeUnlocked` "(and edge variants)" — #261 deleted `forEachEdgeUnlocked`. Drop/qualify in a future CLAUDE.md touch. (Carried since 0759Z — small, still open.)
4. **Audit M3/M4/M5/M6/M7 framing**: M5/M6/M4 are FIXED (#269/#271/#272). M3 is invasive (format bump). **M7's "dead code" premise is wrong** — the mirror has live readers; any future summary should say "reframed: deprecate tenant-blind `Find*` first," not "delete dead mirror."
5. **Audit H2 (#266) is FIXED** — HNSW insert runs off `gs.mu`. H3 (auto-embed 2×) remains an inherent sizing note, not a defect.

---

## Open questions for the user

1. **Merge #273** (planning reconciliation for item 4 clean portion)? Docs-only, verified; left unmerged per convention.
2. **M3 approach** — snapshot format version bump (true O(1) set removal) vs sorted-slice (no format change, O(N) shift)?
3. **M7** — is the tenant-blind `FindNodesByLabel`/`FindEdgesByType` API still wanted? That decides whether M7 is a migration or stays as-is.
4. **Batched-WAL default** — still deferred pending a FlushInterval sweep. Carried.
5. **Inherited #240/#241** — still carried; dispose or adopt.

---

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

---

## How to use this handoff

1. Read this first.
2. If continuing Track P: read `AUDIT_performance_saas_load_2026-06-02.md` § M3, § M7, and resolve open questions 2–3 with the user before coding (both are decisions, not drop-ins).
3. Then `docs/NEXT_STEPS_2026-05-15.md` § Decision 9 (item (4) reconciled once #273 merges).
4. Then `CLAUDE.md` § "Orient first" (auto-loaded).
