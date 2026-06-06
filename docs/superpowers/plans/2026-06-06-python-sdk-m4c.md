# Python SDK M4c Implementation Plan (LangChain integration)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship first-class LangChain adapters — `GraphDBRetriever`, `GraphDBVectorStore`, `GraphDBLoader` — behind an optional `graphdb-client[langchain]` extra, leaving the core httpx-only install untouched.

**Architecture:** A new `graphdb_client/langchain/` subpackage that the core never imports. Its `__init__.py` guards on `langchain_core` and raises an actionable install message if absent. Adapters wrap the existing `GraphDBClient`/`AsyncGraphDBClient` (mapping is the only logic — no new HTTP). The core package's top-level `__init__` does NOT import this subpackage.

**Tech Stack:** Python 3.9+, `langchain-core>=0.3` (optional extra; `Document`, `BaseRetriever`, `VectorStore`, `BaseLoader`); tests mock at the `GraphDBClient` boundary with `langchain-core` in the dev group; `ruff` + `mypy --strict`. All commands run from `clients/python/` via `uv run` (cd to `/Users/darraghdowney/Workspace/github.com/graphdb/clients/python`).

**Spec:** `docs/superpowers/specs/2026-06-06-python-sdk-m4-design.md` §5.

## Models the adapters map to (verified against `src/graphdb_client/models.py`)

- `client.retrieve(query, *, k=..., ...) -> RetrieveResult`; `RetrieveResult.documents: list[RetrieveDocument]`; `RetrieveDocument` has `.page_content: str`, `.node_id: int`, `.score: float`, `.source: RetrieveSource` (`.node_id`, `.path: list[int]`, `.label: str | None`), `.node: Node | None`.
- `client.vector_search(property_name, query_vector, *, k=10, ef=None, filter_labels=None, include_nodes=False) -> list[SearchResult]`; `SearchResult` has `.node_id`, `.distance`, `.score`, `.node: Node | None` (`Node` = `.id`, `.labels`, `.properties`).
- `client.embeddings(input, *, model=None, dimensions=None) -> EmbeddingsResult`; `.vectors: list[list[float]]`.
- `client.nodes.list(*, label=None, page_size=100) -> Iterator[Node]`; async `AsyncGraphDBClient.nodes.list(...) -> AsyncIterator[Node]`.

## File structure

- **Modify** `pyproject.toml` — `[project.optional-dependencies] langchain`, dev `langchain-core`, `[[tool.mypy.overrides]]` for `langchain_core.*`.
- **Create** `src/graphdb_client/langchain/__init__.py` — guarded import + exports (grown across T1–T3).
- **Create** `src/graphdb_client/langchain/retriever.py` — `GraphDBRetriever` (pydantic `BaseRetriever`).
- **Create** `src/graphdb_client/langchain/vectorstore.py` — `GraphDBVectorStore` (ABC `VectorStore`, read-optimized).
- **Create** `src/graphdb_client/langchain/loader.py` — `GraphDBLoader` (ABC `BaseLoader`).
- **Tests** `tests/test_langchain_retriever.py`, `tests/test_langchain_vectorstore.py`, `tests/test_langchain_loader.py`.
- **Modify** `README.md` — LangChain section.

## Interface-contact notes (read before coding)

LangChain's exact import paths can shift across `langchain-core` 0.3.x. **Verify against the installed version** and adjust if needed:
- `from langchain_core.documents import Document`
- `from langchain_core.retrievers import BaseRetriever`
- `from langchain_core.callbacks import CallbackManagerForRetrieverRun, AsyncCallbackManagerForRetrieverRun`
- `from langchain_core.vectorstores import VectorStore`
- `from langchain_core.document_loaders import BaseLoader`
- `from langchain_core.embeddings import Embeddings`
- `from pydantic import Field` (langchain-core 0.3 uses pydantic v2 directly)

The core package (`graphdb_client/__init__.py`) MUST NOT import `graphdb_client.langchain`. Adapters are imported as `from graphdb_client.langchain import GraphDBRetriever`.

---

### Task 1: packaging + guarded import + `GraphDBRetriever`

**Files:**
- Modify: `clients/python/pyproject.toml`
- Create: `clients/python/src/graphdb_client/langchain/__init__.py`, `langchain/retriever.py`
- Test: `clients/python/tests/test_langchain_retriever.py`

