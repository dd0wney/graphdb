# graphdb Python SDK — M2a design spec (search & query slice)

**Date**: 2026-06-05
**Status**: design (awaiting review) → implementation plan (M2a)
**Parent spec**: `docs/superpowers/specs/2026-06-04-python-sdk-design.md` (M1). All
cross-cutting decisions there still hold and are NOT relitigated here: stdlib
dataclasses (`httpx`-only runtime dep, decision D1), sync-only, bring-your-own
token/API-key auth with optional login+refresh, in-repo `clients/python/`,
Python 3.9+, error hierarchy in `errors.py`, the `Transport`/`_raw` escape hatch.

---

## 1. Goal & scope

Add ergonomic, typed facades for the **search & query** half of the M2 surface,
following the M1 architecture exactly. Everything here is *already reachable*
via `client._raw.request(...)`; M2a adds ergonomics over the highest
consumer-value endpoints.

**In scope (7 endpoints):**

| Surface | Endpoint(s) | Facade |
|---|---|---|
| Full-text search | `POST /search` | `client.search.fulltext(...)` |
| Hybrid search | `POST /hybrid-search` | `client.search.hybrid(...)` |
| Vector-index mgmt | `GET/POST /vector-indexes`, `GET/DELETE /vector-indexes/{prop}` | `client.vector_indexes.{create,list,get,delete}` |
| Graph-augmented retrieval | `POST /v1/retrieve` | `client.retrieve(...)` |
| Embeddings (OpenAI-shaped) | `POST /v1/embeddings` | `client.embeddings(...)` |
| Cypher query | `POST /query` | `client.query(...)` |
| GraphQL passthrough | `POST /graphql` | `client.graphql(...)` |
| Algorithms | `POST /algorithms`, `POST /shortest-path` | `client.algorithms.{run,shortest_path}` |

**Out of scope** → deferred to **M2b** (admin/ops slice): compliance, security,
tenants, apikeys. Also still out: async (M3), caching/retry/LangChain (M4).
`vector_search` and `traverse` already shipped in M1 (top-level methods);
unchanged.

## 2. Decision (locked in brainstorming): Approach A

