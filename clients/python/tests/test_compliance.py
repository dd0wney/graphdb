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
        200, json={"events": [], "count": 0}))
    out = _res(base_url).audit_log(username="alice", action="create", limit=10)
    assert out["count"] == 0
    p = route.calls.last.request.url.params
    assert p.get("username") == "alice" and p.get("action") == "create" and p.get("limit") == "10"
    assert "user_id" not in p and "status" not in p and "start_time" not in p


@respx.mock
def test_get_masking_policy(base_url):
    respx.get(f"{base_url}/v1/compliance/masking-policy/acme").mock(return_value=httpx.Response(
        200, json={"properties": {"email": "hash"}, "auto_detect": True}))
    out = _res(base_url).get_masking_policy("acme")
    assert out["properties"]["email"] == "hash" and out["auto_detect"] is True


@respx.mock
def test_set_masking_policy_sends_body(base_url):
    route = respx.post(f"{base_url}/v1/compliance/masking-policy").mock(return_value=httpx.Response(
        200, json={"status": "ok"}))
    out = _res(base_url).set_masking_policy({"email": "hash"}, auto_detect=True)
    assert out["status"] == "ok"
    body = route.calls.last.request.read()
    assert b'"properties"' in body and b'"auto_detect"' in body and b'"hash"' in body