- [ ] **Step 1: tooling.** In `pyproject.toml`:
  - Add an optional-dependencies table (after the `[project]` block):
    ```toml
    [project.optional-dependencies]
    langchain = ["langchain-core>=0.3"]
    ```
  - In `[dependency-groups] dev = [...]`, append `"langchain-core>=0.3"`.
  - Add a mypy override (after the `[tool.mypy]` block):
    ```toml
    [[tool.mypy.overrides]]
    module = ["langchain_core.*"]
    ignore_missing_imports = true
    ```
  Then `cd /Users/darraghdowney/Workspace/github.com/graphdb/clients/python && uv sync`.

- [ ] **Step 2: write the failing test** — create `tests/test_langchain_retriever.py`:

```python
from __future__ import annotations

import importlib
import sys

import pytest

from graphdb_client.langchain import GraphDBRetriever
from graphdb_client.models import RetrieveDocument, RetrieveResult, RetrieveSource


def _result():
    return RetrieveResult(documents=[
        RetrieveDocument(
            page_content="hello world", node_id=1, score=0.9,
            source=RetrieveSource(node_id=1, path=[1, 2], label="Doc"),
        )
    ])


class FakeClient:
    def __init__(self, result):
        self._result = result
        self.last = None

    def retrieve(self, query, **kwargs):
        self.last = (query, kwargs)
        return self._result


class FakeAsyncClient:
    def __init__(self, result):
        self._result = result
        self.last = None

    async def retrieve(self, query, **kwargs):
        self.last = (query, kwargs)
        return self._result


def test_retriever_maps_documents_and_metadata():
    fc = FakeClient(_result())
    r = GraphDBRetriever(client=fc, k=3)
    docs = r.invoke("q")
    assert len(docs) == 1
    d = docs[0]
    assert d.page_content == "hello world"
    assert d.metadata["node_id"] == 1
    assert d.metadata["score"] == 0.9
    assert d.metadata["path"] == [1, 2]
    assert d.metadata["label"] == "Doc"
    # k threaded through to client.retrieve
    assert fc.last[1].get("k") == 3


async def test_retriever_async_uses_aclient():
    fac = FakeAsyncClient(_result())
    r = GraphDBRetriever(client=None, aclient=fac, k=2)
    docs = await r.ainvoke("q")
    assert docs[0].page_content == "hello world"
    assert fac.last[1].get("k") == 2


def test_langchain_import_error_message(monkeypatch):
    monkeypatch.setitem(sys.modules, "langchain_core", None)
    for mod in list(sys.modules):
        if mod == "graphdb_client.langchain" or mod.startswith("graphdb_client.langchain."):
            monkeypatch.delitem(sys.modules, mod, raising=False)
    with pytest.raises(ImportError, match=r"graphdb-client\[langchain\]"):
        importlib.import_module("graphdb_client.langchain")
```

- [ ] **Step 3: run, confirm FAIL** — `uv run pytest tests/test_langchain_retriever.py -q` → ImportError (`graphdb_client.langchain` missing).

- [ ] **Step 4: create `src/graphdb_client/langchain/__init__.py`:**

```python
"""LangChain adapters for graphdb. Requires the optional ``langchain`` extra."""
from __future__ import annotations

try:
    import langchain_core  # noqa: F401
except ImportError as exc:  # pragma: no cover - exercised via sys.modules patch in tests
    raise ImportError(
        "graphdb_client.langchain requires langchain-core: "
        "pip install 'graphdb-client[langchain]'"
    ) from exc

from .retriever import GraphDBRetriever

__all__ = ["GraphDBRetriever"]
```

- [ ] **Step 5: create `src/graphdb_client/langchain/retriever.py`:**

