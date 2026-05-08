# A6c Design Spike — Tenant-scoping `/query` and `/graphql`

**Status:** spike output, not implementation. Date: 2026-05-08.
**Predecessors:** A1 (tenantid type) → A3a/A3b (storage `*ForTenant`) → A5 (withTenant middleware) → A6a (`/nodes` `/edges` handlers) → A6a-followup (`verifyNodeExists` strict) → A6b (`/traverse` `/shortest-path`).
**Goal of this doc:** map the territory and pin design choices so PRs #6–#9 in the next-steps plan can be sized and ordered.

## 1. What's tenant-blind today

Every API endpoint listed below currently runs as if it were the default tenant, regardless of the JWT-derived `tenantID` on `r.Context()`. Audit Track A closed this for `/nodes` `/edges` `/traverse` `/shortest-path`; everything in this doc is what's left.

### `/query` — `pkg/api/server_handlers.go:98`
- Sanitize → lex → parse → `executor.ExecuteWithContext(ctx, parsedQuery)` (`server_handlers.go:155`).
- The executor's `ExecutionContext` already carries `context context.Context` (`pkg/query/executor_plan.go:23`) — tenant ID is technically *available* but never read.
- All graph reads in `pkg/query/` use the tenant-blind methods: `ctx.graph.GetNode`, `ctx.graph.FindNodesByPropertyIndexed`, `ctx.graph.CreateNode`, `ctx.graph.CreateEdge`, etc. (~17 sites).
- **Result:** any authenticated caller can run Cypher-ish queries against the full multi-tenant corpus.

### `/graphql` — `pkg/api/server_handlers.go:177` → `pkg/graphql/http.go:42`
- **Headline gotcha:** `ServeHTTP` drops `r.Context()` entirely. `ExecuteQuery(req.Query, h.schema)` and `ExecuteQueryWithVariables(req.Query, h.schema, vars)` take no `context.Context` argument (`http.go:71-73`). The downstream `graphql.Params` is constructed without `Context`, so resolvers run with `p.Context == context.Background()`.
- Resolvers receive `*storage.GraphStorage` closure-captured at schema-construction time and call tenant-blind methods (`gs.CreateEdge` at `edges_resolvers.go:118`, etc.) — ~31 graph-read sites across `pkg/graphql/*`.
- **Result:** GraphQL is fully tenant-blind. Mutations land in `default` tenant; queries return cross-tenant data.

### `pkg/algorithms/*` — exposed via `/algorithms/*` and `/shortest-path`
- 13 files: `centrality`, `community_*`, `cycle_detection`, `khop`, `node_similarity`, `pagerank`, `scc`, `shortest_path`, `topology`, `triangles`.
- `ShortestPath` got `ShortestPathForTenant` in A6b. The other 12 algorithms are still tenant-blind.
- They take `*storage.GraphStorage` and call `GetOutgoingEdges`/`GetIncomingEdges`/`GetAllNodeIDs`/`FindNodesByLabel` directly.

### Other surfaces
| Surface | Files | API-reachable today? | Action |
|---|---|---|---|
| `pkg/parallel/*` | 3 | **No** — only imported by `cmd/benchmark-parallel/main.go` and a doc comment in `pkg/storage/storage.go` | Out of A6c scope |
| `pkg/constraints/*` | 4 | Write-side hooks, may run during create/update | A6c-adjacent — track as A6c-constraints |
| `pkg/partition/*` | 1 | Background only | Out of A6c scope |

## 2. Design questions, answered

### Q1: `/query` — thread `tenantID` through the visitor, or wrap the graph in a tenant-view?
**Answer: thread through `ExecutionContext`. Don't wrap the graph.**

Rationale: `ExecutionContext` is already the natural carrier for per-execution state (bindings, results, cancellation context). Adding a `tenantID string` field is one line; reading it from `ec.context` once in the executor entry point is one more. The wrap-the-graph alternative (decorator pattern over `*storage.GraphStorage`) requires either an interface refactor (research-shaped, much bigger blast radius) or a parallel `*TenantGraphView` type duplicating the surface — neither buys anything that the field approach doesn't, and both lose static-type guarantees about which graph is being used.

