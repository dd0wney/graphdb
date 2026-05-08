# Killer-Features: Data-Model Analysis
**Date**: 2026-05-08  
**Scope**: graphdb property-graph data shape—three extensions that unlock use cases competitors miss, fit existing infra, and carry acceptable migration risk.

---

## Top 3 Killer Features

### 1. **Bitemporal Graph Snapshots** (Time-Travel Queries)
**Competitor**: Memgraph Cloud, Neo4j Enterprise (temporal syntax)  
**Data Shapes Unlocked**:
- Query the graph as it existed at any point in the past (compliance: "show me the topology on 2026-03-15")
- Detect when edges changed, who changed them, and why (audit trail with graph structure)
- Analyze graph evolution over fiscal periods (fraud rings that formed/dissolved)
- Temporal causality: did node A's deletion trigger property changes in downstream nodes?

**Storage Change**: `/Users/darraghdowney/Workspace/github.com/graphdb/pkg/storage/temporal.go` already defines `TemporalEdge`, `GetEdgesAtTime()`, and `GraphSnapshot`. The seam exists. Current limitation: temporal metadata lives in edge properties (`valid_from`, `valid_to` as `IntValue`). To unlock: (a) add node-level temporal tracking (UpdatedAt already exists at `types.go:399`); (b) add snapshot isolation in WAL replay so `GetNode(id, timestamp)` reconstructs deleted nodes; (c) index temporal edges by `(valid_from, valid_to)` ranges for fast point-in-time lookups instead of scanning all edges.

**Effort**: M (3-4 weeks). Index design + WAL snapshot iteration + testing.

**Migration Risk**: None. Temporal properties are optional; existing nodes/edges default to `ValidFrom=CreatedAt, ValidTo=0 (infinity)`.

---

### 2. **Geospatial Hybrid Search** (Location-Aware Graphs)
**Competitor**: Neo4j with APOC Procedures, TigerGraph spatial functions  
**Data Shapes Unlocked**:
- Find nodes within N km of a coordinate (e.g., "all users in San Francisco")
- Route graphs: nearest warehouse, shortest path with distance weighting
- Spatial proximity joins: "connect nodes if they're within 100m"
- Density analysis: "cluster hotspots of high-degree nodes"
- Travel-time networks: combine graph distance with geospatial distance

**Storage Change**: `types.go` has `TypeVector` for embeddings (line 20); add `TypeGeoPoint` (lat/lng as two floats or packed 8-byte struct). Extend `VectorIndex` in `vector_index.go:11` to support spatial indexes (R-tree or quadtree). Current `HNSWIndex` uses `DistanceMetric` which is agnostic to the space; add `MetricHaversine` variant. Attach spatial index to GraphStorage alongside `vectorIndex` at `storage_types.go:31`.

**Effort**: M (3-4 weeks). Spatial distance function + index structure + k-NN query optimization.

**Migration Risk**: None. New type tag in `Value`; existing code reads/writes as before. Spatial indexes are optional per-property.

---

### 3. **Schema Enforcement with Path Queries** (Typed Property Constraints)
**Competitor**: GraphQL schema, Apache Jena shape constraints  
**Data Shapes Unlocked**:
- "Users must have `email` (string) and `age >= 18`" — enforced at write time
- "Order → LineItem edges must carry integer `quantity ≥ 1`"
- "Person.city must refer to an existing City node via a `lives_in` edge"
- Label-scoped schemas: `Person` nodes have `{name: string, dob: timestamp}` but `Company` nodes have `{name: string, founded: timestamp}`
- Automatic schema inference from write patterns (detect `Person.age` values are always int)

**Storage Change**: `pkg/constraints/property.go` already validates properties post-hoc (lines 10–99). Current structure: `PropertyConstraint` checks type, range, and required-ness. Extend to: (a) pre-write validation in `pkg/storage/node_operations.go:CreateNode()` and `edge_operations.go` (not built—only `UpdateEdge` exists); (b) add foreign-key constraints (EdgeConstraint checking target node exists); (c) add label-to-schema mapping in a new `SchemaRegistry` that maps `label → {required_properties, types, ranges}`. Hook it into the write path, not just audit.

**Effort**: S (2-3 weeks). Reuse existing `PropertyConstraint` logic, add pre-write gate, schema registry is a map.

**Migration Risk**: Low if rolled out as advisory-first (log violations, don't reject writes). Enforcement can be per-tenant or toggled by schema version. Existing data with missing properties doesn't fail retroactively.

---

## The Data-Model Decision Doing Heavy Lifting

**The `Value` tagged-byte union (types.go:30–32) is carrying three jobs that competitors split into three systems:**

1. **Type safety**: The 11 type tags (`TypeString` through `TypeBoolArray`) give Go type checking at write time and prevent type confusion in queries.
2. **Extensibility without schema**: New properties on existing nodes don't require schema migration—just add a new (key, Value) pair. Arrays let N-ary properties without schema rework.
3. **Compression preparedness**: The binary encoding (lines 36–143) pre-encodes values in a format amenable to columnar storage or compression—no JSON serialization tax.

**The cost**: Adding new value types (e.g., `TypeGeoPoint`, `TypeJson`) requires binary protocol bumps and careful WAL migration. Schema migration is *possible* but *painful*—any shift in how a type is encoded (e.g., 4-byte to 8-byte coordinates) requires one-way replay. The architect doc (FEATURES_architect_2026-05-08.md) flags this explicitly: "Schema migration touching array types will be painful." This design chose write-path simplicity and query-time polymorphism over declarative, version-aware schemas. For a multi-tenant product, that trade-off is sound—schema sprawl per tenant is the real risk, not per-type migration. The decision deserves credit in the design narrative because it unlocked geospatial and bitemporal support at zero schema cost; competitors with declarative schemas would need a schema-evolution story first.

---

**Recommendation**: Prioritize bitemporal (quickest ROI on audit/compliance use cases) then geospatial (strongest differentiation against Neo4j for routing/location graphs) then schema (table-stakes for B2B SaaS, but enforces discipline on the customer, not the product).
