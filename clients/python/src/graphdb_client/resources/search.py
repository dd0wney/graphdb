from __future__ import annotations

from typing import Any, Sequence

from .._transport import Transport
from ..models import HybridSearchResult, SearchHit


class SearchResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def fulltext(
        self,
        query: str,
        *,
        limit: int = 20,
        offset: int = 0,
        labels: Sequence[str] | None = None,
        include_content: bool = False,
        include_nodes: bool = False,
    ) -> list[SearchHit]:
        """Full-text search (POST /search)."""
        body: dict[str, Any] = {
            "query": query, "limit": limit, "offset": offset,
            "include_content": include_content, "include_nodes": include_nodes,
        }
        if labels is not None:
            body["labels"] = list(labels)
        res = self._t.request("POST", "/search", json=body)
        return [SearchHit.from_dict(d) for d in (res.data.get("results") or [])]

    def hybrid(
        self,
        query: str,
        *,
        limit: int = 20,
        offset: int = 0,
        labels: Sequence[str] | None = None,
        alpha: float | None = None,
        include_content: bool = False,
        include_nodes: bool = False,
    ) -> HybridSearchResult:
        """RRF-merged full-text + LSA hybrid search (POST /hybrid-search).

        The result's `degraded` field is non-None when the server fell back to a
        single stage ("no-lsa-index" | "query-out-of-vocabulary" | "no-fts-match").
        """
        body: dict[str, Any] = {
            "query": query, "limit": limit, "offset": offset,
            "include_content": include_content, "include_nodes": include_nodes,
        }
        if labels is not None:
            body["labels"] = list(labels)
        if alpha is not None:
            body["alpha"] = alpha
        res = self._t.request("POST", "/hybrid-search", json=body)
        return HybridSearchResult.from_dict(res.data)