```python
from __future__ import annotations

from typing import Any

from langchain_core.callbacks import (
    AsyncCallbackManagerForRetrieverRun,
    CallbackManagerForRetrieverRun,
)
from langchain_core.documents import Document
from langchain_core.retrievers import BaseRetriever
from pydantic import Field


def _to_document(doc: Any) -> Document:
    """Map a graphdb RetrieveDocument to a LangChain Document."""
    metadata: dict[str, Any] = {
        "node_id": doc.node_id,
        "score": doc.score,
        "path": doc.source.path,
        "label": doc.source.label,
    }
    if doc.node is not None:
        metadata["labels"] = doc.node.labels
        metadata["properties"] = doc.node.properties
    return Document(page_content=doc.page_content, metadata=metadata)


class GraphDBRetriever(BaseRetriever):
    """LangChain retriever over graphdb GraphRAG (``/v1/retrieve``).

    Provide a sync ``client`` (GraphDBClient) and/or an ``aclient``
    (AsyncGraphDBClient). ``k`` and any ``retrieve_kwargs`` pass through to
    ``client.retrieve(...)``.
    """

    client: Any = None
    aclient: Any = None
    k: int = 4
    retrieve_kwargs: dict[str, Any] = Field(default_factory=dict)

    def _get_relevant_documents(
        self, query: str, *, run_manager: CallbackManagerForRetrieverRun
    ) -> list[Document]:
        if self.client is None:
            raise ValueError("GraphDBRetriever requires a sync `client` for sync retrieval")
        result = self.client.retrieve(query, k=self.k, **self.retrieve_kwargs)
        return [_to_document(d) for d in result.documents]

    async def _aget_relevant_documents(
        self, query: str, *, run_manager: AsyncCallbackManagerForRetrieverRun
    ) -> list[Document]:
        if self.aclient is None:
            return await super()._aget_relevant_documents(query, run_manager=run_manager)
        result = await self.aclient.retrieve(query, k=self.k, **self.retrieve_kwargs)
        return [_to_document(d) for d in result.documents]
```

NOTE: if the installed `langchain_core` exposes the callback managers elsewhere, adjust the import (see "Interface-contact notes"). If mypy-strict objects to subclassing the pydantic `BaseRetriever`, add a targeted `# type: ignore[misc]  # langchain BaseRetriever is a dynamic pydantic base` on the `class` line only — keep method bodies typed.

- [ ] **Step 6: run, confirm PASS** — `uv run pytest tests/test_langchain_retriever.py -q` → 3 passed.

- [ ] **Step 7: gate + commit:**
```
uv run ruff check . && uv run mypy src && uv run pytest -q
cd /Users/darraghdowney/Workspace/github.com/graphdb && git add clients/python/pyproject.toml clients/python/uv.lock clients/python/src/graphdb_client/langchain/__init__.py clients/python/src/graphdb_client/langchain/retriever.py clients/python/tests/test_langchain_retriever.py && git commit -m "feat(sdk): LangChain GraphDBRetriever + [langchain] extra + guarded import

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```
(If `uv.lock` is unchanged, omit it.)

---

### Task 2: `GraphDBVectorStore`

**Files:**
- Create: `clients/python/src/graphdb_client/langchain/vectorstore.py`
- Modify: `clients/python/src/graphdb_client/langchain/__init__.py`
- Test: `clients/python/tests/test_langchain_vectorstore.py`

`GraphDBVectorStore` is read-optimized for RAG: `similarity_search` (+ by-vector + async twins) embed the query and call `client.vector_search`, mapping `SearchResult` → `Document`. Writes (`add_texts`/`from_texts`) raise `NotImplementedError` (ingest vectors via `client.nodes.create` + a vector index; the store is a retrieval adapter).

- [ ] **Step 1: write the failing test** — create `tests/test_langchain_vectorstore.py`:

```python
from __future__ import annotations

import pytest

from graphdb_client.langchain import GraphDBVectorStore
from graphdb_client.models import EmbeddingsResult, Node, SearchResult


class FakeClient:
    def __init__(self):
        self.calls = []

    def embeddings(self, text, **kwargs):
        return EmbeddingsResult(model="lsa", vectors=[[0.1, 0.2, 0.3]])

    def vector_search(self, property_name, query_vector, *, k=10, **kwargs):
        self.calls.append((property_name, query_vector, k, kwargs))
        node = Node(id=7, labels=["Doc"], properties={"text": "hi", "extra": 1})
        return [SearchResult(node_id=7, distance=0.5, score=0.9, node=node)]


def test_similarity_search_embeds_then_searches():
    fc = FakeClient()
    vs = GraphDBVectorStore(fc, property_name="embedding", content_key="text")
    docs = vs.similarity_search("query", k=5)
    assert docs[0].page_content == "hi"
    assert docs[0].metadata["node_id"] == 7
    assert docs[0].metadata["score"] == 0.9
    assert docs[0].metadata["extra"] == 1          # non-content props in metadata
    pn, vec, k, kw = fc.calls[0]
    assert pn == "embedding" and vec == [0.1, 0.2, 0.3] and k == 5
    assert kw.get("include_nodes") is True          # nodes needed for page_content


def test_similarity_search_by_vector():
    fc = FakeClient()
    vs = GraphDBVectorStore(fc, property_name="embedding", content_key="text")
    docs = vs.similarity_search_by_vector([0.9, 0.8, 0.7], k=2)
    assert docs[0].page_content == "hi"
    assert fc.calls[0][1] == [0.9, 0.8, 0.7]


def test_writes_raise_not_implemented():
    vs = GraphDBVectorStore(FakeClient(), property_name="embedding")
    with pytest.raises(NotImplementedError):
        vs.add_texts(["a"])
    with pytest.raises(NotImplementedError):
        GraphDBVectorStore.from_texts(["a"], embedding=None)
```

