# Design: REST-exposed FTS + hybrid search

**Status:** Proposal
**Target version:** v0.3.0 (v1) + v0.4.0 (v2)
**Audience:** An implementer agent. Assume you have full repo access and no
prior context from the session that wrote this doc.

---

## TL;DR

`pkg/search/fulltext_*.go` already implements a working in-memory inverted
index with TF-IDF scoring — but it's only reachable via GraphQL and the
internal Cypher-like query function layer. The REST server has a
`/vector-search` endpoint but no `/search` endpoint. Both of graphdb's
current downstream consumers (Ulysses and Syntopica) need literal
full-text search over REST. Adding it is a small, well-scoped piece
of work. Hybrid search (FTS + vector merged) is the real differentiator
and lands in a follow-up.

**v1 (one PR):** Expose the existing `FullTextIndex` as `POST /search`.
Tenant / namespace isolation. No new algorithms.

**v2 (one PR):** Add `POST /hybrid-search` that runs FTS + vector search
in parallel and merges via Reciprocal Rank Fusion. Uses the existing
`/vector-search` embedding path internally.

---

## Existing state

### What's already there (read these first)

- `pkg/search/fulltext_index.go` — inverted index construction, per-node
  content storage, tokenization pipeline
- `pkg/search/fulltext_search.go` — TF-IDF query evaluation, ranked
  results with `SearchResult { NodeID, Score, Node }`
- `pkg/search/fulltext_types.go` — `FullTextIndex` struct; config
  includes `indexedLabels` + `indexedProps` so not every property
  is indexed by default
- `pkg/search/fulltext_test.go` — unit tests for indexer and query
- `pkg/graphql/schema_search.go` — how GraphQL exposes the same
  index; useful reference for argument/response shape
- `pkg/query/functions_search.go` — `search()` function inside the
  query DSL; another reference implementation

### What's missing

- No REST endpoint. `grep -rn '"/search"'` on `pkg/api/` returns zero
  hits. Clients on HTTP can only do vector search today.
- No hybrid scoring. FTS results and vector results live in parallel
  worlds with no merge primitive.
- No per-tenant index isolation plan beyond the general tenant
  middleware (see `pkg/api/middleware_tenant.go`) — needs verification.

### Existing patterns to copy

Look at `pkg/api/handlers_vectors.go` + its route registration in
`pkg/api/server.go` line 61:

```go
mux.HandleFunc("/vector-search", s.requireAuth(s.handleVectorSearch))
```

The FTS handler follows the exact same shape. Tests mirror
`handlers_vectors_test.go`.

---

## v1: REST expose the existing FTS

### API contract

```
POST /search
Authorization: Bearer <token>
Content-Type: application/json

{
  "query": "metempsychosis",
  "limit": 20,
  "offset": 0,
  "labels": ["Scene", "Note"],          // optional — restrict to these node labels
  "properties": ["title", "body"],      // optional — restrict to these property keys
  "include_content": false              // optional — whether to return the matched text alongside the node
}
```

**Response 200:**

```json
{
  "results": [
    {
      "node_id": 12345,
      "score": 7.42,
      "snippet": "…the word metempsychosis he'd heard from the bookseller…",
      "node": {
        "id": 12345,
        "labels": ["Scene"],
        "properties": { "title": "The Shaving Bowl", "chapter": "I" }
      }
    }
  ],
  "total": 17,
  "took_ms": 3
}
```

**Response 400** on invalid input, **401** on auth failure, **500** on
internal error — all matching existing `writeJSONError` shape in
`pkg/api/handler_helper.go`.

### Files to add / modify

1. **New:** `pkg/api/handlers_search.go`
   - `handleSearch(w http.ResponseWriter, r *http.Request)` — method =
     POST only; anything else returns 405. Parse body via
     `decodeJSON` helper. Call the tenant-scoped `FullTextIndex` via
     the Server's storage accessor. Marshal response.
   - `searchRequest` + `searchResult` + `searchResponse` structs next
     to the handler. Keep them unexported.

2. **New:** `pkg/api/handlers_search_test.go`
   - Copy the test table pattern from `handlers_vectors_test.go`
   - Cases: empty query (400), non-POST method (405), happy path with
     mock index, label filter applied, pagination (limit + offset),
     unauthenticated (401), exceeds max limit (clamp or 400 —
     decide, pick one, document)
   - Use `httptest.NewRequest` + `httptest.NewRecorder`
   - Minimum 8 test cases, each with a clear `name` + `want` block

3. **Modify:** `pkg/api/server.go`
   - Add one line after the existing `/vector-search` registration
     (line 61):

     ```go
     mux.HandleFunc("/search", s.requireAuth(s.handleSearch))
     ```

