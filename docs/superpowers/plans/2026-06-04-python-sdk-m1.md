# graphdb Python SDK — M1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a first-party, sync Python SDK (`graphdb-client`) covering graphdb's core, contract-pinned resources (nodes, edges, traverse, vector-search) with full-surface raw access, in `clients/python/`.

**Architecture:** Layered — a `Transport` (httpx wrapper: auth, error mapping, raw access, 401-refresh) under hand-written dataclass `models` under Pythonic resource facades, assembled by `GraphDBClient`. Full surface is reachable now via `client._raw.request(...)`; ergonomic facades cover the CC1–CC9 core.

**Tech Stack:** Python 3.9+, **`uv`** (project + venv + tool runner), `httpx` (only runtime dep), `pytest` + `respx` (tests), `ruff` + `mypy` (CI), `datamodel-code-generator` (dev-only, generates `_generated/` models). Models are **stdlib dataclasses** (spec D1).

**Spec:** `docs/superpowers/specs/2026-06-04-python-sdk-design.md`

**Tooling convention (uv):** the project is uv-managed. The env is created/synced with `uv sync` (run from `clients/python/`), which installs the project editable + the dev dependency group. **Every tool command below runs via `uv run`** — for brevity the task steps write `pytest …` / `mypy` / `ruff …` / `datamodel-codegen …`, but each must be invoked as `uv run pytest …` etc. Do not create a venv with `python -m venv` or install with `pip`; use `uv`.

---

## File structure (locked)

```
clients/python/
  pyproject.toml                      # PEP 621; runtime dep httpx; dev deps; ruff/mypy config
  Makefile                            # generate / lint / typecheck / test
  README.md                           # quickstart + auth + raw escape hatch + IT opt-in
  src/graphdb_client/
    __init__.py                       # public exports
    errors.py                         # exception hierarchy + from_response()
    models.py                         # Node, Edge, SearchResult dataclasses + from_dict
    _transport.py                     # Transport, ApiResult; auth, error-map, raw, refresh
    client.py                         # GraphDBClient; traverse(), vector_search()
    resources/
      __init__.py
      nodes.py                        # NodesResource
      edges.py                        # EdgesResource
    _generated/
      __init__.py
      models.py                       # generated dataclasses (full surface; for M2)
  tests/
    __init__.py
    conftest.py                       # base_url + transport/client fixtures
    test_errors.py
    test_models.py
    test_transport.py
    test_auth.py
    test_nodes.py
    test_edges.py
    test_client.py
    integration/
      __init__.py
      test_smoke.py                   # opt-in (GRAPHDB_SDK_IT=1), real binary
```

**Key signatures (consistent across tasks):**
- `errors.from_response(status_code: int, body, method: str, path: str) -> GraphDBError`
- `ApiResult(data: Any, headers: Mapping[str, str])` (dataclass)
- `Transport(base_url, *, token=None, api_key=None, username=None, password=None, timeout=30.0, max_retries=2)`; `.request(method, path, *, json=None, params=None) -> ApiResult`; `.close()`
- `Node(id: int, labels: list[str], properties: dict[str, Any])`; `Node.from_dict(d) -> Node`
- `Edge(id, from_node_id, to_node_id, type, properties, weight)`; `Edge.from_dict`
- `SearchResult(node_id: int, distance: float, score: float, node: Node | None)`; `SearchResult.from_dict`
- `GraphDBClient(base_url, *, token=None, api_key=None, username=None, password=None, timeout=30.0, max_retries=2)` → `.nodes`, `.edges`, `.traverse(...)`, `.vector_search(...)`, `.close()`, context manager

Every module starts with `from __future__ import annotations` so `X | None` annotations work on 3.9.

---

## Task 1: Package scaffold

**Files:**
- Create: `clients/python/pyproject.toml`
- Create: `clients/python/src/graphdb_client/__init__.py`
- Create: `clients/python/tests/__init__.py`, `clients/python/tests/conftest.py`
- Create: `clients/python/Makefile`

- [ ] **Step 1: Write `pyproject.toml`**

```toml
[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[project]
name = "graphdb-client"
version = "0.1.0"
description = "First-party Python client for graphdb"
readme = "README.md"
requires-python = ">=3.9"
license = { text = "Apache-2.0" }
dependencies = ["httpx>=0.27"]

[dependency-groups]
dev = ["pytest>=8", "respx>=0.21", "ruff>=0.6", "mypy>=1.11", "datamodel-code-generator>=0.25"]

[tool.hatch.build.targets.wheel]
packages = ["src/graphdb_client"]

[tool.ruff]
line-length = 100
src = ["src", "tests"]

[tool.mypy]
python_version = "3.9"
packages = ["graphdb_client"]
strict = true

[tool.pytest.ini_options]
testpaths = ["tests"]
addopts = "-q"
```

- [ ] **Step 2: Write the package + test init files + .gitignore**

`src/graphdb_client/__init__.py`:
```python
from __future__ import annotations

__version__ = "0.1.0"
```

`clients/python/.gitignore`:
```gitignore
.venv/
dist/
__pycache__/
*.egg-info/
.mypy_cache/
.ruff_cache/
.pytest_cache/
```

`tests/__init__.py`: (empty file)

