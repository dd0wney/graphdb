# Architecture Audit — graphdb (github.com/dd0wney/cluso-graphdb)

Audited: 2026-05-06. ~77K LOC, 42 packages.

---

## Findings

### HIGH-1 — No storage interface: every consumer takes `*storage.GraphStorage` directly

**Files:** `pkg/storage/storage_types.go` (concrete `GraphStorage` struct), all callers in `pkg/algorithms`, `pkg/query`, `pkg/graphql`, `pkg/parallel`, `pkg/search`, `pkg/replication`, `pkg/api`.

There are zero `type ... interface` declarations anywhere in `pkg/storage`. The 60+ exported methods live on the concrete struct, and every downstream package takes `*storage.GraphStorage` by pointer. The kernel layering is clean (storage only imports `wal`, `lsm`, `metrics`, `encryption`, `pools`, `vector` — all leaf-ward), but the absence of an interface means:
- No package can be tested without a real `GraphStorage` instance.
- There is no contract boundary — callers can observe all 60+ methods, making every one of them load-bearing.
- Retargeting storage (e.g., swapping in a distributed backend) requires touching every callsite.

**Direction:** Define a small set of role interfaces (`NodeReader`, `EdgeWriter`, `Transactor`, `Indexer`) in a `pkg/storage/iface` package or alongside the struct. Let `*GraphStorage` satisfy all of them. Consumers should accept the narrowest interface they need.

---

### HIGH-2 — `pkg/api.Server` is a 30-field god-struct with 18 internal package imports

**File:** `pkg/api/server_types.go`

`Server` holds direct references to `*storage.GraphStorage`, `*query.Executor`, `*search.TenantIndexes`, `*graphql.GraphQLHandler`, auth stores, audit loggers, metrics, health checker, TLS, CORS, rate limiters, encryption engine, OIDC, tenant store, and two internal WaitGroups. Handler files themselves are manageable (largest non-test: `handlers_tenant.go` at 543 LOC). The problem is not file count — it is that `Server` has accreted every subsystem as a flat field, making the struct the implicit wiring layer for the entire application.

**Direction:** Extract a service layer (`NodeService`, `SearchService`, `VectorService`) that owns the storage-interaction logic. Handlers call services; `Server` holds services, not storage. This also breaks the REST/GraphQL duplication (finding MEDIUM-1).

---

### MEDIUM-1 — REST and GraphQL plumb separately into `*GraphStorage`

**Files:** `pkg/graphql/` (25+ files all importing `pkg/storage`), `pkg/api/handlers_*.go`.

Mutations, node resolution, edge resolution, search, and pagination are each implemented twice — once in `pkg/graphql/*_resolvers.go` and once in `pkg/api/handlers_*.go`. No shared service or use-case layer exists. A schema change (e.g., adding a property filter) must be applied in two places. The boundary between REST and GraphQL is clean at the routing level but absent at the logic level.

**Direction:** The service layer recommended in HIGH-2 solves this. Both REST handlers and GraphQL resolvers call the same service methods.

---

### MEDIUM-2 — `pkg/editions.Current` is a process-global mutable singleton

**File:** `pkg/editions/edition.go`

`Current` is a package-level `var` set once at startup by `DetectEdition()`. It is not safe to vary per-tenant or per-test. Combined with `pkg/plugins` using Go's native `plugin` package (`.so` files loaded from disk), the editions/plugins story is real at the loader level but frozen at runtime. Feature gating calls `editions.IsEnabled(feature)` with no way to inject a different feature set in unit tests.

**Direction:** Replace the global with an `Edition` value threaded through context or dependency injection. For testing, pass a mock `FeatureGate` interface.

---

### MEDIUM-3 — Tenant concept is split across two packages with no shared type

**Files:** `pkg/storage/tenant_operations.go`, `pkg/tenant/store.go`, `pkg/tenant/context.go`.

`pkg/storage` maintains its own per-tenant index structures (`tenantNodesByLabel`, `tenantEdgesByType`, `tenantStats`) keyed by raw `string` tenant IDs and does not import `pkg/tenant`. `pkg/tenant` is an HTTP-layer concept (context propagation, TenantStore). There is no canonical `TenantID` type; the boundary is enforced only by convention. Adding a tenant-scoped operation means touching both packages independently.

**Direction:** Define a canonical `TenantID` type in a shared leaf package (e.g., `pkg/tenantid`). Both `pkg/storage` and `pkg/tenant` import it. This is a small change with large clarifying effect.

---

### LOW-1 — Replication transport is polymorphic (not duplication)

**File:** `pkg/replication/transport.go`

`Socket`, `ListenSocket`, `DialSocket`, and `SocketFactory` interfaces are defined. Both `nng_*` and `zmq_*` files implement these interfaces. This is intentional polymorphism, not copy-paste duplication. No action needed.

---

### LOW-2 — `pkg/cluster` is a real Raft-flavored implementation

**Files:** `pkg/cluster/election.go`, `membership.go`, `discovery.go`

Election timer loop, term tracking, quorum detection, and membership tracking are all present and operational. `pkg/replication/primary.go` imports and wires cluster membership. Not a stub; does not need grading-down.

---

## Load-Bearing Abstractions

**`storage.GraphStorage` (concrete struct, 60+ exported methods)**
Rating: Under-specified at the type-system level (no interface contract), over-specified at the method level (single struct claiming responsibility for node I/O, edge I/O, indexing, vector search, batch operations, statistics, tenant scoping, and transaction management). The struct works, but it is the single point of failure for testability and future extensibility across the entire codebase. Priority-one target for interface extraction.

**`storage.Value` (tagged-byte union, `types.go`)**
Rating: Appropriate for an embedded engine. The eleven type tags (`TypeString` through `TypeBoolArray`) with LittleEndian binary encoding are the effective on-disk and in-memory wire format. The encoding is implicit in the public API (callers who call `AsVector()` are coupled to the 4-byte-dims layout). This is a pragmatic locked-in tradeoff, not an error, but any schema migration touching array types will be painful.

---

## Shape Verdict

The kernel layering is correct: `pkg/storage` depends only on true leaf packages (`wal`, `lsm`, `metrics`, `encryption`) and no upward imports exist. The damage is at the consumer side. Because `*storage.GraphStorage` exposes no interface, every feature layer (query engine, algorithms, graphql, REST, replication) has grown a direct structural dependency on the full concrete storage object, and `pkg/api.Server` has become the de-facto application wiring layer by accumulating all of those dependencies as flat fields. The package layout names the right concepts but is fighting the codebase because there is no service layer to absorb duplication between REST and GraphQL, and no interface boundary to let any consumer be tested in isolation. The fix is a single architectural intervention: extract role interfaces from `pkg/storage` and introduce a thin service layer between transport (api, graphql) and storage — everything else follows from that.
