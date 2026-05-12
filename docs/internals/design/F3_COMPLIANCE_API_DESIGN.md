# F3 — Compliance API: design spike + PR plan

**Date**: 2026-05-10
**Author**: Claude Opus 4.7 (1M context)
**Status**: spike complete; recommended GO with 4-PR plan in §4
**Filename per planning-doc spec**: head of the critical-path queue per `docs/NEXT_STEPS_2026-05-10.md:122-126` (Track F → F3)
**Predecessor pattern**: `docs/F1_1_PER_TENANT_LSA_DESIGN.md` (F1.1 spike-on-discovery, 2026-05-10)

---

## TL;DR

F3 is **partial-substrate**, not greenfield and not "already shipped." Three findings reframe the work:

1. The audit / masking / compliance packages exist with rich primitives — `audit.Filter` already supports tenant-scoped queries, `pkg/masking` has the field-type strategy machinery, `pkg/compliance` ships GDPR/SOC2/HIPAA/PCI/FIPS/ISO control checkers.
2. **Latent bug discovered**: `auditMiddleware` sits *outside* `requireAuth`/`withTenant` in the chain (`pkg/api/server.go:207`). Both inner middlewares use `r.WithContext(ctx)`, so context additions are visible downstream only. The middleware's `r.Context().Value(claimsContextKey)` lookup at `pkg/api/middleware_audit.go:45` always misses for the standard middleware chain. `Event.UserID`, `Username`, and `TenantID` on every middleware-emitted event have been silently empty. **`/api/v1/security/audit/logs`'s filter on those fields has been broken since the chain was assembled.** This is load-bearing for F3 — without it, `/v1/compliance/audit-log` returns zero results for any non-admin tenant.
3. **Read-path masking is a single-site change**, not 4+. All 13 node/edge response sites funnel through `nodeToResponse`/`edgeToResponse` at `pkg/api/server_helpers.go:13,26`. The planning-doc's "Get/List/Search/Vector" enumeration over-estimates integration scope.

**Recommendation: GO on F3 with the 4-PR plan in §4.** PR-0 is a standalone audit-middleware fix (independent of F3, security-relevant on its own merits). PR-1..3 deliver F3.

---

## §1 — Discovery: what exists, what doesn't

### Substrate: rich

| Layer | File:line | State |
|---|---|---|
| Audit event with tenant | `pkg/audit/audit.go:47` `Event.TenantID string`; `:267` `NewEventWithTenant(...)` | ✅ Exists. Event carries tenant; constructor exists. |
| Audit filter with tenant | `pkg/audit/audit.go:62` `Filter.TenantID`; `:146` filter check | ✅ Exists. Tenant-scoped queries supported at the storage layer. |
| In-memory + persistent loggers | `pkg/audit/audit.go:84` `AuditLogger`; `pkg/audit/persistent_logger.go:16` `PersistentAuditLogger` | ✅ Exists. Two-tier: in-memory for API queries, on-disk for retention. |
| Masking field-type strategies | `pkg/masking/masking.go:9` `Masker`; `pkg/masking/masking_types.go:36` `MaskingConfig` | ✅ Exists. `MaskString`, `MaskMap`, `IsSensitiveField`, `SanitizeForLogging`, custom rules. Email/phone/CC/SSN/password/IP heuristics built-in. |
| Compliance control checker | `pkg/compliance/checker.go:9` `ComplianceChecker`; `frameworks.go` initializers for GDPR, SOC2, HIPAA, PCI-DSS, FIPS-140-2, ISO-27001 | ✅ Exists. Enumerable controls per framework with status + evidence + summary. |
| Existing admin audit endpoints | `pkg/api/handlers_security.go:51` `handleSecurityAuditLogs`; `:134` `handleSecurityAuditExport` | ✅ Exists. **Cross-tenant**, `requireAdmin`-gated. Filter/pagination already plumbed. Routes: `/api/v1/security/audit/logs`, `/api/v1/security/audit/export`. |
| Read-path response helpers | `pkg/api/server_helpers.go:13` `nodeToResponse`; `:26` `edgeToResponse` | ✅ Exists. **Universal hook** — 13 call sites across nodes/edges/retrieve/search/vector/algorithm handlers all funnel through these two functions. |

