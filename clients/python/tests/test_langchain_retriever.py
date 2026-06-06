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
