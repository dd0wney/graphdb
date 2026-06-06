# graphdb Python SDK — M4 design spec (resilience + caching + LangChain)

**Date**: 2026-06-06
**Status**: design (awaiting review) → implementation plans (M4a, M4b, M4c)
**Parent specs**: M1 (`2026-06-04-python-sdk-design.md`), M2a/M2b, M3
(`2026-06-06-python-sdk-m3-design.md`). All cross-cutting decisions hold and are
NOT relitigated: stdlib dataclasses (`httpx`-only core runtime dep, decision D1),
the `Transport`/`AsyncTransport` choke point, the M1 error hierarchy, Python 3.9+,
`uv` toolchain, `ruff` + `mypy --strict` gates, `respx` unit tests.

---

## 1. Goal & scope

M4 is the "polish" milestone from the M1 roadmap: make the client resilient,
optionally cache reads, and ship first-class LangChain adapters. Delivered as
**three independently-shippable sub-milestones**, each its own implementation
plan + PR:

- **M4a — retry/backoff** (transport-level resilience).
- **M4b — response caching** (pluggable backend, in-memory default).
- **M4c — LangChain integration** (retriever + vector store + loader, optional extra).

**In scope:** everything above, for BOTH the sync and async clients where it
applies (retry + caching mirror into `aio/`; LangChain provides sync + async
methods).

**Out of scope:** no new graphdb endpoints; no change to the existing resource
surface or models; no GraphQL query-builder; no pydantic (D1 stands). LangChain
is an **optional extra** — the core `pip install graphdb-client` stays httpx-only.

## 2. Decisions (locked in brainstorming)

| Decision | Choice |
|---|---|
| Architecture (retry/cache attach) | **Approach B** — retry folded into the transport (next to the existing 401-refresh, which is already a transport-level retry); caching as a single optional wrapper. Pure retry-policy helpers extracted so sync + async share the decision logic. |
| M4 scope | **All three** sub-milestones. |
| Cache configurability | **Pluggable `CacheBackend` protocol + `InMemoryCache` default.** |
| LangChain depth | **Full kit** — retriever + vector store + document loader. |
| Retry default | **On by default** (`max_retries=2`), settable to `0` to disable. Resilience is the point of the milestone; this matches `requests`/`urllib3` norms. |
| LangChain dependency | **Optional extra** `graphdb-client[langchain]` (`langchain-core`); guarded import gives a clear install message; core install unaffected. |

## 3. M4a — retry/backoff

### 3.1 New module `src/graphdb_client/_retry.py` (pure, sync/async-shared)

```python
@dataclass(frozen=True)
class RetryConfig:
    max_retries: int = 2
    backoff_factor: float = 0.5
    max_backoff: float = 30.0
    retry_statuses: frozenset[int] = frozenset({429, 502, 503, 504})
    retry_methods: frozenset[str] = frozenset({"GET", "PUT", "DELETE", "HEAD", "OPTIONS"})
    respect_retry_after: bool = True
```

Pure helpers (no I/O — unit-testable directly, shared by both transports):

- `is_retryable(method, status, exc, config) -> bool`
  - A connection/timeout error (`httpx.TransportError`) → retryable (request not
    completed).
  - A retryable status: **`429` is retryable on any method** (rate-limit ⇒ request
    not processed); other `retry_statuses` (5xx) retry **only for idempotent
    `retry_methods`** (a non-idempotent POST may have been applied server-side).
- `compute_delay(attempt, config, retry_after) -> float`
  - If `respect_retry_after` and a `Retry-After` header is present (delta-seconds
    or HTTP-date) → use it (clamped to `max_backoff`).
  - Else full-jitter exponential: `random.uniform(0, min(max_backoff, backoff_factor * 2**attempt))`.
- `parse_retry_after(value: str | None) -> float | None` (seconds or HTTP-date → seconds).

### 3.2 Folding into the transports

`Transport.request` / `AsyncTransport.request` wrap their existing body in a
retry loop of up to `max_retries + 1` attempts:

1. Run one attempt (the **existing** lazy-login + request + 401-refresh-retry +
   error-map logic is the body of one attempt — unchanged).
2. On a retryable failure (a retryable `GraphDBError` raised by the attempt, or a
   caught `httpx.TransportError`) with attempts remaining: sleep
   `compute_delay(...)` then retry. Sync uses `time.sleep`; async uses
   `asyncio.sleep`. The `Retry-After` value is read from the response that
   triggered the retry.
3. On the final attempt, the error propagates (same exceptions as today).

