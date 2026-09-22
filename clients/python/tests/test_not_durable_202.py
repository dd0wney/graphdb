"""A 202 Accepted write is applied but not durable (graphdb:v1.4-python-client-202-not-durable).

The server answers 202 when it applied a write in memory but could not append it
to the write-ahead log. The write is real. It is not on disk yet. The client must
surface that, and must not turn it into an exception: a caller that catches an
exception would retry, POST is not idempotent, and the retry would duplicate the
entity the server already created.

The shape mirrors the TypeScript client (PR #625): detection by the 202 status
alone and never by the message text, five server fields plus an optional entity
id, and a DeleteResult so a delete can carry the same report.
"""

from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client import DeleteResult, NotDurable
from graphdb_client._transport import Transport
from graphdb_client.resources.edges import EdgesResource
from graphdb_client.resources.nodes import NodesResource
from graphdb_client.resources.vector_indexes import VectorIndexesResource

# The five flat fields the server adds to the ordinary body, plus the entity id.
ACCEPTED_BODY = {
    "applied": True,
    "durable": False,
    "retry": False,
    "error": "WAL write failed",
    "message": "change applied in memory, not yet durable",
}


def _transport(base_url):
    return Transport(base_url, token="tok")


def _node_body(**extra):
    return {"id": 5, "labels": ["Person"], "properties": {"name": "A"}, **extra}


def _edge_body(**extra):
    return {"id": 9, "from_node_id": 1, "to_node_id": 2, "type": "KNOWS", **extra}


def _assert_report(nd, *, entity_id):
    assert isinstance(nd, NotDurable)
    assert nd.applied is True
    assert nd.durable is False
    assert nd.retry is False
    assert nd.error == "WAL write failed"
    assert nd.message == "change applied in memory, not yet durable"
    assert nd.id == entity_id


@respx.mock
def test_node_create_on_202_carries_the_report(base_url):
    respx.post(f"{base_url}/nodes").mock(
        return_value=httpx.Response(202, json={**_node_body(), **ACCEPTED_BODY})
    )
    n = NodesResource(_transport(base_url)).create(["Person"], {"name": "A"})
    # The write is still returned in full: a caller that ignores the report
    # keeps exactly the behaviour it had before this change.
    assert n.id == 5 and n.labels == ["Person"] and n.properties["name"] == "A"
    _assert_report(n.not_durable, entity_id=5)


@respx.mock
def test_node_create_on_201_has_no_report(base_url):
    respx.post(f"{base_url}/nodes").mock(return_value=httpx.Response(201, json=_node_body()))
    n = NodesResource(_transport(base_url)).create(["Person"], {"name": "A"})
    assert n.not_durable is None


@respx.mock
def test_detection_is_by_status_not_by_message(base_url):
    """A 201 whose body happens to carry the fields is NOT a not-durable write."""
    respx.post(f"{base_url}/nodes").mock(
        return_value=httpx.Response(201, json={**_node_body(), **ACCEPTED_BODY})
    )
    n = NodesResource(_transport(base_url)).create(["Person"], {"name": "A"})
    assert n.not_durable is None


@respx.mock
def test_node_update_on_202_carries_the_report(base_url):
    respx.put(f"{base_url}/nodes/5").mock(
        return_value=httpx.Response(202, json={**_node_body(), **ACCEPTED_BODY})
    )
    n = NodesResource(_transport(base_url)).update(5, {"name": "A"})
    _assert_report(n.not_durable, entity_id=5)


@respx.mock
def test_edge_create_on_202_carries_the_report(base_url):
    respx.post(f"{base_url}/edges").mock(
        return_value=httpx.Response(202, json={**_edge_body(), **ACCEPTED_BODY})
    )
    e = EdgesResource(_transport(base_url)).create(1, 2, "KNOWS")
    assert e.id == 9 and e.type == "KNOWS"
    _assert_report(e.not_durable, entity_id=9)


@respx.mock
def test_edge_update_on_202_carries_the_report(base_url):
    respx.put(f"{base_url}/edges/9").mock(
        return_value=httpx.Response(202, json={**_edge_body(), **ACCEPTED_BODY})
    )
    e = EdgesResource(_transport(base_url)).update(9, {"k": "v"})
    _assert_report(e.not_durable, entity_id=9)


@respx.mock
def test_vector_index_create_on_202_carries_the_report(base_url):
    body = {"property_name": "embedding", "dimensions": 8, "metric": "cosine"}
    respx.post(f"{base_url}/vector-indexes").mock(
        return_value=httpx.Response(202, json={**body, **ACCEPTED_BODY})
    )
    vi = VectorIndexesResource(_transport(base_url)).create("embedding", 8)
    assert vi.property_name == "embedding"
    # A vector index has no numeric entity id, so the report carries none.
    assert vi.not_durable is not None and vi.not_durable.id is None


