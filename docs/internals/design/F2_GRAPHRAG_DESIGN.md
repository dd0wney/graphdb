# F2 Design Spike — Graph-Augmented Retrieval (`/v1/retrieve`)

**Status:** spike output, not implementation. Date: 2026-05-08.
**Predecessors:** F1 (`/v1/embeddings`), audit Track A complete (Tenant scoping landed in PRs #17-#27 + spike #21).
**Goal:** map the implementation surface for graph-augmented retrieval and pin the design choices that frame F2's PRs #2–#7.

## 1. What's tenant-blind today / what's already there

Existing primitives we compose. Each is tenant-scoped after audit Track A.

| Primitive | Source | Tenant scoping |
|---|---|---|
| `/v1/embeddings` | `pkg/api/handlers_embeddings.go` | F1 — encodes via per-tenant LSA index |
| `/hybrid-search` (FTS+LSA via RRF) | `pkg/api/handlers_hybrid_search.go:60` | A5 + per-tenant `searchIndexes.Get(tenantID)` and `lsaIndexes.Get(tenantID)` |
| `/vector-search` (HNSW) | `pkg/api/handlers_vectors.go` | Per-tenant via the property-vector store |
| `/traverse` (BFS) | `pkg/api/handlers_algorithms_traversal.go` | A6b — `GetOutgoingEdgesForTenant` + `GetNodeForTenant` |

**Hidden dependency check:** `pkg/api/handlers_hybrid_search.go` does NOT expose a `search.SearchHybridForTenant` function — RRF composition lives inline in the handler at lines 100-200ish (per-tenant FTS + LSA fetched via `searchIndexes.Get` / `lsaIndexes.Get`, then merged). F2 has two options here, decided in §2.

## 2. Six design decisions

### Q1: API shape — LangChain `BaseRetriever`, not OpenAI-compat

**Decision: LangChain Retriever shape.** OpenAI doesn't define a retrieval endpoint, so "OpenAI-compatible" is meaningless for F2. LangChain has the widest live-user surface; LlamaIndex's shape (`BaseRetriever`) is similar enough to wrap from a LangChain payload. Bolting GraphRAG into `/v1/chat/completions` tool-calling is wrong — that's an LLM concern, not a retrieval concern.

```jsonc
// POST /v1/retrieve
{
  "query": "What does Alice know about graph databases?",
  "k": 10,                       // Max chunks returned (after token budget)
  "max_tokens": 4096,            // Drop lowest-scored chunks whole until under
  "max_hops": 2,                 // BFS depth from seed nodes
  "alpha": 0.7,                  // Vector weight (0..1)
  "beta": 0.3,                   // Graph-distance weight (0..1)
  "labels": ["Doc", "Person"],   // Optional seed-stage label filter
  "include_node": false          // If true, embed full node in metadata
}
// → 200 OK
{
  "documents": [
    {
      "page_content": "...",
      "metadata": {
        "node_id": 42,
        "score": 0.87,
        "source": {
          "node_id": 42,
          "label": "Doc",
          "path": [17, 23, 42]   // seed → ... → this
        }
      }
    }
  ],
  "took_ms": 124
}
```

### Q2: Path / package — `/v1/retrieve` and `pkg/retrieval/`

**Decision: `POST /v1/retrieve`. Internal package `pkg/retrieval/`.** The endpoint name is forever; "graphrag" is a 2024–2026 buzzword. `/retrieve` describes what it does and matches LangChain's vocabulary.

### Q3: Hybrid composition — factor `pkg/search.SearchHybrid`

**Decision: factor inline RRF logic from `handlers_hybrid_search.go` into `pkg/search.SearchHybridForTenant(tenantID, query, opts) → []SearchHit`.** Both the existing `/hybrid-search` handler and the new `pkg/retrieval/` consumer call it. ~30 lines move from handler to package. Avoids duplication; gives `pkg/retrieval/` a clean primitive to compose against.

This is one extra task in the implementation plan — see §4.

### Q4: Scoring — RRF seed score + exponential graph-distance decay

**Decision:**
```
score(chunk) = α · rrf_seed_score + β · exp(-d / τ)
```
- `rrf_seed_score`: from `pkg/search.SearchHybridForTenant`, normalized to `[0, 1]`
- `d`: graph-distance from any seed (0 = is-a-seed)
- `τ`: decay constant
- Defaults: `α = 0.7`, `β = 0.3`, `τ = 2.0` (chunks 2 hops from a seed get ~37% of distance bonus)
- All three configurable per-request

**Not in v1: centrality weighting (PageRank).** Per-tenant PageRank at retrieval-time isn't free; adds 100–500ms on graphs > 1k nodes. File as v2.

### Q5: Token budget — caller-provided, drop whole chunks

**Decision: caller-provided `max_tokens`, default 4096. Truncate by dropping lowest-scored chunks whole, never by truncating within chunks.** Preserves citation integrity. Document that callers know their model's context window.

Token estimation: simple word-count × 1.3 (rough rule of thumb). v2 considers a real tokenizer if accuracy matters.

### Q6: Citation — `metadata.source.path`

**Decision: every chunk includes `metadata.source.path: [seed_id, ..., this_node_id]`.** This is GraphRAG's value-add over plain vector RAG — it lets the downstream LLM (or app code) explain *why* a chunk is in context. Without `path`, you have a fancy vector retriever with no graph signal exposed.

For seeds themselves, `path = [node_id]` (length 1).

## 3. Algorithm sketch

```
Retrieve(ctx, graph, query, opts, tenantID) → ([]Chunk, error):
  // 1. Seed retrieval — tenant-scoped hybrid search
  seeds := search.SearchHybridForTenant(tenantID, query, {
    limit: 20,
    labels: opts.labels,
  })
  if len(seeds) == 0: return empty result, "no seeds" diagnostic

  // 2. Multi-source BFS expansion — bounded by max_hops AND a hard
  //    cap of 50 nodes total (dense graphs at 3 hops can return
  //    thousands; the cap prevents pathological blow-up)
  visited := map[nodeID → distance]{seed.ID → 0 for each seed}
  parent  := map[nodeID → seedID]{seed.ID → seed.ID for each seed}
  queue   := seeds[:]
  for hop := 1; hop ≤ opts.max_hops AND len(visited) < 50; hop++:
    next := []
    for node := range queue:
      edges := graph.GetOutgoingEdgesForTenant(node.ID, tenantID)
      for edge := range edges:
        if edge.ToNodeID not in visited:
          visited[edge.ToNodeID] = hop
          parent[edge.ToNodeID] = parent[node.ID]
          next = append(next, edge.ToNodeID)
          if len(visited) >= 50: break
    queue = next

  // 3. Score each visited node
  for nodeID, distance := range visited:
    rrfScore := normalize(seedScores[parent[nodeID]])
    score    := opts.alpha * rrfScore + opts.beta * exp(-distance / opts.tau)
    
  // 4. Sort by score desc, take top-k
  // 5. Build chunks (content from node properties; configurable property)
  // 6. Token-budget drop: while sum(tokens) > opts.max_tokens, drop lowest
  // 7. Build path for each chunk via parent[] backtrack to seed
  return chunks
```

**Tenant scoping:** every storage call uses `*ForTenant`. The audit Track A guarantee from PRs #18–#26 is load-bearing — if any of those regressed, F2 inherits the leak. The cross-tenant regression suite (`TestAuditRegressionSuite_CrossTenantIsolation`, A7 PR #27) catches it.

**Edge-type weighting:** v1 follows all edge types uniformly. Configurable per-edge-type weighting is a v2 candidate (e.g., `AUTHORED_BY` weighs more than `MENTIONED`).

## 4. PR breakdown (revised from initial plan)

| PR | Scope | Estimate |
|---|---|---|
| **#1 (this PR)** | Spike doc | S — done |
| **#2** | `pkg/search.SearchHybridForTenant` factor + handler refactor to call it. Pure additive + a one-line handler change. Existing tests still pass. | S |
| **#3** | `pkg/retrieval/` package: `Retrieve(ctx, graph, query, opts, tenantID)` composing search + traverse + scoring. Unit tests including cross-tenant isolation at package level. | M |
| **#4** | `POST /v1/retrieve` HTTP handler. Wraps `pkg/retrieval`. Registered under `withTenant`. Request validation, timeout. LangChain Retriever response shape. | S |
| **#5** | Cross-tenant HTTP test + new row in `TestAuditRegressionSuite_CrossTenantIsolation`: `F2/retrieve-only-returns-caller-tenant`. Plus dedicated test file: happy path, max-hops respected, token budget respected, empty corpus. | S |
| **#6** | Latency benchmark `BenchmarkRetrieve_TypicalQuery` — seed 1k nodes / 5k edges; report p50/p95/p99 for 5-seed × 2-hop. Document the budget in this doc; if not hit, file follow-up. | S |
| **#7** | Docs + curl example + LangChain wrapper example (`examples/retrieve-langchain.py`). No PyPI publish. | S |

**Critical-path:** `#1 → #2 → #3 → #4 → #5`, with `#6` and `#7` parallel after `#4`.

## 5. Out of scope (v2 candidates)

| v2 candidate | Why deferred |
|---|---|
| Microsoft GraphRAG community-summarization | Separate research question (build summaries from the graph, query them at retrieve time). Our graph is already structured — the value of summarization is unclear without specific workloads. |
| Centrality-weighted ranking | Adds PageRank precomputation (100–500ms on graphs > 1k nodes). Defaults can ship without it. |
| Edge-type weighting | Defer until a real workload demands non-uniform weights. |
| Streaming (SSE) | Batch is fine for v1 — most LangChain retrievers are batch. Streaming makes sense if max_tokens > 32k. |
| Real tokenizer | Word-count × 1.3 heuristic is acceptable for budgeting; switch to tiktoken/sentencepiece if budget overruns become a customer issue. |
| Re-ranking with cross-encoder | Would require a separate model server; v2 if quality demands it. |

## 6. Risks

1. **Token budget overruns** if a single node has a huge property. v1 truncates whole chunks, but a 10k-token single chunk can blow past `max_tokens=4096` even after dropping everything else. Document the failure mode (return that chunk alone with a `degraded: "single-chunk-exceeds-budget"` flag) — same pattern as `/hybrid-search`'s `degraded` field.

2. **Hard 50-node cap can hide relevant nodes** in dense graphs. The cap is correctness-preserving but quality-impacting. Document it; consider per-tenant config in v2 if customers complain.

3. **Path metadata is graph-specific** — LangChain consumers may ignore it. Acceptable: the path is opaque metadata, doesn't break the contract, and adds value for consumers that look. Don't omit it — that's the only graph signal in the response.

4. **`pkg/search.SearchHybridForTenant` factor risks regressing `/hybrid-search`.** Mitigation: PR #2 keeps the existing test suite as-is; the factor is mechanical (move logic, add wrapper).

## 7. Recommended next action

Ship this spike. Then run PR #2 (`SearchHybridForTenant` factor) — small, prerequisite for #3. Re-evaluate after #4 lands whether v2 candidates rise above other priorities (A4 shard locks, lint sweep, F3 compliance).
