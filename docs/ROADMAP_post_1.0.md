# Roadmap: post-1.0 (graphdb) — the 1.0 → 2.0 arc

**Status:** proposal / living planning doc. Sketches the minor-version line from the
v1.0.0 GA to the next major (v2.0.0).
**Current release:** `v1.0.0` (2026-06-23, GA, GPG-signed).
**Companion:** [`ROADMAP_v1.md`](./ROADMAP_v1.md) defined what 1.0 means and the path to GA
(now complete). This doc picks up after it. The live per-track queue continues to live in
the dated `NEXT_STEPS_<DATE>.md` checkpoints.

> **Confidence decays with distance.** v1.1–v1.3 are grounded in the deferred/carry-forward
> inventory and prior spike work. v1.6 onward is **directional** — it *will* re-sequence once
> v1.1's corpus evidence and real adoption signal arrive. The durable value here is the
> **dependency spine** and the **SemVer discipline**, not the exact contents of v1.8.

---

## Governing constraint

v1.0.0 made a written compatibility promise ([`STABILITY_POLICY.md`](./STABILITY_POLICY.md)).
That sorts every post-1.0 item:

- **Minor (1.x)** — additive and **backward-compatible** only: new endpoints, opt-in flags,
  ecosystem, observability, and performance that keeps the on-disk formats versioned.
- **Major (2.0)** — anything that breaks a covered surface: clustering/HA, the `/v1`→`/v2`
  module path, the architecture refactors that change public Go/API surfaces, or an
  incompatible on-disk format change.

