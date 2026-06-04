from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.edges import EdgesResource


def _res(base_url):
    return EdgesResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_edge(base_url):
    route = respx.post(f"{base_url}/edges").mock(return_value=httpx.Response(201, json={
        "id": 3, "from_node_id": 1, "to_node_id": 2,
        "type": "KNOWS", "properties": {}, "weight": 1.0,
    }))
    e = _res(base_url).create(1, 2, "KNOWS", weight=1.0)
    assert (e.id, e.from_node_id, e.to_node_id, e.type) == (3, 1, 2, "KNOWS")
    body = route.calls.last.request
    assert b'"from_node_id": 1' in body.content or b'"from_node_id":1' in body.content


@respx.mock
def test_batch_create_edges(base_url):
    edge_row = {
        "id": 1, "from_node_id": 1, "to_node_id": 2,
        "type": "R", "properties": {}, "weight": 0.0,
    }
    respx.post(f"{base_url}/edges/batch").mock(return_value=httpx.Response(201, json={
        "edges": [edge_row],
        "created": 1, "time": "1ms",
    }))
    edges = _res(base_url).batch_create([{"from_node_id": 1, "to_node_id": 2, "type": "R"}])
    assert len(edges) == 1 and edges[0].type == "R"


@respx.mock
def test_get(base_url):
    respx.get(f"{base_url}/edges/3").mock(return_value=httpx.Response(200, json={
        "id": 3, "from_node_id": 1, "to_node_id": 2,
        "type": "KNOWS", "properties": {}, "weight": 1.0,
    }))
    assert _res(base_url).get(3).id == 3


@respx.mock
def test_update_edge_sends_properties_and_weight(base_url):
    route = respx.put(f"{base_url}/edges/3").mock(return_value=httpx.Response(200, json={
        "id": 3, "from_node_id": 1, "to_node_id": 2, "type": "KNOWS",
        "properties": {"k": "v2"}, "weight": 2.0,
    }))
    e = _res(base_url).update(3, {"k": "v2"}, weight=2.0)
    assert e.weight == 2.0 and e.properties["k"] == "v2"
    body = route.calls.last.request.content
    assert b'"weight"' in body and b'"k"' in body


@respx.mock
def test_update_edge_omits_weight_when_not_given(base_url):
    # Pointer-weight contract: a properties-only update must NOT send a weight
    # field, so the server leaves the edge's weight unchanged.
    route = respx.put(f"{base_url}/edges/3").mock(return_value=httpx.Response(200, json={
        "id": 3, "from_node_id": 1, "to_node_id": 2, "type": "KNOWS",
        "properties": {"k": "v2"}, "weight": 5.0,
    }))
    _res(base_url).update(3, {"k": "v2"})
    assert b'"weight"' not in route.calls.last.request.content


@respx.mock
def test_delete_edge(base_url):
    route = respx.delete(f"{base_url}/edges/3").mock(return_value=httpx.Response(200))
    _res(base_url).delete(3)
    assert route.called
