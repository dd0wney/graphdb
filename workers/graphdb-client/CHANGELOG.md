# Changelog

All notable changes to `@graphdb/client` are documented in this file.

## 2.0.0

Rewrites the client against the server's actual HTTP contract
(`pkg/api` in the root graphdb repo). Every change below was verified by
reading the matching Go handler; each fix landed behind a test that was
first observed failing against the pre-fix client. This is a major
version bump: public methods are removed, and several method signatures
and return shapes change incompatibly.

### Removed

- **`getTrustScore(userId)`** and **`findFraudRing(userId)`** — both
  built GraphQL queries against a `trustScore` / `fraudRing` field on
  `User` that no resolver in `pkg/graphql` has ever defined. Every call
  failed with a GraphQL "cannot query field" error. No replacement:
  nothing could have depended on these working.
- `TrustScore` and `FraudRing` types.

### Fixed (contract defects)

- **`updateNode(id, input)`** now sends `PUT`, not `PATCH`. The route
  `/nodes/{id}` accepts `GET`, `PUT`, `DELETE` only
  (`pkg/api/handlers_nodes.go`); PATCH was rejected with 405 on every
  call.
- **`queryNodes(filter, options)`** now reads the next-page cursor from
  the `X-Next-Cursor` response header, not a `cursor` field in the JSON
  body (the server never sent one). Returns `{ nodes, cursor }` —
  `cursor` is absent on the last page. The `filters` parameter is now
  `{ label?: string }`: `?label=` is the only filter GET `/nodes`
  reads; the old generic `filters` object was serialized into a
  `?filter=` param the server never parsed. `offset`, `sortBy`,
  `sortOrder`, `fields` are removed from `QueryNodesOptions` for the
  same reason — none were read server-side.
- **`traverse(options)`** now calls `POST /traverse`
  (`pkg/api/handlers_algorithms_traversal.go`) instead of a GraphQL
  `traverse` query, which — like `trustScore`/`fraudRing` — matches no
  resolver. The result is now `{ nodes, count, time, truncated? }`; the
  server's `TraversalResponse` has no `edges` or `paths` field.
  `TraversalOptions.limit` and `.nodeFilter` are removed: the server's
  `TraversalRequest` has no such fields, so both were silently ignored.
- **`Node`** now matches `NodeResponse`: `id` is a JSON number (the
  server's `uint64`, not a string), and a node's type is carried as
  `labels: string[]`, not `type: string`. `createdAt`/`updatedAt` are
  removed — the server never returns them.
- **`Edge`** now matches `EdgeResponse`: numeric `id`, `from_node_id`/
  `to_node_id` (not `source`/`target`), and `weight: number`.
- **`CreateNodeInput`** now sends `labels: string[]` (matching
  `NodeRequest`), not `type: string`. The old shape meant `createNode()`
  failed validation ("at least one label is required") on every call.
- **`CreateEdgeInput`** now sends `from_node_id`/`to_node_id` (matching
  `EdgeRequest`), not `source`/`target`, and accepts an optional
  `weight`. The old field names were never read by the server, so
  every edge was created with `from_node_id: 0, to_node_id: 0`.
- **`batchCreateNodes`/`batchCreateEdges`** request bodies inherit the
  `labels`/`from_node_id`/`to_node_id` fixes above (`BatchNodeRequest`/
  `BatchEdgeRequest` embed the same `NodeRequest`/`EdgeRequest` shape).
  Their response types are renamed `BatchNodeResult`/`BatchEdgeResult`
  and now match `BatchNodeResponse`/`BatchEdgeResponse`: `nodes`/`edges`
  arrays, a `failed` **count** (not an array), and per-item failures in
  `errors: [{ index, error }]`. The old `BatchResult<T>` (`{success,
  failed}`, with `failed` as created-item objects) never matched the
  wire shape.
- **`deleteNode`/`deleteEdge`** tests and docs now reflect that the
  server responds `200` with a JSON body (`{"deleted": id}`), not
  `204`. `deleteVectorIndex` is unaffected — that route is the one that
  really does respond `204`.

### Added

- `getEdge(id)`, `updateEdge(id, input)` (`PUT`), `deleteEdge(id)` for
  `/edges/{id}` (`pkg/api/handlers_edges.go`).
- `getAuditLog(options)` — `GET /v1/compliance/audit-log`.
- `getMaskingPolicy(tenant)` — `GET
  /v1/compliance/masking-policy/{tenant}`.
- `setMaskingPolicy(policy)` — `POST /v1/compliance/masking-policy`
  (admin-only; the target tenant comes from the caller's auth context).
- `listVectorIndexes()`, `createVectorIndex(input)`,
  `getVectorIndex(name)`, `deleteVectorIndex(name)` for
  `/vector-indexes` (`pkg/api/handlers_vectors.go`).
- A README example and test showing GraphQL `persons(limit, after)`
  cursor pagination (server-side since PR #585): `after` is the ID of
  the last item seen, a page shorter than `limit` is the last page,
  and `after` cannot be combined with `orderBy` or a non-zero `offset`.
  No client code change was needed — the existing generic `query()`
  method already sends this.

### Also changed (required to keep the package internally consistent)

- `GraphDBCache` (the KV cache wrapper): `getTrustScore`/
  `invalidateTrustScore` are removed along with the client method they
  wrapped; `getNode`/`invalidateNode`/`traverse`/`generateKey` take a
  numeric node id, matching the new `Node.id` type. `CacheConfig` drops
  `trustScoreTTL` and the already-unused `fraudTTL`.
- `examples/trust-score-worker.ts` is removed (built entirely around
  the removed methods). `examples/concept-graph-worker.ts` is updated
  for numeric ids, the `from_node_id`/`to_node_id` edge shape, and the
  `traverse()` response no longer carrying `paths`.

### Not in scope

`healthCheck()` and `getMetrics()` were not audited or changed. A
quick check while writing this changelog suggests `getMetrics()`'s
`GET /metrics` route serves Prometheus text exposition format, not
JSON, and the JSON metrics endpoint actually lives at the admin-only
`GET /api/metrics` with a materially different response shape — but
this was not verified to the same standard as the rest of this
document and neither method was touched. Flagging it for a follow-up.