- [ ] **Step 2: run, confirm FAIL** — `uv run pytest tests/test_langchain_vectorstore.py -q` → ImportError (`GraphDBVectorStore`).

- [ ] **Step 3: create `src/graphdb_client/langchain/vectorstore.py`:**

```python
from __future__ import annotations

from typing import Any, Iterable

from langchain_core.documents import Document
from langchain_core.embeddings import Embeddings
from langchain_core.vectorstores import VectorStore


def _to_document(result: Any, content_key: str) -> Document:
    node = result.node
    props = dict(node.properties) if node is not None else {}
    content = str(props.pop(content_key, "")) if node is not None else ""
    metadata: dict[str, Any] = {
        "node_id": result.node_id,
        "score": result.score,
        "distance": result.distance,
    }
    if node is not None:
        metadata["labels"] = node.labels
        metadata.update(props)  # remaining (non-content) properties
    return Document(page_content=content, metadata=metadata)


class GraphDBVectorStore(VectorStore):
    """Read-optimized LangChain VectorStore over graphdb ``/vector-search``.

    Retrieval-only: ``similarity_search`` embeds the query (via a supplied
    LangChain ``Embeddings``, else graphdb's ``client.embeddings``) and runs a
    vector search. Ingest vectors with ``client.nodes.create`` + a vector index;
    ``add_texts``/``from_texts`` raise ``NotImplementedError``.
    """

    def __init__(
        self,
        client: Any,
        property_name: str,
        *,
        embedding: Embeddings | None = None,
        content_key: str = "text",
        aclient: Any = None,
    ) -> None:
        self._client = client
        self._aclient = aclient
        self._property_name = property_name
        self._embedding = embedding
        self._content_key = content_key

    @property
    def embeddings(self) -> Embeddings | None:
        return self._embedding

    def _embed_query(self, query: str) -> list[float]:
        if self._embedding is not None:
            return list(self._embedding.embed_query(query))
        return list(self._client.embeddings(query).vectors[0])

    def similarity_search(self, query: str, k: int = 4, **kwargs: Any) -> list[Document]:
        return self.similarity_search_by_vector(self._embed_query(query), k=k, **kwargs)

    def similarity_search_by_vector(
        self, embedding: list[float], k: int = 4, **kwargs: Any
    ) -> list[Document]:
        results = self._client.vector_search(
            self._property_name, embedding, k=k, include_nodes=True, **kwargs
        )
        return [_to_document(r, self._content_key) for r in results]

    async def asimilarity_search(self, query: str, k: int = 4, **kwargs: Any) -> list[Document]:
        if self._aclient is None:
            raise ValueError("GraphDBVectorStore requires an `aclient` for async search")
        if self._embedding is not None:
            vector = list(await self._embedding.aembed_query(query))
        else:
            res = await self._aclient.embeddings(query)
            vector = list(res.vectors[0])
        return await self.asimilarity_search_by_vector(vector, k=k, **kwargs)

    async def asimilarity_search_by_vector(
        self, embedding: list[float], k: int = 4, **kwargs: Any
    ) -> list[Document]:
        if self._aclient is None:
            raise ValueError("GraphDBVectorStore requires an `aclient` for async search")
        results = await self._aclient.vector_search(
            self._property_name, embedding, k=k, include_nodes=True, **kwargs
        )
        return [_to_document(r, self._content_key) for r in results]

    def add_texts(
        self,
        texts: Iterable[str],
        metadatas: list[dict[str, Any]] | None = None,
        **kwargs: Any,
    ) -> list[str]:
        raise NotImplementedError(
            "GraphDBVectorStore is retrieval-only; ingest vectors via "
            "client.nodes.create(...) + a vector index."
        )

    @classmethod
    def from_texts(
        cls,
        texts: list[str],
        embedding: Embeddings | None = None,
        metadatas: list[dict[str, Any]] | None = None,
        **kwargs: Any,
    ) -> "GraphDBVectorStore":
        raise NotImplementedError(
            "GraphDBVectorStore is retrieval-only; construct it directly with an "
            "existing graphdb client + vector index property_name."
        )
```

