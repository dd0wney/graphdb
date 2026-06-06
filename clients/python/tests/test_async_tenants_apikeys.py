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
    respx.post(f"{base_url}/api/v1/tenants/acme/suspend").mock(
        return_value=httpx.Response(200, json={}))
    assert await AsyncTenantsResource(_t(base_url)).suspend("acme") is None


@respx.mock
async def test_api_key_create_and_list(base_url):
    respx.post(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(201, json={
        "key": "secret", "id": "k1", "name": "ci", "prefix": "p"}))
    c = await AsyncApiKeysResource(_t(base_url)).create("ci")
    assert c.key == "secret"
    respx.get(f"{base_url}/api/v1/apikeys").mock(return_value=httpx.Response(200, json={
        "keys": [{"id": "k1", "name": "ci", "prefix": "p", "permissions": [], "revoked": False}],
        "count": 1}))
    got = await AsyncApiKeysResource(_t(base_url)).list()
    assert got[0].id == "k1"
