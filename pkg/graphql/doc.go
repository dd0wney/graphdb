// Package graphql exposes graphdb over GraphQL with per-tenant schema isolation.
//
// [GenerateSchemaForTenant] builds a schema scoped to a single tenant's labels
// so introspection cannot leak other tenants' metadata; [GenerateSchema] is the
// tenant-blind variant for single-tenant or CLI use. (Richer variants add
// mutations, filtering, aggregation, depth/complexity limits — see the
// GenerateSchemaWith* functions.) [NewGraphQLHandler] wraps a schema as an HTTP
// handler and threads the request context through to resolvers, which read the
// caller's tenant from it.
//
// Node and edge property values serialize as a JSON string built through the
// shared storage converter (storage.PropertiesToJSON), so arrays, nested
// objects, and null round-trip correctly.
package graphql
