from __future__ import annotations

import httpx
import respx

from graphdb_client import CacheConfig, GraphDBClient, InMemoryCache

OK = {"id": 1, "labels": [], "properties": {}}


def test_cache_exports():
    assert InMemoryCache().get("x") is None
    assert CacheConfig().default_ttl == 300.0


@respx.mock
def test_client_caches_get(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    c = GraphDBClient(base_url, token="tok", cache=InMemoryCache())
    assert c.nodes.get(1).id == 1
    assert c.nodes.get(1).id == 1
    assert route.call_count == 1
    assert c.cache_stats is not None and c.cache_stats["hits"] == 1


@respx.mock
def test_client_no_cache_by_default(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    c = GraphDBClient(base_url, token="tok")
    c.nodes.get(1)
    c.nodes.get(1)
    assert route.call_count == 2
    assert c.cache_stats is None
