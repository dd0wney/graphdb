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
    assert b'"permissions"' not in body and b'"expires_in"' not in body and b'"environment"' not in body  # noqa: E501


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
