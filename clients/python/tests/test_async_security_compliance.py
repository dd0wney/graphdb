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
