# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Multi-tenancy support with data isolation and quota enforcement
- Generic OIDC authentication support for enterprise identity providers
- Modularity calculation for community detection algorithms (ConnectedComponents, LabelPropagation)
- Query optimizer property index usage for WHERE equality conditions (O(n) to O(1))
- EdgeDeletionRate calculation from temporal edge tombstones (valid_to timestamps)
- Automatic version detection from Go module build info in licensing client
- Plugin config loading from environment variables (PLUGIN_<NAME>_<KEY>=<VALUE>)
- Node listing endpoint in replica nodes (/nodes GET)
- Datacenter link parsing in ZMQ and NNG primary nodes
- License key checksum validation (SHA256-based, backward compatible with legacy keys)
- Snapshot transfer handling foundation in replica replication
- Primary mode transition support with PromotionCallback for HA failover

### Performance
- Zero-allocation Contains() for compressed edge lists (sequential scan with early termination)

### Fixed
- Goroutine leaks in OIDC StateStore and server metrics subsystems
- Memory leak in JWKS client key cache (unbounded growth on key rotation)
- Goroutine leaks in crash simulation tests (LSM worker cleanup)
- Go vet errors in benchmark and example code

## [0.6.0] - 2026-06-17

graphdb ask #1 — **cheap reopen of a large persisted store**. A flag-gated,
mmap-backed lazy-reopen storage mode (`GRAPHDB_STORAGE_MODE=mmap` /
`StorageConfig.UseMmapSnapshot`, **off by default**; the JSON path is unchanged)
takes reopen of a ~937k-node / 1.3M-edge store from ~14.4s to near-instant.

### Added
- mmap-backed lazy-reopen storage mode: binary `snapshot.mmap` format (magic `GMNP`, version 4) mapped at open with nodes/edges/indexes served lazily. Reopen ~14.4s → ~7ms; membership-index first-enumeration ~2s → ~11ms; full-graph first enumeration 1.165s → 479ms. Off by default; plaintext-only and in-memory-adjacency-only (encrypted stores and disk-backed edges fall back to the JSON path). (#408–#410, #412–#414)
- Persisted CSR adjacency + per-tenant membership inverted indexes in the mmap format, served via copy-on-read accessors merging the immutable base with the post-open overlay minus tombstones.
- Standing JSON↔mmap public-interface equivalence oracle (`fingerprintTenant`): an mmap-reopened store must enumerate byte-identically to the same store via JSON, across reopen / writes / batch / WAL-replay / second-reopen, with a randomized-fixture parity test. (#417)

### Fixed
- mmap membership writer double-counted a node with duplicate labels (e.g. `["Person","Person"]`) — its ID was written into a label run once per occurrence, while the in-memory index dedups, so `GetNodesByLabelForTenant` returned the node twice in mmap vs once in JSON. Found by the hardened equivalence oracle. (#417)

## [0.3.0] - 2025-02-18

### Added
- GraphQL DoS protection with query complexity validation and rate limiting
- Comprehensive vector search endpoint tests
- Vector search API documentation and OpenAPI spec updates

### Security
- Query complexity limits to prevent resource exhaustion attacks
- Request rate limiting for GraphQL endpoints

## [0.2.0] - 2025-02-17

### Added
- Vector search with HNSW indexes for semantic similarity queries
- GraphQL subscriptions with pub/sub system for real-time updates
- Full-text search with inverted indexes
- DataLoader pattern for N+1 query optimization
- Query depth limiting and result limits for GraphQL
- REST API handlers and middleware
- Graph algorithms: cycle detection and topological sort
- Data import tool with CSV/JSON support
- Storage layer enhancements: compression, batched WAL
- Logical operators (AND/OR/NOT) in GraphQL filtering
- Aggregation queries (count, sum, avg, min, max)
- Sorting/ordering support in GraphQL queries
- Cursor-based pagination
- Edge CRUD operations in GraphQL

### Security
- JWT-based authentication system
- API key authentication for admin endpoints
- Enterprise-grade audit logging
- Storage encryption with CLI management tools
- Comprehensive input validation and sanitization

### Changed
- Migrated replication to NNG/mangos with SRP refactoring
- Split schema.go into modular files for maintainability

### Fixed
- Replace panics with error returns for production safety

## [0.1.0] - 2025-02-10

### Added
- Core graph storage engine with LSM trees
- Node and edge CRUD operations
- Property storage with typed values
- Label and type indexes
- GraphQL API with queries and mutations
- Pagination support for large result sets
- Basic filtering in GraphQL queries
- Write-ahead logging (WAL) for durability
- Crash recovery from WAL replay
- Disk-backed adjacency lists with LRU cache
- Replication support with primary/replica architecture
- Docker support with multi-architecture builds
- GitHub Actions CI/CD pipeline
- Benchmarking workflow for performance testing

### Performance
- 5x memory reduction through optimized data structures
- 100x concurrency improvement
- 650x faster LSM read performance

[Unreleased]: https://github.com/dd0wney/graphdb/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/dd0wney/graphdb/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/dd0wney/graphdb/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dd0wney/graphdb/releases/tag/v0.1.0
