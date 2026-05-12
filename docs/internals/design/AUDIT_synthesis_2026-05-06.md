# Audit Synthesis — graphdb (2026-05-06)

**Method**: four parallel specialist agents (security, performance, code-quality, architecture) plus automated tooling baseline (`go vet`, `golangci-lint --no-config`). Individual reports: [`AUDIT_security_2026-05-06.md`](./AUDIT_security_2026-05-06.md), [`AUDIT_performance_2026-05-06.md`](./AUDIT_performance_2026-05-06.md), [`AUDIT_code_quality_2026-05-06.md`](./AUDIT_code_quality_2026-05-06.md), [`AUDIT_architecture_2026-05-06.md`](./AUDIT_architecture_2026-05-06.md).

**Codebase**: 77K LOC production, 102K LOC tests, 42 packages. Module `github.com/dd0wney/cluso-graphdb`. Recent activity: heavy "Track A" lint cleanup (errcheck/ineffassign/SA4010), property_filter privacy work for Syntopica integration, hybrid search REST surface.

---

## Executive verdict

The kernel layering is correct and the code-quality cleanup pass is bearing fruit. **But multi-tenancy isolation is half-shipped — the search and vector paths got it; the foundational CRUD and query surfaces never did.** Five cross-cutting findings are independently confirmed by multiple agents and share root causes. Fix in priority order:

1. **Cross-tenant CRUD + query exposure** (security CRITICAL × 3 + architecture root cause). Any authenticated user can read/overwrite/delete any other tenant's nodes by ID enumeration; `GET /nodes` dumps the entire multi-tenant corpus; `/query` and `/graphql` traverse the whole graph. **Do not co-locate multi-tenant data in a shared instance until this is fixed.**
2. **`GetNode` is the hot path that gates everything** (performance HIGH × 2). Clones the node + properties on every read (5× GC pressure), takes the global mutex (serializes all node reads). Same operation that's missing tenant enforcement.
3. **WAL fsync per write by default** (performance HIGH). Sets the p99 write latency floor at ~50–200µs per operation. `BatchedWAL` is wired but not the default.
4. **JWT silent secret rotation** (security HIGH). Misconfigured staging environments silently rotate per restart unless `GRAPHDB_ENV="production"` exactly.
5. **No storage interface** (architecture HIGH). Every consumer takes `*storage.GraphStorage` directly. This is *why* tenant enforcement is hard: there's no contract layer where it could be added uniformly.

---

## Cross-cutting themes (where multiple agents converged)

### Theme A — Tenant isolation is the dominant gap

| Agent | Finding | File |
|---|---|---|
| Security | CRIT #1: `/nodes/{id}` GET/PUT/DELETE no tenant check | `handlers_nodes.go:74,109,124` |
| Security | CRIT #2: `GET /nodes` calls `GetAllNodes()` (global) | `handlers_nodes.go:18` |
| Security | CRIT #3: `/query` and `/graphql` zero tenant scope | `server.go:36,39` |
| Security | HIGH #5: `/traverse`, `/shortest-path`, `/algorithms` lack `withTenant` | `server.go:52-56` |
| Security | LOW #9: vector index create/delete unscoped | `handlers_vectors.go:148-240` |
| Architecture | MED-3: tenant concept split across two packages, no shared `TenantID` type | `tenant_operations.go`, `pkg/tenant/` |
| Performance | (compounded) `parallel_aggregation` partitions by ID range, no tenant filter | `parallel_aggregation.go:66` |

**Root cause**: `*storage.GraphStorage` has *no tenant-aware point-lookup methods*. The handlers that *do* enforce tenants (search, vector search) had to retrofit it via post-filter loops. Handlers that don't post-filter (CRUD, query, traverse) silently leak.

