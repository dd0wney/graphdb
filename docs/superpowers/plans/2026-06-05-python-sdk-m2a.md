# Python SDK M2a Implementation Plan (search & query slice)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add typed, ergonomic facades to the graphdb Python SDK for the search & query surface — full-text/hybrid search, vector-index management, `/v1/retrieve`, embeddings, Cypher `/query`, `/graphql`, and algorithms/shortest-path.

**Architecture:** Mirror M1 exactly. New dataclass models in `models.py` (each with a `from_dict` classmethod). New resources (`search`, `vector_indexes`, `algorithms`) under `resources/`, each holding a `Transport` and mapping responses through `from_dict`. Four single-verb top-level methods on `GraphDBClient` (`retrieve`, `embeddings`, `query`, `graphql`). `Transport`, the `_raw` escape hatch, and the error hierarchy are reused unchanged.

**Tech Stack:** Python 3.9+, `httpx` (only runtime dep), stdlib `dataclasses`; tests use `respx` + `pytest`; `ruff` + `mypy` gates. All commands run from `clients/python/` via `uv run`.

**Spec:** `docs/superpowers/specs/2026-06-05-python-sdk-m2a-design.md`.

**Conventions (from M1 — follow exactly):**
- Resource: `class FooResource: def __init__(self, transport: Transport) -> None: self._t = transport`; methods call `self._t.request(METHOD, path, json=..., params=...)` → `ApiResult` (`.data`, `.headers`) → `Model.from_dict(...)`.
- Model: `@dataclass` + `@classmethod from_dict(cls, d: Mapping[str, Any])` with `int()/float()/str()/list()/dict()` coercion; nested `Node.from_dict(raw) if raw else None`.
- Optional body fields are **omitted when `None`** (the server applies defaults; for `alpha` the server distinguishes `0` from unset).
- Test: `@respx.mock` + `respx.post(f"{base_url}/path").mock(return_value=httpx.Response(status, json={...}))`; build via `Transport(base_url, token="tok")`.
- Run tests: `uv run pytest tests/<file> -v`. Lint/type: `uv run ruff check .` and `uv run mypy src`.

---

### Task 1: M2a models

**Files:**
- Modify: `clients/python/src/graphdb_client/models.py`
- Test: `clients/python/tests/test_models_m2a.py`

- [ ] **Step 1: Write the failing test**

Create `clients/python/tests/test_models_m2a.py`:

