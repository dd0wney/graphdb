# Python SDK M4a Implementation Plan (retry/backoff)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add transport-level retry/backoff (exponential + full jitter, `Retry-After`-aware, idempotency-safe) to both the sync `Transport` and async `AsyncTransport`, configurable per-client and on by default (`max_retries=2`).

**Architecture:** Approach B. Pure, sync/async-shared retry-policy helpers live in a new `_retry.py`. Each transport's existing per-request body (lazy-login + request + 401-refresh) becomes a private `_attempt(...)` returning the raw `httpx.Response`; `request(...)` wraps it in a retry loop that decides retryability from the status/exception and sleeps `compute_delay(...)` between attempts. The 401-refresh stays *inside* an attempt (it is not a backoff retry). `RetryConfig` is threaded through both clients.

**Tech Stack:** Python 3.9+, `httpx`, stdlib `dataclasses`/`random`/`time`/`asyncio`/`email.utils`; tests use `respx` + `pytest` (+`pytest-asyncio`, already configured); `ruff` + `mypy --strict`. All commands run from `clients/python/` via `uv run`.

**Spec:** `docs/superpowers/specs/2026-06-06-python-sdk-m4-design.md` (§3).

---

## File structure

- **Create** `src/graphdb_client/_retry.py` — `RetryConfig`, `parse_retry_after`, `is_retryable`, `compute_delay`, `coerce_retry_config`. Pure (no network); the single source of retry-policy truth for both transports + clients.
- **Modify** `src/graphdb_client/_transport.py` — `Transport` gains `retries` param + `_attempt` + retry loop in `request`.
- **Modify** `src/graphdb_client/aio/transport.py` — `AsyncTransport` mirror (await + `asyncio.sleep`).
- **Modify** `src/graphdb_client/client.py` + `aio/client.py` — `retries: RetryConfig | int | None = 2` param, coerced and passed to the transport.
- **Modify** `src/graphdb_client/__init__.py` — export `RetryConfig`.
- **Modify** `README.md` — "Resilience" section (the one behavioral change: retry on by default).
- **Tests** `tests/test_retry.py` (pure helpers), `tests/test_retry_transport.py` (sync loop), `tests/test_async_retry.py` (async loop), `tests/test_retry_client.py` (wiring).

**Test-speed convention:** all loop/behavior tests construct `RetryConfig(max_retries=2, backoff_factor=0.0, max_backoff=0.0, respect_retry_after=False)` so `compute_delay` returns `0.0` and `time.sleep(0)`/`asyncio.sleep(0)` are instant — no sleep monkeypatching needed. The jitter/`Retry-After` math is covered by direct pure-helper tests in Task 1.

---

### Task 1: `_retry.py` pure helpers

**Files:**
- Create: `clients/python/src/graphdb_client/_retry.py`
- Test: `clients/python/tests/test_retry.py`

- [ ] **Step 1: write the failing test** — create `tests/test_retry.py`:

```python
from __future__ import annotations

import httpx

from graphdb_client._retry import (
    RetryConfig,
    coerce_retry_config,
    compute_delay,
    is_retryable,
    parse_retry_after,
)


def test_coerce_retry_config():
    assert coerce_retry_config(None).max_retries == 0
    assert coerce_retry_config(0).max_retries == 0
    assert coerce_retry_config(5).max_retries == 5
    cfg = RetryConfig(max_retries=3)
    assert coerce_retry_config(cfg) is cfg


def test_is_retryable_transport_errors():
    cfg = RetryConfig()
    # connect failures never reached the server -> safe on ANY method
    assert is_retryable("POST", None, httpx.ConnectError("x"), cfg) is True
    # other transport errors (read timeout) may have been processed -> idempotent only
    assert is_retryable("POST", None, httpx.ReadTimeout("x"), cfg) is False
    assert is_retryable("GET", None, httpx.ReadTimeout("x"), cfg) is True


def test_is_retryable_statuses():
    cfg = RetryConfig()
    assert is_retryable("POST", 429, None, cfg) is True   # rate-limit: not processed, any method
    assert is_retryable("GET", 503, None, cfg) is True
    assert is_retryable("POST", 503, None, cfg) is False  # 5xx idempotent-only
    assert is_retryable("GET", 500, None, cfg) is False   # 500 not in default retry_statuses
    assert is_retryable("GET", 404, None, cfg) is False
    assert is_retryable("GET", None, None, cfg) is False


def test_compute_delay_bounds_and_zero():
    zero = RetryConfig(backoff_factor=0.0, max_backoff=0.0)
    assert compute_delay(0, zero, None) == 0.0
    cfg = RetryConfig(backoff_factor=0.5, max_backoff=30.0)
    for attempt in range(4):
        d = compute_delay(attempt, cfg, None)
        assert 0.0 <= d <= min(30.0, 0.5 * (2 ** attempt))


def test_compute_delay_retry_after_wins_and_clamps():
    cfg = RetryConfig(backoff_factor=10.0, max_backoff=5.0)
    assert compute_delay(0, cfg, 2.0) == 2.0      # honored over computed backoff
    assert compute_delay(0, cfg, 99.0) == 5.0      # clamped to max_backoff


def test_parse_retry_after():
    assert parse_retry_after(None) is None
    assert parse_retry_after("") is None
    assert parse_retry_after("3") == 3.0
    assert parse_retry_after("not-a-date") is None
    # an HTTP-date in the past -> 0.0 (never negative)
    assert parse_retry_after("Wed, 21 Oct 2015 07:28:00 GMT") == 0.0
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_retry.py -q` → ImportError.

