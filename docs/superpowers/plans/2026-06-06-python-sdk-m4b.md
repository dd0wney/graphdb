# Python SDK M4b Implementation Plan (response caching)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opt-in, pluggable response caching (GET-only, TTL-bounded, fail-open) to both the sync and async clients, with an in-memory LRU default backend.

**Architecture:** Approach B — caching is a thin wrapper (`CachingTransport`/`AsyncCachingTransport`) around the transport, composed by the client only when a `cache=` backend is supplied (zero overhead otherwise). Backends satisfy a `CacheBackend` (sync) / `AsyncCacheBackend` (async, `aget/aset/...`) Protocol; `InMemoryCache` implements both. Hit/miss stats live on the wrapper.

**Tech Stack:** Python 3.9+, `httpx`, stdlib `dataclasses`/`threading`/`collections.OrderedDict`/`time`/`urllib.parse`; tests use `respx` + `pytest` (+`pytest-asyncio`); `ruff` + `mypy --strict`. All commands run from `clients/python/` via `uv run`.

**Spec:** `docs/superpowers/specs/2026-06-06-python-sdk-m4-design.md` §4.

## Refinements of the spec (deliberate — implement these, not the looser spec wording)

1. **`AsyncCacheBackend` uses `aget/aset/adelete/aclear`** (NOT same-named async methods). A class cannot have both a sync and an async method of the same name, and `InMemoryCache` implements both protocols.
2. **Write-invalidation fires only on `PUT/PATCH/DELETE`** (`_MUTATING` excludes POST). graphdb uses POST for reads (`/query`, `/search`, `/traverse`, `/v1/retrieve`, …); clearing on POST would defeat caching. POST-creates rely on TTL for freshness.
3. **Stats live on the wrapper** (`CachingTransport.stats`), exposed via `client.cache_stats` — they apply to any backend, not just the in-memory one.
4. The wrapper is bridged to the resources' `Transport`-typed param with a single localized `cast` (it implements the exact `request()`/`close()` surface resources + client use); resources are unchanged.

## File structure

- **Create** `src/graphdb_client/cache.py` — `CacheBackend`, `AsyncCacheBackend` (protocols), `InMemoryCache` (both), `CacheConfig`. Pure storage + config; no transport dependency.
- **Create** `src/graphdb_client/_caching.py` — `cache_key(...)`, `_MUTATING`, `CachingTransport` (sync wrapper).
- **Create** `src/graphdb_client/aio/caching.py` — `AsyncCachingTransport` (async wrapper; reuses `cache_key`/`_MUTATING`).
- **Modify** `src/graphdb_client/client.py` + `aio/client.py` — `cache`/`cache_config` params, wrap-when-set, `cache_stats` property.
- **Modify** `src/graphdb_client/__init__.py` — export `InMemoryCache`, `CacheConfig`, `CacheBackend`, `AsyncCacheBackend`.
- **Modify** `README.md` — caching section.
- **Tests**: `tests/test_cache.py`, `tests/test_caching_transport.py`, `tests/test_async_caching.py`, `tests/test_cache_client.py`.

---

### Task 1: `cache.py` — backends + config

**Files:**
- Create: `clients/python/src/graphdb_client/cache.py`
- Test: `clients/python/tests/test_cache.py`

- [ ] **Step 1: write the failing test** — create `tests/test_cache.py`:

