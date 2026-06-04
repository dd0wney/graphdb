from __future__ import annotations

from typing import Any, Iterator, Mapping, Sequence

from .._transport import Transport
from ..models import Node


class NodesResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(self, labels: Sequence[str], properties: Mapping[str, Any] | None = None) -> Node:
        res = self._t.request("POST", "/nodes",
                              json={"labels": list(labels), "properties": dict(properties or {})})
        return Node.from_dict(res.data)

    def get(self, node_id: int) -> Node:
        res = self._t.request("GET", f"/nodes/{node_id}")
        return Node.from_dict(res.data)

    def update(self, node_id: int, properties: Mapping[str, Any]) -> Node:
        res = self._t.request("PUT", f"/nodes/{node_id}", json={"properties": dict(properties)})
        return Node.from_dict(res.data)

    def delete(self, node_id: int) -> None:
        self._t.request("DELETE", f"/nodes/{node_id}")

    def batch_create(self, nodes: Sequence[Mapping[str, Any]]) -> list[Node]:
        payload = {"nodes": [
            {"labels": list(n.get("labels", [])), "properties": dict(n.get("properties", {}))}
            for n in nodes
        ]}
        res = self._t.request("POST", "/nodes/batch", json=payload)
        return [Node.from_dict(d) for d in (res.data.get("nodes") or [])]

    def list(self, *, label: str | None = None, page_size: int = 100) -> Iterator[Node]:
        """Yield every node (optionally filtered by label), auto-following X-Next-Cursor."""
        cursor: str | None = None
        prev_cursor: str | None = None
        while True:
            params: dict[str, Any] = {"limit": page_size}
            if label is not None:
                params["label"] = label
            if cursor is not None:
                params["cursor"] = cursor
            res = self._t.request("GET", "/nodes", params=params)
            for d in res.data or []:
                yield Node.from_dict(d)
            cursor = res.headers.get("X-Next-Cursor")
            # Terminate on absent/empty cursor, or if the server fails to advance
            # it (a non-spec-compliant server returning the same cursor forever
            # would otherwise loop indefinitely).
            if not cursor or cursor == prev_cursor:
                return
            prev_cursor = cursor
