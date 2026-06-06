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
    assert "text" not in docs[0].metadata          # content key excluded from metadata
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


class FakeAsyncClient:
    def __init__(self):
        self.calls = []

    async def embeddings(self, text, **kwargs):
        return EmbeddingsResult(model="lsa", vectors=[[0.4, 0.5, 0.6]])

    async def vector_search(self, property_name, query_vector, *, k=10, **kwargs):
        self.calls.append((property_name, query_vector, k, kwargs))
        node = Node(id=8, labels=["Doc"], properties={"text": "async-hi", "extra": 2})
        return [SearchResult(node_id=8, distance=0.4, score=0.8, node=node)]


async def test_async_similarity_search_embeds_then_searches():
    fac = FakeAsyncClient()
    vs = GraphDBVectorStore(
        FakeClient(), property_name="embedding", content_key="text", aclient=fac
    )
    docs = await vs.asimilarity_search("query", k=4)
    assert docs[0].page_content == "async-hi"
    assert docs[0].metadata["node_id"] == 8 and docs[0].metadata["extra"] == 2
    pn, vec, k, kw = fac.calls[0]
    assert pn == "embedding" and vec == [0.4, 0.5, 0.6] and k == 4
    assert kw.get("include_nodes") is True


async def test_async_search_requires_aclient():
    vs = GraphDBVectorStore(FakeClient(), property_name="embedding")
    with pytest.raises(ValueError):
        await vs.asimilarity_search("query")
