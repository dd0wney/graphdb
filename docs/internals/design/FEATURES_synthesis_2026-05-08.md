# Analysis: Identify missing killer features

**Method**: Manager-orchestrated multi-specialist analysis. Phase 2 (architect) → Phase 3 (market, api, performance, database in parallel) → Phase 4 (tester, reviewer in parallel). Individual reports: [`FEATURES_architect`](./FEATURES_architect_2026-05-08.md), [`FEATURES_market`](./FEATURES_market_2026-05-08.md), [`FEATURES_api`](./FEATURES_api_2026-05-08.md), [`FEATURES_performance`](./FEATURES_performance_2026-05-08.md), [`FEATURES_database`](./FEATURES_database_2026-05-08.md), [`FEATURES_tester`](./FEATURES_tester_2026-05-08.md), [`FEATURES_reviewer`](./FEATURES_reviewer_2026-05-08.md).

## Architecture

1. **The FTS + LSA + HNSW + RRF stack at `pkg/api/handlers_hybrid_search.go` is the load-bearing differentiator.** No competitor in graphdb's weight class fuses all four signals in a single binary with no external embedding-API dependency. GraphRAG retrieval is a *days-of-feature-work* extension, not rework — `expand_hops` parameter + SSE streaming on the existing handler.
2. **Storage interface extraction is the single architectural unlock** that opens five follow-on capabilities: open-core feature gating, plugin sandboxing, tiered backends, REST/GraphQL service-layer parity, distributed-transactions precondition. This is *the* lever the prior audit identified, and it remains the lever for ambitious commercial features.
3. **Plugin/editions split is currently cosmetic** (`pkg/editions/features.go` is an advisory map; `.so` distribution model is fragile). Becomes a real open-core business lever only after items #1 (storage interface) and a replacement of Go's native `plugin` package with gRPC-over-Unix-socket or WASM. 2-4 focused sprints.

## Specialist Findings

| Specialist | Key Insight | Action |
|---|---|---|
| **Architect + Market + Performance** (3-vote consensus) | **GraphRAG retrieval** — query → hybrid retrieval → n-hop expansion → SSE-streamed context. Unique in sub-5K-node Go graph DB space. Existing seam at `handleHybridSearch`; ICIJ Offshore Leaks corpus available for benchmark vs. Weaviate. | Add `expand_hops int` + streaming response to `pkg/api/handlers_hybrid_search.go`. ~1-2 weeks. |
| **API + Tester + Reviewer** (3-vote consensus on cheapest first ship) | **`/v1/embeddings` OpenAI-compat endpoint** — drop-in for LangChain/Vercel AI SDK. LSA already deterministic (`pkg/search/lsa.go`), seed-fixed at 42, reproducible per corpus hash. Kills "which embedding model?" friction. | New `pkg/api/handlers_embeddings.go`; route in `pkg/api/server.go`. ~2-3 days. Risk: LSA recall must be competitive at 100K-500K nodes (the current ceiling). |
| **Market** (sole signal, but strong) | **Privacy + audit compliance API** — almost-shipped capability. `pkg/masking/`, per-tenant property filter, persistent audit log, per-tenant indexes are all live. Lacks marketable surface (versioned REST API, SOC2 reference integration). No competitor in the comparison set ships query-time per-tenant property masking + immutable audit log as native DB features. | Package the existing work — `pkg/api/handlers_compliance.go` (new), Swagger tags, SOC2/GDPR reference guide. ~1 week. Real open-core gating candidate. |

## Implementation Plan

1. **Ship `/v1/embeddings`** — `pkg/api/handlers_embeddings.go` (new). Backed by `pkg/search/lsa.go`. Routes in `pkg/api/server.go`. Mirror OpenAI request/response shape. ~2-3 days. *This is the cheapest validation of the whole "private, deterministic embeddings" pitch.*
2. **Extend hybrid search to GraphRAG** — add `expand_hops` + SSE streaming to `pkg/api/handlers_hybrid_search.go`. Reuse `pkg/storage` traversal primitives. Publish reproducible ICIJ corpus benchmark vs. Weaviate. ~1-2 weeks. *Composes the existing differentiator into a marketable surface.*
3. **Package compliance API** — `pkg/api/handlers_compliance.go` (new), Swagger annotations, SOC2 reference integration guide tying together `pkg/masking` + per-tenant property filter + `pkg/audit`. ~1 week. *Code exists; this is integration story and documentation.*

## Risks

- **LSA scale ceiling** (~100K-500K docs at 200 dims; `pkg/search/lsa.go:545-573` is single-threaded O(D×k) scan with no SIMD). Any "10M nodes" claim involving LSA would fail on a buyer's bench. *Mitigation*: pitch as "private, deterministic, no external API dependency for sub-1M-node deployments." Document the ceiling explicitly in feature copy.
- **Geospatial / temporal data-model features compound the LSA bottleneck** if shipped without isolation. The database specialist proposed both; the reviewer flagged that adding spatial R-tree at 500K nodes risks compounding the existing single-threaded HNSW path. *Mitigation*: defer geospatial until LSA ceiling is bench-validated; prefer features (compliance API, GraphRAG) that ride decoupled paths.
- **openCypher half-shipped is worse than absent.** Market specialist named it "OSS table stakes" for adoption; reviewer flagged it as gated on the storage interface (months) — shipping half-baked Cypher breaks the queries Neo4j users actually run. *Mitigation*: don't announce Cypher until (a) storage interface extraction lands and (b) a defined query corpus passes.

## Open Questions

- **Resource contention with in-flight Track A audit work.** The audit's storage interface extraction (priority-one) is *the* unlock for many of these features, but Track A is currently mid-flight (PR #5 unmerged, A3a/A3b still ahead). Does pursuing `/v1/embeddings` + GraphRAG retrieval in parallel with Track A create scheduling pressure, or are they orthogonal? Specifically: do GraphRAG's traversal calls need to be tenant-aware before launch?
- **Multi-tenant LSA**: `pkg/search/lsa.go:32` notes the LSA model is *not* tenant-scoped — tenant B's writes influence tenant A's semantic results. Performance specialist named this as a follow-up; reviewer flagged it as a hidden coupling with the proposed `/v1/embeddings` endpoint. **Required to fix before launch, or acceptable for v1?**
- **Sequencing vs. Syntopica integration commitment.** The property_filter privacy work (PR #1) was driven by Syntopica's Phase 2a. GraphRAG retrieval would expose the same paths through new endpoints. Should GraphRAG ship be gated on completion of audit Track A so the new endpoint inherits tenant isolation correctly, rather than re-introducing the same security finding under a new URL?