**Why this matters most**: the `property_filter` privacy work shipped recently (graphdb#1) added the post-filter pattern to the vector path because the alternative — server-side property gating — was already missing on what should have been a privacy-safe surface. The audit finds the same underlying bug class is still present everywhere else. Phase 2a of the Syntopica integration (which writes private `:Submission` data) **must not roll out** until findings #1–3 are closed; otherwise vector search is privacy-bounded but `/nodes/{id}` GET is not.

### Theme B — `GetNode` is the chokepoint *and* the security gap

| Agent | Finding |
|---|---|
| Performance | HIGH-1: `GetNode` clones every read → 5× GC pressure on hot paths |
| Performance | HIGH-2: `GetNode` takes global RLock; shard locks exist but only used for edges |
| Security | CRIT #1: `GetNode` is tenant-blind |

The same single function is the dominant performance issue, the dominant lock contention point, and the dominant tenant-isolation failure. A correct refactor adds a tenant parameter, drops the clone for internal callers, and switches to shard locking. Three findings, one operation.

### Theme C — God-struct + missing service layer

| Agent | Finding |
|---|---|
| Architecture | HIGH-2: `pkg/api.Server` is a 30-field god-struct with 18 internal package imports |
| Architecture | MED-1: REST and GraphQL plumb separately into `*GraphStorage` |
| Code-quality | LOW #8: `handle*` (router) vs `*` (logic) naming pattern only partially enforced |

Lack of a service layer means handlers reach directly into storage; GraphQL resolvers reach directly into storage; there's no shared use-case object. Schema changes get implemented twice. This is also why fixing tenant isolation is hard — there's no central place to put `WithTenant(ctx)` and have every code path inherit it.

### Theme D — Lint cleanup left design-level patterns intact

The Track A passes (errcheck, ineffassign, SA4010) closed thousands of static-analysis findings. They couldn't catch:

- **`fmt.Errorf("%s", sanitizeError(err, "op"))`** at 13 algorithm-handler sites. Returns a non-nil error (errcheck happy), strips the wrap chain (`errors.Is`/`errors.As` silently no-op). *Fixed today — commit `cb291db`.*
- **Duplicate `TrimPrefix`+`TrimSuffix` path extraction** at 8 handler sites. *Fixed today — commit `cb291db`.*

Deep-read audit beats incremental linter cleanup for this class.

---

## Prioritized action list

### Sprint 1 (urgent — security)

| # | Action | Effort | Files |
|---|---|---|---|
| S1 | Add `withTenant` middleware to `/nodes/`, `/edges/`, `/query`, `/graphql`, `/traverse`, `/shortest-path`, `/algorithms` route registrations | S | `pkg/api/server.go` |
| S2 | Add tenant validation in `GetNode`/`UpdateNode`/`DeleteNode`/`GetEdge`: take a tenantID arg, return `ErrNotFound` (not `ErrCrossTenant` — don't leak existence) on mismatch | M | `pkg/storage/node_operations.go`, `edge_operations.go` |
| S3 | Replace `GetAllNodes()` in `listNodes` with `GetAllNodesForTenant` | XS | `pkg/api/handlers_nodes.go:18` |
| S4 | Inject tenant context into query executor + GraphQL resolvers; scope `MATCH` iteration | L | `pkg/query/executor*.go`, `pkg/graphql/` |
| S5 | Fail-closed on missing `JWT_SECRET` regardless of `GRAPHDB_ENV` | XS | `pkg/api/server_init.go:66-78` |

After S1–S4 land, **add an integration test that runs a request from tenant-A targeting a tenant-B node ID for every CRUD verb**; assert 404 (or 403). Lock the regression in.

### Sprint 2 (performance — read path)

| # | Action | Effort | Files |
|---|---|---|---|
| P1 | Add `getNodeRef` (or rename internal-only callers) returning `*Node` without clone, RLock held by caller | M | `pkg/storage/node_operations.go` |
| P2 | Migrate `GetNode` to shard-lock pattern (already wired for edges) | M | `pkg/storage/node_operations.go` + writers |
| P3 | Switch production config default to `EnableBatching: true, FlushInterval: 10ms` | XS | `pkg/storage/storage.go:24` |

P1 + P2 + S2 are conceptually one refactor of the same hot path. Land them as a single PR if scheduling allows.

### Sprint 3 (performance — query layer)

| # | Action | Effort |
|---|---|---|
| P4 | `sync.Pool` for HNSW visited maps + priority queues | S |
| P5 | Hoist `||query||` out of cosine inner loop (or store `||v||` per `hnswNode` at insert) | S |
| P6 | LSM `BlockCache.Get` two-level lock (RLock for lookup, Lock only on miss/promote) | M |

### Code-quality follow-ups (sprint 4 or anytime)

| # | Action | Status |
|---|---|---|
| Q1 | Fix `fmt.Errorf("%s", sanitizeError(...))` × 13 in `handlers_algorithms_generic.go` | ✅ Done — `cb291db` |
| Q2 | Dedupe path extraction × 8 via `ExtractString`/`ExtractParts` | ✅ Done — `cb291db` |
| Q3 | Migrate legacy `extractPathParam` callers (3 more sites in `handlers_tenant.go`) to the new helpers | Not started |
| Q4 | Standardize error-message construction (`fmt.Sprintf` over `+` concatenation) | Not started |
| Q5 | Centralize handler defaults into `pkg/api/config.go` (`maxDimensions`, `maxK`, `snippetLen`) | Not started |
| Q6 | Split `handlers_vectors_test.go` (1346 LOC) by feature | Not started |

### Architecture (longer horizon — pays for itself when ready)

| # | Action | Effort | Note |
|---|---|---|---|
| A1 | Define role interfaces in `pkg/storage` (`NodeReader`, `EdgeWriter`, `Transactor`, `Indexer`) | L | Enables S2, S4, future testability |
| A2 | Extract service layer (`NodeService`, `SearchService`, `VectorService`) between handlers/resolvers and storage | L | Resolves REST/GraphQL duplication; gives a place to put `WithTenant` once |
| A3 | Define canonical `pkg/tenantid.TenantID` type, used by both `pkg/storage` and `pkg/tenant` | XS | Sequencing: do this *before* S2 to avoid string-typed proliferation |
| A4 | Replace `editions.Current` global with injected `FeatureGate` interface | M | Unblocks per-tenant feature variation, testability |

---

## What's already shipped (this session)

| Commit | Branch | Scope |
|---|---|---|
| `cb291db` | `fix/audit-high-error-wrap-and-path-extract` | HIGH code-quality fixes (Q1 + Q2). Branch is local-only — not pushed yet. |

**Branch is not pushed.** Pending your call on whether to push and open a PR, or fast-forward to main, or merge into the existing property_filter PR (#1 has a different base; merging would require a rebase).

---

## Lint baseline status

```
golangci-lint run --no-config ./...
  errcheck:  50
  staticcheck: 27
  unused:    15
                      total: 92 issues
```

The 92 issues *contradict* the recent "Track A errcheck cleanup (114 → 0)" commit messages. Most likely explanation: the cleanup was scoped to a stricter project config (the broken `.golangci.yml` I noted earlier), while `--no-config` runs the full default linter set. The `unused` findings include `testEdge`, `testGraphStorageWithNodes`, `testCrashRecovery` in `pkg/storage/testhelpers_test.go` — these look like test-helper infrastructure that was built but never adopted. Worth understanding before deleting.

---

## What I'd recommend *not* doing

- **Don't fix `.golangci.yml` schema first.** It's a distraction. The audit found design-level findings that no linter catches.
- **Don't refactor to interfaces (A1) before fixing security S1–S5.** Security debt is bleeding right now; architectural debt is paid in slower future work. Bleeding wins.
- **Don't try to fix all 92 lint findings at once.** Most are likely benign post-cleanup drift. Triage by package and address as part of touching the package.
- **Don't add new feature work before S1–S5 land** — especially anything that adds new tenant-bearing data to a multi-tenant deployment.

---

## Where these reports should live

The four `AUDIT_*.md` files are currently at the repo root. The existing `docs/AUDIT_DURABILITY.md` precedent suggests they belong under `docs/` alongside this synthesis. Suggest a `git mv` of all four when you commit the audit artifacts.
