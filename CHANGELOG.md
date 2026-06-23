# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Release signing.** From v0.8.0, release artifacts are GPG-signed; see
> [`docs/RELEASE_SIGNING.md`](docs/RELEASE_SIGNING.md) to verify a download.

## [Unreleased]

The following are present in the codebase but not yet part of a tagged release
(several relate to the EXPERIMENTAL, not-wired clustering/replication path — see
`pkg/cluster/doc.go`):

### Added
- Multi-node/replication groundwork: node listing on replicas (`/nodes` GET), datacenter-link parsing in ZMQ/NNG primaries, snapshot-transfer handling, and primary-mode transition with `PromotionCallback` for HA failover
- Generic OIDC authentication support for enterprise identity providers
- Modularity calculation for community detection (ConnectedComponents, LabelPropagation)
- Query optimizer property-index usage for WHERE equality conditions (O(n) → O(1))
- EdgeDeletionRate from temporal edge tombstones (valid_to timestamps)
- Automatic version detection from Go module build info in the licensing client
- Plugin config loading from environment variables (`PLUGIN_<NAME>_<KEY>=<VALUE>`)
- License key checksum validation (SHA256-based, backward compatible)

### Performance
- Zero-allocation `Contains()` for compressed edge lists (sequential scan with early termination)

### Fixed
- Goroutine leaks in OIDC StateStore and server metrics subsystems
- Memory leak in JWKS client key cache (unbounded growth on key rotation)
- Goroutine leaks in crash-simulation tests (LSM worker cleanup)
- `go vet` errors in benchmark and example code

## [1.0.0] - 2026-06-23

**General Availability.** GraphDB 1.0 is a production-hardened, **single-node**
graph database. As of this release the REST/GraphQL APIs and the on-disk
snapshot/WAL/backup formats are covered by a written compatibility promise — see
[`docs/STABILITY_POLICY.md`](docs/STABILITY_POLICY.md). Breaking changes to a
covered surface now require a major version bump.

This GA consolidates the production-hardening (0.7.0) and operability (0.8.0)
work shipped since 0.6.0; it adds no new engine features beyond the stability
commitment itself.

