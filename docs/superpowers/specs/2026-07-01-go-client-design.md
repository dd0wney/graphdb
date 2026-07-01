# Design: Go-native client (`clients/go`) — v1.3

**Date**: 2026-07-01
**Track**: v1.3.0 "Deploy anywhere" — deliverable 2 of 3 (Helm+Terraform shipped in #450; the
repo-wide `gofmt` gate remains a separate cycle).
**Scope of this spec**: the first-party Go client, "core graph + raw escape hatch" cut.
**Status**: approved (design), pending implementation plan.

## Problem

graphdb ships first-party clients for Python (`clients/python`) and TypeScript
(`workers/graphdb-client`) but not Go — a gap for the language the server itself is written in.
This spec adds `clients/go`, a lightweight, idiomatic Go client covering the core graph
operations, with a raw escape hatch so no endpoint is unreachable.

## Reference: the Python client (surface to mirror)

`clients/python/src/graphdb_client` is the model. It has 9 faceted resources
(nodes, edges, search, vector_indexes, algorithms, tenants, api_keys, security, compliance),
a transport with retry + auth, a typed error taxonomy, streaming pagination, and top-level
helpers (`traverse`, `vector_search`, `query`, `graphql`, `embeddings`, `retrieve`).

### Server contract (from the Python transport + resources)

- **Auth** (exactly one mode):
  - Bearer token → `Authorization: Bearer <token>`
  - API key → `X-API-Key: <key>`
  - Username/password → `POST /auth/login` returns `{access_token, refresh_token}`; on a `401`
    the client calls `POST /auth/refresh` with `{refresh_token}` (falling back to re-login) and
    retries the request **once**.
- **Endpoints in scope:**
  - Nodes: `POST /nodes`, `GET /nodes/{id}`, `PUT /nodes/{id}`, `DELETE /nodes/{id}`,
    `POST /nodes/batch`, `GET /nodes?label=&page=&page_size=` (paginated).
  - Edges: `POST /edges`, `GET /edges/{id}`, `PUT /edges/{id}`, `DELETE /edges/{id}`,
    `POST /edges/batch`.
  - Search: `POST /search` (full-text), `POST /hybrid-search`; vector search + vector-index
    management; `POST /traverse`; `POST /query` (Cypher), `POST /graphql`, `POST /embeddings`,
    `POST /retrieve`.
- **Error mapping** (Python taxonomy): 400 → Validation, 401/403 → Auth, 404 → NotFound,
  409 → Conflict, 429 → RateLimit, 5xx → Server.

> Exact request/response JSON shapes are pinned at implementation time by reading each Python
> resource method and, where ambiguous, the server handler in `pkg/api`. The plan names the
> specific files.

## Chosen approach (and rejected alternatives)

### 1. Separate nested Go module

**Chosen:** `clients/go/go.mod` with `module github.com/dd0wney/graphdb/clients/go`, **zero
third-party dependencies** (stdlib `net/http` + `encoding/json` only).

- A consumer runs `go get github.com/dd0wney/graphdb/clients/go` and gets a tiny dep tree.
- **Rejected — client in the root module**: the root module `github.com/dd0wney/graphdb`
  pulls the entire server (storage/query engine, etc.); a root-module client would drag all
  of that into every consumer's build. The nested-module split is the standard fix
  (cf. `k8s.io/client-go`).
- Side benefit: the root module's gitignored `enterprise-plugins/` build gotcha never touches
  a separate module. `go build ./...` at the repo root does not descend into nested modules.
- **Go version:** `go 1.23` in the client's `go.mod` (for `iter.Seq2`); the repo builds on 1.26.

### 2. Idiomatic Go, faceted like the other clients

- Construction via functional options:
  `graphdb.New(baseURL, graphdb.WithToken(...) | WithAPIKey(...) | WithLogin(user, pass),
  WithTimeout(d), WithRetries(n), WithHTTPClient(*http.Client))`.
  `New` returns `(*Client, error)` and validates that exactly one auth mode was supplied.
- `context.Context` is the first argument of every network method.
- Faceted resources reachable as fields: `client.Nodes`, `client.Edges`, `client.Search`.
- **Rejected — flat method surface** (`client.CreateNode`): loses the cross-client familiarity
  and groups poorly as the surface grows.

### 3. Typed errors

```go
type Error struct {
    Status  int
    Code    string
    Message string
    Method  string
    Path    string
}
func (e *Error) Error() string
func (e *Error) Unwrap() error   // returns the matching sentinel
```
Sentinels: `ErrValidation`, `ErrAuth`, `ErrNotFound`, `ErrConflict`, `ErrRateLimit`,
`ErrServer`. Every non-2xx response becomes an `*Error` whose `Unwrap()` returns the sentinel
for its status, so `errors.Is(err, graphdb.ErrNotFound)` works. Mirrors the Python taxonomy.

