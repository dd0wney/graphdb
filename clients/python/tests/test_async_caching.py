from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.caching import AsyncCachingTransport
from graphdb_client.aio.transport import AsyncTransport
from graphdb_client.cache import CacheConfig, InMemoryCache

OK = {"id": 1, "labels": [], "properties": {}}


def _ct(base_url, cache=None, config=None):
    inner = AsyncTransport(base_url, token="tok")
    return AsyncCachingTransport(inner, cache or InMemoryCache(), config or CacheConfig())


@respx.mock
async def test_second_get_served_from_cache(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    assert (await ct.request("GET", "/nodes/1")).data == OK
    assert (await ct.request("GET", "/nodes/1")).data == OK
    assert route.call_count == 1
    assert ct.stats["hits"] == 1 and ct.stats["misses"] == 1


@respx.mock
async def test_put_invalidates_cache(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.put(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    ct = _ct(base_url)
    await ct.request("GET", "/nodes/1")
    await ct.request("PUT", "/nodes/1", json={})
    await ct.request("GET", "/nodes/1")
    assert get.call_count == 2


@respx.mock
async def test_post_does_not_invalidate(base_url):
    get = respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))
    respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={"results": []}))
    ct = _ct(base_url)
    await ct.request("GET", "/nodes/1")
    await ct.request("POST", "/search", json={})
    await ct.request("GET", "/nodes/1")
    assert get.call_count == 1


@respx.mock
async def test_fail_open_on_broken_backend(base_url):
    respx.get(f"{base_url}/nodes/1").mock(return_value=httpx.Response(200, json=OK))

    class Broken:
        async def aget(self, key): raise RuntimeError("boom")
        async def aset(self, key, value, *, ttl): raise RuntimeError("boom")
        async def adelete(self, key): raise RuntimeError("boom")
        async def aclear(self): raise RuntimeError("boom")

    ct = AsyncCachingTransport(AsyncTransport(base_url, token="tok"), Broken(), CacheConfig())
    assert (await ct.request("GET", "/nodes/1")).data == OK