```python
from __future__ import annotations

import graphdb_client.cache as cache_mod
from graphdb_client.cache import AsyncCacheBackend, CacheBackend, CacheConfig, InMemoryCache


def test_set_get_delete_clear():
    c = InMemoryCache()
    assert c.get("missing") is None
    c.set("k", "v", ttl=100)
    assert c.get("k") == "v"
    c.delete("k")
    assert c.get("k") is None
    c.set("x", 1, ttl=100)
    c.clear()
    assert c.get("x") is None


def test_ttl_expiry(monkeypatch):
    t = {"now": 1000.0}
    monkeypatch.setattr(cache_mod.time, "monotonic", lambda: t["now"])
    c = InMemoryCache()
    c.set("k", "v", ttl=10)
    assert c.get("k") == "v"
    t["now"] = 1011.0
    assert c.get("k") is None


def test_lru_eviction_at_maxsize():
    c = InMemoryCache(maxsize=2)
    c.set("a", 1, ttl=100)
    c.set("b", 2, ttl=100)
    c.set("c", 3, ttl=100)
    assert c.get("a") is None      # oldest evicted
    assert c.get("b") == 2 and c.get("c") == 3


def test_lru_recency_refresh_on_get():
    c = InMemoryCache(maxsize=2)
    c.set("a", 1, ttl=100)
    c.set("b", 2, ttl=100)
    assert c.get("a") == 1          # touch a -> a becomes MRU
    c.set("c", 3, ttl=100)         # evicts LRU = b
    assert c.get("b") is None and c.get("a") == 1 and c.get("c") == 3


async def test_async_methods_delegate():
    c = InMemoryCache()
    await c.aset("k", "v", ttl=100)
    assert await c.aget("k") == "v"
    await c.adelete("k")
    assert await c.aget("k") is None
    await c.aset("x", 1, ttl=100)
    await c.aclear()
    assert await c.aget("x") is None


def test_inmemory_satisfies_both_protocols():
    c = InMemoryCache()
    assert isinstance(c, CacheBackend)
    assert isinstance(c, AsyncCacheBackend)


def test_cache_config_defaults():
    cfg = CacheConfig()
    assert cfg.default_ttl == 300.0
    assert cfg.invalidate_on_write is True
    assert cfg.ttl_overrides == {}
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_cache.py -q` → ImportError.

- [ ] **Step 3: create `src/graphdb_client/cache.py`:**

```python
from __future__ import annotations

import threading
import time
from collections import OrderedDict
from dataclasses import dataclass, field
from typing import Any, Mapping, Protocol, runtime_checkable


@runtime_checkable
class CacheBackend(Protocol):
    """Sync cache backend. Implement this to plug in Redis, memcached, etc."""

    def get(self, key: str) -> Any | None: ...
    def set(self, key: str, value: Any, *, ttl: float) -> None: ...
    def delete(self, key: str) -> None: ...
    def clear(self) -> None: ...


@runtime_checkable
class AsyncCacheBackend(Protocol):
    """Async cache backend. Distinct method names so one class can implement both."""

    async def aget(self, key: str) -> Any | None: ...
    async def aset(self, key: str, value: Any, *, ttl: float) -> None: ...
    async def adelete(self, key: str) -> None: ...
    async def aclear(self) -> None: ...


@dataclass
class CacheConfig:
    default_ttl: float = 300.0
    invalidate_on_write: bool = True
    ttl_overrides: Mapping[str, float] = field(default_factory=dict)  # path-prefix -> ttl


class InMemoryCache:
    """Thread-safe bounded LRU cache with per-entry TTL. Implements both backend protocols."""

    def __init__(self, *, maxsize: int = 1024) -> None:
        self._maxsize = maxsize
        self._lock = threading.Lock()
        self._store: "OrderedDict[str, tuple[float, Any]]" = OrderedDict()

    def get(self, key: str) -> Any | None:
        with self._lock:
            item = self._store.get(key)
            if item is None:
                return None
            expiry, value = item
            if expiry < time.monotonic():
                del self._store[key]
                return None
            self._store.move_to_end(key)
            return value

    def set(self, key: str, value: Any, *, ttl: float) -> None:
        with self._lock:
            self._store[key] = (time.monotonic() + ttl, value)
            self._store.move_to_end(key)
            while len(self._store) > self._maxsize:
                self._store.popitem(last=False)

    def delete(self, key: str) -> None:
        with self._lock:
            self._store.pop(key, None)

    def clear(self) -> None:
        with self._lock:
            self._store.clear()

    async def aget(self, key: str) -> Any | None:
        return self.get(key)

    async def aset(self, key: str, value: Any, *, ttl: float) -> None:
        self.set(key, value, ttl=ttl)

    async def adelete(self, key: str) -> None:
        self.delete(key)

    async def aclear(self) -> None:
        self.clear()
```

- [ ] **Step 4: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_cache.py -q` → 7 passed.

- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/cache.py clients/python/tests/test_cache.py
git commit -m "feat(sdk): cache backends (CacheBackend/AsyncCacheBackend protocols + InMemoryCache)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `_caching.py` — sync CachingTransport

**Files:**
- Create: `clients/python/src/graphdb_client/_caching.py`
- Test: `clients/python/tests/test_caching_transport.py`

- [ ] **Step 1: write the failing test** — create `tests/test_caching_transport.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._caching import CachingTransport, cache_key
from graphdb_client._transport import Transport
from graphdb_client.cache import CacheConfig, InMemoryCache

OK = {"id": 1, "labels": [], "properties": {}}


