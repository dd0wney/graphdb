# Python SDK M3 Implementation Plan (async client)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `AsyncGraphDBClient` — a complete async drop-in for `GraphDBClient` (same method names/signatures, awaitable) in a new `aio/` subpackage.

**Architecture:** Hand-written parallel hierarchy. `AsyncTransport` (httpx.AsyncClient) mirrors `Transport`'s auth/login/refresh/request logic with `await`; 9 `Async*Resource` classes + `AsyncGraphDBClient` mirror the sync resources/client. Models (`from_dict` is pure) and the error hierarchy are SHARED, not duplicated. Two pure transport helpers are extracted so the async transport doesn't re-implement them.

**Tech Stack:** Python 3.9+, `httpx` (only runtime dep), stdlib `dataclasses`; tests use `respx` + `pytest` + `pytest-asyncio` (added this milestone); `ruff` + `mypy --strict` gates. All commands run from `clients/python/` via `uv run`.

**Spec:** `docs/superpowers/specs/2026-06-06-python-sdk-m3-design.md`.

---

## THE ASYNC-MIRROR TRANSFORM (used by Tasks 3–5)

To turn a sync resource `src/graphdb_client/resources/X.py` into its async twin
`src/graphdb_client/aio/resources/X.py`, apply EXACTLY these mechanical edits and
nothing else (read the sync file as the source of truth):

1. Class `XResource` → `AsyncXResource`.
2. Imports: `from .._transport import Transport` → `from ..transport import AsyncTransport`; replace the `Transport` type hint in `__init__` with `AsyncTransport`. `from ..models import ...` → `from ...models import ...` (one extra dot — the file is one level deeper).
3. Every method `def m(self, ...)` → `async def m(self, ...)` — SAME name, SAME signature, SAME path, SAME body-building (omit-when-None logic unchanged).
4. Every `res = self._t.request(...)` → `res = await self._t.request(...)`; every bare `self._t.request(...)` (no assignment, e.g. `delete`) → `await self._t.request(...)`.
5. `Model.from_dict(...)` / dict-guard returns are UNCHANGED (same shared models).
6. Docstrings carried over unchanged.

There is exactly one non-mechanical case — `NodesResource.list` is a generator;
its async form is an `AsyncIterator` (Task 2 gives the full code).

Conventions: `respx` mocks `httpx.AsyncClient` identically; async tests are plain
`async def test_*` (enabled by `asyncio_mode = "auto"`, Task 1). The `base_url`
fixture (`tests/conftest.py`) returns `https://graphdb.test`.

---

### Task 1: shared helper extraction + AsyncTransport + deep tests

**Files:**
- Modify: `clients/python/src/graphdb_client/_transport.py` (extract `build_auth_headers`)
- Modify: `clients/python/pyproject.toml` (add `pytest-asyncio`, set `asyncio_mode`)
- Create: `clients/python/src/graphdb_client/aio/__init__.py`, `aio/transport.py`
- Test: `clients/python/tests/test_async_transport.py`

- [ ] **Step 1: tooling — add the async test runner.** In `pyproject.toml`:
  - In `[dependency-groups]` `dev = [...]`, add `"pytest-asyncio>=0.24"`.
  - Under `[tool.pytest.ini_options]` add: `asyncio_mode = "auto"`.
  Then `cd clients/python && uv sync` to install.

- [ ] **Step 2: behavior-preserving sync refactor — extract `build_auth_headers`.**
  In `src/graphdb_client/_transport.py`, add a module-level function (near `_safe_json`):

```python
def build_auth_headers(token: str | None, api_key: str | None) -> dict[str, str]:
    headers: dict[str, str] = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    elif api_key:
        headers["X-API-Key"] = api_key
    return headers
```

  and change `Transport._auth_headers` to delegate:

```python
    def _auth_headers(self) -> dict[str, str]:
        return build_auth_headers(self._token, self._api_key)
```

  Run `cd clients/python && uv run pytest tests/test_transport.py tests/test_auth.py -q` → still green (behavior unchanged).

- [ ] **Step 3: write the failing async-transport test** — create `tests/test_async_transport.py`:

