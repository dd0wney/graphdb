from __future__ import annotations

import graphdb_client.cache as cache_mod
from graphdb_client.cache import AsyncCacheBackend, CacheBackend, CacheConfig, InMemoryCache


def test_set_get_delete_clear():
    c = InMemoryCache()
    assert c.get("missing") is None
    c.set("k", "v", ttl=100)
    assert c.get("k") == "v"
    c.delete("k")
    assert c.get("k") is None
    c.set("x", 1, ttl=100)
    c.clear()
    assert c.get("x") is None


def test_ttl_expiry(monkeypatch):
    t = {"now": 1000.0}
    monkeypatch.setattr(cache_mod.time, "monotonic", lambda: t["now"])
    c = InMemoryCache()
    c.set("k", "v", ttl=10)
    assert c.get("k") == "v"
    t["now"] = 1011.0
    assert c.get("k") is None


def test_lru_eviction_at_maxsize():
    c = InMemoryCache(maxsize=2)
    c.set("a", 1, ttl=100)
    c.set("b", 2, ttl=100)
    c.set("c", 3, ttl=100)
    assert c.get("a") is None      # oldest evicted
    assert c.get("b") == 2 and c.get("c") == 3


def test_lru_recency_refresh_on_get():
    c = InMemoryCache(maxsize=2)
    c.set("a", 1, ttl=100)
    c.set("b", 2, ttl=100)
    assert c.get("a") == 1          # touch a -> a becomes MRU
    c.set("c", 3, ttl=100)         # evicts LRU = b
    assert c.get("b") is None and c.get("a") == 1 and c.get("c") == 3


async def test_async_methods_delegate():
    c = InMemoryCache()
    await c.aset("k", "v", ttl=100)
    assert await c.aget("k") == "v"
    await c.adelete("k")
    assert await c.aget("k") is None
    await c.aset("x", 1, ttl=100)
    await c.aclear()
    assert await c.aget("x") is None


def test_inmemory_satisfies_both_protocols():
    c = InMemoryCache()
    assert isinstance(c, CacheBackend)
    assert isinstance(c, AsyncCacheBackend)


def test_cache_config_defaults():
    cfg = CacheConfig()
    assert cfg.default_ttl == 300.0
    assert cfg.invalidate_on_write is True
    assert cfg.ttl_overrides == {}