### Added
- Written API & on-disk format **stability policy** ([`docs/STABILITY_POLICY.md`](docs/STABILITY_POLICY.md)), effective at 1.0. (#434)
- **Getting Started** onboarding guide ([`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md)). (#435)
- GPG-signed releases with a published signing key (`KEYS`, keys.openpgp.org). (#430–#432)

### Changed
- Declared **single-node** scope for 1.0; clustering (`pkg/cluster`) remains EXPERIMENTAL and not wired in.
- Backfilled the CHANGELOG (0.4.0–0.8.0) and corrected README quickstart accuracy (`JWT_SECRET` is required). (#433, #435)

## [0.8.0] - 2026-06-22

**Operability — hot backup, verifiable restore, signed releases.** ROADMAP
blocker **B4**. (B5a — the versioned JSON snapshot envelope — was already shipped
in 0.5.0 as M-14.)

### Added
- `POST /admin/backup` (admin-only) streams a snapshot-consistent `.tar.gz` (snapshot + `wal/` + `auth/` + `lsa/` (+ `edgestore/`) + manifest) without stopping the server. (#429)
- Per-file backup integrity: a versioned manifest envelope recording `manifest_version` + `{path, size_bytes, sha256}` per file, emitted as a trailer so each hash describes exactly the archived bytes. (#429)
- `pkg/backup` leaf package + `graphdb-admin backup verify <archive>` (offline integrity check) and `graphdb-admin backup restore --into <dir> [--dry-run] [--force]` (verifies integrity and snapshot-mode compatibility before extracting; zip-slip guarded). (#429)
- Backup observability metrics: `graphdb_backup_total{result}`, `graphdb_backup_duration_seconds`, `graphdb_backup_size_bytes`. (#429)
- GPG-signed releases: artifacts carry detached `.asc` signatures; the public key is published in `KEYS`, `docs/RELEASE_SIGNING.md`, as a release asset, and on keys.openpgp.org. (#430, #431, #432)
- Admin-UI backups page wired to the real `/admin/backup` endpoint (replaced the previously mocked page) + offline-restore instructions. (#429)

### Fixed
- Release workflow signing repaired: removed a malformed `gpg --quick-add-uid` call that failed the release job even with a valid key, and guarded the signing steps on the key being configured. (#430)
- `pkg/backup` archive reads bounded with `io.CopyN` (gosec G110 — decompression-bomb hardening). (#429)

## [0.7.0] - 2026-06-19

**Production hardening.** ROADMAP blockers B1/B2/B3/B6. Shipped to `main`
(#427/#428); rolled into the v0.8.0 release tag rather than tagged standalone.

### Security
- **B1** — tenant-safe delete: added `DeleteAllNodesForTenant`; `DELETE /nodes` now clears only the caller's tenant via the per-node cascade instead of every tenant's data. (#427)

### Changed
- **B2** — graceful shutdown now drains: `http.Server.Shutdown(ctx)` then `graph.Close()`, stopping the listener and exiting as soon as in-flight requests finish (no more fixed 30s sleep). (#427)
- **B3** — WAL durability default decided by measurement: batched WAL benchmarked ~13× slower than per-write `fsync` on NVMe, so per-write fsync stays the default (batching remains opt-in for slow/networked disks), now documented. (#427)
- **B6** — clustering scoped out of 1.0: `pkg/cluster` marked EXPERIMENTAL / NOT WIRED, single-node stated in README + CAPABILITIES, with an import-guard test preventing `cmd/server`/`pkg/api` from importing it. (#427)
- Defined the path to GA in `docs/ROADMAP_v1.md`. (#426)
- Split oversized source files into focused same-package siblings. (#424)

### Fixed
- `DeleteAllNodes` drops the mmap base so a delete-all is real in memory and across reopen (mmap mode). (#423)
- Dropped Windows from the goreleaser build matrix (mmap uses Unix-only syscalls). (#421)

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

## [0.5.0] - 2026-06-16

**Security re-audit hardening (Track S).** Closes the 11 High / 16 Medium / 10
Low backlog from the 2026-06-10 security re-audit (#371), across Waves 1–3.

### Security
- Versioned snapshot envelope (`GSNP` magic + version + flags) replacing the `data[0] != '{'` heuristic, plus construction-time encryption (M-14). (#395)
- WAL entry payloads encrypted through the snapshot engine (H-3). (#399)
- WAL checkpoint compaction purges deleted-tenant remanence (M-1) + GDPR Art-17 immediate tenant-erasure control. (#396, #393)
- Token revocation via a per-user generation counter + an admin revoke endpoint (M-7). (#390, #398)
- SHA-256 plugin manifest verification before `plugin.Open` (M-15, OSS side). (#397)
- Tenant-status enforcement + tenant-ID override validation (H-1, M-5); rate limiting on by default (H-5); owner-only at-rest files + WAL record-size cap (H-2, H-4); HNSW/traversal result caps (H-7, H-8); GraphQL depth/body/audit caps (M-3, M-4, M-16); request context honored in heavy algorithms (H-6). (#372–#387)
- Toolchain pinned to go1.26.4 to close stdlib vulnerabilities (H-9). (#374)

### Fixed
- TS/Python SDK hardening: path-segment encoding, cache identity namespace, idempotent-only retries, proxy handling, and keeping plaintext API keys out of representations (M-13, M-11, H-11). (#378–#380)
- CI: migrated actions to Node-24 majors + scoped workflow permissions; retired the tolerated benchmark-comment-step failure. (#368)

## [0.4.1] - 2026-06-05

### Changed
- Renamed the Go module `github.com/dd0wney/cluso-graphdb` → `github.com/dd0wney/graphdb`. (#335)

### Added
- First-party Python SDK (`clients/python`) — milestone M1. (#326, #327)
- `PUT`/`DELETE /edges/{id}` + SDK `edges.update`/`edges.delete`. (#332)

### Fixed
- Reject non-finite edge weights (±Inf/NaN) at the API boundary. (#334)
- Gate property-index `Remove`/`Insert` on type across the update and delete paths (no partial apply). (#321, #324)
- WAL-log vector-index create/drop for crash durability. (#320)

## [0.4.0] - 2026-06-04

**Tenant-isolation and durability hardening.**

### Added
- Durable, index-consistent `Transaction.Commit` built on a new atomic batch-WAL primitive. (#279, #280)

### Security
- Tenant-isolation sweep: gate `/api/metrics` admin-only (#300), scope `/api/v1/tenants/{id}` with `withTenant` (#301), scope GraphQL aggregate-schema discovery to the requesting tenant (#295), and rename tenant-blind helpers to `*AcrossTenants` (#302).

### Fixed
- Maintain per-tenant indexes across the batch/bulk-import and cascade-delete paths; rebuild the vector index and edge adjacency across restart. (#287, #288, #298, #304, #305, #307)
- Consumer-contract regression harness + metamorphic/invariant test suites for storage write paths (Tracks P/Q). (#291, #310, #311, #314)

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

[Unreleased]: https://github.com/dd0wney/graphdb/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/dd0wney/graphdb/compare/v0.8.0...v1.0.0
[0.8.0]: https://github.com/dd0wney/graphdb/compare/v0.6.0...v0.8.0
[0.7.0]: https://github.com/dd0wney/graphdb/compare/v0.6.0...b6eefef
[0.6.0]: https://github.com/dd0wney/graphdb/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dd0wney/graphdb/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/dd0wney/graphdb/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/dd0wney/graphdb/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/dd0wney/graphdb/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/dd0wney/graphdb/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/dd0wney/graphdb/releases/tag/v0.1.0
