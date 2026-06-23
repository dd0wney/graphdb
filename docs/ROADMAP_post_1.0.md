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

### v1.1.0 — Validate & observe
*Front-load the evidence that gates later releases; ship one easy adoption win.*
- **Real-corpus coi-screen validation** (~814K ICIJ): first end-to-end mmap exercise on a real
  consumer + product evidence + the empirical answer to decision **B-1** ("is full-graph
  enumeration a hot path?"). Output is a decision doc, not a feature flip.
- **Harden the JSON↔mmap equivalence oracle** to property-based / fuzzed coverage — the
  precondition for making mmap the default.
- **OpenTelemetry tracing** + SLO/SLI docs (additive; pairs with the metrics already shipped).
- CI: re-enable the disabled fuzz tests (`pkg/api/fuzz_test.go.disabled`, `pkg/query/fuzz_test.go.disabled`).
- **Gates:** none · **Size:** M · **Risk:** low

### v1.2.0 — mmap by default
*The headline single-node-scale win — gated on v1.1.*
- Flip **mmap-backed lazy reopen to the default** (JSON path stays as opt-out). Backward-
  compatible: the format is already versioned (`GMNP` v4) and the oracle is now property-based.
- Deploy-ordering / index-build operability docs.
- CI: bring `cmd/...` into the test allowlist.
- **Gates:** v1.1 (oracle hardening + real-consumer validation) · **Size:** S–M · **Risk:** medium (default-behavior change)

### v1.3.0 — Deploy anywhere
*Adoption unblock — independent of the perf spine.*
- **Helm chart + Terraform module** (the #1 "can't deploy on k8s" gap).
- First-party **Go-native client** (rounds out Python + TS).
- CI: `gofmt` lint gate.
- **Gates:** none · **Size:** M · **Risk:** low

### v1.4.0 — Finish the API surface
*Completes the additive API work carried since pre-1.0.*
- **GraphQL index-level pagination** (resolver offset→ID-cursor; the REST side landed in #366).
- **F3 compliance HTTP-API** surface (the framework exists in `pkg/compliance`; only the
  endpoints are missing).
- SDK parity (Python/TS/Go catch up to the new endpoints).
- **Gates:** none · **Size:** M · **Risk:** low

### v1.5.0 — Scale the read path *(conditional)*
*The deepest perf win — sequenced here because it's the hardest to keep backward-compatible.*
- **DoD Levers 2–3** (lazy property bag → ~3.6× on the 479ms full-enum residual; columnar SoA),
  done as **internal representation changes behind the existing public `*Node`/`Properties`
  types** so the 1.0 promise holds. **Only if v1.1 confirmed enumeration is a hot path.**
- If v1.1 says enumeration *isn't* hot → this release becomes additional ecosystem/connectors
  work instead.
- **Gates:** v1.1 decision · **Size:** L · **Risk:** medium (public-type blast radius — must stay internal)

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
v1.3 IaC + Go client                                              │  single-node
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
| **v1.3.0** | Deploy anywhere | Helm + Terraform, Go-native client | — | M |
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