`tests/conftest.py`:
```python
from __future__ import annotations

import pytest

BASE_URL = "https://graphdb.test"


@pytest.fixture
def base_url() -> str:
    return BASE_URL
```

- [ ] **Step 3: Write `Makefile`**

```makefile
.PHONY: install generate lint typecheck test
install:
	uv sync
generate:
	uv run datamodel-codegen --input ../../docs/internals/openapi.yaml --input-file-type openapi \
	  --output-model-type dataclasses.dataclass \
	  --output src/graphdb_client/_generated/models.py
lint:
	uv run ruff check src tests
typecheck:
	uv run mypy
test:
	uv run pytest
```

- [ ] **Step 4: Sync the uv env and verify the empty suite runs**

Run: `cd clients/python && uv sync && uv run pytest`
Expected: `uv sync` creates `.venv` + `uv.lock` and installs the project editable + dev group; `uv run pytest` reports `no tests ran` (exit 0) — packaging is valid, imports resolve.

- [ ] **Step 5: Commit**

```bash
git add clients/python/pyproject.toml clients/python/uv.lock clients/python/Makefile clients/python/.gitignore clients/python/src clients/python/tests
git commit -m "feat(py-sdk): package scaffold (clients/python, uv-managed)"
```

---

## Task 2: Error hierarchy

**Files:**
- Create: `clients/python/src/graphdb_client/errors.py`
- Test: `clients/python/tests/test_errors.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

import pytest

from graphdb_client.errors import (
    AuthError, ConflictError, GraphDBError, NotFoundError,
    RateLimitError, ServerError, ValidationError, from_response,
)


@pytest.mark.parametrize("status,cls", [
    (400, ValidationError), (401, AuthError), (404, NotFoundError),
    (409, ConflictError), (429, RateLimitError), (500, ServerError), (503, ServerError),
])
def test_from_response_maps_status(status, cls):
    err = from_response(status, {"error": "boom"}, "GET", "/nodes/1")
    assert isinstance(err, cls)
    assert isinstance(err, GraphDBError)
    assert err.status_code == status
    assert err.method == "GET"
    assert err.path == "/nodes/1"
    assert "boom" in str(err)


def test_unmapped_4xx_is_base_error():
    err = from_response(418, {}, "GET", "/x")
    assert type(err) is GraphDBError
    assert err.status_code == 418
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_errors.py -v`
Expected: FAIL — `ModuleNotFoundError: graphdb_client.errors`

- [ ] **Step 3: Write the implementation**

`src/graphdb_client/errors.py`:
```python
from __future__ import annotations

from typing import Any


class GraphDBError(Exception):
    """Base error for all graphdb client failures."""

    def __init__(
        self,
        message: str,
        *,
        status_code: int | None = None,
        body: Any = None,
        method: str | None = None,
        path: str | None = None,
    ) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.body = body
        self.method = method
        self.path = path


class ValidationError(GraphDBError):
    """400 — request rejected by validation."""


class AuthError(GraphDBError):
    """401 — missing/invalid/expired credentials (after refresh attempt)."""


class NotFoundError(GraphDBError):
    """404 — node/edge not found, or cross-tenant (unified error)."""


class ConflictError(GraphDBError):
    """409 — unique-constraint violation."""


class RateLimitError(GraphDBError):
    """429 — rate limited."""


class ServerError(GraphDBError):
    """5xx — server-side failure."""


_STATUS_MAP: dict[int, type[GraphDBError]] = {
    400: ValidationError,
    401: AuthError,
    404: NotFoundError,
    409: ConflictError,
    429: RateLimitError,
}


def _extract_message(body: Any) -> str:
    if isinstance(body, dict):
        for key in ("error", "message", "detail"):
            if key in body and body[key]:
                return str(body[key])
    if isinstance(body, str) and body:
        return body
    return "request failed"


def from_response(status_code: int, body: Any, method: str, path: str) -> GraphDBError:
    if status_code >= 500:
        cls: type[GraphDBError] = ServerError
    else:
        cls = _STATUS_MAP.get(status_code, GraphDBError)
    msg = f"{method} {path} -> {status_code}: {_extract_message(body)}"
    return cls(msg, status_code=status_code, body=body, method=method, path=path)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_errors.py -v`
Expected: PASS (8 cases)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/errors.py clients/python/tests/test_errors.py
git commit -m "feat(py-sdk): error hierarchy + status mapping"
```

---

## Task 3: Core models

**Files:**
- Create: `clients/python/src/graphdb_client/models.py`
- Test: `clients/python/tests/test_models.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

from graphdb_client.models import Edge, Node, SearchResult


def test_node_from_dict():
    n = Node.from_dict({"id": 7, "labels": ["Person"], "properties": {"_key": "p:1", "age": 30}})
    assert n.id == 7
    assert n.labels == ["Person"]
    assert n.properties["_key"] == "p:1"


def test_node_from_dict_tolerates_missing_optional_fields():
    n = Node.from_dict({"id": 1})
    assert n.labels == []
    assert n.properties == {}


def test_edge_from_dict():
    e = Edge.from_dict({
        "id": 3, "from_node_id": 1, "to_node_id": 2,
        "type": "KNOWS", "properties": {"since": 2020}, "weight": 1.5,
    })
    assert (e.from_node_id, e.to_node_id, e.type, e.weight) == (1, 2, "KNOWS", 1.5)