```python
from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client.aio.transport import AsyncTransport
from graphdb_client.errors import AuthError, NotFoundError


@respx.mock
async def test_token_auth_header_and_success(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    t = AsyncTransport(base_url, token="tok")
    res = await t.request("GET", "/nodes/1")
    assert res.data["id"] == 1
    assert route.calls.last.request.headers["Authorization"] == "Bearer tok"
    await t.aclose()


@respx.mock
async def test_api_key_header(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    t = AsyncTransport(base_url, api_key="ak")
    await t.request("GET", "/nodes/1")
    assert route.calls.last.request.headers["X-API-Key"] == "ak"
    await t.aclose()


@respx.mock
async def test_lazy_login_when_credentials(base_url):
    login = respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "AT", "refresh_token": "RT"}))
    respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    t = AsyncTransport(base_url, username="u", password="p")
    await t.request("GET", "/nodes/1")
    assert login.called
    await t.aclose()


@respx.mock
async def test_401_triggers_refresh_then_retry(base_url):
    # first call 401, refresh succeeds, retry 200
    respx.post(f"{base_url}/auth/refresh").mock(
        return_value=httpx.Response(200, json={"access_token": "AT2"}))
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(401, json={"error": "expired"}),
        httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}),
    ])
    t = AsyncTransport(base_url, token="old")
    t._refresh_token = "RT"  # simulate a prior login
    res = await t.request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2
    await t.aclose()


@respx.mock
async def test_error_mapping(base_url):
    respx.get(f"{base_url}/nodes/9").mock(return_value=httpx.Response(404, json={"error": "nope"}))
    t = AsyncTransport(base_url, token="tok")
    with pytest.raises(NotFoundError):
        await t.request("GET", "/nodes/9")
    await t.aclose()


@respx.mock
async def test_async_context_manager_closes(base_url):
    respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    async with AsyncTransport(base_url, token="tok") as t:
        await t.request("GET", "/nodes/1")
    # no assertion on close beyond no-raise; covered by client lifecycle test too


def test_auth_validation_sync():
    # username/password must be provided together (validated in __init__, not async)
    import pytest as _pytest
    with _pytest.raises(ValueError):
        AsyncTransport("https://x", username="u")
```

- [ ] **Step 4: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_async_transport.py -q` → ImportError (`aio.transport`).

- [ ] **Step 5: create `aio/__init__.py`** — EMPTY for now (just a docstring), so `graphdb_client.aio` is a package without importing the not-yet-existing `client`. Task 6 adds the `AsyncGraphDBClient` export here.

```python
"""Async client for graphdb (AsyncGraphDBClient). See .client."""
```

- [ ] **Step 6: create `aio/transport.py`:**

```python
from __future__ import annotations

from typing import Any, Mapping

import httpx

from .._transport import ApiResult, _safe_json, build_auth_headers
from ..errors import from_response


class AsyncTransport:
    """Async choke point for every HTTP request: auth, error mapping, raw access.

    Mirrors graphdb_client._transport.Transport with httpx.AsyncClient and awaited
    I/O; the auth/login/refresh/retry/error-map contract is identical.
    """

    def __init__(
        self,
        base_url: str,
        *,
        token: str | None = None,
        api_key: str | None = None,
        username: str | None = None,
        password: str | None = None,
        timeout: float = 30.0,
    ) -> None:
        if (username is None) != (password is None):
            raise ValueError("username and password must be provided together")
        self._token = token
        self._api_key = api_key
        self._username = username
        self._password = password
        self._refresh_token: str | None = None
        self._http = httpx.AsyncClient(base_url=base_url.rstrip("/"), timeout=timeout)

    def _has_credentials(self) -> bool:
        return self._username is not None and self._password is not None

    async def _login(self) -> None:
        resp = await self._http.post(
            "/auth/login", json={"username": self._username, "password": self._password}
        )
        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), "POST", "/auth/login")
        data = _safe_json(resp) or {}
        self._token = data.get("access_token")
        self._refresh_token = data.get("refresh_token")

    async def _refresh(self) -> bool:
        if self._refresh_token:
            resp = await self._http.post("/auth/refresh", json={"refresh_token": self._refresh_token})
            if resp.status_code < 400:
                self._token = (_safe_json(resp) or {}).get("access_token")
                return True
        if self._has_credentials():
            await self._login()
            return True
        return False

    async def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        if self._token is None and self._has_credentials():
            await self._login()

        resp = await self._http.request(
            method, path, json=json, params=params,
            headers=build_auth_headers(self._token, self._api_key),
        )

        if resp.status_code == 401 and (self._refresh_token or self._has_credentials()):
            if await self._refresh():
                resp = await self._http.request(
                    method, path, json=json, params=params,
                    headers=build_auth_headers(self._token, self._api_key),
                )

        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), method, path)
        return ApiResult(data=_safe_json(resp), headers=resp.headers)

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncTransport":
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.aclose()
```

- [ ] **Step 7: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_async_transport.py -q` → 7 passed.

- [ ] **Step 8: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/pyproject.toml clients/python/uv.lock clients/python/src/graphdb_client/_transport.py clients/python/src/graphdb_client/aio/__init__.py clients/python/src/graphdb_client/aio/transport.py clients/python/tests/test_async_transport.py
git commit -m "feat(sdk): AsyncTransport + shared build_auth_headers + async test runner

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: async core resources — nodes + edges (full worked examples)

