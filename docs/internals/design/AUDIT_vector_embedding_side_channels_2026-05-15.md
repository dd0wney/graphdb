# Audit — Vector / Embedding Side-Channels (Track R post-implementation)

**Date:** 2026-05-15
**Auditor:** Senior Security Engineer (post-Track-R sweep)
**Scope:** Code shipped by R1.x (#184/#185), R2.x (#186-#190, #193), and R3 (#191). Specifically:

- `pkg/storage/vector_index.go` — per-tenant HNSW data structure (R1.1)
- `pkg/storage/vector_operations.go` — GraphStorage `*VectorIndexForTenant` methods (R1.2)
- `pkg/intelligence/embedder.go` — Embedder interface + `ErrNoIndexForTenant` (R2.3)
- `pkg/intelligence/lsa_embedder.go` — LSAEmbedder adapter (R2.4)
- `pkg/intelligence/auto_embed_observer.go` — NodeObserver + Task (R2.5a)
- `pkg/intelligence/worker.go` — bounded worker Pool (R2.2)
- `pkg/api/handlers_vectors.go` — REST surface (touched by R1.2)
- `pkg/api/handlers_embeddings.go` — `/v1/embeddings` (pre-Track-R; in scope as the embedding surface)

**Out of scope:** Pre-Track-R surfaces unrelated to vectors/embeddings (auth middleware, JWT handling, GraphQL endpoint isolation, etc.) — covered by [`AUDIT_security_2026-05-06.md`](./AUDIT_security_2026-05-06.md).

**Attacker model:** authenticated user of tenant A attempting to learn about, infer state of, or affect operations of tenant B. Cross-tenant unauthorized access is the bar for Critical. Same-tenant disclosure of operational state is Low.

---

## Methodology

Each in-scope file was traced line-by-line for the failure modes listed below. Findings are reported only with concrete file:line refs and a plausible attacker scenario; speculation about hypothetical channels with no code path is omitted. The "Investigated — no finding" section documents cleared-investigations so the audit's value isn't just in the issues found.

Investigated failure-mode categories:

1. **Timing side-channels** — does method X take measurably different time for tenant A's data vs. tenant B's?
2. **Status-code / error-shape distinguishers** — does the API response shape leak existence of other tenants' resources?
3. **Logging leaks** — do server logs capture content (queries, source text, embeddings) that operators with log access could read?
4. **Concurrency-observable state** — does a shared counter, queue, or pool let one tenant observe another's activity?
5. **Persistence leaks** — does WAL / snapshot / log capture vector data in a tenant-blind way?
6. **Cross-handler composition** — does an in-scope handler's behavior compose with a Track-R-adjacent surface to leak something neither alone would?

---

## Investigated — no finding

The bulk of the audit. Each item below was traced; the conclusion is "no leak under the attacker model."

### I-1. `HasVectorIndexForTenant` cross-tenant existence probing — `pkg/storage/vector_operations.go:117`

The method returns `false` uniformly for empty tenantID, unknown tenant, and known tenant with no index for the property. Routes through `pkg/storage/vector_index.go:HasIndexForTenant` which performs two O(1) map lookups under a read lock. Constant-time per Go map semantics; theoretical hash-collision variance is not a useful side channel for tenant-ID strings.

**Attacker can't reach this anyway**: the handler at `pkg/api/handlers_vectors.go` extracts tenantID via `getTenantFromContext(r)` which is set by auth middleware. An attacker authenticated as tenant A cannot supply a different tenant via the request; the API only ever calls `HasVectorIndexForTenant("A", ...)`.

### I-2. `VectorSearchForTenant` cross-tenant timing — `pkg/storage/vector_operations.go:107-128`

Same routing constraint as I-1: search is always against the caller's own tenant. Search latency depends on the caller's own `N_tenant` (their vector count) and `ef`. Self-timing is not a side channel; the caller already knows their own corpus size.

The per-tenant HNSW partition (R1.1) means another tenant's vectors don't affect this tenant's search time at all — the only memory walked is `indexes[caller_tenant][propertyName]`.

### I-3. Empty-tenantID fast path leak — `pkg/storage/vector_operations.go:107-128`

Public methods reject empty tenantID per F4 spike §1.3 (return `ErrNodeNotFound` for `Search`/`GetMetric`; descriptive error for `Create`/`Drop`; false/empty for `Has`/`List`). The fast-path return for empty tenantID is faster than the map-lookup path for non-empty unknown tenants.

**Not attacker-reachable**: `getTenantFromContext` defaults missing context to `tenant.DefaultTenantID` (= `"default"`), never empty string. Internal callers (`UpdateNodeVectorIndexes`, `RemoveNodeFromVectorIndexes`) also can't pass empty without an explicit miswiring.

### I-4. `WithNodeRefForTenant` post-filter in vector search — `pkg/api/handlers_vectors.go:337`

After R1.2, search returns only the caller's tenant's vectors (per-tenant HNSW partition). The post-filter still runs `WithNodeRefForTenant(sr.ID, tenantID, ...)` for each result, which validates tenant ownership AND holds the per-shard `RLock` around label/property filter evaluation.

Defense-in-depth: even if R1.x had a bug and a cross-tenant ID leaked into the search results, the post-filter returns `ErrNodeNotFound` and the handler skips the result. Stale results from concurrent deletes also drop here.

### I-5. `Pool.Dropped()` global counter — `pkg/intelligence/worker.go:151-153`

The pool's dropped-task counter is process-wide, not per-tenant. Tenant A submitting many creates increments the counter; tenant B observing the counter would learn that the pool is under pressure.

**Not externally observable**: `Pool.Dropped()` is not wired into any HTTP endpoint, Prometheus metric, or response header. The accessor exists for the unit + load tests and for future operator-side instrumentation. Tenant boundary on this hook is enforced by absence-of-API.

### I-6. LSA model cross-tenant leak — `pkg/search/tenant_lsa_indexes.go` + `pkg/intelligence/lsa_embedder.go`

Each per-tenant `*LSAIndex` is constructed by `search.BuildLSAIndex` from a tenant-scoped corpus. `TenantLSAIndexes.Get(tenantID)` returns the model for that tenant or nil. `LSAEmbedder.Embed(ctx, tenantID, text)` routes by tenantID; cross-tenant call paths return `ErrNoIndexForTenant`.

Each `*LSAIndex` has its own `vocab map[string]int32` and SVD basis. No shared state across tenants beyond the registry map's outer key. Tenant A's vocabulary is not observable from tenant B's `Embed`.

### I-7. Auto-embed observer cross-tenant routing — `pkg/intelligence/auto_embed_observer.go:172-184`

`OnNodeCreated` dispatches a task per matching policy. The task captures `node.TenantID` and writes back via `UpdateNodeForTenant(t.node.ID, ..., t.node.TenantID)`. The Embedder call also passes `node.TenantID`.

A node with `TenantID = "A"` cannot have its auto-embed result written to tenant B's namespace. The dispatch path is tenant-scoped end-to-end via the node's own field.

### I-8. WAL / snapshot vector exposure — `pkg/storage/snapshot.go` (not in scope), WAL apply paths

Vector indexes are not persisted as of R3 — the in-memory `VectorIndex` is rebuilt on restart from WAL replay of node creates + `UpdateNodeVectorIndexes` calls. No vector-specific snapshot entries exist; no WAL records of HNSW graph state.

Source text (the node property) DOES go through the WAL as part of node create/update records. But the WAL is global (already documented as such in `pkg/wal` package docs); it is not a Track-R-introduced leak. Operators with WAL access already have cross-tenant read access by design.

---

## Findings

### Medium

#### M-1. `/v1/embeddings` logs the FoldQuery error which contains the user's query string — `pkg/api/handlers_embeddings.go:153`

When `lsa.FoldQuery(input)` fails (out-of-vocabulary or zero-vector projection), the handler logs:

```go
log.Printf("embeddings: tenant=%s input[%d]: %v", tenantID, i, err)
```

`FoldQuery` has two error paths and both echo the query string verbatim:

- `pkg/search/lsa.go:415` — `fmt.Errorf("no vocabulary terms matched in query %q", query)` (out-of-vocabulary failure: no tokens map to the per-tenant LSA vocabulary)
- `pkg/search/lsa.go:451` — `fmt.Errorf("query %q maps to zero vector in LSA space", query)` (zero-vector projection: vocab matched but SVD projection collapses to origin)

Logs are therefore an unbounded sink for client-supplied text that triggered either FoldQuery failure mode.

**Attacker impact:** Not cross-tenant; a tenant cannot read another tenant's logs. But operators with log access (which in many SaaS setups includes shared support staff, error-tracking pipelines like Sentry, or aggregated structured-log indexes) can read every out-of-vocab query string passed to `/v1/embeddings`. Query strings may contain user PII, confidential queries, or sensitive contextual text.

**Fix:** Log the failure category and tenantID, NOT the original error message. Either:
- Drop the query content: `log.Printf("embeddings: tenant=%s input[%d]: %s", tenantID, i, "out-of-vocab or zero-vector projection")`
- Log only error class (use a typed error in `pkg/search`): `log.Printf("embeddings: tenant=%s input[%d]: %T", tenantID, i, err)`

The user-facing response (line 154-156) is already stable and doesn't echo the query; only the server-side log needs hardening.

**Status (2026-05-13):** Closed by PR #200 — `/v1/embeddings` handler now logs a fixed sanitization tag (`"out-of-vocab or zero-vector projection"`) at the log site rather than passing the FoldQuery error string through `%v`. Sanitization at the log site (handler) covers both `pkg/search/lsa.go:415` and `pkg/search/lsa.go:451` uniformly without needing per-error-type typed errors in `pkg/search`. The worker-side analogue (auto-embed observer's structured error logs) reuses the same fixed-vocabulary discipline via `embedErrorCategory` in PR #202.

### Low

#### L-1. `/v1/embeddings` returns 503 for missing-LSA; `/vector-search` returns 404 for missing-vector-index — `pkg/api/handlers_embeddings.go:121`, `pkg/api/handlers_vectors.go:285`

The semantically-equivalent condition ("this tenant has no `<vector-thing>` configured for this property") returns different HTTP status codes depending on the endpoint:

- `/v1/embeddings`: **503 Service Unavailable** with `"LSA index not built for this tenant; build via POST /hybrid-search/lsa-index"`
- `/vector-search`: **404 Not Found** with `"Vector index not found: <property>"`

503 implies *transient* (retry later); the condition is *permanent* (requires admin action). Inconsistent with the same-tenant-this-resource-doesn't-exist pattern used elsewhere.

**Attacker impact:** No cross-tenant leak (both endpoints route by `getTenantFromContext`). Pure consistency / operability issue. Self-tenant clients seeing 503 may add retry loops that never succeed.

**Fix:** Change `/v1/embeddings` 503 to 404 (or 412 Precondition Failed). Update the message wording to not promise transience.

---

## Out-of-scope observations

Things noticed during the trace that are NOT vector/embedding side channels but should be surfaced for follow-up.

### O-1. AutoEmbedObserver silently drops on every error path — `pkg/intelligence/auto_embed_observer.go:218-243`

The task's `Execute` method returns silently on:
- Pre-cancelled context
- Target property already set
- Source property missing
- Source property non-string
- Embedder returns any error (including `ErrNoIndexForTenant`)
- `UpdateNodeForTenant` returns any error

The docstring acknowledges this and says "errors are logged + metered at the wire-up layer" — but R2.5b's `bootstrapAutoEmbedFromEnv` does NOT wire logging or metrics. The observer has no visibility into auto-embed failures in production.

**This is an operability gap, not a security finding.** Misconfigured tenant + auto-embed → silent drops, no log, no metric. Operators only learn via "embedding property is missing on some nodes."

**Recommendation:** Add structured logging in the task's Execute (rate-limited so a misconfigured tenant doesn't flood the log) + a per-error-type counter visible via Prometheus.

