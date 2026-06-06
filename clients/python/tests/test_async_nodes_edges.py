from __future__ import annotations

import httpx
import respx

from graphdb_client.aio.resources.edges import AsyncEdgesResource
from graphdb_client.aio.resources.nodes import AsyncNodesResource
from graphdb_client.aio.transport import AsyncTransport


def _nodes(base_url):
    return AsyncNodesResource(AsyncTransport(base_url, token="tok"))


def _edges(base_url):
    return AsyncEdgesResource(AsyncTransport(base_url, token="tok"))


@respx.mock
async def test_node_create_and_get(base_url):
    respx.post(f"{base_url}/nodes").mock(return_value=httpx.Response(
        201, json={"id": 5, "labels": ["P"], "properties": {"n": "a"}}))
    n = await _nodes(base_url).create(["P"], {"n": "a"})
    assert n.id == 5 and n.labels == ["P"]


@respx.mock
async def test_node_list_async_iterates_cursor(base_url):
    page1 = httpx.Response(200, json=[{"id": 1, "labels": ["P"], "properties": {}}],
                           headers={"X-Next-Cursor": "c2"})
    page2 = httpx.Response(200, json=[{"id": 2, "labels": ["P"], "properties": {}}])
    respx.get(f"{base_url}/nodes").mock(side_effect=[page1, page2])
    ids = [n.id async for n in _nodes(base_url).list(page_size=1)]
    assert ids == [1, 2]


@respx.mock
async def test_edge_create_and_delete(base_url):
    respx.post(f"{base_url}/edges").mock(return_value=httpx.Response(201, json={
        "id": 9, "from_node_id": 1, "to_node_id": 2,
        "type": "LINKS", "properties": {}, "weight": 1.0}))
    e = await _edges(base_url).create(1, 2, "LINKS", weight=1.0)
    assert e.id == 9 and e.type == "LINKS"
    respx.delete(f"{base_url}/edges/9").mock(return_value=httpx.Response(204))
    assert await _edges(base_url).delete(9) is None