### What's not built

| Surface | Status | Notes |
|---|---|---|
| `GET /v1/compliance/audit-log` (tenant-scoped) | ❌ None | The admin-scoped `/security/audit/logs` is structurally similar but cross-tenant. F3 needs a tenant-scoped sibling — see §3 decision 1. |
| `POST /v1/compliance/masking-policy` | ❌ None | No HTTP route. |
| `GET /v1/compliance/masking-policy/{tenant}` | ❌ None | No HTTP route. |
| Per-tenant masking policy storage | ❌ None | `pkg/masking` has `MaskingConfig` for runtime configuration but **no notion of policies-by-tenant**. Net-new infrastructure required — see §3 decision 4. |
| Read-path masking integration | ❌ None | `nodeToResponse` + `edgeToResponse` are tenant-blind today (no `context.Context` arg). Single-site signature change unlocks all 13 call sites. |
| `docs/COMPLIANCE.md` | ❌ None | Net-new doc per planning-doc spec. |
| F3 audit-regression row | ❌ None | Per `pkg/api/audit_regression_test.go` convention — cross-tenant policy-access denial. |

---

## §2 — The hidden bug: audit middleware can't see auth/tenant context

### Symptom

`auditMiddleware` (`pkg/api/middleware_audit.go:32-83`) reads `r.Context().Value(claimsContextKey)` at line 45 to extract `UserID`/`Username`. The lookup always fails for routes wrapped in `requireAuth(withTenant(...))`. Result: emitted events have empty `UserID`, empty `Username`, and (because the middleware never reads tenant context at all) empty `TenantID`.

### Root cause: middleware order × `r.WithContext` semantics

The handler chain at `pkg/api/server.go:207`:

```
suilMiddleware → metricsMiddleware → panicRecoveryMiddleware → requestIDMiddleware
  → rateLimitMiddleware → securityHeadersMiddleware → inputValidationMiddleware
  → auditMiddleware → loggingMiddleware → corsMiddleware → mux → requireAuth → withTenant → handler
```

`auditMiddleware` calls `next.ServeHTTP(wrapper, r)` (line 40) and then logs the event after `next` returns. By the time it logs, the inner `requireAuth` (`pkg/api/middleware_auth.go:102`) and `withTenant` (`pkg/api/middleware_tenant.go:60`) have already run — but both wrap the request via `r.WithContext(ctx)`, which creates a *new* `*http.Request`. Context additions on the new request are visible downstream of those middlewares only; they do not propagate back to upstream middleware's locally held `r`.

### Independent reproducibility

The bug is observable without F3 by hitting `GET /api/v1/security/audit/logs?user_id=alice` — the filter matches zero events even when alice is making requests. The persistent audit log on disk shows the same gap. This is the cleanest "test case" for the fix: any filter on user/tenant against the existing endpoint should start working.

### Test-coverage evidence

No existing test catches this bug. Specifically:

- `pkg/api/middleware_test.go:614-640` `TestAuditMiddleware_WithAuth` is the closest, but it **injects claims directly onto the outer request** via `req = req.WithContext(...)` before calling `server.auditMiddleware(...)`. This artificially makes claims visible to `auditMiddleware` — a path that does not exist in production, where claims are wrapped *inside* the chain by `requireAuth`. The test also never reads back the emitted event to assert `UserID` was populated; it only asserts HTTP 200.
- Every `GetEvents` test in `pkg/api/handlers_security_test.go` (e.g., lines 270, 281, 334) populates events synthetically (`UserID: "user123"` literal) by writing directly to the logger. These tests pin the logger's filter behavior, not the middleware's event-emission behavior.

