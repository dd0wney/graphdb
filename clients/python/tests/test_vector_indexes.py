from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.vector_indexes import VectorIndexesResource


def _res(base_url):
    return VectorIndexesResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_omits_optional_params_when_none(base_url):
    route = respx.post(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(
        201, json={"property_name": "embedding", "dimensions": 384, "metric": "cosine"}))
    vi = _res(base_url).create("embedding", 384)
    assert vi.property_name == "embedding" and vi.dimensions == 384
    body = route.calls.last.request.read()
    assert b'"m"' not in body and b'"ef_construction"' not in body and b'"metric"' not in body


@respx.mock
def test_create_sends_optional_params(base_url):
    route = respx.post(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(
        201, json={"property_name": "e", "dimensions": 8, "metric": "dot_product"}))
    _res(base_url).create("e", 8, m=32, ef_construction=400, metric="dot_product")
    body = route.calls.last.request.read()
    assert b'"m"' in body and b'"ef_construction"' in body and b'"dot_product"' in body


@respx.mock
def test_list(base_url):
    respx.get(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(200, json={
        "indexes": [{"property_name": "a", "dimensions": 3, "metric": "cosine"}], "count": 1}))
    got = _res(base_url).list()
    assert len(got) == 1 and got[0].property_name == "a"


@respx.mock
def test_get(base_url):
    respx.get(f"{base_url}/vector-indexes/embedding").mock(return_value=httpx.Response(
        200, json={"property_name": "embedding", "dimensions": 384, "metric": "cosine"}))
    assert _res(base_url).get("embedding").dimensions == 384


@respx.mock
def test_delete_returns_none(base_url):
    respx.delete(f"{base_url}/vector-indexes/embedding").mock(return_value=httpx.Response(204))
    assert _res(base_url).delete("embedding") is None
