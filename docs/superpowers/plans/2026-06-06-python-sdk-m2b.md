# Python SDK M2b Implementation Plan (admin slice)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add typed/ergonomic Python SDK facades for the admin surface — tenants, API keys, security, and compliance.

**Architecture:** Mirror M1/M2a exactly. New dataclasses in `models.py` (each with `from_dict`). New resources (`tenants`, `api_keys`, `security`, `compliance`) under `resources/`, wired into `GraphDBClient.__init__`. Tenants + api_keys return typed models; security + compliance return plain `dict`/`list` (the server emits freeform `map[string]any`). `Transport` (incl. `params=` for GET query strings) and the error hierarchy are reused unchanged.

**Tech Stack:** Python 3.9+, `httpx` (only runtime dep), stdlib `dataclasses`; tests use `respx` + `pytest`; `ruff` + `mypy` gates. All commands run from `clients/python/` via `uv run`.

**Spec:** `docs/superpowers/specs/2026-06-06-python-sdk-m2b-design.md`.

**Conventions (locked in M1/M2a — follow exactly):**
- Resource: `class FooResource: def __init__(self, transport: Transport) -> None: self._t = transport`; methods call `self._t.request(METHOD, path, json=..., params=...)` → `ApiResult` (`.data`, `.headers`).
- Typed methods map via `Model.from_dict(...)`. Dict-returning methods guard the type: `return res.data if isinstance(res.data, dict) else {}` (mirrors M2a's `graphql`); list-returning: `... if isinstance(res.data, list) else []`.
- Optional body/query fields omitted when `None`.
- Test: `@respx.mock` + `respx.<method>(f"{base_url}/path").mock(return_value=httpx.Response(status, json=...))`; build via `Transport(base_url, token="tok")`. For query params assert on `route.calls.last.request.url.params`.
- Run: `cd clients/python && uv run pytest tests/<file> -v`; gate: `uv run ruff check . && uv run mypy src && uv run pytest -q`.

---

### Task 1: M2b models

**Files:**
- Modify: `clients/python/src/graphdb_client/models.py`
- Test: `clients/python/tests/test_models_m2b.py`

- [ ] **Step 1: Write the failing test** — create `tests/test_models_m2b.py`:

```python
from __future__ import annotations

from graphdb_client.models import APIKey, CreatedAPIKey, Tenant, TenantUsage


def test_tenant():
    t = Tenant.from_dict({
        "id": "acme", "name": "Acme", "status": "active", "description": "d",
        "quota": {"max_nodes": 100}, "metadata": {"tier": "gold"},
        "created_at": 111, "updated_at": 222,
    })
    assert t.id == "acme" and t.name == "Acme" and t.status == "active"
    assert t.description == "d" and t.quota == {"max_nodes": 100}
    assert t.metadata == {"tier": "gold"} and t.created_at == 111 and t.updated_at == 222


def test_tenant_optional_fields_default():
    t = Tenant.from_dict({"id": "x", "name": "X", "status": "active"})
    assert t.description is None and t.quota is None and t.metadata == {}
    assert t.created_at == 0 and t.updated_at == 0


def test_tenant_usage():
    u = TenantUsage.from_dict({
        "tenant_id": "acme", "node_count": 5, "edge_count": 7,
        "storage_bytes": 1024, "quota_usage": {"nodes_pct": 0.5}, "last_updated": 99,
    })
    assert u.tenant_id == "acme" and u.node_count == 5 and u.edge_count == 7
    assert u.storage_bytes == 1024 and u.quota_usage == {"nodes_pct": 0.5} and u.last_updated == 99


def test_api_key_list_item():
    k = APIKey.from_dict({
        "id": "k1", "name": "ci", "prefix": "gdb_live_",
        "permissions": ["read", "write"], "created": "2026-06-06T00:00:00Z",
        "expires": None, "last_used": "2026-06-06T01:00:00Z", "revoked": False,
    })
    assert k.id == "k1" and k.prefix == "gdb_live_" and k.permissions == ["read", "write"]
    assert k.created == "2026-06-06T00:00:00Z" and k.expires is None
    assert k.last_used == "2026-06-06T01:00:00Z" and k.revoked is False


def test_created_api_key_carries_plaintext_key():
    c = CreatedAPIKey.from_dict({
        "key": "gdb_live_secret", "id": "k1", "name": "ci",
        "prefix": "gdb_live_", "created": "2026-06-06T00:00:00Z", "expires": None,
    })
    assert c.key == "gdb_live_secret" and c.id == "k1" and c.prefix == "gdb_live_"
    assert c.expires is None
```

- [ ] **Step 2: Run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_models_m2b.py -v` → ImportError.

- [ ] **Step 3: Append to the END of `clients/python/src/graphdb_client/models.py`:**

```python
@dataclass
class Tenant:
    id: str
    name: str
    status: str
    description: str | None = None
    quota: dict[str, Any] | None = None
    metadata: dict[str, Any] = field(default_factory=dict)
    created_at: int = 0
    updated_at: int = 0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "Tenant":
        return cls(
            id=str(d["id"]),
            name=str(d.get("name", "")),
            status=str(d.get("status", "")),
            description=d.get("description") or None,
            quota=dict(d["quota"]) if d.get("quota") is not None else None,
            metadata=dict(d.get("metadata") or {}),
            created_at=int(d.get("created_at", 0)),
            updated_at=int(d.get("updated_at", 0)),
        )


@dataclass
class TenantUsage:
    tenant_id: str
    node_count: int = 0
    edge_count: int = 0
    storage_bytes: int = 0
    quota_usage: dict[str, Any] | None = None
    last_updated: int = 0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "TenantUsage":
        return cls(
            tenant_id=str(d.get("tenant_id", "")),
            node_count=int(d.get("node_count", 0)),
            edge_count=int(d.get("edge_count", 0)),
            storage_bytes=int(d.get("storage_bytes", 0)),
            quota_usage=dict(d["quota_usage"]) if d.get("quota_usage") is not None else None,
            last_updated=int(d.get("last_updated", 0)),
        )


@dataclass
class APIKey:
    id: str
    name: str
    prefix: str
    permissions: list[str] = field(default_factory=list)
    created: str | None = None
    expires: str | None = None
    last_used: str | None = None
    revoked: bool = False

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "APIKey":
        return cls(
            id=str(d["id"]),
            name=str(d.get("name", "")),
            prefix=str(d.get("prefix", "")),
            permissions=list(d.get("permissions") or []),
            created=d.get("created") or None,
            expires=d.get("expires") or None,
            last_used=d.get("last_used") or None,
            revoked=bool(d.get("revoked", False)),
        )


@dataclass
class CreatedAPIKey:
    key: str
    id: str
    name: str
    prefix: str
    created: str | None = None
    expires: str | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "CreatedAPIKey":
        return cls(
            key=str(d.get("key", "")),
            id=str(d["id"]),
            name=str(d.get("name", "")),
            prefix=str(d.get("prefix", "")),
            created=d.get("created") or None,
            expires=d.get("expires") or None,
        )
```

(`models.py` already imports `from dataclasses import dataclass, field` and `from typing import Any, Mapping` — confirm.)

- [ ] **Step 4: Run, confirm PASS (5 passed)** — `cd clients/python && uv run pytest tests/test_models_m2b.py -v`.

- [ ] **Step 5: Gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/models.py clients/python/tests/test_models_m2b.py
git commit -m "feat(sdk): add M2b admin models (Tenant, TenantUsage, APIKey, CreatedAPIKey)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: TenantsResource

**Files:**
- Create: `clients/python/src/graphdb_client/resources/tenants.py`
- Test: `clients/python/tests/test_tenants.py`

- [ ] **Step 1: Write the failing test** — create `tests/test_tenants.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.tenants import TenantsResource


def _res(base_url):
    return TenantsResource(Transport(base_url, token="tok"))


def _tenant(**over):
    d = {"id": "acme", "name": "Acme", "status": "active", "created_at": 1, "updated_at": 2}
    d.update(over)
    return d


@respx.mock
def test_create_omits_optional_when_none(base_url):
    route = respx.post(f"{base_url}/api/v1/tenants").mock(
        return_value=httpx.Response(201, json=_tenant()))
    t = _res(base_url).create("acme", "Acme")
    assert t.id == "acme" and t.status == "active"
    body = route.calls.last.request.read()
    assert b'"id"' in body and b'"name"' in body
    assert b'"description"' not in body and b'"quota"' not in body and b'"metadata"' not in body


@respx.mock
def test_create_sends_optional(base_url):
    route = respx.post(f"{base_url}/api/v1/tenants").mock(
        return_value=httpx.Response(201, json=_tenant(description="d")))
    _res(base_url).create("acme", "Acme", description="d", quota={"max_nodes": 5}, metadata={"t": "g"})
    body = route.calls.last.request.read()
    assert b'"description"' in body and b'"quota"' in body and b'"metadata"' in body


@respx.mock
def test_list_maps_envelope(base_url):
    respx.get(f"{base_url}/api/v1/tenants").mock(return_value=httpx.Response(
        200, json={"tenants": [_tenant(), _tenant(id="globex")], "count": 2}))
    got = _res(base_url).list()
    assert [t.id for t in got] == ["acme", "globex"]


@respx.mock
def test_get(base_url):
    respx.get(f"{base_url}/api/v1/tenants/acme").mock(return_value=httpx.Response(200, json=_tenant()))
    assert _res(base_url).get("acme").name == "Acme"


@respx.mock
def test_update_sends_only_provided(base_url):
    route = respx.put(f"{base_url}/api/v1/tenants/acme").mock(
        return_value=httpx.Response(200, json=_tenant(name="Acme2")))
    t = _res(base_url).update("acme", name="Acme2")
    assert t.name == "Acme2"
    body = route.calls.last.request.read()
    assert b'"name"' in body and b'"description"' not in body and b'"quota"' not in body


@respx.mock
def test_delete_suspend_activate_return_none(base_url):
    respx.delete(f"{base_url}/api/v1/tenants/acme").mock(return_value=httpx.Response(204))
    respx.post(f"{base_url}/api/v1/tenants/acme/suspend").mock(return_value=httpx.Response(200, json={}))
    respx.post(f"{base_url}/api/v1/tenants/acme/activate").mock(return_value=httpx.Response(200, json={}))
    r = _res(base_url)
    assert r.delete("acme") is None
    assert r.suspend("acme") is None
    assert r.activate("acme") is None


@respx.mock
def test_usage(base_url):
    respx.get(f"{base_url}/api/v1/tenants/acme/usage").mock(return_value=httpx.Response(200, json={
        "tenant_id": "acme", "node_count": 5, "edge_count": 7, "storage_bytes": 1024, "last_updated": 9}))
    u = _res(base_url).usage("acme")
    assert u.node_count == 5 and u.edge_count == 7 and u.storage_bytes == 1024
```

- [ ] **Step 2: Run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_tenants.py -v`.

- [ ] **Step 3: Create `clients/python/src/graphdb_client/resources/tenants.py`:**

```python
from __future__ import annotations

from typing import Any, Mapping

from .._transport import Transport
from ..models import Tenant, TenantUsage


class TenantsResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        id: str,
        name: str,
        *,
        description: str | None = None,
        quota: Mapping[str, Any] | None = None,
        metadata: Mapping[str, Any] | None = None,
    ) -> Tenant:
        """Create a tenant (POST /api/v1/tenants). Admin-only."""
        body: dict[str, Any] = {"id": id, "name": name}
        if description is not None:
            body["description"] = description
        if quota is not None:
            body["quota"] = dict(quota)
        if metadata is not None:
            body["metadata"] = dict(metadata)
        res = self._t.request("POST", "/api/v1/tenants", json=body)
        return Tenant.from_dict(res.data)

    def list(self) -> list[Tenant]:
        """List tenants (GET /api/v1/tenants). Admin-only."""
        res = self._t.request("GET", "/api/v1/tenants")
        return [Tenant.from_dict(d) for d in (res.data.get("tenants") or [])]

    def get(self, tenant_id: str) -> Tenant:
        """Get one tenant (GET /api/v1/tenants/{id})."""
        res = self._t.request("GET", f"/api/v1/tenants/{tenant_id}")
        return Tenant.from_dict(res.data)

    def update(
        self,
        tenant_id: str,
        *,
        name: str | None = None,
        description: str | None = None,
        quota: Mapping[str, Any] | None = None,
        metadata: Mapping[str, Any] | None = None,
    ) -> Tenant:
        """Update a tenant (PUT /api/v1/tenants/{id}). Sends only provided fields. Admin-only."""
        body: dict[str, Any] = {}
        if name is not None:
            body["name"] = name
        if description is not None:
            body["description"] = description
        if quota is not None:
            body["quota"] = dict(quota)
        if metadata is not None:
            body["metadata"] = dict(metadata)
        res = self._t.request("PUT", f"/api/v1/tenants/{tenant_id}", json=body)
        return Tenant.from_dict(res.data)

    def delete(self, tenant_id: str) -> None:
        """Delete a tenant (DELETE /api/v1/tenants/{id}). Admin-only."""
        self._t.request("DELETE", f"/api/v1/tenants/{tenant_id}")

    def usage(self, tenant_id: str) -> TenantUsage:
        """Tenant usage stats (GET /api/v1/tenants/{id}/usage)."""
        res = self._t.request("GET", f"/api/v1/tenants/{tenant_id}/usage")
        return TenantUsage.from_dict(res.data)

    def suspend(self, tenant_id: str) -> None:
        """Suspend a tenant (POST /api/v1/tenants/{id}/suspend). Admin-only."""
        self._t.request("POST", f"/api/v1/tenants/{tenant_id}/suspend")

    def activate(self, tenant_id: str) -> None:
        """Activate a tenant (POST /api/v1/tenants/{id}/activate). Admin-only."""
        self._t.request("POST", f"/api/v1/tenants/{tenant_id}/activate")
```

- [ ] **Step 4: Run, confirm PASS (7 passed)** — `cd clients/python && uv run pytest tests/test_tenants.py -v`.

- [ ] **Step 5: Gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/resources/tenants.py clients/python/tests/test_tenants.py
git commit -m "feat(sdk): TenantsResource (create/list/get/update/delete/usage/suspend/activate)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: ApiKeysResource

**Files:**
- Create: `clients/python/src/graphdb_client/resources/api_keys.py`
- Test: `clients/python/tests/test_api_keys.py`

- [ ] **Step 1: Write the failing test** — create `tests/test_api_keys.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.api_keys import ApiKeysResource


def _res(base_url):
    return ApiKeysResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_surfaces_one_time_key(base_url):
    route = respx.post(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(201, json={
        "key": "gdb_live_secret", "id": "k1", "name": "ci", "prefix": "gdb_live_",
        "created": "2026-06-06T00:00:00Z"}))
    c = _res(base_url).create("ci", permissions=["read"], expires_in=3600)
    assert c.key == "gdb_live_secret" and c.id == "k1"
    body = route.calls.last.request.read()
    assert b'"permissions"' in body and b'"expires_in"' in body


@respx.mock
def test_create_omits_optional_when_none(base_url):
    route = respx.post(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(
        201, json={"key": "k", "id": "k1", "name": "ci", "prefix": "p"}))
    _res(base_url).create("ci")
    body = route.calls.last.request.read()
    assert b'"permissions"' not in body and b'"expires_in"' not in body and b'"environment"' not in body


@respx.mock
def test_list_maps_keys_envelope(base_url):
    # The server envelope key is "keys", NOT "api_keys".
    respx.get(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(200, json={
        "keys": [{"id": "k1", "name": "ci", "prefix": "p", "permissions": [], "revoked": False}],
        "count": 1}))
    got = _res(base_url).list()
    assert len(got) == 1 and got[0].id == "k1" and got[0].revoked is False


@respx.mock
def test_revoke_returns_none(base_url):
    respx.delete(f"{base_url}/api/v1/apikeys/k1").mock(return_value=httpx.Response(200, json={}))
    assert _res(base_url).revoke("k1") is None
```

- [ ] **Step 2: Run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_api_keys.py -v`.

- [ ] **Step 3: Create `clients/python/src/graphdb_client/resources/api_keys.py`:**

```python
from __future__ import annotations

from typing import Any, Sequence

from .._transport import Transport
from ..models import APIKey, CreatedAPIKey


class ApiKeysResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        name: str,
        *,
        permissions: Sequence[str] | None = None,
        expires_in: int | None = None,
        environment: str | None = None,
    ) -> CreatedAPIKey:
        """Create an API key (POST /api/v1/apikeys). Admin-only.

        The returned CreatedAPIKey.key is the plaintext key — it is shown ONCE
        and cannot be retrieved again; store it securely. `expires_in` is seconds
        (0/None = never).
        """
        body: dict[str, Any] = {"name": name}
        if permissions is not None:
            body["permissions"] = list(permissions)
        if expires_in is not None:
            body["expires_in"] = expires_in
        if environment is not None:
            body["environment"] = environment
        res = self._t.request("POST", "/api/v1/apikeys", json=body)
        return CreatedAPIKey.from_dict(res.data)

    def list(self) -> list[APIKey]:
        """List API keys (GET /api/v1/apikeys). Admin-only. The plaintext key is
        never returned here — only metadata."""
        res = self._t.request("GET", "/api/v1/apikeys")
        return [APIKey.from_dict(d) for d in (res.data.get("keys") or [])]

    def revoke(self, key_id: str) -> None:
        """Revoke an API key (DELETE /api/v1/apikeys/{id}). Admin-only."""
        self._t.request("DELETE", f"/api/v1/apikeys/{key_id}")
