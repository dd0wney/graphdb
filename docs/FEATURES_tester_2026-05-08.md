# Validation Strategy: GraphDB Killer Features
**Date**: 2026-05-08  
**Scope**: Consolidate specialist recommendations into testable and measurable validation gates.

---

## Top 5 Killer Features (Consolidated)

### 1. **GraphRAG Retrieval (LLM-Ready Graph Expansion)**
**Specialists**: Architect (highest confidence), Market (1–2 week build), Performance (already benchmarkable)

**Critical-Path Test**: Hybrid search seed + 1-hop expansion returns both vector-matched node and all outgoing neighbors in a single response, with P50 latency < 50 ms on 800K-node ICIJ corpus.

**Adversarial Test**: Expansion with `limit=1000` and fan-out `10` doesn't OOM; backpressure serialization doesn't drop neighbors on slow clients.

**Killer Benchmark**: Side-by-side latency on ICIJ (graphdb LSA + expansion) vs. Weaviate (external embedding + GraphQL traversal) — same 100-node result set, context-packed. This is reproducible and public (ICIJ data is open).

**Gotcha**: If context packer doesn't respect max-tokens, LLM frameworks will truncate silently mid-edge and lose graph structure. Test with LangChain `StructuredOutputParser`.

**Effort**: S. Code seam exists in `handlers_hybrid_search.go:60–258`.

---

### 2. **Per-Tenant Property Masking + Audit Compliance API**
**Specialists**: Architect (live), Market (split OSS/Enterprise gate), Database (optional data model)

**Critical-Path Test**: Query with role `viewer` returns masked properties (`***`); same query with role `admin` returns plaintext. Audit log records both accesses with timestamp and role.

**Adversarial Test**: Attacker craft query with `COUNT(*)` aggregate on masked field — does it leak via row counts? Subquery with `UNION` — does masking boundary hold across joins?

**Killer Benchmark**: SOC2 auditor can reproduce: "Verify GDPR right-to-be-forgotten" by showing audit log entry deletion + masking policy enforcement on all read paths.

**Gotcha**: If masking applies only to REST `/nodes` endpoint but not to `/edges?from=X` traversal, compliance is illusory. Audit boundary must cover all entry points.

**Effort**: S. Enforcement code exists; effort is API surface + integration guide.

---

### 3. **Bitemporal Graph Snapshots (Time-Travel Queries)**
**Specialists**: Database (M effort, zero migration risk), Market (GDPR/fraud audit use case)

**Critical-Path Test**: Query graph at `timestamp=2026-03-15` returns only nodes/edges valid on that date; re-added nodes show correct historical properties; deleted nodes are absent.

**Adversarial Test**: Time-travel query concurrent with a mutation at the current timestamp doesn't see uncommitted changes. Snapshot consistency holds across 10M edge temporal index scans.

**Killer Benchmark**: Compliance auditor: "Show me topology on incident date" vs. Neo4j's Cypher temporal (both systems, same dataset, latency P50/P99).

**Gotcha**: If temporal index isn't ranged (valid_from, valid_to), scanning all edges for a single timestamp is O(N) and defeats the purpose at 100K+ nodes.

**Effort**: M. Index design + WAL snapshot iteration.

---

### 4. **OpenAI-Compatible `/v1/embeddings` Endpoint**
**Specialists**: API (S effort, solves embedding friction), Market (eliminates external embedding dependency)

**Critical-Path Test**: `POST /v1/embeddings` with `input=["text1", "text2"]` returns two 384-dim vectors with cosine similarity matching LSA corpus semantics; deterministic across restarts (seed=42).

**Adversarial Test**: Corpus with 1M nodes — `/v1/embeddings` request for new text (not in training corpus) should degrade gracefully (OOV handling), not crash.

**Killer Benchmark**: LangChain client configured with `baseURL="http://localhost:8080"` and `model="text-embedding-lsa"` retrieves correct top-5 neighbors at 100K corpus, P50 < 5 ms (no external API call overhead).

**Gotcha**: If LSA embeddings aren't normalized or corpus-dependent noise is high, similarity ranking breaks at scale. Benchmark against Weaviate's hybrid at 100K+ nodes.

**Effort**: S. 2–3 days. API shape is standard.

---

### 5. **Graph-Bounded k-NN (Traversal-Predicated Vector Search)**
**Specialists**: Performance (only feature beating HNSW alone), Architect (composes existing subsystems)

**Critical-Path Test**: k-NN among nodes within 2 hops of seed node, P50 < 20 ms at 500K-vector index, returning nearest 10 docs in the bounded set.

**Adversarial Test**: 2-hop expansion with fan-out 5 returns 50 candidates; brute-force dot-product over 50 vectors < HNSW entry cost. Hop radius explosion (fan-out 100, 3 hops → 1M candidates) doesn't crash — candidate set caps at `Limit * max_fan_out`.

**Killer Benchmark**: "Query: nearest docs co-authored with Alice's 2-hop network" — compare graphdb (single fused query) vs. Memgraph (two separate calls) on academic collaboration graph (10K nodes, avg degree 3).

**Gotcha**: If candidate set is pre-filtered to a label only *after* HNSW walk, the bounded optimization is lost. Candidate set must constrain HNSW entry, not post-filter.

**Effort**: M. New `SearchWithCandidates()` method on HNSWIndex + executor wiring.

---

## Validation Sequencing

**Cheapest first** (validate early, ship fast):
1. **OpenAI `/v1/embeddings`** (2–3 days, zero risk, kills embedding friction, validates LSA quality). Ships Community.
2. **GraphRAG expansion** (4–5 days, composes existing stack, immediate LLM use-case unlock). Ships Community.

**Highest risk of post-launch failure**:
1. **Bitemporal snapshots** — temporal index design is subtle; wrong range indexing makes timestamps O(N) and breaks the claim.
2. **Graph-bounded k-NN** — candidate set cardinality explosion (3-hop fan-out 100 → millions of nodes) silently kills the feature in production if not bounded.

**Compliance gate (splits OSS/Enterprise)**:
Property masking + audit API — the enforcement code is live; effort is documentation + integration proof. This is the strongest candidate for real open-core gating because the capability difference (advanced compliance exports, retention policy) is concrete and auditable.

---

## Recommended First Validation (Cheapest Path)

**Ship `/v1/embeddings` first.** It takes 2–3 days, requires zero architectural change, and immediately proves LSA quality at scale. Integrate with a public LangChain example (RAG over ICIJ corpus) and publish the latency numbers. This unblocks downstream LLM integrations and validates the deterministic embedding claim before committing to GraphRAG expansion and bitemporal snapshot designs, which have higher complexity and require indexing guarantees.
