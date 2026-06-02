# Session handoff — 2026-06-02 08:44 UTC

**Date**: 2026-06-02 (continuation session — completed Track P item (2) then item (3); 5 code PRs + 4 docs PRs merged across the day's arc, this segment landed #259–#266)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

**Track P items (1), (2), and (3) are all complete on `main`** — the audit's top three measured-leverage recommendations. Item (3) this segment: the HNSW insert is lifted out of `gs.mu` (#266). The critical path advances to **item (4): index-structure hygiene (M3/M4/M7) + vector-search read hygiene (M5/M6)**. Stopping here at a planning checkpoint (user chose a fresh session for item (4)).

---

## What's done this session (this segment, #259 onward)

| PR | Title | Notes |
|---|---|---|
| #259 | `fix(storage)`: rebuild tenant edge index on snapshot load + WAL replay | Prerequisite + standalone latent-bug fix (`tenantEdgesByType` never rebuilt on restart). |
| #260 | `perf(storage)`: per-tenant node-ID enumeration index | Item (2) node half; A4 read pattern; 92× on the noisy-neighbor bench. |
| #261 | `perf(storage)`: per-tenant edge-ID enumeration index | Item (2) edge half (depends on #259). |
| #262 | `perf(storage)`: index-level `CountNodesByLabelForTenant` (M1) | Item (2) count fix. |
| #263 | `docs(planning)`: close Track P items (1) and (2) | Planning reconciliation. |
| #264 | `docs(claude)`: correct `pkg/intelligence` "absent" claim | CLAUDE.md date-scoping fix. |
| #265 | `docs`: session handoff — 07:59 UTC | Superseded by this one. |
| #266 | `perf(storage)`: lift the HNSW insert out of `gs.mu` (item 3 / H2) | Plan-under-lock / apply-after-unlock; race-proven; bad-dim create aborts as no-op. |

**Method notes worth keeping:**
- **The advisor caught two design errors before code was written**, both this segment: (a) the edge tenant index isn't rebuilt on restart (→ #259 had to precede the edge enumeration index), and (b) the H2 lift is a *memory-safety* problem, not just latency — `UpdateNode`'s vector read touches the live `node.Properties` map a concurrent writer mutates, so the off-lock insert must read a snapshot captured under the lock. Both reframings changed the implementation. Call the advisor before committing to an approach.
- **Benchmark fixtures:** measure variants (indexed vs legacy) on ONE shared graph; per-size fresh graphs fold GC/cache noise into per-call numbers and faked an "algorithmic regression" until corrected (saved as auto-memory `feedback_benchmark_shared_fixture_not_per_size`).
- The stacked-PR merge dance (retarget dependent → merge parent → `git rebase --onto` the child) ran cleanly for the #260→#261→#262 stack.

---

## Current state

- **`origin/main` HEAD**: `c396c00` (#266).
- **Open PRs (mine, awaiting merge)**: **#267** `docs(planning)`: close Track P item (3) — single-file planning reconciliation (marks item 3 done, advances to item 4). Docs-only; merge when CI green.
- **Open PRs (inherited, NOT mine)**: `#240`, `#241` — carried since 2026-05-24; left per user decision (off critical path).
- **Open branches**: `main` + `docs/planning-track-p-item-3-done` (#267) + stale non-mine locals (`feat/expose-label-mutation`, `feat/expose-property-indexes-and-uniqueness`, `perf/int8-hnsw`). This segment's code branches were `--delete-branch`'d.
- **Uncommitted changes**: none tracked. Two pre-existing untracked files (`.claude/scheduled_tasks.lock`, `docker-compose.override.yml`) — leave them.
- **Build/test/lint**: `main` builds; `pkg/storage` suite green; `-race -count=2/3` clean on vector/concurrent/create/update/tenant/persistence paths; `pkg/vector`+`pkg/query`+`pkg/api`+`pkg/graphql` caller suites green; `golangci-lint` 0 issues. Routine `UNSTABLE` per PR is only the benchmark comment-step permissions failure.

---

## What's next

**Track P** (`docs/internals/design/AUDIT_performance_saas_load_2026-06-02.md` § Recommendations; `docs/NEXT_STEPS_2026-05-15.md` § Decision 9):

1. WAL group-commit — ✅ done (#255–#257). Remainder (minor): `RemoveNodeProperties` + admin index ops still synchronous.
2. Cross-tenant read scans (H4) — ✅ done (#259–#262).
3. HNSW-out (H2) — ✅ done (#266).
4. **Index-structure + vector-read hygiene — the next critical-path step.**
   - **M3:** label-index removal is an O(K) linear scan run twice per `DeleteNode` under `gs.mu` (`node_indexing.go:51`, `tenant_operations.go:57`) → `map[uint64]struct{}` or sorted slice (binary-search removal). Bulk delete / tenant offboarding is O(N²) today.
   - **M4:** `DeleteNode → findNewEntryPoint` (`pkg/vector/hnsw_graph.go:107`) is an O(N) scan of the tenant's HNSW under `gs.mu` → maintain an O(1) max-layer candidate set.
   - **M7:** drop the dead tenant-blind global `nodesByLabel`/`edgesByType` mirror — **the per-tenant indexes now fully cover enumeration after items (2)/(3)**, so confirm no live readers remain, then delete (shrinks memory + snapshot footprint).
   - **M5:** `sync.Pool` the HNSW per-layer visited set (`pkg/vector/hnsw_search.go:9,67`). **M6:** cache query/stored norms (`pkg/vector/distance.go:29`).

### Follow-ups on the queue (below item 4)

- **Resolver-level index-level pagination** (rec #2's deferred half): GraphQL edge connection resolver (`pkg/graphql/pagination_resolvers.go:123`) + REST `listNodes` (`pkg/api/handlers_nodes.go:80`) still fetch the full per-tenant slice then page in memory. Lower-leverage (scales with the tenant's own data, not total DB). Resolver contract change (cursors are integer offsets today).
- **Batched-WAL default** — deferred pending a FlushInterval latency-vs-throughput sweep (unstarted).
- **Item-(1) remainder** — `RemoveNodeProperties` + admin index ops to the group-commit pattern.

---

## Stale assumptions to retire

1. **`docs/NEXT_STEPS_2026-05-15.md` § Decision 9** — item (3) is reconciled to ✅ in **#267** (open at handoff). Until #267 merges, the live doc still reads item (3) as "next." Once merged, the doc is current and item (4) is the head of the queue.
2. **`CLAUDE.md` § "Partitioned shard maps" (line ~69)** still lists `forEachNodeUnlocked` "(and edge variants)" — #261 deleted `forEachEdgeUnlocked`. Drop/qualify "(edge variants)" in a future CLAUDE.md touch. (Carried from the 0759Z handoff; still unaddressed — small.)
3. **The audit's H2 finding (HNSW insert under `gs.mu`) is now FIXED (#266).** Any summary should say "closed 2026-06-02," not present it as open. H3 (auto-embed 2×) remains an inherent sizing note, not a defect.
4. **Auto-memory `reference_graphdb_embedding_search_api.md`** — optional refresh: vector create/update now insert into the HNSW index off the global write lock; a malformed-dimension vector on create aborts as a true no-op (was: stored-then-rejected).

---

## Open questions for the user

1. **Merge #267** (planning reconciliation for item 3)? Docs-only, verified; left unmerged per the planning-doc-update convention.
2. **Batched-WAL default** — still deferred pending a FlushInterval sweep (unstarted). Carried.
3. **Inherited #240/#241** — still carried; dispose or adopt eventually.

---

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

---

## How to use this handoff

1. Read this first.
2. Then `docs/internals/design/AUDIT_performance_saas_load_2026-06-02.md` § M3–M7 (the next task).
3. Then `docs/NEXT_STEPS_2026-05-15.md` § Decision 9 (item (4) is head-of-queue once #267 merges).
4. Then `CLAUDE.md` § "Orient first" (auto-loaded).
