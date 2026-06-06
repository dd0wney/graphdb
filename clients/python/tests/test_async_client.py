from __future__ import annotations

import httpx
import respx

from graphdb_client import AsyncGraphDBClient
from graphdb_client.aio.resources.nodes import AsyncNodesResource
from graphdb_client.aio.resources.tenants import AsyncTenantsResource


def test_async_client_wires_resources():
    c = AsyncGraphDBClient("https://graphdb.test", token="tok")
    assert isinstance(c.nodes, AsyncNodesResource)
    assert isinstance(c.tenants, AsyncTenantsResource)
    # spot-check a couple of the others exist
    assert c.search is not None and c.security is not None and c.compliance is not None


@respx.mock
async def test_async_query_and_graphql(base_url):
    respx.post(f"{base_url}/query").mock(return_value=httpx.Response(
        200, json={"columns": ["n"], "rows": [{"n": 1}], "count": 1, "time": "1ms"}))
    async with AsyncGraphDBClient(base_url, token="tok") as db:
        r = await db.query("MATCH (n) RETURN n")
        assert r.columns == ["n"] and r.count == 1
    respx.post(f"{base_url}/graphql").mock(
        return_value=httpx.Response(200, json={"data": {"x": 1}})
    )
    async with AsyncGraphDBClient(base_url, token="tok") as db:
        out = await db.graphql("{ x }")
        assert out["data"]["x"] == 1


@respx.mock
async def test_async_traverse_and_embeddings(base_url):
    respx.post(f"{base_url}/traverse").mock(return_value=httpx.Response(
        200, json={"nodes": [{"id": 1, "labels": [], "properties": {}}]}))
    respx.post(f"{base_url}/v1/embeddings").mock(return_value=httpx.Response(200, json={
        "object": "list", "model": "lsa",
        "data": [{"object": "embedding", "embedding": [0.1], "index": 0}],
        "usage": {}}))
    async with AsyncGraphDBClient(base_url, token="tok") as db:
        ns = await db.traverse(1)
        assert ns[0].id == 1
        emb = await db.embeddings("hi")
        assert emb.vectors == [[0.1]]
