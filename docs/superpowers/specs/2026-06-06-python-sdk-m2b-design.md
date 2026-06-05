# graphdb Python SDK — M2b design spec (admin slice)

**Date**: 2026-06-06
**Status**: design (awaiting review) → implementation plan (M2b)
**Parent specs**: `docs/superpowers/specs/2026-06-04-python-sdk-design.md` (M1),
`docs/superpowers/specs/2026-06-05-python-sdk-m2a-design.md` (M2a). All
cross-cutting decisions hold and are NOT relitigated: stdlib dataclasses
(`httpx`-only, decision D1), sync-only, the `Transport`/`_raw` escape hatch, the
M1 error hierarchy, Python 3.9+. Approach A (resources for multi-op surfaces;
typed dataclasses where the server has a stable struct, plain `dict` where it
returns a freeform `map[string]any`) — same as M2a.

---

## 1. Goal & scope

Add ergonomic facades for the **admin slice** of M2 (deferred from M2a):
tenant management, API-key management, security operations, and compliance.
Everything is already reachable via `client._raw.request(...)`; M2b adds
ergonomics over the admin endpoints.

**In scope:**

| Area | Endpoints | Facade | Return |
|---|---|---|---|
| Tenants | `POST/GET /api/v1/tenants`, `GET/PUT/DELETE /api/v1/tenants/{id}`, `GET /api/v1/tenants/{id}/usage`, `POST /api/v1/tenants/{id}/{suspend,activate}` | `client.tenants` | typed `Tenant` / `TenantUsage` |
| API keys | `GET/POST /api/v1/apikeys`, `DELETE /api/v1/apikeys/{id}` | `client.api_keys` | typed `CreatedAPIKey` / `APIKey` |
| Security | `POST /api/v1/security/keys/rotate`, `GET /api/v1/security/keys/info`, `GET /api/v1/security/audit/logs`, `GET /api/v1/security/audit/export`, `GET /api/v1/security/health` | `client.security` | `dict` (freeform) |
| Compliance | `GET /v1/compliance/audit-log`, `GET/POST /v1/compliance/masking-policy` | `client.compliance` | `dict` (freeform) |

**Out of scope:** async (M3), caching/retry/LangChain (M4). With M2b the SDK has
ergonomic facades over the full REST surface.

## 2. Decisions

- **Approach A** (locked in brainstorming): 4 new resources. Typed dataclasses
  for tenants + api keys (the server returns stable structs); `dict` for
  security + compliance (the server returns `map[string]any` — typing them would
  wrap genuinely loose shapes, low value, against D1).
- **Admin auth is server-side.** These endpoints are `requireAdmin`; the SDK
  does NO client-side gating — a non-admin token yields the existing `AuthError`
  (HTTP 403) from the M1 error hierarchy. Document, don't enforce.
- **Path quirk preserved.** Compliance is under `/v1/compliance/...`; tenants /
  api keys / security are under `/api/v1/...`. The methods use the literal paths.
- **Timestamps as the server sends them** (D1-thin): api-key times are RFC3339
  strings; tenant `created_at`/`updated_at` are int64 unix seconds. No conversion.
- **Nested config as passthrough dicts**: tenant `quota`/`metadata` and
  `quota_usage` are `dict | None` (server uses `tenant.TenantQuota` etc.; the SDK
  keeps them loose — D1-thin typed surface).

## 3. File structure

```
src/graphdb_client/
  client.py              # MODIFY: wire 4 new resources
  models.py              # MODIFY: add Tenant, TenantUsage, APIKey, CreatedAPIKey
  resources/
    tenants.py           # NEW: TenantsResource
    api_keys.py          # NEW: ApiKeysResource
    security.py          # NEW: SecurityResource (dict-returning)
    compliance.py        # NEW: ComplianceResource (dict-returning)
  __init__.py            # MODIFY: export the 4 new models
tests/
  test_tenants.py        # NEW
  test_api_keys.py       # NEW
  test_security.py       # NEW
  test_compliance.py     # NEW
```

No new runtime deps. `Transport`, `errors.py`, the `from_dict` pattern, and all
M1/M2a code are reused/untouched.

## 4. Facade surface (signatures)

All methods call `self._t.request(METHOD, path, json=..., params=...)`; optional
body/query fields are omitted when `None` (M2a convention).

### 4.1 `TenantsResource` (`client.tenants`)

```python
def create(self, id, name, *, description=None, quota=None, metadata=None) -> Tenant   # POST /api/v1/tenants
def list(self) -> list[Tenant]                                                          # GET  /api/v1/tenants  ({tenants, count})
def get(self, tenant_id) -> Tenant                                                      # GET  /api/v1/tenants/{id}
def update(self, tenant_id, *, name=None, description=None, quota=None, metadata=None) -> Tenant  # PUT /api/v1/tenants/{id}
def delete(self, tenant_id) -> None                                                     # DELETE /api/v1/tenants/{id}
def usage(self, tenant_id) -> TenantUsage                                               # GET  /api/v1/tenants/{id}/usage
def suspend(self, tenant_id) -> None                                                    # POST /api/v1/tenants/{id}/suspend
def activate(self, tenant_id) -> None                                                   # POST /api/v1/tenants/{id}/activate
```

`create` body: `{id, name}` + `description`/`quota`/`metadata` when not None.
`quota`/`metadata` are `Mapping[str, Any]` passthroughs. `update` sends only the
provided fields.

### 4.2 `ApiKeysResource` (`client.api_keys`)

