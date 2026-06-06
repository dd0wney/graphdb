from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client.aio.transport import AsyncTransport
from graphdb_client.errors import NotFoundError


@respx.mock
async def test_token_auth_header_and_success(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    t = AsyncTransport(base_url, token="tok")
    res = await t.request("GET", "/nodes/1")
    assert res.data["id"] == 1
    assert route.calls.last.request.headers["Authorization"] == "Bearer tok"
    await t.aclose()


@respx.mock
async def test_api_key_header(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    t = AsyncTransport(base_url, api_key="ak")
    await t.request("GET", "/nodes/1")
    assert route.calls.last.request.headers["X-API-Key"] == "ak"
    await t.aclose()


@respx.mock
async def test_lazy_login_when_credentials(base_url):
    login = respx.post(f"{base_url}/auth/login").mock(
        return_value=httpx.Response(200, json={"access_token": "AT", "refresh_token": "RT"}))
    respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    t = AsyncTransport(base_url, username="u", password="p")
    await t.request("GET", "/nodes/1")
    assert login.called
    await t.aclose()


@respx.mock
async def test_401_triggers_refresh_then_retry(base_url):
    respx.post(f"{base_url}/auth/refresh").mock(
        return_value=httpx.Response(200, json={"access_token": "AT2"}))
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(401, json={"error": "expired"}),
        httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}),
    ])
    t = AsyncTransport(base_url, token="old")
    t._refresh_token = "RT"
    res = await t.request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2
    await t.aclose()


@respx.mock
async def test_error_mapping(base_url):
    respx.get(f"{base_url}/nodes/9").mock(return_value=httpx.Response(404, json={"error": "nope"}))
    t = AsyncTransport(base_url, token="tok")
    with pytest.raises(NotFoundError):
        await t.request("GET", "/nodes/9")
    await t.aclose()


@respx.mock
async def test_async_context_manager_closes(base_url):
    respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}}))
    async with AsyncTransport(base_url, token="tok") as t:
        await t.request("GET", "/nodes/1")


def test_auth_validation_sync():
    with pytest.raises(ValueError):
        AsyncTransport("https://x", username="u")
