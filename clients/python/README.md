# graphdb-client

First-party Python client for [graphdb](https://github.com/dd0wney/graphdb). Sync, `httpx`-only.

## Install
```bash
pip install graphdb-client
```

## Quickstart
```python
from graphdb_client import GraphDBClient

with GraphDBClient("https://your-graphdb", token="YOUR_TOKEN") as db:
    alice = db.nodes.create(["Person"], {"name": "Alice", "_key": "p:alice"})

    # Batch import; reconcile assigned IDs by your own correlation key.
    created = db.nodes.batch_create([{"labels": ["Person"], "properties": {"_key": "p:bob"}}])
    by_key = {n.properties["_key"]: n.id for n in created}

    # List every Person — pagination is followed automatically.
    for node in db.nodes.list(label="Person"):
        print(node.id, node.properties)

    hits = db.vector_search("embedding", [0.1, 0.2, 0.3], k=5, filter_labels=["Document"])
    neighbours = db.traverse(alice.id, max_depth=1)
```

## Auth
- Static token / API key: `GraphDBClient(url, token=...)` or `GraphDBClient(url, api_key=...)`.
- Auto login + refresh: `GraphDBClient(url, username=..., password=...)`.

## Endpoints not yet faceted
Every endpoint is reachable via the raw escape hatch:
```python
res = db._raw.request("POST", "/hybrid-search", json={"query": "..."})
res.data  # parsed JSON
```

## Search & query

```python
with GraphDBClient("http://localhost:8080", token=TOKEN) as db:
    # vector index management
    db.vector_indexes.create("embedding", dimensions=384)
    print(db.vector_indexes.list())

    # full-text + hybrid search
    hits = db.search.fulltext("graph database", labels=["Doc"])
    hybrid = db.search.hybrid("graph database", alpha=0.5)
    if hybrid.degraded:
        print("hybrid degraded:", hybrid.degraded)

    # embeddings + graph-augmented retrieval
    vecs = db.embeddings(["hello", "world"]).vectors
    docs = db.retrieve("how does X relate to Y?", k=5).documents

    # cypher + graphql + algorithms
    rows = db.query("MATCH (n:Person) RETURN n.name").rows
    gql = db.graphql("{ __typename }")
    pr = db.algorithms.run("pagerank").results
    path = db.algorithms.shortest_path(1, 42)
```

## Admin (requires an admin token)

```python
with GraphDBClient("http://localhost:8080", token=ADMIN_TOKEN) as db:
    # tenants
    db.tenants.create("acme", "Acme Corp")
    print([t.id for t in db.tenants.list()])
    print(db.tenants.usage("acme").node_count)
    db.tenants.suspend("acme"); db.tenants.activate("acme")

    # api keys (the plaintext key is returned ONCE)
    created = db.api_keys.create("ci-pipeline", permissions=["read"], expires_in=86400)
    print("save this:", created.key)
    db.api_keys.revoke(created.id)

    # security + compliance (raw dicts)
    print(db.security.health()["status"])
    db.compliance.set_masking_policy({"email": "hash"}, auto_detect=True)
    print(db.compliance.audit_log(username="admin", limit=10))
```

## Async

`AsyncGraphDBClient` is a drop-in async twin of `GraphDBClient` — same resources
and methods, awaitable:

```python
import asyncio
from graphdb_client import AsyncGraphDBClient

async def main():
    async with AsyncGraphDBClient("http://localhost:8080", token=TOKEN) as db:
        node = await db.nodes.create(["Person"], {"name": "Ada"})
        async for n in db.nodes.list(label="Person"):
            print(n.id)
        hits = await db.search.hybrid("graph database")
        rows = (await db.query("MATCH (n) RETURN n LIMIT 1")).rows

asyncio.run(main())
```

## Tests
- Setup: `uv sync`.
- Unit: `make test` (= `uv run pytest`; mock transport, no server).
- Integration: `GRAPHDB_SDK_IT=1 GRAPHDB_SDK_URL=http://localhost:8080 GRAPHDB_SDK_TOKEN=... uv run pytest tests/integration`.