@pytest.mark.parametrize(
    "kind,path,call",
    [
        ("node", "/nodes/5", lambda t: NodesResource(t).delete(5)),
        ("edge", "/edges/9", lambda t: EdgesResource(t).delete(9)),
        ("vector-index", "/vector-indexes/embedding",
         lambda t: VectorIndexesResource(t).delete("embedding")),
    ],
)
@respx.mock
def test_delete_on_202_returns_a_result_carrying_the_report(base_url, kind, path, call):
    respx.delete(f"{base_url}{path}").mock(
        return_value=httpx.Response(202, json=dict(ACCEPTED_BODY))
    )
    res = call(_transport(base_url))
    assert isinstance(res, DeleteResult), f"{kind} delete must return a DeleteResult"
    assert res.not_durable is not None
    assert res.not_durable.applied is True
    assert res.not_durable.durable is False


@respx.mock
def test_delete_on_204_returns_an_empty_result(base_url):
    respx.delete(f"{base_url}/nodes/5").mock(return_value=httpx.Response(204))
    res = NodesResource(_transport(base_url)).delete(5)
    assert isinstance(res, DeleteResult)
    assert res.not_durable is None


@respx.mock
def test_a_malformed_202_body_still_yields_a_report(base_url):
    """A 202 whose body is not an object must not raise: the write still applied."""
    respx.delete(f"{base_url}/nodes/5").mock(
        return_value=httpx.Response(
            202, content=b"not json", headers={"Content-Type": "text/plain"}
        )
    )
    res = NodesResource(_transport(base_url)).delete(5)
    assert res.not_durable is not None
    assert res.not_durable.applied is False
    assert res.not_durable.error == ""
    assert res.not_durable.id is None


# --- the async client carries the same report -------------------------------
#
# The async resources are separate modules, so a fix applied only to the sync
# ones would pass every test above. These cover the async side of each shape.

from graphdb_client.aio.resources.edges import AsyncEdgesResource  # noqa: E402
from graphdb_client.aio.resources.nodes import AsyncNodesResource  # noqa: E402
from graphdb_client.aio.resources.vector_indexes import (  # noqa: E402
    AsyncVectorIndexesResource,
)
from graphdb_client.aio.transport import AsyncTransport  # noqa: E402


def _atransport(base_url):
    return AsyncTransport(base_url, token="tok")


@respx.mock
async def test_async_node_create_on_202_carries_the_report(base_url):
    respx.post(f"{base_url}/nodes").mock(
        return_value=httpx.Response(202, json={**_node_body(), **ACCEPTED_BODY})
    )
    n = await AsyncNodesResource(_atransport(base_url)).create(["Person"], {"name": "A"})
    assert n.id == 5
    _assert_report(n.not_durable, entity_id=5)


@respx.mock
async def test_async_edge_create_on_202_carries_the_report(base_url):
    respx.post(f"{base_url}/edges").mock(
        return_value=httpx.Response(202, json={**_edge_body(), **ACCEPTED_BODY})
    )
    e = await AsyncEdgesResource(_atransport(base_url)).create(1, 2, "KNOWS")
    _assert_report(e.not_durable, entity_id=9)


@respx.mock
async def test_async_vector_index_create_on_202_carries_the_report(base_url):
    body = {"property_name": "embedding", "dimensions": 8, "metric": "cosine"}
    respx.post(f"{base_url}/vector-indexes").mock(
        return_value=httpx.Response(202, json={**body, **ACCEPTED_BODY})
    )
    vi = await AsyncVectorIndexesResource(_atransport(base_url)).create("embedding", 8)
    assert vi.not_durable is not None and vi.not_durable.id is None


@respx.mock
async def test_async_node_delete_on_202_returns_a_result(base_url):
    respx.delete(f"{base_url}/nodes/5").mock(
        return_value=httpx.Response(202, json=dict(ACCEPTED_BODY))
    )
    res = await AsyncNodesResource(_atransport(base_url)).delete(5)
    assert isinstance(res, DeleteResult)
    assert res.not_durable is not None and res.not_durable.applied is True


@respx.mock
async def test_async_node_create_on_201_has_no_report(base_url):
    respx.post(f"{base_url}/nodes").mock(return_value=httpx.Response(201, json=_node_body()))
    n = await AsyncNodesResource(_atransport(base_url)).create(["Person"], {"name": "A"})
    assert n.not_durable is None