```

- [ ] **Step 4: Run, confirm PASS (4 passed)** — `cd clients/python && uv run pytest tests/test_api_keys.py -v`.

- [ ] **Step 5: Gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/resources/api_keys.py clients/python/tests/test_api_keys.py
git commit -m "feat(sdk): ApiKeysResource (create/list/revoke)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: SecurityResource (dict-returning)

**Files:**
- Create: `clients/python/src/graphdb_client/resources/security.py`
- Test: `clients/python/tests/test_security.py`

- [ ] **Step 1: Write the failing test** — create `tests/test_security.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.security import SecurityResource


def _res(base_url):
    return SecurityResource(Transport(base_url, token="tok"))


@respx.mock
def test_rotate_keys_returns_raw_dict(base_url):
    respx.post(f"{base_url}/api/v1/security/keys/rotate").mock(return_value=httpx.Response(
        200, json={"message": "Key rotated successfully", "new_version": 3, "timestamp": "t"}))
    out = _res(base_url).rotate_keys()
    assert out["new_version"] == 3


@respx.mock
def test_key_info(base_url):
    respx.get(f"{base_url}/api/v1/security/keys/info").mock(return_value=httpx.Response(
        200, json={"statistics": {"total": 2}, "keys": [{"version": 1}]}))
    assert _res(base_url).key_info()["statistics"]["total"] == 2