The PR-0 regression test (described in §4) writes the missing assertion: a request through the *full* production chain (`auditMiddleware(requireAuth(withTenant(handler)))`), then `GetEvents(nil)` returns at least one event with non-empty `UserID` and `TenantID`. That test fails on `main` today; passes after PR-0.

### Why F3 forces the fix

F3's `/v1/compliance/audit-log` is *defined* as tenant-scoped. Without populating `Event.TenantID`, the endpoint can return at best the events emitted by *direct* `s.logAuditEvent(...)` callers (`requireAdmin` denial path at `middleware_auth.go:28-41`, plus a few similar). The vast majority of audit traffic (every API request) has empty TenantID and is unfilterable per-tenant. Shipping F3 atop the bug delivers a hollow feature.

### Fix shape

**Option A — Move auditMiddleware inside per-route chain** (e.g., `requireAuth(withTenant(s.audit(handler)))`). Simple per-handler but requires touching ~30 route registrations. Disruption-heavy.

**Option B — Audit collector pattern (recommended)**. Introduce a request-scoped mutable `*auditCollector` struct attached via context *before* `auditMiddleware`. `requireAuth` and `withTenant` write user/tenant into the collector via context lookup (no new context needed; pointer is shared). After `next` returns, `auditMiddleware` reads from the collector to populate the event.

```go
type auditCollector struct {
    UserID, Username, TenantID string
}

func (s *Server) auditCollectorMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        coll := &auditCollector{}
        ctx := context.WithValue(r.Context(), auditCollectorKey, coll)
        next.ServeHTTP(w, r.WithContext(ctx))
        // r.Context() here still has the collector pointer (value writes
        // are visible because it's a pointer; new context not required).
    })
}
```

**Option C — Refactor `requireAuth` / `withTenant` to mutate-in-place via `*r = *r.WithContext(ctx)`**. Works (the http.Server caller holds the same pointer). Idiomatic-Go-debatable. Avoid.

**Recommendation: Option B.** ~150 LOC + tests. Standalone PR (PR-0 below) — independent value, security-relevant, fixes a pre-existing latent bug regardless of whether F3 ever ships.

---

## §3 — Five forced decisions

The next-session prompt branched "spike-on-discovery vs. design doc" — neither alone fits. F3 is partial-substrate with material design choices that the planning doc punted on. Each decision below resolves an unforced choice.

### Decision 1 — What happens to `/api/v1/security/audit/logs`?

**Options**:
- **(1a) Deprecate.** Replace with tenant-scoped `/v1/compliance/audit-log`. Admin can pass `?tenant=...` (or `X-Tenant-ID` header — admin-override pattern from `withTenant`).
- **(1b) Keep both.** Admin endpoint stays cross-tenant; new endpoint is tenant-scoped only.
- **(1c) Single endpoint, scope-by-claims.** `/v1/compliance/audit-log` returns the caller's tenant's events; admin gets cross-tenant via `X-Tenant-ID` header.

**Recommendation: 1c, with a sharper admin contract.** Matches the existing `withTenant` admin-override idiom (`pkg/api/middleware_tenant.go:65-72`); avoids dual-endpoint maintenance.

**Trade-off the recommendation must resolve**: the existing `/security/audit/logs` is *the cross-tenant audit surface* — admins use it for fleet-wide compliance review and DR investigation, not just per-tenant queries. Deprecating without a substitute removes a real capability. To keep that capability inside one endpoint:

- `GET /v1/compliance/audit-log` — caller's tenant only (no header → returns own tenant; non-admin gets only own tenant always).
- `GET /v1/compliance/audit-log` with `X-Tenant-ID: <other>` (admin only) — returns the named tenant's events.
- `GET /v1/compliance/audit-log?tenant=*` (admin only) — returns events across all tenants. **This is the cross-tenant capability**, gated behind explicit query syntax + admin role rather than living on a separate endpoint.

PR-2 ships this contract; `/security/audit/logs` gets a deprecation comment in the same PR and is removed in a follow-up after one release window. **Reject 1b** (dual endpoints) — it splits the admin's mental model of audit query for no gain once `?tenant=*` exists.