**Files:**
- Create: `clients/python/src/graphdb_client/aio/resources/__init__.py` (empty)
- Create: `aio/resources/nodes.py`, `aio/resources/edges.py`
- Test: `clients/python/tests/test_async_nodes_edges.py`

- [ ] **Step 1: write the failing test** — create `tests/test_async_nodes_edges.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.resources.edges import AsyncEdgesResource
from graphdb_client.aio.resources.nodes import AsyncNodesResource
from graphdb_client.aio.transport import AsyncTransport


def _nodes(base_url):
    return AsyncNodesResource(AsyncTransport(base_url, token="tok"))


def _edges(base_url):
    return AsyncEdgesResource(AsyncTransport(base_url, token="tok"))


@respx.mock
async def test_node_create_and_get(base_url):
    respx.post(f"{base_url}/nodes").mock(return_value=httpx.Response(
        201, json={"id": 5, "labels": ["P"], "properties": {"n": "a"}}))
    n = await _nodes(base_url).create(["P"], {"n": "a"})
    assert n.id == 5 and n.labels == ["P"]


@respx.mock
async def test_node_list_async_iterates_cursor(base_url):
    page1 = httpx.Response(200, json=[{"id": 1, "labels": ["P"], "properties": {}}],
                           headers={"X-Next-Cursor": "c2"})
    page2 = httpx.Response(200, json=[{"id": 2, "labels": ["P"], "properties": {}}])
    respx.get(f"{base_url}/nodes").mock(side_effect=[page1, page2])
    ids = [n.id async for n in _nodes(base_url).list(page_size=1)]
    assert ids == [1, 2]


@respx.mock
async def test_edge_create_and_delete(base_url):
    respx.post(f"{base_url}/edges").mock(return_value=httpx.Response(201, json={
        "id": 9, "from_node_id": 1, "to_node_id": 2, "type": "LINKS", "properties": {}, "weight": 1.0}))
    e = await _edges(base_url).create(1, 2, "LINKS", weight=1.0)
    assert e.id == 9 and e.type == "LINKS"
    respx.delete(f"{base_url}/edges/9").mock(return_value=httpx.Response(204))
    assert await _edges(base_url).delete(9) is None
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_async_nodes_edges.py -q`.

- [ ] **Step 3: create `aio/resources/__init__.py`** (empty file with a docstring `"""Async resource facades."""`).

- [ ] **Step 4: create `aio/resources/nodes.py`** (async mirror of `resources/nodes.py`; note `list` is an `AsyncIterator`):

```python
from __future__ import annotations

from typing import Any, AsyncIterator, Mapping, Sequence

from ..transport import AsyncTransport
from ...models import Node


class AsyncNodesResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._t = transport

    async def create(self, labels: Sequence[str], properties: Mapping[str, Any] | None = None) -> Node:
        res = await self._t.request("POST", "/nodes",
                                    json={"labels": list(labels), "properties": dict(properties or {})})
        return Node.from_dict(res.data)

    async def get(self, node_id: int) -> Node:
        res = await self._t.request("GET", f"/nodes/{node_id}")
        return Node.from_dict(res.data)

    async def update(self, node_id: int, properties: Mapping[str, Any]) -> Node:
        res = await self._t.request("PUT", f"/nodes/{node_id}", json={"properties": dict(properties)})
        return Node.from_dict(res.data)

    async def delete(self, node_id: int) -> None:
        await self._t.request("DELETE", f"/nodes/{node_id}")

    async def batch_create(self, nodes: Sequence[Mapping[str, Any]]) -> list[Node]:
        payload = {"nodes": [
            {"labels": list(n.get("labels", [])), "properties": dict(n.get("properties", {}))}
            for n in nodes
        ]}
        res = await self._t.request("POST", "/nodes/batch", json=payload)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    async def list(self, *, label: str | None = None, page_size: int = 100) -> AsyncIterator[Node]:
        """Yield every node (optionally filtered by label), auto-following X-Next-Cursor."""
        cursor: str | None = None
        prev_cursor: str | None = None
        while True:
            params: dict[str, Any] = {"limit": page_size}
            if label is not None:
                params["label"] = label
            if cursor is not None:
                params["cursor"] = cursor
            res = await self._t.request("GET", "/nodes", params=params)
            for d in res.data or []:
                yield Node.from_dict(d)
            cursor = res.headers.get("X-Next-Cursor")
            if not cursor or cursor == prev_cursor:
                return
            prev_cursor = cursor
```

- [ ] **Step 5: create `aio/resources/edges.py`** (async mirror of `resources/edges.py`):

