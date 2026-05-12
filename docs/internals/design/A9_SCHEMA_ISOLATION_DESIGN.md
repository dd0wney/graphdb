# A9 Design Spike — GraphQL Schema Per-Tenant Isolation

**Status:** spike output, not implementation. Date: 2026-05-08.
**Predecessors:** A6c-graphql-resolvers (PR #24, merged) closed cross-tenant *data* leakage through `/graphql`. A9 closes cross-tenant *metadata* leakage via GraphQL introspection — flagged as an out-of-scope follow-up in A6c spike (#21) §3.
**Goal:** map the implementation surface for per-tenant schema construction so the next PR can ship without scope-blowup.

## 1. What's leaking today

GraphQL clients can issue an introspection query:

```graphql
{ __schema { types { name } } }
```

The `graphql-go/graphql` library answers this from the schema's type registry. Today the schema is **constructed once at startup** in `pkg/api/server_init.go:54`:

```go
schema, err := graphql.GenerateSchemaWithLimits(graph, limitConfig)
```

Inside `pkg/graphql/limits.go:60` (and parallel functions in `aggregation.go`, `edges_schema.go`, `filtering_schema.go`, `mutations.go`, `schema.go`):

```go
labels := gs.GetAllLabels()
```

`GetAllLabels()` is **tenant-blind** — returns every label across every tenant. So a tenant-A caller running `__schema` sees `Person`, `Doc`, ... PLUS any tenant-B-only labels like `InternalSecretThing` or `PrivateProject`.

This isn't a data leak — `/graphql` queries themselves are tenant-scoped per A6c (#24) — but it's a **metadata leak**: tenant-A learns what tenant-B has labels for. For competitive intelligence or compliance contexts, that's enough to matter.

Audit Track A's umbrella suite (A7 #27) doesn't catch this because the row-set tests check returned data, not introspection responses.

## 2. Design decisions

### Q1: Per-tenant schema, lazily built, cached

**Decision: build once per tenant on first request; cache in `sync.Map[tenantID]*GraphQLHandler`.**

Alternatives considered and rejected:

- **Rebuild schema on every request.** Correctness-clean but ~50-200ms per request is a 1000× regression on the existing path's near-zero latency. Hard no.
- **Static schema with introspection filtering.** The `graphql-go/graphql` library handles `__schema` queries internally via the schema's type registry; intercepting them requires forking the library or shipping a parallel reflection layer. Real cost; not worth it when the cache approach is straightforward.
- **Eager schema build on tenant creation.** Avoids cold-start latency for the first request per tenant. v2 candidate — defer until cold-start latency is a real customer issue.

Trade-offs of the chosen approach:

- **Cold-start: 50-200ms per tenant**, paid once per process lifetime per tenant. Subsequent requests: cache hit (~0ms).
- **Memory: O(tenants × labels)**. Each schema is a few hundred KB. 100 tenants ≈ 50MB. Acceptable.
- **Cache invalidation: admin-triggered, per-tenant** (Q4 below).

### Q2: Storage primitive — `GetLabelsForTenant` already exists

`pkg/storage/tenant_operations.go:350` exposes `GetLabelsForTenant(tenantID)` — added in A6c. No new storage method needed; this is the foundation.

### Q3: Schema-builder migration — `*ForTenant` parallel functions

Add tenant-scoped variants of the schema-builder functions, passing `tenantID` through:

```
GenerateSchemaWithLimitsForTenant(graph, config, tenantID)
GenerateSchemaWithFilteringForTenant(graph, tenantID)
GenerateSchemaWithEdgesForTenant(graph, tenantID)
GenerateSchemaWithAggregationForTenant(graph, tenantID)
GenerateSchemaWithMutationsForTenant(graph, tenantID)
GenerateSchemaForTenant(graph, tenantID)
```

Each is a small refactor: replace `gs.GetAllLabels()` with `gs.GetLabelsForTenant(tenantID)`. The resolver closures inside each schema (created via `createNodeResolver`, etc.) already extract `tenantID` from `p.Context` per A6c #24 — no change needed there.

The existing tenant-blind functions stay (CLI / single-tenant / admin paths). Same additive pattern as A3a's `*ForTenant` storage variants.

### Q4: Cache invalidation — admin endpoint, per-tenant

`/api/v1/schema/regenerate` becomes per-tenant: regenerates the caller's tenant's cached schema, leaves others untouched. The `tenantID` is already in the request context.

**Auto-invalidation on every CREATE/DELETE is overkill** — every write would invalidate everyone's cache, defeating the cache. Document the lag: when a tenant adds a node with a new label, that label won't appear in their introspection until the admin endpoint is called for that tenant.

For tenants who care about new-label visibility (rare in practice; clients usually know their own labels), the admin endpoint is the escape valve.

### Q5: Concurrency — `sync.Map`, first-write-wins

The cache is `sync.Map[string]*GraphQLHandler`. On a cache miss, build the schema and `Store`. If two requests race, both compute the same schema and one wins; the loser's work is wasted but safe (schemas are idempotent for a given graph state at request time).

If contention proves real, switch to `sync.Map[string]*sync.Once` and gate construction. Don't pre-emptively over-engineer.

### Q6: Transitional state of `s.graphqlSchema` and `s.schemaLock`

The current `Server` struct fields:
- `graphqlHandler *gqlpkg.GraphQLHandler` — the shared handler
- `graphqlSchema graphql.Schema` — the underlying schema
- `schemaLock sync.RWMutex` — protects regeneration

After A9:
- `graphqlHandler` becomes `graphqlHandlers *sync.Map` (per-tenant)
- `graphqlSchema` is dead — it was only re-assigned during regenerate; remove
- `schemaLock` is transitional — only protects the now-removed `graphqlSchema`; remove with it

This is a small structural cleanup as part of A9; not a separate PR.

## 3. Implementation breakdown

| PR | Scope | Estimate |
|---|---|---|
| **#1 (this spike)** | Design doc | S — done |
| **#2** | Add `GenerateSchemaForTenant` + 5 parallel `*WithLimits/Filtering/Edges/Aggregation/Mutations` variants in `pkg/graphql/`. Pure additive. The tenant-blind functions stay. | S |
| **#3** | Server-side cache: `sync.Map` + lazy build + per-tenant lookup in `handleGraphQL`. Remove `graphqlSchema` field + `schemaLock`. Update `/api/v1/schema/regenerate` to per-tenant. | M |
| **#4** | HTTP-level cross-tenant introspection test: tenant-A `__schema { types }` does not show tenant-B's labels. Plus a row in `audit_regression_test.go` (`A9/introspection-tenant-isolated`). | S |

Critical path: `#2 → #3 → #4`.

## 4. Risks

1. **First-request latency spike for new tenants.** 50-200ms cold-start is invisible in benchmarks but visible to users. If this matters for a specific customer, eager-build on tenant creation is the v2 fix.

2. **Schema-cache memory growth on multi-tenant deployments.** 100 tenants × few-hundred-KB ≈ 50MB. 10,000 tenants ≈ 5GB. If this scale is realistic, the cache needs an eviction policy (LRU on least-recent-introspection?). v1 ships without eviction; flag if customer use case demands more.

3. **Schema lag after label changes.** A tenant adds a node with a new label `Foo`; their next introspection still shows the pre-`Foo` schema until they hit `/api/v1/schema/regenerate`. Documented behavior; worth a one-line note in the README's GraphQL section.

4. **Cache-coherency under concurrent regenerate + query.** Mitigated by `sync.Map` semantics: a `Store` during a `Load` returns the old value to the in-flight reader (no torn reads). A request that arrives after `Store` sees the new schema. No locks needed.

5. **`graphql-go/graphql` library upgrades** could change introspection internals. Low probability; flag the dependency in the implementation PR.

## 5. Out of scope (v2 candidates)

- Eager schema construction on tenant creation
- LRU cache eviction policy
- Schema version IDs in responses (helpful for clients; not security)
- Per-tenant schema introspection rate limiting
- Schema diffing API for clients to detect cache lag
- Auto-invalidation hooks on label changes (would require write-path instrumentation)

## 6. Recommended next action

Ship this spike. Then run #2 (`GenerateSchemaForTenant` factor) — pure additive, prerequisite for #3 (server-side cache). Critical path totals ~1 day's work; A9 should close cleanly.