### Decision 2 — When does the audit-collector fix land?

**Options**:
- **(2a) Standalone PR-0** before F3.
- **(2b) First commit of F3 design-doc PR** (this PR).
- **(2c) First commit of F3 audit-log impl PR** (PR-2 in §4 below).

**Recommendation: 2a.** The bug is independent of F3 — `/security/audit/logs`'s filter has been broken since the chain was assembled. Fixing it is a security-relevant standalone PR with its own merits. Bundling into F3 muddies the change story and couples a clear bug-fix to a multi-PR feature.

### Decision 3 — Read-path masking integration scope

**Options**:
- **(3a) Single-site at `nodeToResponse`/`edgeToResponse`** — covers REST's 13 call sites by signature change to take `context.Context`.
- **(3b) Two integration points: REST helpers + GraphQL resolvers**.
- **(3c) All-or-nothing — also include direct `node.Properties` accesses** in predicate-eval paths (e.g., `pkg/api/handlers_vectors.go:341` `matchesPropertyFilter`).

**Verification done**: GraphQL **does not** flow through `nodeToResponse`. `pkg/graphql/` has multiple direct `node.Properties` accesses constructing response shapes:

| File:line | Path |
|---|---|
| `pkg/graphql/edges_types.go:133` | edge resolver iterates `node.Properties` |
| `pkg/graphql/mutations_types.go:40` | mutation result iterates `node.Properties` |
| `pkg/graphql/schema.go:130` | base node type resolver iterates `node.Properties` |
| `pkg/graphql/aggregation_resolvers.go:34` | aggregation resolver |
| `pkg/graphql/aggregation_types.go:29,79` | aggregation type resolvers |
| `pkg/graphql/schema_search.go:46` | search result `json.Marshal(node.Properties)` |
| `pkg/graphql/filtering_eval.go:58` | filter predicate (predicate-eval — leave unmasked, see below) |
| `pkg/graphql/sorting_core.go:114-118` | sort key extraction (predicate-eval — leave unmasked) |

**Recommendation: 3b.** REST hook at `nodeToResponse`/`edgeToResponse` covers REST's 13 sites; GraphQL hook needs to be added at each response-shaping resolver (~6 sites). Predicate-eval and sort-key sites (`filtering_eval.go`, `sorting_core.go`, REST's `matchesPropertyFilter`) stay unmasked — masking pre-evaluation would make filter/sort behavior depend on masking config in user-surprising ways. Document explicitly in `COMPLIANCE.md` that masking is *post-evaluation, post-filter, pre-serialization*.

**Sub-decision**: extract a `maskNodeProperties(ctx, props)` helper from `pkg/masking/policy_apply.go` (PR-3) and call it from both the REST helpers and each GraphQL resolver site. Single source of truth; mechanical to add hook sites.

**PR-count impact**: PR-3 grows by ~6 GraphQL integration sites + their tests. Total LOC for PR-3 roughly doubles from initial estimate. Consider splitting PR-3a (REST + policy CRUD) and PR-3b (GraphQL integration) if reviewer pressure or merge-conflict risk warrants.

### Decision 4 — Per-tenant masking policy storage

**Options**:
- **(4a) In-memory map** keyed by tenantID, lost on restart unless layered atop something durable.
- **(4b) Snapshot-persisted** — extend the existing snapshot format (`pkg/storage` flat map convention per project `CLAUDE.md`). On-disk format is customer-data-equivalent; needs version bump per repo convention.
- **(4c) Stored as `:MaskingPolicy` graph nodes** — free persistence via existing snapshot/replay machinery; couples policy storage to the graph engine.

**Cost estimate** (rough, for the LOC-per-decision question the advisor flagged):

