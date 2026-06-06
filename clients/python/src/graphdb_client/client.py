from __future__ import annotations

from types import TracebackType
from typing import Any, Mapping, Sequence, cast

from ._caching import CachingTransport
from ._retry import RetryConfig, coerce_retry_config
from ._transport import Transport
from .cache import CacheBackend, CacheConfig
from .models import (
    EmbeddingsResult,
    Node,
    QueryResult,
    RetrieveResult,
    SearchResult,
)
from .resources.algorithms import AlgorithmsResource
from .resources.api_keys import ApiKeysResource
from .resources.compliance import ComplianceResource
from .resources.edges import EdgesResource
from .resources.nodes import NodesResource
from .resources.search import SearchResource
from .resources.security import SecurityResource
from .resources.tenants import TenantsResource
from .resources.vector_indexes import VectorIndexesResource


class GraphDBClient:
    """Top-level client: assembles sub-resources and exposes high-level operations."""

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
        cache: CacheBackend | None = None,
        cache_config: CacheConfig | None = None,
    ) -> None:
        inner = Transport(
            base_url,
            token=token,
            api_key=api_key,
            username=username,
            password=password,
            timeout=timeout,
            retries=coerce_retry_config(retries),
        )
        # CachingTransport implements the request()/close() surface resources + client use;
        # cast keeps the resource transport type as Transport (it duck-types correctly).
        self._raw: Transport = (
            cast(Transport, CachingTransport(inner, cache, cache_config or CacheConfig()))
            if cache is not None
            else inner
        )
        self.nodes = NodesResource(self._raw)
        self.edges = EdgesResource(self._raw)
        self.search = SearchResource(self._raw)
        self.vector_indexes = VectorIndexesResource(self._raw)
        self.algorithms = AlgorithmsResource(self._raw)
        self.tenants = TenantsResource(self._raw)
        self.api_keys = ApiKeysResource(self._raw)
        self.security = SecurityResource(self._raw)
        self.compliance = ComplianceResource(self._raw)

    def traverse(
        self,
        start_node_id: int,
        *,
        max_depth: int = 1,
        direction: str | None = None,
        edge_types: Sequence[str] | None = None,
    ) -> list[Node]:
        """Traverse the graph from a starting node.

        POST /traverse — returns all reachable nodes within max_depth hops.
        Optional direction ("in"|"out"|"both") and edge_types filter the walk.
        """
        body: dict[str, Any] = {"start_node_id": start_node_id, "max_depth": max_depth}
        if direction is not None:
            body["direction"] = direction
        if edge_types is not None:
            body["edge_types"] = list(edge_types)
        res = self._raw.request("POST", "/traverse", json=body)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    def vector_search(
        self,
        property_name: str,
        query: Sequence[float],
        *,
        k: int = 10,
        ef: int | None = None,
        filter_labels: Sequence[str] | None = None,
        include_nodes: bool = False,
    ) -> list[SearchResult]:
        """ANN vector search over a property index.

        POST /vector-search — returns up to k nearest neighbours with distance
        and score.  Pass include_nodes=True to embed full node data in each result.
        ef controls HNSW search-time accuracy/speed trade-off (server default applies
        when omitted).
        """
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
        res = self._raw.request("POST", "/vector-search", json=body)
        return [SearchResult.from_dict(d) for d in (res.data.get("results") or [])]

    def retrieve(
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
        """Graph-augmented retrieval (POST /v1/retrieve). Each document carries the
        graph signal in source.path. `degraded` mirrors hybrid-search fallback."""
        body: dict[str, Any] = {"query": query}
        for name, val in (("k", k), ("max_tokens", max_tokens), ("max_hops", max_hops),
                          ("alpha", alpha), ("beta", beta), ("tau", tau)):
            if val is not None:
                body[name] = val
        if labels is not None:
            body["labels"] = list(labels)
        if include_node:
            body["include_node"] = True
        res = self._raw.request("POST", "/v1/retrieve", json=body)
        return RetrieveResult.from_dict(res.data)

    def embeddings(
        self,
        input: str | Sequence[str],
        *,
        model: str | None = None,
        dimensions: int | None = None,
    ) -> EmbeddingsResult:
        """OpenAI-shaped embeddings (POST /v1/embeddings). A str is sent as a
        one-element input array; vectors come back ordered to match the input."""
        items = [input] if isinstance(input, str) else list(input)
        body: dict[str, Any] = {"input": items}
        if model is not None:
            body["model"] = model
        if dimensions is not None:
            body["dimensions"] = dimensions
        res = self._raw.request("POST", "/v1/embeddings", json=body)
        return EmbeddingsResult.from_dict(res.data)

    def query(
        self,
        cypher: str,
        *,
        parameters: Mapping[str, Any] | None = None,
        timeout_seconds: int | None = None,
    ) -> QueryResult:
        """Run a Cypher query (POST /query)."""
        body: dict[str, Any] = {"query": cypher}
        if parameters is not None:
            body["parameters"] = dict(parameters)
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        res = self._raw.request("POST", "/query", json=body)
        return QueryResult.from_dict(res.data)

    def graphql(
        self,
        query: str,
        *,
        variables: Mapping[str, Any] | None = None,
        operation_name: str | None = None,
    ) -> dict[str, Any]:
        """Execute a GraphQL document (POST /graphql). Returns the raw response
        dict ({"data": ..., "errors": ...}); GraphQL-level errors are returned in
        the dict, not raised (only HTTP >= 400 raises)."""
        body: dict[str, Any] = {"query": query}
        if variables is not None:
            body["variables"] = dict(variables)
        if operation_name is not None:
            body["operationName"] = operation_name
        res = self._raw.request("POST", "/graphql", json=body)
        return res.data if isinstance(res.data, dict) else {}

    @property
    def cache_stats(self) -> dict[str, float] | None:
        """Cache hit/miss stats, or None when caching is disabled."""
        stats = getattr(self._raw, "stats", None)
        return stats if isinstance(stats, dict) else None

    def close(self) -> None:
        """Close the underlying HTTP transport. Safe to call multiple times."""
        self._raw.close()

    def __enter__(self) -> "GraphDBClient":
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self.close()
