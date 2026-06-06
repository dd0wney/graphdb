from __future__ import annotations

from typing import Any, Mapping

from ...models import AlgorithmResult, ShortestPath
from ..transport import AsyncTransport


class AsyncAlgorithmsResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._t = transport

    async def run(
        self, algorithm: str, *, parameters: Mapping[str, Any] | None = None
    ) -> AlgorithmResult:
        """Run a graph algorithm (POST /algorithms).

        algorithm is one of the server-supported names ("pagerank", "betweenness",
        "louvain", ...). results is a freeform dict whose shape depends on the
        algorithm.
        """
        body: dict[str, Any] = {"algorithm": algorithm}
        if parameters is not None:
            body["parameters"] = dict(parameters)
        res = await self._t.request("POST", "/algorithms", json=body)
        return AlgorithmResult.from_dict(res.data)

    async def shortest_path(
        self, start_node_id: int, end_node_id: int, *, max_depth: int | None = None
    ) -> ShortestPath:
        """Shortest path between two nodes (POST /shortest-path). `found` is False
        (with an empty path) when no path exists within max_depth."""
        body: dict[str, Any] = {"start_node_id": start_node_id, "end_node_id": end_node_id}
        if max_depth is not None:
            body["max_depth"] = max_depth
        res = await self._t.request("POST", "/shortest-path", json=body)
        return ShortestPath.from_dict(res.data)
