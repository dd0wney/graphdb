# Session handoff — 2026-06-04 03:04 UTC

**Date**: 2026-06-04 (very long single session: 11 PRs merged + 1 open. Five threads — Track P tail, CC6, a tenant-isolation sweep, batch-path parity, and a silent-bug hunt that found a CRITICAL bug now in flight)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

graphdb was substantially hardened: Track P tail closed (M3 set-based label index + M7 rename), a tenant-isolation sweep (validated-strong; 3 small fixes), the batch write path brought to full parity with the canonical path, and a systematic silent-bug hunt that found a **CRITICAL bug — the HNSW vector index was lost on every restart**, now fixed in **open PR #305**. The next session merges #305, then does the small **C/D/E** sibling bundle.

---

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #294 | Track P tail: M3 set-based label index + M7 `*AcrossTenants` rename | M3 O(K)→O(1) removal, no format bump (derive-on-load); M7 rename completed A3b convention. Both reframed the audit (trust-the-code). |
| #295 | fix(graphql): scope aggregate-schema property discovery per-tenant | Latent cross-tenant schema-metadata leak the M7 rename surfaced; test-only generator, not live. |
| #296 | docs(planning): close Track P tail (M3+M7) | Reconciliation. |
| #297 | docs: session handoff — 2026-06-03 23:29 UTC | Mid-session handoff (the stack-landing one). |
| #298 | fix(storage): per-tenant indexes on batch delete (CC6) | Delete-side sibling of #288. **Lesson:** assert COUNTS not list-membership; do NOT reopen (rebuild self-heals) — memory `feedback_in_memory_index_drift_test_design`. |
| #299 | docs(planning): mark CC6 done | Reconciliation; corrected "delete/update"→delete-only. |
| #300 | fix(api): gate `/api/metrics` admin-only (sweep F1) | Cross-tenant global-stats leak. Extracted testable `registerRoutes(mux)` from `Start()`. |
| #301 | fix(api): scope `/api/v1/tenants/{id}` with `withTenant` (sweep F2) | Non-admin denied own tenant + could read `default`'s. Root cause: A5 `buildTestMux` replica drifted → rewired to `registerRoutes`. |
| #302 | refactor(storage): rename orphaned tenant-blind footguns to `*AcrossTenants` (sweep F3) | `FindNodesByProperty{Range,Prefix}`, `*EdgeBetween`; latent (no request callers). |
| #303 | docs: tenant-isolation sweep findings (2026-06-04) | `AUDIT_tenant_isolation_2026-06-04.md` — methodology + confirmed-clean inventory + by-design surfaces. |
| #304 | fix(storage): full batch-path parity — vectors, observers, edge-index cascade | G1–G4: batch create/update now index vectors; create/update/delete dispatch observers; delete cascade cleans global edge index + opposite adjacency. Mirrors `Transaction.Commit`. |

Also: **closed inherited PRs #240/#241** (verified not on `main`, no consumer need — disposition resolved).

---

## Current state