def _ct(base_url, cache=None, config=None):
    inner = Transport(base_url, token="tok")
    return CachingTransport(inner, cache or InMemoryCache(), config or CacheConfig())


def test_cache_key_sorts_params():
    assert cache_key("GET", "/nodes", {"b": 2, "a": 1}) == "GET:/nodes?a=1&b=2"
    assert cache_key("get", "/nodes", None) == "GET:/nodes"


@respx.mock
def test_second_get_served_from_cache(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    assert ct.request("GET", "/nodes/1").data == OK
    assert ct.request("GET", "/nodes/1").data == OK
    assert route.call_count == 1
    assert ct.stats["hits"] == 1 and ct.stats["misses"] == 1 and ct.stats["hit_rate"] == 0.5


@respx.mock
def test_put_invalidates_cache(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.put(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    ct.request("GET", "/nodes/1")             # cached
    ct.request("PUT", "/nodes/1", json={})    # PUT clears cache
    ct.request("GET", "/nodes/1")             # miss -> refetch
    assert get.call_count == 2


@respx.mock
def test_post_does_not_invalidate(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={"results": []}))
    ct = _ct(base_url)
    ct.request("GET", "/nodes/1")             # cached
    ct.request("POST", "/search", json={})    # POST is a read here -> must NOT clear
    ct.request("GET", "/nodes/1")             # still served from cache
    assert get.call_count == 1


@respx.mock
def test_post_never_cached(base_url):
    route = respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={"results": [1]}))
    ct = _ct(base_url)
    ct.request("POST", "/search", json={})
    ct.request("POST", "/search", json={})
    assert route.call_count == 2


@respx.mock
def test_fail_open_on_broken_backend(base_url):
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))

    class Broken:
        def get(self, key): raise RuntimeError("boom")
        def set(self, key, value, *, ttl): raise RuntimeError("boom")
        def delete(self, key): raise RuntimeError("boom")
        def clear(self): raise RuntimeError("boom")

    ct = CachingTransport(Transport(base_url, token="tok"), Broken(), CacheConfig())
    assert ct.request("GET", "/nodes/1").data == OK   # backend errors swallowed


@respx.mock
def test_cache_hit_preserves_headers(base_url):
    respx.get(f"{base_url}/nodes").mock(
        return_value=httpx.Response(200, json=[OK], headers={"X-Next-Cursor": "c2"}))
    ct = _ct(base_url)
    ct.request("GET", "/nodes")
    r2 = ct.request("GET", "/nodes")
    assert r2.headers.get("X-Next-Cursor") == "c2"
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_caching_transport.py -q` → ImportError.

- [ ] **Step 3: create `src/graphdb_client/_caching.py`:**

```python
from __future__ import annotations

from typing import Any, Mapping
from urllib.parse import urlencode

from ._transport import ApiResult, Transport
from .cache import CacheBackend, CacheConfig

# Only unambiguous mutations invalidate. graphdb uses POST for reads
# (/query, /search, /traverse, ...), so POST is deliberately excluded.
_MUTATING = frozenset({"PUT", "PATCH", "DELETE"})


def cache_key(method: str, path: str, params: Mapping[str, Any] | None) -> str:
    if params:
        query = urlencode(sorted((str(k), str(v)) for k, v in params.items()))
        return f"{method.upper()}:{path}?{query}"
    return f"{method.upper()}:{path}"


class CachingTransport:
    """Wraps a Transport with cache-aside GET caching + write invalidation. Fail-open."""

    def __init__(self, inner: Transport, cache: CacheBackend, config: CacheConfig) -> None:
        self._inner = inner
        self._cache = cache
        self._config = config
        self._hits = 0
        self._misses = 0

    @property
    def stats(self) -> dict[str, float]:
        total = self._hits + self._misses
        return {
            "hits": self._hits,
            "misses": self._misses,
            "hit_rate": (self._hits / total) if total else 0.0,
        }

    def _ttl_for(self, path: str) -> float:
        for prefix, ttl in self._config.ttl_overrides.items():
            if path.startswith(prefix):
                return ttl
        return self._config.default_ttl

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        m = method.upper()
        if m != "GET":
            res = self._inner.request(method, path, json=json, params=params)
            if self._config.invalidate_on_write and m in _MUTATING:
                try:
                    self._cache.clear()
                except Exception:
                    pass
            return res

        key = cache_key(method, path, params)
        try:
            cached = self._cache.get(key)
        except Exception:
            cached = None
        if cached is not None:
            self._hits += 1
            return cached
        self._misses += 1
        res = self._inner.request(method, path, json=json, params=params)
        try:
            self._cache.set(key, res, ttl=self._ttl_for(path))
        except Exception:
            pass
        return res

    def close(self) -> None:
        self._inner.close()
