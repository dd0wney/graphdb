# Plan: Audit Fixes (graphdb)

**Source**: [`AUDIT_synthesis_2026-05-06.md`](./AUDIT_synthesis_2026-05-06.md) and the four specialist reports.
**Goal**: convert the synthesis's prioritized actions into atomic tasks. Each task is one logical commit.
**Already done**: Q1 + Q2 (error wrap chain + path-extraction dedup) — committed as `cb291db` on branch `fix/audit-high-error-wrap-and-path-extract` (local, unpushed).

Tracks below are ordered by priority. **Track A is sequential** (each task builds on the previous). Tracks B–E are parallel-ok with A and with each other.

---

## Track A — Tenant isolation (CRITICAL; sequential)

**Why sequential**: each task assumes the previous landed. The middleware in A5 is a no-op without the storage check in A3. The query/GraphQL work in A6 follows the storage pattern set in A3.

### A1. Define canonical `TenantID` type

- [ ] Create `pkg/tenantid/` (or use `pkg/tenant/types.go`) with `type TenantID string`
- [ ] Constants: `DefaultTenant TenantID = "default"`, `Empty TenantID = ""`
- [ ] Helpers: `(t TenantID) String() string`, `(t TenantID) IsEmpty() bool`
- [ ] Migrate `pkg/storage/tenant_operations.go` and `pkg/tenant/store.go` to use the new type
- [ ] All other call sites that today take `string` for tenant ID switch to `TenantID`
- **Acceptance**:
  - `grep -rn 'tenantID string' pkg/` returns zero hits in production code (test code allowed)
  - `go build ./...` clean; full test suite passes
  - The new type is a type alias *or* a defined type — defined type preferred (catches accidental `string` mixing)
- **Risk**: many call sites. Use the compiler as the migration tool — change the central definition first, then fix each compile error.
- **Why first**: prevents string-typed proliferation when A3 adds tenant args to storage methods.

### A2. Fail-closed on missing `JWT_SECRET`

- [ ] Remove the `GRAPHDB_ENV == "production"` guard at `pkg/api/server_init.go:66-78`
- [ ] If `JWT_SECRET` is empty, return error from server initialization regardless of env
- [ ] Update local-dev docs / `.env.example` to specify a fixed dev-only secret
- [ ] Test: server fails to start with empty `JWT_SECRET`
- **Acceptance**:
  - Server refuses to start with empty `JWT_SECRET` in any env
  - Existing dev workflow still works when `.env` provides a value
  - No silent random-secret generation path remains
- **Why early**: XS effort, eliminates a HIGH security finding, independent of A1.

### A3. Add tenant validation to storage point-lookups

- [ ] Add `tenantID TenantID` parameter to: `GetNode`, `UpdateNode`, `DeleteNode`, `GetEdge`, `UpdateEdge`, `DeleteEdge`, `GetAllNodes` (rename to `GetAllNodesForTenant` if not already), `GetAllEdges`, all batch variants
- [ ] On tenant mismatch: return `ErrNotFound` (NOT `ErrCrossTenant` — don't leak existence of other tenants' data)
- [ ] Internal helper `getNodeRefForTenant(id, tenantID)` returning `*Node` (no clone, RLock held by caller) for use by post-filter loops
- [ ] Update every call site (algorithms, query, GraphQL, REST handlers, search, vector search, replication)
- [ ] Update existing post-filter loops (e.g. `vectorSearch` in `handlers_vectors.go`): instead of fetching then filtering, pass tenant ID into the storage call
- **Acceptance**:
  - `grep -rn 'GraphStorage.GetNode\b' pkg/` returns zero hits in production (replaced everywhere)
  - Cross-tenant access returns 404 (matches the no-leak semantic)
  - Existing tenant-aware tests continue passing; existing tenant-blind tests are updated to be tenant-aware
  - Benchmark: vector search throughput unchanged or better (the post-filter Clone is gone for the dropped candidates)
