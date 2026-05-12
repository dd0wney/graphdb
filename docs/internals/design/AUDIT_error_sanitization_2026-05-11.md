# Audit — Error-message sanitization at the HTTP boundary (2026-05-11)

**Trigger**: PR #109 review surfaced that `pkg/storage.UniqueConstraintError.Error()` includes `tenant=<tenantID>` in its formatted message. When that error reaches `respondError(w, 409, err.Error())`, the response body becomes a cross-tenant existence-leak probe ("is `for_task=X` claimed in tenant `T`?"). This audit checks how many other sites have the same shape.

**Scope**: Every site in `pkg/api/` where `err.Error()` (or `fmt.Sprintf("%v", err)`) reaches `respondError`'s body. Tests excluded. As of `main` HEAD `a0e8552`: **24 direct `err.Error()` sites + 3 `fmt.Sprintf("...: %v", err)` sites = 27 surfaces**.

**Out of scope**: Errors that travel through `respondJSON` (these are non-error responses with embedded error fields, see `pkg/api/handlers_compliance.go`), logs/metrics emission (internal), audit-event records (admin-only, intentionally detailed).

## TL;DR

The repo already has good sanitization primitives — `sanitizeError(err, op)` and `wrapForClient(err, op)` in `pkg/api/handler_helper.go` — and the algorithms layer uses them consistently. The audit found:

- **21 of 24 `err.Error()` sites are SAFE** (validation errors with user-supplied data, or pre-sanitized via `wrapForClient`).
- **1 LEAKY site** at `handlers_apikeys.go:179` (RevokeKey 500 → raw store error).
- **2 NEEDS-INVESTIGATION sites** at `handler_helper.go:136` (`requestDecoder.RespondError`) and `handlers_apikeys.go:141` (CreateKeyWithEnv 400).
- **1 ROOT-CAUSE leak** in `pkg/storage/errors.go:32` (`UniqueConstraintError.Error()` formats `tenant=<id>`) — currently only reaches HTTP responses via PR #109's H4.4 work, but the source-level fix is the right place to plug it.
- **3 `fmt.Sprintf("...: %v", err)` sites** in `pkg/api/server_handlers.go` (lexer/parser/query errors) — SAFE (errors come from the query language, not from storage internals).

The headline finding: **the discipline is already in place**, it's the storage layer's error formatting that's the actual leak source. Fixing `UniqueConstraintError.Error()` to omit `tenant=` (or returning a structured error type that the API layer can format safely) closes the practical leak without touching most of the 24 sites.

## Classification by site

### SAFE (21 sites)

| File:Line | Status code | Source | Why safe |
|---|---|---|---|
| `handlers_algorithms_generic.go:38` | 400 | `executePageRank` → `wrapForClient` | `clientError.Error()` returns `"PageRank computation failed"`; raw err only logged. |
| `handlers_algorithms_generic.go:46` | 500 | `executeBetweenness` → `wrapForClient` | Same pattern. |
| `handlers_algorithms_generic.go:54` | 500 | `executeEdgeBetweenness` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:62` | 400 | `executeDetectCycles` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:70` | 500 | `executeHasCycle` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:78` | 500 | `executeTriangles` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:86` | 500 | `executeSCC` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:94` | 400 | `executeNodeSimilarity` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:102` | 400 | `executeLinkPrediction` → `wrapForClient` | Same. |
| `handlers_algorithms_generic.go:110` | 400 | `executeKHop` → `wrapForClient` | Same. |
| `handlers_tenant.go:98` | 409 | `tenantStore.Create` → `fmt.Errorf("%w: %s", ErrTenantExists, tenant.ID)` | Tenant ID is user-supplied, echoed back. |
| `handlers_tenant.go:101` | 400 | `tenantStore.Create` (non-conflict) | Validation errors only (ID format / quota). |
| `handlers_tenant.go:232` | 400 | `tenantStore.Update` | Validation; no internal-state leak. |
| `handlers_tenant.go:278` | 403 | `tenantStore.Delete` "cannot delete" | Literal `"cannot delete default tenant"`. |
| `handlers_tenant.go:281` | 400 | `tenantStore.Delete` (other) | Validation only. |
| `handlers_tenant.go:394` | 400 | `tenantStore.Suspend` (non-404) | Validation only. |
| `handlers_tenant.go:446` | 400 | `tenantStore.Activate` (non-404) | Validation only. |
| `handlers_nodes.go:110` | 400 | `validation.ValidateNodeRequest` | Pure validation. |
| `handlers_nodes.go:173` | 400 | `validation.ValidateBatchSize` | Pure validation. |
| `handlers_edges.go:90` | 400 | `validation.ValidateBatchSize` | Pure validation. |
| `handlers_search_admin.go:177` | 422 | `BuildLSAIndex` only when err contains "vocabulary size" / "empty document corpus" | Substring-gated to user-correctable. The 500 fallthrough at line 180 correctly uses `sanitizeError`. |

### LEAKY (1 site)

| File:Line | Status code | Risk | Recommended fix |
|---|---|---|---|
| `handlers_apikeys.go:179` | 500 | `apiKeyStore.RevokeKey` error reaches response body raw. RevokeKey can fail with store-internal errors (file I/O, JSON marshal). On 500, internal details leak; on the path where `keyID` is in the error message, that's a key-ID echo back to the caller (acceptable, the caller already knows the ID they passed) — but other internal error fields (file paths, marshal failures) are not safe. | Replace with `sanitizeError(err, "revoke api key")`. Mirrors line 180's pattern in `handlers_search_admin.go`. |

