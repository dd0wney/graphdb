# Killer-Features: Architect Analysis
**Date**: 2026-05-08
**Scope**: `github.com/dd0wney/cluso-graphdb` — what the current shape enables, forecloses, and what one unlock buys.

---

## 1. What the Package Layout Enables

The FTS + LSA + HNSW + RRF stack in `pkg/search/` and `pkg/vector/` is architecturally unusual: most graph databases ship zero or one of these signals; graphdb ships four with a working fusion layer (`pkg/api/handlers_hybrid_search.go`, RRF constant at line 51). This directly enables:

**GraphRAG / LLM retrieval over graphs.** The hybrid search endpoint is already the retrieval seam. Adding a streaming result writer and a context-window packer on top of `handleHybridSearch` is days of feature work, not rework. No competitor in the sub-5K-node Go graph DB space ships this natively.

**Semantic graph snapshots.** `pkg/search/lsa.go` fixes seed at 42 by default, making document vectors deterministic per-corpus-hash. Batch-loaded knowledge bases (Syntopica model) can cache embeddings keyed on WAL LSN — no rebuild on read, cheap invalidation on write. The seam is `BuildLSAIndex`.

**Privacy-annotated graphs.** `pkg/masking/`, per-tenant property_filter work (recent commits), per-tenant indexes in `storage_types.go` (`tenantNodesByLabel`, `tenantEdgesByType`), and the persistent audit log (`pkg/audit/`) are all live. The remaining work is integration story and documentation, not net-new capability.

---

## 2. What the Layout Forecloses Without Rework

**Distributed transactions / MVCC.** `pkg/storage/transaction_types.go` implements pending-ops over shared in-memory maps — no versioned row storage, no read snapshot isolation. Adding true ACID distributed transactions requires a fundamentally different storage engine. This is months, not weeks.

**Real-time incremental semantic search.** `pkg/search/lsa.go` constraint 1 (line 22): the LSA model is not incremental — any vocabulary shift requires a full rebuild. For corpora with continuous writes (stream ingestion, CDC), the semantic layer degrades silently until the next rebuild. Algorithmic rework, not refactor.

**Shared-nothing multi-tenancy.** `storage_types.go` implements logical isolation (tenant-keyed sub-maps inside shared structures). If a consumer (SOC2 audit, BYOC, regulated data) demands physical process or storage separation per tenant, the architecture does not support it without splitting `GraphStorage` into per-tenant instances — which cascades through every `pkg/api` handler.

**Stable binary plugin distribution.** `pkg/plugins/loader.go` uses Go's native `plugin` package. Known hard constraints: same Go toolchain version required at build and load time, Linux/macOS only, plugins cannot be unloaded, no cross-compilation. Commercial distribution of `.so` files is fragile — a customer Go toolchain upgrade breaks plugins silently.

**Coherent one-button HA.** `pkg/cluster/` (Raft-flavored election, `election.go`) and `pkg/replication/` (NNG+ZMQ WAL streaming, `nng_primary.go`, `zmq_primary.go`) coexist without unification. Features requiring a single coherent distribution story must first resolve which stack leads.

---

## 3. The Smallest Architectural Unlock

**Extract a `StorageReader`/`StorageWriter` interface from `*storage.GraphStorage`.** This is bounded and mechanical: the god-struct surface is large but the set of methods each consumer actually calls is narrow (verifiable by grepping each handler file). The unlock chain is concrete:

1. **Plugin sandboxing.** `pkg/plugins/features.go` defines `StoragePlugin.AttachToStorage(*storage.GraphStorage)` — the plugin receives the entire god-struct. A narrow interface breaks that; plugins get only the contract they need and can't call methods that bypass tenant isolation.

2. **Capability-layer feature gating.** `pkg/editions/features.go` is a hard-coded `map[Edition]map[Feature]bool` — feature gating is a config flag, not a capability gate. With a storage interface, Enterprise variants can implement additional interface methods (e.g., `VectorizeSearch()`) that Community storage simply does not expose. That is real open-core; the current arrangement is cosmetic.

3. **Tiered backends.** Cold tenants (no recent reads) could be routed to a disk-only `StorageReader` without touching the hot path. Impossible today without touching `GraphStorage` directly.

4. **REST/GraphQL parity.** REST handlers (`pkg/api/`) and GraphQL resolvers (`pkg/graphql/`) both take `*storage.GraphStorage` directly with no shared service layer. A service layer — preconditioned on a storage interface — eliminates the split brain where the same graph operation is implemented twice.

5. **Distributed transactions precondition.** Not sufficient alone, but a Transactor interface cannot be introduced meaningfully while every caller holds the concrete struct.

This was priority-one in the prior audit and remains so. It is not research; it is extraction.

---

## 4. Plugin/Editions Split: Real or Cosmetic?

**Currently cosmetic, three provable reasons:**

First, `pkg/editions/features.go` is a static `map[Edition]map[Feature]bool`. There is no capability check — calling `editions.IsEnabled(FeatureVectorSearch)` returns a boolean, but the HNSW index is always constructed in `storage.go:NewGraphStorageWithConfig` regardless. The gate is advisory, not enforced.

Second, `pkg/plugins/loader.go` uses `plugin.Open()` from the Go standard library. The `.so` distribution model (documented in `docs/ENTERPRISE_PLUGINS.md`) cannot survive a customer toolchain upgrade, cannot run on Windows, and cannot be unloaded at runtime. This is too fragile for commercial use at scale.

Third, `StoragePlugin.AttachToStorage` takes `*storage.GraphStorage` directly (`docs/ENTERPRISE_PLUGINS.md`, line 95). Any plugin that attaches gets the full internal surface — no narrowing, no sandboxing, no audit boundary.

To become a real open-core business, the path is: (a) extract the storage interface (Section 3), (b) replace Go plugins with a gRPC-over-Unix-socket sidecar model or embed a WASM runtime, and (c) move feature gating to the capability layer. That is 2-4 sprints of focused work, not a rearchitecture.

---

## Frame for Domain Specialists

Filter every candidate feature proposal through this question: **does it require writes through a storage interface, multi-backend storage, MVCC/snapshot isolation, distributed transactions, or shared-nothing tenancy?** If yes, it is gated on the storage interface extraction and is months away regardless of feature complexity. Features that compose the existing FTS+LSA+HNSW+RRF stack, ride the live per-tenant indexes, or extend the masking/property-filter/audit privacy stack are buildable in the current shape in weeks. The highest-probability killer feature is GraphRAG-style LLM retrieval — it rides all four existing search signals, no competitor in this weight class ships it, and the code seam (`handleHybridSearch`) already exists. The market specialists should price that and the perf specialists should benchmark it against Weaviate and Qdrant hybrid search at the 100K-node scale that is graphdb's realistic current ceiling.
