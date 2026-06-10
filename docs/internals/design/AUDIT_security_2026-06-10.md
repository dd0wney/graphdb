# Security audit — 2026-06-10

**Scope:** full-surface re-audit, commissioned per Decision 10 option B (`NEXT_STEPS_2026-06-03.md`). The last security audit was 2026-05-06; since then the attack surface grew substantially: admin `login`/`mint-token` (#348), the Value⇄JSON converter + `TypeJSON` (#344/#345), Cypher param substitution (#347), edge mutation endpoints (#330/#332), `/traverse` filters (#342), index-level pagination (#366), tenant cascade deletion (#340), WAL group-commit + `AppendBatchAtomic` + Transaction durability (#255–#280), vector-index WAL ops (#320), HNSW rebuild-on-load (#305), and the entire Python SDK (M1→M4).

**Covered ground NOT re-derived here** (start from these, don't re-audit):
- Tenant isolation — `AUDIT_tenant_isolation_2026-06-04.md` (request-facing posture strong; its "confirmed clean" list held up under this audit's fresh passes).
- Error sanitization — `AUDIT_error_sanitization_2026-05-11.md`.
- Vector/embedding side-channels — `AUDIT_vector_embedding_side_channels_2026-05-15.md`.
- The 2026-05-06 findings — verified fixed where touched (e.g. old finding 4: `JWT_SECRET` is now fail-closed at `pkg/api/server_init.go:79`; old finding 6: admin checks moved to route registration for all but two defensible handler-level sites).

## Method

Six parallel read-only specialist audits — (1) authn/authz machinery, (2) input handling & injection, (3) new/changed REST surface, (4) storage/WAL/data-at-rest, (5) DoS & resource exhaustion, (6) first-party clients (Python SDK + TS Workers client) — plus `gosec v2.27.1` and `govulncheck` tool scans. Load-bearing High findings were verified at source (file:line quoted below); two findings were verified experimentally by the auditing agent (SDK-1's httpx path normalization); two were independently converged on by two auditors (DR-1 remanence, DOS-1 rate limiting). Findings the auditors themselves refuted or that tool-noise triage excluded are listed in § Excluded.

**Threat model labels:** `[tenant]` = authenticated tenant user via API; `[admin]` = compromised/malicious admin credential; `[local]` = attacker with OS/filesystem access to the host; `[operator]` = misconfiguration; `[sdk-consumer]` = application embedding a first-party client.

## Findings — High

### H-1. Suspended tenants retain full access — `withTenant` uses `Get()` not `GetActive()` `[tenant]`
`pkg/api/middleware_tenant.go:37`. The middleware validates tenant existence with `s.tenantStore.Get(tenantID)`, which ignores `TenantStatus`. `GetActive()` (`pkg/tenant/store.go:106`) exists but has no API-layer caller. A tenant suspended via `POST /api/v1/tenants/{id}/suspend` keeps full read/write access to every data endpoint until its JWTs expire (15 min access / 7 day refresh). Suspension is currently cosmetic.
**Fix:** use `GetActive()` in `withTenant` (keep `Get()` only for the default-tenant auto-create branch); regression test: suspended tenant → 403.

### H-2. WAL + LSA snapshots world-readable; SSTables umask-dependent `[local]`
`pkg/wal/wal.go:32` (0644), `pkg/wal/compressed_wal.go:19`, `pkg/wal/fileutil.go:29,100`, `pkg/search/lsa_persistence.go:157` (0644), `pkg/lsm/sstable_create.go:26` (`os.Create` → 0666 pre-umask). WAL entries carry full serialized node/edge JSON (all customer properties); `.lsa` snapshots carry the raw per-tenant content map. Any local OS user reads all-tenant data without credentials. Corroborated by gosec G302×13 / G301×10 / G306×3.
**Fix:** 0600 files / 0700 dirs across `pkg/wal`, `pkg/search` LSA persistence, `pkg/lsm` (mirror `pkg/storage`'s explicit-permission constants).

### H-3. WAL is plaintext even when snapshot encryption is enabled `[local]`
`pkg/storage/persistence_wal.go:26-41` vs `persistence.go:91-96`. `SetEncryption()` encrypts only `snapshot.json`; every WAL entry (i.e. everything written since the last snapshot) is raw JSON. An operator who enabled encryption gets silent partial coverage.
**Fix:** encrypt WAL entry payloads through the same engine, or document loudly + alert until implemented. Pairs with H-2.

### H-4. Crafted WAL record `dataLen=0xFFFFFFFF` → ~4 GB allocation before CRC check → restart-loop DoS `[local]`
`pkg/wal/wal_serialization.go:69`, `pkg/wal/compressed_wal_io.go:140`. `make([]byte, dataLen)` happens before any CRC validation; a 13-byte poisoned record OOM-kills the server on every restart (DoS persistence).
**Fix:** `maxWALRecordSize` cap (e.g. 64 MB) checked before allocation at both sites; treat oversize as corruption (stop-replay, same as CRC mismatch).

### H-5. General API rate limiting is OFF by default `[tenant]` *(convergent: DOS + AUTH auditors)*
`pkg/api/middleware_ratelimit.go:55-59`: the general limiter is nil unless `RATE_LIMIT_ENABLED=true` — `middleware/ratelimit.go:225` then passes everything through. Only `/auth/*` brute-force limiting is always on. Every other finding in the DoS section is amplified by this default.
**Fix:** default-on with opt-out; key limits by user/tenant identity, not just IP.

### H-6. Heavy algorithms ignore context cancellation — uncancellable multi-minute goroutines `[tenant]`
`pkg/api/handlers_algorithms_generic.go:179-193`, `pkg/algorithms/centrality.go:22-105`, `pkg/algorithms/node_similarity.go:229-270`. Betweenness / edge-betweenness / triangles / SCC / all-pairs similarity get a pre-flight `ctx.Done()` check only; the 60 s deadline has nothing to cancel inside the O(N·E) loops. The client connection drops at 30 s but the goroutine runs to completion. Repeated requests stack goroutines without bound (compounds H-5).
**Fix:** thread `context.Context` into the algorithm inner loops (or wrap + abandon with 408); cap concurrent algorithm executions.

### H-7. Unbounded HNSW `M`/`ef_construction` on index create `[tenant]`
`pkg/api/handlers_vectors.go:162-196` (verified: floor-only defaults, no ceiling), `pkg/vector/hnsw.go:61-80` (validates `> 0` only). `M=100000` makes every subsequent node insert a multi-second CPU+memory event holding the index write mutex. `/vector-search`'s `ef` is similarly uncapped (its `k` is capped at 1000).
**Fix:** API-layer ceilings (`M≤128`, `ef_construction≤2048`, `ef≤4096`).

### H-8. `/traverse` has no result-count cap `[tenant]`
`pkg/api/handlers_algorithms_traversal.go:83-95`. Depth (≤100) and a 60 s deadline are enforced — but a dense tenant graph materializes ALL reachable nodes into one slice + one JSON array per request. Memory pinned for the full window; concurrency amplifies linearly (compounds H-5).
**Fix:** `MaxTraversalNodes` cap (e.g. 10 K) with a truncation header, mirroring the pagination model.

### H-9. Toolchain: Go 1.26.3 carries 2 reachable stdlib vulns, fixed in 1.26.4 `[operator]`
`govulncheck`: GO-2026-XXXX via `http.Server.ListenAndServe → textproto.ReadMIMEHeader` (`pkg/api/server.go:271`) and GO-2026-5037 (`crypto/x509` hostname parsing) via `pkg/security/tls_validation.go:84`. Both on live paths.
**Fix:** bump `toolchain go1.26.4` in go.mod + rebuild/release. CI pins `go-version: '1.26'` (resolves to latest patch on runners), so this primarily affects local/release builds pinned older.

### H-10. Python SDK: path traversal via unencoded string path params `[sdk-consumer]` *(experimentally verified)*
`clients/python/src/graphdb_client/resources/vector_indexes.py:41,46`, `compliance.py:50`, `tenants.py:40-80`, plus `api_keys` `key_id` and the `aio/` mirrors. String params are f-string-interpolated into paths; httpx normalizes `..` segments before sending — confirmed: `masking-policy/../admin` → `/v1/compliance/admin`. A caller-supplied `tenant_id` of `../other-tenant` retargets the request. Integer-ID resources are safe.
**Fix:** `urllib.parse.quote(value, safe="")` on every string path segment (sync + async).

### H-11. TS Workers client cache has no identity namespace — cross-user data bleed by default `[sdk-consumer]`
`workers/graphdb-client/src/cache.ts:208-222`. Keys are `trust:{userId}` / `node:{nodeId}` / `traversal:{start}:…` with no requesting-identity component, and the KV namespace is Worker-global: two users' requests hitting the same Worker share cached graph reads. Unlike the Python SDK (where sharing a backend across auth contexts is an explicit, documented opt-in — see M-10), this is the client's default documented usage.
**Fix:** add a tenant/user context parameter to `GraphDBCache` and prefix all generated keys.

## Findings — Medium

- **M-1. Tenant-delete data remanence in the WAL** `[local]`/compliance *(convergent: REST + STOR auditors)* — `pkg/api/handlers_tenant.go:291-335`, `pkg/storage/persistence.go:283-309`. `DeleteTenant` appends delete-ops but prior create/update entries (full PII) persist until the next graceful-close snapshot+truncate; `pkg/compliance` GDPR controls don't mention WAL purging. **Fix:** snapshot+truncate after cascade (or background compaction + documented remanence window in the GDPR control).
- **M-2. LSA snapshot delete failure swallowed on tenant delete** `[local]` — `handlers_tenant.go:299-303` logs and returns 200; the orphaned `.lsa` file holds the deleted tenant's full content map (compounded by H-2's 0644). **Fix:** fail or alert + retry queue.
- **M-3. GraphQL depth limit exists but is not wired** `[tenant]` — `pkg/graphql/depth.go:148` vs `pkg/api/server_handlers.go:287-299`: only `ValidateQueryComplexity` runs; structure-based complexity scores deep nesting cheaply, leaving resolver-recursion DoS open. **Fix:** call `ValidateQueryDepth` (maxDepth ≈ 10) next to the complexity check.
- **M-4. `/auth/*` endpoints have NO body-size limit (pre-auth)** `[anonymous]` — `pkg/api/middleware/input_validation.go:22-29` SkipPaths exempts login/register/refresh from the only `MaxBytesReader` in the chain (verified); `bodySizeLimitMiddleware` (`middleware_wrappers.go:24`) exists but is absent from the `Start()` chain. A 500 MB body to `/auth/login` is read whole by `json.NewDecoder`, pre-auth. **Fix:** wire `bodySizeLimitMiddleware` globally; strict small cap (64 KB) on `/auth/*`.
- **M-5. Log injection via `X-Tenant-ID` (and no tenant-ID format validation)** `[tenant]` — `pkg/api/middleware_tenant.go:73,77`: raw header logged via `log.Printf`, newlines included; any authenticated user reaches line 77. Admin path also accepts arbitrary-length/charset tenant IDs into storage keys + audit logs (corroborated by gosec G706×20). **Fix:** ingress allowlist `^[a-zA-Z0-9_-]{1,64}$` for tenant IDs; escape control chars in log values generally.
- **M-6. `mint-token` defaults to admin role** `[operator]` — `cmd/graphdb-admin/auth.go:80`. Requires `JWT_SECRET` to run, so it's not an escalation (the holder can mint anything) — but the default shapes operator habit toward admin tokens for service accounts. **Fix:** default `viewer` or make `--role` required; warn loudly on admin mints.
- **M-7. No token revocation; role changes don't propagate to live tokens** `[tenant]` — `pkg/auth/handlers.go:34` (no logout/blocklist), `pkg/api/middleware_auth.go:95` (existence check only, role read from claims). Compromise/downgrade window = full token lifetime; only global-secret rotation revokes. **Fix:** per-user generation counter embedded as a claim, checked in `requireAuth`.
- **M-8. OIDC gaps: user-management handlers bypass the composite validator; `nbf` not checked** — `pkg/auth/handlers_operations.go:113` + `handlers_users.go:28` validate via bare `jwtManager` (OIDC admins get 401 → forces a parallel local-admin credential store); `pkg/auth/oidc/token_validator.go:124` skips not-before. **Fix:** thread the composite `TokenValidator` into both handlers; add `nbf` check.
- **M-9. WAL `AppendBatchAtomic` LSN rollback wrong on mid-batch write failure** `[operator]` — `pkg/wal/batched_wal.go:198-213`: rollback subtracts `len(entries)` instead of entries-written; subsequent appends reuse LSNs and buffered partial entries can flush. Disk-full/IO-error trigger, not API-reachable. **Fix:** roll back by `written`; mark WAL corrupted to block flushing the partial tail.
- **M-10. Python SDK cache key lacks identity namespace (shared-backend misuse)** `[sdk-consumer]` — `clients/python/src/graphdb_client/_caching.py:14-18`: key is `METHOD:path?params`; a shared external `CacheBackend` across differently-authed clients cross-serves responses. Documented warning exists; default `InMemoryCache` is per-instance and safe. **Fix:** optional `namespace` in `CacheConfig` + warn when an external backend is wired without one.
- **M-11. SDK retry semantics: TS client retries ALL methods on 5xx/network errors; Python's 429-retry-comment overclaims safety** `[sdk-consumer]` — `workers/graphdb-client/src/client.ts:452-454, 426-429` (no idempotency discrimination at all → duplicate mutations under transient failures); `clients/python/src/graphdb_client/_retry.py:47` (429 ≠ "not processed"). **Fix:** idempotent-method guard in TS; fix the Python comment + consider blocking write-POST on 429-retry.
- **M-12. Python SDK: `trust_env=True` default lets `HTTPS_PROXY` exfiltrate the bearer token** `[sdk-consumer]` — `clients/python/src/graphdb_client/_transport.py:40` + `aio/transport.py:38`: env-injected proxy receives the `Authorization` header. **Fix:** `trust_env=False` + explicit proxy parameter.
- **M-13. `CreatedAPIKey` dataclass reprs the plaintext key** `[sdk-consumer]` — `clients/python/src/graphdb_client/models.py:296-314`: default dataclass `__repr__` puts the one-time key into logs/tracebacks/crash reporters. **Fix:** `field(repr=False)`.
- **M-14. Snapshot encrypted-vs-plaintext detection is a first-byte heuristic** `[operator]` — `pkg/storage/persistence.go:129` (`data[0] != '{'`); a BOM or leading whitespace bricks load. The LSA snapshot already does this right (magic + version, `lsa_persistence.go:52`). **Fix:** magic header + version envelope for `snapshot.json` (needs the snapshot-format version-bump discipline).
- **M-15. Enterprise `.so` plugins load with no hash/signature verification, default dir is CWD-relative** `[local]`/supply-chain — `pkg/plugins/loader.go:42-79`: `plugin.Open` runs `init()` before any interface assertion; write access to `./plugins` (or `GRAPHDB_PLUGIN_DIR`) = RCE as the server. **Fix:** SHA-256 manifest verification pre-`Open`; require absolute path; 0700 ownership check.
- **M-16. Admin audit-log endpoints unbounded** `[admin]` — `pkg/api/handlers_security.go:152-153` (export serializes the whole ring buffer; `logs?limit=` has no upper cap at line 114, unlike the F3 compliance handler's 1000). **Fix:** mirror the F3 cap; paged export.

## Findings — Low

- **L-1. Edge `Update/DeleteEdgeForTenant` TOCTOU** — `pkg/storage/edge_operations.go:91-103,212-222`: tenant check under shard-RLock, mutation under a later global lock; benign today (no ID reuse; re-check yields 404) but the node ops do the check inside the write lock — mirror that.
- **L-2. `GET /edges?from=<foreign-node>` timing side-channel** — `pkg/storage/query_operations.go:64-76` walks the global adjacency of a node the caller doesn't own before post-filtering to `[]`; response *content* is leak-free, latency isn't. Gate with `GetNodeForTenant` first.
- **L-3. ID-gap inference via pagination cursors** — global sequential IDs already appear in create responses; cursors (#366) just make cross-tenant write-volume inference cheaper. Accepted-risk candidate; opaque HMAC cursors or per-tenant IDs are the (expensive) fixes.
- **L-4. Zombie tenant on partial delete** — `handlers_tenant.go:291-313`: if `tenantStore.Delete` fails after the graph cascade, the tenant still authenticates against an empty graph. Pre-tombstone (status=DELETING) before cascading.
- **L-5. Cypher sanitizer SQL-pattern false positives** — `pkg/query/sanitizer.go:23-26`: pre-parse substring match rejects legitimate queries containing `drop ` / `delete from` in string literals; the patterns defend against SQL, which this engine doesn't speak. Availability/correctness, not injection.
- **L-6. `UniqueConstraintError` 409 body discloses the conflicting node ID** — `pkg/storage/errors.go:57` → `handlers_nodes.go:135`. Same-tenant ID only (uniqueness is per-tenant), and IDs already appear in create responses — intentional for coord callers. Document as contract or strip + `errors.As` for internal callers.
- **L-7. float64→TypeInt misclassification near MaxInt64** — `pkg/storage/value_from_json.go:119`: whole floats in [2^63−512, 2^63+1024) silently store a corrupted int64. Add a ±2^53 exact-integer range guard.
- **L-8. `/auth/register`-adjacent: `GET /api/v1/tenants/{id}/usage` admin check lives in the handler, not the dispatcher** — `handlers_tenant.go:554` vs the suspend/activate pattern; correct today, drift-prone.
- **L-9. SDK redirect behavior undocumented** — Python's `follow_redirects=False` is protective (keep + comment); TS `fetch` default follows same-origin redirects with the auth header — set `redirect: 'manual'`.
- **L-10. Python SDK cache: POST mutations don't invalidate** + fail-open cache errors are fully silent — documented staleness, but a broken external backend degrades silently. Log-once on cache errors; consider invalidating known write-POST paths.

### L-tier disposition (2026-06-10 follow-up)

| # | Disposition |
|---|---|
| **L-5** | **Fixed** — SQL patterns removed from the Cypher sanitizer (this engine doesn't speak SQL; the patterns false-positived on legit string literals). Pinned by the two L-5 cases in `sanitizer_test.go`. |
| **L-7** | **Fixed** — `ValueFromJSON` now only collapses a float to int within ±2^53 (the exact-integer range), so floats near MaxInt64 stay float instead of silently corrupting. Pinned by `TestValueFromJSON_LargeFloatStaysFloat`. |
| **L-9, L-10** | **Fixed** in the client release (#379 Python `follow_redirects=False` + cache-error logging; #380 TS `redirect:'manual'`). |
| **L-1** | **Accept-risk / deferred.** Edge `Update/DeleteEdgeForTenant` TOCTOU is benign without ID reuse (re-check yields 404); moving the check inside the write lock is a storage-concurrency change better done with the M-1 WAL-compaction spike than as a one-line cleanup. |
| **L-2** | **Accept-risk.** `?from=<foreign-node>` adjacency-length timing channel: content is leak-free; the latency signal is low-value and the gate (`GetNodeForTenant` first) adds a lookup to the hot path. Revisit if a timing-oracle threat is in scope. |
| **L-3** | **Accept-risk (by design).** Global sequential IDs already appear in create responses; opaque-cursor / per-tenant-ID fixes are a snapshot-format change disproportionate to the inference value. |
| **L-4** | **Deferred** to the M-1/delete-path work. Zombie-tenant-on-partial-delete wants a pre-tombentone (status=DELETING) delete-ordering change — pairs naturally with the M-1 remanence spike. |
| **L-6** | **By design (documented).** The 409 conflict body discloses a *same-tenant* node ID intentionally for coord callers; IDs already appear in create responses. No change. |
| **L-8** | **Accept-risk (documented).** `GET …/usage`'s admin check lives in the handler, not the dispatcher; correct today (the handler's self-tenant comparison is sound), flagged drift-prone. A dispatcher-level move is a behavior-preserving refactor deferred to avoid churn here. |

## Tool-scan triage (gosec 400 issues, govulncheck)

| Class | Count | Disposition |
|---|---|---|
| G302/G301/G306 file perms | 26 | Real — folded into **H-2** |
| G706 log injection | 20 | Real instances folded into **M-5**; rest are operator-supplied values |
| G115 int overflow conversions | 113 | Mostly shard-index/length conversions on internal values; no API-reachable overflow identified by the input auditor — sample-audit before bulk-suppressing |
| G104 unhandled errors | 133 | Code-quality backlog, not security findings (existing `//nolint` policy applies) |
| G404 math/rand | 64 | Benchmarks/jitter; API keys + JWT already use crypto/rand (verified) |
| G101 hardcoded creds | 4 | Test-framework placeholder JWT in `enterprise_tests_framework.go:219` — not a secret |
| G402 InsecureSkipVerify | 1 | Config pass-through (`pkg/tls/config.go:43`), operator-controlled — fine |
| G703 path traversal | 3 | License-file paths, operator-controlled — fine |
| govulncheck | 2 reachable | **H-9** (toolchain 1.26.4); plus 3 imported-not-called + 14 module-level — recheck after bump |

## Consolidated confirmed-clean (verified this audit — next audit starts here)

- **JWT core**: HMAC-only enforced (`alg:none` rejected); `JWT_SECRET` fail-closed (old finding 4 FIXED); double expiry validation; bcrypt cost 12.
- **API keys**: 256-bit crypto/rand, HMAC-SHA256 hashed at rest, constant-time compare, env (test/live) enforcement, revocation honored + persisted.
- **Admin gating**: all admin routes wrapped `requireAdmin` at registration; `mint-token` adds no escalation beyond `JWT_SECRET` possession; auth brute-force limiter always on (5 r/s).
- **Cypher params** (#347): resolved pre-execution, no string interpolation, unresolved refs fail loud. **Pagination cursors** (#366): `ParseUint`-validated, tenant-set-bounded — cross-tenant cursor probing hits only the caller's own sorted set (no oracle). **`/traverse` filters** (#342): enum-validated direction; `edge_types` is a caller-supplied allowlist, not an enumeration surface.
- **Edge mutation** (#330/#332): tenant-enforced via `getEdgeRefForTenant` with uniform not-found; only Properties+Weight mutable (live + WAL replay); non-finite weight rejected pre-WAL.
- **WAL robustness**: CRC-32 per entry with stop-on-mismatch; truncated-tail recovery; unknown op-types skipped; snapshot writes atomic (tmp+rename); LSA snapshots have magic+version and path-traversal-safe tenant filenames.
- **Search/index admin ops**: FTS + LSA rebuilds `requireAdmin` + tenant-scoped. **Limits present**: 10 MB body cap (authed routes), batch ≤1000, vector-search k ≤1000, dimensions ≤4096, GraphQL complexity ≤5000, query timeouts 1–300 s, traversal ctx-cancellation works.
- **SDK**: TLS verify on; tokens not repr'd on transports; LangChain adapters don't interpolate user content into queries; httpx bounded `>=0.27,<1`; core stays httpx-only; `Retry-After` parsing robust; `InMemoryCache` lock-protected.

## Excluded (auditor-refuted or noise — recorded so they aren't re-found)

- TypeJSON depth-recursion DoS: storage converters operate on already-decoded values under the 10 MB body cap (authed routes) — no second-parse recursion. (The *pre-auth* gap is M-4.)
- GraphQL introspection cross-tenant: closed by A9 (per-tenant schema), reconfirmed.
- `:Claim`/`for_task` resolver abuse: uniqueness scoped per-tenant; see L-6 for the only residue.
- gosec G101/G402/G703 — see triage table.

## Recommended backlog (severity × effort)

**Wave 1 — small fixes, big posture (each ≤ a day):** H-1 (GetActive), H-2 (file perms), H-4 (WAL record cap), H-7 (HNSW caps), H-8 (traverse cap), H-9 (toolchain bump), M-4 (auth body cap), M-3 (wire depth limit), M-5 (tenant-ID allowlist), M-6 (mint-token default), M-13 (repr=False), M-16 (audit-log caps).
**Wave 2 — SDK release (bundle as one client release):** H-10 (path quoting), H-11 (TS cache namespace), M-10/M-11/M-12, L-9/L-10.
**Wave 3 — design-required:** H-3 (WAL encryption), H-5 (rate-limit default — config-compat decision), H-6 (ctx-threading through algorithms), M-1/M-2 (deletion remanence — interacts with pkg/compliance), M-7 (revocation generation counter), M-8 (OIDC validator threading), M-15 (plugin verification — enterprise repo coordination), M-14 (snapshot magic header — format-bump discipline).

**Verdict:** materially stronger than 2026-05-06 (that audit's verdict was FAIL on broken multi-tenancy; the tenant posture is now the *strongest* part). Today's profile is: hardening gaps in defaults (rate limiting, file permissions, resource ceilings) + a compliance-grade data-remanence story + client-library polish. Nothing found suggests live cross-tenant data exposure.