@respx.mock
def test_audit_logs_sends_limit_param(base_url):
    route = respx.get(f"{base_url}/api/v1/security/audit/logs").mock(return_value=httpx.Response(
        200, json={"events": [], "count": 0, "total": 0}))
    out = _res(base_url).audit_logs(limit=5)
    assert out["total"] == 0
    assert route.calls.last.request.url.params.get("limit") == "5"


@respx.mock
def test_audit_logs_omits_limit_when_none(base_url):
    route = respx.get(f"{base_url}/api/v1/security/audit/logs").mock(return_value=httpx.Response(
        200, json={"events": [], "count": 0, "total": 0}))
    _res(base_url).audit_logs()
    assert "limit" not in route.calls.last.request.url.params


@respx.mock
def test_audit_export_returns_list(base_url):
    respx.get(f"{base_url}/api/v1/security/audit/export").mock(return_value=httpx.Response(
        200, json=[{"action": "login"}, {"action": "logout"}]))
    out = _res(base_url).audit_export()
    assert isinstance(out, list) and out[0]["action"] == "login"


@respx.mock
def test_health(base_url):
    respx.get(f"{base_url}/api/v1/security/health").mock(return_value=httpx.Response(
        200, json={"status": "healthy", "components": {}}))
    assert _res(base_url).health()["status"] == "healthy"
