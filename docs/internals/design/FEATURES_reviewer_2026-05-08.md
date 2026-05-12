# Killer-Features: Quality-Gate Review
**Date**: 2026-05-08  
**Reviewer**: Quality Gate (Senior Engineering)  
**Scope**: Pressure-test all four specialist reports for overclaims, omissions, contradictions, and highest-leverage recommendations.

---

## 1. Overclaims & Corrections

The **strongest overclaim is the "LSA at 10M-node scale" framing** appearing implicitly across multiple reports (market: "offline semantic search," database: "bitemporal at any scale," performance: not stated but architect framing suggests it). Performance spec is blunt: `TopKByVector` is single-threaded O(D×k) full-scan dot-product with no SIMD, **hard ceiling ~500K documents before P99 exceeds 100ms**. Database spec's bitemporal and geospatial features propose LSA-adjacent indexes without acknowledging this. Marketing the LSA determinism as a killer feature without qualifying the 500K-node ceiling is a false-positive buyer signal — a customer querying a 2M-node graph expecting LSA-backed semantic search will discover the feature degrades gracefully by month three. **Correction**: All killer-feature recommendations must include the LSA performance ceiling in fine print and must not be marketed as "scale-independent." The 500K bound is the hard constraint on Features 1 & 2 (graph-bounded k-NN, GraphRAG retrieval).

---

## 2. Notable Omissions

**Developer Experience & Debugging** is the most glaring gap across all four reports. No specialist proposed:
- **Query explainer** (e.g., "why did this hybrid query choose FTS over LSA?", "why did RRF rank this node #5?") — critical for LLM app debugging.
- **Schema introspection API** (`GET /schema` returning types, constraints, indexes) — table-stakes for SDKs and IDE plugins.
- **Sample/fixture data loaders** (demo knowledge graphs, benchmark corpora auto-loaded) — kills the "I can't evaluate without building a dataset" friction.
- **Telemetry/metrics endpoints** (cardinality stats, query latency histograms, tenant isolation confirmation) — required before production multi-tenant SaaS.

**On-prem / air-gapped value prop beyond LSA** is undersold. The architect notes "LSA determinism" and "no API dependency," but the database spec does not propose:
- **Backup/restore with point-in-time recovery** (compliance: restore to 2026-03-15 snapshot) — already scaffolded by WAL and temporal indexes but not exposed as a user-facing feature.
- **Migration from Neo4j/Memgraph** (Cypher→REST mapping, Cypher output format, automated schema inference) — zero support for teams evaluating replacement.

**Self-serve onboarding** was not proposed by any specialist. Docker Compose template with sample data, managed Cloud Run image, or Heroku button — all are missing. The API specialist proposed embeddings endpoint but no "hello-world" integration guide.

---

## 3. Contradictions & Hidden Couplings

**Performance ↔ Database contradiction**: The database specialist proposes **geospatial hybrid search** as a "3–4 week effort" adding R-tree or quadtree spatial indexes to `VectorIndex`. The performance specialist already warned that HNSW-driven queries hit a 500K-document ceiling due to single-threaded scanning. **Geospatial queries on a 500K-node graph with spatial distance + semantic distance + graph membership constraints are not benchmarked and risk compounding the bottleneck.** The database spec assumes spatial indexes are orthogonal; they're not. Performance must benchmark geospatial k-NN before shipping.

**Architect ↔ Market/API implicit coupling**: The architect states openCypher/GQL is "3–5 person-months" and is gated on the storage interface extraction (Section 3). The market specialist lists openCypher as **Killer Feature #1 and "OSS table stakes."** The API specialist's three proposals (SSE subscriptions, embeddings, CDC) are all unblocked in the current architecture. **This creates a resource contention issue the synthesis did not name**: if both a storage interface extraction (Architect priority) and openCypher implementation (Market priority) are required to be competitive, the synthesis must choose which one unblocks first. The architect implies storage interface is the gating blocker; market positioning assumes openCypher is the revenue gate. **This is a sequencing decision that requires explicit hand-off.**

