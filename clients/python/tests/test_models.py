from __future__ import annotations

from graphdb_client.models import Edge, Node, SearchResult


def test_node_from_dict():
    n = Node.from_dict({"id": 7, "labels": ["Person"], "properties": {"_key": "p:1", "age": 30}})
    assert n.id == 7
    assert n.labels == ["Person"]
    assert n.properties["_key"] == "p:1"


def test_node_from_dict_tolerates_missing_optional_fields():
    n = Node.from_dict({"id": 1})
    assert n.labels == []
    assert n.properties == {}


def test_edge_from_dict():
    e = Edge.from_dict({
        "id": 3, "from_node_id": 1, "to_node_id": 2,
        "type": "KNOWS", "properties": {"since": 2020}, "weight": 1.5,
    })
    assert (e.from_node_id, e.to_node_id, e.type, e.weight) == (1, 2, "KNOWS", 1.5)


def test_search_result_from_dict_with_embedded_node():
    r = SearchResult.from_dict({
        "node_id": 9, "distance": 0.1, "score": 0.9,
        "node": {"id": 9, "labels": ["Doc"], "properties": {}},
    })
    assert r.node_id == 9 and r.score == 0.9
    assert r.node is not None and r.node.id == 9


def test_search_result_from_dict_without_node():
    r = SearchResult.from_dict({"node_id": 9, "distance": 0.1, "score": 0.9})
    assert r.node is None