def test_search_result_from_dict_with_embedded_node():
    r = SearchResult.from_dict({
        "node_id": 9, "distance": 0.1, "score": 0.9,
        "node": {"id": 9, "labels": ["Doc"], "properties": {}},
    })
    assert r.node_id == 9 and r.score == 0.9
    assert r.node is not None and r.node.id == 9


def test_search_result_from_dict_without_node():
    r = SearchResult.from_dict({"node_id": 9, "distance": 0.1, "score": 0.9})
    assert r.node is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_models.py -v`
Expected: FAIL — `ModuleNotFoundError: graphdb_client.models`

- [ ] **Step 3: Write the implementation**

`src/graphdb_client/models.py`:
```python
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Mapping


@dataclass
class Node:
    id: int
    labels: list[str] = field(default_factory=list)
    properties: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "Node":
        return cls(
            id=int(d["id"]),
            labels=list(d.get("labels") or []),
            properties=dict(d.get("properties") or {}),
        )


@dataclass
class Edge:
    id: int
    from_node_id: int
    to_node_id: int
    type: str
    properties: dict[str, Any] = field(default_factory=dict)
    weight: float = 0.0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "Edge":
        return cls(
            id=int(d["id"]),
            from_node_id=int(d["from_node_id"]),
            to_node_id=int(d["to_node_id"]),
            type=str(d.get("type", "")),
            properties=dict(d.get("properties") or {}),
            weight=float(d.get("weight", 0.0)),
        )


@dataclass
class SearchResult:
    node_id: int
    distance: float
    score: float
    node: Node | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "SearchResult":
        raw_node = d.get("node")
        return cls(
            node_id=int(d["node_id"]),
            distance=float(d.get("distance", 0.0)),
            score=float(d.get("score", 0.0)),
            node=Node.from_dict(raw_node) if raw_node else None,
        )
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_models.py -v`
Expected: PASS (5 cases)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/models.py clients/python/tests/test_models.py
git commit -m "feat(py-sdk): core dataclass models (Node, Edge, SearchResult)"
```

---

## Task 4: Transport — requests, headers, error mapping, raw

**Files:**
- Create: `clients/python/src/graphdb_client/_transport.py`
- Test: `clients/python/tests/test_transport.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._transport import ApiResult, Transport
from graphdb_client.errors import NotFoundError, ValidationError


@respx.mock
def test_request_injects_bearer_token(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}})
    )
    t = Transport(base_url, token="tok")
    res = t.request("GET", "/nodes/1")
    assert isinstance(res, ApiResult)
    assert res.data["id"] == 1
    assert route.calls.last.request.headers["Authorization"] == "Bearer tok"
    t.close()


@respx.mock
def test_request_injects_api_key_header(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1})
    )
    t = Transport(base_url, api_key="k-123")
    t.request("GET", "/nodes/1")
    assert route.calls.last.request.headers["X-API-Key"] == "k-123"
    t.close()


@respx.mock
def test_request_exposes_response_headers(base_url):
    respx.get(f"{base_url}/nodes").mock(
        return_value=httpx.Response(200, json=[], headers={"X-Next-Cursor": "42"})
    )
    t = Transport(base_url, token="tok")
    res = t.request("GET", "/nodes")
    assert res.headers.get("X-Next-Cursor") == "42"
    t.close()


@respx.mock
def test_error_mapping(base_url):
    respx.get(f"{base_url}/nodes/9").mock(return_value=httpx.Response(404, json={"error": "not found"}))
    respx.post(f"{base_url}/nodes").mock(return_value=httpx.Response(400, json={"error": "bad"}))
    t = Transport(base_url, token="tok")
    with pytest.raises(NotFoundError):
        t.request("GET", "/nodes/9")
    with pytest.raises(ValidationError):
        t.request("POST", "/nodes", json={})
    t.close()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_transport.py -v`
Expected: FAIL — `ModuleNotFoundError: graphdb_client._transport`

- [ ] **Step 3: Write the implementation**

`src/graphdb_client/_transport.py`:
```python
from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Mapping

import httpx

from .errors import from_response


@dataclass
class ApiResult:
    data: Any
    headers: Mapping[str, str]


class Transport:
    """Single choke point for every HTTP request: auth, error mapping, raw access."""

    def __init__(
        self,
        base_url: str,
        *,
        token: str | None = None,
        api_key: str | None = None,
        username: str | None = None,
        password: str | None = None,
        timeout: float = 30.0,
        max_retries: int = 2,
    ) -> None:
        self._token = token
        self._api_key = api_key
        self._username = username
        self._password = password
        self._refresh_token: str | None = None
        self._max_retries = max_retries
        self._http = httpx.Client(base_url=base_url.rstrip("/"), timeout=timeout)

    def _auth_headers(self) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self._token:
            headers["Authorization"] = f"Bearer {self._token}"
        elif self._api_key:
            headers["X-API-Key"] = self._api_key
        return headers

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        resp = self._http.request(
            method, path, json=json, params=params, headers=self._auth_headers()
        )
        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), method, path)
        return ApiResult(data=_safe_json(resp), headers=resp.headers)

    def close(self) -> None:
        self._http.close()


def _safe_json(resp: httpx.Response) -> Any:
    if not resp.content:
        return None
    try:
        return resp.json()
    except ValueError:
        return resp.text
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_transport.py -v`
Expected: PASS (4 cases)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/_transport.py clients/python/tests/test_transport.py
git commit -m "feat(py-sdk): Transport with auth headers, error mapping, raw access"
```

---

## Task 5: Transport — login + 401 refresh

**Files:**
- Modify: `clients/python/src/graphdb_client/_transport.py`
- Test: `clients/python/tests/test_auth.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._transport import Transport
from graphdb_client.errors import AuthError


