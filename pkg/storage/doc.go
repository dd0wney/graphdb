// Package storage is graphdb's core graph store: a multi-tenant, in-memory
// property graph with durable snapshot + write-ahead-log (WAL) persistence.
//
// The entry type is [GraphStorage], created with [NewGraphStorage]. Nodes and
// edges are addressed by uint64 IDs and carry typed [Value] properties
// (construct them with [StringValue], [IntValue], [JSONValue], etc.).
//
// # Tenant scoping
//
// Every operation has a tenant-blind form (e.g. [GraphStorage.CreateNode],
// [GraphStorage.GetNode]) and a tenant-strict *ForTenant form (e.g.
// [GraphStorage.CreateNodeForTenant], [GraphStorage.GetNodeForTenant]). New
// code should use the *ForTenant methods. A cross-tenant lookup returns
// [ErrNodeNotFound] / [ErrEdgeNotFound] — the same error as a genuinely missing
// entity — so a tenant cannot probe another tenant's data via error shape.
//
// # Internals
//
// Nodes and edges live in 256-way partitioned shard maps with per-shard read
// locks plus a global write lock for the cross-cutting indexes (label/type,
// per-tenant enumeration, property, and per-tenant HNSW vector indexes). State
// is persisted as a flat snapshot plus a WAL that is replayed on open;
// structured property values round-trip through [ValueFromJSON] / [ValueToJSON].
//
// See the runnable examples for the basic create/traverse flow and the
// tenant-isolation contract.
package storage
