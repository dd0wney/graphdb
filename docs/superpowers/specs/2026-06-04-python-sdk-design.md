# graphdb Python SDK — design spec

**Date**: 2026-06-04
**Status**: design (awaiting review) → implementation plan (M1)
**Track**: productization (per `NEXT_STEPS_2026-06-03.md` — `v0.4.0` unblocked the consumer-pin path; first-party SDKs are the highest external-value off-path move)

---

## 1. Goal & scope

A first-party **Python SDK** for graphdb, talking to the existing REST API. The
end goal is **full-surface coverage** of all ~40 endpoints, delivered in
milestones. Every endpoint is reachable from M1 (via a raw layer); the
most-used, contract-pinned resources get an ergonomic facade first.

Non-goals (this spec): async client (M3), response caching (M4), LangChain
integration (M4), GraphQL query-builder ergonomics (the `/graphql` passthrough
is enough for M1/M2).

## 2. Decisions locked in brainstorming

| Decision | Choice |
|---|---|
| Surface | Full surface is the goal; reachable from M1 via raw layer, ergonomic facade phased in |
| Build approach | **Hybrid** — generated models from OpenAPI + hand-written Pythonic facade |
| Concurrency | **Sync only** to start; async (M3) shares the same transport + models |
| Auth | Bring-your-own **token / API key** (primary); optional **username+password → `/auth/login` + auto-refresh on 401** (convenience). Tenant is carried by the credential server-side (`withTenant` reads JWT claims), so **no explicit tenant arg** |
| Location | **In-repo** `clients/python/`, mirroring the in-repo TS client (`workers/graphdb-client/`); published to PyPI from CI on tag |
| Python floor | 3.9+ |
| PyPI name | `graphdb-client` (import `graphdb_client`) |

## 3. Open decisions (resolve at spec review, before the implementation plan)

- **D1 — Models / dependency footprint.** Generate **pydantic v2** models via
  `datamodel-code-generator` (recommended: strong validation, best DX, standard
  for typed API clients; runtime deps = `httpx` + `pydantic`) **vs** stdlib
  **dataclasses/TypedDict** (zero extra runtime deps beyond `httpx`, lighter
  install, weaker validation). *Recommendation: pydantic v2.* This choice only
  affects the `_generated/` + facade return types; the architecture is identical
  either way.

## 4. Milestone decomposition

Full surface is the goal; one implementation plan per milestone.

- **M1 (this spec → first plan)** — project scaffold; generated models +
  transport giving **full-surface raw access**; ergonomic **sync facade over the
  core, contract-pinned resources** (nodes, edges, traverse, vector/search);
  auth (token + optional login/refresh); error hierarchy; unit + opt-in
  integration tests; packaging; CI.
- **M2** — ergonomic facades for the remainder: vector-index management,
  hybrid-search, embeddings, `/v1/retrieve` (GraphRAG), `/query` + `/graphql`,
  compliance, security, tenants, apikeys, algorithms, shortest-path.
- **M3** — `AsyncGraphDBClient` over the shared transport + models.
- **M4 (optional)** — response caching (mirror `workers/graphdb-client/src/cache.ts`),
  retry/backoff polish, LangChain retriever helper.

## 5. Architecture (M1)

Layers, bottom-up. Each is independently testable and has one responsibility.

### 5.1 Transport (`src/graphdb_client/_transport.py`)
A thin wrapper over `httpx.Client` — the single choke point for every request.
- Owns: `base_url`, default headers, auth-header injection, timeout, JSON
  encode/decode, status→exception mapping.
- **Auth refresh**: on `401`, if the client was constructed with
  username/password, call `/auth/refresh` (or re-`/auth/login`) once and retry
  the original request a single time; a second `401` raises `AuthError`.
- **Raw escape hatch**: `request(method, path, *, json=None, params=None) -> Any`
  returns parsed JSON. This is how the full surface is reachable in M1 before a
  facade exists (`client._raw.request("POST", "/hybrid-search", json=...)`).
- Reads the `X-Next-Cursor` response header and exposes it to facade pagination.

### 5.2 Generated models (`src/graphdb_client/_generated/`)
Typed models generated from the committed OpenAPI spec
(`docs/internals/openapi.yaml`) via `datamodel-code-generator`.
- Regeneration is a `make generate` target (documented; never hand-edited).
- **Spec drift is a feature, not a risk to hide**: where the spec disagrees with
  a live handler, that's a graphdb bug to fix (a productization win). M1 records
  any drift found during generation as follow-up issues; it does not silently
  paper over it.
- D1 decides pydantic v2 vs dataclasses for the emitted models.

### 5.3 Resource facades (`src/graphdb_client/resources/`)
Hand-written, Pythonic, return typed models. M1 ships:
- `resources/nodes.py` → `client.nodes`
- `resources/edges.py` → `client.edges`
- top-level convenience methods on the client: `traverse(...)`, `vector_search(...)`