- **Risk**: largest single commit on this track. Suggest landing behind a feature flag if the team prefers staged rollout.
- **Combined with P1 + P2**: this commit also closes `Performance HIGH-1` (clone elimination via `getNodeRefForTenant`) and *prepares for* `Performance HIGH-2` (shard-locking, which becomes A4).

### A4. Migrate node reads from global mutex to shard locks

- [ ] Extend the existing `rlockShard`/`lockShard` helpers (used by edges) to node reads
- [ ] Audit every writer that mutates `gs.nodes` to take the per-shard write lock in addition to (or instead of) `gs.mu.Lock`
- [ ] Run race-detector tests under concurrency
- [ ] Add concurrent-read benchmark (4, 8, 16 goroutines) with before/after numbers in the commit message
- **Acceptance**:
  - `go test -race ./pkg/storage/... -count=3`: clean
  - Benchmark shows ≥ 2× throughput at concurrency = 4 reader goroutines
  - No new lock-ordering deadlocks under `go test -race`
- **Risk**: correctness-sensitive. The audit explicitly flagged this as "not a line edit." Suggest a focused spike before committing.
- **Why after A3**: A3's tenant-arg refactor is invasive enough that combining with the lock change makes review harder. Land them sequentially.

### A5. Add `withTenant` middleware to remaining routes

- [ ] At `pkg/api/server.go`, wrap these mux registrations with `withTenant`:
  - `/nodes/` (and `/nodes`)
  - `/edges/` (and `/edges`)
  - `/query`
  - `/graphql`
  - `/traverse`
  - `/shortest-path`
  - `/algorithms`
- [ ] Confirm middleware chain order is `requireAuth → withTenant → handler`
- [ ] Tests: requests without a tenant header / context fail with 400 (or appropriate status)
- **Acceptance**:
  - All non-public routes have `withTenant` in the chain
  - Existing tests pass (they should already provide tenant context via `setupTestServer`)
  - A new integration test confirms a request without tenant context is rejected
- **Why after A3**: a `withTenant` middleware without storage-side enforcement *looks* fixed but still leaks. Land in this order.

### A6. Tenant scope in query executor and GraphQL resolvers

- [ ] `pkg/query`: add tenant context to `Executor.ExecuteWithContext`; scope `MATCH` node iteration to `GetAllNodesForTenant`
- [ ] `pkg/graphql`: inject tenant from request context into resolvers; thread through to all node/edge lookups
- [ ] Existing query-DSL tests run under explicit tenant; add cross-tenant negative tests
- [ ] Update GraphQL schema docs if the surface changes
- **Acceptance**:
  - `grep -rn 'TenantID\|FromContext' pkg/query/ pkg/graphql/` shows tenant scoping in the executor and every resolver
  - Cross-tenant integration test (run query from tenant A targeting node from tenant B) returns empty result, not the leaked node
  - Existing query/GraphQL tests pass after being updated to be tenant-aware
- **Risk**: largest scope on this track. Touches 25+ files in `pkg/graphql/`.

### A7. Cross-tenant regression test suite

- [ ] New file `pkg/api/cross_tenant_test.go`
- [ ] For every CRUD/list/query/traverse/algorithm/vector route: assert that a request from tenant A targeting tenant B's node ID returns 404 (or empty result)
- [ ] Include a test for `/vector-indexes/{name}` (delete and get) — covers Security LOW #9
- **Acceptance**:
  - At least one test per route flagged in the security audit
  - All tests pass
  - Test runs in under 5 seconds (uses in-memory storage)
- **Why last in track**: this is the regression net that locks all of A1–A6 in place. Without it, future code changes can silently re-introduce the bug.

---

## Track B — Performance (parallel-ok with A)

### B1. Default WAL to batching mode in production config