**Status (2026-05-13):** Log dimension addressed in a follow-up PR that adds M-1-sanitized structured logs at the embedder-error, writeback-error, source-type-mismatch, and pool-panic-recovery paths. Pool drop events deliberately remain silent (S11 spike §7.5 — counters, not logs). Metrics dimension (Prometheus counters) remains open for a future track.

### O-2. Vector embedding inversion (generic concern; not graphdb-specific)

Vectors produced by LSA encode the source text in a lossy projection. For known corpora and a small attacker-recoverable vocabulary, partial source-text recovery from the projection is feasible (cosine-similarity-based reverse search). This is a property of vector embeddings generally and applies equally to graphdb's LSAEmbedder and any future enterprise-plugin embedder.

**Not a graphdb-specific issue and not addressable at the storage layer.** Document for awareness — if a future use case has high-sensitivity source text, the operator should treat the embedding as if it were the source for access-control purposes (the F4 spike's tenant-strict guards already do this — embeddings are per-tenant just like the source nodes).

### O-3. Bench-only artifact: `slowFakeEmbedder` is in the production package's test file — `pkg/intelligence/auto_embed_observer_load_test.go`

Not a security issue, but worth flagging: the load test imports `slowFakeEmbedder` from a test file. It's `*_test.go`-gated so it doesn't compile into the production binary; the symbol is unreachable from non-test code. Confirmed via `go build ./...` produces no `slowFakeEmbedder` references.

This is fine — Go's `*_test.go` rules enforce the isolation. Mentioned only because audit-reading reviewers occasionally flag "fake X in production" before checking the file suffix.

---

## Summary

| Severity | Count | Items |
|---|---|---|
| Critical | 0 | — |
| High | 0 | — |
| Medium | 1 | M-1 (log leak of query content) |
| Low | 1 | L-1 (status-code semantics inconsistency) |
| Investigated — no finding | 8 | I-1 through I-8 |
| Out-of-scope observations | 3 | O-1 (observability), O-2 (embedding inversion), O-3 (test-only symbols) |

**The F4 + S11 spikes' tenant-strict design plus the implementation discipline of R1.x/R2.x/R3 covered the obvious vectors.** The remaining issues are: one operationally-meaningful logging discipline issue (M-1), one consistency nit (L-1), and one operability gap that's not a security finding but should be tracked separately (O-1).

No findings at Critical or High severity were identified. **No code-change is required for the audit to be considered closed; M-1 is the only meaningful remediation work and is bounded to a one-line change in `handlers_embeddings.go`.**

The audit was bounded as specified by the planning doc's § Critical path option (C). The next planning checkpoint should record this audit's conclusion and route M-1 + L-1 + O-1 to the appropriate tracks (M-1 likely "operability/security follow-ups," L-1 a one-line API consistency PR, O-1 a separate observability-track audit if scope warrants).

---

## References

- F4 spike: [`F4_VECTOR_TENANT_REDESIGN.md`](./F4_VECTOR_TENANT_REDESIGN.md) §1.3 (existence-leak threat model), §6 (API method signatures + error shapes).
- S11 spike: [`S11_AUTO_EMBEDDER_REDESIGN.md`](./S11_AUTO_EMBEDDER_REDESIGN.md) §1.1 (mock-embedding facade), §7.2 (NodeObserver contract), §7.5 (bounded pool).
- Prior security audit: [`AUDIT_security_2026-05-06.md`](./AUDIT_security_2026-05-06.md) (Track A driver; predates Track R; sets the severity-calibration bar).
- Verification gap closure: PR #195 (memory bench), PR #196 (backpressure load test), PR #197 (deployment quickstart).
- Planning: `NEXT_STEPS_2026-05-15.md` § Critical path option (C) "new audit."