```python
from __future__ import annotations

from graphdb_client.models import (
    AlgorithmResult,
    EmbeddingsResult,
    HybridSearchResult,
    QueryResult,
    RetrieveDocument,
    RetrieveResult,
    SearchHit,
    ShortestPath,
    VectorIndex,
)


def test_search_hit_fulltext_has_no_ranks():
    h = SearchHit.from_dict({"node_id": 7, "score": 1.5, "snippet": "hi"})
    assert h.node_id == 7 and h.score == 1.5 and h.snippet == "hi"
    assert h.fts_rank is None and h.lsa_rank is None and h.node is None


def test_search_hit_hybrid_carries_ranks_and_node():
    h = SearchHit.from_dict({
        "node_id": 3, "score": 0.9, "fts_rank": 1, "lsa_rank": 2,
        "node": {"id": 3, "labels": ["Doc"], "properties": {"t": "x"}},
    })
    assert h.fts_rank == 1 and h.lsa_rank == 2
    assert h.node is not None and h.node.id == 3


def test_hybrid_result_maps_results_and_degraded():
    r = HybridSearchResult.from_dict({
        "results": [{"node_id": 1, "score": 0.5}],
        "count": 1, "took_ms": 4, "degraded": "no-lsa-index",
    })
    assert len(r.hits) == 1 and r.count == 1 and r.took_ms == 4
    assert r.degraded == "no-lsa-index"


def test_hybrid_result_degraded_absent_is_none():
    r = HybridSearchResult.from_dict({"results": [], "count": 0, "took_ms": 1})
    assert r.degraded is None


def test_vector_index():
    vi = VectorIndex.from_dict({"property_name": "embedding", "dimensions": 384, "metric": "cosine"})
    assert vi.property_name == "embedding" and vi.dimensions == 384 and vi.metric == "cosine"


def test_retrieve_document_flattens_metadata_and_keeps_path():
    doc = RetrieveDocument.from_dict({
        "page_content": "chunk",
        "metadata": {
            "node_id": 9, "score": 0.8,
            "source": {"node_id": 5, "label": "Doc", "path": [5, 7, 9]},
            "node": {"id": 9, "labels": ["Doc"], "properties": {}},
        },
    })
    assert doc.page_content == "chunk" and doc.node_id == 9 and doc.score == 0.8
    assert doc.source.path == [5, 7, 9] and doc.source.label == "Doc"
    assert doc.node is not None and doc.node.id == 9


def test_retrieve_result():
    r = RetrieveResult.from_dict({
        "documents": [{"page_content": "c", "metadata": {"node_id": 1, "score": 0.1,
                       "source": {"node_id": 1, "path": [1]}}}],
        "took_ms": 12, "degraded": "query-out-of-vocabulary",
    })
    assert len(r.documents) == 1 and r.took_ms == 12
    assert r.degraded == "query-out-of-vocabulary"


def test_embeddings_result_orders_vectors_by_index():
    r = EmbeddingsResult.from_dict({
        "object": "list", "model": "lsa",
        "data": [
            {"object": "embedding", "embedding": [0.2], "index": 1},
            {"object": "embedding", "embedding": [0.1], "index": 0},
        ],
        "usage": {"prompt_tokens": 3, "total_tokens": 3},
    })
    assert r.vectors == [[0.1], [0.2]]  # reordered by index
    assert r.model == "lsa" and r.usage["total_tokens"] == 3


def test_query_result_rows_stay_dicts():
    r = QueryResult.from_dict({"columns": ["n"], "rows": [{"n": 1}], "count": 1, "time": "1ms"})
    assert r.columns == ["n"] and r.rows == [{"n": 1}] and r.count == 1


def test_algorithm_result_freeform():
    r = AlgorithmResult.from_dict({"algorithm": "pagerank", "results": {"1": 0.15}, "time": "2ms"})
    assert r.algorithm == "pagerank" and r.results == {"1": 0.15}


def test_shortest_path_found_flag_distinct_from_empty():
    r = ShortestPath.from_dict({"path": [], "length": 0, "found": False})
    assert r.found is False and r.path == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd clients/python && uv run pytest tests/test_models_m2a.py -v`
Expected: FAIL — `ImportError: cannot import name 'SearchHit' …`

- [ ] **Step 3: Append the models**

Append to `clients/python/src/graphdb_client/models.py` (after `SearchResult`):

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd clients/python && uv run pytest tests/test_models_m2a.py -v`
Expected: PASS (11 tests)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/models.py clients/python/tests/test_models_m2a.py
git commit -m "feat(sdk): add M2a result models"
```

---

### Task 2: SearchResource (fulltext + hybrid)

**Files:**
- Create: `clients/python/src/graphdb_client/resources/search.py`
- Test: `clients/python/tests/test_search.py`

- [ ] **Step 1: Write the failing test**

Create `clients/python/tests/test_search.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.search import SearchResource


def _res(base_url):
    return SearchResource(Transport(base_url, token="tok"))


@respx.mock
def test_fulltext_maps_hits(base_url):
    route = respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 1, "score": 2.0, "snippet": "hi"}], "count": 1, "took_ms": 3,
    }))
    hits = _res(base_url).fulltext("hello", labels=["Doc"])
    assert len(hits) == 1 and hits[0].node_id == 1 and hits[0].snippet == "hi"
    body = route.calls.last.request.read()
    assert b'"labels"' in body and b'"hello"' in body


@respx.mock
def test_hybrid_maps_degraded_and_ranks(base_url):
    respx.post(f"{base_url}/hybrid-search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 4, "score": 0.7, "fts_rank": 1, "lsa_rank": -1}],
        "count": 1, "took_ms": 8, "degraded": "no-lsa-index",
    }))
    r = _res(base_url).hybrid("q")
    assert r.degraded == "no-lsa-index" and r.hits[0].fts_rank == 1


@respx.mock
def test_hybrid_omits_alpha_when_none_sends_when_zero(base_url):
    route = respx.post(f"{base_url}/hybrid-search").mock(
        return_value=httpx.Response(200, json={"results": [], "count": 0, "took_ms": 1}))
    _res(base_url).hybrid("q")
    assert b'"alpha"' not in route.calls.last.request.read()
    _res(base_url).hybrid("q", alpha=0.0)
    assert b'"alpha"' in route.calls.last.request.read()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd clients/python && uv run pytest tests/test_search.py -v`
