# Compliance

Surface-level guide to the controls graphdb's Compliance API exposes
(Track F3 — `/v1/compliance/*`), and how those controls map onto
SOC2 Trust Services Criteria and GDPR Article 32. The audience is a
customer's compliance/security reviewer evaluating graphdb for
regulated workloads.

> **Scope note.** graphdb is *not* SOC2-certified or GDPR-audited as a
> product. This document describes the controls graphdb makes
> available so a *customer* operating graphdb can demonstrate their
> own compliance posture. Authoritative certifications are the
> customer's responsibility.

For implementation details and tests, see:

- `docs/F3_COMPLIANCE_API_DESIGN.md` — design rationale, the five
  decisions that shaped the F3 surface.
- `pkg/api/handlers_compliance.go` — the HTTP handlers.
- `pkg/masking/` — masking strategies, policy store, apply path.
- `pkg/audit/` — audit-event model, in-memory and persistent loggers.
- `pkg/api/audit_regression_test.go` — cross-tenant regression suite
  pinning the contracts below as an end-to-end smoke gate.

---

## 1. The surface at a glance

| Endpoint | Method | Purpose | Authorization |
|---|---|---|---|
| `/v1/compliance/audit-log` | `GET` | Read tenant's audit events (paginated). | Authenticated; tenant-scoped by default. Admin may widen via `X-Tenant-ID:<id>` or `?tenant=*`. |
| `/v1/compliance/masking-policy` | `POST` | Set/replace the calling tenant's masking policy. | Admin only. Target via `X-Tenant-ID` if admin is acting on another tenant. |
| `/v1/compliance/masking-policy/{tenant}` | `GET` | Read a tenant's masking policy. | Admin (any tenant) **or** non-admin (own tenant only). Cross-tenant non-admin returns `403`. |

All three endpoints are tenant-aware. Cross-tenant existence-leak
avoidance follows the same idiom as `/nodes` and `/edges`: a
non-admin caller reading another tenant's resource sees either
`403` (with a specific access-denied message — masking-policy/{tenant}
case, since tenants are independently enumerable elsewhere) or `404`
(audit-log filter is tenant-scoped by default; events for other
tenants are simply absent from the result, not flagged as
"forbidden").

---

## 2. SOC2 control mapping

The Compliance API targets the **Common Criteria (CC)** and
**Confidentiality (C1)** categories. graphdb provides the *control
surface*; a customer's SOC2 audit ties their operational use of
these controls back to their evidence pack.

| Control | What F3 provides | Evidence |
|---|---|---|
| **CC6.1** (Logical access) | Auth-gated endpoints; role-based (admin vs non-admin) widening. | `pkg/api/middleware_auth.go`; `TestComplianceAuditLog_NonAdminCannotWidenCrossTenant`. |
| **CC6.6** (Restrict information transmission) | Per-tenant masking policy applied to every response on REST and GraphQL read paths. | `pkg/api/handlers_nodes_masking_test.go` (`TestMasking_PolicyFollowsTenant`); `pkg/api/handlers_graphql_masking_test.go` (`TestGraphQL_Masking_PolicyFollowsTenant`). |
| **CC7.2** (System monitoring — security events) | Authenticated calls produce structured audit events recording `user_id`, `username`, `tenant_id`, `action`, `resource_type`, `status`, `timestamp`. | `pkg/audit/audit.go`; `pkg/api/middleware_audit_collector.go`; `TestAuditCollector_PopulatedAfterAuth`. |
| **CC7.3** (Evaluate security events) | Audit log queryable via `GET /v1/compliance/audit-log` with filters (`user_id`, `action`, `resource_type`, `status`, time range). | `TestComplianceAuditLog_Pagination`, `TestComplianceAuditLog_LimitCap`. |
| **C1.1** (Confidential information protected) | Masking strategies cover field-level confidentiality: `full`, `partial`, `hash`, `redact`, `tokenize`, `none`. Per-tenant policies isolate confidentiality posture across tenants. | `pkg/masking/masking_types.go`; `TestAuditRegressionSuite_CrossTenantIsolation`. |
| **C1.2** (Confidential information disposed) | Audit log retention configurable via `RetentionDays` on the persistent logger. | `pkg/audit/persistent_types.go`; `pkg/audit/persistent_rotation.go`. |