- [ ] **Step 3: create `src/graphdb_client/_retry.py`:**

```python
from __future__ import annotations

import random
from dataclasses import dataclass
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime

import httpx


@dataclass(frozen=True)
class RetryConfig:
    max_retries: int = 2
    backoff_factor: float = 0.5
    max_backoff: float = 30.0
    retry_statuses: frozenset[int] = frozenset({429, 502, 503, 504})
    retry_methods: frozenset[str] = frozenset({"GET", "PUT", "DELETE", "HEAD", "OPTIONS"})
    respect_retry_after: bool = True


def coerce_retry_config(retries: "RetryConfig | int | None") -> RetryConfig:
    """Normalize the client-facing ``retries`` param. None/0 -> disabled."""
    if retries is None:
        return RetryConfig(max_retries=0)
    if isinstance(retries, int):
        return RetryConfig(max_retries=retries)
    return retries


def is_retryable(
    method: str,
    status: int | None,
    exc: BaseException | None,
    config: RetryConfig,
) -> bool:
    """Decide whether a failed attempt should be retried (before checking attempt budget)."""
    if exc is not None:
        # Connection never established -> request not processed -> safe on any method.
        if isinstance(exc, httpx.ConnectError):
            return True
        # Other transport errors (e.g. read/write timeout) may have been applied
        # server-side -> only retry idempotent methods.
        return method.upper() in config.retry_methods
    if status is None:
        return False
    if status == 429:
        return True  # rate-limited: request not processed, safe on any method
    if status in config.retry_statuses:
        return method.upper() in config.retry_methods
    return False


def compute_delay(attempt: int, config: RetryConfig, retry_after: float | None) -> float:
    """Seconds to wait before the next attempt. Retry-After wins (clamped); else full-jitter exp."""
    if retry_after is not None:
        return min(retry_after, config.max_backoff)
    upper = min(config.max_backoff, config.backoff_factor * (2 ** attempt))
    if upper <= 0:
        return 0.0
    return random.uniform(0, upper)


def parse_retry_after(value: str | None) -> float | None:
    """Parse a Retry-After header value (delta-seconds or HTTP-date) into seconds, or None."""
    if not value:
        return None
    value = value.strip()
    if value.isdigit():
        return float(value)
    try:
        dt = parsedate_to_datetime(value)
    except (TypeError, ValueError):
        return None
    if dt is None:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return max(0.0, (dt - datetime.now(timezone.utc)).total_seconds())
```

- [ ] **Step 4: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_retry.py -q` → 6 passed.

- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/_retry.py clients/python/tests/test_retry.py
git commit -m "feat(sdk): retry-policy pure helpers (RetryConfig, is_retryable, compute_delay)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: fold retry into sync `Transport`

**Files:**
- Modify: `clients/python/src/graphdb_client/_transport.py`
- Test: `clients/python/tests/test_retry_transport.py`

- [ ] **Step 1: write the failing test** — create `tests/test_retry_transport.py`:

```python
from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._retry import RetryConfig
from graphdb_client._transport import Transport
from graphdb_client.errors import ServerError

FAST = RetryConfig(max_retries=2, backoff_factor=0.0, max_backoff=0.0, respect_retry_after=False)
OK = {"id": 1, "labels": [], "properties": {}}


def _t(base_url, retries=FAST):
    return Transport(base_url, token="tok", retries=retries)