```

- [ ] **Step 2: Run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_security.py -v`.

- [ ] **Step 3: Create `clients/python/src/graphdb_client/resources/security.py`:**

```python
from __future__ import annotations

from typing import Any

from .._transport import Transport


def _as_dict(data: Any) -> dict[str, Any]:
    return data if isinstance(data, dict) else {}


class SecurityResource:
    """Admin security operations. All methods return the server's raw JSON (a
    freeform dict, or a list for audit_export); the shapes are not stable enough
    to type. Admin-only — a non-admin token raises AuthError (403)."""

    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def rotate_keys(self) -> dict[str, Any]:
        """Rotate encryption keys (POST /api/v1/security/keys/rotate)."""
        return _as_dict(self._t.request("POST", "/api/v1/security/keys/rotate").data)

    def key_info(self) -> dict[str, Any]:
        """Encryption key info (GET /api/v1/security/keys/info)."""
        return _as_dict(self._t.request("GET", "/api/v1/security/keys/info").data)

    def audit_logs(self, *, limit: int | None = None) -> dict[str, Any]:
        """In-memory security audit logs (GET /api/v1/security/audit/logs)."""
        params: dict[str, Any] = {}
        if limit is not None:
            params["limit"] = limit
        return _as_dict(self._t.request("GET", "/api/v1/security/audit/logs", params=params).data)

    def audit_export(self) -> list[dict[str, Any]]:
        """Export the audit-log events (GET /api/v1/security/audit/export). The
        server encodes a JSON array of event records; returned in-memory (no file)."""
        data = self._t.request("GET", "/api/v1/security/audit/export").data
        return data if isinstance(data, list) else []

    def health(self) -> dict[str, Any]:
        """Security component health (GET /api/v1/security/health)."""
        return _as_dict(self._t.request("GET", "/api/v1/security/health").data)
```