### 5.4 Client (`src/graphdb_client/client.py`)
`GraphDBClient` — context manager (`with GraphDBClient(...) as c:`), assembles
transport + resource facades. Constructor:
```
GraphDBClient(
    base_url: str,
    *,
    token: str | None = None,        # JWT → sent as "Authorization: Bearer <token>"
    api_key: str | None = None,      # API key → sent as "X-API-Key: <key>"
    username: str | None = None,     # optional auto-login
    password: str | None = None,
    timeout: float = 30.0,
    max_retries: int = 2,            # transport-level retry on 429/5xx + the 401 refresh
)
```

### 5.5 Errors (`src/graphdb_client/errors.py`)
```
GraphDBError                # base; carries status_code, body, method, path
├── AuthError               # 401 (after refresh attempt)
├── ValidationError         # 400
├── NotFoundError           # 404 (maps ErrNodeNotFound / ErrEdgeNotFound)
├── ConflictError           # 409 (unique-constraint, e.g. :Claim/for_task)
├── RateLimitError          # 429
└── ServerError             # 5xx
```

## 6. Core facade surface (M1) — anchored to the consumer contracts

M1's facade covers exactly the behaviors `docs/CONSUMER_CONTRACTS.md` (CC1–CC9)
pins, so the SDK ships with tests mirroring the real-consumer paths.

| Method | Maps to | Contract |
|---|---|---|
| `client.nodes.create(labels, properties) -> Node` | `POST /nodes` | — |
| `client.nodes.get(id) -> Node` | `GET /nodes/{id}` | — |
| `client.nodes.update(id, properties)` | `PUT/PATCH /nodes/{id}` | — |
| `client.nodes.delete(id)` | `DELETE /nodes/{id}` | — |
| `client.nodes.batch_create(items) -> list[Node]` | `POST /nodes/batch` | **CC7** — returns only created nodes, **with echoed `properties`** (caller reconciles by `_key`); partial-success, unspecified order |
| `client.nodes.list(label=None, page_size=100) -> Iterator[Node]` | `GET /nodes?label=` | **CC8** — **auto-paginates** by following `X-Next-Cursor` to completion, yielding nodes **with properties** |
| `client.edges.{create,get,update,delete,batch_create}` | `/edges*` | — |
| `client.traverse(start_node_id, max_depth=1) -> list[Node]` | `POST /traverse` | **CC9** — outgoing neighbors within `max_depth` |
| `client.vector_search(property_name, query, k=10, ef=None, filter_labels=None) -> list[SearchResult]` | `POST /vector-search` | **CC1/CC2/CC5** — float-array ingest, NN identity+order, label post-filter |

`nodes.list` is a **generator** that transparently follows the cursor — callers
never see pagination. This is the single most valuable ergonomic in M1 (it's the
exact footgun jailgraph's CC8 guards against: a partial fetch silently drops
nodes).

## 7. Testing

- **Unit (default, no server)** — `respx` mocks `httpx`. Assert: request shape
  (method/path/headers/body), response parsing into models, the 401→refresh→retry
  flow, error mapping, and especially the two pieces of real logic the SDK owns:
  `nodes.list` cursor-following (multi-page → all nodes, terminates) and
  `nodes.batch_create` `_key`-reconcilable echo.
- **Integration (opt-in, `GRAPHDB_SDK_IT=1`)** — build + launch the real graphdb
  binary (the pattern `scripts/consumer-drive.sh` already uses) and run a smoke
  mirroring CC7/CC8/CC9 end-to-end. Not in the default `pytest` run; documented
  in the README.

## 8. Packaging & CI

- `clients/python/` — `pyproject.toml` (PEP 621), `src/graphdb_client/`,
  `tests/`, `README.md`, `Makefile` (`generate`, `lint`, `test`).
- Tooling: `ruff` (lint+format), `mypy` (types), `pytest` (+`respx`).
- CI: a `clients/python` job (ruff + mypy + pytest on the Python matrix);
  **PyPI publish on tag** (`py-sdk-vX.Y.Z`), trusted-publishing/OIDC.
- README: quickstart (install, auth, the four core calls), the auto-pagination
  note, the raw escape hatch for not-yet-faceted endpoints, the integration-test
  opt-in.

## 9. Risks & mitigations

- **OpenAPI spec drift/gaps** (it's hand-maintained). *Mitigation*: M1's
  generation step surfaces drift as graphdb issues; the facade (hand-written) is
  authoritative for the core, so core ergonomics don't depend on spec accuracy.
  The raw layer works regardless.
- **Scope creep toward "full surface now"**. *Mitigation*: M1 is core facade +
  full raw access only; everything else is M2/M3, explicitly out of this plan.
- **pydantic dependency weight** (if D1 picks pydantic). *Mitigation*: it's the
  only non-`httpx` runtime dep; acceptable for a typed API client. D1 can pick
  dataclasses if a zero-extra-dep install is required.

## 10. Definition of done (M1)

- `pip install graphdb-client` (from a built wheel) gives a working sync client.
- The four core flows (create/batch/list-paginated/traverse/vector-search) work
  against a live graphdb with auth.
- Unit suite green (mock transport); opt-in integration smoke green against the
  real binary; `ruff` + `mypy` clean; CI job added.
- README quickstart + the raw escape hatch documented.
- Any OpenAPI drift found is filed as graphdb follow-up issues.
