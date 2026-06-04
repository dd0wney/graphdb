from __future__ import annotations

from typing import Any, Mapping, Sequence

from .._transport import Transport
from ..models import Edge


class EdgesResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        from_node_id: int,
        to_node_id: int,
        edge_type: str,
        *,
        properties: Mapping[str, Any] | None = None,
        weight: float = 0.0,
    ) -> Edge:
        res = self._t.request("POST", "/edges", json={
            "from_node_id": from_node_id,
            "to_node_id": to_node_id,
            "type": edge_type,
            "properties": dict(properties or {}),
            "weight": weight,
        })
        return Edge.from_dict(res.data)

    def get(self, edge_id: int) -> Edge:
        res = self._t.request("GET", f"/edges/{edge_id}")
        return Edge.from_dict(res.data)

    def update(self, edge_id: int, properties: Mapping[str, Any]) -> Edge:
        res = self._t.request("PUT", f"/edges/{edge_id}", json={"properties": dict(properties)})
        return Edge.from_dict(res.data)

    def delete(self, edge_id: int) -> None:
        self._t.request("DELETE", f"/edges/{edge_id}")

    def batch_create(self, edges: Sequence[Mapping[str, Any]]) -> list[Edge]:
        payload = {"edges": [
            {
                "from_node_id": e["from_node_id"],
                "to_node_id": e["to_node_id"],
                "type": e["type"],
                "properties": dict(e.get("properties", {})),
                "weight": float(e.get("weight", 0.0)),
            }
            for e in edges
        ]}
        res = self._t.request("POST", "/edges/batch", json=payload)
        return [Edge.from_dict(d) for d in (res.data.get("edges") or [])]
