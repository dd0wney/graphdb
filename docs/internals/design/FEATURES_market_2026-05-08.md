# Killer-Features: Market Analysis
**Date**: 2026-05-08
**Scope**: graphdb vs. Neo4j, Memgraph, ArangoDB, Dgraph, NebulaGraph, TigerGraph, Weaviate, Qdrant, Pinecone, Apache AGE, FalkorDB

---

## Top 3 Candidate Killer Features

---

### 1. openCypher / GQL query surface

**Competitor that has it**: Neo4j (Cypher), Memgraph (Cypher), FalkorDB (openCypher), Apache AGE (Cypher-on-Postgres), ArangoDB (AQL covers the same graph pattern space). ISO/IEC 39075 GQL was ratified April 2024; the entire incumbent ecosystem is converging on it.

**Developer pain it removes**: The single most common reason a developer stops evaluating a graph database in the first minute — "does it speak Cypher?" graphdb currently offers REST and GraphQL. Neither lets a developer express a variable-length path traversal, a MATCH pattern, or a shortest-path computation without writing application code. Every competitor on this list except the pure vector stores ships a declarative traversal language. Absence of one forces developers to pre-implement traversal logic in the client, blocks adoption by any team that came from Neo4j, and eliminates graphdb from any evaluation that touches knowledge-graph or network analysis use cases.

**Why graphdb's stack is positioned (or not)**: The underlying storage has nodes, edges, types, and per-tenant indexes — the data model is a labeled property graph. Adding an openCypher parser and an executor that translates to storage reads is not a rearchitecture. The execution engine would target the same node/edge scan primitives the REST handlers already call. The hybrid search could be exposed as a Cypher procedure (CALL graphdb.hybridSearch(...)), which immediately differentiates the Cypher surface from Neo4j's.

**Build effort**: L. Parser, type system, executor, planner, test corpus. Nothing is foreclosed architecturally, but this is 3–5 person-months of focused engineering with a correct implementation.

**Commercial vs. OSS placement**: OSS table stakes. Without it, the OSS offering cannot compete for the developer evaluating their first graph DB. Must be in Community.

---

### 2. Graph-traversal-aware vector retrieval (GraphRAG retrieval endpoint)

**Competitor that has it**: Neo4j Vector Cypher Retriever (December 2025 release) combines HNSW vector search to seed a starting node with Cypher-driven graph expansion to gather neighbors — the retrieval result includes both the vector-matched node and its relationship context. FalkorDB's GraphRAG SDK does the same via openCypher. Weaviate has Hybrid Search 2.0 (BM25 + dense vectors + RRF) but traverses HNSW index graphs, not semantic property graphs.

**Developer pain it removes**: LLM retrieval pipelines hit graphdb today as two serial REST calls — one to hybrid-search for seed nodes, one to fetch neighbors. This round-trip forces the client to implement graph expansion and context packing, which means the LLM integration code grows outside the database. A single endpoint that does vector seed + n-hop expansion + context window serialization removes that client complexity and makes graphdb the natural LLM retrieval backend.

**Why graphdb's stack is well-positioned**: This is the highest-confidence recommendation because nothing architectural needs to change. `handleHybridSearch` already merges FTS + LSA + HNSW + RRF into a ranked list. Adding a `traversal_depth` parameter and a streaming result writer that serializes node context as `text/event-stream` (SSE) composes the entire existing stack. The architect's framing confirms: "Adding a streaming result writer and a context-window packer on top of handleHybridSearch is days of feature work, not rework." No competitor in the sub-5K-node Go graph DB space ships this natively.

**Build effort**: S. 1–2 weeks. Constrained to `pkg/api/handlers_hybrid_search.go` and the storage graph-walk primitives already present.

**Commercial vs. OSS placement**: OSS core (tenant-scoped); Enterprise tier can gate advanced context-window strategies (e.g., community-summarization passes). Ships in Community to drive adoption.

---

### 3. Native per-tenant property-filter and audit compliance API

**Competitor that has it**: Neo4j's data privacy and compliance graph use-case documentation positions it for GDPR/CCPA workloads, but the enforcement is application-layer — no database-level property masking on reads. Weaviate's RBAC controls access at the collection level, not the property level. None of the competitors in this set ship a documented, query-time, per-tenant property masking layer with a persistent audit log as native database features.

**Developer pain it removes**: Regulated-data customers (healthcare, finance, legal) building multi-tenant SaaS on a graph database today must implement field-level redaction, audit trails, and consent-based visibility in application code. That code is bespoke, untested against the database's access paths, and fails audits because it is not enforced at the data layer. A database that enforces property masking at query time and produces an immutable audit log eliminates an entire category of compliance engineering work.

**Why graphdb's stack is well-positioned**: `pkg/masking/`, per-tenant property filter (recent commits), per-tenant indexes in `storage_types.go`, and the persistent audit log in `pkg/audit/` are all live. The architect confirms: "The remaining work is integration story and documentation, not net-new capability." The feature exists; it lacks a marketable surface (a documented compliance API, a Swagger-tagged `/audit` endpoint, a reference integration guide).

**Build effort**: S. The enforcement code exists. Effort is documentation, a stable REST compliance API under a versioned route, and a reference SOC2/GDPR integration guide.

**Commercial vs. OSS placement**: Split. Basic masking and audit log — OSS. Advanced compliance exports, retention policy enforcement, immutable audit chain verification — Enterprise. This is the strongest candidate for real open-core gating because the capability difference is concrete and auditable.

---

## What graphdb already has that no competitor in this list has

The architect's "FTS + LSA + HNSW + RRF — nobody ships this" claim holds when narrowed precisely. Weaviate ships BM25 + dense vectors + RRF (three of the four signals); FalkorDB ships full-text + vector + range; Neo4j ships Lucene full-text + HNSW-backed vector search fused via Cypher procedures. What none of them ship is a **deterministic, in-process LSA semantic index** — no external embedding model, no API call, no GPU, no model version pinning problem. graphdb's `pkg/search/lsa.go` seeds at a fixed value (42) per corpus hash, making semantic vectors reproducible and cacheable against the WAL LSN. For air-gapped deployments, cost-sensitive workloads, and regulated environments where data cannot leave the process boundary to reach an embedding API, this is a genuine differentiator with no direct equivalent in the competitor set. The marketing claim to make is not "four search signals" but "offline, deterministic semantic search with no embedding API dependency."

---

## Foreclosed features (do not recommend as near-term wins)

Distributed transactions, MVCC/snapshot isolation, real-time incremental LSA, shared-nothing multi-tenancy, stable binary plugin distribution via `.so` — all require storage-layer rework prior to the storage interface extraction. None are near-term.