@respx.mock
def test_login_on_first_request_when_credentials_given(base_url):
    login = respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "acc", "refresh_token": "ref"})
    )
    nodes = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1})
    )
    t = Transport(base_url, username="u", password="p")
    t.request("GET", "/nodes/1")
    assert login.called
    assert nodes.calls.last.request.headers["Authorization"] == "Bearer acc"
    t.close()


@respx.mock
def test_401_triggers_refresh_then_retry_succeeds(base_url):
    respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "old", "refresh_token": "ref"})
    )
    refresh = respx.post(f"{base_url}/auth/refresh").mock(
        return_value=httpx.Response(200, json={"access_token": "new"})
    )
    # First call 401 (expired), second call (after refresh) 200.
    nodes = respx.get(f"{base_url}/nodes/1").mock(
        side_effect=[httpx.Response(401, json={"error": "expired"}),
                     httpx.Response(200, json={"id": 1})]
    )
    t = Transport(base_url, username="u", password="p")
    res = t.request("GET", "/nodes/1")
    assert res.data["id"] == 1
    assert refresh.called
    assert nodes.calls[-1].request.headers["Authorization"] == "Bearer new"
    t.close()


@respx.mock
def test_second_401_raises_autherror(base_url):
    respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "old", "refresh_token": "ref"})
    )
    respx.post(f"{base_url}/auth/refresh").mock(
        return_value=httpx.Response(200, json={"access_token": "new"})
    )
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(401, json={"error": "nope"}))
    t = Transport(base_url, username="u", password="p")
    with pytest.raises(AuthError):
        t.request("GET", "/nodes/1")
    t.close()


@respx.mock
def test_static_token_401_does_not_refresh(base_url):
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(401, json={"error": "nope"}))
    refresh = respx.post(f"{base_url}/auth/refresh")
    t = Transport(base_url, token="static")
    with pytest.raises(AuthError):
        t.request("GET", "/nodes/1")
    assert not refresh.called  # no credentials → no refresh path
    t.close()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_auth.py -v`
Expected: FAIL — login not attempted / no refresh logic (AssertionError on `login.called`)

- [ ] **Step 3: Modify the implementation**

In `_transport.py`, add a `_login` method, a `_refresh` method, lazy-login, and a one-shot refresh-retry in `request`. Replace the `request` method and add helpers:

```python
    def _has_credentials(self) -> bool:
        return self._username is not None and self._password is not None

    def _login(self) -> None:
        resp = self._http.post("/auth/login", json={"username": self._username, "password": self._password})
        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), "POST", "/auth/login")
        data = resp.json()
        self._token = data.get("access_token")
        self._refresh_token = data.get("refresh_token")

    def _refresh(self) -> bool:
        """Return True if the token was refreshed. Falls back to re-login."""
        if self._refresh_token:
            resp = self._http.post("/auth/refresh", json={"refresh_token": self._refresh_token})
            if resp.status_code < 400:
                self._token = resp.json().get("access_token")
                return True
        if self._has_credentials():
            self._login()
            return True
        return False

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        # Lazy login: if we have credentials but no token yet, get one.
        if self._token is None and self._has_credentials():
            self._login()

        resp = self._http.request(method, path, json=json, params=params, headers=self._auth_headers())

        # One-shot refresh-and-retry on 401 when we can refresh.
        if resp.status_code == 401 and (self._refresh_token or self._has_credentials()):
            if self._refresh():
                resp = self._http.request(
                    method, path, json=json, params=params, headers=self._auth_headers()
                )

        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), method, path)
        return ApiResult(data=_safe_json(resp), headers=resp.headers)
```

(Delete the old `request` body; keep `_auth_headers`, `__init__`, `close`, `_safe_json`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_auth.py tests/test_transport.py -v`
Expected: PASS (all auth + transport cases — the static-token transport tests still pass since no credentials → no refresh)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/_transport.py clients/python/tests/test_auth.py
git commit -m "feat(py-sdk): optional login + 401 refresh-and-retry"
```

---

## Task 6: NodesResource (incl. CC7 batch echo, CC8 auto-pagination)

**Files:**
- Create: `clients/python/src/graphdb_client/resources/__init__.py` (empty)
- Create: `clients/python/src/graphdb_client/resources/nodes.py`
- Test: `clients/python/tests/test_nodes.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.nodes import NodesResource


def _res(base_url):
    return NodesResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_and_get(base_url):
    respx.post(f"{base_url}/nodes").mock(
        return_value=httpx.Response(201, json={"id": 5, "labels": ["Person"], "properties": {"name": "A"}})
    )
    n = _res(base_url).create(["Person"], {"name": "A"})
    assert n.id == 5 and n.labels == ["Person"] and n.properties["name"] == "A"