The 401-refresh stays **inside** an attempt (auth refresh is not "a retry" in the
backoff sense). Retries wrap the whole attempt including any refresh.

`RetryConfig` is threaded through: `Transport.__init__` / `AsyncTransport.__init__`
and `GraphDBClient` / `AsyncGraphDBClient` gain `retries: RetryConfig | int | None = 2`
(an `int` is `max_retries` shorthand; `0`/`None` disables → today's behavior).

### 3.3 Tests (`tests/test_retry.py`, `tests/test_async_retry.py`)

`respx` `side_effect` sequences; **monkeypatch `time.sleep`/`asyncio.sleep`** so
tests don't actually wait. Cases: 429→200 retried; 503×N exhausts → raises
`ServerError`; `httpx.ConnectError`→200 retried; non-idempotent POST + 503 **not**
retried (idempotency guard); `Retry-After: 2` honored over computed backoff;
`max_retries=0` disables; pure-helper unit tests for `is_retryable`/`compute_delay`/
`parse_retry_after`. The async file re-checks the same matrix with `asyncio.sleep`
patched.

## 4. M4b — response caching

### 4.1 Backend protocols (`src/graphdb_client/cache.py`)

```python
@runtime_checkable
class CacheBackend(Protocol):
    def get(self, key: str) -> Any | None: ...
    def set(self, key: str, value: Any, *, ttl: float) -> None: ...
    def delete(self, key: str) -> None: ...
    def clear(self) -> None: ...

@runtime_checkable
class AsyncCacheBackend(Protocol):
    async def get(self, key: str) -> Any | None: ...
    async def set(self, key: str, value: Any, *, ttl: float) -> None: ...
    async def delete(self, key: str) -> None: ...
    async def clear(self) -> None: ...
```

`InMemoryCache` implements **both** (it is pure-memory — no `await` needed; the
async methods just call the sync ones). Thread-safe bounded LRU: an
`OrderedDict` under a `threading.Lock`, `maxsize` (default 1024) eviction +
per-entry expiry timestamp (lazy eviction on `get`). Exposes `stats` →
`{"hits": int, "misses": int, "hit_rate": float}`.

### 4.2 Caching wrappers (`CachingTransport`, `AsyncCachingTransport`)

A wrapper implementing the same `request(...) -> ApiResult` + `close()`/`aclose()`
surface as the transport, so resources are unchanged (the client passes the
wrapper as `_raw`):

- **Cache GET only.** Key = `f"{method}:{path}?{urlencode(sorted(params))}"`.
  On hit (and unexpired) → return the cached `ApiResult` (data **and** headers, so
  `X-Next-Cursor` survives), count a hit. On miss → delegate, store with
  `config.default_ttl` (or a per-path-prefix override), count a miss.
- **Writes invalidate.** A mutating method with `config.invalidate_on_write`
  (default `True`) → `backend.clear()` after the delegated call succeeds
  (conservative + correct; TTL is the secondary staleness bound, matching the TS
  reference's TTL-driven freshness).
  - **Reconciled with shipped M4b (#362):** the mutating set is **`PUT/PATCH/DELETE`
    only** — `POST` is **excluded**. graphdb uses `POST` for *reads* (`/query`,
    `/search`, `/traverse`, `/v1/retrieve`, …), so clearing on `POST` would defeat
    the cache on every search. `POST`-creates therefore rely on TTL for freshness,
    not invalidation (a create-then-list can be TTL-stale). This refines the
    original `POST/PUT/PATCH/DELETE` wording above.
- **Fail-open.** Any backend exception on get/set/clear is swallowed (optionally
  logged via the stdlib `logging`), and the request proceeds — a cache must never
  break a call (mirrors `workers/graphdb-client/src/cache.ts`).

```python
@dataclass
class CacheConfig:
    default_ttl: float = 300.0
    invalidate_on_write: bool = True
    ttl_overrides: Mapping[str, float] = field(default_factory=dict)  # path-prefix -> ttl
```

### 4.3 Wiring

`GraphDBClient` / `AsyncGraphDBClient` gain `cache: CacheBackend | None = None`
(async accepts `AsyncCacheBackend`) and `cache_config: CacheConfig | None = None`.
When `cache` is set, the client wraps its transport: `self._raw =
CachingTransport(Transport(...), cache, cache_config or CacheConfig())`. When
unset, **zero overhead** — the plain transport is used directly. Retry (M4a) lives
*inside* the wrapped transport, so a cache hit short-circuits before any retry/HTTP.

### 4.4 Tests (`tests/test_cache.py`, `tests/test_async_cache.py`)

`InMemoryCache` unit tests (TTL expiry, LRU eviction at `maxsize`, stats, thread
-safety smoke). Wrapper tests via `respx`: a second identical GET is served from
cache (one upstream call); a mutating call clears the cache (next GET re-fetches);
TTL expiry re-fetches (monkeypatch the clock); a backend that raises is
fail-open; non-GET is never cached; headers (`X-Next-Cursor`) survive a hit.

## 5. M4c — LangChain integration

### 5.1 Packaging

`pyproject.toml`: `[project.optional-dependencies] langchain = ["langchain-core>=0.3"]`
and `langchain-core` added to the dev group. New subpackage
`src/graphdb_client/langchain/` with `__init__.py` that imports the three adapters
behind a guarded import — if `langchain_core` is absent, raise `ImportError("LangChain
integration requires: pip install graphdb-client[langchain]")`. The core package
never imports `graphdb_client.langchain`, so the httpx-only install is unaffected.

### 5.2 Adapters

- **`GraphDBRetriever(BaseRetriever)`** (`retriever.py`) — wraps a
  `GraphDBClient` (and/or `AsyncGraphDBClient`) over **`/v1/retrieve`** (GraphRAG).
  `_get_relevant_documents(query, *, run_manager) -> list[Document]` calls
  `client.retrieve(query, k=..., **opts)` and maps each `RetrieveResult` source →
  `Document(page_content=<source text>, metadata={node_id, score, ...})`.
  `_aget_relevant_documents` uses the async client when supplied. Constructor
  fields: `client`, optional `aclient`, `k`, and pass-through retrieve options.
- **`GraphDBVectorStore(VectorStore)`** (`vectorstore.py`) — over
  **`/vector-search`**. `__init__(client, property_name, *, embedding=None,
  aclient=None)`. `similarity_search(query, k)` embeds the query (via the supplied
  LangChain `Embeddings`, else graphdb `client.embeddings(...)`) then calls
  `client.vector_search(property_name, vector, k=k)`; `similarity_search_by_vector`
  skips the embed step; results map to `Document`s. Async twins
  (`asimilarity_search*`). The abstract `from_texts` classmethod is implemented as
  a thin builder where embeddings exist, else raises `NotImplementedError` with a
  reason (writing vectors into graphdb is a separate concern; documented).
- **`GraphDBLoader(BaseLoader)`** (`loader.py`) — `__init__(client, *, label=None,
  content_key="text")`. `lazy_load()` iterates `client.nodes.list(label=label)`
  yielding `Document(page_content=props.get(content_key, ""), metadata={id, labels,
  **other_props})`; `load()` materializes the iterator.

### 5.3 Tests (`tests/test_langchain.py`)

`langchain-core` in the dev group, so tests run in CI. Mock at the
**`GraphDBClient`** boundary (the adapters' job is mapping, not HTTP): a fake
client returning canned `RetrieveResult`/`SearchResult`/`Node` lists; assert the
adapters produce correctly-shaped `Document`s and call the right client methods
with the right args. One guarded-import test: importing `graphdb_client.langchain`
gives the helpful message when `langchain_core` is monkeypatched absent.

## 6. Cross-cutting

- **Sync/async parity** (M4a, M4b): the async client gets the same retry config +
  caching wrapper; `_retry.py` helpers and the `CacheConfig`/backend protocols are
  shared; only the sleep primitive and the wrapper's `await`s differ.
- **No breaking changes**: all new params default to today's behavior except
  retry (default `max_retries=2`, documented in the README + changelog as the one
  behavioral change; opt out with `retries=0`).
- **README**: a "Resilience & caching" section (retry config, the `cache=` opt-in,
  `InMemoryCache` example, writing a custom backend) and a "LangChain" section
  (the extra, retriever/vectorstore/loader quickstarts).

## 7. Definition of done (per sub-milestone)

- **M4a**: retry on both transports; `_retry.py` pure helpers; default-on, opt-out;
  unit matrix green (sync + async, sleeps patched); ruff + mypy clean; README +
  changelog note.
- **M4b**: `CacheBackend`/`AsyncCacheBackend` protocols + `InMemoryCache`;
  `CachingTransport`/`AsyncCachingTransport`; `cache=` wiring on both clients;
  fail-open; stats; unit suite green; ruff + mypy clean; README.
- **M4c**: `graphdb-client[langchain]` extra; retriever + vector store + loader
  (sync + async where supported); guarded import; tests green with `langchain-core`;
  ruff + mypy clean; README.
- Full suite stays green across all three; each lands as its own squash-merged PR.
- M4 closes the SDK roadmap (M1→M4); any further work is consumer-driven.