```python
from __future__ import annotations

from typing import Any, Mapping, Sequence

from ..transport import AsyncTransport
from ...models import Edge


class AsyncEdgesResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._t = transport

    async def create(
        self,
        from_node_id: int,
        to_node_id: int,
        edge_type: str,
        *,
        properties: Mapping[str, Any] | None = None,
        weight: float = 0.0,
    ) -> Edge:
        res = await self._t.request("POST", "/edges", json={
            "from_node_id": from_node_id,
            "to_node_id": to_node_id,
            "type": edge_type,
            "properties": dict(properties or {}),
            "weight": weight,
        })
        return Edge.from_dict(res.data)

    async def get(self, edge_id: int) -> Edge:
        res = await self._t.request("GET", f"/edges/{edge_id}")
        return Edge.from_dict(res.data)

    async def update(
        self,
        edge_id: int,
        properties: Mapping[str, Any] | None = None,
        *,
        weight: float | None = None,
    ) -> Edge:
        body: dict[str, Any] = {}
        if properties is not None:
            body["properties"] = dict(properties)
        if weight is not None:
            body["weight"] = weight
        res = await self._t.request("PUT", f"/edges/{edge_id}", json=body)
        return Edge.from_dict(res.data)

    async def delete(self, edge_id: int) -> None:
        await self._t.request("DELETE", f"/edges/{edge_id}")

    async def batch_create(self, edges: Sequence[Mapping[str, Any]]) -> list[Edge]:
        payload = {"edges": [
            {
                "from_node_id": e["from_node_id"],
                "to_node_id": e["to_node_id"],
                "type": e["type"],
                "properties": dict(e.get("properties", {})),
                "weight": float(e.get("weight", 0.0)),
            }
            for e in edges
        ]}
        res = await self._t.request("POST", "/edges/batch", json=payload)
        return [Edge.from_dict(d) for d in (res.data.get("edges") or [])]
```

- [ ] **Step 6: run, confirm PASS (3 passed)** — `cd clients/python && uv run pytest tests/test_async_nodes_edges.py -q`.

- [ ] **Step 7: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/aio/resources/__init__.py clients/python/src/graphdb_client/aio/resources/nodes.py clients/python/src/graphdb_client/aio/resources/edges.py clients/python/tests/test_async_nodes_edges.py
git commit -m "feat(sdk): async nodes + edges resources (worked mirror + AsyncIterator list)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: async search + vector_indexes + algorithms

**Files:** Create `aio/resources/{search,vector_indexes,algorithms}.py`; Test `tests/test_async_search_vector_algo.py`.

Apply THE ASYNC-MIRROR TRANSFORM to each named sync file. Exact async signatures to produce (mirror bodies from the sync files):

- `aio/resources/search.py` → `AsyncSearchResource` (mirror `resources/search.py`):
  - `async def fulltext(self, query, *, limit=20, offset=0, labels=None, include_content=False, include_nodes=False) -> list[SearchHit]`
  - `async def hybrid(self, query, *, limit=20, offset=0, labels=None, alpha=None, include_content=False, include_nodes=False) -> HybridSearchResult`
  - imports: `from ..transport import AsyncTransport`, `from ...models import HybridSearchResult, SearchHit`
- `aio/resources/vector_indexes.py` → `AsyncVectorIndexesResource` (mirror `resources/vector_indexes.py`):
  - `async def create(self, property_name, dimensions, *, m=None, ef_construction=None, metric=None) -> VectorIndex`
  - `async def list(self) -> list[VectorIndex]`
  - `async def get(self, property_name) -> VectorIndex`
  - `async def delete(self, property_name) -> None`
  - imports: `from ...models import VectorIndex`
- `aio/resources/algorithms.py` → `AsyncAlgorithmsResource` (mirror `resources/algorithms.py`):
  - `async def run(self, algorithm, *, parameters=None) -> AlgorithmResult`
  - `async def shortest_path(self, start_node_id, end_node_id, *, max_depth=None) -> ShortestPath`
  - imports: `from ...models import AlgorithmResult, ShortestPath`

- [ ] **Step 1: write the failing smoke test** — `tests/test_async_search_vector_algo.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.resources.algorithms import AsyncAlgorithmsResource
from graphdb_client.aio.resources.search import AsyncSearchResource
from graphdb_client.aio.resources.vector_indexes import AsyncVectorIndexesResource
from graphdb_client.aio.transport import AsyncTransport


def _t(base_url):
    return AsyncTransport(base_url, token="tok")


@respx.mock
async def test_search_hybrid(base_url):
    respx.post(f"{base_url}/hybrid-search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 1, "score": 0.5}], "count": 1, "took_ms": 2, "degraded": "no-lsa-index"}))
    r = await AsyncSearchResource(_t(base_url)).hybrid("q")
    assert r.degraded == "no-lsa-index" and r.hits[0].node_id == 1


@respx.mock
async def test_search_fulltext(base_url):
    respx.post(f"{base_url}/search").mock(return_value=httpx.Response(
        200, json={"results": [{"node_id": 2, "score": 1.0}], "count": 1, "took_ms": 1}))
    hits = await AsyncSearchResource(_t(base_url)).fulltext("q")
    assert hits[0].node_id == 2


@respx.mock
async def test_vector_indexes_list(base_url):
    respx.get(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(
        200, json={"indexes": [{"property_name": "e", "dimensions": 3, "metric": "cosine"}], "count": 1}))
    got = await AsyncVectorIndexesResource(_t(base_url)).list()
    assert got[0].property_name == "e"


@respx.mock
async def test_algorithms_shortest_path(base_url):
    respx.post(f"{base_url}/shortest-path").mock(return_value=httpx.Response(
        200, json={"path": [1, 2], "length": 2, "found": True}))
    sp = await AsyncAlgorithmsResource(_t(base_url)).shortest_path(1, 2)
    assert sp.found is True and sp.path == [1, 2]
```

