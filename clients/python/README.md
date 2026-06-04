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

## Tests
- Setup: `uv sync`.
- Unit: `make test` (= `uv run pytest`; mock transport, no server).
- Integration: `GRAPHDB_SDK_IT=1 GRAPHDB_SDK_URL=http://localhost:8080 GRAPHDB_SDK_TOKEN=... uv run pytest tests/integration`.