@respx.mock
def test_batch_create_echoes_properties_for_key_reconcile(base_url):
    # CC7: response carries echoed properties; reconcile by _key, not index.
    respx.post(f"{base_url}/nodes/batch").mock(return_value=httpx.Response(201, json={
        "nodes": [
            {"id": 11, "labels": ["P"], "properties": {"_key": "p:1"}},
            {"id": 12, "labels": ["P"], "properties": {"_key": "p:2"}},
        ],
        "created": 2, "time": "1ms",
    }))
    nodes = _res(base_url).batch_create([
        {"labels": ["P"], "properties": {"_key": "p:1"}},
        {"labels": ["P"], "properties": {"_key": "p:2"}},
    ])
    by_key = {n.properties["_key"]: n.id for n in nodes}
    assert by_key == {"p:1": 11, "p:2": 12}


@respx.mock
def test_list_auto_paginates_across_cursor(base_url):
    # CC8: follow X-Next-Cursor to completion; yield nodes with properties.
    page1 = httpx.Response(200, json=[
        {"id": 1, "labels": ["P"], "properties": {"_key": "a"}},
        {"id": 2, "labels": ["P"], "properties": {"_key": "b"}},
    ], headers={"X-Next-Cursor": "2"})
    page2 = httpx.Response(200, json=[
        {"id": 3, "labels": ["P"], "properties": {"_key": "c"}},
    ])  # no cursor header => last page
    route = respx.get(f"{base_url}/nodes").mock(side_effect=[page1, page2])

    got = list(_res(base_url).list(label="P", page_size=2))
    assert [n.id for n in got] == [1, 2, 3]
    assert [n.properties["_key"] for n in got] == ["a", "b", "c"]
    # Second request carried the cursor.
    assert route.calls[-1].request.url.params["cursor"] == "2"
    assert route.calls[0].request.url.params["label"] == "P"


@respx.mock
def test_delete(base_url):
    route = respx.delete(f"{base_url}/nodes/7").mock(return_value=httpx.Response(200))
    _res(base_url).delete(7)
    assert route.called
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_nodes.py -v`
Expected: FAIL — `ModuleNotFoundError: graphdb_client.resources.nodes`

- [ ] **Step 3: Write the implementation**

`src/graphdb_client/resources/__init__.py`: (empty file)

`src/graphdb_client/resources/nodes.py`:
```python
from __future__ import annotations

from typing import Any, Iterator, Mapping, Sequence

from .._transport import Transport
from ..models import Node


class NodesResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(self, labels: Sequence[str], properties: Mapping[str, Any] | None = None) -> Node:
        res = self._t.request("POST", "/nodes",
                              json={"labels": list(labels), "properties": dict(properties or {})})
        return Node.from_dict(res.data)

    def get(self, node_id: int) -> Node:
        res = self._t.request("GET", f"/nodes/{node_id}")
        return Node.from_dict(res.data)

    def update(self, node_id: int, properties: Mapping[str, Any]) -> Node:
        res = self._t.request("PUT", f"/nodes/{node_id}", json={"properties": dict(properties)})
        return Node.from_dict(res.data)

    def delete(self, node_id: int) -> None:
        self._t.request("DELETE", f"/nodes/{node_id}")

    def batch_create(self, nodes: Sequence[Mapping[str, Any]]) -> list[Node]:
        payload = {"nodes": [
            {"labels": list(n.get("labels", [])), "properties": dict(n.get("properties", {}))}
            for n in nodes
        ]}
        res = self._t.request("POST", "/nodes/batch", json=payload)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    def list(self, *, label: str | None = None, page_size: int = 100) -> Iterator[Node]:
        """Yield every node (optionally filtered by label), auto-following X-Next-Cursor."""
        cursor: str | None = None
        while True:
            params: dict[str, Any] = {"limit": page_size}
            if label is not None:
                params["label"] = label
            if cursor is not None:
                params["cursor"] = cursor
            res = self._t.request("GET", "/nodes", params=params)
            for d in res.data or []:
                yield Node.from_dict(d)
            cursor = res.headers.get("X-Next-Cursor")
            if not cursor:
                return
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_nodes.py -v`
Expected: PASS (4 cases)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/resources clients/python/tests/test_nodes.py
git commit -m "feat(py-sdk): NodesResource (CC7 batch echo, CC8 auto-pagination)"
```

---

## Task 7: EdgesResource