Expected: FAIL — `ModuleNotFoundError: …resources.search`

- [ ] **Step 3: Create the resource**

Create `clients/python/src/graphdb_client/resources/search.py`:

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd clients/python && uv run pytest tests/test_search.py -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/resources/search.py clients/python/tests/test_search.py
git commit -m "feat(sdk): SearchResource (fulltext + hybrid)"
```

---

### Task 3: VectorIndexesResource (create/list/get/delete)

**Files:**
- Create: `clients/python/src/graphdb_client/resources/vector_indexes.py`
- Test: `clients/python/tests/test_vector_indexes.py`

- [ ] **Step 1: Write the failing test**

Create `clients/python/tests/test_vector_indexes.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.vector_indexes import VectorIndexesResource


def _res(base_url):
    return VectorIndexesResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_omits_optional_params_when_none(base_url):
    route = respx.post(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(
        201, json={"property_name": "embedding", "dimensions": 384, "metric": "cosine"}))
    vi = _res(base_url).create("embedding", 384)
    assert vi.property_name == "embedding" and vi.dimensions == 384
    body = route.calls.last.request.read()
    assert b'"m"' not in body and b'"ef_construction"' not in body and b'"metric"' not in body


@respx.mock
def test_create_sends_optional_params(base_url):
    route = respx.post(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(
        201, json={"property_name": "e", "dimensions": 8, "metric": "dot_product"}))
    _res(base_url).create("e", 8, m=32, ef_construction=400, metric="dot_product")
    body = route.calls.last.request.read()
    assert b'"m"' in body and b'"ef_construction"' in body and b'"dot_product"' in body


@respx.mock
def test_list(base_url):
    respx.get(f"{base_url}/vector-indexes").mock(return_value=httpx.Response(200, json={
        "indexes": [{"property_name": "a", "dimensions": 3, "metric": "cosine"}], "count": 1}))
    got = _res(base_url).list()
    assert len(got) == 1 and got[0].property_name == "a"


@respx.mock
def test_get(base_url):
    respx.get(f"{base_url}/vector-indexes/embedding").mock(return_value=httpx.Response(
        200, json={"property_name": "embedding", "dimensions": 384, "metric": "cosine"}))
    assert _res(base_url).get("embedding").dimensions == 384


@respx.mock
def test_delete_returns_none(base_url):
    respx.delete(f"{base_url}/vector-indexes/embedding").mock(return_value=httpx.Response(204))
    assert _res(base_url).delete("embedding") is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd clients/python && uv run pytest tests/test_vector_indexes.py -v`
Expected: FAIL — `ModuleNotFoundError: …resources.vector_indexes`

- [ ] **Step 3: Create the resource**

Create `clients/python/src/graphdb_client/resources/vector_indexes.py`:

```python
from __future__ import annotations

from typing import Any

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
        res = self._t.request("GET", f"/vector-indexes/{property_name}")
        return VectorIndex.from_dict(res.data)

    def delete(self, property_name: str) -> None:
        """Drop a vector index (DELETE /vector-indexes/{property_name})."""
        self._t.request("DELETE", f"/vector-indexes/{property_name}")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd clients/python && uv run pytest tests/test_vector_indexes.py -v`
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/resources/vector_indexes.py clients/python/tests/test_vector_indexes.py
git commit -m "feat(sdk): VectorIndexesResource (create/list/get/delete)"
```