- [ ] **Step 2: run, confirm FAIL.**
- [ ] **Step 3: create the 3 files** by applying THE ASYNC-MIRROR TRANSFORM to `resources/search.py`, `resources/vector_indexes.py`, `resources/algorithms.py` with the signatures above.
- [ ] **Step 4: run, confirm PASS (4 passed).**
- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/aio/resources/search.py clients/python/src/graphdb_client/aio/resources/vector_indexes.py clients/python/src/graphdb_client/aio/resources/algorithms.py clients/python/tests/test_async_search_vector_algo.py
git commit -m "feat(sdk): async search/vector_indexes/algorithms resources

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: async tenants + api_keys

**Files:** Create `aio/resources/{tenants,api_keys}.py`; Test `tests/test_async_tenants_apikeys.py`.

Apply THE ASYNC-MIRROR TRANSFORM. Exact async signatures (mirror bodies from `resources/tenants.py`, `resources/api_keys.py`):

- `AsyncTenantsResource` (mirror `resources/tenants.py`): `create(self, id, name, *, description=None, quota=None, metadata=None) -> Tenant`, `list(self) -> list[Tenant]`, `get(self, tenant_id) -> Tenant`, `update(self, tenant_id, *, name=None, description=None, quota=None, metadata=None) -> Tenant`, `delete(self, tenant_id) -> None`, `usage(self, tenant_id) -> TenantUsage`, `suspend(self, tenant_id) -> None`, `activate(self, tenant_id) -> None`. imports: `from ...models import Tenant, TenantUsage`.
- `AsyncApiKeysResource` (mirror `resources/api_keys.py`): `create(self, name, *, permissions=None, expires_in=None, environment=None) -> CreatedAPIKey`, `list(self) -> list[APIKey]`, `revoke(self, key_id) -> None`. imports: `from ...models import APIKey, CreatedAPIKey`.

- [ ] **Step 1: failing smoke test** — `tests/test_async_tenants_apikeys.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.resources.api_keys import AsyncApiKeysResource
from graphdb_client.aio.resources.tenants import AsyncTenantsResource
from graphdb_client.aio.transport import AsyncTransport


def _t(base_url):
    return AsyncTransport(base_url, token="tok")


@respx.mock
async def test_tenant_create_and_usage(base_url):
    respx.post(f"{base_url}/api/v1/tenants").mock(return_value=httpx.Response(
        201, json={"id": "acme", "name": "Acme", "status": "active"}))
    t = await AsyncTenantsResource(_t(base_url)).create("acme", "Acme")
    assert t.id == "acme"
    respx.get(f"{base_url}/api/v1/tenants/acme/usage").mock(return_value=httpx.Response(
        200, json={"tenant_id": "acme", "node_count": 3}))
    u = await AsyncTenantsResource(_t(base_url)).usage("acme")
    assert u.node_count == 3


@respx.mock
async def test_tenant_suspend_returns_none(base_url):
    respx.post(f"{base_url}/api/v1/tenants/acme/suspend").mock(return_value=httpx.Response(200, json={}))
    assert await AsyncTenantsResource(_t(base_url)).suspend("acme") is None


@respx.mock
async def test_api_key_create_and_list(base_url):
    respx.post(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(201, json={
        "key": "secret", "id": "k1", "name": "ci", "prefix": "p"}))
    c = await AsyncApiKeysResource(_t(base_url)).create("ci")
    assert c.key == "secret"
    respx.get(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(200, json={
        "keys": [{"id": "k1", "name": "ci", "prefix": "p", "permissions": [], "revoked": False}], "count": 1}))
    got = await AsyncApiKeysResource(_t(base_url)).list()
    assert got[0].id == "k1"
```

- [ ] **Step 2: run, confirm FAIL.**
- [ ] **Step 3: create the 2 files** via the transform with the signatures above.
- [ ] **Step 4: run, confirm PASS (3 passed).**
- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/aio/resources/tenants.py clients/python/src/graphdb_client/aio/resources/api_keys.py clients/python/tests/test_async_tenants_apikeys.py
git commit -m "feat(sdk): async tenants + api_keys resources

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: async security + compliance