**API ↔ Architect implicit dependency**: The API specialist recommends `/v1/embeddings` endpoint as "Priority 1" with no storage interface dependency. This is correct for the API surface. However, if the performance specialist's geospatial work (database spec) and the architect's note about LSA determinism per tenant (architect spec, line 13: "LSA per-corpus-hash") combine, then a multi-tenant deployment with per-tenant LSA indexes will have **per-tenant embeddings endpoints**, which the API spec did not design for. The spec assumes a single shared LSA model driving `/v1/embeddings`. If per-tenant isolation is required (compliance use case in market spec), the endpoint must have tenant-scoped routing, not shown.

---

## 4. Highest-Leverage Feature to Lead With

**Priority #1: `/v1/embeddings` endpoint (OpenAI-compatible LSA surface).**

This feature has the strongest combined signal:
- **Market**: Kills the "which embedding model?" friction. Unblocks 99% of post-2024 agentic LLM code that expects OpenAI API shape. Zero friction for LangChain, Vercel AI SDK, other integrations.
- **Performance**: Validates LSA quality at the 100K–500K node scale (the realistic ceiling). If LSA embeddings don't rank well at 500K nodes, the feature fails; bench will reveal that early.
- **Architecture**: Entirely unblocked. No storage interface extraction, no distributed transactions, no MVCC required.
- **Build effort**: 2–3 days (API spec). Fastest time-to-market with highest cross-cutting leverage.
- **Business**: Converts LSA from an internal implementation detail into a competitive API surface. No competitor in the sub-5K-node Go graph DB space exposes LSA-based embeddings as a standard API.

Ship this first, benchmark it, then decide whether geospatial and bitemporal are next.

---

## 5. Highest-Risk Feature (Avoid Leading With This)

**Avoid leading with: openCypher / GQL query surface (Market Killer Feature #1).**

Reasons:
- **Effort mismatch**: 3–5 person-months is team capacity for months. Market positions it as "OSS table stakes," implying it must be in Community before launch. This creates pressure to ship incomplete Cypher (missing procedures, no graph procedures, limited planner optimization) just to have it exist.
- **Gated on storage interface extraction**: The architect explicitly states Cypher/GQL work is months away and depends on the storage interface extraction first. If the architecture team is still in week 1 of interface extraction, Cypher becomes a false signal (promised but months delayed), eroding credibility with OSS evaluators.
- **Quality risk**: Cypher/GQL without a competent query planner and graph procedure library (shortest path, community detection, PageRank) is a Trojan horse — users evaluate, hit the missing planner, and conclude "graphdb Cypher is toy-grade." Shipping it half-baked does more damage than not shipping it.
- **Market capture risk**: Teams evaluating graph databases for Cypher compatibility are already Neo4j/Memgraph/FalkorDB customers. They have high bar for Cypher parity. A 1.0 Cypher that is 60% feature-complete will not convert them; it will reinforce that graphdb is a specialization, not a replacement.

**Recommendation**: Ship `/v1/embeddings` + GraphRAG retrieval (both low-effort, high-impact), then use that momentum to fund the architect's storage interface extraction properly, then tackle Cypher. Cypher is a 2–3 sprint commitment that must be done right, not rushed to check a box.

---

## Summary

**Overclaims**: LSA scale ceiling (500K nodes) is not respected in geospatial and bitemporal marketing.  
**Omissions**: Query explainers, schema introspection, backup/restore, Neo4j migration, self-serve onboarding, telemetry endpoints.  
**Contradictions**: Geospatial indexing risk compounding HNSW bottleneck; storage interface vs. Cypher sequencing not explicit; multi-tenant embeddings not designed.  
**Highest-leverage**: `/v1/embeddings` (OpenAI-compatible LSA). 2–3 days, unblocked, kills framework friction, fastest ROI.  
**Highest-risk**: openCypher/GQL. 3–5 months, gated on architecture work, half-baked release risks credibility.