- **`origin/main` HEAD**: `27c3316` (#304).
- **Open PRs**: **#305** — `fix(storage): persist + rebuild the vector index across restart` (the silent-bug hunt's CRITICAL finding). Independent, base `main`. CI running at handoff time; expect routine `UNSTABLE` (benchmark comment-step) only.
- **Open branches**: `main` + `fix/vector-replay-parity` (#305) + `docs/session-handoff-2026-06-04-0304Z` (this) + stale inherited (`feat/expose-*` — now CLOSED PRs, local branches can be pruned; `perf/int8-hnsw`).
- **Uncommitted**: none (pre-existing untracked `.claude/scheduled_tasks.lock`, `docker-compose.override.yml`).
- **Test/lint**: `main` green. #305 verified: full storage + vector + `pkg/api`(vector) suites green, vet + `golangci-lint` clean, `-race` clean ×2.

---

## What's next

**First: merge #305** (the CRITICAL vector-persistence fix; advisor-gated, race-clean). Then the queued **silent-bug bundle** and the bug-hunt's other findings:

### C/D/E — the "replay + delete vector parity" bundle (next PR; the user already chose to do these)
- **C**: WAL `replayDeleteNode`/`replayDeleteEdge` skip `removeNodeFromTenantIndex`/`removeEdgeFromTenantIndex` → per-tenant COUNT drift after crash recovery (the CC6 shape on the replay path). `persistence_replay.go`.
- **E**: batch `executeDeleteNode` never calls `RemoveNodeFromVectorIndexes` (canonical `DeleteNode` does) — a gap #304 missed. `batch_executor.go`.
- **D**: `RemoveNodeProperties` skips vector re-plan → stale vector when a vector-indexed prop is removed. `node_operations.go`.
Use the CC6 test discipline (counts not membership for index drift; vector tests assert search results).

### Silent-bug-hunt findings noted but NOT yet chased (reachability-triaged)
- **Full-text index lost on restart** (B) — like the vector bug, but the API server bootstraps FTS via env on startup, and embedded is manual; verify the server-bootstrap path before treating as a bug.
- **`CreateVectorIndex` is not WAL-logged** → an index *created* after the last snapshot is lost on crash (defs durable only via snapshot). Narrow; the #305 fix covers the common (index-at-setup) case.
- **persist-HNSW escalation**: #305 rebuilds the HNSW on load (O(N log N) for real embeddings; no-op without vectors). If startup cost bites at very large N (e.g. the 814K ICIJ corpus), serialize the HNSW graph instead. Measured-escalation follow-up.
- LSA stale after WAL-replay; partial-apply in `persist*Locked` if `idx.Insert` fails (low-probability). Lower priority.

### Standing off-path candidates (no earned critical path once #305 + bundle land)
Productization (Python SDK + reconcile the 4 documented-but-unbuilt enterprise plugins + generic onboarding — the larger strategic play); real ~814K ICIJ corpus run; a fresh audit in another dimension.

---

## Stale assumptions to retire

1. **`NEXT_STEPS_2026-06-03.md` is now well behind reality.** It reflects Track P/Q/R/H closed but knows nothing of: the tenant-isolation sweep (F1/F2/F3, audit doc #303), the batch-path parity work (#304), or the vector-persistence fix (#305). A planning-doc refresh (or a fresh `NEXT_STEPS_2026-06-04.md`) should absorb these + the silent-bug-hunt backlog (C/D/E + the noted findings). The off-path "batch delete/update tenant-index gap" line is DONE (#298).
2. **Memory `project_track_p_m3_m7_deferred`** — current (M3+M7 done). **New memories this session**: `feedback_in_memory_index_drift_test_design` (CC6 test discipline), `project_tenant_isolation_sweep_2026_06_04` (don't re-commission). No action; listed for awareness.
3. **`/api/metrics` "for dashboard" framing** — it's now admin-only (#300); not tenant-facing.
4. **The vector subsystem's "rebuilt by the application on restart" comment** was unfulfilled until #305 — it's now rebuilt by storage on load. (Comment updated in #305.)
5. **#240/#241 are CLOSED** (not carried) — drop them from any "inherited PRs" tracking.
6. **`CLAUDE.md` § "Partitioned shard maps"** still references `forEachNodeUnlocked` "(and edge variants)" — `forEachEdgeUnlocked` removed in #261 (carried stale note; small).

---

## Open questions for the user

1. **C/D/E bundle** — do now (the user's standing choice) or after #305 merges. The session paused before starting it.
2. **persist-HNSW vs rebuild-on-load** at scale — #305 ships rebuild; escalate to persist-HNSW only if a real workload's startup cost is unacceptable (needs a real-embeddings measurement, not synthetic).
3. **Next track after the bundle** — no earned critical path; productization is the standing strategic candidate.

---

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

---

## How to use this handoff

1. Read this first.
2. **Merge #305** (CRITICAL vector-persistence fix) when CI is green.
3. Then `CLAUDE.md` § "Orient first" + `NEXT_STEPS_2026-06-03.md` (stale — see §6). The queued work is the **C/D/E bundle**; after that there's no earned critical path — run a planning checkpoint.
4. If picking up the bundle, read `pkg/storage/persistence_replay.go` (C), `pkg/storage/batch_executor.go` `executeDeleteNode` (E), `pkg/storage/node_operations.go` `RemoveNodeProperties` (D), and memory `feedback_in_memory_index_drift_test_design` for the test discipline.
