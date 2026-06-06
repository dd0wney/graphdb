from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._retry import RetryConfig
from graphdb_client._transport import Transport
from graphdb_client.errors import ServerError

FAST = RetryConfig(max_retries=2, backoff_factor=0.0, max_backoff=0.0, respect_retry_after=False)
OK = {"id": 1, "labels": [], "properties": {}}


def _t(base_url, retries=FAST):
    return Transport(base_url, token="tok", retries=retries)


@respx.mock
def test_retries_429_then_succeeds(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(429), httpx.Response(200, json=OK)])
    res = _t(base_url).request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2


@respx.mock
def test_exhausts_then_raises(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(503), httpx.Response(503)])
    with pytest.raises(ServerError):
        _t(base_url).request("GET", "/nodes/1")
    assert route.call_count == 3  # 1 + max_retries(2)


@respx.mock
def test_retries_connect_error_then_succeeds(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.ConnectError("boom"), httpx.Response(200, json=OK)])
    res = _t(base_url).request("GET", "/nodes/1")
    assert res.data["id"] == 1 and route.call_count == 2


@respx.mock
def test_post_5xx_not_retried(base_url):
    route = respx.post(f"{base_url}/nodes").mock(side_effect=[
        httpx.Response(503), httpx.Response(201, json=OK)])
    with pytest.raises(ServerError):
        _t(base_url).request("POST", "/nodes", json={})
    assert route.call_count == 1  # non-idempotent 5xx: no retry


@respx.mock
def test_disabled_when_retries_zero(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    with pytest.raises(ServerError):
        _t(base_url, retries=RetryConfig(max_retries=0)).request("GET", "/nodes/1")
    assert route.call_count == 1