### 4. Pagination

`Nodes.List(ctx, opts) iter.Seq2[Node, error]` — a Go 1.23 range-over-func iterator that
streams pages lazily and yields a terminal `(zero, err)` on failure. Plus
`Nodes.ListAll(ctx, opts) ([]Node, error)` for the common "just give me the slice" case.

- **Rejected — slice-only**: simple but buffers the whole result set; the iterator matches the
  server's paging and lets callers stop early.
- **Rejected — custom Pager struct**: `iter.Seq2` is the modern idiom and needs no new type.

### 5. Raw escape hatch

`client.Raw(ctx, method, path string, body any) (*RawResponse, error)` where `RawResponse`
exposes the status and the parsed/`[]byte` JSON. Covers every endpoint not yet faceted
(tenants, api_keys, security, compliance, and anything new) so the MVP is never a dead end.

## File layout

```
clients/go/
  go.mod                 # module github.com/dd0wney/graphdb/clients/go ; go 1.23 ; no deps
  graphdb.go             # Client, New + options, facet wiring; high-level helpers:
                         #   Traverse, VectorSearch, Query, GraphQL, Embeddings, Retrieve
  transport.go           # base-URL join, auth-header injection, retry+backoff, 401→refresh→
                         #   retry-once, JSON encode/decode, non-2xx → *Error
  auth.go                # auth state (token/api_key/login), login + refresh calls
  errors.go              # Error type, sentinels, status→sentinel mapping, from-response ctor
  models.go              # Node, Edge, SearchHit, SearchResult, QueryResult, EmbeddingsResult,
                         #   RetrieveResult, VectorIndex (JSON-tagged)
  nodes.go               # Nodes: Create, Get, Update, Delete, BatchCreate, List, ListAll
  edges.go               # Edges: Create, Get, Update, Delete, BatchCreate
  search.go              # Search: FullText, Hybrid; VectorSearch; VectorIndexes Create/List/Delete
  raw.go                 # Raw + RawResponse
  README.md              # install, auth, quickstart, escape-hatch, godoc pointer
  graphdb_test.go, transport_test.go, nodes_test.go, edges_test.go, search_test.go, errors_test.go
  example_test.go        # runnable Example* for godoc
```

Each file has one responsibility; resources depend only on the internal transport interface,
not on each other.

## Testing strategy

All tests use `net/http/httptest.Server` — no live graphdb needed:

- **Per-resource happy path**: assert method, path, query params, and request body; return
  canned JSON; assert the decoded model.
- **Error mapping**: a table test driving 400/401/403/404/409/429/500 → the right sentinel via
  `errors.Is`, and that `*Error` carries status/method/path.
- **Auth**: token → `Authorization: Bearer`; api_key → `X-API-Key`; login flow hits
  `/auth/login`; a seeded `401` triggers `/auth/refresh` and a single retry that then succeeds.
- **Pagination**: a 3-page fake; assert `List` streams all items in order and `ListAll`
  collects them; assert a mid-stream error surfaces through the iterator.
- **Options validation**: `New` with zero or multiple auth modes returns an error.
- `example_test.go` compiles as part of `go test` (guards the public surface).

Table-driven tests throughout (repo Go convention). No `unwrap`-less `//nolint`. `go vet` +
`gofmt` clean.

## CI

The client is a separate module, so the repo's `go build ./...` won't reach it. Add a job to
the existing lint/test workflow (or a new small workflow) that runs, from `clients/go`:

```
gofmt -l .            # fails if any file is unformatted
go vet ./...
go build ./...
go test ./...
```

This gives the Go client its own gofmt gate. The **repo-wide** `gofmt` gate (the third v1.3
deliverable) stays a separate cycle.

## Explicitly out of scope

- Admin facets: **tenants, api_keys, security, compliance** (reachable via `Raw`; own follow-up).
- A **caching layer** (Python has one) — YAGNI for the Go v1.
- Async/sync split — Go's `net/http` + `context` is one API usable concurrently; no split needed.
- Publishing/tagging (`clients/go/vX.Y.Z`) — a release step; consumers can `go get ...@<commit>`
  meanwhile. Noted, not built here.

## Open items to resolve at implementation time

- Confirm exact JSON field names for each request/response by reading the Python resource
  methods and the corresponding `pkg/api` handlers (esp. batch payloads, search filters,
  traverse params, and the `access_token`/`refresh_token` login shape).
- Confirm node/edge ID type on the wire (Python treats IDs as `int`; verify `uint64` vs `int64`
  for the Go models to avoid overflow — graphdb uses `uint64` IDs internally).
- Confirm pagination param/cursor names (`page`/`page_size` vs a `next` token) from the server.
