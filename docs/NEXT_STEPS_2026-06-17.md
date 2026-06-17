# Plan: Next Steps (graphdb) — 2026-06-17

**Predecessor**: [`NEXT_STEPS_2026-06-03.md`](./NEXT_STEPS_2026-06-03.md) (the living doc that carried Tracks P/Q/S + the productization waves through 2026-06-12). This doc supersedes it.

**Why a fresh doc**: the dominant work since 06-03 — the **graphdb ask #1 "cheap reopen" track** (mmap Stages 1/2a/2b/2c + the JSON↔mmap equivalence oracle) — was driven by the ask-#1 brief, not by 06-03, and is absent from it. This checkpoint records that track and re-states the (still unforced) critical path. Per the repo convention, a new dated doc marks the new checkpoint.

**`main` HEAD at write time**: `3806d09` (#414).

---

## State reconciliation

### Ask #1 — "cheap reopen of a large persisted store" ✅ **SHIPPED (Stages 1–2c)**

Reopening a ~937k-node / 1.3M-edge persisted store cost ≈ a cold rebuild (~14.4s), defeating persistence. A flag-gated, mmap-backed lazy-reopen storage mode (`StorageConfig.UseMmapSnapshot` / `GRAPHDB_STORAGE_MODE=mmap`, **off by default**; JSON path unchanged) fixed it in stages:

| Stage | What | PRs | Result |
|---|---|---|---|
| Spike + Stage 1 | reopen-cost investigation; mmap snapshot format (v2) + lazy-reopen wiring | #408, #409, #410 | reopen **14.4s → 2.9s** (0.18× rebuild) |
| 2a | persist CSR adjacency + lazy membership + per-tenant counts in metadata (format v3) | **#412** | reopen **→ ~7ms** (eager index field-scan gone from open) |
| 2b | persist membership inverted indexes (format **v4**); delete the 2a lazy build | **#413** | membership first-enumeration **~2s → ~11ms** |
| 2c | skip the redundant `Clone` on the mmap-base read path | **#414** | full-graph first enumeration **1.165s → 479ms** (residual is node materialization, not index cost) |

Design/measurement trail: `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` (Stages 1/2a/2b) + `SPIKE_DOD_MATERIALIZATION_2026-06-17.md` (2c + the DoD levers). Per-stage spec/plan under `docs/superpowers/`.

**Correctness gate**: a standing **JSON↔mmap public-interface equivalence oracle** (`fingerprintTenant` / `assertFingerprintEqual`, `pkg/storage/mmap_reopen_test.go`) — an mmap-reopened store must enumerate byte-identically to the same store via JSON, across reopen / writes / batch / WAL-replay / second-reopen. (`checkGraphInvariants` does NOT work in mmap mode.)

### Ask #1 follow-on — equivalence-oracle hardening (in review)

The oracle was a fixed Person/Org fixture signing only `name` + outgoing `to:weight`. Hardened to sign **all labels + full property bags + outgoing AND incoming edges (type/weight/props)** for every node, plus a **randomized-fixture** parity test (`TestMmapReopen_RandomizedParity`). **It immediately caught a real mmap≠JSON soundness divergence** (a node with duplicate labels double-counted in the mmap membership index vs deduped in JSON) — fixed in the same PR. **PR #417** (open).

## Current state

- **`origin/main` HEAD**: `3806d09` (#414) — the full Stage-2 stack (#412/#413/#414) is merged.
- **Open PRs**:
  - **#417** — oracle hardening + the dup-label membership fix (the soundness fix; merge after CI).
  - **#418** — CLAUDE.md accuracy (v4 format doc + 37→42/29→24 counts + this doc as the orient-first pointer).
  - **#419 (this doc)** — the planning checkpoint.
- **Uncommitted changes**: none.
- **Test/lint**: `pkg/storage` `-short` + `-race` green; `go build ./pkg/... ./cmd/...` + `go vet` clean. (`golangci-lint` couldn't run locally — go1.25-vs-1.26.4 toolchain gate; CI runs it.)

### Gated follow-ups surfaced by the ask-#1 work (none promoted to critical path)

- **DoD Levers 2–3** (lazy property bag → ~3.6× on the 479ms full-enum residual; columnar SoA) — `SPIKE_DOD_MATERIALIZATION_2026-06-17.md`. **Gated** on an open question: is full-graph `GetAllNodesForTenant`-on-reopen a real consumer hot path? If bounded/by-label, those queries are already ~0 (Stage 2b) and Levers 2–3 aren't worth their `*Node`/`Properties`-public-type blast radius.
- **`DeleteAllNodes` mmap-awareness bug** — pre-existing (Stage 1), out of scope for #412–#414; leaves the base mapped so deleted-all survives reopen. **Tracked: issue #416.**
- **Harden the oracle further before mmap-default**: the randomized fixture is a strong start; consider property-based/fuzzed coverage before flipping mmap on for any consumer. mmap is off-by-default, so no consumer is exposed today — the gate is "before mmap-default," not "before landing."
- **mmap + encryption / mmap + `UseDiskBackedEdges`**: still fall back to JSON. A page/segment-decrypt path would be needed to combine mmap with at-rest encryption.

## Critical path — none forced (carry-forward candidates)

Consistent with 06-03's closing state: no audit-driven track is currently earned. The ask-#1 reopen track was the most recent earned work and is now shipped. Candidates, none promoted:

- **Real-corpus coi-screen Milestone-1-proper** (the ~814K ICIJ corpus run, deferred in 06-03 Q3 for lack of a local corpus) — likeliest source of new evidence; would also exercise mmap mode end-to-end on a real consumer (the validation 2a–2c haven't had).
- **Productization / operability** (06-03 item 7's second wave) — customer-facing onboarding docs remain sparse (a standing gap).
- **GraphQL index-level pagination** (carried from 06-03).
- **Batched-WAL default sweep** (carried from 06-03 Decision carry-forward).
- **CI hygiene** (carried): `cmd/...` packages outside the CI test allowlist; `golangci-lint` config doesn't flag `gofmt` violations.
- **mmap validation + (if hot-path confirmed) DoD Levers 2–3** — see gated follow-ups above.

## Decision points

- **Open (for the user): is full-graph enumeration-on-reopen a consumer hot path?** Gates DoD Levers 2–3.
- **Open: should mmap mode become a default** (or per-deployment opt-in) now that open + index lookup are ~0? Today strictly opt-in via env. Recommended precondition: harden the equivalence oracle to property-based coverage + validate on a real consumer first.

## How to use this document

1. Read this first, then `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` + `SPIKE_DOD_MATERIALIZATION_2026-06-17.md` for the ask-#1 arc.
2. `06-03` remains the reference for Tracks P/Q/S detail + the productization/security waves (carried, unchanged).
3. No critical path is forced — pick from the carry-forward candidates, or resolve a decision-point above to earn one.