4. **Modify:** `pkg/api/server.go` Server struct
   - If it doesn't already have a `searchIndex` or equivalent field,
     add a `getSearchIndex(tenantID string) *search.FullTextIndex`
     accessor on Server. Follow the pattern whatever
     `handleVectorSearch` uses to reach its index — look there first
     rather than inventing a new wire-up.

5. **Modify:** `api/proto/graphdb.proto` (if gRPC parity matters)
   - Add a `Search` RPC. gRPC handler mirrors REST. If Ulysses /
     Syntopica don't use gRPC, defer this to a follow-up.

6. **Modify:** `pkg/api/openapi.yaml` (or equivalent)
   - Document the new endpoint. Response shape must match `/vector-search`
     style for consistency (both should expose `took_ms`, both should
     return an object with a `results` array, both should paginate).

### Per-tenant isolation

**Non-negotiable.** The FTS index must be per-tenant or per-namespace,
not a global singleton. If the current `FullTextIndex` is global,
wrap it so the tenant middleware can select the right one based on
`request.Context()`.

Read `pkg/api/middleware_tenant.go` to understand how tenant context
flows. Search + vector search must both honor it identically.

### Snippet generation

`FullTextIndex` already stores `nodeContent` — use it. Snippet window:
- Find first occurrence of any query term in the content
- Window: 80 chars either side, UTF-8 char-boundary safe (`utf8.RuneStart`)
- `…` prefix if `start > 0`, `…` suffix if `end < len(content)`
- Match ranked by score, not by snippet position

If content isn't stored (config says no `include_content`), snippet
is an empty string. Return empty string, not null; client callers
don't want to null-check.

### Limits + validation

- `limit`: default 20, max 100. Clamp silently; log a warning above 100.
- `offset`: default 0, max = total. Values > total return empty results,
  not an error.
- `query`: trim whitespace. Empty after trim → 400 with
  `{"error": "query must be non-empty"}`.
- `labels` / `properties`: arrays of strings, optional. Empty arrays
  mean "no restriction" (same as omitted).

### Acceptance criteria

- [ ] `POST /search` returns a ranked result for a query against an
      indexed corpus
- [ ] All 8+ test cases pass: `go test ./pkg/api/ -run TestSearch`
- [ ] `gofmt` + `go vet ./...` clean
- [ ] `grep -rn "/search\"" pkg/api/` shows exactly one registration +
      corresponding tests
- [ ] Benchmark added: `cmd/benchmark-search/` mirroring existing
      `cmd/benchmark-query` shape. Target: <10ms p99 for 100k docs,
      single-term query, warm index
- [ ] OpenAPI spec updated + rendered matches actual responses
- [ ] Per-tenant isolation verified with a test that inserts into
      tenant A, searches as tenant B, gets zero results
- [ ] CHANGELOG.md entry under v0.3.0

---

## v2: Hybrid search

### Motivation

FTS finds literal matches. Vector search finds semantic matches. Either
alone misses half the signal. Syntopica's syntopical-reading use case
especially wants both: "passages about attention in Nietzsche" should
surface the word `attention` in context AND concept-adjacent passages
that don't use the literal word.

### API contract

```
POST /hybrid-search
Authorization: Bearer <token>
Content-Type: application/json

{
  "query": "attention as mental discipline",
  "limit": 20,
  "labels": ["Book", "Note"],
  "fts_weight": 0.6,                   // optional — default 0.5
  "vector_weight": 0.4,                // optional — default 0.5
  "vector_index": "node_embeddings",   // which vector index to use
  "embedding_model": "nomic-embed"     // optional — for the query embedding
}
```

Server-side, graphdb:
1. Takes the query string
2. Runs FTS internally (reusing v1 codepath) → top 3×`limit` results
3. Embeds the query (via the same provider config the existing embedding
   pipeline uses) and runs vector search → top 3×`limit` results
4. Merges via **Reciprocal Rank Fusion** (RRF):
   - `score(doc) = Σ (weight_i / (k + rank_i(doc)))`
   - Default `k = 60` (RRF literature standard)
   - Each source contributes proportional to its configured weight
5. Returns the merged top `limit`

### Why RRF over weighted sum

Raw FTS scores and raw vector similarities aren't on the same scale.
Weighted sum of `tfidf + cosine_similarity` is nonsense unless you
normalize, and normalization is brittle (a query with 1 great match
vs. 100 okay matches looks different post-normalization). RRF uses
only rank positions, which is scale-invariant. See Cormack, Clarke,
Buettcher 2009 for the original paper.

### Files to add / modify

