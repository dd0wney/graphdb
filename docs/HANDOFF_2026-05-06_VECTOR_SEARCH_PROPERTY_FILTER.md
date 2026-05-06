# Handoff: Add `property_filter` to `/vector-search` HTTP API

**Date**: 2026-05-06
**Origin**: syntopica-v2 workers stack (`~/Workspace/github.com/syntopica-v2`)
**Priority**: Privacy CRITICAL — blocks Phase 2a of the Syntopica submissions-as-graph migration
**Estimated effort**: 2–4 hours including tests

---

## TL;DR

Add an optional `property_filter` field to graphdb's `POST /vector-search` request. The field carries an exact-match predicate (`map[string]any`, primitive values only) that is applied **inside the HNSW post-filter loop** alongside the existing tenant + label filters. Only nodes whose properties match every key/value in `property_filter` are returned.

This is a backwards-compatible additive change. Existing callers that don't send the field continue to work identically.

The Syntopica TypeScript client at `~/Workspace/github.com/syntopica-v2/workers/src/lib/graphdb-client.ts` already serializes this field; once your Go side accepts it, the privacy boundary closes.

---

## Why this matters

Syntopica is migrating user submissions (verification responses, commonplace book entries) to graphdb as first-class nodes (`:Submission` label) with HNSW-indexed embeddings. The migration enables cross-user similarity queries — but each submission carries an `isPublic: boolean` property, and most are private.

The security review of the architecture flagged this:

> **CRITICAL**: `/vector-search` returns `node_id` + `score` to anyone who can call it — Workers-side `isPublic` filter is not a real privacy boundary. A caller embeds target text, calls vector-search, and receives the graphdb node IDs and similarity scores of private Submission nodes. The Workers-side filter on `:SIMILAR_TO` edge targets is never consulted.

**Reference**: `~/Workspace/github.com/syntopica-v2/docs/architecture/adr/0001-submissions-as-graph.md` §"Privacy enforcement" — the architectural decision to enforce privacy server-side via this property predicate, not in Workers.

Without this change, **Phase 2a of the Syntopica migration cannot ship**. The ADR explicitly names this as launch gate #2.

---

## What to build

### 1. Extend `VectorSearchRequest` (Go struct)

**File**: `pkg/api/handlers_vectors.go` (around line 38)

Current shape:

```go
type VectorSearchRequest struct {
    PropertyName string    `json:"property_name"`
    QueryVector  []float32 `json:"query_vector"`
    K            int       `json:"k"`
    Ef           int       `json:"ef,omitempty"`
    IncludeNodes bool      `json:"include_nodes"`
    FilterLabels []string  `json:"filter_labels"`
}
```

Add:

```go
type VectorSearchRequest struct {
    // ...existing fields...

    // PropertyFilter is an exact-match predicate applied during the
    // HNSW post-filter loop. Only nodes whose properties match every
    // key/value in this map are returned. Optional — when empty or
    // nil, no property filtering is applied (existing behaviour).
    //
    // Values must be string, JSON number, or bool. Non-primitive
    // values (arrays, objects, null) are rejected at request
    // validation with 400 Bad Request.
    //
    // Like filter_labels, this is a post-filter on HNSW results, so
    // selective predicates may yield fewer than k results even when
    // the index contains many matching nodes deeper in the candidate
    // pool. If this becomes a UX problem, raise ef internally when
    // filters are present (out of scope for this change).
    //
    // Used by Syntopica to enforce per-submission privacy: the
    // Syntopica TypeScript client sends `{"isPublic": true}` for any
    // search against :Submission nodes. See
    // syntopica-v2/docs/architecture/adr/0001-submissions-as-graph.md
    // for the security rationale.
    PropertyFilter map[string]any `json:"property_filter,omitempty"`
}
```

### 2. Apply the predicate in the search handler

**File**: `pkg/api/handlers_vectors.go::vectorSearch` (the routing wrapper `handleVectorSearch` only dispatches; the actual logic lives in `vectorSearch`, around lines 232–334).

**Important type-system note before you write any code:** node properties are stored as `map[string]storage.Value`, where `storage.Value{Type ValueType; Data []byte}` is a tagged union (see `pkg/storage/types.go`). They are *not* `map[string]interface{}`. JSON-decoded primitives (`bool`, `float64`, `string`) cannot be compared directly against `Value` — you must lift the predicate into `Value` form first. The existing helper `Server.convertToValue(any) storage.Value` at `pkg/api/server_helpers.go:42` does exactly this; reuse it.

#### 2a. Validate the predicate (request validation block, before the search call)

```go
// Reject non-primitive predicate values up front. Failing closed
// here matters: convertToValue's default branch stringifies, which
// would silently produce fuzzy matches on arrays/objects — defeating
// the privacy boundary this feature exists to enforce.
for k, v := range req.PropertyFilter {
    switch v.(type) {
    case string, float64, bool:
        // ok
    default:
        s.respondError(w, http.StatusBadRequest,
            fmt.Sprintf("property_filter[%q]: only string, number, and boolean values are supported", k))
        return
    }
}
```