**Files:**
- Create: `clients/python/src/graphdb_client/resources/edges.py`
- Test: `clients/python/tests/test_edges.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.edges import EdgesResource


def _res(base_url):
    return EdgesResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_edge(base_url):
    route = respx.post(f"{base_url}/edges").mock(return_value=httpx.Response(201, json={
        "id": 3, "from_node_id": 1, "to_node_id": 2, "type": "KNOWS", "properties": {}, "weight": 1.0,
    }))
    e = _res(base_url).create(1, 2, "KNOWS", weight=1.0)
    assert (e.id, e.from_node_id, e.to_node_id, e.type) == (3, 1, 2, "KNOWS")
    body = route.calls.last.request
    assert b'"from_node_id": 1' in body.content or b'"from_node_id":1' in body.content


@respx.mock
def test_batch_create_edges(base_url):
    respx.post(f"{base_url}/edges/batch").mock(return_value=httpx.Response(201, json={
        "edges": [{"id": 1, "from_node_id": 1, "to_node_id": 2, "type": "R", "properties": {}, "weight": 0.0}],
        "created": 1, "time": "1ms",
    }))
    edges = _res(base_url).batch_create([{"from_node_id": 1, "to_node_id": 2, "type": "R"}])
    assert len(edges) == 1 and edges[0].type == "R"


@respx.mock
def test_get_and_delete(base_url):
    respx.get(f"{base_url}/edges/3").mock(return_value=httpx.Response(200, json={
        "id": 3, "from_node_id": 1, "to_node_id": 2, "type": "KNOWS", "properties": {}, "weight": 1.0}))
    d = respx.delete(f"{base_url}/edges/3").mock(return_value=httpx.Response(200))
    r = _res(base_url)
    assert r.get(3).id == 3
    r.delete(3)
    assert d.called
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_edges.py -v`
Expected: FAIL — `ModuleNotFoundError: graphdb_client.resources.edges`

- [ ] **Step 3: Write the implementation**

`src/graphdb_client/resources/edges.py`:
```python
from __future__ import annotations

from typing import Any, Mapping, Sequence

from .._transport import Transport
from ..models import Edge


class EdgesResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        from_node_id: int,
        to_node_id: int,
        edge_type: str,
        *,
        properties: Mapping[str, Any] | None = None,
        weight: float = 0.0,
    ) -> Edge:
        res = self._t.request("POST", "/edges", json={
            "from_node_id": from_node_id,
            "to_node_id": to_node_id,
            "type": edge_type,
            "properties": dict(properties or {}),
            "weight": weight,
        })
        return Edge.from_dict(res.data)

    def get(self, edge_id: int) -> Edge:
        res = self._t.request("GET", f"/edges/{edge_id}")
        return Edge.from_dict(res.data)

    def update(self, edge_id: int, properties: Mapping[str, Any]) -> Edge:
        res = self._t.request("PUT", f"/edges/{edge_id}", json={"properties": dict(properties)})
        return Edge.from_dict(res.data)

    def delete(self, edge_id: int) -> None:
        self._t.request("DELETE", f"/edges/{edge_id}")

    def batch_create(self, edges: Sequence[Mapping[str, Any]]) -> list[Edge]:
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
        res = self._t.request("POST", "/edges/batch", json=payload)
        return [Edge.from_dict(d) for d in (res.data.get("edges") or [])]
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_edges.py -v`
Expected: PASS (3 cases)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/resources/edges.py clients/python/tests/test_edges.py
git commit -m "feat(py-sdk): EdgesResource"
```

---

## Task 8: GraphDBClient (assembly + traverse + vector_search + raw)

**Files:**
- Create: `clients/python/src/graphdb_client/client.py`
- Test: `clients/python/tests/test_client.py`

- [ ] **Step 1: Write the failing test**

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client.client import GraphDBClient


@respx.mock
def test_traverse_returns_nodes(base_url):
    # CC9: outgoing neighbors at depth.
    respx.post(f"{base_url}/traverse").mock(return_value=httpx.Response(200, json={
        "nodes": [
            {"id": 1, "labels": ["Process"], "properties": {}},
            {"id": 2, "labels": ["File"], "properties": {}},
        ],
        "count": 2, "time": "1ms",
    }))
    with GraphDBClient(base_url, token="tok") as c:
        nodes = c.traverse(1, max_depth=1)
    assert {n.id for n in nodes} == {1, 2}


@respx.mock
def test_vector_search_returns_results(base_url):
    respx.post(f"{base_url}/vector-search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 9, "distance": 0.0, "score": 1.0}],
        "count": 1, "took_ms": 1,
    }))
    with GraphDBClient(base_url, token="tok") as c:
        res = c.vector_search("embedding", [1.0, 0.0, 0.0], k=1, filter_labels=["Document"])
    assert res[0].node_id == 9 and res[0].score == 1.0


@respx.mock
def test_raw_escape_hatch(base_url):
    respx.post(f"{base_url}/hybrid-search").mock(return_value=httpx.Response(200, json={"results": []}))
    with GraphDBClient(base_url, token="tok") as c:
        out = c._raw.request("POST", "/hybrid-search", json={"query": "x"})
    assert out.data == {"results": []}


@respx.mock
def test_context_manager_closes(base_url):
    c = GraphDBClient(base_url, token="tok")
    c.close()  # idempotent, no error
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/test_client.py -v`
Expected: FAIL — `ModuleNotFoundError: graphdb_client.client`

- [ ] **Step 3: Write the implementation**

