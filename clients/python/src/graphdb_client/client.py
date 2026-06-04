from __future__ import annotations

from types import TracebackType
from typing import Any, Sequence

from ._transport import Transport
from .models import Node, SearchResult
from .resources.edges import EdgesResource
from .resources.nodes import NodesResource


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
    ) -> None:
        self._raw = Transport(
            base_url,
            token=token,
            api_key=api_key,
            username=username,
            password=password,
            timeout=timeout,
        )
        self.nodes = NodesResource(self._raw)
        self.edges = EdgesResource(self._raw)

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