- [ ] **Step 4: Run, confirm PASS (6 passed)** — `cd clients/python && uv run pytest tests/test_security.py -v`.

- [ ] **Step 5: Gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/resources/security.py clients/python/tests/test_security.py
git commit -m "feat(sdk): SecurityResource (rotate_keys/key_info/audit_logs/audit_export/health)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: ComplianceResource (dict-returning)

**Files:**
- Create: `clients/python/src/graphdb_client/resources/compliance.py`
- Test: `clients/python/tests/test_compliance.py`

- [ ] **Step 1: Write the failing test** — create `tests/test_compliance.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.compliance import ComplianceResource


def _res(base_url):
    return ComplianceResource(Transport(base_url, token="tok"))


@respx.mock
def test_audit_log_builds_params_omitting_none(base_url):
    route = respx.get(f"{base_url}/v1/compliance/audit-log").mock(return_value=httpx.Response(
        200, json={"entries": [], "count": 0}))
    out = _res(base_url).audit_log(username="alice", action="create", limit=10)
    assert out["count"] == 0
    p = route.calls.last.request.url.params
    assert p.get("username") == "alice" and p.get("action") == "create" and p.get("limit") == "10"
    assert "user_id" not in p and "status" not in p and "start_time" not in p


@respx.mock
def test_get_masking_policy(base_url):
    respx.get(f"{base_url}/v1/compliance/masking-policy").mock(return_value=httpx.Response(
        200, json={"properties": {"email": "hash"}, "auto_detect": True}))
    out = _res(base_url).get_masking_policy()
    assert out["properties"]["email"] == "hash" and out["auto_detect"] is True


@respx.mock
def test_set_masking_policy_sends_body(base_url):
    route = respx.post(f"{base_url}/v1/compliance/masking-policy").mock(return_value=httpx.Response(
        200, json={"status": "ok"}))
    out = _res(base_url).set_masking_policy({"email": "hash"}, auto_detect=True)
    assert out["status"] == "ok"
    body = route.calls.last.request.read()
    assert b'"properties"' in body and b'"auto_detect"' in body and b'"hash"' in body
```