**Files:** Create `aio/resources/{security,compliance}.py`; Test `tests/test_async_security_compliance.py`.

Apply THE ASYNC-MIRROR TRANSFORM. These are dict/list-returning (no models). Mirror `resources/security.py` and `resources/compliance.py`, including their module-level `_as_dict` helper (copy it into each async file, same as the sync files do). Exact async signatures:

- `AsyncSecurityResource` (mirror `resources/security.py`): `rotate_keys(self) -> dict[str, Any]`, `key_info(self) -> dict[str, Any]`, `audit_logs(self, *, limit=None) -> dict[str, Any]`, `audit_export(self) -> list[dict[str, Any]]` (POST), `health(self) -> dict[str, Any]`.
- `AsyncComplianceResource` (mirror `resources/compliance.py`): `audit_log(self, *, user_id=None, username=None, action=None, resource_type=None, status=None, start_time=None, end_time=None, limit=None, offset=None) -> dict[str, Any]`, `get_masking_policy(self, tenant) -> dict[str, Any]` (path `/v1/compliance/masking-policy/{tenant}`), `set_masking_policy(self, properties, *, auto_detect=False) -> dict[str, Any]`.

- [ ] **Step 1: failing smoke test** — `tests/test_async_security_compliance.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.resources.compliance import AsyncComplianceResource
from graphdb_client.aio.resources.security import AsyncSecurityResource
from graphdb_client.aio.transport import AsyncTransport


def _t(base_url):
    return AsyncTransport(base_url, token="tok")


@respx.mock
async def test_security_health(base_url):
    respx.get(f"{base_url}/api/v1/security/health").mock(return_value=httpx.Response(
        200, json={"status": "healthy"}))
    assert (await AsyncSecurityResource(_t(base_url)).health())["status"] == "healthy"


@respx.mock
async def test_security_audit_export_is_post_and_returns_list(base_url):
    route = respx.post(f"{base_url}/api/v1/security/audit/export").mock(return_value=httpx.Response(
        200, json=[{"action": "login"}]))
    out = await AsyncSecurityResource(_t(base_url)).audit_export()
    assert isinstance(out, list) and out[0]["action"] == "login" and route.called


@respx.mock
async def test_compliance_get_masking_policy_tenant_path(base_url):
    respx.get(f"{base_url}/v1/compliance/masking-policy/acme").mock(return_value=httpx.Response(
        200, json={"properties": {"email": "hash"}, "auto_detect": True}))
    out = await AsyncComplianceResource(_t(base_url)).get_masking_policy("acme")
    assert out["properties"]["email"] == "hash"


@respx.mock
async def test_compliance_audit_log_params(base_url):
    route = respx.get(f"{base_url}/v1/compliance/audit-log").mock(return_value=httpx.Response(
        200, json={"events": [], "count": 0}))
    await AsyncComplianceResource(_t(base_url)).audit_log(username="alice", limit=5)
    p = route.calls.last.request.url.params
    assert p.get("username") == "alice" and p.get("limit") == "5" and "user_id" not in p
```

- [ ] **Step 2: run, confirm FAIL.**
- [ ] **Step 3: create the 2 files** via the transform with the signatures above (copy the `_as_dict` helper into each).
- [ ] **Step 4: run, confirm PASS (4 passed).**
- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/aio/resources/security.py clients/python/src/graphdb_client/aio/resources/compliance.py clients/python/tests/test_async_security_compliance.py
git commit -m "feat(sdk): async security + compliance resources

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: AsyncGraphDBClient + exports + README + final gate

**Files:** Create `aio/client.py`; Modify `aio/__init__.py`, `src/graphdb_client/__init__.py`, `README.md`; Test `tests/test_async_client.py`.

- [ ] **Step 1: failing test** — `tests/test_async_client.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client import AsyncGraphDBClient
from graphdb_client.aio.resources.nodes import AsyncNodesResource
from graphdb_client.aio.resources.tenants import AsyncTenantsResource


def test_async_client_wires_resources():
    c = AsyncGraphDBClient("https://graphdb.test", token="tok")
    assert isinstance(c.nodes, AsyncNodesResource)
    assert isinstance(c.tenants, AsyncTenantsResource)
    # spot-check a couple of the others exist
    assert c.search is not None and c.security is not None and c.compliance is not None


@respx.mock
async def test_async_query_and_graphql(base_url):
    respx.post(f"{base_url}/query").mock(return_value=httpx.Response(
        200, json={"columns": ["n"], "rows": [{"n": 1}], "count": 1, "time": "1ms"}))
    async with AsyncGraphDBClient(base_url, token="tok") as db:
        r = await db.query("MATCH (n) RETURN n")
        assert r.columns == ["n"] and r.count == 1
    respx.post(f"{base_url}/graphql").mock(return_value=httpx.Response(200, json={"data": {"x": 1}}))
    async with AsyncGraphDBClient(base_url, token="tok") as db:
        out = await db.graphql("{ x }")
        assert out["data"]["x"] == 1


@respx.mock
async def test_async_traverse_and_embeddings(base_url):
    respx.post(f"{base_url}/traverse").mock(return_value=httpx.Response(
        200, json={"nodes": [{"id": 1, "labels": [], "properties": {}}]}))
    respx.post(f"{base_url}/v1/embeddings").mock(return_value=httpx.Response(200, json={
        "object": "list", "model": "lsa",
        "data": [{"object": "embedding", "embedding": [0.1], "index": 0}],
        "usage": {}}))
    async with AsyncGraphDBClient(base_url, token="tok") as db:
        ns = await db.traverse(1)
        assert ns[0].id == 1
        emb = await db.embeddings("hi")
        assert emb.vectors == [[0.1]]
```

