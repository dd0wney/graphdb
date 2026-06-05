from __future__ import annotations

import httpx
import respx

from graphdb_client import GraphDBClient


def _c(base_url):
    return GraphDBClient(base_url, token="tok")


def _resp():
    return httpx.Response(200, json={
        "object": "list", "model": "lsa",
        "data": [{"object": "embedding", "embedding": [0.1, 0.2], "index": 0}],
        "usage": {"prompt_tokens": 2, "total_tokens": 2},
    })


@respx.mock
def test_embeddings_single_string_becomes_array_input(base_url):
    route = respx.post(f"{base_url}/v1/embeddings").mock(return_value=_resp())
    r = _c(base_url).embeddings("hello")
    assert r.vectors == [[0.1, 0.2]] and r.model == "lsa"
    assert b'["hello"]' in route.calls.last.request.read()


@respx.mock
def test_embeddings_list_input(base_url):
    route = respx.post(f"{base_url}/v1/embeddings").mock(return_value=_resp())
    _c(base_url).embeddings(["a", "b"])
    assert b'"a"' in route.calls.last.request.read()
