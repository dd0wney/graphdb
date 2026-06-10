from __future__ import annotations

from typing import Any, Mapping, cast

from .._caching import _MUTATING, _log, cache_key
from .._transport import ApiResult
from ..cache import AsyncCacheBackend, CacheConfig
from .transport import AsyncTransport


class AsyncCachingTransport:
    """Async mirror of CachingTransport: cache-aside GET caching + write invalidation. Fail-open."""

    def __init__(
        self, inner: AsyncTransport, cache: AsyncCacheBackend, config: CacheConfig
    ) -> None:
        self._inner = inner
        self._cache = cache
        self._config = config
        self._hits = 0
        self._misses = 0

    @property
    def stats(self) -> dict[str, float]:
        total = self._hits + self._misses
        return {
            "hits": self._hits,
            "misses": self._misses,
            "hit_rate": (self._hits / total) if total else 0.0,
        }

    def _ttl_for(self, path: str) -> float:
        for prefix, ttl in self._config.ttl_overrides.items():
            if path.startswith(prefix):
                return ttl
        return self._config.default_ttl

    async def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        m = method.upper()
        if m != "GET":
            res = await self._inner.request(method, path, json=json, params=params)
            if self._config.invalidate_on_write and m in _MUTATING:
                try:
                    await self._cache.aclear()
                except Exception:
                    # Fail-open but observable (security audit L-10).
                    _log.warning("cache clear failed; entries may be stale", exc_info=True)
            return res

        key = cache_key(method, path, params, self._config.namespace)
        try:
            cached = await self._cache.aget(key)
        except Exception:
            _log.warning("cache get failed; bypassing cache for this request", exc_info=True)
            cached = None
        if cached is not None:
            self._hits += 1
            return cast(ApiResult, cached)
        self._misses += 1
        res = await self._inner.request(method, path, json=json, params=params)
        try:
            await self._cache.aset(key, res, ttl=self._ttl_for(path))
        except Exception:
            _log.warning("cache set failed; response not cached", exc_info=True)
        return res

    async def aclose(self) -> None:
        await self._inner.aclose()