`src/graphdb_client/client.py`:
```python
from __future__ import annotations

from types import TracebackType
from typing import Any, Sequence

from ._transport import Transport
from .models import Node, SearchResult
from .resources.edges import EdgesResource
from .resources.nodes import NodesResource


class GraphDBClient:
    def __init__(
        self,
        base_url: str,
        *,
        token: str | None = None,
        api_key: str | None = None,
        username: str | None = None,
        password: str | None = None,
        timeout: float = 30.0,
        max_retries: int = 2,
    ) -> None:
        self._raw = Transport(
            base_url, token=token, api_key=api_key, username=username,
            password=password, timeout=timeout, max_retries=max_retries,
        )
        self.nodes = NodesResource(self._raw)
        self.edges = EdgesResource(self._raw)

    def traverse(
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
        res = self._raw.request("POST", "/traverse", json=body)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    def vector_search(
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
        res = self._raw.request("POST", "/vector-search", json=body)
        return [SearchResult.from_dict(d) for d in (res.data.get("results") or [])]

    def close(self) -> None:
        self._raw.close()

    def __enter__(self) -> "GraphDBClient":
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self.close()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/test_client.py -v`
Expected: PASS (4 cases)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/client.py clients/python/tests/test_client.py
git commit -m "feat(py-sdk): GraphDBClient assembly + traverse + vector_search + raw"
```

---

## Task 9: Public exports + generated-models tooling + README

**Files:**
- Modify: `clients/python/src/graphdb_client/__init__.py`
- Create: `clients/python/src/graphdb_client/_generated/__init__.py` (empty), `clients/python/src/graphdb_client/_generated/models.py` (generated)
- Create: `clients/python/README.md`

- [ ] **Step 1: Write the public exports**

`src/graphdb_client/__init__.py`:
```python
from __future__ import annotations

from .client import GraphDBClient
from .errors import (
    AuthError, ConflictError, GraphDBError, NotFoundError,
    RateLimitError, ServerError, ValidationError,
)
from .models import Edge, Node, SearchResult

__version__ = "0.1.0"
__all__ = [
    "GraphDBClient", "Node", "Edge", "SearchResult",
    "GraphDBError", "ValidationError", "AuthError", "NotFoundError",
    "ConflictError", "RateLimitError", "ServerError",
]
```

- [ ] **Step 2: Verify the package imports cleanly**

Run: `uv run python -c "import graphdb_client; print(graphdb_client.__all__)"`
Expected: prints the `__all__` list, no ImportError.

- [ ] **Step 3: Generate the full-surface models (establish M2 tooling)**

Run: `cd clients/python && touch src/graphdb_client/_generated/__init__.py && make generate`
Expected: `src/graphdb_client/_generated/models.py` is created with dataclasses for the OpenAPI schemas. If `datamodel-codegen` reports spec issues, record them as graphdb follow-up issues (do NOT hand-edit the generated file).

- [ ] **Step 4: Verify the generated module imports**

Run: `uv run python -c "import graphdb_client._generated.models"`
Expected: no error (generated file is importable). Commit it as-is.

- [ ] **Step 5: Write `README.md`**

````markdown
# graphdb-client

First-party Python client for [graphdb](https://github.com/dd0wney/graphdb). Sync, `httpx`-only.

## Install
```bash
pip install graphdb-client
```

## Quickstart
```python
from graphdb_client import GraphDBClient

with GraphDBClient("https://your-graphdb", token="YOUR_TOKEN") as db:
    alice = db.nodes.create(["Person"], {"name": "Alice", "_key": "p:alice"})

    # Batch import; reconcile assigned IDs by your own correlation key.
    created = db.nodes.batch_create([{"labels": ["Person"], "properties": {"_key": "p:bob"}}])
    by_key = {n.properties["_key"]: n.id for n in created}

    # List every Person — pagination is followed automatically.
    for node in db.nodes.list(label="Person"):
        print(node.id, node.properties)

    hits = db.vector_search("embedding", [0.1, 0.2, 0.3], k=5, filter_labels=["Document"])
    neighbours = db.traverse(alice.id, max_depth=1)
```

## Auth
- Static token / API key: `GraphDBClient(url, token=...)` or `GraphDBClient(url, api_key=...)`.
- Auto login + refresh: `GraphDBClient(url, username=..., password=...)`.

## Endpoints not yet faceted
Every endpoint is reachable via the raw escape hatch:
```python
res = db._raw.request("POST", "/hybrid-search", json={"query": "..."})
res.data  # parsed JSON
```

## Tests
- Setup: `uv sync`.
- Unit: `make test` (= `uv run pytest`; mock transport, no server).
- Integration: `GRAPHDB_SDK_IT=1 GRAPHDB_SDK_URL=http://localhost:8080 GRAPHDB_SDK_TOKEN=... uv run pytest tests/integration`.
````

- [ ] **Step 6: Commit**

```bash
git add clients/python/src/graphdb_client/__init__.py clients/python/src/graphdb_client/_generated clients/python/README.md
git commit -m "feat(py-sdk): public exports, generated-models tooling, README"
```

---

## Task 10: CI + opt-in integration smoke

**Files:**
- Create: `clients/python/tests/integration/__init__.py` (empty), `clients/python/tests/integration/test_smoke.py`
- Create: `.github/workflows/python-sdk.yml`

- [ ] **Step 1: Write the opt-in integration smoke (mirrors CC7/CC8/CC9)**

