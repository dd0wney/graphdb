# S11 Design Spike — Auto-Embedder + NodeObserver Redesign

**Status:** spike output, not implementation. Date: 2026-05-13.
**Predecessors:** S1 narrowed-landing (PR #145); audit verdict matrix
(`docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md`, S11c row).
**Goal:** close the three open R2 questions from `docs/NEXT_STEPS_2026-05-13.md`:
which default embedder ships, how `AddObserver` re-attaches to the S1 interface,
and what the observer contract must guarantee beyond the archive's sketch.

---

## 1. Problem Statement

### 1.1 The mock embedding (archive `pkg/intelligence/embedder.go:119-122`)

The archive's embedder, produced by the Gemini bulk session and captured at
`origin/archive/gemini-bulk-2026-05-13^3:pkg/intelligence/embedder.go`, contains
the following at lines 119–122:

```go
// Mock embedding for spike
// In real life, call external embedding API
embedding := mockEmbedding(content)
```

Where `mockEmbedding` is defined at the bottom of the same file:

```go
func mockEmbedding(content string) []float32 {
    // Simple deterministic mock based on string length and first char
    h := float32(len(content)) / 100.0
    if len(content) > 0 {
        h += float32(content[0]) / 255.0
    }
    return []float32{h, h * 0.5, h * 0.25}
}
```

This produces a **3-element vector** derived entirely from the string's byte length and
first ASCII value. Two different strings with the same length and the same first
character produce identical vectors. There is no semantic content in these values.
The self-comment acknowledges this: `// Mock embedding for spike`.

If the archive were merged as-is, every `CreateNode` that matched an embedding
policy would silently write a 3-float fake into the node's vector property. Vector
search results built on top of these values would be nonsense, with no user-visible
signal that anything was wrong.

### 1.2 The LLM mock fallback (archive `pkg/intelligence/llm.go:28`)

The archive's `LLMClient.Generate` method, at line 28, silently returns fabricated
text instead of erroring when `APIKey == ""`:

```go
if c.APIKey == "" {
    // Mock response for development/testing if no API key is provided
    return fmt.Sprintf("[MOCK RESPONSE for model %s]: You asked: %s", c.Model, prompt), nil
}
```

This returns `nil` error with a fake string body. A caller with no API key configured
receives an apparently-successful response that is entirely fabricated. Logging or
downstream storage of this output would contaminate production data.

### 1.3 Facade risk

Both patterns share the same failure mode: **misconfiguration produces a successful
response with fake data rather than an error.** Landing either pattern in a production
database means users can reasonably trust data that was silently invented. The S11
redesign must structurally prevent this: any path that cannot produce a real result
must return an error that the caller can surface.

---

## 2. Design Constraints

The following constraints are non-negotiable for any design that lands on main:

1. **Real embeddings.** Embedding output must reflect the semantic content of the
   input. A known input must produce a known, deterministic output that a test can
   pin. A 3-float output derived from string length is not acceptable.

2. **Non-blocking hot path.** `CreateNode*` must not wait for embedding computation.
   The observer notification and embedding write must happen asynchronously. The
   `CreateNode*` call-site latency should be unchanged relative to its baseline without
   any observer registered.

3. **Bounded async path.** The archive dispatches a bare `go func(...)` per node with
   no concurrency limit, no backpressure, no shutdown drain, and no test handle. The
   redesign must use a bounded worker pool with a configurable queue depth, a shutdown
   drain (block until the queue is empty or a timeout elapses), and a synchronous test
   mode that allows deterministic verification.

4. **Tenant isolation end-to-end.** The observer receives a node with its `TenantID`
   populated. Any storage write the observer performs must use `UpdateNodeForTenant`
   (or equivalent `*ForTenant` variant), never a tenant-blind method. The embedding
   backend must not mix corpus or configuration across tenants.

5. **Misconfiguration must error.** If the embedder is registered but not properly
   configured (missing API key, missing corpus index, wrong dimensions), the embedder
   returns an error. The error is logged at the observer dispatch layer. It is never
   silenced and never replaced with mock data.

6. **Observer loop prevention.** The embedder's `UpdateNodeForTenant` call is itself
   a node mutation. If observer notification runs on update paths, this can produce an
   infinite loop. The design must specify a mechanism that prevents this: either
   restrict notification to `CreateNode*` only (archive approach), or use a sentinel
   context value that the observer checks before dispatching.

7. **Observer interface is general-purpose.** The `NodeObserver` interface and the
   `AddObserver` attachment point must be useful beyond auto-embedding — audit hooks,
   change-data-capture, and invalidation side effects are plausible future consumers.
   The interface must not couple to the embedder domain.

---

## 3. Option A: Pluggable Interface, No Default

**Shape:** define a `NodeObserver` interface and `AddObserver(NodeObserver)` on
`GraphStorage`. Ship no implementation. The operator is responsible for registering
an embedder at startup if they want auto-embedding.

**Out-of-box experience:** zero auto-embedding. A fresh deployment produces no
vector properties unless the operator explicitly builds and registers an embedder.
This is intentional — there is no embedder that works correctly with zero
configuration.

**Pros:**
- No surprise defaults. An operator who has not read the embedder documentation
  cannot accidentally write fake or malformed vectors.
- No cold-start problem. The observer pool starts empty; adding an observer later
  is a restart-required config change, not a data consistency risk.
- The interface is genuinely general: the first consumer could be an audit hook
  before an embedder is ever registered.

**Cons:**
- "Ships out-of-box" claim is false. A new user sees no auto-embedding until they
  explicitly configure and wire an implementation.
- Documentation burden is higher: the operator must know what to register.

---

## 4. Option B: LSA as Default

**Shape:** wire `pkg/search.LSAIndex` (via the per-tenant `TenantLSAIndexes`
registry) as the default embedder. On `CreateNode*`, if the tenant has a built
LSA index, extract the `SourceProperty` text and call `FoldQuery`.

`FoldQuery`'s actual signature (`pkg/search/lsa.go:406`):

```go
func (i *LSAIndex) FoldQuery(query string) (vec []float32, tokens []string, err error)
```

`FoldQuery` projects a query string into the LSA's latent space and returns a
200-dimensional `[]float32` (at `DefaultLSAConfig().Dims = 200`). This is a real,
deterministic, non-mock embedding. The same input against the same corpus always
produces the same output.

**LSA cold-start problem.** `TenantLSAIndexes.Get(tenantID)` returns `nil` for a
tenant that has not yet built an index (see `pkg/search/tenant_lsa_indexes.go:33`:
`// Get returns the LSA index registered for tenantID, or nil if none has been
registered`). This is the same condition that causes `/v1/embeddings` to return 503.
For Option B, three sub-choices exist:

- **B-skip:** if `Get(tenantID) == nil`, skip embedding and log. Silences the
  absence — a new tenant never sees auto-embeddings until an admin builds, with
  no visible error.
- **B-error:** if `Get(tenantID) == nil`, the observer returns an error. Logged at
  the dispatch layer. CreateNode still succeeds (async path); the node is created
  without a vector property. Visible in logs.
- **B-metric:** if `Get(tenantID) == nil`, log and increment a counter. Same as
  B-skip but observable via metrics.

F1.1 §3.G2 already ruled: "manual trigger is the right default for an LSA build;
auto-trigger surprises are worse than 503 with a clear message." The same logic
applies here. B-skip silently degrades; B-error surfaces the missing index clearly.

**Scale ceiling (from F1.1 §4):** per-tenant LSA scales to approximately 100K–500K
documents at 200 dimensions. Above that ceiling, `FoldQuery` returns out-of-vocabulary
errors as the corpus-representative vocabulary saturates. The S11 embedder must
propagate these errors rather than silently truncating or substituting.

**Corpus assumption.** LSA requires a pre-built index. The index is built from
tenant-scoped corpus data via the admin endpoint (`POST /hybrid-search/lsa-index`).
The S11 embedder cannot build or rebuild this corpus — it can only consume it. This
means embedding quality is entirely dependent on when the operator last built the
index. A tenant with a stale index gets stale embeddings; there is no drift detection.

**Dimension mismatch.** If the tenant builds their LSA index at a non-default
`Dims` (e.g. 100 instead of 200), then the auto-embedder produces 100-dimensional
vectors. The HNSW vector index, if configured for 200 dimensions, will reject these
writes. The observer must check dimension compatibility before dispatching, or
accept that the write error surfaces in the async log.

**Pros:**
- Deterministic, in-tree, already per-tenant, already tested.
- No external API dependency.
- No API key management.

**Cons:**
- Requires a pre-built per-tenant corpus. Fresh tenants see no embeddings.
- Per-tenant scale ceiling (~100K–500K docs) is hard-coded into the LSA algorithm.
- Dimension mismatch between index and stored vectors is a latent misconfiguration.
- "Default that ships out-of-box" is marketing language: it works only after the
  operator has explicitly built a corpus, which is not a zero-configuration action.

---

## 5. Option C: External API (`/v1/embeddings`-compatible) as Default

**Shape:** call the existing OpenAI-shape `/v1/embeddings` endpoint (or a configured
remote equivalent) during the async observer dispatch.

The primary upside is embedding-space consistency between the read path (query
embeddings via `/v1/embeddings`) and the write path (auto-embedder). A vector search
query and the documents it retrieves would have been embedded by the same model.
The primary downsides are: a network dependency inside the async pool (remote API
outages back up the queue); API key management with the same misconfiguration-silences
risk that `pkg/intelligence/llm.go:28` demonstrated; and a circular-loopback problem
if the server calls its own `/v1/embeddings` before it is ready to serve. Option C
is deferred. The `Embedder` interface in §7.1 accommodates a future
`RemoteAPIEmbedder` without any interface changes.

---

## 6. Recommendation

**Recommendation: Option A (pluggable interface, no default) + ship an in-tree
`LSAEmbedder` adapter as the canonical first-party implementation.**

**Rationale:**

Option B collapses to Option A with one adapter registered: "LSA as default" is not
an out-of-box experience — it requires the operator to build a per-tenant corpus
first. The only difference between "no default" and "LSA default" is whether
embedding silently skips (B-skip) or logs an error (B-error) for tenants without
an index. B-error is the correct behavior. But B-error is indistinguishable from
a pluggable embedder that returns an error when not configured. The architectural
question resolves to whether the option lives inside the binary or outside it.

Keeping the default as "no embedder registered" aligns with the established
pattern: `/v1/embeddings` returns 503 when no LSA is built; `CreateNode*` should
not silently skip auto-embedding either. Both paths require explicit operator action
before they produce results.

The `LSAEmbedder` adapter ships in-tree (probably `pkg/intelligence/lsa_embedder.go`
or `pkg/search/lsa_embedder.go`) as the first `NodeObserver` implementation. Operators
wire it at startup if they want LSA-backed auto-embedding. Its behavior when
`TenantLSAIndexes.Get(tenantID)` returns `nil` is to return a typed error:

```
ErrNoEmbedderForTenant{TenantID: tenantID}
```

Logged at the dispatch layer. The node write is not rolled back — the node is created
without a vector property, and the error is surfaced in logs and metrics.

Option C is deferred. The read-path/write-path consistency argument is real, but
the circular-loopback problem and key-management surface are higher-risk for an
initial design. Option C becomes attractive if a future operator deploys an external
embedding service and wants the full consistency guarantee — at that point, they
register a `RemoteAPIEmbedder` implementation of the same `NodeObserver` interface.

**Missing constraint.** If the product requires a zero-configuration auto-embedding
experience — create a node, get a vector, no admin steps — none of the three options
satisfy it. That would require a bundled embedding model (ONNX runtime or similar)
that doesn't exist in this codebase. This spike cannot close that requirement;
it defers it to a future track once external embedding model packaging is scoped.

---

## 7. Final Interface Signatures

### 7.1 `Embedder` interface

```go
// Embedder computes a dense vector embedding for a text input, scoped to a
// specific tenant. The tenant scoping is the caller's responsibility — the
// embedder may use it to select a per-tenant model, corpus, or configuration.
//
// Embed must return a non-nil error if it cannot produce a real embedding.
// It must never return a mock or placeholder vector alongside a nil error.
// The returned slice length must be consistent with the embedding space
// the caller has configured (e.g., the HNSW index dimensions).
type Embedder interface {
    Embed(ctx context.Context, tenantID string, text string) ([]float32, error)
}
```

Error shape for the LSA adapter when the tenant index is absent:

```go
// ErrNoIndexForTenant is returned by LSAEmbedder when the target tenant
// has no built LSA index. It is NOT a transient error — retrying will
// return the same result until an admin builds the index.
type ErrNoIndexForTenant struct {
    TenantID string
}

func (e ErrNoIndexForTenant) Error() string {
    return fmt.Sprintf("embedder: no LSA index for tenant %q — build via POST /hybrid-search/lsa-index", e.TenantID)
}
```

### 7.2 `NodeObserver` interface

```go
// NodeObserver is notified after node mutations commit. Implementations
// must be safe for concurrent calls from multiple goroutines.
//
// OnNodeCreated is called after CreateNode* completes and shard locks are
// released. The notification happens on the creation goroutine; implementations
// that do I/O or slow work must dispatch to their own pool and return promptly.
//
// OnNodeUpdated is called after UpdateNode* completes. oldNode is a snapshot
// of the node before the update; it is safe to read without locking.
// Implementations that themselves call UpdateNodeForTenant MUST avoid
// re-triggering notification. The canonical guard is an internal sentinel
// property key (unexported, prefixed with a zero-byte or similar) or a
// context value set before the internal write.
//
// OnNodeDeleted is called after DeleteNode* completes. The node's data is
// no longer accessible; only nodeID and tenantID are provided.
//
// None of these methods receive an error return. Errors encountered by the
// observer must be handled internally (logged, metered, written to a dead-letter
// queue). A panicking observer will crash the server — implementations must
// recover from panics or be verified panic-free.
type NodeObserver interface {
    OnNodeCreated(ctx context.Context, node *Node)
    OnNodeUpdated(ctx context.Context, node *Node, oldNode *Node)
    OnNodeDeleted(ctx context.Context, nodeID uint64, tenantID string)
}
```

**Why `ctx`:** the archive's `OnNodeCreated(node *Node)` has no context. Context is
needed for cancellation during shutdown (drain the pool; in-flight work can check
`ctx.Done()`), tracing (propagate the span from the write), and timeout enforcement.
The context passed to the observer is derived from the storage call's context, with
a deadline imposed by the observer dispatch layer.

**Why no error return from `OnNodeCreated`:** the storage layer cannot meaningfully
act on an observer error after the write has committed. The contract is fire-and-
observe, not fire-and-verify. Errors are the observer's problem.

### 7.3 `AddObserver` method

```go
// AddObserver registers obs to receive node lifecycle events. Observers are
// called in registration order. AddObserver is safe to call concurrently with
// storage operations; the observer slice is snapshot-copied under gs.mu.RLock
// before dispatch, so the lock is released before any observer code runs.
//
// AddObserver does NOT add AddObserver to the Storage interface until R3. It
// is a concrete method on *GraphStorage that external wiring code (e.g.,
// pkg/api/server_init.go) calls at startup before serving requests. See
// docs/NEXT_STEPS_2026-05-13.md §R3 for the S1 surface re-closure plan.
func (gs *GraphStorage) AddObserver(obs NodeObserver)
```

The method is not on the `Storage` interface yet. That gap closes in R3 after
R1 (F4 vector methods) also lands. Wiring code in `server_init.go` calls it via
the concrete `*GraphStorage` type, same as the existing `BuildLSAIndex` wiring at
`pkg/api/server_init.go:333-378`.

### 7.4 Notification placement (lock discipline)

The notification calls — `notifyNodeCreated`, `notifyNodeUpdated`, `notifyNodeDeleted`
— must run **after all shard and global mutex locks are released**, not inside them.
The archive holds `gs.mu.RLock` over the observer slice copy (which is correct), but
the actual dispatch to `o.OnNodeCreated(node)` happens under that read lock. This
means a slow observer blocks all concurrent readers.

The correct placement:

1. Complete the mutation (acquire write lock, update shard map, release).
2. Copy the observer slice under `gs.mu.RLock`, then release the read lock.
3. Dispatch to each observer outside any lock.

The embedder's `processNode` goroutine (`go func(...)`) must not be launched while
holding any shard lock. Enforcement: the `notify*` helpers are only called from the
storage method's final line, after all deferred unlocks run.

### 7.5 Async dispatch shape

The archive launches one goroutine per node per matching policy. This is unbounded.
The redesigned embedder uses a bounded pool:

```
EmbedderWorker{
    queue    chan embedTask        // bounded; configurable depth (default 256)
    pool     [N]goroutine         // N = configurable concurrency (default 4)
    shutdown chan struct{}
    wg       sync.WaitGroup
}
```

- `embedTask` carries `ctx`, `node`, `policy`, `embedder`.
- On enqueue: if the queue is full, the task is dropped and a metric is incremented.
  Dropping on back-pressure is preferable to blocking `CreateNode`.
- On shutdown (called from `GraphStorage.Close`): close the queue, drain in-flight
  tasks within a 5-second deadline, then return. If the deadline expires, log the
  count of unfinished tasks and return anyway.
- Test mode: a constructor option `WithSynchronousDispatch(true)` makes `OnNodeCreated`
  block until the embedding write completes. Used in tests only; never set in
  production code.

---

## 8. Test Plan

### T1. Real-input → known-embedding round-trip

**Premise:** `LSAEmbedder.Embed` against a known corpus produces a pinned output.

**Setup:** build an `LSAIndex` from a small deterministic corpus (3 documents, seeded
with `DefaultLSAConfig().Seed = 42`). Call `Embed(ctx, tenantID, "graph database")`.
Assert the returned `[]float32` has length 200 (the default `Dims`) and that the
first five values match a pre-computed golden fixture.

This test prevents any change to the LSA integration path from silently regressing
the embedding space.

### T2. Mock-fallback detection

**Premise:** a correctly-wired embedder never returns the archive's mock output.

**Structural pin:** `mockEmbedding` always returns a 3-element `[]float32` regardless
of input. The redesigned `LSAEmbedder` uses `DefaultLSAConfig().Dims = 200`. Assert
that `LSAEmbedder.Embed` for any in-vocabulary input returns a slice of length 200,
not 3. A 3-element result is definitional proof that the mock formula leaked back in.

**Value pin (belt and suspenders):** for completeness, document the archive's formula:
`h := float32(len("hi"))/100 + float32('h')/255 ≈ 0.4278`; result `[0.4278, 0.2139,
0.1070]`. The test may assert `len(vec) != 3` rather than pin specific values, since
the specific float values of the real LSA output are covered by T1.

### T3. Misconfiguration surfaces as error

**Premise:** an embedder with no LSA index returns `ErrNoIndexForTenant`, not fake data.

**Setup:** construct an `LSAEmbedder` with an empty `TenantLSAIndexes` registry (no
`Set` called). Call `Embed(ctx, "acme", "some text")`. Assert that the returned error
is of type `ErrNoIndexForTenant` with `TenantID == "acme"`. Assert the returned
`[]float32` is nil. No panic.

### T4. Tenant isolation regression (mirrors `TestEmbeddings_TenantIsolation`)

**Premise:** tenant-A's embedding space does not contaminate tenant-B's.

**Setup:** build two `LSAIndex` instances from disjoint corpora. Register them for
`"tenant-a"` and `"tenant-b"` in a shared `TenantLSAIndexes`. Assert that the vocab
keysets are disjoint (a term present only in tenant-A's corpus does not appear in
tenant-B's index's `vocab` map). Assert that `Embed(ctx, "tenant-a", commonText) !=
Embed(ctx, "tenant-b", commonText)` when the two corpora treat that text differently.

### T5. Non-blocking semantics

**Premise:** `CreateNode*` latency is unchanged with an observer registered.

**Setup:** register an observer backed by a deliberate 50ms sleep (`SlowObserver`).
Benchmark `CreateNode*` for 1000 nodes with and without the observer. Assert that
the p99 latency with the observer registered is within 5% of the baseline (because
dispatch is async; the 50ms sleep happens in the worker pool, not on the create path).

### T6. Shutdown drain

**Premise:** `Close` waits for in-flight embed tasks to complete.

**Setup (synchronous mode):** register a `CountingEmbedder` and create 10 nodes.
Call `Close`. Assert the counter equals 10. No data races under `-race`.

### T7. Backpressure drop — queue full, no block

**Premise:** a full task queue causes drops, not blocking.

**Setup:** construct a pool with `queueDepth=1` and `WithSynchronousDispatch(false)`.
Lock the pool's internal goroutines so they cannot dequeue. Enqueue 10 tasks. Assert
that the enqueue calls return promptly (under 1ms each). Assert the dropped-task
counter is 9 (one slot consumed, nine dropped).

---

## 9. PR Breakdown

### PR R2.1 — `NodeObserver` interface + `AddObserver` on `*GraphStorage`

**Goal:** land the notification infrastructure without any embedder implementation.

**Files:**
- `pkg/storage/observation.go` (new): `NodeObserver` interface, `notify*` helpers,
  lock-discipline comment, `NodeObservers` slice type.
- `pkg/storage/storage.go`: add `observers []NodeObserver` field to `GraphStorage`;
  add `AddObserver` method; wire `notifyNodeCreated` at the end of
  `CreateNodeWithTenant` and `CreateNodeWithUniquePropertyForTenant`.

**Acceptance:** `AddObserver` compiles; a no-op observer registered at startup
produces no behavior change in any existing test; race detector clean; golangci-lint
clean.

**Deliberately excluded:** `AddObserver` is NOT added to the `Storage` interface.
That is R3's job.

### PR R2.2 — Bounded worker pool (`pkg/intelligence/embed_worker.go`)

**Goal:** ship the async dispatch infrastructure that any observer can use.

**Files:**
- `pkg/intelligence/embed_worker.go` (new): `EmbedWorker` struct, `Start`, `Submit`,
  `Shutdown`; configurable queue depth and worker count; synchronous test mode;
  dropped-task counter.

**Acceptance:** `EmbedWorker` under race detector with concurrent `Submit` and
`Shutdown` calls passes T6 and T7; no goroutine leak after `Shutdown`; lint clean.

### PR R2.3 — `Embedder` interface + `ErrNoIndexForTenant`

**Goal:** define the contract that all embedder backends will satisfy.

**Files:**
- `pkg/intelligence/embedder.go` (new, replacing the archive's facade version):
  `Embedder` interface; `ErrNoIndexForTenant` error type.

**Acceptance:** the file contains no `mockEmbedding`, no `h := float32(len(content))`,
and no `// Mock` comments. Compile-only PR. T3 passes.

### PR R2.4 — `LSAEmbedder` adapter

**Goal:** ship the first real implementation of `Embedder` using the existing LSA path.

**Files:**
- `pkg/intelligence/lsa_embedder.go` (new): `LSAEmbedder` struct wrapping
  `*search.TenantLSAIndexes`; `Embed(ctx, tenantID, text)` calls `FoldQuery` on the
  tenant's index; returns `ErrNoIndexForTenant` when `Get(tenantID) == nil`.

**Acceptance:** T1 (golden fixture), T2 (mock-fallback detection), T3, T4 pass.

### PR R2.5 — Wire-up + `AutoEmbedObserver`

**Goal:** connect the observer infrastructure to the embedder + policy registration.

**Files:**
- `pkg/intelligence/auto_embed_observer.go` (new): `AutoEmbedObserver` struct;
  `EmbeddingPolicy` type (label, sourceProperty, targetProperty); `OnNodeCreated`
  dispatches to `EmbedWorker`; no `OnNodeUpdated` side effects for now (deferred).
- `pkg/api/server_init.go`: optional wiring block that reads embedding policy from
  config and registers `AutoEmbedObserver` on `*GraphStorage` via `AddObserver`.

**Acceptance:** T5 (non-blocking); T6 (drain); end-to-end integration test creates
a node matching a policy, waits for the observer pool to drain, reads the node back,
and asserts a non-nil vector property of length 200.

---

## 10. Out of Scope

- **`AddObserver` on the `Storage` interface** — this is R3. R2 uses the concrete
  `*GraphStorage` type at the wiring call site.
- **`OnNodeUpdated` auto-re-embed** — the archive had this; it is deferred. The
  loop-prevention mechanism is non-trivial and the value of re-embedding on update is
  unclear without a customer workload.
- **External API embedder (`RemoteAPIEmbedder`)** — Option C deferred. The interface
  defined in §7.1 allows it to land as a future PR without any interface changes.
- **BTreeGraphStorage observer wiring** — the archive's `observation.go` reused
  `vectorIndexesMu` as the observer lock on `BTreeGraphStorage`. The redesign should
  give observers their own dedicated `sync.RWMutex`. Defer to the C2 PR that extracts
  `BTreeGraphStorage` (Track C).
- **Dimension compatibility check** — left to the `LSAEmbedder` to surface as an
  error from the `UpdateNodeForTenant` call; no pre-flight check in this spike.
- **GNN embedding path** — S6 is spike-quality and deferred per
  `docs/NEXT_STEPS_2026-05-13.md`. The observer interface is compatible with a
  future GNN embedder without changes.
