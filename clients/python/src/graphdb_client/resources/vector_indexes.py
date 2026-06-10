from __future__ import annotations

from typing import Any

from .._path import quote_segment
from .._transport import Transport
from ..models import VectorIndex


class VectorIndexesResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        property_name: str,
        dimensions: int,
        *,
        m: int | None = None,
        ef_construction: int | None = None,
        metric: str | None = None,
    ) -> VectorIndex:
        """Create a vector index (POST /vector-indexes). Optional HNSW params use
        the server defaults (16 / 200 / "cosine") when omitted."""
        body: dict[str, Any] = {"property_name": property_name, "dimensions": dimensions}
        if m is not None:
            body["m"] = m
        if ef_construction is not None:
            body["ef_construction"] = ef_construction
        if metric is not None:
            body["metric"] = metric
        res = self._t.request("POST", "/vector-indexes", json=body)
        return VectorIndex.from_dict(res.data)

    def list(self) -> list[VectorIndex]:
        """List vector indexes (GET /vector-indexes)."""
        res = self._t.request("GET", "/vector-indexes")
        return [VectorIndex.from_dict(d) for d in (res.data.get("indexes") or [])]

    def get(self, property_name: str) -> VectorIndex:
        """Get one vector index (GET /vector-indexes/{property_name})."""
        res = self._t.request("GET", f"/vector-indexes/{quote_segment(property_name)}")
        return VectorIndex.from_dict(res.data)

    def delete(self, property_name: str) -> None:
        """Drop a vector index (DELETE /vector-indexes/{property_name})."""
        self._t.request("DELETE", f"/vector-indexes/{quote_segment(property_name)}")