```

- [ ] **Step 4: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_caching_transport.py -q` → 7 passed.

- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/_caching.py clients/python/tests/test_caching_transport.py
git commit -m "feat(sdk): sync CachingTransport (GET cache-aside, PUT/PATCH/DELETE invalidation, fail-open)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `aio/caching.py` — AsyncCachingTransport

**Files:**
- Create: `clients/python/src/graphdb_client/aio/caching.py`
- Test: `clients/python/tests/test_async_caching.py`

The async mirror of Task 2. It reuses `cache_key` and `_MUTATING` from `.._caching` (DRY), uses `AsyncCacheBackend` (await `aget`/`aset`/`aclear`), and delegates `aclose`.

- [ ] **Step 1: write the failing test** — create `tests/test_async_caching.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.caching import AsyncCachingTransport
from graphdb_client.aio.transport import AsyncTransport
from graphdb_client.cache import CacheConfig, InMemoryCache

OK = {"id": 1, "labels": [], "properties": {}}


def _ct(base_url, cache=None, config=None):
    inner = AsyncTransport(base_url, token="tok")
    return AsyncCachingTransport(inner, cache or InMemoryCache(), config or CacheConfig())


@respx.mock
async def test_second_get_served_from_cache(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    assert (await ct.request("GET", "/nodes/1")).data == OK
    assert (await ct.request("GET", "/nodes/1")).data == OK
    assert route.call_count == 1
    assert ct.stats["hits"] == 1 and ct.stats["misses"] == 1


@respx.mock
async def test_put_invalidates_cache(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.put(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    await ct.request("GET", "/nodes/1")
    await ct.request("PUT", "/nodes/1", json={})
    await ct.request("GET", "/nodes/1")
    assert get.call_count == 2


@respx.mock
async def test_post_does_not_invalidate(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={"results": []}))
    ct = _ct(base_url)
    await ct.request("GET", "/nodes/1")
    await ct.request("POST", "/search", json={})
    await ct.request("GET", "/nodes/1")
    assert get.call_count == 1


@respx.mock
async def test_fail_open_on_broken_backend(base_url):
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))

    class Broken:
        async def aget(self, key): raise RuntimeError("boom")
        async def aset(self, key, value, *, ttl): raise RuntimeError("boom")
        async def adelete(self, key): raise RuntimeError("boom")
        async def aclear(self): raise RuntimeError("boom")

    ct = AsyncCachingTransport(AsyncTransport(base_url, token="tok"), Broken(), CacheConfig())
    assert (await ct.request("GET", "/nodes/1")).data == OK
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_async_caching.py -q` → ImportError.

- [ ] **Step 3: create `src/graphdb_client/aio/caching.py`:**

```python
from __future__ import annotations

from typing import Any, Mapping

from .._caching import _MUTATING, cache_key
from .._transport import ApiResult
from ..cache import AsyncCacheBackend, CacheConfig
from .transport import AsyncTransport


class AsyncCachingTransport:
    """Async mirror of CachingTransport: cache-aside GET caching + write invalidation. Fail-open."""

    def __init__(
        self, inner: AsyncTransport, cache: AsyncCacheBackend, config: CacheConfig
    ) -> None:
        self._inner = inner
        self._cache = cache
        self._config = config
        self._hits = 0
        self._misses = 0

    @property
    def stats(self) -> dict[str, float]:
        total = self._hits + self._misses
        return {
            "hits": self._hits,
            "misses": self._misses,
            "hit_rate": (self._hits / total) if total else 0.0,
        }

    def _ttl_for(self, path: str) -> float:
        for prefix, ttl in self._config.ttl_overrides.items():
            if path.startswith(prefix):
                return ttl
        return self._config.default_ttl

    async def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        m = method.upper()
        if m != "GET":
            res = await self._inner.request(method, path, json=json, params=params)
            if self._config.invalidate_on_write and m in _MUTATING:
                try:
                    await self._cache.aclear()
                except Exception:
                    pass
            return res

        key = cache_key(method, path, params)
        try:
            cached = await self._cache.aget(key)
        except Exception:
            cached = None
        if cached is not None:
            self._hits += 1
            return cached
        self._misses += 1
        res = await self._inner.request(method, path, json=json, params=params)
        try:
            await self._cache.aset(key, res, ttl=self._ttl_for(path))
        except Exception:
            pass
        return res

    async def aclose(self) -> None:
        await self._inner.aclose()
```