- [ ] **Step 2: Run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_compliance.py -v`.

- [ ] **Step 3: Create `clients/python/src/graphdb_client/resources/compliance.py`:**

```python
from __future__ import annotations

from typing import Any, Mapping

from .._transport import Transport


def _as_dict(data: Any) -> dict[str, Any]:
    return data if isinstance(data, dict) else {}


class ComplianceResource:
    """Compliance operations (audit log + masking policy). Returns the server's
    raw JSON dict. Note: these live under /v1/compliance/... (NOT /api/v1/...)."""

    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def audit_log(
        self,
        *,
        user_id: str | None = None,
        username: str | None = None,
        action: str | None = None,
        resource_type: str | None = None,
        status: str | None = None,
        start_time: str | None = None,
        end_time: str | None = None,
        limit: int | None = None,
        offset: int | None = None,
    ) -> dict[str, Any]:
        """Query the compliance audit log (GET /v1/compliance/audit-log).
        `start_time`/`end_time` are RFC3339 strings. Unset filters are omitted."""
        params: dict[str, Any] = {}
        for name, val in (
            ("user_id", user_id), ("username", username), ("action", action),
            ("resource_type", resource_type), ("status", status),
            ("start_time", start_time), ("end_time", end_time),
            ("limit", limit), ("offset", offset),
        ):
            if val is not None:
                params[name] = val
        return _as_dict(self._t.request("GET", "/v1/compliance/audit-log", params=params).data)

    def get_masking_policy(self) -> dict[str, Any]:
        """Get the tenant's masking policy (GET /v1/compliance/masking-policy)."""
        return _as_dict(self._t.request("GET", "/v1/compliance/masking-policy").data)

    def set_masking_policy(
        self, properties: Mapping[str, str], *, auto_detect: bool = False
    ) -> dict[str, Any]:
        """Set the tenant's masking policy (POST /v1/compliance/masking-policy). Admin-only.

        `properties` maps a property name to a strategy: one of "full", "partial",
        "hash", "redact", "tokenize", "none"."""
        body: dict[str, Any] = {"properties": dict(properties), "auto_detect": auto_detect}
        return _as_dict(self._t.request("POST", "/v1/compliance/masking-policy", json=body).data)