`tests/integration/test_smoke.py`:
```python
from __future__ import annotations

import os
import uuid

import pytest

from graphdb_client import GraphDBClient

pytestmark = pytest.mark.skipif(
    os.environ.get("GRAPHDB_SDK_IT") != "1",
    reason="integration test; set GRAPHDB_SDK_IT=1 (+ GRAPHDB_SDK_URL/TOKEN) to run",
)


@pytest.fixture
def client():
    url = os.environ.get("GRAPHDB_SDK_URL", "http://localhost:8080")
    token = os.environ.get("GRAPHDB_SDK_TOKEN")
    with GraphDBClient(url, token=token) as c:
        yield c


def test_smoke_batch_list_traverse(client):
    run = uuid.uuid4().hex[:8]
    label = f"SDKSmoke_{run}"
    # CC7: batch create, reconcile by _key.
    created = client.nodes.batch_create([
        {"labels": [label], "properties": {"_key": f"{run}:a"}},
        {"labels": [label], "properties": {"_key": f"{run}:b"}},
    ])
    by_key = {n.properties["_key"]: n.id for n in created}
    assert set(by_key) == {f"{run}:a", f"{run}:b"}

    # CC8: list with a small page size returns all, with properties.
    listed = list(client.nodes.list(label=label, page_size=1))
    assert {n.properties["_key"] for n in listed} == {f"{run}:a", f"{run}:b"}

    # CC9: edge + depth-1 traverse returns the neighbour.
    client.edges.create(by_key[f"{run}:a"], by_key[f"{run}:b"], "LINKS")
    neighbours = client.traverse(by_key[f"{run}:a"], max_depth=1)
    assert by_key[f"{run}:b"] in {n.id for n in neighbours}
```

- [ ] **Step 2: Run it (skipped without the env flag)**

Run: `uv run pytest tests/integration -v`
Expected: SKIPPED (reason printed). With a local server: `GRAPHDB_SDK_IT=1 GRAPHDB_SDK_TOKEN=... uv run pytest tests/integration` → PASS.

- [ ] **Step 3: Write the CI workflow**

`.github/workflows/python-sdk.yml`:
```yaml
name: Python SDK
on:
  pull_request:
    paths: ["clients/python/**", ".github/workflows/python-sdk.yml"]
  push:
    branches: [main]
    paths: ["clients/python/**"]
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: clients/python
    strategy:
      matrix:
        python-version: ["3.9", "3.12"]
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v5
        with:
          enable-cache: true
      - run: uv sync --python ${{ matrix.python-version }}
      - run: uv run ruff check src tests
      - run: uv run mypy
      - run: uv run pytest
```

- [ ] **Step 4: Verify the full unit suite + lint + types locally**

Run: `cd clients/python && uv run ruff check src tests && uv run mypy && uv run pytest`
Expected: ruff clean, mypy clean (strict), all unit tests PASS (integration SKIPPED).

- [ ] **Step 5: Commit**

```bash
git add clients/python/tests/integration .github/workflows/python-sdk.yml
git commit -m "feat(py-sdk): CI workflow + opt-in integration smoke (CC7/CC8/CC9)"
```

---

## Task 11: PyPI publish workflow (on tag)

**Files:**
- Create: `.github/workflows/python-sdk-publish.yml`

- [ ] **Step 1: Write the publish workflow**

`.github/workflows/python-sdk-publish.yml`:
```yaml
name: Python SDK Publish
on:
  push:
    tags: ["py-sdk-v*"]
permissions:
  id-token: write   # PyPI trusted publishing (OIDC)
jobs:
  build-and-publish:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: clients/python
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v5
      - run: uv build
      - run: uv publish   # OIDC trusted publishing; no token needed
```

- [ ] **Step 2: Verify the wheel builds**

Run: `cd clients/python && uv build`
Expected: `dist/graphdb_client-0.1.0-py3-none-any.whl` + sdist produced, no errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/python-sdk-publish.yml
git commit -m "feat(py-sdk): PyPI publish workflow on py-sdk-v* tag"
```

---

## Self-review (completed)

- **Spec coverage:** scaffold/packaging (T1, §8) · errors (T2, §5.5) · models-dataclasses/D1 (T3) · transport+raw+error-map (T4, §5.1) · auth token+login+refresh (T5, §2) · nodes incl. CC7 batch echo + CC8 auto-pagination (T6, §6) · edges (T7) · client+traverse CC9+vector_search CC1/2/5 (T8, §6) · generated-models tooling (T9, §5.2) · unit+opt-in integration tests (T6–T10, §7) · CI (T10, §8) · PyPI publish (T11, §8). All M1 spec sections map to a task.
- **Placeholder scan:** no TBD/TODO; every code step has complete code; commands have expected output.
- **Type consistency:** `Transport.request(...) -> ApiResult` used uniformly; `ApiResult.data`/`.headers` consistent across nodes/edges/client; `*.from_dict` used everywhere models are parsed; `GraphDBClient._raw` is the `Transport` (matches T8 test + README raw example); resource constructors take the transport positionally (T6/T7 tests + T8 assembly agree).
- **Note:** `nodes.update`/`edges.update` assume PUT returns the updated entity body; if a deployment returns 204-no-body, `from_dict(None)` would fail — verified `updateNode` is `PUT` (handlers_nodes.go:158) and returns the node. If integration reveals no body, return `None` from `update` and adjust the (currently unwritten) update test. Flagged, not blocking M1's tested surface.