---

### Task 4: AlgorithmsResource (run + shortest_path)

**Files:**
- Create: `clients/python/src/graphdb_client/resources/algorithms.py`
- Test: `clients/python/tests/test_algorithms.py`

- [ ] **Step 1: Write the failing test**

Create `clients/python/tests/test_algorithms.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.algorithms import AlgorithmsResource


def _res(base_url):
    return AlgorithmsResource(Transport(base_url, token="tok"))


@respx.mock
def test_run_maps_results(base_url):
    route = respx.post(f"{base_url}/algorithms").mock(return_value=httpx.Response(200, json={
        "algorithm": "pagerank", "results": {"1": 0.15, "2": 0.85}, "time": "2ms"}))
    r = _res(base_url).run("pagerank", parameters={"iterations": 20})
    assert r.algorithm == "pagerank" and r.results["2"] == 0.85
    assert b'"iterations"' in route.calls.last.request.read()


@respx.mock
def test_run_omits_parameters_when_none(base_url):
    route = respx.post(f"{base_url}/algorithms").mock(return_value=httpx.Response(
        200, json={"algorithm": "louvain", "results": {}, "time": "1ms"}))
    _res(base_url).run("louvain")
    assert b'"parameters"' not in route.calls.last.request.read()


@respx.mock
def test_shortest_path_found(base_url):
    respx.post(f"{base_url}/shortest-path").mock(return_value=httpx.Response(200, json={
        "path": [1, 2, 3], "length": 3, "found": True, "time": "1ms"}))
    sp = _res(base_url).shortest_path(1, 3, max_depth=5)
    assert sp.found is True and sp.path == [1, 2, 3] and sp.length == 3


@respx.mock
def test_shortest_path_not_found(base_url):
    respx.post(f"{base_url}/shortest-path").mock(return_value=httpx.Response(200, json={
        "path": [], "length": 0, "found": False, "time": "1ms"}))
    sp = _res(base_url).shortest_path(1, 99)
    assert sp.found is False and sp.path == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd clients/python && uv run pytest tests/test_algorithms.py -v`
Expected: FAIL — `ModuleNotFoundError: …resources.algorithms`

- [ ] **Step 3: Create the resource**

Create `clients/python/src/graphdb_client/resources/algorithms.py`:

```python
from __future__ import annotations

from typing import Any, Mapping

from .._transport import Transport
from ..models import AlgorithmResult, ShortestPath


class AlgorithmsResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def run(self, algorithm: str, *, parameters: Mapping[str, Any] | None = None) -> AlgorithmResult:
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd clients/python && uv run pytest tests/test_algorithms.py -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/resources/algorithms.py clients/python/tests/test_algorithms.py
git commit -m "feat(sdk): AlgorithmsResource (run + shortest_path)"
```

---

### Task 5: Top-level methods (retrieve, embeddings, query, graphql) + wire resources

**Files:**
- Modify: `clients/python/src/graphdb_client/client.py`
- Test: `clients/python/tests/test_retrieve.py`, `clients/python/tests/test_embeddings.py`, `clients/python/tests/test_query_graphql.py`

- [ ] **Step 1: Write the failing tests**

Create `clients/python/tests/test_retrieve.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client import GraphDBClient


def _c(base_url):
    return GraphDBClient(base_url, token="tok")


@respx.mock
def test_retrieve_maps_documents_and_path(base_url):
    route = respx.post(f"{base_url}/v1/retrieve").mock(return_value=httpx.Response(200, json={
        "documents": [{"page_content": "chunk", "metadata": {
            "node_id": 9, "score": 0.8, "source": {"node_id": 5, "label": "Doc", "path": [5, 9]}}}],
        "took_ms": 7, "degraded": "no-lsa-index",
    }))
    r = _c(base_url).retrieve("q", k=5, include_node=True)
    assert r.documents[0].source.path == [5, 9] and r.degraded == "no-lsa-index"
    body = route.calls.last.request.read()
    assert b'"k"' in body and b'"include_node"' in body


@respx.mock
def test_retrieve_omits_unset_tuning_params(base_url):
    route = respx.post(f"{base_url}/v1/retrieve").mock(return_value=httpx.Response(
        200, json={"documents": [], "took_ms": 1}))
    _c(base_url).retrieve("q")
    body = route.calls.last.request.read()
    for absent in (b'"k"', b'"alpha"', b'"beta"', b'"tau"', b'"max_hops"', b'"labels"'):
        assert absent not in body
```

