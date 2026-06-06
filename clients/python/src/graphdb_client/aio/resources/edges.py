from __future__ import annotations

from typing import Any, Mapping, Sequence

from ...models import Edge
from ..transport import AsyncTransport


class AsyncEdgesResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._t = transport

    async def create(
        self,
        from_node_id: int,
        to_node_id: int,
        edge_type: str,
        *,
        properties: Mapping[str, Any] | None = None,
        weight: float = 0.0,
    ) -> Edge:
        res = await self._t.request("POST", "/edges", json={
            "from_node_id": from_node_id,
            "to_node_id": to_node_id,
            "type": edge_type,
            "properties": dict(properties or {}),
            "weight": weight,
        })
        return Edge.from_dict(res.data)

    async def get(self, edge_id: int) -> Edge:
        res = await self._t.request("GET", f"/edges/{edge_id}")
        return Edge.from_dict(res.data)

    async def update(
        self,
        edge_id: int,
        properties: Mapping[str, Any] | None = None,
        *,
        weight: float | None = None,
    ) -> Edge:
        body: dict[str, Any] = {}
        if properties is not None:
            body["properties"] = dict(properties)
        if weight is not None:
            body["weight"] = weight
        res = await self._t.request("PUT", f"/edges/{edge_id}", json=body)
        return Edge.from_dict(res.data)

    async def delete(self, edge_id: int) -> None:
        await self._t.request("DELETE", f"/edges/{edge_id}")

    async def batch_create(self, edges: Sequence[Mapping[str, Any]]) -> list[Edge]:
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
        res = await self._t.request("POST", "/edges/batch", json=payload)
        return [Edge.from_dict(d) for d in (res.data.get("edges") or [])]
