# Capabilities Audit (graphdb) — 2026-05-10

**Purpose**: catalogue what exists across both repositories of the open-core graphdb product, with a maturity tag per component. Input to the next planning checkpoint and to the commercial-offering decision flagged in `NEXT_STEPS_2026-05-10.md` ("Known limitations + productization gaps").

**Methodology**: file-tree survey + spot-read of doc comments and test counts. Maturity tags are coarse heuristics, not deep audits. Tags marked **[?]** mean "not personally verified for this audit; tag is inferred from test count + file count."

## Open-core model

graphdb ships in two repositories:

| Repository | Visibility | Contents |
|---|---|---|
| [`dd0wney/graphdb`](https://github.com/dd0wney/graphdb) | Public | OSS core: graph engine, REST + GraphQL APIs, vector search, query language, auth, license validation framework, plugin loading system. |
| [`dd0wney/graphdb-enterprise`](https://github.com/dd0wney/graphdb-enterprise) | Private | Enterprise plugins distributed as `.so` (Go plugin) binaries. License-gated at runtime. Currently 2 plugins built, ~4 more named in `docs/ENTERPRISE_PLUGINS.md` but not yet implemented. |

The OSS repo defines `pkg/plugins/EnterprisePlugin` + specialised sub-interfaces (`StoragePlugin`, `APIPlugin`, `BackupPlugin`, `MonitoringPlugin`); the enterprise repo implements them. Plugin loader code is OSS; plugin code is closed. License validation framework is OSS; license issuance is presumably internal (`cmd/license-server` exists in OSS but its production deployment is out of scope for this audit).

## Maturity tag legend

- **mature**: heavy test coverage, evidently in active production paths, mature API surface.
- **solid**: real implementation with adequate tests; fewer signals of "battle-tested" but no obvious scaffolding markers.
- **scaffolding**: interface or skeleton present; minimal tests; may be a hook for enterprise plugins or work in progress.
- **planned**: documented in OSS but not yet implemented as code (typically because it lives in `graphdb-enterprise`).
- **disabled**: code present but explicitly turned off (`.disabled` files, gated by env var, etc.).

---

## OSS — `pkg/` (37 packages)

### Storage layer

| Package | Maturity | One-line |
|---|---|---|
| `storage` | mature | Core in-memory graph engine. Partitioned `nodeShards` + `edgeShards` after A4/A4-edges; shard-level locks; WAL + snapshot persistence; tenant-scoped surface. 53 test files / 91 source files — by far the largest package. |
| `lsm` | mature | LSM-tree storage primitive used by `storage` for disk-backed edges. Background flush + compaction workers. 6 test files. |
| `wal` | mature | Write-ahead log: standard, batched, and compressed variants. Concurrent-read-write tested. 5 test files. |
| `partition` | scaffolding | 1 source file + 1 test. Likely the partition primitive that A4/A4-edges adopted into `storage`; may be unused now or general-purpose. **[?]** |

### Query layer

| Package | Maturity | One-line |
|---|---|---|
| `query` | mature | Query executor. Tenant-scoped after A6c-query. 32 test files / 78 source files — second-largest. |
| `queryutil` | solid | Helpers for wiring search and vector capabilities into a `query.Executor` without import cycles. New leaf package. |
| `algorithms` | mature | Graph algorithms (shortest path, traversals, etc.) exposed via `/algorithms` API. Tenant-scoped via `graphView`. 14 test files. |
| `parallel` | solid | Parallel-aggregation primitives. Audit performance HIGH-3 noted partition issues; status of follow-up unclear. **[?]** |
| `pools` | scaffolding | Object pools (likely for hot-path allocations). 1 test. **[?]** |

### Search & retrieval

| Package | Maturity | One-line |
|---|---|---|
| `search` | mature | Hybrid search infrastructure. Powers `/hybrid-search` and `pkg/retrieval`. 5 test files. |
| `retrieval` | solid | LangChain-style Retriever interface backed by hybrid search + graph expansion. F2 deliverable; powers `POST /v1/retrieve`. 2 test files (recent ship). |
| `vector` | solid | HNSW vector index. Powers `/vector-search` and `/v1/embeddings`. 2 test files but heavy coverage from `storage`'s tests. |

### API surface

| Package | Maturity | One-line |
|---|---|---|
| `api` | mature | REST API surface. 28 test files / 58 source files. Includes `handlers_nodes`, `handlers_edges`, `handlers_query`, `handlers_embeddings`, `handlers_retrieve`, `handlers_hybrid_search`, `handlers_algorithms`, `handlers_apikeys`, `handlers_security`, `handlers_monitoring`, `handlers_search_admin`. |
| `graphql` | mature | GraphQL API surface with schema introspection. Tenant-scoped after A9. 21 test files / 51 source files. |
| `server` | scaffolding | 2 source files. Probably a thin server bootstrap. **[?]** |

### Tenancy & identity

| Package | Maturity | One-line |
|---|---|---|
| `tenant` | solid | Tenant context propagation + middleware. Used everywhere post-A5. |
| `tenantid` | solid | Leaf package: canonical `TenantID` type. Boundary-conversion helpers. Audit A1. |
| `auth` | solid | JWT + API-key authentication. JWT_SECRET fail-closed since A2. 6 test files. |

### Security & compliance

| Package | Maturity | One-line |
|---|---|---|
| `security` | solid | Security scanning + checks. Backs `handlers_security`. 2 test files. |
| `tls` | solid | TLS configuration helpers; see `docs/TLS_CONFIGURATION.md`. |
| `encryption` | solid | Data-at-rest encryption with typed `EncryptDecrypter` + `KeyProvider` interfaces. KMS integration unverified. **[?]** See `docs/ENCRYPTION_ARCHITECTURE.md`. |
| `masking` | solid | Per-tenant property masking; used by per-tenant property filter (Syntopica integration). |
| `validation` | solid | Input validation primitives. |
| `audit` | solid | Append-only audit log. 4 test files. Backs F3's eventual `/v1/compliance/audit-log` (when that handler ships). |
| `compliance` | solid | GDPR + SOC2 control framework with status tracking, export. **The control framework exists; the HTTP API surface tying it to `pkg/api/` is the F3 work that's named as "not started."** |

### Operations

| Package | Maturity | One-line |
|---|---|---|
| `admin` | solid | Admin operations (backs `cmd/graphdb-admin`). 2 test files. |
| `health` | solid | Health checks and `/health` endpoints. 1 test file. **[?]** |
| `metrics` | solid | Metrics registry; per-operation timing. Used pervasively. Plain Prometheus metrics implementation appears to be a planned enterprise plugin (`prometheus-metrics` already exists in the enterprise repo). |
| `logging` | scaffolding | Structured logging helper. 1 test file, has README. Likely a thin wrapper. **[?]** |

### Distribution & lifecycle

| Package | Maturity | One-line |
|---|---|---|
| `replication` | retired | Deleted 2026-05-12 (A8.1, PR #133). The package contained two forked TCP+NNG role implementations + an NNG-vocabulary socket abstraction; not a foundation for a future cmd/server-native rebuild. The audit-load-bearing primitive (`WriteOperation` + `ApplyWriteOperation` fail-closed tenant gate) was lifted to `pkg/wal/apply/` before deletion. |
| `cluster` | solid | **Distributed cluster code**: leader election, membership, discovery, voting. 14 source files (2,835 LOC), 4 test files. Notably contradicts the `NEXT_STEPS_2026-05-10.md` "single-node assumption baked in" claim — what the planning doc means is presumably that *write throughput* is single-node (no sharding) even with a cluster, but the cluster substrate exists. **[?] Verification needed on what cluster code is actually wired into the runtime.** |
| `pubsub` | scaffolding | 2 source files, 1 test. Likely an internal event bus. Possibly the foundation for the planned CDC enterprise plugin. **[?]** |
| `constraints` | solid | Schema constraints (uniqueness, etc.). 4 test files. |

### Enterprise extension points

| Package | Maturity | One-line |
|---|---|---|
| `licensing` | mature | License validation, hardware fingerprinting, edition gating. 9 test files, has README. Backs `cmd/license-server`. |
| `editions` | solid | Edition (Community / Enterprise) feature gates. 2 test files. |
| `plugins` | scaffolding | Plugin loader for `.so` enterprise plugins. Defines `EnterprisePlugin` + `StoragePlugin` + `APIPlugin` + `BackupPlugin` + `MonitoringPlugin` interfaces. Loads license-gated `.so` files at runtime. 1 test file. The loader is OSS; the plugins are not. |

### Visualization & UX

| Package | Maturity | One-line |
|---|---|---|
| `visualization` | solid | Graph layout algorithms: force-directed, circular, hierarchical. 7 source files. Powers visualisations in `cmd/tui` (presumably). 1 test file. **[?]** |

### Other

| Package | Maturity | One-line |
|---|---|---|
| `integration` | solid | Integration test fixtures and shared test infrastructure. Includes `race_conditions_test.go`, `security_e2e_test.go`. |

---

## OSS — `cmd/` (25 binaries after A8.1)

### Server binaries

| Binary | Purpose |
|---|---|
| `graphdb` | Primary server entrypoint. |
| `server` | Alternative server bootstrap. **[?] Relationship to `graphdb` unclear from filenames alone.** |

> **Retired 2026-05-12 (A8.1, PRs #129/#130/#133):** `graphdb-primary`, `graphdb-replica`, `graphdb-nng-primary`, `graphdb-nng-replica`. The standalone replication binaries pre-dated multi-tenancy and routed writes to the default tenant; the deployment surface is now `cmd/server` only. See `docs/A8_1_SPIKE_2026-05-12.md`.

### Operations & admin

| Binary | Purpose |
|---|---|
| `graphdb-admin` | Admin CLI. 4 source files — includes `update` subcommand for in-place binary updates (added 2026-05-13 with `pkg/updater`). |
| `cli` | General-purpose CLI client. **[?] Relationship to `graphdb-admin` unclear.** |
| `license-server` | License issuance + validation server (separate process from main graphdb). |

### Bulk import

| Binary | Purpose |
|---|---|
| `import-icij` | Bulk import for the ICIJ Offshore Leaks corpus (named benchmark). |
| `import-dimacs` | Bulk import for DIMACS graph format (academic benchmarks). |

### Demo & developer experience

| Binary | Purpose |
|---|---|
| `tui` | Terminal UI for browsing a graphdb instance. |
| `tui-demo` | TUI demo / showcase. |
| `api-demo` | REST API demo / showcase. |
| `integration-test` | Standalone integration-test runner. |
| `test-lsm` | LSM exerciser (likely a debugging tool). **[?]** |

### Benchmarks

13 separate benchmark binaries: `benchmark`, `benchmark-algorithms`, `benchmark-batched`, `benchmark-compression`, `benchmark-graph-storage`, `benchmark-index`, `benchmark-lsm`, `benchmark-mmap`, `benchmark-parallel`, `benchmark-query`, `benchmark-road-network`, `benchmark-wal-compression`. Each is one source file. The proliferation suggests these accreted over time; some consolidation might be due in a separate cleanup task, not on the critical path.

---

## OSS — first-party client SDKs

| Surface | Language | Maturity | Notes |
|---|---|---|---|
| `workers/graphdb-client/` | TypeScript | solid | Cloudflare Workers / Edge client. Has tests, examples (`concept-graph-worker.ts`, `trust-score-worker.ts`), an `IMPLEMENTATION-COMPLETE.md`. Distributed via npm? **[?]** |
| (none) | Python | missing | No first-party Python SDK. Customers go via REST. |
| (none) | Java | missing | No first-party Java SDK. |
| (none) | Rust | missing | No first-party Rust SDK. |
| Direct `pkg/` import | Go | mature | Embedding graphdb as a Go library is supported via direct `pkg/storage` etc. import. Not packaged separately. |

---

## Enterprise — `graphdb-enterprise` repo

### Built and shipping

| Plugin | Purpose | Source path |
|---|---|---|
| `prometheus-metrics` | Advanced Prometheus metrics, health endpoints, query performance tracking. | `prometheus-metrics/` |
| `r2-backup` | Cloudflare R2 backup with incremental backups, scheduling, and restore. | `r2-backup/` |

### Named in `docs/ENTERPRISE_PLUGINS.md` but not yet implemented

| Plugin | Promised in docs |
|---|---|
| `cloudflare-vectorize` | Cloudflare Vectorize backend for vector search (addresses LSA scale ceiling for Cloudflare-shop customers). |
| `cdc` (Change Data Capture) | Stream graph mutations to external systems (Kafka, webhooks, etc.). |
| `multi-region-replication` | Geo-distributed primary/replica. |
| `saml-oidc-auth` | SAML / OIDC enterprise SSO integration. |

The 4 unbuilt plugins are real product gaps for enterprise prospects. The fact that they're documented but not implemented should be reconciled with the enterprise sales/marketing surface — either build them or scrub the docs.

---

## Genuinely missing (cross-repo, after acknowledging the enterprise tier)

These are gaps that exist in *both* repos:

### API surface

- **gRPC API**. No `.proto` files in either repo. REST + GraphQL only.

### Client SDKs

- **Python, Java, Rust SDKs**. Only TypeScript (workers) and direct-Go.

### Infrastructure as code

- **Helm chart** for Kubernetes deployment.
- **Terraform provider** for graphdb resource management.
- **Kubernetes operator** (CRD-based lifecycle management).
- **Kustomize overlays** or other deployment-recipe primitives.

### Testing & quality

- **Chaos engineering test suite**. Disk-fault injection, network-partition tests, kill-9 recovery validation. The disabled fuzz tests (`pkg/api/fuzz_test.go.disabled`, `pkg/query/fuzz_test.go.disabled`) suggest fuzzing was started but stalled.

### Data-platform integrations

- **Kafka source/sink** beyond the named-but-unbuilt CDC plugin.
- **ETL connectors**: Airbyte, Fivetran, Dagster, dbt.
- **Lakehouse export**: Iceberg, Delta, Parquet bulk export.
- **BI tool drivers**: Tableau, Looker, Metabase, Power BI.

### Operations beyond plugins

- **OpenTelemetry tracing** (the `prometheus-metrics` enterprise plugin covers metrics; tracing is a separate concern).
- **SLO / SLI documentation** with target latency / error budget per endpoint.
- **Production deployment checklist** beyond `PRODUCTION_QUICKSTART.md`.
- **Capacity planning guide** beyond `CAPACITY_TESTING.md`.
- **Migration guides from competitors** (Neo4j, ArangoDB, JanusGraph) — both for reducing customer switching friction and for competitive positioning.

### Commercial / packaging

- **No stated pricing**. (Already captured in `NEXT_STEPS_2026-05-10.md` productization-gaps section.)
- **No support model** (response-time SLAs, escalation paths).
- **No public roadmap** for the enterprise plugin tier (the 4 named-but-unbuilt plugins above).

---

## Reconciliation with `NEXT_STEPS_2026-05-10.md`

The "Known limitations + productization gaps" section added in PR #71 was written without checking the enterprise repo. It needs follow-up correction:

| Gap as written | Actual state |
|---|---|
| "No production-grade observability narrative beyond `pkg/metrics`" | The `prometheus-metrics` enterprise plugin already exists. **OpenTelemetry tracing** is the genuine OSS-side gap; advanced metrics are a paid feature. |
| "Single-node assumption baked in" | `pkg/cluster` (2,835 LOC) provides distributed-cluster substrate. The honest claim is "**no sharded write path**" — clustering exists, but write throughput is bounded by a single primary. |
| (Compliance ambiguity) | F3 framing of "not started" overstates the gap: `pkg/compliance/` has the framework + GDPR/SOC2 controls. F3 is the **HTTP-API surface** tying that framework to customer-callable endpoints — narrower than "build compliance from scratch." |

A small follow-up doc PR could add a one-paragraph correction note pointing readers from the gaps section to this capabilities audit. Worth doing before the next planning checkpoint reads the gaps section as gospel.

---

## Suggested triage for the next planning checkpoint

In dependency order, since this audit changes which gaps are urgent:

1. **Resolve the 4 named-but-unbuilt enterprise plugins** (`cloudflare-vectorize`, `cdc`, `multi-region-replication`, `saml-oidc-auth`). Either commit to building them on a stated timeline, or scrub the docs that promise them. The current state (documented but not built) is the worst of both worlds — enterprise prospects who read the docs will ask, and the answer "those don't exist yet" damages trust.
2. **Add OpenTelemetry tracing to the OSS core**. Tracing is now a baseline expectation, not a paid feature — and the metrics-as-paid-plugin pattern means OSS users have *no* observability story (their `pkg/metrics` data is internal-only by default).
3. **Ship a Python SDK** as the first non-TypeScript first-party client. Python is the LangChain/LlamaIndex ecosystem; the F1 / F2 work positions graphdb in that space and currently those users have to write their own HTTP wrappers.
4. **Helm chart** as the first IaC primitive. Kubernetes is the deployment target for ~all enterprise prospects; not having a Helm chart means every customer writes their own deployment recipe.
5. **Decide the commercial-offering question** (founder-led, per PR #71's gaps section). The capabilities audit shows the OSS/enterprise split is real and shipping, but the *go-to-market* story is undocumented.

This list is the input to the next `NEXT_STEPS_<DATE>.md`, not a commitment. The first item is the most urgent because it's a credibility issue, not a scope issue.