@respx.mock
def test_retries_429_then_succeeds(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(429), httpx.Response(200, json=OK)])
    res = _t(base_url).request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2


@respx.mock
def test_exhausts_then_raises(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(503), httpx.Response(503)])
    with pytest.raises(ServerError):
        _t(base_url).request("GET", "/nodes/1")
    assert route.call_count == 3  # 1 + max_retries(2)


@respx.mock
def test_retries_connect_error_then_succeeds(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.ConnectError("boom"), httpx.Response(200, json=OK)])
    res = _t(base_url).request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2


@respx.mock
def test_post_5xx_not_retried(base_url):
    route = respx.post(f"{base_url}/nodes").mock(side_effect=[
        httpx.Response(503), httpx.Response(201, json=OK)])
    with pytest.raises(ServerError):
        _t(base_url).request("POST", "/nodes", json={})
    assert route.call_count == 1  # non-idempotent 5xx: no retry


@respx.mock
def test_disabled_when_retries_zero(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    with pytest.raises(ServerError):
        _t(base_url, retries=RetryConfig(max_retries=0)).request("GET", "/nodes/1")
    assert route.call_count == 1
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_retry_transport.py -q` → `TypeError` (`Transport` has no `retries` kwarg).

- [ ] **Step 3: edit `src/graphdb_client/_transport.py`.** Add imports at the top (after the existing `import httpx`):

```python
import time
```

and (after `from .errors import from_response`):

```python
from ._retry import RetryConfig, compute_delay, is_retryable, parse_retry_after
```

Add `retries` to `Transport.__init__` signature (after `timeout: float = 30.0,`):

```python
        retries: RetryConfig | None = None,
```

and at the end of `__init__` body (after `self._http = httpx.Client(...)`):

```python
        self._retries = retries if retries is not None else RetryConfig()
```

Replace the existing `request` method with a private `_attempt` (its old body, minus the raise/return) plus a new looping `request`:

```python
    def _attempt(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> httpx.Response:
        if self._token is None and self._has_credentials():
            self._login()

        resp = self._http.request(
            method, path, json=json, params=params, headers=self._auth_headers()
        )

        if resp.status_code == 401 and (self._refresh_token or self._has_credentials()):
            if self._refresh():
                resp = self._http.request(
                    method, path, json=json, params=params, headers=self._auth_headers()
                )
        return resp

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        attempt = 0
        while True:
            try:
                resp = self._attempt(method, path, json=json, params=params)
            except httpx.TransportError as exc:
                if attempt < self._retries.max_retries and is_retryable(
                    method, None, exc, self._retries
                ):
                    time.sleep(compute_delay(attempt, self._retries, None))
                    attempt += 1
                    continue
                raise

            if (
                resp.status_code >= 400
                and attempt < self._retries.max_retries
                and is_retryable(method, resp.status_code, None, self._retries)
            ):
                retry_after = (
                    parse_retry_after(resp.headers.get("Retry-After"))
                    if self._retries.respect_retry_after
                    else None
                )
                time.sleep(compute_delay(attempt, self._retries, retry_after))
                attempt += 1
                continue

            if resp.status_code >= 400:
                raise from_response(resp.status_code, _safe_json(resp), method, path)
            return ApiResult(data=_safe_json(resp), headers=resp.headers)
```

- [ ] **Step 4: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_retry_transport.py tests/test_transport.py tests/test_auth.py -q` → all pass (new retry tests + existing transport/auth tests unchanged).

- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/_transport.py clients/python/tests/test_retry_transport.py
git commit -m "feat(sdk): retry loop in sync Transport (idempotency-safe, Retry-After aware)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: fold retry into async `AsyncTransport`

**Files:**
- Modify: `clients/python/src/graphdb_client/aio/transport.py`
- Test: `clients/python/tests/test_async_retry.py`

- [ ] **Step 1: write the failing test** — create `tests/test_async_retry.py` (the async mirror of Task 2's tests):

```python
from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._retry import RetryConfig
from graphdb_client.aio.transport import AsyncTransport
from graphdb_client.errors import ServerError

FAST = RetryConfig(max_retries=2, backoff_factor=0.0, max_backoff=0.0, respect_retry_after=False)
OK = {"id": 1, "labels": [], "properties": {}}


def _t(base_url, retries=FAST):
    return AsyncTransport(base_url, token="tok", retries=retries)


@respx.mock
async def test_retries_429_then_succeeds(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(429), httpx.Response(200, json=OK)])
    res = await _t(base_url).request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2


@respx.mock
async def test_exhausts_then_raises(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(503), httpx.Response(503)])
    with pytest.raises(ServerError):
        await _t(base_url).request("GET", "/nodes/1")
    assert route.call_count == 3


@respx.mock
async def test_retries_connect_error_then_succeeds(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.ConnectError("boom"), httpx.Response(200, json=OK)])
    res = await _t(base_url).request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2


@respx.mock
async def test_post_5xx_not_retried(base_url):
    route = respx.post(f"{base_url}/nodes").mock(side_effect=[
        httpx.Response(503), httpx.Response(201, json=OK)])
    with pytest.raises(ServerError):
        await _t(base_url).request("POST", "/nodes", json={})
    assert route.call_count == 1


@respx.mock
async def test_disabled_when_retries_zero(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    with pytest.raises(ServerError):
        await _t(base_url, retries=RetryConfig(max_retries=0)).request("GET", "/nodes/1")
    assert route.call_count == 1
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_async_retry.py -q` → `TypeError` (no `retries` kwarg).

- [ ] **Step 3: edit `src/graphdb_client/aio/transport.py`.** Add imports at the top (after `import httpx`):

```python
import asyncio
```

and (after `from ..errors import from_response`):

```python
from .._retry import RetryConfig, compute_delay, is_retryable, parse_retry_after
```

Add `retries` to `AsyncTransport.__init__` signature (after `timeout: float = 30.0,`):

```python
        retries: RetryConfig | None = None,
```

and at the end of `__init__` (after `self._http = httpx.AsyncClient(...)`):

```python
        self._retries = retries if retries is not None else RetryConfig()
```

Replace the existing `request` with `_attempt` + a looping `request` (async mirror of Task 2):

```python
    async def _attempt(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> httpx.Response:
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
        return resp

    async def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        attempt = 0
        while True:
            try:
                resp = await self._attempt(method, path, json=json, params=params)
            except httpx.TransportError as exc:
                if attempt < self._retries.max_retries and is_retryable(
                    method, None, exc, self._retries
                ):
                    await asyncio.sleep(compute_delay(attempt, self._retries, None))
                    attempt += 1
                    continue
                raise

            if (
                resp.status_code >= 400
                and attempt < self._retries.max_retries
                and is_retryable(method, resp.status_code, None, self._retries)
            ):
                retry_after = (
                    parse_retry_after(resp.headers.get("Retry-After"))
                    if self._retries.respect_retry_after
                    else None
                )
                await asyncio.sleep(compute_delay(attempt, self._retries, retry_after))
                attempt += 1
                continue

            if resp.status_code >= 400:
                raise from_response(resp.status_code, _safe_json(resp), method, path)
            return ApiResult(data=_safe_json(resp), headers=resp.headers)
```

- [ ] **Step 4: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_async_retry.py tests/test_async_transport.py -q` → all pass.

- [ ] **Step 5: gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/aio/transport.py clients/python/tests/test_async_retry.py
git commit -m "feat(sdk): retry loop in AsyncTransport (mirror of sync)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: thread `retries` through both clients + export + README

**Files:**
- Modify: `clients/python/src/graphdb_client/client.py`
- Modify: `clients/python/src/graphdb_client/aio/client.py`
- Modify: `clients/python/src/graphdb_client/__init__.py`
- Modify: `clients/python/README.md`
- Test: `clients/python/tests/test_retry_client.py`

- [ ] **Step 1: write the failing test** — create `tests/test_retry_client.py`:

```python
from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client import GraphDBClient, RetryConfig
from graphdb_client.errors import ServerError

OK = {"id": 1, "labels": [], "properties": {}}
FAST = RetryConfig(max_retries=2, backoff_factor=0.0, max_backoff=0.0, respect_retry_after=False)


def test_retry_config_exported():
    assert RetryConfig(max_retries=1).max_retries == 1


@respx.mock
def test_client_default_retries_on(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    # default retries=2; force zero backoff so the test is instant
    c = GraphDBClient(base_url, token="tok", retries=FAST)
    assert c.nodes.get(1).id == 1 and route.call_count == 2


@respx.mock
def test_client_retries_zero_disables(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    c = GraphDBClient(base_url, token="tok", retries=0)
    with pytest.raises(ServerError):
        c.nodes.get(1)
    assert route.call_count == 1
```

- [ ] **Step 2: run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_retry_client.py -q` → ImportError (`RetryConfig` not exported) / TypeError (`retries` kwarg).

- [ ] **Step 3: edit `src/graphdb_client/client.py`.** Add the import (after `from ._transport import Transport`):

```python
from ._retry import RetryConfig, coerce_retry_config
```

Add `retries` to `GraphDBClient.__init__` signature (after `timeout: float = 30.0,`):

```python
        retries: RetryConfig | int | None = 2,
```

and pass it into the `Transport(...)` construction by adding this kwarg to that call:

```python
            retries=coerce_retry_config(retries),
```

- [ ] **Step 4: edit `src/graphdb_client/aio/client.py`** identically. Add (after `from .transport import AsyncTransport`):

```python
from .._retry import RetryConfig, coerce_retry_config
```

Add to `AsyncGraphDBClient.__init__` signature (after `timeout: float = 30.0,`):

```python
        retries: RetryConfig | int | None = 2,
```

and add to the `AsyncTransport(...)` construction kwargs:

```python
            retries=coerce_retry_config(retries),
```

- [ ] **Step 5: edit `src/graphdb_client/__init__.py`** to export `RetryConfig`. Add an import line (after the `from .errors import (...)` block, before `from .models import (...)`):

```python
from ._retry import RetryConfig
```

and add `"RetryConfig"` to `__all__` (immediately after `"AsyncGraphDBClient"`).

- [ ] **Step 6: run, confirm PASS** — `cd clients/python && uv run pytest tests/test_retry_client.py -q` → 3 passed. Verify the export: `cd clients/python && uv run python -c "from graphdb_client import RetryConfig; print(RetryConfig())"`.

- [ ] **Step 7: README.** Append to `clients/python/README.md`:

````markdown
## Resilience (retry & backoff)

The client retries transient failures automatically. **This is on by default**
(`retries=2`) — the one behavioral change in M4. Retries use full-jitter
exponential backoff and honor a `Retry-After` header.

- Retried: connection failures, HTTP `429`, and `502/503/504`.
- Idempotency-safe: `429` and connection-refused are retried on any method; other
  `5xx` only on idempotent methods (`GET/PUT/DELETE/HEAD/OPTIONS`).

```python
from graphdb_client import GraphDBClient, RetryConfig

# Tune it:
db = GraphDBClient(url, token=TOKEN, retries=RetryConfig(max_retries=5, backoff_factor=1.0))

# Or disable:
db = GraphDBClient(url, token=TOKEN, retries=0)
```

`AsyncGraphDBClient` takes the same `retries` argument.
````

- [ ] **Step 8: full gate:**
```
cd clients/python && uv run pytest -q          # expect 116 prior + 16 new (6+5+5 +3 - overlaps) ~= 132 passed, 2 skipped
cd clients/python && uv run ruff check .
cd clients/python && uv run mypy src
```
All must pass. (Exact count: Task1 +6, Task2 +5, Task3 +5, Task4 +3 = +19 → ~135 passed, 2 skipped. Report the real number.)

- [ ] **Step 9: commit:**
```
git add clients/python/src/graphdb_client/client.py clients/python/src/graphdb_client/aio/client.py clients/python/src/graphdb_client/__init__.py clients/python/README.md clients/python/tests/test_retry_client.py
git commit -m "feat(sdk): wire retries through both clients + export RetryConfig + README

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementer

- **The 401-refresh stays inside `_attempt`** — it is auth recovery, not a backoff retry. Retries wrap the whole attempt (including any refresh). Do NOT move it into the retry loop.
- **`_login`/`_refresh` failures are not retried by status** (a `GraphDBError` they raise propagates straight out, same as today); only a `httpx.TransportError` raised during an attempt — including during login — is caught and retried. This matches the spec; don't try to "fix" it.
- **Behavior preservation:** with `max_retries=0` the loop runs exactly once and raises/returns exactly as the pre-M4a code did. The existing `test_transport.py`/`test_auth.py`/`test_async_transport.py` must stay green untouched.
- **Idempotency nuance** (refines the spec's simpler "connection error → retryable"): `httpx.ConnectError` is retried on any method (connection never established); other transport errors only on idempotent methods. This is the safe direction; it's encoded in `is_retryable` (Task 1) and asserted there.
- **mypy is `--strict`** — `RetryConfig` is a frozen dataclass; `frozenset` literals are valid immutable defaults. Annotate the new params exactly as shown.
- After all 4 tasks: final whole-implementation review, then `superpowers:finishing-a-development-branch` (PR `feat/python-sdk-m4a`, which already carries the M4 spec commit `cbf9a40`).
```
