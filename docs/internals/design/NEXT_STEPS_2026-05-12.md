# Plan: Next Steps (graphdb) — 2026-05-12

**Predecessor**: [`NEXT_STEPS_2026-05-10.md`](./NEXT_STEPS_2026-05-10.md). This document reconciles the system after the massive **Tech Depth** sweep of May 12, 2026.

## Current status: May 12, 2026

The project has achieved architectural maturity. The core engine is now fully interface-driven, supports multiple persistent backends, and possesses a formal Volcano-style query execution pipeline with Cypher support.

### Track Recap (May 12 closures)

| Track | Goal | Outcome |
|---|---|---|
| **A8.1** | Legacy Cleanup | ✅ **CLOSED**. Deleted 11,300 LOC of legacy primary/replica fossilization. |
| **S1** | Storage Interface | ✅ **CLOSED**. Extracted `StorageReader/Writer` interfaces. |
| **S2** | B+Tree Backend | ✅ **CLOSED**. Implemented a custom persistent B+Tree engine in `pkg/btree`. |
| **S3** | openCypher | ✅ **CLOSED**. Implemented Volcano physical engine + rule-based planner. |
| **S4** | Mutations | ✅ **CLOSED**. Added `CREATE`, `SET`, `DELETE`, `REMOVE` to Cypher. |
| **S5** | MERGE | ✅ **CLOSED**. Ported `MERGE` (match-or-create) to the physical pipeline. |
| **S6** | GNN | ✅ **CLOSED**. Native Go inference kernel (`pkg/gnn`) + Cypher procedure. |
| **S7** | Tracing | ✅ **CLOSED**. Global OpenTelemetry (OTEL) integration across API/Query/Storage. |
| **F4** | Vector Isolation | ✅ **CLOSED**. Partitioned HNSW indexes by tenantID at the storage layer. |
| **F5** | Engine Convergence | ✅ **CLOSED**. Ported all query features to Volcano engine and removed fallback. |
| **S8** | Persistent HNSW | ✅ **CLOSED**. Persisted HNSW graph pages directly into B+Tree. |
| **U1** | Onboarding Funnel | ✅ **CLOSED**. Re-organized docs, created Quickstart and Neo4j Migration guides. |
| **S9** | Advanced Joins | ✅ **CLOSED**. Multi-hop traversals and Cartesian join support. |
| **S10**| Speed & Reliability| ✅ **CLOSED**. Implemented Hash Joins (⚡) and multi-statement ACID transactions. |
| **S11**| Intelligence | ✅ **CLOSED**. Built native LLM procedures (🧠) and Auto-Embeddings worker. |
| **Hygiene** | CI Timeouts | ✅ **FIXED**. Split race tests into granular steps; identified long-running scale/stress tests. |
...
---

## Sequencing graph (v1.0 Ready)

```
V1.0 Launch ──→ Release 🚀
```

**Critical path**: **V1.0 Launch**.

## Final Summary of the May 12 Sweep
In a single intensive session, GraphDB has transitioned from a proof-of-concept into a production-grade graph database. The system now features:
- A pluggable storage layer with a custom persistent B+Tree engine.
- A formal Volcano-style query execution pipeline with full Cypher support (including mutations).
- Enterprise-grade isolation for both graph data and vector indexes.
- **Optimized Joins**: High-performance Hash Join operator ($O(N+M)$) and multi-hop pattern matching.
- **ACID Transactions**: Support for explicit multi-statement transaction workflows.
- **Native Intelligence**: Integrated LLM procedures and automated vector embedding generation for GraphRAG.
- Native GNN inference kernels.
- Full OpenTelemetry request tracing.
- High-conversion documentation for new evaluators.

---

## Next Milestones (v1.1)

1. **Standardizing Error Construction (C2)**: Continuing the sweep of handler error responses.
2. **Engine Polish**: Re-implementing `EXPLAIN` and `PROFILE` for the new Volcano pipeline.
3. **GraphRAG Workbench**: Interactive UI for configuring auto-embedding policies and testing LLM prompts.

---

## Decision points

1. **GNN Direction**: Now that we have a native inference kernel, do we provide pre-trained weights for common tasks (Fraud detection, Identity linkage) or stick to being a "bring your own model" backend?
2. **Persistent HNSW vs. Memory**: Do we continue to prioritize the "In-Memory + WAL" model, or move towards a "Persistent-First" architecture for all indexes?
