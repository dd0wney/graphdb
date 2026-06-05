from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.search import SearchResource


def _res(base_url):
    return SearchResource(Transport(base_url, token="tok"))


@respx.mock
def test_fulltext_maps_hits(base_url):
    route = respx.post(f"{base_url}/search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 1, "score": 2.0, "snippet": "hi"}], "count": 1, "took_ms": 3,
    }))
    hits = _res(base_url).fulltext("hello", labels=["Doc"])
    assert len(hits) == 1 and hits[0].node_id == 1 and hits[0].snippet == "hi"
    body = route.calls.last.request.read()
    assert b'"labels"' in body and b'"hello"' in body


@respx.mock
def test_hybrid_maps_degraded_and_ranks(base_url):
    respx.post(f"{base_url}/hybrid-search").mock(return_value=httpx.Response(200, json={
        "results": [{"node_id": 4, "score": 0.7, "fts_rank": 1, "lsa_rank": -1}],
        "count": 1, "took_ms": 8, "degraded": "no-lsa-index",
    }))
    r = _res(base_url).hybrid("q")
    assert r.degraded == "no-lsa-index" and r.hits[0].fts_rank == 1


@respx.mock
def test_hybrid_omits_alpha_when_none_sends_when_zero(base_url):
    route = respx.post(f"{base_url}/hybrid-search").mock(
        return_value=httpx.Response(200, json={"results": [], "count": 0, "took_ms": 1}))
    _res(base_url).hybrid("q")
    assert b'"alpha"' not in route.calls.last.request.read()
    _res(base_url).hybrid("q", alpha=0.0)
    assert b'"alpha"' in route.calls.last.request.read()
