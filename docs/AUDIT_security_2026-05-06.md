# Security Audit — graphdb (multi-tenant graph database)

**Date:** 2026-05-06  
**Auditor:** Senior Security Engineer (automated deep-mode scan)  
**Scope:** Multi-tenancy isolation, auth/authz, input validation, secrets, common Go vulns

---

## Critical (must fix)

### 1. Cross-tenant node and edge access via CRUD endpoints — `pkg/api/handlers_nodes.go:74`, `pkg/api/handlers_nodes.go:109`, `pkg/api/handlers_nodes.go:124`, `pkg/api/handlers_edges.go:50`

`/nodes/{id}`, `PUT /nodes/{id}`, `DELETE /nodes/{id}`, and `GET /edges/{id}` all call `s.graph.GetNode(nodeID)` / `s.graph.UpdateNode` / `s.graph.DeleteNode` with no tenant check. `GetNode` (storage/node_operations.go:85) is tenant-blind: it returns the node for any caller who supplies a valid uint64 ID. Node IDs are sequential (atomic counter), so an attacker from tenant A can enumerate tenant B's nodes by incrementing the ID, and can overwrite or delete them. The handler routes (`server.go:42-44, 48-49`) are wrapped in `requireAuth` only — no `withTenant`.

**Attacker impact:** Full read/write/delete of any other tenant's graph data by ID enumeration.  
**Fix:** Wrap all `/nodes/`, `/edges/` routes with `withTenant`, then call `matchesTenant(node.TenantID, getTenantFromContext(r))` before returning or mutating; reject non-matching with 403.

---

### 2. `GET /nodes` returns all tenants' nodes — `pkg/api/handlers_nodes.go:18`

`listNodes` calls `s.graph.GetAllNodes()` which returns every node from every tenant. `GetAllNodesForTenant(tenantID string)` already exists at `pkg/storage/tenant_operations.go:182` but is never called from any API handler.

**Attacker impact:** Any authenticated user can dump the full node set of all tenants in a single request.  
**Fix:** Replace `s.graph.GetAllNodes()` with `s.graph.GetAllNodesForTenant(getTenantFromContext(r))`; add `withTenant` to the route.

---

### 3. `/query` and `/graphql` endpoints have no tenant context — `pkg/api/server.go:36, 39`

`/query` (server_handlers.go:98) passes the parsed query to `s.executor.ExecuteWithContext` with no tenant scope; the executor has no tenant-awareness (no hits for `TenantID` or `FromContext` in `pkg/query/executor*.go`). `/graphql` similarly delegates to a schema-generated handler with no tenant injection (no hits in `pkg/graphql/`). Any authenticated user can traverse or query the entire graph.

**Attacker impact:** A non-admin user from tenant A can read or traverse all nodes/edges belonging to tenant B via the query DSL or GraphQL introspection.  
**Fix:** Inject tenant context into the executor and GraphQL resolver before routing; the query executor must scope `MATCH` node iteration to `GetAllNodesForTenant`.

---

## High (should fix)

### 4. JWT secret silently randomizes per-process unless `GRAPHDB_ENV=production` is set — `pkg/api/server_init.go:66-78`

If `JWT_SECRET` is unset but `GRAPHDB_ENV` is anything other than the exact string `"production"` (including unset, `"prod"`, `"staging"`), the server silently generates a random 32-byte secret and logs a warning. All previously issued tokens become invalid on every restart, but — more critically — a misconfigured staging or pre-prod environment that receives real traffic will rotate secrets on every deploy, silently invalidating all active sessions rather than refusing to start.

**Attacker impact:** Session disruption in misconfigured environments; easy to accidentally run production workloads on a rotating secret.  
**Fix:** Fail-closed: if `JWT_SECRET` is empty, always return an error (remove the `GRAPHDB_ENV` guard); development environments should explicitly opt in with a fixed dev-only secret in their `.env`.

### 5. `/traverse`, `/shortest-path`, `/algorithms` lack tenant isolation — `pkg/api/server.go:52-56`

These routes use `requireAuth` only. Traversal algorithms (`handleTraversal`, `handleShortestPath`, `handleAlgorithm`) take node IDs as parameters and iterate the graph without tenant checks. An authenticated user can traverse from their own node into another tenant's subgraph.

**Attacker impact:** Cross-tenant graph traversal, exposing relationship structure and node properties of other tenants.  
**Fix:** Add `withTenant` to these routes and enforce tenant boundary in traversal start/end node validation and result filtering.

### 6. `PUT /api/v1/tenants/{id}` and `DELETE /api/v1/tenants/{id}` perform admin check in handler body, not middleware — `pkg/api/handlers_tenant.go:526-538`

The route `/api/v1/tenants/` is registered with `requireAuth` only (server.go:92). The `handleTenantEndpoint` dispatcher performs inline `claims.Role != auth.RoleAdmin` checks for PUT/DELETE and for the `/suspend` and `/activate` sub-paths. This pattern is fragile: a future code path added inside `handleTenantEndpoint` may forget the inline check, and the intent is not captured at the routing layer where it is immediately auditable.

**Attacker impact:** Currently safe, but high-risk pattern for privilege escalation via future handler additions.  
**Fix:** Split into separate admin-wrapped and auth-wrapped mux registrations, or move to `requireAdmin` at route registration.

---

## Medium

### 7. `handleQuery` returns raw internal errors in some paths — `pkg/api/server_handlers.go:140-145`

Lexer and parser errors (lines 140, 145) are returned verbatim to the client as `"Lexer error: ..."` / `"Parser error: ..."`. While `sanitizeError` is applied on executor errors (line 163), parse-stage errors expose the internal query DSL error format and potentially internal type/path details.

**Fix:** Wrap lexer/parser errors through `sanitizeError` before responding, or return a generic "invalid query syntax" with the detail logged server-side only.

### 8. `GET /nodes` and `GET /edges` are missing `withTenant` but traversal/algorithm routes can reference these nodes — compounding with finding 1/2/5

(No additional file reference; this is a compound of the above findings combined into a cross-cutting observation: the tenant isolation gap is systematic across all non-search routes, not isolated to a single handler.)

---

## Low

### 9. `/vector-indexes` create and delete are tenant-unscoped — `pkg/api/handlers_vectors.go:148-240`

`createVectorIndex` and `deleteVectorIndex` check index existence against the global store but do not enforce that the index belongs to the requesting tenant. Any authenticated user can delete another tenant's vector index by name.

**Fix:** Tag vector indexes with a tenant owner at creation time and enforce ownership on delete.

---

## Positive findings

- JWT algorithm is enforced to HMAC (jwt.go:137-140); `alg: none` is correctly rejected.
- `/vector-search` post-filter chain is correct: tenant → label → property, in that order, with primitive-only enforcement on `property_filter`.
- Admin routes for security key management and audit log access are consistently behind `requireAdmin`.
- Password hashing uses bcrypt at cost 12 (user_store.go:26).
- API key environment enforcement (test vs. live) is correctly applied.

---

**Verdict: FAIL**

The multi-tenancy isolation is broken across the majority of non-search read/write paths. Findings 1-3 are independently CRITICAL and affect the core node/edge/query surface. Fix these before any multi-tenant data is stored in a shared instance.