NOTE: importing `_MUTATING` (a private name) from `.._caching` is intentional DRY — the async wrapper shares the sync module's invalidation set. If ruff flags the underscore import, keep it (it's a deliberate internal reuse); do not duplicate the frozenset.

- [ ] **Step 4: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_async_caching.py -q` → 4 passed.

- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/aio/caching.py clients/python/tests/test_async_caching.py
git commit -m "feat(sdk): AsyncCachingTransport (mirror of sync caching)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: wire `cache=` into both clients + exports + README

**Files:**
- Modify: `clients/python/src/graphdb_client/client.py`
- Modify: `clients/python/src/graphdb_client/aio/client.py`
- Modify: `clients/python/src/graphdb_client/__init__.py`
- Modify: `clients/python/README.md`
- Test: `clients/python/tests/test_cache_client.py`

- [ ] **Step 1: write the failing test** — create `tests/test_cache_client.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client import CacheConfig, GraphDBClient, InMemoryCache

OK = {"id": 1, "labels": [], "properties": {}}


def test_cache_exports():
    assert InMemoryCache().get("x") is None
    assert CacheConfig().default_ttl == 300.0


@respx.mock
def test_client_caches_get(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    c = GraphDBClient(base_url, token="tok", cache=InMemoryCache())
    assert c.nodes.get(1).id == 1
    assert c.nodes.get(1).id == 1
    assert route.call_count == 1
    assert c.cache_stats is not None and c.cache_stats["hits"] == 1


@respx.mock
def test_client_no_cache_by_default(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    c = GraphDBClient(base_url, token="tok")
    c.nodes.get(1)
    c.nodes.get(1)
    assert route.call_count == 2
    assert c.cache_stats is None
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_cache_client.py -q` → ImportError / TypeError.

- [ ] **Step 3: edit `src/graphdb_client/client.py`.** Add imports (after `from ._retry import RetryConfig, coerce_retry_config`):

```python
from typing import cast

from ._caching import CachingTransport
from .cache import CacheBackend, CacheConfig
```

(There is already a `from typing import Any, Mapping, Sequence` near the top — add `cast` there instead if you prefer; a second `from typing import cast` is also fine and ruff will merge/sort. Ensure no duplicate-import lint.)

Add two params to `GraphDBClient.__init__` signature (after `retries: RetryConfig | int | None = 2,`):

```python
        cache: CacheBackend | None = None,
        cache_config: CacheConfig | None = None,
```

Replace the `self._raw = Transport(...)` assignment block with:

```python
        inner = Transport(
            base_url,
            token=token,
            api_key=api_key,
            username=username,
            password=password,
            timeout=timeout,
            retries=coerce_retry_config(retries),
        )
        # CachingTransport implements the request()/close() surface resources + client use;
        # cast keeps the resource transport type as Transport (it duck-types correctly).
        self._raw: Transport = (
            cast(Transport, CachingTransport(inner, cache, cache_config or CacheConfig()))
            if cache is not None
            else inner
        )
```

Add a `cache_stats` property (place it next to `close`, after the top-level methods):

```python
    @property
    def cache_stats(self) -> dict[str, float] | None:
        """Cache hit/miss stats, or None when caching is disabled."""
        stats = getattr(self._raw, "stats", None)
        return stats if isinstance(stats, dict) else None
```

- [ ] **Step 4: edit `src/graphdb_client/aio/client.py`** identically (async). Add imports (after `from .._retry import RetryConfig, coerce_retry_config`):

```python
from typing import cast

from ..cache import AsyncCacheBackend, CacheConfig
from .caching import AsyncCachingTransport
```

(Merge `cast` into the existing `from typing import ...` line if present; avoid duplicate imports.)

Add params to `AsyncGraphDBClient.__init__` (after `retries: RetryConfig | int | None = 2,`):

```python
        cache: AsyncCacheBackend | None = None,
        cache_config: CacheConfig | None = None,
```

Replace the `self._raw = AsyncTransport(...)` assignment with:

```python
        inner = AsyncTransport(
            base_url, token=token, api_key=api_key,
            username=username, password=password, timeout=timeout,
            retries=coerce_retry_config(retries),
        )
        self._raw: AsyncTransport = (
            cast(AsyncTransport, AsyncCachingTransport(inner, cache, cache_config or CacheConfig()))
            if cache is not None
            else inner
        )
```

Add the `cache_stats` property (next to `aclose`):

```python
    @property
    def cache_stats(self) -> dict[str, float] | None:
        """Cache hit/miss stats, or None when caching is disabled."""
        stats = getattr(self._raw, "stats", None)
        return stats if isinstance(stats, dict) else None
```

- [ ] **Step 5: edit `src/graphdb_client/__init__.py`** to export the cache surface. Add an import:

```python
from .cache import AsyncCacheBackend, CacheBackend, CacheConfig, InMemoryCache
```

(ruff will order it; place near the other relative imports.) Add to `__all__` (after `"RetryConfig"`):

```python
    "InMemoryCache", "CacheConfig", "CacheBackend", "AsyncCacheBackend",
```

- [ ] **Step 6: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_cache_client.py -q` → 3 passed. Verify exports: `cd clients/python && uv run python -c "from graphdb_client import InMemoryCache, CacheConfig, CacheBackend, AsyncCacheBackend; print('ok')"`.

- [ ] **Step 7: README.** Append to `clients/python/README.md` (tilde-fenced so the inner code block renders):

~~~markdown
## Caching (optional)

Pass a cache backend to enable opt-in, GET-only response caching (off by default —
zero overhead when unset). The built-in `InMemoryCache` is a thread-safe bounded
LRU with per-entry TTL; implement `CacheBackend` (sync) / `AsyncCacheBackend`
(async) to plug in Redis etc.

```python
from graphdb_client import GraphDBClient, InMemoryCache, CacheConfig

db = GraphDBClient(url, token=TOKEN,
                   cache=InMemoryCache(maxsize=2048),
                   cache_config=CacheConfig(default_ttl=60))

db.nodes.get(1)          # fetched + cached
db.nodes.get(1)          # served from cache
print(db.cache_stats)    # {"hits": 1, "misses": 1, "hit_rate": 0.5}
```

Only `GET` responses are cached. Freshness is TTL-bounded; `PUT`/`PATCH`/`DELETE`
clear the cache (graphdb uses `POST` for reads, so `POST` does not invalidate).
`AsyncGraphDBClient` takes the same `cache`/`cache_config` arguments
(pass a backend implementing `AsyncCacheBackend`; `InMemoryCache` implements both).
~~~

- [ ] **Step 8: full gate:**
```
cd clients/python && uv run pytest -q          # expect 135 prior + ~21 new = ~156 passed, 2 skipped (report real)
cd clients/python && uv run ruff check .
cd clients/python && uv run mypy src
```

- [ ] **Step 9: commit:**
```
git add clients/python/src/graphdb_client/client.py clients/python/src/graphdb_client/aio/client.py clients/python/src/graphdb_client/__init__.py clients/python/README.md clients/python/tests/test_cache_client.py
git commit -m "feat(sdk): wire cache into both clients + exports + cache_stats + README

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementer

- **GET-only caching.** Only `GET` is cached; every other method passes straight through. `PUT/PATCH/DELETE` clear the whole cache when `invalidate_on_write` (default True). `POST` is NEVER an invalidator (graphdb uses POST for reads) and POST responses are never cached.
- **Fail-open is mandatory.** Every backend call (`get/set/clear` / `aget/aset/aclear`) is wrapped in `try/except Exception` and swallowed — a broken cache must never break a request. The `test_fail_open_on_broken_backend` test enforces this.
- **The `cast`** in the clients is the single, intentional type bridge: `CachingTransport`/`AsyncCachingTransport` implement exactly the `request()`/`close()`/`aclose()` surface the resources + client use, so casting to `Transport`/`AsyncTransport` is sound and keeps the 9 resource classes unchanged. `cache_stats` reads via `getattr` + `isinstance(dict)` so it stays mypy-clean without an `isinstance(CachingTransport)` check.
- **`InMemoryCache` implements BOTH protocols** (sync `get/...` + async `aget/...`). The async client accepts `AsyncCacheBackend`; passing `InMemoryCache()` satisfies it.
- **TTL uses `time.monotonic()`** (immune to wall-clock jumps); tests monkeypatch `cache_mod.time.monotonic`.
- **mypy is `--strict`** — annotate everything; the `stats` dict is `dict[str, float]` (ints are accepted in float positions via the numeric tower).
- After all 4 tasks: final whole-implementation review, then `superpowers:finishing-a-development-branch` (PR `feat/python-sdk-m4b`).
```