NOTE: `VectorStore` is an ABC (not pydantic) — normal `__init__`. Its required abstract methods are `add_texts`, `similarity_search`, and the classmethod `from_texts`; all three are defined above. If the installed version marks additional abstract methods, implement the read ones and raise `NotImplementedError` for write ones, matching this pattern. Verify `embed_query`/`aembed_query` exist on `Embeddings` for the installed version.

- [ ] **Step 4: add the export to `src/graphdb_client/langchain/__init__.py`** — add `from .vectorstore import GraphDBVectorStore` (after the retriever import) and add `"GraphDBVectorStore"` to `__all__`.

- [ ] **Step 5: run, confirm PASS** — `uv run pytest tests/test_langchain_vectorstore.py -q` → 3 passed.

- [ ] **Step 6: gate + commit:**
```
uv run ruff check . && uv run mypy src && uv run pytest -q
cd /Users/darraghdowney/Workspace/github.com/graphdb && git add clients/python/src/graphdb_client/langchain/vectorstore.py clients/python/src/graphdb_client/langchain/__init__.py clients/python/tests/test_langchain_vectorstore.py && git commit -m "feat(sdk): LangChain GraphDBVectorStore (retrieval-only over /vector-search)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `GraphDBLoader`

**Files:**
- Create: `clients/python/src/graphdb_client/langchain/loader.py`
- Modify: `clients/python/src/graphdb_client/langchain/__init__.py`
- Test: `clients/python/tests/test_langchain_loader.py`

`GraphDBLoader` turns graphdb nodes into LangChain `Document`s by iterating `client.nodes.list(label=...)`.

- [ ] **Step 1: write the failing test** — create `tests/test_langchain_loader.py`:

```python
from __future__ import annotations

from graphdb_client.langchain import GraphDBLoader
from graphdb_client.models import Node


class _NodesList:
    def __init__(self, nodes):
        self._nodes = nodes
        self.last_label = "UNSET"

    def list(self, *, label=None, page_size=100):
        self.last_label = label
        for n in self._nodes:
            yield n


class FakeClient:
    def __init__(self, nodes):
        self.nodes = _NodesList(nodes)


def _nodes():
    return [
        Node(id=1, labels=["Doc"], properties={"text": "alpha", "k": 1}),
        Node(id=2, labels=["Doc"], properties={"text": "beta"}),
    ]


def test_loader_maps_nodes_to_documents():
    fc = FakeClient(_nodes())
    loader = GraphDBLoader(fc, label="Doc", content_key="text")
    docs = loader.load()
    assert [d.page_content for d in docs] == ["alpha", "beta"]
    assert docs[0].metadata["id"] == 1
    assert docs[0].metadata["labels"] == ["Doc"]
    assert docs[0].metadata["k"] == 1          # non-content prop in metadata
    assert "text" not in docs[0].metadata      # content_key excluded from metadata
    assert fc.nodes.last_label == "Doc"


def test_loader_lazy_load_is_iterator():
    fc = FakeClient(_nodes())
    loader = GraphDBLoader(fc)
    it = loader.lazy_load()
    first = next(it)
    assert first.page_content == "alpha"
```

- [ ] **Step 2: run, confirm FAIL** — `uv run pytest tests/test_langchain_loader.py -q` → ImportError.

- [ ] **Step 3: create `src/graphdb_client/langchain/loader.py`:**

```python
from __future__ import annotations

from typing import Any, AsyncIterator, Iterator

from langchain_core.document_loaders import BaseLoader
from langchain_core.documents import Document


def _node_to_document(node: Any, content_key: str) -> Document:
    props = dict(node.properties)
    content = str(props.pop(content_key, ""))
    metadata: dict[str, Any] = {"id": node.id, "labels": node.labels}
    metadata.update(props)
    return Document(page_content=content, metadata=metadata)


class GraphDBLoader(BaseLoader):
    """LangChain document loader: streams graphdb nodes as Documents.

    ``page_content`` comes from ``content_key`` (default ``"text"``); the node id,
    labels, and remaining properties become metadata. Pass an ``aclient``
    (AsyncGraphDBClient) to use ``alazy_load``.
    """

    def __init__(
        self,
        client: Any,
        *,
        label: str | None = None,
        content_key: str = "text",
        aclient: Any = None,
    ) -> None:
        self._client = client
        self._aclient = aclient
        self._label = label
        self._content_key = content_key

    def lazy_load(self) -> Iterator[Document]:
        for node in self._client.nodes.list(label=self._label):
            yield _node_to_document(node, self._content_key)

    async def alazy_load(self) -> AsyncIterator[Document]:
        if self._aclient is None:
            raise ValueError("GraphDBLoader requires an `aclient` for alazy_load")
        async for node in self._aclient.nodes.list(label=self._label):
            yield _node_to_document(node, self._content_key)