#### 2b. Convert the predicate to `Value` form once, before the post-filter loop

```go
var propertyPredicate map[string]storage.Value
if len(req.PropertyFilter) > 0 {
    propertyPredicate = make(map[string]storage.Value, len(req.PropertyFilter))
    for k, v := range req.PropertyFilter {
        propertyPredicate[k] = s.convertToValue(v)
    }
}
```

#### 2c. Fold the property check into the existing post-filter loop

The existing loop (currently around lines 299–326) already does one `GetNode` per hit and applies tenant + label filters against the fetched node. Add the property check there — do **not** add a second pass.

```go
for _, sr := range searchResults {
    node, err := s.graph.GetNode(sr.ID)
    if err != nil || node == nil {
        continue
    }

    if !matchesTenant(node.TenantID, tenantID) {
        continue
    }

    if len(req.FilterLabels) > 0 && !hasAnyLabel(node.Labels, req.FilterLabels) {
        continue
    }

    if len(propertyPredicate) > 0 && !matchesPropertyFilter(node.Properties, propertyPredicate) {
        continue
    }

    // ...existing result-building...
}
```

Order matters: tenant first (security boundary), label second (cheap), property third. Property and label filters AND together — a node must satisfy both to be returned.

#### 2d. Helper

```go
// matchesPropertyFilter returns true when every key in predicate is
// present in props AND the typed values are byte-identical. Both maps
// must already hold storage.Value — convert the request-side predicate
// once, before calling this.
func matchesPropertyFilter(props, predicate map[string]storage.Value) bool {
    for key, want := range predicate {
        got, ok := props[key]
        if !ok {
            return false
        }
        if got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
            return false
        }
    }
    return true
}
```

**Performance note**: zero extra fetches. The post-filter loop already runs `GetNode` once per HNSW hit for the tenant + label checks; the property check piggybacks on the same fetched node. Predicate conversion happens once before the loop, so per-node cost is at most one `bytes.Equal` per predicate key.

