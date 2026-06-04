# Tenant-isolation sweep — 2026-06-04

A systematic audit of every surface that could leak another tenant's data,
identity, **or metadata**. Commissioned because two cross-tenant leaks surfaced
in quick succession — the GraphQL aggregate-schema property-key leak (#295) and
the batch-delete per-tenant-count drift (#298) — both *latent until a refactor or
consumer exposed them*. The sweep's goal was to find the rest of that class
before a caller does.

**Headline: the live request-facing posture is strong.** The prior audits (A1
tenant keying, A3/A3b `*AcrossTenants` naming, A6a–c resolver/edge scoping, A9
per-tenant schema) plus #295/#298 closed the real leaks. The sweep found one
live low-severity leak, one non-leak middleware bug, and a class of latent
footguns — all small and bounded.

## Method

Three parallel read-only audits over (1) the storage layer, (2) the
request-facing layers (REST handlers + GraphQL), (3) the specialized index/search
subsystems — followed by **reachability verification** of every flagged item (a
tenant-blind method is only a *leak* if a request path reaches it).

## Confirmed clean (verified — a future audit can start here, not re-derive)

- **GraphQL** — all 5 live schema generators (`GenerateSchemaWith*ForTenant`) thread the tenant into label *and* property discovery; all node/edge/aggregate/mutation resolvers scope via `tenant.MustFromContext` + `*ForTenant`; introspection returns only the caller's labels (`GetLabelsForTenant`, not `GetAllLabels`); F3 masking applies the per-tenant policy in both REST and GraphQL. The non-`ForTenant` generators are test/CLI-only (no request caller).
- **Vector search** — per-tenant partitioned (`map[TenantID]map[prop]*HNSWIndex`); `VectorSearchForTenant` rejects empty tenant and returns `ErrNodeNotFound` for cross-tenant probes; inserts route by `node.TenantID`.
- **Storage existence-oracle discipline** — `GetNodeForTenant`/`GetEdgeForTenant` (+ the update/delete `*ForTenant` wrappers) return `ErrNodeNotFound`/`ErrEdgeNotFound` for foreign-tenant IDs, identical to missing — no existence side channel. `CreateEdgeWithTenant` rejects cross-tenant endpoints (A6a).
- **Per-tenant index + stat maintenance** — create/update/delete (direct, batch, transaction) all maintain the per-tenant indexes and counters after #288/#298.

## Findings

| # | Finding | Reachable | Severity | Disposition |
|---|---|---|---|---|
| **F1** | `/api/metrics` was `requireAuth`-only and returned **global** `GetStatistics()` (NodeCount/EdgeCount/TotalQueries across all tenants) + operator system stats to any authenticated tenant user | LIVE | Low — cross-tenant volume signal | **Fixed**: gate `requireAdmin` (PR #300) |
| **F2** | `/api/v1/tenants/{id}` lacked `withTenant`; the non-admin self-check compared the path tenant against `getTenantFromContext`, which fell back to `DefaultTenantID` → a non-admin was denied their own tenant **and** could read the `default` tenant's metadata | LIVE | Low | **Fixed**: add `withTenant`; also rewired the A5 `buildTestMux` replica (which had drifted and missed this route) to the real `registerRoutes` (PR #301) |
| **F3** | Five `GraphStorage` methods returned cross-tenant data under neutral names with **no `*ForTenant` sibling and no request caller** — latent footguns: `FindNodesByProperty{Range,Prefix}`, `FindEdgeBetween`/`FindAllEdgesBetween`/`DeleteEdgeBetween`; plus full-text `IndexNodes`/`UpdateNode` (test/CLI-only) | NOT reachable | Latent | **Hardened**: renamed to `*AcrossTenants` (A3b convention) + scope docstrings; doc-deprecated the full-text pair (PR #302) |

## By-design cross-tenant (left as-is — not leaks)

- **`pkg/constraints`** uniqueness/cardinality constraints run across all tenants *by design* (documented, audit A3b) and have **no live API wiring** (only `examples/` + tests). Flag for review if ever wired to a write path (cross-tenant uniqueness would be an existence oracle on insert).
- **`GetStatistics` / `GetAllLabels` / global property index (post-filtered by `*ForTenant`)** — intentional global structures; tenant-scoped access is via the `*ForTenant` / `GetLabelsForTenant` variants.
- **Legacy tenant-blind `Foo` readers** with a `*ForTenant` sibling (`GetNode`, `GetEdge`, `FindNodesByProperty`, `GetOutgoingEdges`, …) — kept per the CLAUDE.md convention; the sibling already signals scope.

## Out of scope (recorded, separate follow-ups)

- **Per-tenant property-index partitioning** — current design is global index + post-filter (`FindNodesByPropertyIndexedForTenant`); documented perf trade-off (A6c), not a leak.
- **Batch vector indexing gap** — batch-created nodes aren't inserted into vector indexes (a correctness gap, not isolation).
- **`/metrics` Prometheus endpoint** exposes global counts publicly — infra-mitigated (network-isolate the scraper); no code change.

## Lesson reinforced

Both #295 and the F3 footguns share one shape: a tenant-blind method under a
neutral name is a leak waiting for a caller. The durable defense is the A3b
`*AcrossTenants` naming — it moves the leak from "discovered when exploited" to
"impossible to write without seeing the word AcrossTenants at the call site."
