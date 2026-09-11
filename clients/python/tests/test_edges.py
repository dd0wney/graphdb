from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.edges import EdgesResource


def _res(base_url):
    return EdgesResource(Transport(base_url, token="tok"))


def _edge_row(id_: int, from_id: int, to_id: int, edge_type: str = "R") -> dict:
    return {
        "id": id_, "from_node_id": from_id, "to_node_id": to_id,
        "type": edge_type, "properties": {}, "weight": 0.0,
    }


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


@respx.mock
def test_list_auto_paginates_across_cursor(base_url):
    page1 = httpx.Response(
        200,
        json=[_edge_row(1, 1, 2, "KNOWS"), _edge_row(2, 2, 3, "KNOWS")],
        headers={"X-Next-Cursor": "2"},
    )
    page2 = httpx.Response(200, json=[_edge_row(3, 3, 4, "KNOWS")])
    route = respx.get(f"{base_url}/edges").mock(side_effect=[page1, page2])

    got = list(_res(base_url).list(edge_type="KNOWS", page_size=2))
    assert [e.id for e in got] == [1, 2, 3]
    assert route.calls[-1].request.url.params["cursor"] == "2"
    assert route.calls[0].request.url.params["type"] == "KNOWS"


@respx.mock
def test_list_terminates_on_stuck_cursor(base_url):
    # A non-spec-compliant server that never advances the cursor must not hang
    # the client: the generator stops once the cursor repeats.
    page = httpx.Response(200, json=[_edge_row(1, 1, 2)], headers={"X-Next-Cursor": "stuck"})
    respx.get(f"{base_url}/edges").mock(side_effect=[page, page, page])
    got = list(_res(base_url).list(page_size=1))
    # page1: cursor="stuck" (advances); page2: cursor=="stuck"==prev -> terminate.
    assert len(got) == 2
