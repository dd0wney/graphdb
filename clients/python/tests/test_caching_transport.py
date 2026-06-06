from __future__ import annotations

import httpx
import respx

from graphdb_client._caching import CachingTransport, cache_key
from graphdb_client._transport import Transport
from graphdb_client.cache import CacheConfig, InMemoryCache

OK = {"id": 1, "labels": [], "properties": {}}


def _ct(base_url, cache=None, config=None):
    inner = Transport(base_url, token="tok")
    return CachingTransport(inner, cache or InMemoryCache(), config or CacheConfig())


def test_cache_key_sorts_params():
    assert cache_key("GET", "/nodes", {"b": 2, "a": 1}) == "GET:/nodes?a=1&b=2"
    assert cache_key("get", "/nodes", None) == "GET:/nodes"


@respx.mock
def test_second_get_served_from_cache(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    assert ct.request("GET", "/nodes/1").data == OK
    assert ct.request("GET", "/nodes/1").data == OK
    assert route.call_count == 1
    assert ct.stats["hits"] == 1 and ct.stats["misses"] == 1 and ct.stats["hit_rate"] == 0.5


@respx.mock
def test_put_invalidates_cache(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.put(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    ct.request("GET", "/nodes/1")             # cached
    ct.request("PUT", "/nodes/1", json={})    # PUT clears cache
    ct.request("GET", "/nodes/1")             # miss -> refetch
    assert get.call_count == 2


@respx.mock
def test_post_does_not_invalidate(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={"results": []}))
    ct = _ct(base_url)
    ct.request("GET", "/nodes/1")             # cached
    ct.request("POST", "/search", json={})    # POST is a read here -> must NOT clear
    ct.request("GET", "/nodes/1")             # still served from cache
    assert get.call_count == 1


@respx.mock
def test_post_never_cached(base_url):
    resp = httpx.Response(200, json={"results": [1]})
    route = respx.post(f"{base_url}/search").mock(return_value=resp)
    ct = _ct(base_url)
    ct.request("POST", "/search", json={})
    ct.request("POST", "/search", json={})
    assert route.call_count == 2


@respx.mock
def test_fail_open_on_broken_backend(base_url):
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))

    class Broken:
        def get(self, key): raise RuntimeError("boom")
        def set(self, key, value, *, ttl): raise RuntimeError("boom")
        def delete(self, key): raise RuntimeError("boom")
        def clear(self): raise RuntimeError("boom")

    ct = CachingTransport(Transport(base_url, token="tok"), Broken(), CacheConfig())
    assert ct.request("GET", "/nodes/1").data == OK   # backend errors swallowed


@respx.mock
def test_cache_hit_preserves_headers(base_url):
    respx.get(f"{base_url}/nodes").mock(
        return_value=httpx.Response(200, json=[OK], headers={"X-Next-Cursor": "c2"}))
    ct = _ct(base_url)
    ct.request("GET", "/nodes")
    r2 = ct.request("GET", "/nodes")
    assert r2.headers.get("X-Next-Cursor") == "c2"