**Footgun in `convertToValue`**: it collapses any whole-number `float64` to `IntValue` (`server_helpers.go:48`). So a predicate `{"version": 2}` matches a stored `IntValue(2)` but **not** a stored `FloatValue(2.0)`. Booleans (Syntopica's only use case) are unambiguous, but pin this with a test if you anticipate numeric predicates downstream.

### 3. Tests

**File**: `pkg/api/handlers_vectors_test.go` (mirror the existing test structure)

Required test cases:

1. **No-op when `property_filter` absent** — existing tests should continue to pass unchanged. Confirm by running the suite.
2. **Filters out non-matching nodes** — set up two nodes with similar embeddings, one with `{"isPublic": true}` and one with `{"isPublic": false}`. Search with `property_filter: {"isPublic": true}`. Assert the private node is not in results.
3. **Empty result when filter excludes everything** — search with `property_filter: {"nonexistent_key": "value"}`. Assert `count: 0`, `results: []`. Should return 200 OK with empty results, not 500.
4. **AND semantics with `filter_labels`** — required, not optional. The AND-not-OR semantic *is* the security contract: a result must satisfy both the label filter and the property filter. A regression here is exactly the class of bug the feature exists to prevent. Set up four nodes covering each (label-match × property-match) cell of the 2×2 truth table; assert only the (true, true) cell appears in results.
5. **Bool predicate round-trips through `convertToValue`** — pins the `Value`-type comparison. Store a node with `BoolValue(true)`, send `property_filter: {"isPublic": true}` over JSON, assert it matches. This catches regressions where the conversion path silently changes (e.g., someone "simplifies" `convertToValue` and breaks bool encoding).
6. **Non-primitive predicate values rejected with 400** — send `property_filter: {"foo": ["a","b"]}`, then `{"foo": map[string]any{"nested":1}}`, then `{"foo": nil}`. Each must return 400 Bad Request, not 200 with surprising results. The 400 response should name the offending key.

---

## Wire format

The TypeScript client serializes this exact JSON. Pin the format in your tests by reading the test file from the Syntopica side:

**Source**: `~/Workspace/github.com/syntopica-v2/workers/src/tests/graphdb-client-vector.test.ts`, the test named `"forwards property_filter (the privacy-boundary contract for :Submission)"`.

The JSON payload looks like:

```json
{
  "property_name": "embedding",
  "query_vector": [0.1, 0.1, ..., 0.1],
  "k": 10,
  "filter_labels": ["Submission"],
  "property_filter": { "isPublic": true }
}
```

Property values must be `string | number | boolean`. The graphdb side enforces this with a 400 Bad Request on any non-primitive value (arrays, objects, null) — failing closed, since `convertToValue`'s default branch stringifies, which would silently produce fuzzy matches and defeat the privacy boundary. The Syntopica TS contract documents the same restriction; this is the server-side enforcement of it.

---

## Acceptance criteria

- [ ] `VectorSearchRequest` Go struct has `PropertyFilter map[string]any` with `json:"property_filter,omitempty"` tag.
- [ ] `vectorSearch` validates `property_filter` values are `string | float64 | bool` only and rejects others with 400 Bad Request, naming the offending key.
- [ ] `vectorSearch` converts the predicate to `map[string]storage.Value` once before the post-filter loop and applies the check inside the existing single-fetch loop, AFTER the tenant + label filters. No additional `GetNode` calls.
- [ ] `matchesPropertyFilter` compares `storage.Value` by `Type` equality and `bytes.Equal(Data, Data)`. No `reflect.DeepEqual` against raw `interface{}`.
- [ ] Six new tests in `handlers_vectors_test.go`:
  - existing behaviour preserved (no filter sent)
  - filter excludes non-matching nodes
  - filter that excludes everything returns `count: 0` not an error
  - filter ANDs with `filter_labels` (full 2×2 truth table)
  - bool predicate round-trips through `convertToValue` (pins the `Value`-type comparison)
  - non-primitive predicate values (arrays, objects, null) return 400, not 200
- [ ] All existing `handlers_vectors_test.go` tests still pass.
- [ ] One integration test (or manual `curl` + assertion) demonstrates the wire format from the Syntopica TS test passes through end-to-end.

When done, ping the syntopica-v2 side: the gate to start Phase 2a clears once this PR is open (doesn't have to be merged — open and reviewed is the documented criterion in ADR 0001).

---

## Out of scope (don't do these)

- **Don't touch the Syntopica workers code.** The TypeScript client side is already wired (`graphdb-client.ts:VectorSearchRequest`); Workers tests pass against a stub. Your work stays in this repo.
- **Don't redesign the HNSW indexing.** The change is purely in the search post-filter; the index itself is unaware of properties.
- **Don't add bulk operators (`$gt`, `$in`, etc.)** to the predicate. Syntopica's contract is exact-match only. If a future feature needs richer predicates, that's a follow-up — keep the surface minimal now.
- **Don't infer privacy semantics.** The predicate is a generic property-match. Syntopica happens to use it for `isPublic`, but the graphdb side has no business knowing that. Treat the filter as opaque user-supplied criteria, like the existing `filter_labels`.

---

## References

- **ADR**: `~/Workspace/github.com/syntopica-v2/docs/architecture/adr/0001-submissions-as-graph.md` §"Privacy enforcement"
- **TS contract**: `~/Workspace/github.com/syntopica-v2/workers/src/lib/graphdb-client.ts` → `VectorSearchRequest.property_filter`
- **TS test (wire format)**: `~/Workspace/github.com/syntopica-v2/workers/src/tests/graphdb-client-vector.test.ts` → `"forwards property_filter (the privacy-boundary contract for :Submission)"`
- **Security review excerpt** (pasted for self-contained context):

  > `handlers_vectors.go:300–326` returns `node_id` and `score` for every candidate that passes the tenant and label filters, regardless of `include_nodes`. A caller who can reach `/vector-search` directly does not need the SIMILAR_TO edge path: they embed target text, call vector-search, and receive the graphdb node IDs and similarity scores of private Submission nodes. The Workers-side filter on SIMILAR_TO edge targets is never consulted. Mitigation: ensure `is_public` is enforced as a property predicate inside the HNSW post-filter loop — moving the privacy boundary into graphdb where it belongs, so it cannot be bypassed by a future refactor.

---

## Coordination

Once your PR is open, leave a comment in the syntopica-v2 ADR file (`docs/architecture/adr/0001-submissions-as-graph.md` §"Operational gates") noting the PR URL. The Syntopica side has a checkbox there waiting for this:

```markdown
- [ ] graphdb-side property-predicate filter for `/vector-search` confirmed (PR open in graphdb repo, even if not yet merged).
```

That's the signal the Phase 2a write path can start landing in workers.

If you discover the existing handler structure is materially different from what this brief describes (e.g., the post-filter loop has been refactored since 2026-05-06), proceed with the equivalent change at the right point in the current code — the contract (`property_filter` is honoured server-side) is what matters, not the specific lines named.

---

## Revision history

- **2026-05-06**: Initial handoff.
- **2026-05-06 (patch)**: Corrected after a code review against the actual repo state. Original draft assumed `node.Properties` was `map[string]interface{}` and proposed a `reflect.DeepEqual`-based helper plus a separate post-filter pass; both were wrong. Properties are `map[string]storage.Value` (tagged union), so the helper now compares `Type` + `bytes.Equal(Data, Data)` against a predicate pre-converted via `Server.convertToValue`. The implementation now folds into the existing single-fetch loop instead of adding a second pass (zero extra `GetNode` calls). Added 400-on-non-primitive request validation, promoted the AND-with-labels test to required, and added bool round-trip and rejection tests. Field type changed from `map[string]interface{}` to `map[string]any`.