```python
def create(self, name, *, permissions=None, expires_in=None, environment=None) -> CreatedAPIKey  # POST /api/v1/apikeys
def list(self) -> list[APIKey]                                                          # GET  /api/v1/apikeys  ({keys, count})
def revoke(self, key_id) -> None                                                        # DELETE /api/v1/apikeys/{id}
```

`create` returns `CreatedAPIKey`, which carries the plaintext `key` — **returned
only once**; the docstring must say so. `expires_in` is seconds (`0`/None =
never). `list` maps the `keys` envelope key (NOT `api_keys`).

### 4.3 `SecurityResource` (`client.security`) — all return `dict`

```python
def rotate_keys(self) -> dict     # POST /api/v1/security/keys/rotate    -> {message, new_version, timestamp}
def key_info(self) -> dict        # GET  /api/v1/security/keys/info      -> {statistics, keys}
def audit_logs(self, *, limit=None) -> dict   # GET /api/v1/security/audit/logs  -> {events, count, total, persistent_audit?}
def audit_export(self) -> list[dict]  # GET /api/v1/security/audit/export -> JSON array of event records
def health(self) -> dict          # GET  /api/v1/security/health         -> {status, components, ...}
```

`audit_logs` sends `limit` as a query param when set. `audit_export` returns the
server's downloadable JSON parsed in-memory — the server encodes the raw events
**array**, so this returns `list[dict]` (the SDK doesn't write a file). The
implementer must coerce a non-list body to `[]` for type-safety (mirrors the
`graphql` dict-guard in M2a).

### 4.4 `ComplianceResource` (`client.compliance`) — all return `dict`

```python
def audit_log(self, *, user_id=None, username=None, action=None, resource_type=None,
              status=None, start_time=None, end_time=None, limit=None, offset=None) -> dict   # GET /v1/compliance/audit-log
def get_masking_policy(self) -> dict                                  # GET  /v1/compliance/masking-policy
def set_masking_policy(self, properties, *, auto_detect=False) -> dict  # POST /v1/compliance/masking-policy
```

`audit_log` builds a query-param dict, omitting any `None` filter; `start_time`/
`end_time` are RFC3339 strings (caller-formatted). `set_masking_policy` body:
`{properties, auto_detect}` where `properties` is `Mapping[str, str]`
(property → strategy: `"full"|"partial"|"hash"|"redact"|"tokenize"|"none"`).

### 4.5 Wiring (`client.py`)

In `GraphDBClient.__init__`, after the M2a resources:
```python
self.tenants = TenantsResource(self._raw)
self.api_keys = ApiKeysResource(self._raw)
self.security = SecurityResource(self._raw)
self.compliance = ComplianceResource(self._raw)
```
No new top-level methods. M1/M2a methods unchanged.

## 5. Models (`models.py` additions)

```python
@dataclass
class Tenant:
    id: str
    name: str
    status: str
    description: str | None = None
    quota: dict[str, Any] | None = None
    metadata: dict[str, Any] = field(default_factory=dict)
    created_at: int = 0          # unix seconds
    updated_at: int = 0
    # from_dict: id/name/status str; description or None; quota dict|None;
    # metadata dict; created_at/updated_at int(...).

@dataclass
class TenantUsage:
    tenant_id: str
    node_count: int = 0
    edge_count: int = 0
    storage_bytes: int = 0
    quota_usage: dict[str, Any] | None = None
    last_updated: int = 0

@dataclass
class APIKey:
    id: str
    name: str
    prefix: str
    permissions: list[str] = field(default_factory=list)
    created: str | None = None     # RFC3339 string
    expires: str | None = None
    last_used: str | None = None
    revoked: bool = False

@dataclass
class CreatedAPIKey:
    key: str                       # plaintext — returned ONCE
    id: str
    name: str
    prefix: str
    created: str | None = None
    expires: str | None = None
```

Each gets a `from_dict` classmethod (M1 coercion pattern: `int()/str()/list()/
dict()/bool()`, `... or None` for optional strings). Security/compliance have NO
models (dict).

## 6. Error handling

No change. All facades go through `Transport.request` → M1 error hierarchy on
HTTP ≥ 400. A non-admin caller hitting an admin endpoint gets `AuthError` (403);
that's the documented behaviour, not an SDK-side check.

## 7. Testing

Mirror M2a: one unit-test file per resource, `respx`-mocking the request shape +
response mapping. Teeth:
- `tenants.create` omits description/quota/metadata when None; `list` maps the
  `tenants` envelope; `usage` maps `TenantUsage`; `suspend`/`activate`/`delete`
  return None and hit the right method+path.
- `api_keys.create` surfaces the one-time `key`; `list` maps the **`keys`**
  envelope (regression guard against assuming `api_keys`); `revoke` → DELETE.
- `security.*` return the raw dict unchanged (e.g. `rotate_keys()["new_version"]`,
  `health()["status"]`).
- `compliance.audit_log` builds query params omitting None filters and sends set
  ones; `set_masking_policy` sends `{properties, auto_detect}`.

Opt-in integration smoke is OPTIONAL for M2b and only meaningful with an admin
token; if added, gate it on the existing `GRAPHDB_SDK_IT` guard AND skip-if no
admin token. Unit tests are the gate.

## 8. Definition of done (M2b)

- `client.tenants`/`api_keys`/`security`/`compliance` resources per §4–5; 4 new
  models exported from `__init__.py`.
- Unit tests green (`respx`); `ruff` + `mypy` clean (the M1/M2a gates).
- README updated with an "Admin" usage section.
- The SDK now has ergonomic facades over the full REST surface (M2 complete);
  async (M3) and caching/retry/LangChain (M4) remain the roadmap.