- [ ] **Step 2: run, confirm FAIL.**

- [ ] **Step 3: create `aio/client.py`** — async mirror of `src/graphdb_client/client.py`. Wire all 9 async resources; the 6 top-level methods are the async mirror of the sync client's `traverse`/`vector_search`/`retrieve`/`embeddings`/`query`/`graphql` (apply THE ASYNC-MIRROR TRANSFORM to the sync client method bodies — same signatures/paths/body-building, `await self._raw.request(...)`); async lifecycle. Full code:

```python
from __future__ import annotations

from types import TracebackType
from typing import Any, Mapping, Sequence

from ..models import EmbeddingsResult, Node, QueryResult, RetrieveResult, SearchResult
from .resources.algorithms import AsyncAlgorithmsResource
from .resources.api_keys import AsyncApiKeysResource
from .resources.compliance import AsyncComplianceResource
from .resources.edges import AsyncEdgesResource
from .resources.nodes import AsyncNodesResource
from .resources.search import AsyncSearchResource
from .resources.security import AsyncSecurityResource
from .resources.tenants import AsyncTenantsResource
from .resources.vector_indexes import AsyncVectorIndexesResource
from .transport import AsyncTransport


class AsyncGraphDBClient:
    """Async drop-in for GraphDBClient: same surface, awaitable."""

    def __init__(
        self,
        base_url: str,
        *,
        token: str | None = None,
        api_key: str | None = None,
        username: str | None = None,
        password: str | None = None,
        timeout: float = 30.0,
    ) -> None:
        self._raw = AsyncTransport(
            base_url, token=token, api_key=api_key,
            username=username, password=password, timeout=timeout,
        )
        self.nodes = AsyncNodesResource(self._raw)
        self.edges = AsyncEdgesResource(self._raw)
        self.search = AsyncSearchResource(self._raw)
        self.vector_indexes = AsyncVectorIndexesResource(self._raw)
        self.algorithms = AsyncAlgorithmsResource(self._raw)
        self.tenants = AsyncTenantsResource(self._raw)
        self.api_keys = AsyncApiKeysResource(self._raw)
        self.security = AsyncSecurityResource(self._raw)
        self.compliance = AsyncComplianceResource(self._raw)

    async def traverse(
        self,
        start_node_id: int,
        *,
        max_depth: int = 1,
        direction: str | None = None,
        edge_types: Sequence[str] | None = None,
    ) -> list[Node]:
        body: dict[str, Any] = {"start_node_id": start_node_id, "max_depth": max_depth}
        if direction is not None:
            body["direction"] = direction
        if edge_types is not None:
            body["edge_types"] = list(edge_types)
        res = await self._raw.request("POST", "/traverse", json=body)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    async def vector_search(
        self,
        property_name: str,
        query: Sequence[float],
        *,
        k: int = 10,
        ef: int | None = None,
        filter_labels: Sequence[str] | None = None,
        include_nodes: bool = False,
    ) -> list[SearchResult]:
        body: dict[str, Any] = {
            "property_name": property_name,
            "query_vector": list(query),
            "k": k,
            "include_nodes": include_nodes,
        }
        if ef is not None:
            body["ef"] = ef
        if filter_labels is not None:
            body["filter_labels"] = list(filter_labels)
        res = await self._raw.request("POST", "/vector-search", json=body)
        return [SearchResult.from_dict(d) for d in (res.data.get("results") or [])]

    async def retrieve(
        self,
        query: str,
        *,
        k: int | None = None,
        max_tokens: int | None = None,
        max_hops: int | None = None,
        alpha: float | None = None,
        beta: float | None = None,
        tau: float | None = None,
        labels: Sequence[str] | None = None,
        include_node: bool = False,
    ) -> RetrieveResult:
        body: dict[str, Any] = {"query": query}
        for name, val in (("k", k), ("max_tokens", max_tokens), ("max_hops", max_hops),
                          ("alpha", alpha), ("beta", beta), ("tau", tau)):
            if val is not None:
                body[name] = val
        if labels is not None:
            body["labels"] = list(labels)
        if include_node:
            body["include_node"] = True
        res = await self._raw.request("POST", "/v1/retrieve", json=body)
        return RetrieveResult.from_dict(res.data)

    async def embeddings(
        self,
        input: str | Sequence[str],
        *,
        model: str | None = None,
        dimensions: int | None = None,
    ) -> EmbeddingsResult:
        items = [input] if isinstance(input, str) else list(input)
        body: dict[str, Any] = {"input": items}
        if model is not None:
            body["model"] = model
        if dimensions is not None:
            body["dimensions"] = dimensions
        res = await self._raw.request("POST", "/v1/embeddings", json=body)
        return EmbeddingsResult.from_dict(res.data)

    async def query(
        self,
        cypher: str,
        *,
        parameters: Mapping[str, Any] | None = None,
        timeout_seconds: int | None = None,
    ) -> QueryResult:
        body: dict[str, Any] = {"query": cypher}
        if parameters is not None:
            body["parameters"] = dict(parameters)
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        res = await self._raw.request("POST", "/query", json=body)
        return QueryResult.from_dict(res.data)

    async def graphql(
        self,
        query: str,
        *,
        variables: Mapping[str, Any] | None = None,
        operation_name: str | None = None,
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"query": query}
        if variables is not None:
            body["variables"] = dict(variables)
        if operation_name is not None:
            body["operationName"] = operation_name
        res = await self._raw.request("POST", "/graphql", json=body)
        return res.data if isinstance(res.data, dict) else {}

    async def aclose(self) -> None:
        await self._raw.aclose()

    async def __aenter__(self) -> "AsyncGraphDBClient":
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()
```

