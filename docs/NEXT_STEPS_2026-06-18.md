# Plan: Next Steps (graphdb) — 2026-06-18

**Predecessor**: [`NEXT_STEPS_2026-06-17.md`](./NEXT_STEPS_2026-06-17.md) (recorded the ask-#1 "cheap reopen" track at the point its Stage-2 stack had just merged but the three follow-on PRs were still open). This doc supersedes it.

**Why a fresh doc**: 06-17 was written mid-flight — it lists #417/#418/#419 as open. They (and #420) have since merged and **v0.6.0 is cut**. This checkpoint reconciles to that reality, corrects a stale-audit hazard (below), and recommends the next track. Per repo convention, a new dated doc marks the new checkpoint.

**`main` HEAD at write time**: `20bd767` (#420, the v0.6.0 CHANGELOG).

---

## State reconciliation

### Ask #1 — "cheap reopen" ✅ **SHIPPED and RELEASED (v0.6.0)**

The full arc is merged. mmap-backed lazy-reopen storage mode (`StorageConfig.UseMmapSnapshot` / `GRAPHDB_STORAGE_MODE=mmap`, **off by default**; JSON path unchanged):

| Stage / follow-on | What | PR(s) | Result |
|---|---|---|---|
| Spike + Stage 1 | reopen-cost spike; mmap snapshot format (v2) + lazy-reopen wiring | #408, #409, #410 | reopen **14.4s → 2.9s** |
| 2a | persist CSR adjacency + lazy membership + per-tenant counts (format v3) | #412 | reopen **→ ~7ms** |
| 2b | persist membership inverted indexes (format **v4**); drop the 2a lazy build | #413 | membership first-enum **~2s → ~11ms** |
| 2c | skip the redundant `Clone` on the mmap-base read path | #414 | full-graph first-enum **1.165s → 479ms** |
| Oracle hardening | sign all labels + full property bags + in/out edges; randomized-fixture parity test; **caught + fixed a real dup-label mmap≠JSON soundness divergence** | #417 | correctness gate strengthened |
| Docs | CLAUDE.md v4-format documentation + scale-drift fixes | #418 | — |
| Checkpoint | the 06-17 planning doc | #419 | — |
| Release | v0.6.0 CHANGELOG | #420 | **v0.6.0 cut** |

Design/measurement trail: `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` (Stages 1/2a/2b) + `SPIKE_DOD_MATERIALIZATION_2026-06-17.md` (2c + the DoD levers).

**Correctness gate**: the standing **JSON↔mmap public-interface equivalence oracle** (`fingerprintTenant` / `assertFingerprintEqual` + `TestMmapReopen_RandomizedParity`, `pkg/storage/mmap_reopen_test.go`). An mmap-reopened store must enumerate byte-identically to the same store via JSON. (`checkGraphInvariants` does NOT work in mmap mode.)

### ⚠️ Stale-audit hazard — do not re-open closed findings

A scan of the 2026-05-06 audit docs (`AUDIT_{security,performance,architecture,code_quality}_2026-05-06.md`) will surface CRIT/HIGH findings that **read as open in the audit text but are closed in code** (Tracks A/A4/P/S landed between 05-06 and 06-12). Verified against current code on 2026-06-18:

| Audit finding | Status | Evidence |
|---|---|---|
| SEC CRIT-2 — `listNodes` dumps all tenants | **closed** | `pkg/api/handlers_nodes.go:64-101` routes through `NodesPageForTenant` / `NodesByLabelPageForTenant` |
| SEC HIGH-4 — JWT secret randomizes off-prod | **closed** | `pkg/api/server_init.go` fail-closes: `JWT_SECRET … is required` |
| PERF HIGH-1 — `GetNode` always clones | **closed** | no-clone `getNodeRefForTenant` path, `pkg/storage/node_operations.go:399` |
| PERF HIGH-2 — global `mu` serializes node reads | **closed** | reads take `rlockShard(nodeID)`, `node_operations.go:327,357` (A4 partitioned shard maps) |

Per CLAUDE.md ("trust the code, then surface the discrepancy"): the audit docs are historical. Verify against code before treating any 05-06 finding as outstanding. Items genuinely still open from those audits are listed individually below (e.g. PERF HIGH-3 batched-WAL default; the A5 `withTenant` residual).

## Current state

- **`origin/main` HEAD**: `dfbac2a`. v0.6.0 released; since this doc was written: the file-split refactor landed (#424), and the **v0.7.0 production-hardening track shipped (#427)** closing ROADMAP blockers **B1/B2/B3/B6** (see [`ROADMAP_v1.md`](./ROADMAP_v1.md)).
- **Open PRs**: none.
- **GA roadmap**: [`ROADMAP_v1.md`](./ROADMAP_v1.md) (#426) defines v1.0 scope + blockers. v0.7.0 done; **v0.8.0** (B4 hot backup/restore, B5a JSON snapshot header) and **v1.0.0** (B5b stability policy, docs) remain.
- **mmap mode**: shipped, **off by default**, no consumer exposed.

## Genuinely-outstanding inventory (reconciled)

### A — In-flight / quick wins — ✅ both landed
- **Land `fix/goreleaser-drop-windows`** — ✅ **DONE (#421, `5b97e3f`)**: dropped `windows` from all three goreleaser builds + the Windows `format_overrides`.
- **#416 — `DeleteAllNodes` mmap-awareness bug** — ✅ **DONE (#423, `0ccb976`)**: `DeleteAllNodes` now unmaps the mmap base (mirroring `Close()`) before snapshotting, so a delete-all is real in memory and across reopen instead of silently re-persisting the base. Root cause was deeper than the issue title — in mmap mode the op was a no-op that rewrote the full old graph. Gated by a new JSON↔mmap parity test (`TestMmapReopen_DeleteAllNodesClears`).

### B — Open decisions gating further mmap work (carried from 06-17)
- **B-1**: is full-graph `GetAllNodesForTenant`-on-reopen a real consumer hot path? → gates DoD Levers 2–3.
- **B-2**: should mmap become a default / per-deployment opt-in now that open + index lookup are ~0? → precondition: property-based/fuzzed equivalence oracle + validation on a real consumer.

### C — Gated mmap follow-ups (`SPIKE_DOD_MATERIALIZATION_2026-06-17.md`)
- **DoD Lever 2** (lazy property bag → ~3.6× on the 479ms full-enum residual) and **Lever 3** (columnar SoA) — both gated on B-1; both carry `*Node`/`Properties` public-type blast radius.
- Harden the oracle to property-based coverage before mmap-default.
- mmap + encryption and mmap + `UseDiskBackedEdges` still fall back to JSON (would need a page/segment-decrypt path).

### D — Carry-forward candidate tracks (none forced)
- **Real-corpus coi-screen Milestone-1-proper** (~814K ICIJ run; deferred for lack of a local corpus). See recommendation below.
- **Productization / operability Wave 2** — customer-facing onboarding docs (standing gap), ~~single-node-limitation framing~~ (✅ done #427: cluster marked EXPERIMENTAL + single-node stated in README/CAPABILITIES, ROADMAP B6), deploy-ordering note (create indexes before traffic).
- **GraphQL index-level pagination** — REST side done (#366); resolver offset→ID-cursor change remains.
- **Batched-WAL default sweep** — ✅ **RESOLVED #427 (ROADMAP B3 / PERF HIGH-3)**: the `FlushInterval` sweep measured batched WAL **13× slower** than per-write fsync on fast NVMe, so the default was kept as per-write fsync (strongest durability + fastest on local storage) and documented; batching stays opt-in for slow/networked disk. Not a flip — the data inverted the assumption.
- **CI hygiene** — `cmd/...` packages outside the CI test allowlist; `golangci-lint` config doesn't flag `gofmt`; re-enable disabled fuzz tests (`pkg/api/fuzz_test.go.disabled`, `pkg/query/fuzz_test.go.disabled`).

### E — Known code residuals (lower priority)
- **A5 `withTenant` hardening** — endpoints fall back to the default tenant when no context is set; tenant-aware DSL search not scoped (`pkg/search/tenant_indexes.go:20`, `pkg/api/middleware_tenant.go:124`, `pkg/api/handlers_nodes.go:69`). Single-tenant deployments are unaffected.
- **BTree backend C2.1 partial** — `DeleteNode` / `BeginBatch` unimplemented (`pkg/storage/btree_storage.go:486,555`); experimental alt backend.
- **Coord-domain hardcoded uniqueness constants** → generic uniqueness-rules registry (`pkg/graphql/mutations_resolvers.go:19`, mirrored at `pkg/api/handlers_nodes.go:19`).
- **Query `physical_plan.go` spike residuals** — CallOperator (C3.x / C6 co-land), edge-direction hardcoded.
- btree tombstone compaction + random eviction; intelligence auto-embed R2.x re-entry.

### F — Long-horizon architecture (deferred; real but low priority)
- Storage interface (arch HIGH-1), `pkg/api.Server` god-struct (HIGH-2), REST/GraphQL service-layer duplication (MED-1), `pkg/editions` global singleton, unified `TenantID` type.

### G — Productization / commercial gaps (`CAPABILITIES_2026-05-10.md`)
- Client SDKs (Python/Java/Rust); IaC (Helm chart / Terraform provider / operator); observability (OTel tracing, SLO/SLI docs); data-platform connectors (Kafka, ETL, lakehouse export, BI drivers); commercial packaging (pricing/support/roadmap); F3 compliance HTTP-API surface; the 4 documented-but-unbuilt enterprise plugins (`ENTERPRISE_PLUGINS.md`).

## Recommended next track

**Real-corpus coi-screen Milestone-1-proper (~814K ICIJ).** It's the single pick that *converts open questions into evidence* instead of guessing:

1. likeliest source of new product evidence (deferred only for lack of a local corpus);
2. exercises mmap mode **end-to-end on a real consumer** — the validation Stages 2a–2c never had — which is the explicit precondition for decision **B-2** (mmap-as-default);
3. empirically answers decision **B-1** (is full-graph enumeration a hot path?), which gates DoD Levers 2–3.

One track, three unblocks. **Alongside (cheap, do regardless):** land the goreleaser branch; fix **#416**.

Consistent with 06-03/06-17: **no critical path is forced.** This is a recommendation — pick from the candidates above, or resolve a decision point to earn a track.

## Decision points (open, for the user)

- **B-1**: is full-graph enumeration-on-reopen a consumer hot path? Gates DoD Levers 2–3.
- **B-2**: should mmap mode become a default? Recommended precondition: property-based oracle + real-consumer validation first (which the recommended track delivers).

## How to use this document

1. Read this first. For the ask-#1 arc detail, `SPIKE_REOPEN_COST_2026-06-16.md` + `SPIKE_DOD_MATERIALIZATION_2026-06-17.md`.
2. `NEXT_STEPS_2026-06-03.md` remains the reference for Tracks P/Q/S + the productization/security waves (carried, unchanged).
3. Before acting on any 2026-05-06 audit finding, re-verify against code — see the stale-audit hazard table above.
4. No critical path is forced — take the recommended track, another candidate, or resolve a decision point.
