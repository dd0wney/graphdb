from __future__ import annotations

import httpx
import respx

from graphdb_client.client import GraphDBClient


@respx.mock
def test_traverse_returns_nodes(base_url):
    respx.post(f"{base_url}/traverse").mock(return_value=httpx.Response(200, json={
        "nodes": [
            {"id": 1, "labels": ["Process"], "properties": {}},
            {"id": 2, "labels": ["File"], "properties": {}},
        ],
        "count": 2, "time": "1ms",
    }))
    with GraphDBClient(base_url, token="tok") as c:
        nodes = c.traverse(1, max_depth=1)
    assert {n.id for n in nodes} == {1, 2}


@respx.mock
def test_vector_search_returns_results(base_url):
    respx.post(f"{base_url}/vector-search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 9, "distance": 0.0, "score": 1.0}],
        "count": 1, "took_ms": 1,
    }))
    with GraphDBClient(base_url, token="tok") as c:
        res = c.vector_search("embedding", [1.0, 0.0, 0.0], k=1, filter_labels=["Document"])
    assert res[0].node_id == 9 and res[0].score == 1.0


@respx.mock
def test_raw_escape_hatch(base_url):
    respx.post(f"{base_url}/hybrid-search").mock(
        return_value=httpx.Response(200, json={"results": []})
    )
    with GraphDBClient(base_url, token="tok") as c:
        out = c._raw.request("POST", "/hybrid-search", json={"query": "x"})
    assert out.data == {"results": []}


@respx.mock
def test_context_manager_closes(base_url):
    c = GraphDBClient(base_url, token="tok")
    c.close()  # idempotent, no error
