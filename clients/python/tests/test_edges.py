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