```

- [ ] **Step 4: Run, confirm PASS (3 passed)** — `cd clients/python && uv run pytest tests/test_compliance.py -v`.

- [ ] **Step 5: Gate + commit:**
```
cd clients/python && uv run ruff check . && uv run mypy src && uv run pytest -q
git add clients/python/src/graphdb_client/resources/compliance.py clients/python/tests/test_compliance.py
git commit -m "feat(sdk): ComplianceResource (audit_log/get+set masking-policy)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Wire resources + exports + README + final gate

**Files:**
- Modify: `clients/python/src/graphdb_client/client.py`
- Modify: `clients/python/src/graphdb_client/__init__.py`
- Modify: `clients/python/README.md`
- Test: `clients/python/tests/test_client_admin_wiring.py`

- [ ] **Step 1: Write the failing wiring test** — create `tests/test_client_admin_wiring.py`:

```python
from __future__ import annotations

from graphdb_client import GraphDBClient
from graphdb_client.resources.api_keys import ApiKeysResource
from graphdb_client.resources.compliance import ComplianceResource
from graphdb_client.resources.security import SecurityResource
from graphdb_client.resources.tenants import TenantsResource


def test_admin_resources_wired():
    c = GraphDBClient("https://graphdb.test", token="tok")
    assert isinstance(c.tenants, TenantsResource)
    assert isinstance(c.api_keys, ApiKeysResource)
    assert isinstance(c.security, SecurityResource)
    assert isinstance(c.compliance, ComplianceResource)
```

- [ ] **Step 2: Run, confirm FAIL** — `cd clients/python && uv run pytest tests/test_client_admin_wiring.py -v` → AttributeError (no `c.tenants`).

- [ ] **Step 3: Wire in `client.py`.**

(a) Add the resource imports alongside the existing M2a ones:
```python
from .resources.api_keys import ApiKeysResource
from .resources.compliance import ComplianceResource
from .resources.security import SecurityResource
from .resources.tenants import TenantsResource
```
(b) In `__init__`, after the M2a resources (`self.algorithms = AlgorithmsResource(self._raw)`), add:
```python
        self.tenants = TenantsResource(self._raw)
        self.api_keys = ApiKeysResource(self._raw)
        self.security = SecurityResource(self._raw)
        self.compliance = ComplianceResource(self._raw)
```