- [ ] **Step 4: update `aio/__init__.py`** to export the client:
```python
from __future__ import annotations

from .client import AsyncGraphDBClient

__all__ = ["AsyncGraphDBClient"]
```

- [ ] **Step 5: re-export from the top-level `src/graphdb_client/__init__.py`.** Add `from .aio import AsyncGraphDBClient` (after the `from .client import GraphDBClient` line) and add `"AsyncGraphDBClient"` to `__all__` (right after `"GraphDBClient"`). Verify: `cd clients/python && uv run python -c "from graphdb_client import AsyncGraphDBClient; print(AsyncGraphDBClient)"`.

- [ ] **Step 6: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_async_client.py -q`.

- [ ] **Step 7: README.** Append to `clients/python/README.md`:

````markdown
## Async

`AsyncGraphDBClient` is a drop-in async twin of `GraphDBClient` — same resources
and methods, awaitable:

```python
import asyncio
from graphdb_client import AsyncGraphDBClient

async def main():
    async with AsyncGraphDBClient("http://localhost:8080", token=TOKEN) as db:
        node = await db.nodes.create(["Person"], {"name": "Ada"})
        async for n in db.nodes.list(label="Person"):
            print(n.id)
        hits = await db.search.hybrid("graph database")
        rows = (await db.query("MATCH (n) RETURN n LIMIT 1")).rows

asyncio.run(main())
```
````

- [ ] **Step 8: full gate:**
```
cd clients/python && uv run pytest -q          # expect ~113 passed, 2 skipped (92 prior + ~21 async)
cd clients/python && uv run ruff check .
cd clients/python && uv run mypy src
```
All must pass.

- [ ] **Step 9: commit:**
```
git add clients/python/src/graphdb_client/aio/client.py clients/python/src/graphdb_client/aio/__init__.py clients/python/src/graphdb_client/__init__.py clients/python/README.md clients/python/tests/test_async_client.py
git commit -m "feat(sdk): AsyncGraphDBClient + top-level export + README async section

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementer

- **The async resources are a mechanical mirror of the sync ones — read the named sync file and apply THE ASYNC-MIRROR TRANSFORM.** Do NOT change signatures, paths, body-building, or model mapping; only `async def` + `await` + the import-depth/`AsyncTransport`/class-name changes. The sync file is the source of truth.
- **Do not modify the sync client, sync resources, or `errors.py`.** The only sync change is the behavior-preserving `build_auth_headers` extraction in `_transport.py` (Task 1) — the existing `test_transport.py`/`test_auth.py` must stay green.
- **mypy is `--strict`** — annotate everything; `list` is `AsyncIterator[Node]`; the `_as_dict` guard keeps dict returns typed.
- **`asyncio_mode = "auto"`** lets `async def test_*` run without decorators; if a test needs the event loop and isn't picked up, confirm the pyproject change landed and `uv sync` ran.
- After all tasks: final whole-implementation review, then `superpowers:finishing-a-development-branch`.
```