Each minor must ship **green and signed** on its own (the release pipeline established at
v0.8.0/#430) and must not depend on a later release.

## Strategic framing

The recommended theme for the 1.x line is **adoption + scale** — convert a GA-ready engine
into one that is deployed and fast — rather than internal-architecture-first (which is
invisible to users and risky under a stability promise). Internal v2 groundwork is threaded
through the *back* of the line (v1.9), not led with.

The one sequencing fork worth revisiting per-release: **validation-first** (the default below —
v1.1 evidence gates v1.2 and v1.5) vs **adoption-first** (ship Helm/SDKs/OTel immediately,
defer mmap-default). Validation-first is recommended because the default-flip and the perf
work are gated on the same corpus run; do it once, up front.

---

## The minor line (v1.1 → v1.9)

### v1.1.0 — Validate & observe ✅ **DONE (2026-07-01)**
*Front-load the evidence that gates later releases; ship one easy adoption win.*
- ✅ **coi-screen validation (#444)**: ICIJ-scale (~937K) mmap-vs-JSON measurement of the
  coi access pattern. **Answered B-1: NO — full-graph enumeration is not a coi-screen hot
  path.** coi resolves parties by label index + bounded adjacency BFS (mmap's cheap paths);
  enumeration is never on that path. mmap reopen is **~1370× cheaper** than JSON at scale
  (6ms vs 8.2s). Findings: `docs/internals/design/SPIKE_COI_SCREEN_VALIDATION_2026-07-01.md`.
  (Limitation: the real `../coi-screen` consumer repo isn't vendored here, so this validated
  the storage primitives it depends on, not the consumer binary — runbook in the findings doc.)
- ✅ **Property-based / fuzzed JSON↔mmap oracle (#440)** — raw-byte fingerprint + all-12-value-
  type parity + native `FuzzMmapReopenParity`. The precondition for mmap-default.
- ✅ **OpenTelemetry tracing + SLO/SLI docs (#442)** — env-configurable provider (off by
  default), HTTP root-span middleware, `docs/OBSERVABILITY.md`.
- ✅ **Re-enabled the disabled fuzz tests (#441)** — `pkg/api` rewired to the current API;
  the stale `pkg/query` `.disabled` cruft dropped (its live twin already existed).
- **Bonus (#445):** snapshot ship-and-serve hydration primitive validated (~7.4ms map at ICIJ
  scale, position-independent) → seeds the v2.0 cluster-bootstrap thread below.
- **Gates:** none · **Size:** M · **Risk:** low

### v1.2.0 — mmap by default ✅ **DONE (2026-07-01)**
*The headline single-node-scale win. Gate satisfied by v1.1 (property-based oracle #440 +
coi-screen validation #444).*
- ✅ **Flipped mmap-backed lazy reopen to the default (#447)** — `DefaultStorageConfig`
  `UseMmapSnapshot: true`; JSON path is the opt-out (`GRAPHDB_STORAGE_MODE=json` /
  `UseMmapSnapshot=false`). Backward-compatible: encrypted / disk-backed stores auto-fall-back
  to JSON, and existing `snapshot.json` stores migrate to `snapshot.mmap` on next snapshot.
  Opt-out inverted in lockstep across all three env readers so backup restore-mode detection
  stays consistent.
- ✅ **Deploy-ordering / operability docs (#447)** — `DEPLOYMENT_GUIDE.md` storage-mode section
  + CHANGELOG **Changed** entry.
- ✅ **`cmd/import-icij` mmap opt-in (#448)** — default mmap, `--storage-mode json` /
  `GRAPHDB_STORAGE_MODE=json` opts out; unblocks the coi-screen consumer runbook.
- ⏭️ **Carry-forward:** CI `cmd/...` test-allowlist expansion (separable; not done).
- **Gates:** v1.1 ✅ · **Size:** S–M · **Risk:** medium (default-behavior change) — *shipped; the
  full suite is green under the flipped default.*

### v1.3.0 — Deploy anywhere  🟡 **PARTIAL (2026-07-01)**
*Adoption unblock — independent of the perf spine.*
- ✅ **Helm chart + Terraform module (PR #450)** — single-node StatefulSet+PVC chart
  (`deployments/helm/graphdb`) with a `replicaCount>1` fail-guard (clustering is v2.0),
  values→ConfigMap/Secret, non-root uid 10001 + read-only rootfs, auto-generated
  `JWT_SECRET` persisted across upgrades, opt-in ingress/ServiceMonitor/PDB; a thin
  provider-agnostic Terraform `helm_release` wrapper (`deployments/terraform/graphdb`);
  `deploy-artifacts` CI (helm lint/template + terraform validate). Live-verified on kind
  (default + TLS + encryption); the live runs caught 6 runtime bugs static rendering missed.
  Additive/packaging-only (one Dockerfile uid pin). Design/plan under `docs/superpowers/`.
  - **⚠️ Release prerequisite:** the chart default targets `dd0wney/graphdb:1.2.0`; that
    image must be published (Docker Hub has only `1.0.0`/`latest`/`sha-*` today) or a
    default `helm install` won't pull.
- ✅ First-party **Go-native client (PR #458, 2026-07-17)** — `clients/go` (own module):
  retry/backoff transport with goroutine-safe 401-refresh (coalesced concurrent refreshes),
  token/API-key/login auth, Nodes/Edges/Search facets with `iter.Seq2` cursor pagination,
  Traverse/Query/GraphQL/Embeddings/Retrieve helpers, `Raw` escape hatch, sentinel errors.
  34 tests (~80% coverage) under `-race` in a dedicated least-privilege `go-client` CI
  workflow (which includes a `gofmt` gate for the module). Design/plan under `docs/superpowers/`.
- ⬜ CI: `gofmt` lint gate — **not started** (separate, trivial cycle).
- **Gates:** none · **Size:** M · **Risk:** low

### v1.4.0 — Finish the API surface
*Completes the additive API work carried since pre-1.0.*
- **GraphQL index-level pagination** (resolver offset→ID-cursor; the REST side landed in #366).
- **F3 compliance HTTP-API** surface (the framework exists in `pkg/compliance`; only the
  endpoints are missing).
- SDK parity (Python/TS/Go catch up to the new endpoints).
- **Gates:** none · **Size:** M · **Risk:** low

### v1.5.0 — ~~Scale the read path~~ → ecosystem/connectors *(v1.1 decided this)*
*The conditional resolved: **v1.1 (#444) found enumeration is NOT a hot path** for the
coi-screen workload, so the DoD-Levers track does not earn this slot.*
- **DoD Levers 2–3** (lazy property bag / columnar SoA optimizing the 479ms full-enum residual)
  are **deprioritized** — they optimize a path the validated consumer doesn't use. Revisit only
  if a *different* consumer surfaces full-graph enumeration as a genuine hot path.
- **Redirected optimization target (from #444):** the real coi cost is the label-bucket resolve
  scan (~262ms over 337K officers), not enumeration — so a **name/property index on the label
  bucket** is the higher-value read-path win if coi latency ever matters.
- This release becomes **additional ecosystem/connectors work** instead (see v1.8).
- **Gates:** v1.1 decision ✅ · **Size:** M (was L) · **Risk:** low (no longer a public-type refactor)

### v1.6.0 — Query maturity & developer experience
*Make the engine pleasant to use, not just fast.*
- **`EXPLAIN` / query-plan inspection** — productionize the `physical_plan.go` spike residuals
  (CallOperator, edge-direction) and expose a plan endpoint.
- Close remaining **Cypher coverage** gaps.
- **Admin-UI maturity** — replace remaining mocked/partial dashboard pages with real wiring
  (the backups page was found entirely mocked pre-1.0; audit the rest): query console, schema
  browser, tenant view.
- Replace the **coord-domain hardcoded uniqueness** with a generic uniqueness-rules registry
  (standing TODO in `pkg/graphql/mutations_resolvers.go`) — internal, additive.
- **Gates:** none · **Size:** M · **Risk:** low

### v1.7.0 — Backup, DR & data protection
*OSS-appropriate backup hardening (scheduling/retention/remote targets stay enterprise-plugin
territory per the v0.8.0 design).*
- **Archive encryption / signing** (follow-up scoped out of v0.8.0) — protect the sensitive
  `auth/` payload at rest.
- **Point-in-time restore** — WAL-replay-to-timestamp on the existing offline restore path.
- **Hot in-place restore endpoint** *(behind a flag)* — the live-swap track deferred from
  v0.8.0; needs a design spike + its own equivalence oracle first.
- **Gates:** live-restore needs a design spike · **Size:** M–L · **Risk:** medium

### v1.8.0 — Connectors & SDK completion
*Meet teams where their data already lives.*
- **Data-platform connectors** (§G): Kafka source/sink, CSV/Parquet ETL, lakehouse export, BI
  drivers.
- **Java + Rust SDKs** — complete the client matrix (Go/Python/TS exist after v1.3).
- **Gates:** none · **Size:** L (likely the longest single minor) · **Risk:** low

### v1.9.0 — Residuals + quiet v2 prep
*The last 1.x: finish the long tail and shrink v2.0's breaking surface from the inside.*
- **RAG/intelligence depth** — auto-embed R2.x re-entry (`pkg/intelligence` Track-R), reranking
  on hybrid search, larger corpora via external `/v1/embeddings`.
- **A5 `withTenant` DSL-search residual** — finish tenant-aware search scoping (multi-tenant
  correctness; `pkg/search/tenant_indexes.go`).
- **BTree backend** — complete C2.1 (`DeleteNode`/`BeginBatch`) + tombstone compaction, **or**
  formally retire the experimental backend.
- **Internal-only v2 groundwork (no public-surface change)** — introduce the Storage interface
  *behind* current types and incrementally decompose the `pkg/api.Server` god-struct, so v2.0
  is a smaller, safer breaking change.
- **Gates:** none ("stabilize before the major") · **Size:** L · **Risk:** low

---

## v2.0.0 — Beyond single-node

*The major: everything that breaks the 1.0 stability promise batches here.*

- **Clustering / HA** — wire (or formally remove) `pkg/cluster`; sharded write path; distributed
  consensus. (`pkg/cluster` is ~2,800 LOC, currently EXPERIMENTAL and not wired in.)
- **Snapshot-based replica hydration** *(new — seeded by #445)*. The mmap "cheap reopen" (~6ms)
  makes base-state hydration near-free: a new node maps a shipped `snapshot.mmap` and is
  read-ready in **~7.4ms at ~1M nodes**, byte-identically — the layout is position-independent
  (validated: `TestSnapshotHydration_FromShippedFile`, `SPIKE_SNAPSHOT_HYDRATION_2026-07-01.md`).
  Turning the primitive into a usable replica needs, in order: **delta-tail/freshness** (biggest
  gap — snapshot is point-in-time), snapshot distribution (the `r2-backup` plugin is a substrate),
  **encryption-in-mmap** (mmap is plaintext-only today), and the `pkg/cluster` join path. Fast
  hydration is a building block for the bootstrap story, not the cluster itself.
- **Breaking architecture refactors** that v1.9 could not do internally: Storage interface
  (arch HIGH-1), `pkg/api.Server` god-struct (HIGH-2), unified `TenantID` type, REST/GraphQL
  service-layer de-duplication (MED-1), `pkg/editions` global singleton.
- **Module path `/v1` → `/v2`** (if importing graphdb as a library becomes a supported,
  versioned API).
- Any **on-disk format change** that cannot be done compatibly under a single major.

---

## Dependency spine

```
v1.1 validate + harden oracle ─► v1.2 mmap-default ───────────────┐
                              └─► (informs) ─► v1.5 perf/DoD levers │
v1.3 IaC ✅ (#450) + Go client ✅ (#458)                          │  single-node
v1.4 GraphQL paging + F3 compliance API                           │  1.x line
v1.6 query/EXPLAIN + admin-UI + DX                                │  (all additive,
v1.7 backup/DR + encryption + PITR                                │   green, signed)
v1.8 connectors + Java/Rust SDKs                                  │
v1.9 residuals + INTERNAL v2-prep ───────────────────────────────┘
                                  ▼
v2.0 clustering/HA + breaking refactors + module /v2
```

Only v1.2 and v1.5 have hard gates (both on v1.1). Everything else is independently
schedulable; the order above reflects leverage, not a strict chain.

## Cut-line summary

| Release | Theme | Headline contents | Gate | Size |
|---|---|---|---|---|
| **v1.1.0** | Validate & observe | coi-screen corpus run, property-based oracle, OTel tracing | — | M |
| **v1.2.0** | mmap by default | flip mmap default (opt-out), deploy-ordering docs | v1.1 | S–M |
| **v1.3.0** 🟡 | Deploy anywhere | Helm + Terraform ✅ (#450); Go-native client ✅ (#458); repo-wide gofmt gate pending | — | M |
| **v1.4.0** | Finish the API | GraphQL pagination, F3 compliance API, SDK parity | — | M |
| **v1.5.0** | Scale the read path | DoD Levers 2–3 (internal), *conditional on v1.1* | v1.1 | L |
| **v1.6.0** | Query & DX | EXPLAIN/plan, Cypher coverage, admin-UI maturity | — | M |
| **v1.7.0** | Backup & DR | archive encryption, PITR, flagged hot in-place restore | spike | M–L |
| **v1.8.0** | Connectors & SDKs | Kafka/ETL/lakehouse/BI, Java + Rust SDKs | — | L |
| **v1.9.0** | Residuals + v2 prep | intelligence depth, A5/BTree residuals, internal refactors | — | L |
| **v2.0.0** | Beyond single-node | clustering/HA, breaking refactors, module `/v2` | v1.9 | XL |

**Cadence reality:** ~9 minors + a major is a multi-quarter-to-multi-year arc. The back half
will re-sequence as evidence and demand arrive; the value of writing it down now is the spine
and the additive-vs-breaking discipline, not the precise contents of the later minors.

## How to use this document

1. `ROADMAP_v1.md` is history (the path to GA, complete). This doc is the forward arc.
2. The live per-track queue is the dated `NEXT_STEPS_<DATE>.md` checkpoint — start there for
   what's actually in flight.
3. Re-derive v1.6+ after v1.1's corpus evidence lands; treat the near releases as a plan and the
   far ones as a sketch.
4. The bright line is SemVer: if a change breaks a surface in `STABILITY_POLICY.md`, it waits for
   v2.0 — otherwise it can ride a minor.
