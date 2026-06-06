from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.resources.algorithms import AsyncAlgorithmsResource
from graphdb_client.aio.resources.search import AsyncSearchResource
from graphdb_client.aio.resources.vector_indexes import AsyncVectorIndexesResource
from graphdb_client.aio.transport import AsyncTransport


def _t(base_url):
    return AsyncTransport(base_url, token="tok")


@respx.mock
async def test_search_hybrid(base_url):
    respx.post(f"{base_url}/hybrid-search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 1, "score": 0.5}],
        "count": 1, "took_ms": 2, "degraded": "no-lsa-index",
    }))
    r = await AsyncSearchResource(_t(base_url)).hybrid("q")
    assert r.degraded == "no-lsa-index" and r.hits[0].node_id == 1


@respx.mock
async def test_search_fulltext(base_url):
    respx.post(f"{base_url}/search").mock(return_value=httpx.Response(
        200, json={"results": [{"node_id": 2, "score": 1.0}], "count": 1, "took_ms": 1}))
    hits = await AsyncSearchResource(_t(base_url)).fulltext("q")
    assert hits[0].node_id == 2


@respx.mock
async def test_vector_indexes_list(base_url):
    respx.get(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(
        200, json={
            "indexes": [{"property_name": "e", "dimensions": 3, "metric": "cosine"}],
            "count": 1,
        }))
    got = await AsyncVectorIndexesResource(_t(base_url)).list()
    assert got[0].property_name == "e"


@respx.mock
async def test_algorithms_shortest_path(base_url):
    respx.post(f"{base_url}/shortest-path").mock(return_value=httpx.Response(
        200, json={"path": [1, 2], "length": 2, "found": True}))
    sp = await AsyncAlgorithmsResource(_t(base_url)).shortest_path(1, 2)
    assert sp.found is True and sp.path == [1, 2]