Create `clients/python/tests/test_embeddings.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client import GraphDBClient


def _c(base_url):
    return GraphDBClient(base_url, token="tok")


def _resp():
    return httpx.Response(200, json={
        "object": "list", "model": "lsa",
        "data": [{"object": "embedding", "embedding": [0.1, 0.2], "index": 0}],
        "usage": {"prompt_tokens": 2, "total_tokens": 2},
    })


@respx.mock
def test_embeddings_single_string_becomes_array_input(base_url):
    route = respx.post(f"{base_url}/v1/embeddings").mock(return_value=_resp())
    r = _c(base_url).embeddings("hello")
    assert r.vectors == [[0.1, 0.2]] and r.model == "lsa"
    assert b'["hello"]' in route.calls.last.request.read()


@respx.mock
def test_embeddings_list_input(base_url):
    route = respx.post(f"{base_url}/v1/embeddings").mock(return_value=_resp())
    _c(base_url).embeddings(["a", "b"])
    assert b'"a"' in route.calls.last.request.read()
```

Create `clients/python/tests/test_query_graphql.py`:

```python
from __future__ import annotations

import httpx
import respx

from graphdb_client import GraphDBClient


def _c(base_url):
    return GraphDBClient(base_url, token="tok")


@respx.mock
def test_query_maps_columns_rows(base_url):
    route = respx.post(f"{base_url}/query").mock(return_value=httpx.Response(200, json={
        "columns": ["n.name"], "rows": [{"n.name": "Alice"}], "count": 1, "time": "1ms"}))
    r = _c(base_url).query("MATCH (n) RETURN n.name", parameters={"x": 1})
    assert r.columns == ["n.name"] and r.rows == [{"n.name": "Alice"}] and r.count == 1
    assert b'"parameters"' in route.calls.last.request.read()


@respx.mock
def test_graphql_returns_raw_dict_including_errors(base_url):
    respx.post(f"{base_url}/graphql").mock(return_value=httpx.Response(200, json={
        "data": None, "errors": [{"message": "boom"}]}))
    out = _c(base_url).graphql("{ x }")
    assert out["errors"][0]["message"] == "boom"  # not raised — returned in the dict


@respx.mock
def test_graphql_sends_operation_name_and_variables(base_url):
    route = respx.post(f"{base_url}/graphql").mock(
        return_value=httpx.Response(200, json={"data": {}}))
    _c(base_url).graphql("query Q($a:Int){x}", variables={"a": 1}, operation_name="Q")
    body = route.calls.last.request.read()
    assert b'"operationName"' in body and b'"variables"' in body
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd clients/python && uv run pytest tests/test_retrieve.py tests/test_embeddings.py tests/test_query_graphql.py -v`
Expected: FAIL — `AttributeError: 'GraphDBClient' object has no attribute 'retrieve'`

- [ ] **Step 3: Wire resources + add the four methods**

In `clients/python/src/graphdb_client/client.py`:

(a) Extend imports at the top:

```python
from .resources.algorithms import AlgorithmsResource
from .resources.edges import EdgesResource
from .resources.nodes import NodesResource
from .resources.search import SearchResource
from .resources.vector_indexes import VectorIndexesResource
```

(b) In `__init__`, after `self.edges = EdgesResource(self._raw)`:

```python
        self.search = SearchResource(self._raw)
        self.vector_indexes = VectorIndexesResource(self._raw)
        self.algorithms = AlgorithmsResource(self._raw)
```

(c) Add `Mapping` to the `typing` import line (`from typing import Any, Mapping, Sequence`) and add these methods before `close`:

```python
    def retrieve(
        self,
        query: str,
        *,
        k: int | None = None,
        max_tokens: int | None = None,
        max_hops: int | None = None,
        alpha: float | None = None,
        beta: float | None = None,
        tau: float | None = None,
        labels: Sequence[str] | None = None,
        include_node: bool = False,
    ) -> "RetrieveResult":
        """Graph-augmented retrieval (POST /v1/retrieve). Each document carries the
        graph signal in source.path. `degraded` mirrors hybrid-search fallback."""
        body: dict[str, Any] = {"query": query}
        for name, val in (("k", k), ("max_tokens", max_tokens), ("max_hops", max_hops),
                          ("alpha", alpha), ("beta", beta), ("tau", tau)):
            if val is not None:
                body[name] = val
        if labels is not None:
            body["labels"] = list(labels)
        if include_node:
            body["include_node"] = True
        res = self._raw.request("POST", "/v1/retrieve", json=body)
        return RetrieveResult.from_dict(res.data)

    def embeddings(
        self,
        input: str | Sequence[str],
        *,
        model: str | None = None,
        dimensions: int | None = None,
    ) -> "EmbeddingsResult":
        """OpenAI-shaped embeddings (POST /v1/embeddings). A str is sent as a
        one-element input array; vectors come back ordered to match the input."""
        items = [input] if isinstance(input, str) else list(input)
        body: dict[str, Any] = {"input": items}
        if model is not None:
            body["model"] = model
        if dimensions is not None:
            body["dimensions"] = dimensions
        res = self._raw.request("POST", "/v1/embeddings", json=body)
        return EmbeddingsResult.from_dict(res.data)

    def query(
        self,
        cypher: str,
        *,
        parameters: Mapping[str, Any] | None = None,
        timeout_seconds: int | None = None,
    ) -> "QueryResult":
        """Run a Cypher query (POST /query)."""
        body: dict[str, Any] = {"query": cypher}
        if parameters is not None:
            body["parameters"] = dict(parameters)
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        res = self._raw.request("POST", "/query", json=body)
        return QueryResult.from_dict(res.data)

    def graphql(
        self,
        query: str,
        *,
        variables: Mapping[str, Any] | None = None,
        operation_name: str | None = None,
    ) -> dict[str, Any]:
        """Execute a GraphQL document (POST /graphql). Returns the raw response
        dict ({"data": ..., "errors": ...}); GraphQL-level errors are returned in
        the dict, not raised (only HTTP >= 400 raises)."""
        body: dict[str, Any] = {"query": query}
        if variables is not None:
            body["variables"] = dict(variables)
        if operation_name is not None:
            body["operationName"] = operation_name
        res = self._raw.request("POST", "/graphql", json=body)
        return res.data if isinstance(res.data, dict) else {}
```

(d) Extend the model import so the return-type annotations resolve:

```python
from .models import (
    EmbeddingsResult,
    Node,
    QueryResult,
    RetrieveResult,
    SearchResult,
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd clients/python && uv run pytest tests/test_retrieve.py tests/test_embeddings.py tests/test_query_graphql.py -v`
Expected: PASS (7 tests)

- [ ] **Step 5: Commit**

```bash
git add clients/python/src/graphdb_client/client.py clients/python/tests/test_retrieve.py clients/python/tests/test_embeddings.py clients/python/tests/test_query_graphql.py
git commit -m "feat(sdk): retrieve/embeddings/query/graphql + wire M2a resources"
```

---

### Task 6: Exports, README, integration smoke, full gate

**Files:**
- Modify: `clients/python/src/graphdb_client/__init__.py`
- Modify: `clients/python/README.md`
- Modify: `clients/python/tests/integration/test_smoke.py`

- [ ] **Step 1: Export the new models**

In `clients/python/src/graphdb_client/__init__.py`, add the M2a models to the imports and `__all__` (keep alphabetical with the existing entries):

```python
from .models import (
    AlgorithmResult,
    Edge,
    EmbeddingsResult,
    HybridSearchResult,
    Node,
    QueryResult,
    RetrieveDocument,
    RetrieveResult,
    RetrieveSource,
    SearchHit,
    SearchResult,
    ShortestPath,
    VectorIndex,
)
```