Group multiple-op surfaces into **resources** (mirrors M1's `nodes`/`edges`);
expose single high-level verbs as **top-level methods** (mirrors M1's
`traverse`/`vector_search`). Type the stable shapes with dataclasses; leave
genuinely freeform payloads as `dict`.

Rejected: all-flat top-level methods (namespace pollution, abandons M1's resource
grouping); dict-everywhere (defeats M2's ergonomic purpose, diverges from M1's
typed `Node`/`Edge`).

## 3. File structure

```
src/graphdb_client/
  client.py            # MODIFY: wire 3 new resources + 4 new top-level methods
  models.py            # MODIFY: add the M2a dataclasses (§5)
  resources/
    search.py          # NEW: SearchResource (fulltext, hybrid)
    vector_indexes.py  # NEW: VectorIndexesResource (create, list, get, delete)
    algorithms.py      # NEW: AlgorithmsResource (run, shortest_path)
tests/
  test_search.py       # NEW
  test_vector_indexes.py  # NEW
  test_algorithms.py   # NEW
  test_retrieve.py     # NEW (top-level methods)
  test_embeddings.py   # NEW
  test_query_graphql.py  # NEW
  integration/test_smoke.py  # MODIFY: add opt-in M2a smoke calls
```

No new runtime deps. `Transport`, `errors.py`, and the `from_dict` pattern are
reused verbatim.

## 4. Facade surface (signatures)

All methods call `self._t.request(method, path, json=..., params=...)` and map
the result through a model `from_dict` (or return a dict for freeform shapes).
Keyword-only optional args (`*`) match the M1 style.

### 4.1 `SearchResource` (`client.search`)

```python
def fulltext(self, query, *, limit=20, offset=0, labels=None,
             include_content=False, include_nodes=False) -> list[SearchHit]
def hybrid(self, query, *, limit=20, offset=0, labels=None, alpha=None,
           include_content=False, include_nodes=False) -> HybridSearchResult
```

- `fulltext` → `POST /search` → `SearchResponse.results` → `list[SearchHit]`.
- `hybrid` → `POST /hybrid-search` → `HybridSearchResult` (carries `hits`,
  `count`, `took_ms`, and the **`degraded`** flag: `None` | `"no-lsa-index"` |
  `"query-out-of-vocabulary"` | `"no-fts-match"` — read from the response body's
  `degraded` field). `alpha` is sent only when not `None` (the server
  distinguishes `alpha=0` from unset).

### 4.2 `VectorIndexesResource` (`client.vector_indexes`)

```python
def create(self, property_name, dimensions, *, m=None, ef_construction=None,
           metric=None) -> VectorIndex          # POST /vector-indexes
def list(self) -> list[VectorIndex]             # GET  /vector-indexes
def get(self, property_name) -> VectorIndex     # GET  /vector-indexes/{prop}
def delete(self, property_name) -> None         # DELETE /vector-indexes/{prop}
```

Optional HNSW params (`m`, `ef_construction`, `metric`) are omitted from the body
when `None` so the server applies its defaults (16 / 200 / `"cosine"`).

### 4.3 `AlgorithmsResource` (`client.algorithms`)

```python
def run(self, algorithm, *, parameters=None) -> AlgorithmResult   # POST /algorithms
def shortest_path(self, start_node_id, end_node_id, *, max_depth=None) -> ShortestPath
```

- `run` is the generic entry (`algorithm` ∈ `"pagerank"|"betweenness"|"louvain"|…`);
  `AlgorithmResult.results` is a freeform `dict` (per-algorithm shape).
- `shortest_path` → `POST /shortest-path` → `ShortestPath{path, length, found}`.
- No per-algorithm convenience wrappers (`pagerank()` etc.) in M2a — YAGNI; add
  if a consumer asks.

### 4.4 Top-level methods on `GraphDBClient`

```python
def retrieve(self, query, *, k=None, max_tokens=None, max_hops=None,
             alpha=None, beta=None, tau=None, labels=None,
             include_node=False) -> RetrieveResult            # POST /v1/retrieve
def embeddings(self, input, *, model=None, dimensions=None) -> EmbeddingsResult  # POST /v1/embeddings
def query(self, cypher, *, parameters=None, timeout_seconds=None) -> QueryResult # POST /query
def graphql(self, query, *, variables=None, operation_name=None) -> dict         # POST /graphql
```

- `retrieve` → `RetrieveResult{documents, degraded, took_ms}` (GraphRAG; the
  graph signal lives in each document's `metadata.source.path`).
- `embeddings` accepts a `str` or `Sequence[str]` (sent as the OpenAI `input`
  field); returns `EmbeddingsResult` whose `vectors` is `list[list[float]]`
  (ordered by the response's per-item `index`), plus `model`/`usage`.
- `query` → `QueryResult{columns, rows, count}` (`rows` stays `list[dict]` —
  freeform cell values).
- `graphql` returns the **raw response dict** (arbitrary GraphQL shape;
  `errors`/`data` keys passed through untouched — the SDK does not raise on
  GraphQL-level `errors`, only on HTTP ≥ 400).

## 5. Models (`models.py` additions)

Typed where the shape is stable and load-bearing; `dict` where freeform. Each
dataclass gets a `from_dict` classmethod (M1 pattern). `Node` is reused for
embedded node data (`include_nodes`/`include_node`).

```python
@dataclass
class SearchHit:
    node_id: int
    score: float
    snippet: str | None = None
    fts_rank: int | None = None   # hybrid only; None for fulltext
    lsa_rank: int | None = None   # hybrid only
    node: Node | None = None      # populated when include_nodes=True

@dataclass
class HybridSearchResult:
    hits: list[SearchHit]
    count: int
    took_ms: int
    degraded: str | None = None

@dataclass
class VectorIndex:
    property_name: str
    dimensions: int | None = None
    metric: str | None = None

@dataclass
class RetrieveSource:
    node_id: int
    path: list[int]               # [seed, ..., node_id]; the load-bearing graph signal
    label: str | None = None

@dataclass
class RetrieveDocument:
    page_content: str
    node_id: int
    score: float
    source: RetrieveSource
    node: Node | None = None      # populated when include_node=True

@dataclass
class RetrieveResult:
    documents: list[RetrieveDocument]
    took_ms: int
    degraded: str | None = None

@dataclass
class EmbeddingsResult:
    model: str
    vectors: list[list[float]]    # one vector per input, ordered by the response's `index`
    usage: dict                   # {prompt_tokens, total_tokens}

@dataclass
class QueryResult:
    columns: list[str]
    rows: list[dict]
    count: int

@dataclass
class AlgorithmResult:
    algorithm: str
    results: dict

@dataclass
class ShortestPath:
    path: list[int]
    length: int
    found: bool
```

`RetrieveDocument` flattens the server's `metadata.{node_id,score,source,node}`
into the dataclass (the server nests them under `metadata` for LangChain; the SDK
surfaces them directly and keeps `source.path`).

## 6. Error handling

No change. Every facade goes through `Transport.request`, which raises the M1
error hierarchy (`from_response`) on HTTP ≥ 400. M2a adds no new error types.
`graphql` is the one surface where a 200 can carry logical errors (in the
`errors` key) — by design the SDK returns those in the dict rather than raising,
matching the GraphQL spec and the `_raw` passthrough contract.

## 7. Testing

Mirror M1: one unit-test file per resource/area, driving a **mocked transport**
(`httpx.MockTransport`, as in `tests/conftest.py`) so tests assert the
request shape (method, path, body, omitted-when-None params) and the response
mapping (typed model fields, `degraded` propagation, `include_nodes` embedding).

Specific teeth:
- `hybrid` maps `degraded` and per-stage ranks; `alpha=0` is sent, `alpha=None`
  is omitted.
- `vector_indexes.create` omits `m`/`ef_construction`/`metric` when `None`.
- `embeddings(["a","b"])` and `embeddings("a")` both produce the array `input`;
  `.vectors` returns vectors in input order.
- `shortest_path` maps `found=False` distinctly from an empty path.
- `query` preserves `rows` as `list[dict]`; `graphql` returns the raw dict
  including an `errors` key without raising.

Opt-in integration smoke (`GRAPHDB_INTEGRATION=1`) extends the M1 smoke: create a
vector index → embeddings → vector/hybrid search → retrieve → a trivial
`query`/`graphql` round-trip.

## 8. Definition of done (M2a)

- `client.search`, `client.vector_indexes`, `client.algorithms` resources +
  `retrieve`/`embeddings`/`query`/`graphql` top-level methods, all typed per §4–5.
- Unit tests green (mocked transport); `ruff` + `mypy` clean (M1's gates).
- Integration smoke passes against a real server when `GRAPHDB_INTEGRATION=1`.
- README/usage updated with the new surface; `__init__.py` exports the new models.
- M2b (compliance/security/tenants/apikeys) explicitly remains out — tracked as
  the next milestone slice.
