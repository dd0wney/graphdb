from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.nodes import NodesResource


def _res(base_url):
    return NodesResource(Transport(base_url, token="tok"))


@respx.mock
def test_create_and_get(base_url):
    respx.post(f"{base_url}/nodes").mock(
        return_value=httpx.Response(
            201, json={"id": 5, "labels": ["Person"], "properties": {"name": "A"}}
        )
    )
    n = _res(base_url).create(["Person"], {"name": "A"})
    assert n.id == 5 and n.labels == ["Person"] and n.properties["name"] == "A"


@respx.mock
def test_batch_create_echoes_properties_for_key_reconcile(base_url):
    respx.post(f"{base_url}/nodes/batch").mock(return_value=httpx.Response(201, json={
        "nodes": [
            {"id": 11, "labels": ["P"], "properties": {"_key": "p:1"}},
            {"id": 12, "labels": ["P"], "properties": {"_key": "p:2"}},
        ],
        "created": 2, "time": "1ms",
    }))
    nodes = _res(base_url).batch_create([
        {"labels": ["P"], "properties": {"_key": "p:1"}},
        {"labels": ["P"], "properties": {"_key": "p:2"}},
    ])
    by_key = {n.properties["_key"]: n.id for n in nodes}
    assert by_key == {"p:1": 11, "p:2": 12}


@respx.mock
def test_list_auto_paginates_across_cursor(base_url):
    page1 = httpx.Response(200, json=[
        {"id": 1, "labels": ["P"], "properties": {"_key": "a"}},
        {"id": 2, "labels": ["P"], "properties": {"_key": "b"}},
    ], headers={"X-Next-Cursor": "2"})
    page2 = httpx.Response(200, json=[
        {"id": 3, "labels": ["P"], "properties": {"_key": "c"}},
    ])
    route = respx.get(f"{base_url}/nodes").mock(side_effect=[page1, page2])

    got = list(_res(base_url).list(label="P", page_size=2))
    assert [n.id for n in got] == [1, 2, 3]
    assert [n.properties["_key"] for n in got] == ["a", "b", "c"]
    assert route.calls[-1].request.url.params["cursor"] == "2"
    assert route.calls[0].request.url.params["label"] == "P"


@respx.mock
def test_list_terminates_on_stuck_cursor(base_url):
    # A non-spec-compliant server that never advances the cursor must not hang
    # the client: the generator stops once the cursor repeats.
    page = httpx.Response(
        200, json=[{"id": 1, "labels": ["P"], "properties": {}}], headers={"X-Next-Cursor": "stuck"}
    )
    respx.get(f"{base_url}/nodes").mock(side_effect=[page, page, page])
    got = list(_res(base_url).list(page_size=1))
    # page1: cursor="stuck" (advances); page2: cursor=="stuck"==prev -> terminate.
    assert len(got) == 2


@respx.mock
def test_delete(base_url):
    route = respx.delete(f"{base_url}/nodes/7").mock(return_value=httpx.Response(200))
    _res(base_url).delete(7)
    assert route.called
