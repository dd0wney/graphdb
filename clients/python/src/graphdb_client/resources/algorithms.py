from __future__ import annotations

from typing import Any, Mapping

from .._transport import Transport
from ..models import AlgorithmResult, ShortestPath


class AlgorithmsResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def run(
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
        res = self._t.request("POST", "/algorithms", json=body)
        return AlgorithmResult.from_dict(res.data)

    def shortest_path(
        self, start_node_id: int, end_node_id: int, *, max_depth: int | None = None
    ) -> ShortestPath:
        """Shortest path between two nodes (POST /shortest-path). `found` is False
        (with an empty path) when no path exists within max_depth."""
        body: dict[str, Any] = {"start_node_id": start_node_id, "end_node_id": end_node_id}
        if max_depth is not None:
            body["max_depth"] = max_depth
        res = self._t.request("POST", "/shortest-path", json=body)
        return ShortestPath.from_dict(res.data)
