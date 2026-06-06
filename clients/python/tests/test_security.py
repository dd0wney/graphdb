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
    respx.post(f"{base_url}/api/v1/security/audit/export").mock(return_value=httpx.Response(
        200, json=[{"action": "login"}, {"action": "logout"}]))
    out = _res(base_url).audit_export()
    assert isinstance(out, list) and out[0]["action"] == "login"


@respx.mock
def test_health(base_url):
    respx.get(f"{base_url}/api/v1/security/health").mock(return_value=httpx.Response(
        200, json={"status": "healthy", "components": {}}))
    assert _res(base_url).health()["status"] == "healthy"
