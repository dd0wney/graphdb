from __future__ import annotations

from typing import Any, Mapping, cast
from urllib.parse import urlencode

from ._transport import ApiResult, Transport
from .cache import CacheBackend, CacheConfig

# Only unambiguous mutations invalidate. graphdb uses POST for reads
# (/query, /search, /traverse, ...), so POST is deliberately excluded.
_MUTATING = frozenset({"PUT", "PATCH", "DELETE"})


def cache_key(method: str, path: str, params: Mapping[str, Any] | None) -> str:
    if params:
        query = urlencode(sorted((str(k), str(v)) for k, v in params.items()))
        return f"{method.upper()}:{path}?{query}"
    return f"{method.upper()}:{path}"


class CachingTransport:
    """Wraps a Transport with cache-aside GET caching + write invalidation. Fail-open."""

    def __init__(self, inner: Transport, cache: CacheBackend, config: CacheConfig) -> None:
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

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        m = method.upper()
        if m != "GET":
            res = self._inner.request(method, path, json=json, params=params)
            if self._config.invalidate_on_write and m in _MUTATING:
                try:
                    self._cache.clear()
                except Exception:
                    pass
            return res

        key = cache_key(method, path, params)
        try:
            cached = self._cache.get(key)
        except Exception:
            cached = None
        if cached is not None:
            self._hits += 1
            return cast(ApiResult, cached)
        self._misses += 1
        res = self._inner.request(method, path, json=json, params=params)
        try:
            self._cache.set(key, res, ttl=self._ttl_for(path))
        except Exception:
            pass
        return res

    def close(self) -> None:
        self._inner.close()
