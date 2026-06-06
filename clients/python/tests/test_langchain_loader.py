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
