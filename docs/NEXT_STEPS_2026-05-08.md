# Plan: Next Steps (graphdb)

**Sources**:
- Audit fix plan: [`AUDIT_fixes_plan_2026-05-06.md`](./AUDIT_fixes_plan_2026-05-06.md) — Track A in flight (A1, A2, A3a done; A3b–A7 pending)
- Killer-features synthesis: [`FEATURES_synthesis_2026-05-08.md`](./FEATURES_synthesis_2026-05-08.md) — three lead candidates identified

**Active position on the synthesis's three open questions** (locked in this plan; revisit if reality contradicts):

1. *Sequencing*: ship `/v1/embeddings` in parallel with audit Track A (it doesn't expose graph data, only embeddings). Gate **GraphRAG retrieval** on completion of A5+A6 (tenant middleware + query/GraphQL scope) so the new endpoint inherits tenant isolation rather than re-introducing the same Security CRIT under a new URL.
2. *Multi-tenant LSA*: ship `/v1/embeddings` v1 with a **documented caveat** that LSA is currently a single global model (tenant B's writes influence tenant A's semantic results). Per-tenant LSA is a follow-up (F1.1).
3. *Cypher / GQL*: not in this 90-day plan. Reviewer flagged shipping a half-baked Cypher is worse than absent. Revisit after Track A completes and the storage interface extraction (long-horizon) is scoped.

Each task below is **one logical commit / one PR**. Tracks run in parallel where the dependency graph allows.

---

## Track M — Merge in-flight PRs (this week, blocking)

### M1. Review and merge PR #2 — `fix(api): preserve error wrap chain + dedupe path extraction`

- [ ] Reviewer reads the diff (~5 files, +101/−42)
- [ ] Confirm the slight error-message changes are acceptable
- [ ] Confirm the strict 400-on-missing-prefix is desired (vs. 404)
- [ ] Merge to main
- **Acceptance**: PR #2 closed, main has the audit HIGH code-quality fixes

### M2. Review and merge PR #3 — `docs: 2026-05-06 multi-specialist audit reports + synthesis + fix plan`

- [ ] Reviewer scans the audit reports for accuracy of code references
- [ ] Confirm the dated-filename convention vs. plain
- [ ] Merge to main
- **Acceptance**: All audit artifacts under `docs/`, repo root clean

### M3. Review and merge PR #4 — `fix(api): JWT_SECRET fail-closed`

- [ ] Reviewer confirms the dev-only test secret is acceptable
- [ ] CI runs all tests successfully
- [ ] Merge to main
- **Acceptance**: PR #4 closed; deploy environments now must set `JWT_SECRET` explicitly

### M4. Review and merge PR #5 — `feat: add pkg/tenantid leaf package`

- [ ] Reviewer confirms the `tid := effectiveTenantID(tenantID)` boundary-conversion pattern reads well
- [ ] Confirm dual-name (`DefaultTenantID = "default"` + `tenantid.Default`) is acceptable as transitional
- [ ] Merge to main
- **Acceptance**: `pkg/tenantid` is in main; A3a's PR rebases cleanly

### M5. Rebase PR #6 (A3a) on main after M4 lands

- [ ] `git rebase main` on `feat/audit-a3a-storage-tenant-signatures`
- [ ] Force-push if needed; CI green
- [ ] Reviewer reads through; merge
- **Acceptance**: PR #6 merged; the additive `*ForTenant` variants are in main, ready for A3b's enforcement to land on top

### M6. Decide PR #1 disposition (Syntopica `property_filter`)

- [ ] Confirm the `property_filter` privacy boundary is still wanted standalone (it pre-dates the broader Track A plan)
- [ ] Check if Syntopica has merged its side (PR #75 in `syntopica-v2`)
- [ ] Either merge into main, hold for synchronized rollout, or close as superseded
- **Acceptance**: explicit decision recorded; no ambiguous open PR

---

## Track A — Audit (security CRITICAL closure, sequential)

Continues from `AUDIT_fixes_plan_2026-05-06.md`. Each task assumes the previous merged.

### A3b. Enforce `matchesTenant` in storage; remove `GetAllNodes`

- [ ] Add the `matchesTenant` check inside `GetNodeForTenant`/`UpdateNodeForTenant`/`DeleteNodeForTenant` and edge equivalents — return `ErrNodeNotFound` (not `ErrCrossTenant`; no existence leak)
- [ ] Delete `GetAllNodes()` entirely. Migrate the three known callers (`pkg/api/handlers_nodes.go:18`, `pkg/constraints/uniqueness.go:93`, `cmd/graphdb-replica/main.go:97`) to `GetAllNodesForTenant("")`
- [ ] Add internal helper `getNodeRefForTenant(id, tenantID) *Node` (lowercase, package-private) — no clone, RLock held by caller. For use by post-filter loops in `pkg/api/handlers_vectors.go`
- [ ] Update `TestGetNodeForTenant_WrapsGetNode/non-empty tenantID is currently ignored (A3a)` from "ignore tenantID" to "non-matching tenant returns ErrNodeNotFound" — A3a's trip-wire fires
- [ ] **Advisor call** before starting (security-critical, multi-package ripple)
- **Acceptance**:
  - Cross-tenant `GetNodeForTenant` returns `ErrNodeNotFound` (not the node, not a different error)
  - Existing `pkg/api` test suite still passes
  - Race-detector clean: `go test -race ./pkg/storage/... -count=3`
  - Lint baseline unchanged on touched files

### A4. Migrate node reads from global `gs.mu` to shard locks

- [ ] Extend `rlockShard`/`lockShard` (already wired for edges) to node reads
- [ ] Audit every writer that mutates `gs.nodes` to take per-shard write lock in addition to (or instead of) `gs.mu.Lock`
- [ ] Add concurrent-read benchmark (4, 8, 16 reader goroutines) with before/after numbers in commit message
- [ ] **Advisor call** before starting (lock-ordering analysis)
- **Acceptance**:
  - `go test -race ./pkg/storage/... -count=3` clean
  - Benchmark shows ≥ 2× throughput at 4 reader goroutines
  - No new deadlocks under race detector

### A5. Add `withTenant` middleware to remaining REST routes

- [ ] Wrap `/nodes/`, `/edges/`, `/query`, `/graphql`, `/traverse`, `/shortest-path`, `/algorithms` with `withTenant` in `pkg/api/server.go`
- [ ] Confirm chain order: `requireAuth → withTenant → handler`
- [ ] New integration test: request without tenant context returns 400
- **Acceptance**:
  - All non-public routes have `withTenant`
  - Existing tests pass (they should already provide tenant context via `setupTestServer`)
  - Cross-tenant test: tenant-A request targeting tenant-B node returns 404

### A6. Tenant scope in query executor + GraphQL resolvers

- [ ] `pkg/query`: `Executor.ExecuteWithContext` takes/uses tenant context; scope `MATCH` iteration to `GetAllNodesForTenant`
- [ ] `pkg/graphql`: inject tenant from request context into resolvers; thread through to all node/edge lookups
- [ ] Existing query/GraphQL tests run under explicit tenant; add cross-tenant negative tests
- **Acceptance**:
  - `grep -rn 'TenantID\|FromContext' pkg/query/ pkg/graphql/` shows tenant scoping in executor and every resolver
  - Cross-tenant query test (run from tenant A, target tenant B node) returns empty result, not the leaked node
  - Existing query/GraphQL tests pass after being updated to be tenant-aware

### A7. Cross-tenant regression test suite

- [ ] New `pkg/api/cross_tenant_test.go`
- [ ] One test per route flagged in the security audit (CRUD/list/query/traverse/algorithm/vector/index)
- [ ] Each test: tenant A request targeting tenant B's resource returns 404 (or empty), never leaks
- **Acceptance**:
  - Test count: at least one per CRIT/HIGH-flagged route
  - All pass; runs under 5 seconds total (in-memory storage)
  - Lock the regression in: a future code change can't silently re-introduce the bug without this test failing

---

## Track F — Features (parallel-ok with Track A where noted)

### F1. `/v1/embeddings` OpenAI-compatible endpoint *(parallel with A3b–A7)*

- [ ] New `pkg/api/handlers_embeddings.go`. Route `/v1/embeddings` registered in `pkg/api/server.go` (with `requireAuth + withTenant` middleware once A5 lands; before then, `requireAuth` only)
- [ ] Mirror OpenAI request/response shape: `{ "model": "lsa", "input": [...], "encoding_format": "float" }` → `{ "data": [{"embedding": [...], "index": 0}], "model": "lsa", "usage": {"prompt_tokens": N, "total_tokens": N} }`
- [ ] Backed by `pkg/search/lsa.go` — deterministic, seeded
- [ ] Tests: round-trip with a sample LangChain client request shape; pin the response schema; one adversarial test (empty input, oversized input, non-UTF8)
- [ ] Document in `docs/API.md`: feature copy includes the LSA scale ceiling caveat (~100K-500K docs at 200 dims) and the multi-tenant caveat (single shared LSA model in v1)
- **Acceptance**:
  - LangChain `OpenAIEmbeddings(api_base="http://graphdb/v1")` client can hit the endpoint and get back well-formed embeddings
  - At 100K-node corpus, P50 latency < 50ms / P99 < 200ms (single-node)
  - Schema-pinned test passes (the response shape doesn't drift)

### F1.1. (Follow-up) Per-tenant LSA model

- [ ] Adapt the per-tenant index pattern in `pkg/search/tenant_indexes.go` to LSA models in `pkg/search/lsa.go`
- [ ] Each tenant gets an LSA model built from its own corpus only
- [ ] Migration: when a tenant first accesses semantic search, build their LSA model on demand
- [ ] Update `/v1/embeddings` to route per-tenant
- **Acceptance**:
  - Cross-tenant test: writes to tenant B do not change tenant A's embedding output for the same input
  - Bench: per-tenant LSA build cost is bounded by per-tenant corpus size, not global

### F2. GraphRAG retrieval — `expand_hops` + SSE streaming on `/hybrid-search` *(blocked on A5 + A6)*

- [ ] Add `expand_hops int` field to `HybridSearchRequest` (0 = current behavior, no expansion)
- [ ] After pagination in `handleHybridSearch`, fan out via `GetOutgoingEdges` for `expand_hops` levels with bounded fan-out (default cap: 100 nodes/level)
- [ ] Add SSE response variant: `Accept: text/event-stream` returns `event: hit\ndata: {...}\n\n` per result + final `event: done`
- [ ] Document in `docs/API.md` with reproducible ICIJ Offshore Leaks corpus benchmark
- [ ] Tests: 1-hop expansion includes neighbors; 0-hop matches existing behavior; SSE stream parses correctly with curl `-N` flag
- **Acceptance**:
  - `expand_hops` parameter accepted; results include neighbor nodes
  - SSE response when `Accept: text/event-stream`; JSON when `application/json`
  - Cardinality cap prevents fan-out explosion (3-hop with 100/level = 1M not unbounded)
  - Tenant isolation inherited from A5/A6 (cross-tenant test passes for the new param)

### F3. Compliance API package *(parallel with F2; rides existing work)*

- [ ] New `pkg/api/handlers_compliance.go`: REST surface tying `pkg/masking/` + per-tenant property filter + `pkg/audit/` together
- [ ] Endpoints: `GET /v1/compliance/audit-log` (paginated, filtered), `POST /v1/compliance/masking-policy` (configure per-tenant), `GET /v1/compliance/masking-policy/{tenant}` (read current policy)
- [ ] Swagger annotations on all endpoints
- [ ] Reference SOC2/GDPR integration guide in `docs/COMPLIANCE.md`
- [ ] Tests: audit log returns immutable entries; masking policy applies to all read paths (Get/List/Search/Vector); cross-tenant policy access denied
- **Acceptance**:
  - `/v1/compliance/audit-log` returns the tenant's audit events in append-only order
  - Setting a masking policy on a property hides it from all read paths (the existing per-tenant infra already does this — the test pins it)
  - SOC2 reviewer can run the integration guide end-to-end and produce a compliance artifact

---

## Sequencing graph

```
M1 → M2 → M3 → M4 → M5 → M6  (housekeeping; parallel-ok within track)
            ↓
            A3b → A4 → A5 → A6 → A7   (security; sequential)
                       ↓    ↓
                       F2 (gated on A5+A6)
            F1 (parallel-ok with A3b-A7)
            F3 (parallel-ok with F2)
            F1.1 (after F1)
```

**Critical path**: M4 → A3b → A5 → A6 → F2. Everything else can shift left or right.

---

## Out of scope (for this 90-day window)

- **Cypher / GQL** — months-out, gated on storage interface extraction. Revisit after Track A completes.
- **Storage interface extraction (audit Architecture HIGH-1)** — the long-horizon unlock named in both audits. Worth its own dedicated planning effort; too big for an item in this list.
- **Geospatial / temporal data-model features** — reviewer flagged compounding risk with the LSA bottleneck. Defer until F1 + F2 ship and a real perf bench tells us where headroom is.
- **Performance tracks B2/B3/B4 (HNSW visited-set sync.Pool, cosine norm hoisting, LSM cache lock)** — not on the critical path; pick up opportunistically when touching adjacent code.
- **Code-quality tracks C1/C2/C3/C4** — opportunistic, no urgency.
- **Audit lint discrepancy investigation (D3)** — worth doing but not blocking anything.

---

## Decision points captured for future revisit

1. **Per-tenant LSA timing**: F1.1 is named as a follow-up but could be done before F1 ships. Decide once F1's tests reveal real impact magnitude.
2. **GraphRAG SSE vs WebSocket**: SSE is simpler; WebSocket allows bidirectional. Currently planning SSE — confirm during F2 design.
3. **Compliance API surface**: REST-only as planned, or also expose via GraphQL? Decide during F3 scoping.
4. **Cypher revisit timing**: when storage interface extraction is staffed, fold Cypher into that quarter's plan. Not before.