---

## 3. GDPR Article 32 evidence

Article 32 requires "appropriate technical and organisational
measures to ensure a level of security appropriate to the risk."
F3 contributes the following technical measures.

- **Pseudonymisation and encryption of personal data (Art. 32(1)(a))** —
  Masking strategies `hash` (deterministic SHA-256) and `tokenize`
  (consistent token across reads of the same value) provide
  pseudonymisation at the response layer. Encryption-at-rest is a
  deployment concern, not an F3 control.
- **Confidentiality, integrity, availability, resilience (Art. 32(1)(b))** —
  Per-tenant masking policies enforce confidentiality at the
  read-path boundary; the audit log records every authenticated
  access for after-the-fact integrity review.
- **Regularly testing, assessing and evaluating the effectiveness (Art. 32(1)(d))** —
  The cross-tenant regression suite
  (`TestAuditRegressionSuite_CrossTenantIsolation`) runs on every CI
  build, pinning the contracts that ground both audit-log
  tenant-scoping and masking-policy tenant-scoping. A regression to
  either fails CI immediately.

The audit log records the **subject** (user) and **scope** (tenant)
of access; combined with retention, this supports Art. 15 (right of
access) requests for the data-subject view "who saw my data and
when."

---

## 4. Masking policy semantics

### 4.1. Strategies

A policy maps property names to a strategy:

| Strategy | Behaviour |
|---|---|
| `full` | Replace the entire value with mask characters (preserving length). |
| `partial` | Show first and last N characters; mask the middle. |
| `hash` | Replace with the SHA-256 hex digest (deterministic across reads). |
| `redact` | Replace with the literal `[REDACTED]`. |
| `tokenize` | Replace with a consistent token (same input → same token). |
| `none` | No masking — pass through verbatim. Equivalent to omitting the property from the policy. |

Per `pkg/masking/masking_types.go`.

### 4.2. Auto-detect (opt-in)

A policy can set `auto_detect: true` to apply regex-based heuristics
(email, phone, credit-card, SSN, API-key, IP) to any property *not*
explicitly named in `Properties`. The explicit-allow-list always
wins. Off by default.

### 4.3. **Masking is post-filter, not pre-filter** (foot-gun)

The vector search and predicate-eval paths read **raw** property
values for filter matching, not masked values. The masking pass runs
on the **response** after results are selected. This means:

- A search predicate `WHERE ssn = "123-45-6789"` will *match* a node
  with that SSN even when the response masks `ssn` to `***-**-****`.
- A vector similarity search over a property that contains
  personally-identifiable text will use the unmasked text to compute
  similarity.

This is the correct semantic for query expressivity — masking that
hid values from the filter would silently change result sets — but
**operators relying on masking as an access control must be aware
that masking is a presentation-layer transform, not a query-layer
gate.** If a property must be unreachable by query (not just
unviewable in responses), use schema-level controls (don't store
it, or store it in a separate tenant), not masking.

Reference: `pkg/api/handlers_vectors.go:matchesPropertyFilter` and
the relevant test `TestMasking_NodeGet_PolicyAppliesToReadPath`.

### 4.4. Scope of coverage

F3 PR-3a and PR-3b extended masking to both:

- **REST read paths** — all node/edge response sites flow through
  `nodeToResponse`/`edgeToResponse`, which apply the policy
  (`pkg/api/server_handlers.go`, 13 sites: `/nodes`, `/edges`,
  `/search`, `/vectors`, `/retrieve`, `/hybrid-search`,
  `/algorithms/*/traverse`).
