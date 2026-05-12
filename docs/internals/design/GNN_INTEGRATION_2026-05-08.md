# Graph Neural Networks on graphdb

**Status**: Analysis. No code changes implied.
**Date**: 2026-05-08
**Question**: "Can we build a Graph Neural Network with graphdb?"

---

## TL;DR

**Partially today; fully with one targeted addition.** graphdb has unusually well-aligned primitives for GNN *serving* — vector-typed properties, multi-property HNSW, a parallel k-hop traversal, hybrid retrieval, and per-tenant isolation are all already wired. What's missing is the gradient-flow / training side: no autograd, no GPU support, no native message-passing kernel.

The realistic path is to **use graphdb as a GNN inference backend** for models trained elsewhere (PyTorch Geometric, DGL). The combination of HNSW + FTS + LSA + RRF + traversal is the closest thing to "production GNN serving in a single Go binary" that exists in graphdb's weight class — most competitors ship one or two of those signals; graphdb ships all five.

A small Go inference kernel (`pkg/gnn`) is a 1–2 week addition that unlocks GraphSAGE-style mean aggregation in-process. Building a training engine is the wrong fit and explicitly out of scope.

---

## What graphdb already has that GNNs need

The serving-side primitives are real and tested. Each row below cites the concrete code seam.

| GNN need | What graphdb provides | Code seam |
|---|---|---|
| Node features as vectors | `storage.VectorValue` — float32 arrays as a first-class node property type | `pkg/storage/types.go` |
| Variable-dim per-property | HNSW index per property name; multiple feature spaces per node | `pkg/vector/`, recent commit `4f7d105 feat: startup index bootstrap` |
| Graph traversal for message passing | NumCPU-aware k-hop BFS with `sync.Map` visited tracking, per-level goroutine semaphore, context propagation | `pkg/query/parallel_traversal.go` (named in `docs/FEATURES_performance_2026-05-08.md` as "the most competitively underexposed piece in the codebase") |
| Adjacency lookup hot path | Sub-ms `GetOutgoingEdges` / `GetIncomingEdges` with shard-locked edge access | `pkg/storage/query_operations.go:28` |
| Per-tenant isolation of the dataset | `TenantID` at the storage primitive; per-tenant HNSW indexes | `pkg/tenantid/`, `pkg/storage/storage_types.go` |
| Hybrid retrieval to seed inference | FTS + LSA + HNSW fused via RRF — exactly the input shape a GNN-augmented retriever wants | `pkg/api/handlers_hybrid_search.go` |
| Reproducible / deterministic embeddings | LSA model is seed-fixed (default seed=42), so document vectors are reproducible per corpus hash | `pkg/search/lsa.go:67` |
| OpenAI-compatible inference surface | `/v1/embeddings` endpoint shipped (graphdb#7) — the same shape applies to "embed-and-rerank" pipelines | `pkg/api/handlers_embeddings.go` |

This is real GNN-serving infrastructure. graphdb is already a competent **inference-time backend** for a model trained elsewhere.

---

## What graphdb does NOT have

- **No automatic differentiation, no gradient kernels.** No `pkg/autograd` or equivalent. You can't train a GAT or GraphSAGE *in* graphdb. This is the right call — Python's ML ecosystem (PyTorch Geometric, DGL) is where training lives.
- **No GPU support.** Pure-Go float32 vector ops run on CPU. Fine for inference at the realistic deployment ceiling (~100K-500K nodes at 200 dims, per the LSA bottleneck named in `docs/AUDIT_performance_2026-05-06.md`); not viable for training large GNNs.
- **No native `aggregate_neighbors(features) → updated_features` primitive.** The traversal + per-node feature read + per-tenant aggregate exist as separate calls; there's no single optimized "message passing" kernel that does all three.
- **No batched inference path.** Each request runs one query. Throughput is fine for human-paced workloads; bulk scoring of millions of (query, target) pairs would need batching.

---

## Three integration shapes

Pick ONE based on use case; do not try to do all three.

### Shape 1 — graphdb as a GNN inference backend *(viable today)*

**What you build**:

1. Train your GAT / GCN / GraphSAGE in PyTorch Geometric.
2. Export per-node embeddings (numpy → JSON or Parquet).
3. Load embeddings into graphdb as `storage.VectorValue` properties (one property per embedding head — e.g., `node.gat_h0`, `node.gat_h1`).
4. Serve k-hop predictions via the existing hybrid-search + traversal stack: query → seed nodes (HNSW retrieval over original features) → expand (k-hop traversal) → rerank (cosine over GAT embeddings stored as properties).

**Effort**: zero new graphdb code. All work is in the import pipeline (your responsibility) and in the application layer that orchestrates the queries.

**Why it works**: this is exactly the pattern Neo4j sells as "Graph Data Science" — train externally, store embeddings as node attributes, query via Cypher procedures. graphdb has every primitive Neo4j has for the *serving* side, plus the LSA + RRF fusion that Neo4j doesn't.

**Limit**: model is frozen. Updating embeddings means re-running the training and re-importing.

---

### Shape 2 — graphdb + a thin Go inference kernel *(1-2 weeks)*

**What you build**:

A new `pkg/gnn` package with one function:

```go
package gnn

// MessagePass runs one round of neighbor aggregation. For each nodeID,
// reads the named feature property, gathers the same property from all
// k-hop neighbors, and writes the aggregated result to outProperty.
//
// agg = "mean" | "max" | "sum"
func MessagePass(
    ctx context.Context,
    graph *storage.GraphStorage,
    tenantID string,
    nodeIDs []uint64,
    feature, outProperty string,
    hops int, agg string,
) error
```

Inference-only — no gradient buffers, no backward pass. Composes:
- `pkg/query/parallel_traversal.go` for the k-hop expansion
- `getNodeRefForTenant` (audit A3b) for zero-clone feature reads
- A SIMD-able float32 aggregator (Go's `math.Float32frombits` + manual loops; no external lib)

**Effort**: M. ~500-800 LOC, mostly in one file. Tests use a deterministic 5-node graph with known aggregations.

**What it unlocks**:
- GraphSAGE-style mean aggregation in-process (no PyTorch round-trip per query)
- Inference-only graph attention if the trained attention weights are exported as edge properties
- Real-time recommendation use cases (e.g., "score these 1000 candidate nodes against a starting node by 2-hop GNN aggregation") at sub-100ms latency

**Limit**: still no training. This is "frozen GNN inference, fully in-process."

**Sequencing**: should land *after* GraphRAG retrieval (F2 in `docs/NEXT_STEPS_2026-05-08.md`). F2 establishes the streaming retrieval seam; the GNN kernel composes on top of it.

---

### Shape 3 — graphdb as a training-loop store *(months; not recommended)*

**Why it's the wrong fit**:

Real GNN training needs gradient flow through the graph plus high-bandwidth feature reads (gigabytes/second from disk to GPU). graphdb's storage layer is in-memory + WAL; the LSM is for cold tiering, not for training-throughput reads.

The Python ML ecosystem (PyTorch Geometric, DGL) has solved this with their own data loaders and is where this work belongs. graphdb could in principle be a *source* for those loaders (export-as-Parquet, export-as-DGL-format), but that's a feature of the *export pipeline*, not a training engine living inside graphdb.

**Recommendation**: not in this 90-day window or the next.

---

## Recommendation

**Lead with Shape 1.** It's zero new graphdb code; it validates whether GNN serving has demand from real users; the trained-model artifacts you produce are reusable across Shapes 1 and 2.

If Shape 1 surfaces a clear UX bottleneck — e.g., "the round-trip to a Python embedding service is too slow for our latency budget" — that's the trigger to invest in Shape 2.

**Do NOT** preemptively build Shape 2 before Shape 1 produces feedback. The audit's lead candidates (GraphRAG retrieval, OpenAI-compat embeddings, compliance API) all ride the *current* primitives; they should ship and produce real signal before any new pkg/gnn work starts.

---

## Concrete starting seams (for whoever picks this up)

If you're starting Shape 1:

- **Embedding import**: write a one-shot CLI `cmd/import-embeddings/main.go` that reads `(node_id, [floats])` from JSON Lines or Parquet and calls `s.graph.UpdateNodeForTenant(id, map[string]Value{"gat_h0": storage.VectorValue(vec)}, tenantID)` for each row.
- **Inference query**: extend the existing `/hybrid-search` endpoint with an `embedding_property` parameter that, when set, replaces the LSA cosine scoring with cosine over the named property. Code seam: the LSA section in `pkg/api/handlers_hybrid_search.go:128-143`.

If you're starting Shape 2:

- **Kernel skeleton**: new `pkg/gnn/messagepass.go` with the signature above. Reuse `pkg/query/parallel_traversal.go`'s `NumCPU` worker pool and `sync.Map` visited tracking.
- **Aggregator**: float32 mean/sum/max — pure Go, tight loop, no external dep. Optional: SIMD via `golang.org/x/sys/cpu` feature detection later.
- **Storage hook**: write the result back via `UpdateNodeForTenant`; reuse the audit A3b enforcement for tenant safety.

---

## Out of scope / explicit non-goals

- **In-process training of large GNNs**. Wrong tool. PyTorch Geometric is the right one.
- **GPU acceleration**. graphdb is CPU-only; the LSA scale ceiling (~100K-500K nodes) makes GPU ROI low at this deployment size.
- **A graph-DSL extension to express GNN queries** (a la TigerGraph's GSQL). The existing query DSL plus a `pkg/gnn.MessagePass` API is sufficient for Shape 2; a DSL extension is over-engineering.
- **Online learning / incremental gradient updates**. Same reason as Shape 3 — wrong fit for this codebase.
- **Distillation or compression of trained models inside graphdb**. Belongs upstream of the import pipeline.

---

## Cross-references

- `docs/FEATURES_synthesis_2026-05-08.md` — the orchestration that named GraphRAG retrieval as the lead killer feature; GNN inference is a natural extension of that
- `docs/FEATURES_performance_2026-05-08.md` — names `pkg/query/parallel_traversal.go` as the most underexposed perf differentiator; GNN message passing rides directly on top of it
- `docs/AUDIT_performance_2026-05-06.md` — the LSA / single-node scale ceiling that bounds Shape 2's realistic deployment size
- `pkg/storage/types.go` — `Value.Type = TypeVector` is the existing primitive for storing per-node feature vectors
