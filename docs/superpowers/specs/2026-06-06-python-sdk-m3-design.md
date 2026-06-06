# graphdb Python SDK — M3 design spec (async client)

**Date**: 2026-06-06
**Status**: design (awaiting review) → implementation plan (M3)
**Parent specs**: M1 (`2026-06-04-python-sdk-design.md`), M2a (`2026-06-05-...-m2a-...`),
M2b (`2026-06-06-...-m2b-...`). Cross-cutting decisions hold: stdlib dataclasses,
`httpx`-only runtime dep, error hierarchy, Python 3.9+, mypy strict.

---

## 1. Goal & scope

Add **`AsyncGraphDBClient`** — a complete async drop-in for `GraphDBClient` with
identical method names/signatures, just `async`/awaitable. **Full parity**: all 9
resources + the 6 top-level client methods, async.

**In scope:** async transport (auth/login/refresh/request), async mirrors of
`nodes`, `edges`, `search`, `vector_indexes`, `algorithms`, `tenants`,
`api_keys`, `security`, `compliance`, and `traverse`/`vector_search`/`retrieve`/
`embeddings`/`query`/`graphql`; async context-manager lifecycle.

**Out of scope:** M4 (response caching, retry/backoff, LangChain). No new
endpoints — async mirrors the existing surface exactly.

## 2. Decisions (locked in brainstorming)

- **Approach A — hand-written parallel hierarchy** (not `unasync` codegen, not a
  thin-wrapper abstraction). Transparent, no build magic — consistent with the
  project's hand-written-models, no-codegen philosophy. The cost (sync/async kept
  in step by hand) is accepted; if drift becomes painful, migrating to `unasync`
  later is the escape hatch, with the hand-written async as the reference.
- **Full parity** — async is a whole-client feature (a consumer picks sync OR
  async wholesale), so a partial async surface is not shipped.
- **Models + errors are shared, not duplicated** — `models.py` `from_dict` is pure
  (no I/O); `errors.py` is transport-agnostic. Async code imports the same ones.

## 3. File structure

A parallel `aio/` subpackage mirroring the sync layout:

```
src/graphdb_client/
  _transport.py          # MODIFY (small): extract shared pure helpers (see §4)
  __init__.py            # MODIFY: also export AsyncGraphDBClient
  aio/
    __init__.py          # NEW: exports AsyncGraphDBClient
    transport.py         # NEW: AsyncTransport
    client.py            # NEW: AsyncGraphDBClient
    resources/
      __init__.py        # NEW
      nodes.py edges.py search.py vector_indexes.py algorithms.py
      tenants.py api_keys.py security.py compliance.py   # NEW: Async*Resource ×9
tests/
  test_async_transport.py        # NEW (deep — the real logic)
  test_async_resources.py        # NEW (per-resource smoke)
  test_async_client.py           # NEW (top-level methods + lifecycle)
```

Relative imports inside `aio/`:
- `aio/transport.py`: `from ..errors import from_response`, `from .._transport import _safe_json, build_auth_headers`.
- `aio/resources/<x>.py`: `from ..transport import AsyncTransport`, `from ...models import <Models>`.
- `aio/client.py`: `from .transport import AsyncTransport`, `from .resources.<x> import Async<X>Resource`, `from ..models import <Models>`.

## 4. Shared pure transport helpers (small sync refactor)

To avoid re-implementing auth precedence + JSON parsing in the async transport,
extract two pure helpers in `_transport.py` (behavior-preserving):

- `_safe_json(resp)` — already module-level; the async transport imports it
  (works on an awaited `httpx.Response` — the body is read by default).
- `build_auth_headers(token: str | None, api_key: str | None) -> dict[str, str]`
  — NEW free function holding the token-then-api-key precedence. `Transport._auth_headers`
  becomes `return build_auth_headers(self._token, self._api_key)` (identical
  behaviour; existing sync tests must still pass unchanged).

`_has_credentials` (a one-liner) is trivially re-stated in each transport.

## 5. AsyncTransport (`aio/transport.py`)

Mirrors `Transport` exactly, with `httpx.AsyncClient` and awaited I/O. Reuses
`ApiResult` (import from `.._transport`).