### Q2: `/graphql` — per-resolver `tenantID` extraction, or schema-level gate?
**Answer: per-resolver, via `tenant.GetTenant(p.Context)`.**

Rationale: every resolver makes its own storage call (`gs.CreateEdge`, `gs.GetNode`, etc.). The schema-level-gate alternative (a single root middleware) cannot replace those calls — each resolver still needs the tenant ID at the moment of its read. A schema-level gate would have to either (a) inject tenant into the resolver via a context shim, which is what we'd be doing anyway, or (b) only block requests at the root (binary allow/deny), which doesn't actually scope reads.

The required prerequisite: `pkg/graphql/http.go` must plumb `r.Context()` into `graphql.Params{Context: ctx}` so resolvers *can* read the tenant. This is the headline blocker.

### Q3: Which `*ForTenant` storage iteration methods are needed?

Existing (post-A6b): `GetNodeForTenant`, `GetEdgeForTenant`, `GetOutgoingEdgesForTenant`, `GetNodesByLabelForTenant`, `GetEdgesByTypeForTenant`, `GetAllNodesForTenant`, `GetAllEdgesForTenant`, `CountNodesForTenant`, `CountEdgesForTenant`, `GetLabelsForTenant`, `GetEdgeTypesForTenant`, plus the create/update/delete *ForTenant variants.

**Needed for A6c:**
- `GetIncomingEdgesForTenant(nodeID, tenantID) ([]*Edge, error)` — mirror of A6b's outgoing variant, used by `/algorithms` BFS and undirected traversals.
- `FindNodesByPropertyForTenant(key, value, tenantID) ([]*Node, error)` — used by query executor full-scan paths.
- `FindNodesByPropertyIndexedForTenant(key, value, tenantID) ([]*Node, error)` — used at `executor_steps.go:81`. Index-backed; need to confirm the property index is per-tenant or post-filter the index hit.

**Probably needed (confirm during implementation):**
- `GetStatisticsForTenant()` — `pkg/query/match_node.go:32` reads `ctx.graph.GetStatistics()` for cardinality estimates. Cross-tenant stats poison the optimizer's choices.

### Q4: For the 41 library files, which are post-filter-safe vs. require expansion-time filter?

Same principle as A6b's `ShortestPathForTenant`: **anything that walks the graph must filter at edge expansion**, because a post-filter on the result would deny paths that exist in the caller's subgraph if a shorter cross-tenant route was picked first. Read-only stats that don't traverse can post-filter cheaply.

| Category | Files | Filter location | Notes |
|---|---|---|---|
| **Traversal/path algorithms** (must filter at expansion) | `algorithms/{shortest_path, khop, cycle_detection, scc, community_components, community_propagation, pagerank, triangles, topology}`, `query/{match_path, parallel_pathfinder, parallel_traversal, traversal_bfs, traversal_paths}` | Inside the algorithm | Same shape as A6b. Each becomes `XForTenant(graph, ..., tenantID)` calling `GetOutgoingEdgesForTenant`. |
| **Per-node reads** (post-filter trivially) | `algorithms/{centrality, node_similarity}` | At storage call | Switch `GetNode` → `GetNodeForTenant` and the underlying `GetOutgoingEdges` → `GetOutgoingEdgesForTenant`. No expansion-time loop logic to rewrite. |
| **Aggregations / mixed** (case-by-case) | `algorithms/{community_clustering, community_types}`, `graphql/aggregation*`, `query/{stream_query, optimizer, executor_steps}` | Mixed | Community detection traverses *and* reports membership — needs expansion-time filter. Aggregations sometimes scan node sets — needs `*ForTenant` enumerate-then-aggregate. Optimizer/stats needs `GetStatisticsForTenant` (Q3). |
| **GraphQL resolvers** (mechanical migration) | `graphql/{edges_resolvers, mutations, dataloader_edges, edges_schema, edges_types, filtering_schema, limits, pagination_resolvers, pagination_schema, schema, sorting_resolvers, sorting_schema}` | At storage call | Each resolver: extract `tenantID` from `p.Context`, switch to `*ForTenant`. |
| **Query executor** | `query/executor_steps`, `query/match_*`, `query/traversal_*` | Mixed (traversal at expansion; CRUD at storage call) | After the `ExecutionContext.tenantID` plumbing, mostly mechanical. |
| **Out of scope** | `parallel/*`, `partition/*` | — | Not API-reachable. |
| **Adjacent** | `constraints/*` | — | Constraints fire on writes; they read other tenants' data when validating "no duplicate" rules. Could become a tenant-leak vector. Track separately as A6c-constraints. |