| Option | LOC delta vs. 4a | Notes |
|---|---|---|
| 4a in-memory | baseline (~80 LOC store + tests) | Lost on restart. |
| 4b snapshot-persisted | +~120 LOC (serializer, replay hook, version-bump test) | Requires snapshot-format version bump per `CLAUDE.md`'s "Snapshot format stability" rule. |
| 4c `:MaskingPolicy` graph nodes | +~50 LOC over 4a (insert/lookup helpers, tenant-scoping enforcement, no new persistence code — graph already snapshots) | Couples policy storage to the graph engine; same shape as the `:Claim` resolver special-case. |

**Recommendation: 4a in PR-3, with explicit "policies are lost on restart" caveat in `COMPLIANCE.md`.** The first customer pilot will surface whether durability is load-bearing — at that point, 4b becomes the upgrade (avoids 4c's coupling). 4c looks cheap but the project's existing instance of "treat config as graph nodes" (`pkg/graphql/mutations_resolvers.go:13` TODO) is currently flagged for refactoring; reproducing the pattern is a known anti-pattern in this codebase.

If the user's commercial-direction signal (per `docs/NEXT_STEPS_2026-05-10.md:264` "No stated commercial offering" decision-point) lands as "hosted" before PR-3 ships, escalate to 4b within PR-3 — hosted customers will not tolerate a restart-clearable policy.

### Decision 5 — Is the framework checker (`pkg/compliance`) in F3 scope?

**Background**: `pkg/compliance` ships GDPR/SOC2/HIPAA/PCI/FIPS/ISO control checkers (`ComplianceChecker.CheckCompliance(framework)` returns a `ComplianceReport`). The planning doc names three endpoints (`audit-log`, `masking-policy` GET/POST) — none use this package. A natural fourth endpoint would be `GET /v1/compliance/report?framework=SOC2`.

**Options**:
- **(5a) Out of scope** — F3 ships the planned three endpoints; framework checker is a separate "F3.1" track.
- **(5b) In scope, single endpoint** — add `GET /v1/compliance/report?framework=...` as a fourth endpoint in PR-3 or PR-4.
- **(5c) In scope, full surface** — also export per-tenant compliance dashboards.

**Recommendation: 5a (out of scope).** The framework checker delivers value but is a separate user surface (audit-and-export-style "show me my SOC2 posture"); coupling it to F3's policy/log surface inflates the PR series and dilutes the "what does F3 ship" answer. Track it as a follow-up once F3 lands. Add it to the next planning checkpoint as F3.1.

---

## §4 — PR plan

Four PRs total. PR-0 is independent; PR-1 is this design doc; PR-2 and PR-3 land F3.

### PR-0: `fix(api): audit middleware sees auth+tenant via collector pattern`

**Independent of F3.** Closes the latent bug at `pkg/api/middleware_audit.go:45` (silent empty UserID/Username/TenantID).

**Changes**:
- New `pkg/api/middleware_audit_collector.go` — `auditCollector` struct, `auditCollectorKey` context key, `auditCollectorMiddleware`, helpers to read/write the collector.
- `pkg/api/middleware_auth.go` `requireAuth` — after claims are validated, call `setAuditUser(r.Context(), claims.UserID, claims.Username)`.
- `pkg/api/middleware_tenant.go` `withTenant` — after tenantID is resolved, call `setAuditTenant(r.Context(), tenantID)`.
- `pkg/api/middleware_audit.go` `auditMiddleware` — replace direct `r.Context().Value(claimsContextKey)` with `getAuditCollector(r.Context())` after `next` returns.
- `pkg/api/server.go:207` — add `s.auditCollectorMiddleware` to the chain *before* `s.auditMiddleware` (outermost wrapper that still runs before `auditMiddleware`).
- New test `pkg/api/middleware_audit_collector_test.go` — pin: a request through `requireAuth(withTenant(handler))` produces an audit event with non-empty UserID, Username, TenantID; an unauthenticated request produces an event with all three empty (no panic).

**Acceptance**:
- `/api/v1/security/audit/logs?user_id=...&tenant_id=...` filters return non-empty results when matching events exist.
- All existing tests still pass.
- ~150 LOC + tests.

**Risk**: low. The collector pattern is additive; no existing middleware behavior changes. Worst case the collector lookup fails and audit emits an empty-fields event, matching today's behavior.

### PR-1: this design doc

Single-file commit, `docs/F3_COMPLIANCE_API_DESIGN.md`. Reviewer signs off on the five decisions before any code lands.

### PR-2: `feat(api): /v1/compliance/audit-log tenant-scoped endpoint`

**Depends on PR-0.**

**Changes**:
- New `pkg/api/handlers_compliance.go` with `handleComplianceAuditLog`.
- Route: `mux.HandleFunc("/v1/compliance/audit-log", s.requireAuth(s.withTenant(s.handleComplianceAuditLog)))`. Admin override via `X-Tenant-ID` (per Decision 1c).
- Filter parsing reuses the pattern from `handleSecurityAuditLogs:51-93` — `user_id`, `username`, `action`, `resource_type`, `status`, `start_time`, `end_time`, `limit`. Tenant filter is forced from context, not query.
- Pagination: `limit` (cap at 1000), `offset`. Today's `/security/audit/logs` doesn't paginate beyond `limit`; F3 needs append-only ordering plus offset for cursor-style pagination.
- Swagger annotations.
- Test: `pkg/api/handlers_compliance_test.go` covering tenant-A sees A's events only, tenant-B sees B's only, admin override works.

**Acceptance**: matches the planning-doc acceptance — "audit log returns tenant's events in append-only order."

### PR-3: `feat(api): /v1/compliance/masking-policy + read-path masking`

**Depends on PR-1.**

**Changes**:
- New `pkg/masking/policy_store.go` — `PolicyStore` with in-memory `map[tenantID]*Policy` (Decision 4a). Thread-safe via `sync.RWMutex`. `Get(tenant)`, `Set(tenant, policy)`, `Delete(tenant)`.
- Extend `pkg/api/handlers_compliance.go` with `handleComplianceMaskingPolicySet` (POST) and `handleComplianceMaskingPolicyGet` (GET).
- Routes:
  - `POST /v1/compliance/masking-policy` — admin-only (sets policy for caller's tenant or `X-Tenant-ID` admin override).
  - `GET /v1/compliance/masking-policy/{tenant}` — admin-only or self-tenant.
- Signature change to `nodeToResponse` and `edgeToResponse` in `pkg/api/server_helpers.go` to take `context.Context`. All 13 call sites updated to pass `r.Context()`. Helpers resolve tenant via `getTenantFromContext`, look up policy via `s.maskingPolicyStore.Get(tenant)`, apply via `pkg/masking.Masker` to property values.
- GraphQL spot-check: verify `pkg/graphql` resolvers also flow through these helpers; if not, mirror the integration.
- Test: `pkg/api/handlers_compliance_masking_test.go` covering policy CRUD; `pkg/api/handlers_nodes_masking_test.go` covering masking visible at `/nodes/{id}`, `/edges/{id}`, `/v1/retrieve`, `/search`, `/vector-search`.

**Acceptance**: matches the planning-doc acceptance — "masking policy applies to all read paths (Get/List/Search/Vector)."

### PR-4: `docs(compliance): COMPLIANCE.md + audit-regression row`

**Depends on PR-3.**

**Changes**:
- New `docs/COMPLIANCE.md` covering: SOC2 control mapping, GDPR Article 32 evidence, masking-policy semantics, audit-log retention, on-prem vs. hosted considerations.
- Audit-regression row in `pkg/api/audit_regression_test.go` — cross-tenant policy access denial (tenant-A's masking policy not visible to tenant-B; tenant-A's audit log not visible to tenant-B).
- Update `docs/NEXT_STEPS_2026-05-10.md` Track F to mark F3 done with PR refs (or defer to a separate planning-doc-update PR per the project's `planning-doc-update` skill convention; bundled is cheaper, separate is cleaner).

**Acceptance**: docs ship; regression suite green.

---

## §5 — Audit-regression row template

For inclusion in PR-4 against `pkg/api/audit_regression_test.go`. Pattern follows the existing A6a / A8.2 rows.

```go
// F3 — Compliance API tenant scoping.
// Cross-tenant guarantees:
//   - GET /v1/compliance/audit-log returns only the caller's tenant's events.
//   - GET /v1/compliance/masking-policy/{tenant} for tenant != caller's tenant
//     returns 403 (or 404 for cross-tenant existence-leak avoidance — match
//     the project's existing ErrNodeNotFound / ErrEdgeNotFound idiom).
//   - POST /v1/compliance/masking-policy targeting another tenant requires
//     admin role + explicit X-Tenant-ID header (matches withTenant override).
t.Run("F3_ComplianceAPITenantScoping", func(t *testing.T) {
    // Seed: tenant-A and tenant-B each emit one audit event.
    // Assert: GET /v1/compliance/audit-log as tenant-A returns 1 event with
    //         TenantID == "tenant-a"; same call as tenant-B returns its own.
    // Assert: GET /v1/compliance/masking-policy/tenant-b as tenant-A → 403/404.
    // Assert: POST /v1/compliance/masking-policy with X-Tenant-ID: tenant-b
    //         as non-admin tenant-A → 403.
})
```

---

## §6 — Risks specific to F3

- **Audit-collector PR-0 introduces a new context key + middleware order** — small but structural change to a security-sensitive path. Mitigate with the regression test that pins both the populated and the empty-field cases.
- **Masking on `nodeToResponse`/`edgeToResponse` interacts with vector search predicate-eval** — `matchesPropertyFilter` at `pkg/api/handlers_vectors.go:341` reads raw `node.Properties` for predicate matching, which is correct (filter on real values) but means masked output ≠ filtered values. Document explicitly in `COMPLIANCE.md` that masking is post-filter.
- **`docs/COMPLIANCE.md` is the first customer-shaped compliance doc in the repo** — the planning doc flags broader documentation gaps (`docs/NEXT_STEPS_2026-05-10.md:262-263` "Documentation surface is internal-audit-shaped, not customer-shaped"). Keep COMPLIANCE.md focused on F3's surface; resist scope-creep into a full SOC2 evidence pack.
- **In-memory policy store loses policies on restart** (Decision 4a). Acceptable for v1 but document explicitly. A customer running a pilot will hit this; surface in `COMPLIANCE.md`'s "Operational considerations" section.
- **GraphQL resolver path may bypass `nodeToResponse`** — if so, PR-3 grows. Verify before committing the PR-count estimate.

---

## §7 — Open questions for the user (resolve before PR-2)

1. **Decision 1 (audit endpoint contract)** — agreement on 1c with the `?tenant=*` admin cross-tenant query syntax (preserves `/security/audit/logs`'s capability inside one endpoint), and a one-release deprecation window for `/security/audit/logs`?
2. **Decision 3 PR-shape** — split PR-3 into 3a (REST + policy CRUD) and 3b (GraphQL integration), or land as one larger PR?
3. **Decision 4 fallback** — if commercial direction lands as "hosted" before PR-3 ships, escalate to 4b (snapshot-persisted) within PR-3?
4. **Decision 5 (framework checker scope)** — agreement on 5a (out of F3, track as F3.1)?
5. **Planning-doc update bundling (PR-4)** — bundle the planning-doc edit into PR-4, or use the `planning-doc-update` skill for a separate single-file PR per convention?

---

## §8 — How this doc relates to the next-session prompt

`docs/NEXT_SESSION_PROMPT.md` branched: "F3 partial-discovery → spike-on-discovery; F3 greenfield → design doc." Reality is a **third path**: partial-substrate with material design choices and a load-bearing latent bug. This doc occupies that path.

Pattern after merge:
- PR-0 ships standalone (independent value).
- This PR (PR-1) ships, user reviews and signs off on the five decisions.
- PR-2..3 implement F3 per this plan.
- PR-4 closes with docs + regression row + planning-doc reconciliation.

End of design doc.
