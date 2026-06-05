from __future__ import annotations

import httpx
import respx

from graphdb_client import GraphDBClient


def _c(base_url):
    return GraphDBClient(base_url, token="tok")


@respx.mock
def test_retrieve_maps_documents_and_path(base_url):
    route = respx.post(f"{base_url}/v1/retrieve").mock(return_value=httpx.Response(200, json={
        "documents": [{"page_content": "chunk", "metadata": {
            "node_id": 9, "score": 0.8, "source": {"node_id": 5, "label": "Doc", "path": [5, 9]}}}],
        "took_ms": 7, "degraded": "no-lsa-index",
    }))
    r = _c(base_url).retrieve("q", k=5, include_node=True)
    assert r.documents[0].source.path == [5, 9] and r.degraded == "no-lsa-index"
    body = route.calls.last.request.read()
    assert b'"k"' in body and b'"include_node"' in body


@respx.mock
def test_retrieve_omits_unset_tuning_params(base_url):
    route = respx.post(f"{base_url}/v1/retrieve").mock(return_value=httpx.Response(
        200, json={"documents": [], "took_ms": 1}))
    _c(base_url).retrieve("q")
    body = route.calls.last.request.read()
    for absent in (b'"k"', b'"alpha"', b'"beta"', b'"tau"', b'"max_hops"', b'"labels"'):
        assert absent not in body
