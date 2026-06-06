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

## Resilience (retry & backoff)

The client retries transient failures automatically. **This is on by default**
(`retries=2`) — the one behavioral change in M4. Retries use full-jitter
exponential backoff and honor a `Retry-After` header.

- Retried: connection failures, HTTP `429`, and `502/503/504`.
- Idempotency-safe: `429` and connection-refused are retried on any method; other
  `5xx` only on idempotent methods (`GET/PUT/DELETE/HEAD/OPTIONS`).

~~~python
from graphdb_client import GraphDBClient, RetryConfig

# Tune it:
db = GraphDBClient(url, token=TOKEN, retries=RetryConfig(max_retries=5, backoff_factor=1.0))

# Or disable:
db = GraphDBClient(url, token=TOKEN, retries=0)
~~~

`AsyncGraphDBClient` takes the same `retries` argument.

## Caching (optional)

Pass a cache backend to enable opt-in, GET-only response caching (off by default —
zero overhead when unset). The built-in `InMemoryCache` is a thread-safe bounded
LRU with per-entry TTL; implement `CacheBackend` (sync) / `AsyncCacheBackend`
(async) to plug in Redis etc.

```python
from graphdb_client import GraphDBClient, InMemoryCache, CacheConfig

db = GraphDBClient(url, token=TOKEN,
                   cache=InMemoryCache(maxsize=2048),
                   cache_config=CacheConfig(default_ttl=60))

db.nodes.get(1)          # fetched + cached
db.nodes.get(1)          # served from cache
print(db.cache_stats)    # {"hits": 1, "misses": 1, "hit_rate": 0.5}
```

Only `GET` responses are cached. Freshness is TTL-bounded; `PUT`/`PATCH`/`DELETE`
clear the cache (graphdb uses `POST` for reads, so `POST` does not invalidate).
`AsyncGraphDBClient` takes the same `cache`/`cache_config` arguments
(pass a backend implementing `AsyncCacheBackend`; `InMemoryCache` implements both).

Two caveats:

- **Creates rely on TTL, not invalidation.** `POST` is treated as a read (graphdb
  uses it for `/query`, `/search`, etc.), so `nodes.create(...)` does **not** evict
  a cached `nodes.list(...)`. A create followed immediately by a list can be
  TTL-stale; use a short `default_ttl` (or `cache=None`) if read-your-writes
  matters.
- **Do not share one backend across auth contexts.** The cache key is
  `method:path?params` with no tenant/token component. The per-client
  `InMemoryCache` default is safe, but if you wire a *shared* external backend
  (e.g. one Redis) into multiple clients using different tokens/tenants, they will
  collide — give each auth context its own backend (or key namespace).

## LangChain integration (optional)

Install the extra: `pip install 'graphdb-client[langchain]'`. Adapters live under
`graphdb_client.langchain` (the core install stays httpx-only):

```python
from graphdb_client import GraphDBClient
from graphdb_client.langchain import GraphDBRetriever, GraphDBVectorStore, GraphDBLoader

db = GraphDBClient(url, token=TOKEN)

# GraphRAG retriever (/v1/retrieve) — drop into any LangChain chain
retriever = GraphDBRetriever(client=db, k=5)
docs = retriever.invoke("how does auth work?")

# Vector store (retrieval over /vector-search; embeds via graphdb if no Embeddings given)
store = GraphDBVectorStore(db, property_name="embedding", content_key="text")
hits = store.similarity_search("graph database", k=3)

# Document loader (nodes -> Documents)
loader = GraphDBLoader(db, label="Doc", content_key="text")
documents = loader.load()
```

`GraphDBVectorStore` is retrieval-only (`add_texts`/`from_texts` raise); ingest
vectors with `db.nodes.create(...)` + a vector index. Pass an `aclient=AsyncGraphDBClient(...)`
to `GraphDBRetriever`/`GraphDBVectorStore`/`GraphDBLoader` for the async paths
(`ainvoke`/`asimilarity_search`/`alazy_load`).

## Tests
- Setup: `uv sync`.
- Unit: `make test` (= `uv run pytest`; mock transport, no server).
- Integration: `GRAPHDB_SDK_IT=1 GRAPHDB_SDK_URL=http://localhost:8080 GRAPHDB_SDK_TOKEN=... uv run pytest tests/integration`.