```

NOTE: `BaseLoader.load()` is a concrete default that materializes `lazy_load()` — do not override it. `BaseLoader` is an ABC; only `lazy_load` is required.

- [ ] **Step 4: add the export to `src/graphdb_client/langchain/__init__.py`** — add `from .loader import GraphDBLoader` and add `"GraphDBLoader"` to `__all__`.

- [ ] **Step 5: run, confirm PASS** — `uv run pytest tests/test_langchain_loader.py -q` → 2 passed.

- [ ] **Step 6: gate + commit:**
```
uv run ruff check . && uv run mypy src && uv run pytest -q
cd /Users/darraghdowney/Workspace/github.com/graphdb && git add clients/python/src/graphdb_client/langchain/loader.py clients/python/src/graphdb_client/langchain/__init__.py clients/python/tests/test_langchain_loader.py && git commit -m "feat(sdk): LangChain GraphDBLoader (nodes -> Documents)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: README + final gate

**Files:**
- Modify: `clients/python/README.md`

- [ ] **Step 1: README.** Append to `clients/python/README.md` (tilde-fenced so the inner code block renders):

~~~markdown
## LangChain integration (optional)

Install the extra: `pip install 'graphdb-client[langchain]'`. Adapters live under
`graphdb_client.langchain` (the core install stays httpx-only):

```python
from graphdb_client import GraphDBClient
from graphdb_client.langchain import GraphDBRetriever, GraphDBVectorStore, GraphDBLoader

db = GraphDBClient(url, token=TOKEN)

# GraphRAG retriever (/v1/retrieve) — drop into any LangChain chain
retriever = GraphDBRetriever(client=db, k=5)
docs = retriever.invoke("how does auth work?")

# Vector store (retrieval over /vector-search; embeds via graphdb if no Embeddings given)
store = GraphDBVectorStore(db, property_name="embedding", content_key="text")
hits = store.similarity_search("graph database", k=3)

# Document loader (nodes -> Documents)
loader = GraphDBLoader(db, label="Doc", content_key="text")
documents = loader.load()
```

`GraphDBVectorStore` is retrieval-only (`add_texts`/`from_texts` raise); ingest
vectors with `db.nodes.create(...)` + a vector index. Pass an `aclient=AsyncGraphDBClient(...)`
to `GraphDBRetriever`/`GraphDBVectorStore`/`GraphDBLoader` for the async paths
(`ainvoke`/`asimilarity_search`/`alazy_load`).
~~~

- [ ] **Step 2: full gate:**
```
uv run pytest -q          # expect 156 prior + ~8 new = ~164 passed, 2 skipped (report real)
uv run ruff check .
uv run mypy src
```

- [ ] **Step 3: commit:**
```
cd /Users/darraghdowney/Workspace/github.com/graphdb && git add clients/python/README.md && git commit -m "docs(sdk): README LangChain integration section

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementer

- **The core never imports `graphdb_client.langchain`.** Do NOT add any langchain import to `src/graphdb_client/__init__.py`. The adapters are reached via `from graphdb_client.langchain import ...`, which is guarded.
- **Mapping is the only logic.** Adapters wrap the existing client; no new HTTP, no new models. Tests mock at the `GraphDBClient` boundary (fake objects), not `respx` — the client methods are already tested.
- **`RetrieveDocument` already has `.page_content`** — the retriever maps it directly; don't re-derive content.
- **`GraphDBVectorStore` passes `include_nodes=True`** to `vector_search` so `SearchResult.node` is populated for `page_content`.
- **Verify langchain-core import paths** against the installed version (see "Interface-contact notes"); 0.3.x is stable but double-check the callback-manager and embeddings paths.
- **mypy-strict + langchain:** the `[[tool.mypy.overrides]]` handles missing stubs; if subclassing the pydantic `BaseRetriever` still trips mypy, a single `# type: ignore[misc]  # langchain dynamic pydantic base` on the class line is acceptable — keep all method bodies fully typed.
- After all 4 tasks: final whole-implementation review, then `superpowers:finishing-a-development-branch` (PR `feat/python-sdk-m4c`). This closes M4 and the SDK roadmap (M1→M4).
```