- [ ] Change default in `pkg/storage/storage.go` config: `EnableBatching: true`, `FlushInterval: 10ms`
- [ ] Confirm `BatchedWAL` path is the production default
- [ ] Add a benchmark comparing single-write p99 latency before/after
- [ ] Document the durability window in `docs/AUDIT_DURABILITY.md`
- **Acceptance**:
  - p99 single-write latency drops below the previous fsync floor (~50–200µs → low double digits)
  - `go test -count=1 ./pkg/storage/... ./pkg/wal/...` passes
  - Crash-recovery tests still pass (the durability window is bounded; recovery should still replay correctly)
- **Risk**: relaxes durability guarantee to a 10ms window. Document explicitly.

### B2. `sync.Pool` for HNSW search allocations

- [ ] In `pkg/vector/hnsw_search.go`: pooled `map[uint64]bool` for visited sets
- [ ] Pooled `priorityQueue` backing arrays for `candidates` and `w`
- [ ] Reset between uses; ensure no stale references survive
- [ ] Benchmark: alloc/op before/after for a representative search
- **Acceptance**:
  - `BenchmarkSearch` shows ≥ 50% reduction in `B/op` and `allocs/op`
  - Race-detector clean under concurrent search load
  - No correctness regression in `TestVectorSearch_*`

### B3. Hoist `||query||` out of cosine inner loop

- [ ] Pre-compute query norm once at `Search` entry
- [ ] Add `CosineDistanceWithQueryNorm(a, b []float32, normA float32) float32` in `pkg/vector/distance.go`
- [ ] Use the specialized variant in `searchLayer` and `searchLayerKNN`
- [ ] Optionally: store `||v||` per `hnswNode` at insert (extends the optimization to the stored side)
- [ ] Benchmark: ns/op before/after
- **Acceptance**:
  - Search benchmark shows measurable reduction in ns/op (target: ≥ 10% at 1536 dims, ef=100)
  - Existing `TestVectorSearch_ScoreCalculation` passes (proves the math is identical)

### B4. LSM BlockCache two-level lock

- [ ] In `pkg/lsm/cache.go`: `Get` takes RLock for lookup, escalates to Lock only on miss or every N hits to update LRU
- [ ] Or: replace `container/list`-based LRU with a concurrent-safe variant (e.g., `golang-lru/v2`)
- [ ] Concurrent benchmark before/after
- **Acceptance**:
  - Concurrent cache-hit benchmark shows ≥ 2× throughput at 4+ goroutines
  - Hit-rate metrics unchanged (prove the LRU semantic still holds)

---

## Track C — Code-quality follow-ups (anytime, low risk)

### C1. Migrate legacy `extractPathParam` callers

- [ ] Replace 3 sites in `handlers_tenant.go` (lines 148, 186, 251 per the original audit) with `ExtractString`
- [ ] Delete the `extractPathParam` function itself (now unused)
- **Acceptance**:
  - `grep -rn 'extractPathParam' pkg/api/` returns zero hits
  - Behavior unchanged on valid input; trailing-slash now consistently trimmed

### C2. Standardize error-message construction

- [ ] Replace `+`-concatenation patterns in error strings with `fmt.Sprintf`
- [ ] Touch only handler files; do not modify storage or domain layers
- **Acceptance**:
  - `grep -rn '\\".*\\" +.*err\\.Error\\(\\)\\|\\".*: \\" + ' pkg/api/handlers_*.go` returns zero hits
  - Lint clean
  - Tests pass

### C3. Centralize handler defaults

- [ ] New file `pkg/api/config.go` with `HandlerDefaults` struct
- [ ] Migrate: `maxDimensions`, `maxK`, `defaultM`, `defaultEfConstruction`, `defaultMetric`, `searchSnippetRunes`, etc.
- [ ] Existing handlers reference fields off the struct (read once at server init)
- **Acceptance**:
  - One source of truth for handler limits
  - Changing a default requires editing exactly one file
  - All existing tests pass without modification

### C4. Split `handlers_vectors_test.go` by feature

- [ ] Extract property-filter tests into `handlers_vectors_property_filter_test.go`
- [ ] Consolidate HNSW parameter tests into a table-driven helper
- [ ] Target: no test file > 700 LOC
- **Acceptance**:
  - `wc -l pkg/api/handlers_vectors*_test.go` shows no file > 700 lines
  - Test count unchanged
  - All tests pass

