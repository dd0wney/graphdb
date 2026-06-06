from __future__ import annotations

from types import TracebackType
from typing import Any, Mapping, Sequence, cast

from .._retry import RetryConfig, coerce_retry_config
from ..cache import AsyncCacheBackend, CacheConfig
from ..models import EmbeddingsResult, Node, QueryResult, RetrieveResult, SearchResult
from .caching import AsyncCachingTransport
from .resources.algorithms import AsyncAlgorithmsResource
from .resources.api_keys import AsyncApiKeysResource
from .resources.compliance import AsyncComplianceResource
from .resources.edges import AsyncEdgesResource
from .resources.nodes import AsyncNodesResource
from .resources.search import AsyncSearchResource
from .resources.security import AsyncSecurityResource
from .resources.tenants import AsyncTenantsResource
from .resources.vector_indexes import AsyncVectorIndexesResource
from .transport import AsyncTransport


class AsyncGraphDBClient:
    """Async drop-in for GraphDBClient: same surface, awaitable."""

    def __init__(
        self,
        base_url: str,
        *,
        token: str | None = None,
        api_key: str | None = None,
        username: str | None = None,
        password: str | None = None,
        timeout: float = 30.0,
        retries: RetryConfig | int | None = 2,
        cache: AsyncCacheBackend | None = None,
        cache_config: CacheConfig | None = None,
    ) -> None:
        inner = AsyncTransport(
            base_url, token=token, api_key=api_key,
            username=username, password=password, timeout=timeout,
            retries=coerce_retry_config(retries),
        )
        self._raw: AsyncTransport = (
            cast(AsyncTransport, AsyncCachingTransport(inner, cache, cache_config or CacheConfig()))
            if cache is not None
            else inner
        )
        self.nodes = AsyncNodesResource(self._raw)
        self.edges = AsyncEdgesResource(self._raw)
        self.search = AsyncSearchResource(self._raw)
        self.vector_indexes = AsyncVectorIndexesResource(self._raw)
        self.algorithms = AsyncAlgorithmsResource(self._raw)
        self.tenants = AsyncTenantsResource(self._raw)
        self.api_keys = AsyncApiKeysResource(self._raw)
        self.security = AsyncSecurityResource(self._raw)
        self.compliance = AsyncComplianceResource(self._raw)

    async def traverse(
        self,
        start_node_id: int,
        *,
        max_depth: int = 1,
        direction: str | None = None,
        edge_types: Sequence[str] | None = None,
    ) -> list[Node]:
        body: dict[str, Any] = {"start_node_id": start_node_id, "max_depth": max_depth}
        if direction is not None:
            body["direction"] = direction
        if edge_types is not None:
            body["edge_types"] = list(edge_types)
        res = await self._raw.request("POST", "/traverse", json=body)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    async def vector_search(
        self,
        property_name: str,
        query: Sequence[float],
        *,
        k: int = 10,
        ef: int | None = None,
        filter_labels: Sequence[str] | None = None,
        include_nodes: bool = False,
    ) -> list[SearchResult]:
        body: dict[str, Any] = {
            "property_name": property_name,
            "query_vector": list(query),
            "k": k,
            "include_nodes": include_nodes,
        }
        if ef is not None:
            body["ef"] = ef
        if filter_labels is not None:
            body["filter_labels"] = list(filter_labels)
        res = await self._raw.request("POST", "/vector-search", json=body)
        return [SearchResult.from_dict(d) for d in (res.data.get("results") or [])]

    async def retrieve(
        self,
        query: str,
        *,
        k: int | None = None,
        max_tokens: int | None = None,
        max_hops: int | None = None,
        alpha: float | None = None,
        beta: float | None = None,
        tau: float | None = None,
        labels: Sequence[str] | None = None,
        include_node: bool = False,
    ) -> RetrieveResult:
        body: dict[str, Any] = {"query": query}
        for name, val in (("k", k), ("max_tokens", max_tokens), ("max_hops", max_hops),
                          ("alpha", alpha), ("beta", beta), ("tau", tau)):
            if val is not None:
                body[name] = val
        if labels is not None:
            body["labels"] = list(labels)
        if include_node:
            body["include_node"] = True
        res = await self._raw.request("POST", "/v1/retrieve", json=body)
        return RetrieveResult.from_dict(res.data)

    async def embeddings(
        self,
        input: str | Sequence[str],
        *,
        model: str | None = None,
        dimensions: int | None = None,
    ) -> EmbeddingsResult:
        items = [input] if isinstance(input, str) else list(input)
        body: dict[str, Any] = {"input": items}
        if model is not None:
            body["model"] = model
        if dimensions is not None:
            body["dimensions"] = dimensions
        res = await self._raw.request("POST", "/v1/embeddings", json=body)
        return EmbeddingsResult.from_dict(res.data)

    async def query(
        self,
        cypher: str,
        *,
        parameters: Mapping[str, Any] | None = None,
        timeout_seconds: int | None = None,
    ) -> QueryResult:
        body: dict[str, Any] = {"query": cypher}
        if parameters is not None:
            body["parameters"] = dict(parameters)
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        res = await self._raw.request("POST", "/query", json=body)
        return QueryResult.from_dict(res.data)

    async def graphql(
        self,
        query: str,
        *,
        variables: Mapping[str, Any] | None = None,
        operation_name: str | None = None,
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"query": query}
        if variables is not None:
            body["variables"] = dict(variables)
        if operation_name is not None:
            body["operationName"] = operation_name
        res = await self._raw.request("POST", "/graphql", json=body)
        return res.data if isinstance(res.data, dict) else {}

    @property
    def cache_stats(self) -> dict[str, float] | None:
        """Cache hit/miss stats, or None when caching is disabled."""
        stats = getattr(self._raw, "stats", None)
        return stats if isinstance(stats, dict) else None

    async def aclose(self) -> None:
        await self._raw.aclose()

    async def __aenter__(self) -> "AsyncGraphDBClient":
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()
