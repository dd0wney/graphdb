from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Mapping


@dataclass
class Node:
    id: int
    labels: list[str] = field(default_factory=list)
    properties: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "Node":
        return cls(
            id=int(d["id"]),
            labels=list(d.get("labels") or []),
            properties=dict(d.get("properties") or {}),
        )


@dataclass
class Edge:
    id: int
    from_node_id: int
    to_node_id: int
    type: str
    properties: dict[str, Any] = field(default_factory=dict)
    weight: float = 0.0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "Edge":
        return cls(
            id=int(d["id"]),
            from_node_id=int(d["from_node_id"]),
            to_node_id=int(d["to_node_id"]),
            type=str(d["type"]),
            properties=dict(d.get("properties") or {}),
            weight=float(d.get("weight", 0.0)),
        )


@dataclass
class SearchResult:
    node_id: int
    distance: float
    score: float
    node: Node | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "SearchResult":
        raw_node = d.get("node")
        return cls(
            node_id=int(d["node_id"]),
            distance=float(d.get("distance", 0.0)),
            score=float(d.get("score", 0.0)),
            node=Node.from_dict(raw_node) if raw_node else None,
        )