---

## Track D — Housekeeping

### D1. Move audit artifacts under `docs/`

- [ ] `git mv AUDIT_security.md docs/AUDIT_security_2026-05-06.md`
- [ ] `git mv AUDIT_performance.md docs/AUDIT_performance_2026-05-06.md`
- [ ] `git mv AUDIT_code_quality.md docs/AUDIT_code_quality_2026-05-06.md`
- [ ] `git mv AUDIT_architecture.md docs/AUDIT_architecture_2026-05-06.md`
- [ ] (Synthesis is already under `docs/`)
- [ ] Update any cross-references in the synthesis to use new paths
- **Acceptance**:
  - Repo root has no `AUDIT_*.md` files
  - `docs/` has all five audit artifacts (four reports + synthesis), dated
  - Synthesis cross-references updated and clickable

### D2. Push branch `fix/audit-high-error-wrap-and-path-extract` and open PR

- [ ] `git push -u origin fix/audit-high-error-wrap-and-path-extract`
- [ ] `gh pr create --base main --title "fix(api): preserve error wrap chain + dedupe path extraction"` with body referencing audit synthesis
- [ ] Optional: rebase onto `main` if `main` has advanced since branching
- **Acceptance**:
  - PR open against main
  - CI green
  - PR description references AUDIT_code_quality.md HIGH #1 and #2

### D3. Investigate Track A lint discrepancy

- [ ] Read `.golangci.yml` and identify the schema incompatibility (`output.formats` map vs slice)
- [ ] Fix the config to work with the installed `golangci-lint` version
- [ ] Run `golangci-lint run` (with config) and capture the issue count
- [ ] Compare against `--no-config` count (currently 92)
- [ ] If the configured run shows 0 (matching the Track A claims), document why `--no-config` finds 92 — likely the project disables some default linters
- [ ] If the configured run also shows non-zero, investigate which Track A commits drifted
- **Acceptance**:
  - `golangci-lint run` succeeds (no schema error)
  - Issue count documented in `docs/` or in CI output
  - CI workflow uses the correct config so future lint regressions are caught

---

## Suggested merge order

```
A1 → A2 (parallel) → A3 → A4 → A5 → A6 → A7
   ↘ B1 (parallel)
   ↘ B2 → B3 → B4 (any order, parallel)
   ↘ C1 → C2 → C3 → C4 (any order, parallel)
   ↘ D1 (anytime)
   ↘ D2 (anytime — independent of audit fixes)
   ↘ D3 (anytime — independent of audit fixes)
```

Total: **18 atomic commits** (5 done? — no, only 2 done: Q1+Q2 in commit cb291db; that's outside this plan since it's already merged). Of the 18 in this plan, **A3 and A6 are the largest** (multi-file refactors); everything else is XS–M.

---

## Anti-recommendations (out of scope for this plan)

- **Do not** start the storage interface extraction (Architecture HIGH-1 / A1 from synthesis) before Track A lands. The interface refactor is the right long-horizon move, but it's months of work. Tenant isolation is bleeding right now.
- **Do not** introduce a service layer (Architecture MED-1) as part of this plan. Wait until Track A lands and the new tenant pattern has stabilized; then a service layer is a clean follow-up.
- **Do not** bundle multiple tracks into one PR. Track A in particular is correctness-sensitive; reviewer attention is the limiting resource.

---

## Decision points for the implementer

1. **Land A3 + A4 separately, or combined?** Synthesis suggested combined (same hot path); this plan splits them so that A3's tenant correctness can ship without waiting on A4's lock-ordering analysis. Pick one.
2. **A1 — type alias or defined type?** A defined type catches accidental `string` mixing at compile time but breaks more existing code. Recommend defined type for the safety, but mention the trade-off.
3. **Feature flag for A3?** Some teams stage tenant-validation rollout behind a flag. Worth considering if the production environment has any single-tenant deployments that don't pass tenant context today.