1. **New:** `pkg/search/hybrid.go`
   - `HybridSearch(query string, ftsOpts FTSOpts, vecOpts VectorOpts, merge MergeOpts) ([]Result, error)`
   - Runs both paths via `errgroup.Group` for parallelism
   - Applies RRF
   - Returns the unified result shape

2. **New:** `pkg/search/hybrid_test.go`
   - Property test: merged rank of a doc that's #1 in both sources >
     merged rank of a doc that's #1 in only one
   - Property test: weight=1.0/0.0 degenerates to pure-FTS
   - Empty vector results → falls back to pure FTS (graphdb up but
     index missing) without error
   - Empty FTS results → pure vector

3. **New:** `pkg/api/handlers_hybrid_search.go` + test file
   - Same shape as `handlers_search.go`; handler mostly reads request,
     calls `search.HybridSearch`, marshals response

4. **Modify:** `pkg/api/server.go`
   - `mux.HandleFunc("/hybrid-search", s.requireAuth(s.handleHybridSearch))`

5. **Modify:** OpenAPI spec

### Acceptance criteria

- [ ] `POST /hybrid-search` returns merged results where every result
      has `ftsRank` + `vectorRank` fields so clients can see why it
      ranked
- [ ] Two sources run in parallel (measurable — total latency ≈ max
      of the two, not sum)
- [ ] RRF correctly combines ranks; unit tests cover the edge cases
      above
- [ ] Graceful degradation: vector index missing → returns FTS-only
      results with a warning header `X-GraphDB-Hybrid-Degraded: no-vector-index`
- [ ] Benchmark: 50k-document corpus, single query, vector + FTS both
      populated — <50ms p99

---

## Non-goals (explicit)

Do NOT do any of these in this feature:

- **Typo tolerance / fuzzy matching.** Good feature; separate design.
- **Phrase queries** (`"exact phrase"`). Worth adding but belongs in
  its own PR so the API shape gets its own review.
- **Faceting / aggregations.** Meilisearch sells these hard; we'd need
  them for Syntopica eventually. Not now.
- **Re-indexing on the fly.** Keep the existing write-time index
  pipeline; don't add background reindex jobs.
- **Bleve migration.** The existing `pkg/search/` inverted index is fine
  at current scale. If benchmarks show it falls apart past 1M docs,
  Bleve becomes a follow-up. Don't preemptively rewrite.
- **Multi-language analyzers.** English stemming/tokenization only.
  Add language config when a customer actually has non-English content.
- **Highlights beyond the snippet.** Single snippet per result. If
  multiple matches per doc, pick the best-scoring one; don't return
  all of them.

---

## Downstream impact

**Ulysses** (`~/Workspace/github.com/ulysses`):
- Today: `src-tauri/src/api/search.rs::api_search_active_project` walks
  `<project>/documents/` on every call. Works at ≤500 docs, slow past
  that.
- After v1: Rust client (`src-tauri/src/graphdb/client.rs`) adds
  `fn search(&self, SearchRequest) -> Result<SearchResult>`; the Tauri
  command becomes a thin wrapper. Local scan stays as a fallback when
  graphdb sidecar is down (100ms health-check gate).
- See `docs/SEARCH.md` in the Ulysses repo for the current search
  architecture this displaces.

**Syntopica** (`~/Workspace/github.com/syntopica-v2`):
- Commonplace book + syntopical-reading features both benefit.
  Hybrid search especially — "passages about X across multiple books"
  is exactly what syntopical reading needs.
- Coordinate with Syntopica maintainers on when to upgrade the
  graphdb client version and what backward compatibility they need
  during rollout.

---

## Execution hints for the implementing agent

- **Read before writing.** `pkg/search/fulltext_*.go`, `pkg/api/server.go`,
  `pkg/api/handlers_vectors.go` + its test, `pkg/api/middleware_tenant.go`.
  Budget 30-45 minutes for this before you start changing code.
- **One PR per version.** v1 and v2 should not land in the same PR.
  Each is reviewable in isolation.
- **Follow the vector handler's test shape exactly.** Consistent test
  structure makes PR review fast; every reviewer knows where to look.
- **If tenant isolation is uncertain,** write the isolation test FIRST
  (red → green). Cheaper than finding out at review that you missed a
  crate boundary.
- **Don't invent.** Every design decision here has a reason. If you
  want to deviate (use Bleve, use weighted-sum instead of RRF, expose
  a different endpoint shape), write a comment in the PR description
  explaining why — don't just do it.
- **Benchmarks are acceptance criteria, not optional.** Numbers in
  the PR description, reproducible via the added `cmd/benchmark-search/`.

When in doubt, prefer the smaller / more conservative version of a
decision. A narrower v1 that ships cleanly beats a broader one that
sits in review.