```python
class AsyncTransport:
    def __init__(self, base_url, *, token=None, api_key=None, username=None,
                 password=None, timeout=30.0) -> None:
        # same validation + field init as sync; self._http = httpx.AsyncClient(...)
    async def _login(self) -> None: ...          # await self._http.post("/auth/login", ...)
    async def _refresh(self) -> bool: ...         # await; falls back to _login
    async def request(self, method, path, *, json=None, params=None) -> ApiResult:
        # identical logic to sync, awaited:
        #   lazy login if token is None and creds present
        #   await self._http.request(..., headers=build_auth_headers(self._token, self._api_key))
        #   on 401 with refresh/creds: await self._refresh(); retry once
        #   >=400 -> raise from_response(...); else ApiResult(_safe_json(resp), resp.headers)
    async def aclose(self) -> None:               # await self._http.aclose()
```

The login→401→refresh→retry→error-map contract is byte-for-byte the sync logic
with `await`. This is the one place real care is needed; everything else is
mechanical.

## 6. Async resources + client

Each `Async<X>Resource` mirrors its sync `<X>Resource`: same class shape
(`__init__(self, transport: AsyncTransport)` → `self._t`), same method
names/signatures/paths/omit-when-None body building, but `async def` +
`res = await self._t.request(...)` + the **same** `Model.from_dict(...)` mapping
(or dict/list passthrough for security/compliance). Example:

```python
class AsyncNodesResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._t = transport
    async def get(self, node_id: int) -> Node:
        res = await self._t.request("GET", f"/nodes/{node_id}")
        return Node.from_dict(res.data)
    # ...create/update/delete/batch_create/list (list stays an async generator:
    #    `async def list(...) -> AsyncIterator[Node]` yielding, awaiting each page)
```

`AsyncGraphDBClient` mirrors `GraphDBClient.__init__` (build `AsyncTransport`,
wire all 9 async resources), the 6 top-level async methods (`traverse`,
`vector_search`, `retrieve`, `embeddings`, `query`, `graphql`), and the async
lifecycle: `aclose()`, `__aenter__`/`__aexit__` (so `async with AsyncGraphDBClient(...) as db:` works).

Note: `nodes.list` is the one method that's an iterator — its async form is an
`AsyncIterator` (`async for n in db.nodes.list(...)`), awaiting each page and
following `X-Next-Cursor` exactly as the sync version does.

## 7. Testing

Add the async runner: `pytest-asyncio` to `[dependency-groups].dev`, and
`asyncio_mode = "auto"` under `[tool.pytest.ini_options]` (so `async def test_*`
runs without per-test decorators). `respx` mocks `httpx.AsyncClient` the same way.

- **`test_async_transport.py` (deep — the real logic):** auth header (token vs
  api-key precedence), lazy login when only username/password given, `401 →
  refresh → retry` success, refresh-falls-back-to-login, error mapping (≥400 →
  the right `errors.py` type), and `async with` closing the client.
- **`test_async_resources.py` (per-resource smoke):** one method per async
  resource (9 tests) asserting it awaits, hits the correct method+path, and
  returns the right model type / list. NOT re-testing every field mapping —
  `from_dict` is shared and already covered by the sync suite; duplicating ~44
  mapping assertions would be noise.
- **`test_async_client.py`:** the 6 top-level async methods (smoke: correct path +
  return type), `nodes.list` async iteration across a cursor, and the
  `async with` lifecycle.

All async tests use `respx` + an awaited client built against the `base_url`
fixture. mypy strict must pass on the new async code (explicit return types,
`AsyncIterator[...]` where relevant).

## 8. Definition of done (M3)

- `from graphdb_client import AsyncGraphDBClient` works; `async with` it; every
  sync resource/method has an awaitable async twin with the same signature.
- `pytest-asyncio` added + `asyncio_mode = "auto"`; async tests green; full suite
  (sync + async) green; `ruff` + `mypy --strict` clean.
- README gains a short "Async" section showing `async with AsyncGraphDBClient(...)`.
- Sync client, transport, models, errors behave identically (the only sync change
  is the behavior-preserving `build_auth_headers` extraction).
- M4 (caching/retry/LangChain) remains the roadmap.
