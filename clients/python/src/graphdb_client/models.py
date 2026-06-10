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


@dataclass
class SearchHit:
    node_id: int
    score: float
    snippet: str | None = None
    fts_rank: int | None = None
    lsa_rank: int | None = None
    node: Node | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "SearchHit":
        raw_node = d.get("node")
        return cls(
            node_id=int(d["node_id"]),
            score=float(d.get("score", 0.0)),
            snippet=d.get("snippet") or None,
            fts_rank=int(d["fts_rank"]) if d.get("fts_rank") is not None else None,
            lsa_rank=int(d["lsa_rank"]) if d.get("lsa_rank") is not None else None,
            node=Node.from_dict(raw_node) if raw_node else None,
        )


@dataclass
class HybridSearchResult:
    hits: list[SearchHit] = field(default_factory=list)
    count: int = 0
    took_ms: int = 0
    degraded: str | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "HybridSearchResult":
        return cls(
            hits=[SearchHit.from_dict(x) for x in (d.get("results") or [])],
            count=int(d.get("count", 0)),
            took_ms=int(d.get("took_ms", 0)),
            degraded=d.get("degraded") or None,
        )


@dataclass
class VectorIndex:
    property_name: str
    dimensions: int | None = None
    metric: str | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "VectorIndex":
        return cls(
            property_name=str(d["property_name"]),
            dimensions=int(d["dimensions"]) if d.get("dimensions") is not None else None,
            metric=d.get("metric") or None,
        )


@dataclass
class RetrieveSource:
    node_id: int
    path: list[int] = field(default_factory=list)
    label: str | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "RetrieveSource":
        return cls(
            node_id=int(d["node_id"]),
            path=[int(x) for x in (d.get("path") or [])],
            label=d.get("label") or None,
        )


@dataclass
class RetrieveDocument:
    page_content: str
    node_id: int
    score: float
    source: RetrieveSource
    node: Node | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "RetrieveDocument":
        meta = d.get("metadata") or {}
        raw_node = meta.get("node")
        return cls(
            page_content=str(d.get("page_content", "")),
            node_id=int(meta["node_id"]),
            score=float(meta.get("score", 0.0)),
            source=RetrieveSource.from_dict(meta.get("source") or {"node_id": meta["node_id"]}),
            node=Node.from_dict(raw_node) if raw_node else None,
        )


@dataclass
class RetrieveResult:
    documents: list[RetrieveDocument] = field(default_factory=list)
    took_ms: int = 0
    degraded: str | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "RetrieveResult":
        return cls(
            documents=[RetrieveDocument.from_dict(x) for x in (d.get("documents") or [])],
            took_ms=int(d.get("took_ms", 0)),
            degraded=d.get("degraded") or None,
        )


@dataclass
class EmbeddingsResult:
    model: str
    vectors: list[list[float]] = field(default_factory=list)
    usage: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "EmbeddingsResult":
        data = sorted((d.get("data") or []), key=lambda x: int(x.get("index", 0)))
        return cls(
            model=str(d.get("model", "")),
            vectors=[[float(v) for v in (x.get("embedding") or [])] for x in data],
            usage=dict(d.get("usage") or {}),
        )


@dataclass
class QueryResult:
    columns: list[str] = field(default_factory=list)
    rows: list[dict[str, Any]] = field(default_factory=list)
    count: int = 0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "QueryResult":
        return cls(
            columns=list(d.get("columns") or []),
            rows=list(d.get("rows") or []),
            count=int(d.get("count", 0)),
        )


@dataclass
class AlgorithmResult:
    algorithm: str
    results: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "AlgorithmResult":
        return cls(
            algorithm=str(d.get("algorithm", "")),
            results=dict(d.get("results") or {}),
        )


@dataclass
class ShortestPath:
    path: list[int] = field(default_factory=list)
    length: int = 0
    found: bool = False

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "ShortestPath":
        return cls(
            path=[int(x) for x in (d.get("path") or [])],
            length=int(d.get("length", 0)),
            found=bool(d.get("found", False)),
        )


@dataclass
class Tenant:
    id: str
    name: str
    status: str
    description: str | None = None
    quota: dict[str, Any] | None = None
    metadata: dict[str, Any] = field(default_factory=dict)
    created_at: int = 0
    updated_at: int = 0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "Tenant":
        return cls(
            id=str(d["id"]),
            name=str(d.get("name", "")),
            status=str(d.get("status", "")),
            description=d.get("description") or None,
            quota=dict(d["quota"]) if d.get("quota") is not None else None,
            metadata=dict(d.get("metadata") or {}),
            created_at=int(d.get("created_at", 0)),
            updated_at=int(d.get("updated_at", 0)),
        )


@dataclass
class TenantUsage:
    tenant_id: str
    node_count: int = 0
    edge_count: int = 0
    storage_bytes: int = 0
    quota_usage: dict[str, Any] | None = None
    last_updated: int = 0

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "TenantUsage":
        return cls(
            tenant_id=str(d.get("tenant_id", "")),
            node_count=int(d.get("node_count", 0)),
            edge_count=int(d.get("edge_count", 0)),
            storage_bytes=int(d.get("storage_bytes", 0)),
            quota_usage=dict(d["quota_usage"]) if d.get("quota_usage") is not None else None,
            last_updated=int(d.get("last_updated", 0)),
        )


@dataclass
class APIKey:
    id: str
    name: str
    prefix: str
    permissions: list[str] = field(default_factory=list)
    created: str | None = None
    expires: str | None = None
    last_used: str | None = None
    revoked: bool = False

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "APIKey":
        return cls(
            id=str(d["id"]),
            name=str(d.get("name", "")),
            prefix=str(d.get("prefix", "")),
            permissions=list(d.get("permissions") or []),
            created=d.get("created") or None,
            expires=d.get("expires") or None,
            last_used=d.get("last_used") or None,
            revoked=bool(d.get("revoked", False)),
        )


@dataclass
class CreatedAPIKey:
    # repr=False keeps the one-time plaintext key out of the dataclass's
    # auto-generated repr (security audit M-13): otherwise it lands in
    # logs, tracebacks, and crash reporters as CreatedAPIKey(key='gdb_...').
    key: str = field(repr=False)
    id: str
    name: str
    prefix: str
    created: str | None = None
    expires: str | None = None

    @classmethod
    def from_dict(cls, d: Mapping[str, Any]) -> "CreatedAPIKey":
        return cls(
            key=str(d.get("key", "")),
            id=str(d["id"]),
            name=str(d.get("name", "")),
            prefix=str(d.get("prefix", "")),
            created=d.get("created") or None,
            expires=d.get("expires") or None,
        )
