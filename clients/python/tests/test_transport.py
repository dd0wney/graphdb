from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client._transport import ApiResult, Transport
from graphdb_client.errors import NotFoundError, ValidationError


@respx.mock
def test_request_injects_bearer_token(base_url: str) -> None:
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1, "labels": [], "properties": {}})
    )
    t = Transport(base_url, token="tok")
    res = t.request("GET", "/nodes/1")
    assert isinstance(res, ApiResult)
    assert res.data["id"] == 1
    assert route.calls.last.request.headers["Authorization"] == "Bearer tok"
    t.close()


@respx.mock
def test_request_injects_api_key_header(base_url: str) -> None:
    route = respx.get(f"{base_url}/nodes/1").mock(
        return_value=httpx.Response(200, json={"id": 1})
    )
    t = Transport(base_url, api_key="k-123")
    t.request("GET", "/nodes/1")
    assert route.calls.last.request.headers["X-API-Key"] == "k-123"
    t.close()


@respx.mock
def test_request_exposes_response_headers(base_url: str) -> None:
    respx.get(f"{base_url}/nodes").mock(
        return_value=httpx.Response(200, json=[], headers={"X-Next-Cursor": "42"})
    )
    t = Transport(base_url, token="tok")
    res = t.request("GET", "/nodes")
    assert res.headers.get("X-Next-Cursor") == "42"
    t.close()


@respx.mock
def test_error_mapping(base_url: str) -> None:
    respx.get(f"{base_url}/nodes/9").mock(
        return_value=httpx.Response(404, json={"error": "not found"})
    )
    respx.post(f"{base_url}/nodes").mock(return_value=httpx.Response(400, json={"error": "bad"}))
    t = Transport(base_url, token="tok")
    with pytest.raises(NotFoundError):
        t.request("GET", "/nodes/9")
    with pytest.raises(ValidationError):
        t.request("POST", "/nodes", json={})
    t.close()