- **GraphQL read paths** — the same `Policy.Apply` runs at the
  GraphQL response-shaping resolvers
  (`pkg/graphql/masking_hook.go`).

Both surfaces use the same `pkg/masking.Policy` and `pkg/masking.Masker`,
so adding a new strategy to one path automatically picks it up in the
other.

---

## 5. Audit log retention

graphdb ships **two** audit loggers; both implement the same
`pkg/audit.Logger` interface but with different durability guarantees.

### 5.1. In-memory logger (`pkg/audit.AuditLogger`)

- Stores events in a bounded ring buffer (default cap configurable
  per process).
- Used by the `/v1/compliance/audit-log` endpoint.
- **Loses events on process restart.** Acceptable for development
  and for deployments where the persistent logger is the
  ground-truth and the in-memory ring is a quick-query cache.

### 5.2. Persistent logger (`pkg/audit.PersistentLogger`)

- Appends events to disk with periodic rotation.
- `RetentionDays` configures auto-pruning of files older than the
  named window (`pkg/audit/persistent_rotation.go`).
- Survives restart; suitable as the customer's SOC2 / GDPR
  evidence-of-access source.
- Operator must configure both a `LogDirectory` and a non-zero
  `RetentionDays` for retention to take effect.

### 5.3. Recommendation

For production deployments handling regulated data, **enable the
persistent logger with `RetentionDays` set to your retention policy's
floor** (e.g., 90 days for many SOC2 audits; 7 years for some
financial-services GDPR Art. 30 records-of-processing obligations —
consult your compliance team for the exact figure).

---

## 6. Operational considerations

### 6.1. In-memory policy store (v1)

The Compliance API's masking-policy store is **in-memory** by
default. Setting a policy via `POST /v1/compliance/masking-policy`
persists for the lifetime of the process; **a server restart resets
every tenant to "no policy"** until the policy is re-POSTed. This is
explicit in the design (`docs/F3_COMPLIANCE_API_DESIGN.md` §3
Decision 4) and acceptable for v1 because policies are
declarative-and-idempotent (re-applying the same JSON gives the same
state).

For hosted deployments where a restart is operator-invisible,
re-apply policies as part of the startup runbook. A snapshot-persisted
PolicyStore (Decision 4b in the design doc) is the planned
follow-up for environments that need restart-survival.

### 6.2. No-policy tenants

A tenant with no policy set returns properties verbatim — identical
to pre-F3 behaviour. This is the explicit default. There is no
"fail-closed redact everything" mode.

For tenants that must redact a property by default, set an explicit
policy with that property's strategy. The empty-policy-is-passthrough
behaviour is *load-bearing* for backward compatibility with
pre-F3 deployments (`TestMasking_NoPolicy_PassthroughPreservesPreF3Behavior`).

### 6.3. Cross-tenant admin override

An admin acting on behalf of another tenant must pass
`X-Tenant-ID: <target-tenant>` on the request. The masking policy
applied is **the target tenant's**, not the admin's resident tenant —
this is the load-bearing F3 invariant
(`TestMasking_PolicyFollowsTenant`). An admin reading tenant-B's
data sees tenant-B's masking applied, regardless of where the admin
"lives."

### 6.4. GraphQL parity

Every contract above holds equally on the GraphQL surface
(`/graphql`). The PR-3b integration (`pkg/graphql/masking_hook.go`)
applies the same `Policy.Apply` at every response-shaping resolver,
so a customer using GraphQL as their primary surface gets identical
guarantees.

---

## 7. What this document is NOT

- A SOC2 attestation. graphdb is not certified. This document
  describes the controls available; the customer's operator
  attests to their use.
- A complete GDPR data-protection impact assessment (DPIA). DPIAs
  are workload-specific; F3 provides input, not the full assessment.
- Legal advice. Compliance posture depends on jurisdiction,
  industry, and data classification — consult your compliance team.
- An exhaustive list of every audit event emitted. See
  `pkg/audit/audit.go` for the canonical event types.
