from __future__ import annotations

import threading
import time
from collections import OrderedDict
from dataclasses import dataclass, field
from typing import Any, Mapping, Protocol, runtime_checkable


@runtime_checkable
class CacheBackend(Protocol):
    """Sync cache backend. Implement this to plug in Redis, memcached, etc."""

    def get(self, key: str) -> Any | None: ...
    def set(self, key: str, value: Any, *, ttl: float) -> None: ...
    def delete(self, key: str) -> None: ...
    def clear(self) -> None: ...


@runtime_checkable
class AsyncCacheBackend(Protocol):
    """Async cache backend. Distinct method names so one class can implement both."""

    async def aget(self, key: str) -> Any | None: ...
    async def aset(self, key: str, value: Any, *, ttl: float) -> None: ...
    async def adelete(self, key: str) -> None: ...
    async def aclear(self) -> None: ...


@dataclass
class CacheConfig:
    default_ttl: float = 300.0
    invalidate_on_write: bool = True
    ttl_overrides: Mapping[str, float] = field(default_factory=dict)  # path-prefix -> ttl
    # namespace disambiguates cache entries when one external backend
    # (Redis, etc.) is shared across clients configured with different
    # tokens/tenants (security audit M-10). Without it, the key is just
    # METHOD:path?params, so tenant A's cached GET could be served to
    # tenant B. Set it to a per-auth-context value (e.g. the tenant id).
    namespace: str = ""


class InMemoryCache:
    """Thread-safe bounded LRU cache with per-entry TTL. Implements both backend protocols."""

    def __init__(self, *, maxsize: int = 1024) -> None:
        self._maxsize = maxsize
        self._lock = threading.Lock()
        self._store: "OrderedDict[str, tuple[float, Any]]" = OrderedDict()

    def get(self, key: str) -> Any | None:
        with self._lock:
            item = self._store.get(key)
            if item is None:
                return None
            expiry, value = item
            if expiry < time.monotonic():
                del self._store[key]
                return None
            self._store.move_to_end(key)
            return value

    def set(self, key: str, value: Any, *, ttl: float) -> None:
        with self._lock:
            self._store[key] = (time.monotonic() + ttl, value)
            self._store.move_to_end(key)
            while len(self._store) > self._maxsize:
                self._store.popitem(last=False)

    def delete(self, key: str) -> None:
        with self._lock:
            self._store.pop(key, None)

    def clear(self) -> None:
        with self._lock:
            self._store.clear()

    async def aget(self, key: str) -> Any | None:
        return self.get(key)

    async def aset(self, key: str, value: Any, *, ttl: float) -> None:
        self.set(key, value, ttl=ttl)

    async def adelete(self, key: str) -> None:
        self.delete(key)

    async def aclear(self) -> None:
        self.clear()