### NEEDS-INVESTIGATION (2 sites)

| File:Line | Status code | Concern |
|---|---|---|
| `handlers_apikeys.go:141` | 400 | `apiKeyStore.CreateKeyWithEnv` error is treated as 400, but the store can return errors that should logically be 500 (e.g., persistence-layer failures during create). The class of error isn't inspected; everything → 400 with raw message. **Action**: inspect every error path in `CreateKeyWithEnv` and confirm the 400-only handling is correct; if any 500-class errors can surface here, split the branches like `handlers_search_admin.go:174-180` does. |
| `handler_helper.go:136` | varies | `requestDecoder.RespondError()` is generic — used by callers that set `rd.err` via JSON decode, validation, or any other error. The caller controls the status code (400 most commonly). Most uses are body-decode errors (`json.NewDecoder(r.Body).Decode(&req)`), which can leak parser state (e.g., position offsets) but not internal storage details. **Action**: confirm by `grep`-counting callers and inspecting any non-decode use. If decode-only, this is SAFE in practice; otherwise needs the same `sanitizeError` treatment per-caller. |

### Source-level: `pkg/storage/errors.go:32`

Not in the 24-site count, but the **root cause** of PR #109's finding:

```go
func (e *UniqueConstraintError) Error() string {
    return fmt.Sprintf("unique constraint violation: tenant=%s label=%s property=%s already held by node %d",
        e.TenantID, e.Label, e.PropertyKey, e.ConflictingNodeID)
}
```

The `tenant=%s` substring is the leak. When PR #109 forwards `err.Error()` into a 409 response body for `POST /nodes` :Claim creation, callers can probe whether a `for_task` value is claimed in another tenant by submitting it as their own tenant's claim — a true existence-leak side channel.

**Recommended fix**: omit `TenantID` from `Error()`. The error still carries `TenantID` as a struct field, accessible via `errors.As` for callers that legitimately need it (logs, audit). Drop it from the human-readable `Error()` string. The API layer can format its own response body from struct fields (e.g., PR #109's review recommended a structured JSON 409 with `winning_node_id` field but no `TenantID` field).

This is a one-line fix in `pkg/storage/errors.go` plus a possible test update if any test asserts the exact error string.

### `fmt.Sprintf("...: %v", err)` sites (3, all in `server_handlers.go`)

| Line | Source | Status |
|---|---|---|
| 114 | `fmt.Sprintf("Invalid query: %v", err)` | SAFE — query parser error, echoes user-supplied query syntax. |
| 141 | `fmt.Sprintf("Lexer error: %v", err)` | SAFE — same. |
| 148 | `fmt.Sprintf("Parser error: %v", err)` | SAFE — same. |

These are query-language errors. They contain positions and tokens from the user-submitted query, not storage internals.

## Recommended fixes (prioritized)

### P0: storage-layer source fix

- **`pkg/storage/errors.go:32`** — omit `TenantID` from `UniqueConstraintError.Error()`. One-line change + test update. Closes the leak at the source, mooting any handler-layer mitigation.

### P1: handler-layer fixes

- **`pkg/api/handlers_apikeys.go:179`** — replace `err.Error()` with `sanitizeError(err, "revoke api key")` on the 500 path.

### P2: investigations

- **`pkg/api/handlers_apikeys.go:141`** — audit `CreateKeyWithEnv` error paths; if any 500-class can surface, split branches.
- **`pkg/api/handler_helper.go:136`** — grep callers of `requestDecoder.RespondError()`; if all are decode/validate, document the assumption.

### P3: defensive (optional)

- Add a lint check (golangci-lint custom rule or pre-commit hook) that flags new `err.Error()` arguments to `respondError` outside of the established pattern. Would require either:
  - A wrapper helper `respondErrorSafe(w, code, err, op)` that callers use instead of `respondError(w, code, err.Error())`; or
  - A list of approved callers as a configuration.

  Tradeoff: lint enforcement adds friction but catches regressions. The current discipline is already strong (21/24 sites correct), so this may be over-engineering until a new leak surfaces.

## How this audit relates to F3

F3's read-path masking (PR #114) operates on response *bodies* for `node.Properties` and `edge.Properties`. This audit operates on response *error bodies* for the same write paths. The two coverage surfaces are complementary:

- F3 PR-3a: success-path response bodies masked per tenant policy.
- This audit's fixes: error-path response bodies sanitized to not leak cross-tenant existence.

The F3 design doc §3 Decision 1c also names `/security/audit/logs` deprecation (closed by PR #117 today) as a sibling cleanup. This audit's P0 fix (`UniqueConstraintError.Error()`) is a similar-shape cleanup that emerged from the F3 review cycle.

## Coordination

- This doc closes `graphdb:storage-error-sanitization-audit` in coord (Layer 0, F-track).
- Each P0/P1 fix below should be a separate PR (storage layer change vs. apikeys handler change). Worth opening them as `feat/sanitize-unique-constraint-error` and `feat/sanitize-apikeys-revoke` respectively.
- P2 investigations may become P1 fixes after inspection; surface as additional coord tasks if so.
