# F4 Design Spike — Tenant-Isolated Vector Operations

**Status:** spike output, not implementation. Date: 2026-05-13.
**Predecessors:** S1 narrowed landing (PR #145); audit of Gemini bulk-stash
(`AUDIT_gemini_track_claims_2026-05-13.md`); planning checkpoint
`NEXT_STEPS_2026-05-13.md` Track R1.
**Goal:** close the design questions named in R1, select a per-tenant HNSW
architecture, and produce signatures that re-attach to the S1 `Storage`
interface in a subsequent R3 PR.

---

## 1. Problem Statement

### 1.1 The flawed archive code

The Gemini bulk-stash (captured as `origin/archive/gemini-bulk-2026-05-13`,
main working-tree commit `96ed5b0`) added 6 `*VectorIndexForTenant` wrapper
methods to `pkg/storage/vector_operations.go`. Every wrapper contained the
same silent tenant-fallback:

```go
// pkg/storage/vector_operations.go — stash commit 96ed5b0 (archive)

func (gs *GraphStorage) CreateVectorIndexForTenant(
    tenantID string, propertyName string, dimensions int,
    m int, efConstruction int, metric vector.DistanceMetric,
) error {
    if tenantID == "" {         // line 20 — the offender
        tenantID = "default"
    }
    return gs.vectorIndex.CreateIndex(tenantID, propertyName, dimensions, m, efConstruction, metric)
}

func (gs *GraphStorage) VectorSearchForTenant(
    tenantID string, propertyName string, query []float32, k int, ef int,
) ([]vector.SearchResult, error) {
    if tenantID == "" {         // line 57 — the offender
        tenantID = "default"
    }
    return gs.vectorIndex.Search(tenantID, propertyName, query, k, ef)
}

func (gs *GraphStorage) DropVectorIndexForTenant(tenantID string, propertyName string) error {
    if tenantID == "" {         // line 70 — the offender
        tenantID = "default"
    }
    return gs.vectorIndex.DropIndex(tenantID, propertyName)
}

func (gs *GraphStorage) HasVectorIndexForTenant(tenantID string, propertyName string) bool {
    if tenantID == "" {         // line 82 — the offender
        tenantID = "default"
    }
    return gs.vectorIndex.HasIndex(tenantID, propertyName)
}

func (gs *GraphStorage) ListVectorIndexesForTenant(tenantID string) []string {
    if tenantID == "" {         // line 95 — the offender
        tenantID = "default"
    }
    return gs.vectorIndex.ListIndexes(tenantID)
}

func (gs *GraphStorage) GetVectorIndexMetricForTenant(tenantID string, propertyName string) (vector.DistanceMetric, error) {
    if tenantID == "" {         // line 108 — the offender
        tenantID = "default"
    }
    return gs.vectorIndex.GetIndexMetric(tenantID, propertyName)
}
```

The same fallback appears verbatim in `UpdateNodeVectorIndexes` and
`RemoveNodeFromVectorIndexes` in the same file (stash lines 128 and 141).

### 1.2 Why this is a correctness flaw, not just style

The repo's tenant-strict rule (`CLAUDE.md` § "Tenant scoping") states:
cross-tenant lookups return `ErrNodeNotFound` (not a distinct error) so the
response shape cannot reveal whether a resource exists in another tenant.
A distinct error or a silent route to "default" both break this:

- **Existence-leak via search results**: a caller with no "default" tenant
  credentials can call `VectorSearchForTenant("")` and receive results from
  the default tenant's index — disclosing vectors it should not see.
- **Silent index comingling**: calling `CreateVectorIndexForTenant("")` would
  create (or collide with) the "default" tenant's index, affecting unrelated
  tenants sharing that namespace.

The audit (`AUDIT_gemini_track_claims_2026-05-13.md`, F4 row) scored this
🔴 CORRECTNESS-FLAWED for these reasons.

### 1.3 Why this is different from `GetNodeForTenant`'s empty-tenant handling

`GetNodeForTenant` (`pkg/storage/node_operations.go:226–228`) also defaults
empty tenantID to "default":

```go
// Empty tenantID defaults to tenantid.Default ("default"). This matches
// CreateNode's default-tenant fallback so single-tenant deployments and
// tests that don't supply a tenant continue to work transparently.
```

The vector `*ForTenant` methods must NOT inherit this behavior, for three
reasons:

1. **Admin-tier frequency.** `CreateVectorIndex` is a rare admin-level call,
   not an every-request read. Single-tenant deployments have a single code
   path that can always supply the tenant; there is no compat case to
   preserve.
2. **Scope of exposure.** An empty-tenant `GetNode` returns at most one node
   to an authorized caller. An empty-tenant `VectorSearch` returns the top-k
   results from the default tenant's entire embedding corpus — a much wider
   leak surface.
3. **No existing callers.** The 6 `*ForTenant` methods do not exist on
   `main` today (they were deliberately omitted from PR #145). There are no
   callers to break by making empty tenantID an error.

The redesign therefore rejects empty tenantID with a validation error, not
a silent route.

---

## 2. Design Constraints

1. **Tenant-strict error shape.** A vector search or index operation on an
   empty or unrecognized tenantID must return an error — `ErrNodeNotFound`
   for lookup-style operations (search, metric, has), `ErrInvalidID` or a
   descriptive error for admin-style operations (create, drop, list).
   Under no circumstances may the caller infer whether a tenantID exists in
   the system from the error type.
2. **No existence-leak via result shape.** Results returned from
   `VectorSearchForTenant` for tenantID A must contain only vectors tagged
   to tenant A. A vector tagged to tenant B must be invisible to A — the
   same guarantee `GetNodeForTenant` provides for node reads.
3. **S1 interface re-attachable.** The 6 methods must match a signature that
   can be added to `StorageReader` / `StorageWriter` in the R3 PR without
   modifying the existing 51 methods or breaking `cmd/import-dimacs` and
   other current S1 consumers.
4. **HNSW scale.** The current `pkg/vector.HNSWIndex` is purely in-memory.
   The design must acknowledge memory footprint honestly and state the
   assumption under which the chosen option is acceptable. Persistence
   (KV-backed HNSW, as prototyped in the archive's `hnsw.go:51` with
   `kvNodeStore`) is out of scope for F4 but must not be foreclosed by the
   data-structure choice.
5. **Locking discipline.** The existing `pkg/storage/vector_index.go` holds
   `VectorIndex.mu sync.RWMutex` internally. The redesign must preserve the
   invariant that writers hold `gs.mu.Lock` (for global-index consistency)
   and the per-VectorIndex mutex is managed internally. No naked lock
   exposure to callers.

---

## 3. Option A — Per-Tenant HNSW Index

### Structure

Replace `VectorIndex.indexes map[string]*HNSWIndex` (keyed by property name)
with a two-level structure:

```
// Conceptual (not implementation code)
type VectorIndex struct {
    mu      sync.RWMutex
    tenants map[string]*tenantVectorIndexes   // tenantID → per-property HNSW map
}

type tenantVectorIndexes struct {
    mu      sync.RWMutex
    indexes map[string]*vector.HNSWIndex   // propertyName → HNSW
}
```

Each tenant owns a fully independent set of `HNSWIndex` instances. Tenant A
and tenant B cannot share an entry point, layer structure, or candidate set.
Isolation is structural — there is no filtering step.

### Footprint estimate

Per-vector memory cost (in-memory HNSW, current `pkg/vector/hnsw.go`):

```
bytes_per_vector ≈ d × 4          (float32 vector, d dimensions)
                 + M × 8 × 1.33   (connections across avg 1.33 layers, M links per layer)
```

For common embedding model dimensions:

| d (dims) | M=16, avg 1.33 layers | bytes/vector |
|---|---|---|
| 384 (MiniLM-L6) | 16×8×1.33 ≈ 170 bytes | ~1.7 KB |
| 768 (BERT-base) | 170 bytes | ~3.2 KB |
| 1536 (OpenAI text-3-small) | 170 bytes | ~6.3 KB |

**Illustrative scenario** (not a measurement): V=10,000 vectors per tenant,
d=768, M=16, N=100 tenants.

- Per-tenant: ~32 MB
- Total across 100 tenants: ~3.2 GB

This is the dominant memory cost of Option A. It is acceptable if:
(a) tenant count is low (SaaS with dozens of orgs, not thousands of users);
(b) not all tenants hold vectors (many tenants may have zero indexed
properties, consuming only a map entry).

**If tenant count is large (thousands), Option A's memory footprint becomes
prohibitive and Option B should be preferred.** The spike cannot determine
tenant-count distribution without user input; see §5.

### Build/rebuild semantics

`CreateVectorIndexForTenant` allocates a new `HNSWIndex` under the tenant
map. Index rebuild (after bulk insert) uses the existing
`HNSWIndex.Insert`/`HNSWIndex.Delete` interface — no global rebuild is
needed because tenant indexes are isolated. A tenant's index can be dropped
and recreated without affecting any other tenant.

### Latency model

HNSW search time is `O(log(N_tenant) × ef)` where `N_tenant` is the number
of vectors in the tenant's index. With per-tenant indexes, `N_tenant` is the
tenant's own count — not the global count. This is strictly faster than a
shared index of size `N_global = N_tenant × N` for the same `ef` and `k`
target.

### Complexity

- Data-structure change: `VectorIndex` becomes `map[tenantID]map[property]*HNSWIndex`.
- All existing `VectorIndex` methods gain a `tenantID string` parameter.
- `UpdateNodeVectorIndexes` routes to the tenant map using `node.TenantID`.
- Snapshot format: if/when persistence lands, per-tenant HNSW maps to a
  per-tenant KV prefix. This is cleaner than embedding tenant tags inside
  the HNSW node store.
- The compile-time `var _ Storage = (*GraphStorage)(nil)` check prevents
  interface drift between `VectorIndex` method changes and the S1 surface.

---

## 4. Option B — Shared HNSW + Tenant-Keyed Filter at Search Time

### Structure

Keep the current single `VectorIndex.indexes map[string]*HNSWIndex` structure.
Extend each `hnswNode` (currently `{ id, vector, level, friends }`) with a
`tenantID string` field. Search applies a tenant filter during candidate
evaluation — this is the **filtered HNSW** pattern used by Qdrant, Weaviate,
and Milvus.

Two sub-variants exist:

**B1 — in-search filter (correct):** the tenant predicate is evaluated inside
the HNSW greedy search, pruning candidates that belong to other tenants before
they enter the candidate heap. The `ef` parameter is inflated (e.g., `ef =
k × (N_global / N_tenant_expected)`) to compensate for filtered-out candidates
and maintain recall. This is how Qdrant implements filtered ANN.

**B2 — post-filter (incorrect for this threat model):** HNSW returns the top
`ef` candidates unfiltered; the caller then drops non-tenant results. This is
simple but leaks in two ways: (a) timing — a query that touches tenant B's
vectors takes measurable microseconds longer even if B's results are dropped
before the response; (b) recall collapse — if tenant A's vectors are sparse
in the global index, `ef` may be exhausted before finding `k` tenant-A
results, silently under-returning.

This spike requires **B1 if Option B is chosen**. B2 is excluded by the
no-existence-leak constraint.

### Footprint estimate

Same per-vector memory as Option A (the vector data is the same), plus a
`tenantID string` per node entry in the HNSW store. For a 16-byte UUID
tenantID, overhead is ~16 bytes per vector — negligible.

**Illustrative scenario**: same V=10,000, d=768, M=16, N=100.

- Total across all tenants: ~320 MB (10× smaller than Option A's 3.2 GB).

The footprint advantage of Option B grows linearly with tenant count.

### Latency model

In-search filtering (B1) inflates the effective work: the HNSW traversal
visits `O(ef_effective / selectivity)` candidates where selectivity is
`N_tenant / N_global`. For a tenant holding 1% of vectors (`selectivity=0.01`),
reaching `k=10` results requires exploring ~1000 candidates instead of 10.
At large `ef` values, the graph traversal's log factor is O(log N_global),
not O(log N_tenant), which is slower than Option A for sparse tenants.

Empirical work on filtered HNSW (Qdrant 2022, "Filterable HNSW") shows
recall degradation when selectivity < 0.05. The mitigation is a brute-force
fallback for low-selectivity queries, which adds implementation complexity.

### Complexity

- Requires modifying `pkg/vector/hnsw_types.go:hnswNode` to carry `tenantID`.
- Requires rewriting the HNSW search loop in `pkg/vector/hnsw_search.go` to
  accept a predicate function.
- Requires an ef-inflation heuristic or a selectivity estimator (requires a
  per-tenant count estimate at query time).
- Snapshot format: if persistence arrives, tenant tags must be stored in the
  HNSW node store and round-trip through serialization. This is a non-trivial
  format change.
- No changes to the two-level map structure (simplifies the `VectorIndex`
  type), but the complexity moves into the HNSW core — a package that is
  currently clean and simple.

---

## 5. Recommendation

**Recommendation: Option A — per-tenant HNSW index.**

### Reasoning

1. **Isolation by construction, not by predicate.** Option A eliminates the
   existence-leak threat entirely without relying on a filtering predicate to
   hold under all code paths. Option B requires the in-search filter to be
   correct, to never degrade to B2, and to produce no timing side channels.
   That's a set of ongoing correctness obligations; Option A has none of them.

2. **HNSW core stays simple.** `pkg/vector/hnsw.go`, `hnsw_search.go`, and
   `hnsw_types.go` are currently clean (~400 lines total). Option B requires
   threading a predicate through the search loop and adding an ef-inflation
   heuristic — substantially increasing the complexity of the most
   algorithmic code in the repo.

3. **Search latency is better for tenant queries.** Each tenant's index
   contains only its own vectors. `N_tenant ≤ N_global`, so HNSW search
   time is `O(log(N_tenant) × ef)` — strictly no worse than Option B's
   unfiltered case, and faster when tenants hold a small fraction of total
   vectors.

4. **The footprint concern is real but manageable at expected scale.** This
   is an in-memory graph database currently described in README "Scalability
   & Limitations" as single-node. The expected deployment is SaaS with
   dozens to low hundreds of tenants, not thousands. At that scale (100
   tenants × 10k vectors × 768 dims ≈ 3.2 GB), Option A is within typical
   cloud instance memory budgets. If the user intends to support thousands of
   tenants with dense vector usage, this recommendation reverses — see
   **Missing constraint** below.

5. **Future persistence is cleaner.** When KV-backed HNSW arrives (the
   archive's `kvNodeStore` prototype shows the shape), per-tenant indexes map
   directly to per-tenant KV prefixes. Option B would need to store tenant
   tags inside the HNSW node format and version that.

**Missing constraint that would reverse this recommendation:** if the expected
tenant count is in the thousands and most tenants are expected to hold
non-trivial vector counts, Option A's memory overhead becomes prohibitive.
The user should confirm the expected tenant-count ceiling before R1
implementation begins. If thousands of tenants is the target, return to this
spike and switch to Option B (filtered HNSW, B1 variant), which will require
a filtered-search implementation in `pkg/vector/hnsw_search.go`.

---

## 6. Final Method Signatures

These are the 6 `*VectorIndexForTenant` methods the implementation must
provide. They re-attach to `StorageReader` / `StorageWriter` in R3.

### Rules applied to all methods

- `tenantID == ""` returns a validation error (not a silent route to "default").
  This diverges from `GetNodeForTenant`'s empty→default convention, justified
  in §1.3.
- Lookup-style operations (search, has-index, get-metric) return
  `ErrNodeNotFound` when the tenant has no index for the requested property —
  the same unified error used for cross-tenant node lookups, avoiding
  existence-leak via error type.
- Admin-style operations (create, drop, list) return a descriptive error
  wrapping `ErrInvalidID` for empty tenantID, and a named error for
  "no such index" on drop.

```go
// StorageReader additions (read-only surface)

// VectorSearchForTenant performs k-NN search on a vector-indexed property,
// scoped to tenantID. Returns ErrNodeNotFound if the tenant has no index
// for propertyName — the unified error prevents existence-leak via error shape.
// Returns a validation error if tenantID is empty (not silently routed to
// "default"; see docs/internals/design/F4_VECTOR_TENANT_REDESIGN.md §1.3).
VectorSearchForTenant(
    tenantID string,
    propertyName string,
    query []float32,
    k int,
    ef int,
) ([]vector.SearchResult, error)

// HasVectorIndexForTenant reports whether tenantID has a vector index for
// propertyName. Returns false (not an error) for both "no such tenant" and
// "no index on this property" — callers use this for conditional creation,
// not for tenant existence probing.
HasVectorIndexForTenant(tenantID string, propertyName string) bool

// GetVectorIndexMetricForTenant returns the distance metric for tenantID's
// vector index on propertyName. Returns ErrNodeNotFound if no index exists.
GetVectorIndexMetricForTenant(
    tenantID string,
    propertyName string,
) (vector.DistanceMetric, error)

// ListVectorIndexesForTenant returns the property names for which tenantID
// has active vector indexes. Returns an empty slice (not an error) if
// tenantID has no indexes.
ListVectorIndexesForTenant(tenantID string) []string

// StorageWriter additions (mutative surface)

// CreateVectorIndexForTenant creates a vector index on propertyName for
// tenantID. Returns an error if tenantID is empty, if dimensions/m/
// efConstruction are invalid, or if an index already exists for that
// (tenantID, propertyName) pair.
CreateVectorIndexForTenant(
    tenantID string,
    propertyName string,
    dimensions int,
    m int,
    efConstruction int,
    metric vector.DistanceMetric,
) error

// DropVectorIndexForTenant removes tenantID's vector index for propertyName.
// Returns ErrNodeNotFound if the index does not exist — the same unified
// error prevents callers from distinguishing "wrong tenant" from "no index."
DropVectorIndexForTenant(tenantID string, propertyName string) error
```

### How these re-attach to S1

`StorageReader` gains `VectorSearchForTenant`, `HasVectorIndexForTenant`,
`GetVectorIndexMetricForTenant`, and `ListVectorIndexesForTenant`.

`StorageWriter` gains `CreateVectorIndexForTenant` and
`DropVectorIndexForTenant`.

The existing tenant-blind methods (`VectorSearch`, `HasVectorIndex`,
`CreateVectorIndex`, `DropVectorIndex`, `ListVectorIndexes`,
`GetVectorIndexMetric`) remain on the interface for backward compatibility and
single-tenant use — they delegate to the default tenant's index under the
hood, using the same `effectiveTenantID` helper that node operations use.

The R3 PR adds these 6 signatures to `interface.go` and updates the
compile-time assertion `var _ Storage = (*GraphStorage)(nil)`.

---

## 7. Test Plan

Tests live in `pkg/storage/vector_tenant_isolation_test.go` (new file).
Shape mirrors `TestStorageVectorSearchIntegration`
(`pkg/storage/vector_integration_test.go:165`) — set up storage, insert
nodes, index, search, assert.

### Required test cases

**T1 — Cross-tenant search isolation (regression pin)**

```
Create index on tenantA for property "embedding"
Create index on tenantB for property "embedding"
Insert node N_A in tenantA with vector V_A
Insert node N_B in tenantB with vector V_B (near V_A by cosine)
Call VectorSearchForTenant(tenantA, "embedding", V_A, k=10, ef=50)
Assert: results contain N_A.ID
Assert: results do NOT contain N_B.ID
Assert: VectorSearchForTenant(tenantB, ..., V_A, ...) does NOT return N_A.ID
```

**T2 — Empty tenantID is rejected, not routed**

```
For each of: CreateVectorIndexForTenant, VectorSearchForTenant,
             DropVectorIndexForTenant, GetVectorIndexMetricForTenant:
    Call with tenantID = ""
    Assert: returns non-nil error
    Assert: error is NOT ErrNodeNotFound (it is a validation error)
```

The `HasVectorIndexForTenant("", ...)` case is excluded — returning `false`
for an empty tenant is acceptable because `false` reveals nothing about
other tenants.

**T3 — No-index lookup returns `ErrNodeNotFound`-equivalent**

```
Call VectorSearchForTenant("existingTenant", "no-such-property", ...)
Assert: errors.Is(err, ErrNodeNotFound)
```

This pins the unified-error contract — callers cannot distinguish
"wrong tenant" from "no index" by error shape.

**T4 — Drop on non-existent index**

```
Call DropVectorIndexForTenant("existingTenant", "no-such-property")
Assert: errors.Is(err, ErrNodeNotFound)
```

Consistent with T3; prevents probing index existence via drop.

**T5 — Index lifecycle per tenant**

```
Create index on tenantA
Verify HasVectorIndexForTenant(tenantA, prop) == true
Verify HasVectorIndexForTenant(tenantB, prop) == false (tenantB has no index)
Drop index on tenantA
Verify HasVectorIndexForTenant(tenantA, prop) == false
```

**T6 — Race detector clean under concurrent per-tenant ops**

Table-driven test: 4 goroutines, each operating on a distinct tenantID
(create index, insert vectors, search, drop). Run with `-race -count=3`.
Assert: no data race; results from tenant X never appear in tenant Y's
search results.

---

## 8. PR Breakdown

### R1-1: `pkg/storage/vector_index.go` — per-tenant structure

**Goal:** replace the flat `map[string]*HNSWIndex` with a two-level
`map[tenantID]map[propertyName]*HNSWIndex`; add tenant-aware internal
methods.

**Acceptance:** `NewVectorIndex()` produces the new structure; all existing
single-tenant callers continue to work via `effectiveTenantID("")` routing
to "default"; `go build ./...` passes; `golangci-lint run ./...` passes.

No new public methods in this PR — this is an internal restructure only.

### R1-2: `pkg/storage/vector_operations.go` — 6 `*ForTenant` methods

**Goal:** add `CreateVectorIndexForTenant`, `VectorSearchForTenant`,
`DropVectorIndexForTenant`, `HasVectorIndexForTenant`,
`ListVectorIndexesForTenant`, `GetVectorIndexMetricForTenant` with the
signatures from §6. Update `UpdateNodeVectorIndexes` and
`RemoveNodeFromVectorIndexes` to route through the tenant-aware internal
methods.

**Acceptance:** methods compile; empty `tenantID` returns validation error;
`var _ Storage = (*GraphStorage)(nil)` does not yet check these (they are
not on the interface yet — that is R3).

### R1-3: `pkg/storage/vector_tenant_isolation_test.go` — isolation tests

**Goal:** implement T1–T6 from §7.

**Acceptance:** all 6 test cases pass under `go test -race -count=3
./pkg/storage/ -run TestVectorTenantIsolation`; no data races reported.

### R1-4: R3 partial — add 6 signatures to `interface.go`

**Goal:** add the 6 methods to `StorageReader` and `StorageWriter`;
update the compile-time assertion.

**Acceptance:** `var _ Storage = (*GraphStorage)(nil)` compiles; the B+Tree
backend from C2 either satisfies the expanded interface or gets explicit stub
implementations with `// F4: tenant-isolated vector ops not yet implemented
on BTreeStorage` — that is acceptable as long as the stub returns an error
rather than silently failing.

**Note:** this PR is labeled "R1-4" here because it is the final step of R1.
It is logically part of R3's S1-surface-closure in the planning doc. The
planning doc R3 remains a single PR that also folds in `AddObserver` (from
R2) and resolves the `Snapshot(ctx)` drift. This PR can land after R1-3 and
before R2 if parallelism is desired, or wait for R2 and be combined into the
single R3 PR as planned.

---

## Appendix — data structure delta summary

| Component | Current state | After F4 (Option A) |
|---|---|---|
| `VectorIndex` struct | `map[string]*HNSWIndex` (by propertyName) | `map[string]*tenantVectorIndexes` (by tenantID) → inner `map[string]*HNSWIndex` (by propertyName) |
| `VectorIndex.mu` | one mutex for the outer map | one mutex for the outer map; each `tenantVectorIndexes` holds its own inner mutex |
| `gs.vectorIndex` field | `*VectorIndex` | unchanged type; behavior changes internally |
| `HNSWIndex` struct | unchanged | unchanged — isolation is at the map layer, not inside HNSW |
| Snapshot format | not yet persisted | not yet persisted; future KV persistence maps to per-tenant prefix naturally |
| S1 `StorageReader` | 5 tenant-blind vector methods | +4 `*ForTenant` methods |
| S1 `StorageWriter` | 2 tenant-blind vector maintenance methods | +2 `*ForTenant` admin methods |