Add each new name to `__all__`.

- [ ] **Step 2: Verify the package imports cleanly**

Run: `cd clients/python && uv run python -c "import graphdb_client as g; print(g.HybridSearchResult, g.VectorIndex, g.RetrieveResult)"`
Expected: prints the three classes, no ImportError.

- [ ] **Step 3: README usage**

Append a "Search & query (M2a)" section to `clients/python/README.md` showing each new surface:

````markdown
## Search & query

```python
with GraphDBClient("http://localhost:8080", token=TOKEN) as db:
    # vector index management
    db.vector_indexes.create("embedding", dimensions=384)
    print(db.vector_indexes.list())

    # full-text + hybrid search
    hits = db.search.fulltext("graph database", labels=["Doc"])
    hybrid = db.search.hybrid("graph database", alpha=0.5)
    if hybrid.degraded:
        print("hybrid degraded:", hybrid.degraded)

    # embeddings + graph-augmented retrieval
    vecs = db.embeddings(["hello", "world"]).vectors
    docs = db.retrieve("how does X relate to Y?", k=5).documents

    # cypher + graphql + algorithms
    rows = db.query("MATCH (n:Person) RETURN n.name").rows
    gql = db.graphql("{ __typename }")
    pr = db.algorithms.run("pagerank").results
    path = db.algorithms.shortest_path(1, 42)
```
````

- [ ] **Step 4: Integration smoke (opt-in)**

`tests/integration/test_smoke.py` already has a module-level `pytestmark = pytest.mark.skipif(os.environ.get("GRAPHDB_SDK_IT") != "1", ...)` guard and a `client` fixture (reads `GRAPHDB_SDK_URL`/`GRAPHDB_SDK_TOKEN`). Add a new test in that file using the **existing `client` fixture** — the module `pytestmark` skips it automatically when `GRAPHDB_SDK_IT` is unset:

```python
def test_m2a_search_query_smoke(client):
    client.vector_indexes.create("embedding", dimensions=3)
    assert any(i.property_name == "embedding" for i in client.vector_indexes.list())
    client.embeddings("hello")                 # LSA embeddings round-trip
    client.search.fulltext("hello")            # FTS path (may be empty; must not raise)
    client.search.hybrid("hello")              # hybrid path (may be degraded)
    client.query("MATCH (n) RETURN n LIMIT 1")
    client.graphql("{ __typename }")
```

- [ ] **Step 5: Run the full unit suite + gates**

Run: `cd clients/python && uv run pytest -v 2>&1 | tail -20`
Expected: all unit tests pass (M1 + the ~30 new M2a tests); the integration smoke auto-skips (`GRAPHDB_SDK_IT` unset).

Run: `cd clients/python && uv run ruff check . && uv run mypy src`
Expected: ruff clean; mypy clean.

- [ ] **Step 6: Commit**

```bash
git add clients/python/src/graphdb_client/__init__.py clients/python/README.md clients/python/tests/integration/test_smoke.py
git commit -m "feat(sdk): export M2a models, README usage, integration smoke"
```

---

## Notes for the implementer

- **Confirm the vector-index create response shape** against the live server in the integration smoke. The spec assumes `POST /vector-indexes` returns the created index (`{property_name, dimensions, metric}`). If the server instead returns a `{message: ...}` envelope, adjust `VectorIndexesResource.create` to re-`get(property_name)` (or map the actual shape) — `VectorIndex.from_dict` requires `property_name`.
- **Do not change `Transport`, `errors.py`, or the M1 resources.** M2a is purely additive.
- **`mypy` strictness:** the repo's M1 config is the gate; keep the new code typed (explicit return types on public methods, no bare `Any` leaks beyond the documented freeform `dict` returns for `graphql`/`AlgorithmResult.results`/`QueryResult.rows`).
- After all tasks: dispatch a final review per subagent-driven-development, then `superpowers:finishing-a-development-branch` to open the PR (spec + plan + implementation together, mirroring M1's #326/#327).
```