## 3. Gotchas

1. **`pkg/graphql/http.go` ServeHTTP drops `r.Context()`.** This is the load-bearing fix for `/graphql`. Without it, resolvers cannot read tenant ID even after migration — every other graphql change is downstream of this one.
2. **`ExecutionContext.context` exists but is unused for tenant.** The query executor already carries the request context for cancellation; tenant just needs to be read from it. No new plumbing required, just the read.
3. **The optimizer reads `GetStatistics()` for cardinality estimates.** Cross-tenant stats will mis-estimate selectivities and pick wrong plans. Lower-severity than read-leak but worth scoping.
4. **`pkg/constraints/*` validates on the write path.** Uniqueness/cardinality checks read other-tenant data while validating. Not a CRUD leak (constraints just return pass/fail), but a *constraint-by-constraint* tenant rules question: should "no duplicate name" mean "in your tenant" or "globally"? Different products want different answers — this is product-shaped, not security-shaped, and warrants its own design call.
5. **GraphQL closure-captured `*storage.GraphStorage`.** Resolvers don't need a different graph instance per tenant — they just need to call `*ForTenant` methods with the tenant ID from `p.Context`. The closure pattern stays.
6. **A6b's `verifyNodeExists` follow-up (#20) is a hard prerequisite for `/query` mutations.** `query/executor_steps.go:192` calls `ctx.graph.CreateEdge` (tenant-blind). After A6c migrates this to `CreateEdgeWithTenant`, the from/to node check is tenant-strict — which is correct, but means failing tests if seeded with the gap. Same shape as the test rewrites in #20.

## 4. Out-of-scope but worth surfacing

- **`/algorithms` endpoint scoping** — separate handler per algorithm, all currently call tenant-blind algorithm functions. Once algorithms have `*ForTenant` variants, the handlers are mechanical migrations. Could be its own PR ("A6c-algorithms-handlers") after the algorithm-side migration lands.
- **A8 (replication tenancy)** — already filed. Independent of A6c; replication doesn't go through `/query` or `/graphql`.
- **A6c-constraints** — track separately. Product question, not security.
- **Statistics/optimizer scoping** — small additional surface area; bundles cleanly into the `/query` PR.

## 5. Key counts (sizing hint, not commitment)

- Storage methods to add: **3 (likely 4).**
- `pkg/query/*` graph-read sites: **~17.**
- `pkg/graphql/*` graph-read sites: **~31** + the headline `ServeHTTP` ctx-plumb.
- `pkg/algorithms/*` traversal algorithms to mirror: **~9** (with simpler post-filter migration for ~2-4 more).
- API handlers behind `/algorithms`: per-algorithm, mechanical after the algorithm-side migration.

## 6. Decision points for the next `/plan`

The natural PR breakdown emerges from the categorization table above:
- **A6c-storage**: add the 3-4 new `*ForTenant` storage methods. Pure additive, can ship before any handler work.
- **A6c-graphql-ctx**: plumb `r.Context()` through `pkg/graphql/http.go` into `graphql.Params`. No resolver changes yet — just unblocks the next PR.
- **A6c-graphql-resolvers**: migrate the ~31 resolver call sites to `*ForTenant`. Per-file, mechanical after the ctx-plumb.
- **A6c-query**: add `tenantID` to `ExecutionContext`, migrate the ~17 query executor call sites, scope statistics for the optimizer.
- **A6c-algorithms**: per-algorithm `*ForTenant` variants for the 9 traversal algorithms, plus simpler migrations for centrality/similarity.
- **A6c-algorithms-handlers**: mechanical handler-side switch in `pkg/api/handlers_algorithms*.go` after the algorithms have `*ForTenant` variants.

The `/plan` skill should size and sequence these. This doc deliberately stops short of that — sizing depends on which packages produce surprises during implementation, and the answer to "how big is GraphQL really" only emerges after the ctx-plumb PR lands.
