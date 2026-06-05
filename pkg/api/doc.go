// Package api serves graphdb's graph store over HTTP — the REST surface plus the
// GraphQL endpoint.
//
// The entry type is [Server], constructed with [NewServer] or
// [NewServerWithDataDir]. It wires per-tenant isolation middleware, JWT and
// API-key authentication, and handlers for node/edge CRUD, traversal and graph
// algorithms, vector and full-text/hybrid search, GraphQL, OpenAI-compatible
// embeddings, graph-augmented retrieval, and compliance audit logging.
// Per-tenant GraphQL schemas are built lazily and cached (concurrent cold
// starts deduped via singleflight).
//
// The full HTTP contract is described by docs/internals/openapi.yaml. Auth is
// fail-closed: JWT_SECRET must be set, and an admin user is bootstrapped on
// first start (see ADMIN_PASSWORD).
package api
