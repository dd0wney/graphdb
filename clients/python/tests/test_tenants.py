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
    _res(base_url).create("acme", "Acme", description="d", quota={"max_nodes": 5}, metadata={"t": "g"})  # noqa: E501
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
    respx.get(f"{base_url}/api/v1/tenants/acme").mock(return_value=httpx.Response(200, json=_tenant()))  # noqa: E501
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
    respx.post(f"{base_url}/api/v1/tenants/acme/suspend").mock(return_value=httpx.Response(200, json={}))  # noqa: E501
    respx.post(f"{base_url}/api/v1/tenants/acme/activate").mock(return_value=httpx.Response(200, json={}))  # noqa: E501
    r = _res(base_url)
    assert r.delete("acme") is None
    assert r.suspend("acme") is None
    assert r.activate("acme") is None


@respx.mock
def test_usage(base_url):
    respx.get(f"{base_url}/api/v1/tenants/acme/usage").mock(return_value=httpx.Response(200, json={
        "tenant_id": "acme", "node_count": 5, "edge_count": 7, "storage_bytes": 1024, "last_updated": 9}))  # noqa: E501
    u = _res(base_url).usage("acme")
    assert u.node_count == 5 and u.edge_count == 7 and u.storage_bytes == 1024
