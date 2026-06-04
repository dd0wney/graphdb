from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._transport import Transport
from graphdb_client.errors import AuthError


@respx.mock
def test_login_on_first_request_when_credentials_given(base_url):
    login = respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "acc", "refresh_token": "ref"})
    )
    nodes = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1})
    )
    t = Transport(base_url, username="u", password="p")
    t.request("GET", "/nodes/1")
    assert login.called
    assert nodes.calls.last.request.headers["Authorization"] == "Bearer acc"
    t.close()


@respx.mock
def test_401_triggers_refresh_then_retry_succeeds(base_url):
    respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "old", "refresh_token": "ref"})
    )
    refresh = respx.post(f"{base_url}/auth/refresh").mock(
        return_value=httpx.Response(200, json={"access_token": "new"})
    )
    nodes = respx.get(f"{base_url}/nodes/1").mock(
        side_effect=[httpx.Response(401, json={"error": "expired"}),
                     httpx.Response(200, json={"id": 1})]
    )
    t = Transport(base_url, username="u", password="p")
    res = t.request("GET", "/nodes/1")
    assert res.data["id"] == 1
    assert refresh.called
    assert nodes.calls[-1].request.headers["Authorization"] == "Bearer new"
    t.close()


@respx.mock
def test_second_401_raises_autherror(base_url):
    respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "old", "refresh_token": "ref"})
    )
    respx.post(f"{base_url}/auth/refresh").mock(
        return_value=httpx.Response(200, json={"access_token": "new"})
    )
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(401, json={"error": "nope"}))
    t = Transport(base_url, username="u", password="p")
    with pytest.raises(AuthError):
        t.request("GET", "/nodes/1")
    t.close()


@respx.mock
def test_static_token_401_does_not_refresh(base_url):
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(401, json={"error": "nope"}))
    refresh = respx.post(f"{base_url}/auth/refresh")
    t = Transport(base_url, token="static")
    with pytest.raises(AuthError):
        t.request("GET", "/nodes/1")
    assert not refresh.called
    t.close()