- [ ] **Step 4: Run, confirm PASS** — `cd clients/python && uv run pytest tests/test_client_admin_wiring.py -v`.

- [ ] **Step 5: Export the 4 models in `__init__.py`.** Replace the `from .models import (...)` block with (adds `APIKey`, `CreatedAPIKey`, `Tenant`, `TenantUsage`, alphabetical):

```python
from .models import (
    AlgorithmResult,
    APIKey,
    CreatedAPIKey,
    Edge,
    EmbeddingsResult,
    HybridSearchResult,
    Node,
    QueryResult,
    RetrieveDocument,
    RetrieveResult,
    RetrieveSource,
    SearchHit,
    SearchResult,
    ShortestPath,
    Tenant,
    TenantUsage,
    VectorIndex,
)
```

and replace `__all__` with:

```python
__all__ = [
    "GraphDBClient",
    "Node", "Edge", "SearchResult",
    "SearchHit", "HybridSearchResult", "VectorIndex",
    "RetrieveSource", "RetrieveDocument", "RetrieveResult",
    "EmbeddingsResult", "QueryResult", "AlgorithmResult", "ShortestPath",
    "Tenant", "TenantUsage", "APIKey", "CreatedAPIKey",
    "GraphDBError", "ValidationError", "AuthError", "NotFoundError",
    "ConflictError", "RateLimitError", "ServerError",
]
```

Verify: `cd clients/python && uv run python -c "import graphdb_client as g; print(g.Tenant, g.TenantUsage, g.APIKey, g.CreatedAPIKey)"` → prints 4 classes, no error.

- [ ] **Step 6: README.** Append an "Admin" section to `clients/python/README.md`:

````markdown
## Admin (requires an admin token)

```python
with GraphDBClient("http://localhost:8080", token=ADMIN_TOKEN) as db:
    # tenants
    db.tenants.create("acme", "Acme Corp")
    print([t.id for t in db.tenants.list()])
    print(db.tenants.usage("acme").node_count)
    db.tenants.suspend("acme"); db.tenants.activate("acme")

    # api keys (the plaintext key is returned ONCE)
    created = db.api_keys.create("ci-pipeline", permissions=["read"], expires_in=86400)
    print("save this:", created.key)
    db.api_keys.revoke(created.id)

    # security + compliance (raw dicts)
    print(db.security.health()["status"])
    db.compliance.set_masking_policy({"email": "hash"}, auto_detect=True)
    print(db.compliance.audit_log(username="admin", limit=10))
```
````

- [ ] **Step 7: Full gate:**
```
cd clients/python && uv run pytest -q          # expect ~92 passed, 2 skipped (66 prior + 26 new)
cd clients/python && uv run ruff check .
cd clients/python && uv run mypy src
```
All must pass; fix any issue only in files you changed.

- [ ] **Step 8: Commit:**
```
git add clients/python/src/graphdb_client/client.py clients/python/src/graphdb_client/__init__.py clients/python/README.md clients/python/tests/test_client_admin_wiring.py
git commit -m "feat(sdk): wire admin resources + export M2b models + README

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementer

- **Do not modify `Transport`, `errors.py`, or M1/M2a code.** M2b is purely additive.
- **`api_keys.list` maps the `keys` envelope, not `api_keys`** — the server uses `{"keys": [...], "count": N}`. The test pins this.
- **`audit_export` returns a list**, every other security/compliance method returns a dict — the `_as_dict` / `isinstance list` guards keep mypy happy and tolerate an unexpected body shape.
- **mypy:** keep public methods typed; the documented freeform returns are `dict[str, Any]` / `list[dict[str, Any]]`.
- The `base_url` fixture (`tests/conftest.py`) returns `https://graphdb.test`; `respx` is already a dev dep.
- After all tasks: final whole-implementation review, then `superpowers:finishing-a-development-branch`.
```
