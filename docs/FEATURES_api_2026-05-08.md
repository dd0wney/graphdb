# Killer-Feature API Analysis
**Date**: 2026-05-08  
**Scope**: Identify 3 missing REST/GraphQL surfaces that unblock real integrations.

---

## Top 3 Candidate Killer APIs

### 1. GraphQL Subscriptions over Server-Sent Events (SSE)
**Unblocks**: Real-time collaborative graph apps (Syntopica model), dashboard backends, live notification systems without WebSocket complexity.

**Current State**: `pkg/graphql/subscriptions.go` defines node/edge event types and pubsub mechanics; `pkg/pubsub/` implements in-memory fanout. GraphQL schema has no subscription root field.

**Gap**: Subscription definitions exist but are never wired to HTTP. `pkg/api/handlers_*.go` has no `/graphql/subscribe` endpoint that emits SSE.

**Code Seam**: Wire subscription resolver in `pkg/graphql/schema_subscription.go` (does not exist); add `handleGraphQLSubscriptions(w http.ResponseWriter, r *http.Request)` in `pkg/api/handlers_graphql.go:~120` that opens SSE stream and publishes pubsub events as JSON chunks.

**Effort**: M (3–4 days). SSE encoder is trivial; wiring graphql-go subscriptions requires learning its pubsub interface (~1 day), then integration testing (~1.5 days).

**Risk**: If pubsub channel delivery latency exceeds 500ms in high-throughput scenarios (100+ mutations/sec), SSE backpressure kills the feature. Bench against `/vector-search` + `/edges/batch` concurrency before shipping.

---

### 2. OpenAI-Compatible Embeddings Endpoint
**Unblocks**: Any LLM framework or observability tool that already calls `POST /v1/embeddings`. LangChain, Vercel AI SDK, etc. can drop in `baseURL: "http://localhost:8080"` without a wrapper.

**Current State**: `pkg/search/lsa.go` computes deterministic embeddings via LSA. `pkg/vector/` manages HNSW indexing. No HTTP handler maps embeddings requests.

**Gap**: LSA embeddings are buried in query execution and search indexing; no standalone `/v1/embeddings` endpoint shaped to OpenAI API (array of texts → array of [1536] float64 vectors).

**Code Seam**: Create `pkg/api/handlers_embeddings.go` with handler at `/v1/embeddings` accepting `{ "input": string | string[], "model": "text-embedding-lsa" }` and returning `{ "data": [{ "embedding": float64[], "index": int }], "model": "text-embedding-lsa", "usage": { "prompt_tokens": int, "total_tokens": int } }`. Route in `pkg/api/server_init.go` alongside `/graphql`.

**Effort**: S (2–3 days). LSA is already deterministic; API shape is standard. Integration test with LangChain client validates.

**Risk**: If LSA's 384-dim vectors are not normalized or if corpus-dependent noise makes similarity ranking unstable at scale (1M+ nodes), the endpoint becomes unusable for real embedding-based retrieval. Benchmark against Weaviate's hybrid search at 100K node corpus.

---

### 3. Change-Data Capture (CDC) Stream Endpoint
**Unblocks**: Downstream caches (Redis, Memcached), analytics pipelines (Snowflake, DuckDB), event-driven microservices, and Syntopica's mobile sync (CRDT-aware replay).

**Current State**: `pkg/replication/wal_publisher.go` streams WAL entries to replicas over NNG/ZMQ. `pkg/wal/wal.go` serializes mutations. Events are replicated internally; never exposed to clients.

**Gap**: WAL stream is a private replication protocol; no client-facing HTTP endpoint to subscribe to node/edge mutations.

**Code Seam**: Add `/cdc/stream?filter=node.created,edge.deleted&since_lsn=<uint64>&include_snapshot=true` endpoint in `pkg/api/handlers_cdc.go` that: (a) snaps the graph at `since_lsn` if requested, (b) subscribes to WAL entries after that LSN, (c) emits JSON event lines over SSE. Reuse `pkg/wal/wal_subscriber.go` pattern.

**Effort**: M (3–5 days). WAL format is stable; SSE emission is boilerplate. Per-tenant CDC filtering requires tenant context in WAL entries (~1 day).

**Risk**: If WAL retention is not enforced and clients miss entries due to ring buffer wraparound, CDC becomes unreliable for state sync. Require configurable WAL retention policy and document LSN checkpoint semantics.

---

## Endpoints Summary

| Method | Path | Purpose | Auth |
|--------|------|---------|------|
| POST   | `/graphql/subscribe` | GraphQL subscriptions (SSE) | Bearer + tenant |
| POST   | `/v1/embeddings` | OpenAI-compatible text→vector | Bearer |
| GET    | `/cdc/stream` | WAL event stream (SSE) | Bearer + tenant |

---

## Worst Pain Point: Embedding Integration Friction

**The blocker today**: To use graphdb's HNSW vector search in a Python app, users must:
1. Train a separate embedding model (MiniLM, Sentence-BERT) or call an external API (OpenAI).
2. Manage embedding lifecycle outside graphdb (versioning, staleness on corpus updates).
3. Manually sync embeddings with nodes via batch API.

**Why this matters**: Competitors (Weaviate, Pinecone, Qdrant) ship embedded inference engines or standardized OpenAI API compatibility. Graphdb has LSA built-in but hides it. An `/v1/embeddings` endpoint transforms LSA from an internal detail into a competitive API surface — users no longer need external embedding infrastructure.

**Impact**: Kills the "which embedding model?" decision friction. Unblocks LLM integrations where the app is already OpenAI-compatible (99% of agentic code post-2024).

---

## Storage Interface Blocking Analysis

**CDC Stream** requires per-tenant WAL filtering (minor, ~1 day refactor). **GraphQL Subscriptions** and **OpenAI Embeddings** compose existing search and pubsub layers — no storage interface dependency. All three are unblocked in the current architecture.

---

## Recommendation

**Priority 1: `/v1/embeddings`** (OpenAI compat). Smallest effort, highest leverage. Enables drop-in LLM framework integration. Validates LSA quality at scale (100K+ node corpus).

**Priority 2: `/cdc/stream`** (CDC). Unlocks Syntopica sync, event-driven microservices, and analytics pipelines. Medium effort; per-tenant isolation is critical before shipping.

**Priority 3: `/graphql/subscribe`** (SSE). High polish; lower business impact than 1–2 if users already have long-polling fallbacks. Ships last.
