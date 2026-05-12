# F1.1 — Per-tenant LSA: design spike + go/no-go

**Date**: 2026-05-10
**Author**: Claude Opus 4.7 (1M context)
**Coord claim**: `:Claim` node 45 against `:Task` 2 (`graphdb:F1.1-spike`)
**Status**: complete; recommended go/no-go in §5
**Filename per planning-doc spec**: `NEXT_STEPS_2026-05-10.md:108` named this file

---

## TL;DR

Per-tenant LSA already shipped on 2026-04-20 (commit `cf57251` `feat: add LSAIndex.TopKByVector + TenantLSAIndexes (v2 prep)`, paired with `d7f74d5` for the tenant-scoped admin build endpoint). The planning-doc framing of F1.1 as "Not started" with a "live cross-tenant leak" is stale; verification (§2) shows isolation works.

**Recommendation: NO-GO on F1.1-impl as originally scoped.** What remains is a small cleanup PR (§5).

---

## §1 — Discovery: per-tenant LSA already shipped

The planning-doc framing assumed F1.1 was greenfield work fixing a live cross-tenant leak. The code disagrees.

### Evidence

| Layer | File:line | Per-tenant since |
|---|---|---|
| Registry | `pkg/search/tenant_lsa_indexes.go` (whole file) | 2026-04-20 (`cf57251`) |
| Build endpoint | `pkg/api/handlers_search_admin.go:130-153` calls `s.graph.GetNodesByLabelForTenant(tenantID, label)` | 2026-04-20 (`d7f74d5`) |
| Read path | `pkg/api/handlers_embeddings.go:118-119` resolves `tenantID := getTenantFromContext(r); lsa := s.lsaIndexes.Get(tenantID)` | same |
| Bootstrap helper | `pkg/api/server_init.go:333-378` `buildAndRegisterLSA(tenantID, ...)` is tenant-parameterised | same |
| Hybrid search | `pkg/search/search_hybrid.go:78` `SearchHybridForTenant(searchIdx, lsaIdx *TenantLSAIndexes, tenantID, ...)` | same |
| Public API doc | `docs/API.md:693` "A drop-in OpenAI-compatible endpoint backed by the **per-tenant** LSA index" | as-shipped |

### Stale-doc inventory

The planning-doc and code each carry stale claims that this spike's cleanup PR (§5) should retire:

| File:line | Stale claim | Reality |
|---|---|---|
| `pkg/search/lsa.go:31-32` | "Not tenant-scoped. Mirrors the current posture of FullTextIndex in this package. Per-tenant isolation is a follow-up PR for both indexes together." | Per-tenant follow-up PR shipped same day in `cf57251`. Comment is 3 weeks stale. |
| `docs/NEXT_STEPS_2026-05-10.md:53` | "F1.1: per-tenant LSA — Not started. The multi-tenant caveat documented in F1 is still live." | F1.1 effectively shipped 2026-04-20. No multi-tenant caveat exists in the documented `/v1/embeddings` trade-offs (`docs/API.md:744-762`). |
| `docs/NEXT_STEPS_2026-05-10.md:108-114` | F1.1 spike contract describes designing per-tenant build trigger, storage cost, migration path — premised on F1.1 being unbuilt | Per-tenant infra exists. The remaining questions narrow to operational ergonomics, not architecture. |
| `docs/NEXT_STEPS_2026-05-10.md:217` | "F1.1 storage cost may invalidate per-tenant LSA. … The spike must produce a memory model, not a hand-wave." | Storage model produced in §4. Cost is real but per-tenant LSA is **not** invalidated for typical scale; ceiling matches the existing single-tenant LSA scale ceiling already documented at line 248. |
| `docs/NEXT_STEPS_2026-05-10.md:241` | "Cross-tenant embedding leakage is documented as a caveat in shipped F1, but the documented hole is reachable today by any customer running multi-tenant `/v1/embeddings`" | Verification (§2) shows the hole is not reachable. Tenant-B with no LSA gets 503; no fallback to tenant-A's index. |

The pattern matches the project's `CLAUDE.md` advisory: *"When the planning doc and the code disagree, trust the code, then surface the discrepancy to the user."*

---

## §2 — Verification: tenant isolation actually works

### Test executed

`TestEmbeddings_TenantIsolation` in `pkg/api/handlers_embeddings_test.go:371`.

**Result on 2026-05-10**: PASS in 0.08s.

**Setup**: tenant-A is seeded with an LSA index over a small corpus; tenant-B is left without one.

**Assertions**:

1. Tenant-A baseline: `FoldQuery("graph")` succeeds against tenant-A's index.
2. Tenant-B `/v1/embeddings` request → **503** (no LSA built; no fallback to tenant-A's index — this is the load-bearing isolation signal).
3. Tenant-A `/v1/embeddings` request → **200** with valid embedding response.

### Code path traced

```
POST /v1/embeddings (tenant from JWT/header context)
  → handlers_embeddings.go:118  tenantID := getTenantFromContext(r)
  → handlers_embeddings.go:119  lsa := s.lsaIndexes.Get(tenantID)   // returns nil if absent
  → handlers_embeddings.go:120-124  if lsa == nil → 503 with admin-endpoint pointer
  → handlers_embeddings.go:147  vec, tokens, err := lsa.FoldQuery(input)
                                  // FoldQuery operates only on this LSAIndex's vocab/Q^T,
                                  // which were built from tenant-scoped corpus.
```

```
POST /hybrid-search/lsa-index (build, also tenant-from-context)
  → handlers_search_admin.go:128  tenantID := getTenantFromContext(r)
  → handlers_search_admin.go:130-134  for label in req.Labels:
                                       nodes += s.graph.GetNodesByLabelForTenant(tenantID, label)
                                       // tenant-scoped corpus assembly
  → handlers_search_admin.go:173  idx, err := search.BuildLSAIndex(docs, cfg)
  → handlers_search_admin.go:184  s.lsaIndexes.Set(tenantID, idx)
                                  // registered under requesting tenant's key
```

The corpus assembled at build time is tenant-scoped via `GetNodesByLabelForTenant`. The index served at read time is keyed by the request's `tenantID`. There is no path that mixes tenants.

### Test gap noted

`TestEmbeddings_TenantIsolation` proves the *registry* isolates tenants (B can't access A's index). It does **not** prove *vocabulary-level* isolation — i.e. that a term present only in tenant-A's corpus doesn't somehow appear in tenant-B's. That property is structural (each tenant's `LSAIndex` builds its own `vocab map[string]int32` from its own `[]Document`), but a defensive assertion that names two disjoint tenants and pins distinct `vocab` keysets would be cheap insurance. **§5 cleanup includes adding that assertion.**

---

## §3 — Residual gaps

What's actually missing, in priority order:

### G1. Bootstrap envvar covers default tenant only

`server_init.go:286-330` `bootstrapIndexesFromEnv` builds an FTS and/or LSA index for `defaultTenantID = "default"` only when `GRAPHDB_LSA_BOOTSTRAP_LABELS` is set.

```go
const defaultTenantID = "default"
// …
if err := s.buildAndRegisterLSA(defaultTenantID, labels, titleProp, bodyProps); err != nil {
    log.Printf("bootstrap: LSA build for default tenant failed: %v", err)
}
```

**Implication**: any deployment with multiple tenants must build per-tenant indexes by calling `POST /hybrid-search/lsa-index` for each tenant after boot. There is no env-driven fan-out (e.g. `GRAPHDB_LSA_BOOTSTRAP_TENANTS=acme,corp,bigco`).

**Fix shape (small)**: extend `bootstrapIndexesFromEnv` to read a comma-separated `GRAPHDB_LSA_BOOTSTRAP_TENANTS` (defaulting to just `"default"` for back-compat) and loop `buildAndRegisterLSA(t, labels, titleProp, bodyProps)`. ~20 LOC. Same labels/properties config across tenants — caller is opting into "all my tenants share the same content shape," which is the typical SaaS deployment.

**Fix shape (large)**: per-tenant config block (different labels per tenant) — defer; not justified by current customer signal.

### G2. No auto-trigger on tenant-create or first-query

A new tenant's `/v1/embeddings` call returns 503 until an admin explicitly builds. There's no:

- Auto-build on first `/v1/embeddings` request (lazy)
- Auto-build on first `CreateNodeForTenant` for indexed labels (eager)
- Scheduled rebuild on a cadence (drift)

**Decision deferred**: this is operational UX, not safety. Manual trigger is the right default for an LSA build that takes seconds-to-minutes; auto-trigger surprises are worse than 503 with a clear message. Revisit if customers report the 503 as friction.

### G3. Stale source comments

- `pkg/search/lsa.go:31-32` claims "Not tenant-scoped. … follow-up PR." Wrong since 2026-04-20.
- `pkg/search/lsa.go` header should reference `tenant_lsa_indexes.go` for callers wondering about tenant scoping.

### G4. Stale planning-doc claims

Inventory in §1 above. `NEXT_STEPS_2026-05-10.md` lines 53, 108, 217, 241 misrepresent the state.

### G5. Test gap

`TestEmbeddings_TenantIsolation` covers registry isolation; doesn't cover vocab-disjoint isolation. Adding ~15 lines makes the spec defensive and self-documenting.

---

## §4 — Storage model

Per-tenant `LSAIndex` (struct at `pkg/search/lsa.go:73-89`):

| Field | Shape | Bytes (typical) |
|---|---|---|
| `dims` | int | 8 |
| `vocab` | `map[string]int32`, T entries | T × ~14 (key + value + overhead) |
| `idf` | `[]float32`, T | 4T |
| `b` | `[][]float32`, l×T (l ≈ Dims+Oversamp+1 ≈ 211) | 4 · l · T ≈ 844T |
| `ub` | `[][]float32`, l×k (k = Dims = 200) | 4 · l · k ≈ 168 KB |
| `docVecs` | `[][]float32`, D×k | 4 · D · 200 = 800D |
| `nodeIDs` | `[]uint64`, D | 8D |
| `nodeIDMap` | `map[uint64]int`, D | ~24D |
| `content` | `map[uint64]string`, D entries | ~16 + B per doc, where B = avg body bytes |
| `bm25Post` | `map[string][]bm25Entry` | ~T × avg-DF × 16 |
| `bm25Dlen` | `[]int`, D | 8D |

**Defaults**: `Dims=200`, `MaxVocab=8000`, `MinDocFreq=2`.

### Per-tenant footprint, typical workload

For a tenant with **D = 10,000 documents**, **T = 8,000 vocab** (capped), **B = 5 KB** average body:

| Field | Cost |
|---|---|
| `b` | 6.7 MB (840 × 8000) |
| `docVecs` | 8 MB (800 × 10000) |
| `content` | 50 MB (10000 × 5KB) ← **dominant** |
| `bm25Post` (estimated DF ≈ 100) | 12.8 MB (T × 100 × 16) |
| `vocab` + `nodeIDMap` + `idf` + `bm25Dlen` + `nodeIDs` + `ub` | ~0.5 MB |
| **Total per tenant** | **~78 MB** |

### Multi-tenant scaling

Linear in N (tenants):

| N tenants | D=10K each | D=50K each | D=200K each |
|---|---|---|---|
| 10 | ~0.78 GB | ~3.9 GB | ~15.6 GB |
| 100 | ~7.8 GB | ~39 GB | ~156 GB |
| 1000 | ~78 GB | (impractical) | (impractical) |

### Comparison to documented ceiling

`docs/NEXT_STEPS_2026-05-10.md:248`: *"LSA scale ceiling (~100K-500K docs at 200 dims). Documented in F1 internal docs but not at the README/positioning layer."*

That ceiling is per-LSA-instance (one tenant, or aggregated old-style). Per-tenant LSA preserves this ceiling **per tenant** rather than across the deployment, which is actually a scaling improvement: a 100-tenant deployment with 100K docs each is feasible even though a single-tenant 10M-doc index is not, because each tenant's index stays under the 500K ceiling.

The trade-off is RAM: 100 tenants × 50K docs ≈ 39 GB resident. For deployments above this footprint, the existing API.md guidance ("consider an external embedding service") applies — and the OpenAI-shape `/v1/embeddings` endpoint plus BYO-vector vector-search APIs already support that drop-in.

### Storage-cost concern from planning-doc:217 — resolved

The planning-doc concern was: *"N tenants × 200-dim × vocabulary memory could push small-deployment users over a footprint they accepted in F1."*

**Resolution**: the dominant cost is **per-document content storage** (for `DocSnippet`), not vocabulary. A small deployment (≤10 tenants, modest doc counts) lands at <1 GB resident. The vocabulary axis is bounded by `MaxVocab=8000` and contributes <10% of per-tenant footprint.

The original concern's framing ("N × 200-dim × vocabulary") was the wrong axis. The right axis is **N × D × avg-content-bytes**, which is documented and operator-tunable (lower `MaxVocab`, configure shorter snippet content, or omit `content` storage if `DocSnippet` is unused — all out-of-scope-but-tractable optimizations).

---

## §5 — Go/no-go

### NO-GO on F1.1-impl as originally scoped

The framing assumed greenfield work. The framing is wrong. Per-tenant LSA is in production.

Rejecting the original F1.1-impl ticket releases ~2-5 days of misallocated work and returns the planning queue to the next track (F3 — Compliance API).

### GO on F1.1-cleanup (single-PR scope)

Recommended single PR, ~50-100 LOC + ~1 KB of doc edits:

1. **`pkg/search/lsa.go:22-32`** — rewrite the third "live constraint" bullet. Replace "Not tenant-scoped … follow-up PR" with a short pointer to `TenantLSAIndexes` (`pkg/search/tenant_lsa_indexes.go`) explaining that tenant scoping is provided at the registry layer above this struct.

2. **`docs/NEXT_STEPS_2026-05-10.md`** — three targeted edits:
   - Line 53 (Track F summary table): F1.1 status change from "❌ Not started" to "✅ Shipped 2026-04-20 (`cf57251`)".
   - Line 108 (Track F detail): replace the spike contract with a closure note pointing at this design doc.
   - Line 217 (Risks): remove or reframe the "F1.1 storage cost may invalidate" risk; storage model produced (§4) and ceiling characterised.
   - Line 241 (Productization gaps): remove the "cross-tenant embedding leakage … reachable today" bullet; verification (§2) refutes it.

3. **`pkg/api/handlers_embeddings_test.go`** — add ~15-line assertion to `TestEmbeddings_TenantIsolation` (or as a sibling test) pinning that two tenants with disjoint corpora produce disjoint `vocab` keysets. Closes G5.

4. **`pkg/api/server_init.go`** — optionally extend `bootstrapIndexesFromEnv` to honour `GRAPHDB_LSA_BOOTSTRAP_TENANTS` (~20 LOC). Closes G1. Defer if it stretches the cleanup PR; can land separately.

5. **`docs/API.md`** — no change needed; per-tenant claim already accurate.

### Defer (out of cleanup scope)

- **Auto-trigger on tenant-create or first-query** (G2). Customer-friction-driven; revisit on signal.
- **Per-tenant bootstrap config blocks** (large variant of G1 — different labels per tenant). Not justified.
- **`content` map omission for memory savings**. Would require API contract change for `DocSnippet`. Defer.
- **Scheduled rebuild on cadence**. Operational; not architectural.

### Coord-claim disposition

After this PR merges, the cleanup work satisfies F1.1's effective intent. The `:Claim` for `graphdb:F1.1-spike` should be released and the Task flipped to `done` per the work-claim skill's "Releasing the claim" section. F1.1-impl as a separate Task should be retired (not just closed — its premise is invalid).

---

## Acceptance trace

This document satisfies the spike contract at `docs/NEXT_STEPS_2026-05-10.md:108-114`:

> Specify per-tenant LSA model build trigger (lazy on first semantic-search request? eager on tenant create?), storage cost (N tenants × 200-dim × vocabulary), migration path for existing single-LSA deployments. Explicit go/no-go recommendation at the end.

| Asked | Where answered |
|---|---|
| Build trigger | §3.G2 (manual today; auto-trigger deferred) |
| Storage cost | §4 (model with numbers + multi-tenant table) |
| Migration path | Implicit in §1 — there was never a single-LSA *deployment*; per-tenant shipped before any release. No migration needed. |
| Go/no-go | §5 (no-go on impl; go on cleanup PR) |

## What this spike does NOT do

- **Doesn't write the cleanup PR.** That's a separate piece of work. This is the design output that *justifies* the cleanup.
- **Doesn't audit non-LSA tenant scoping** (search/FTS, hybrid-search) — out of scope; track F1.1 is LSA-only.
- **Doesn't propose API changes.** `/v1/embeddings`, `/hybrid-search/lsa-index`, and `/hybrid-search` retain their current contracts.
- **Doesn't address the architectural ceiling.** ~100K-500K-doc-per-tenant ceiling stands; `/v1/embeddings` BYO-vector path exists for above-ceiling use cases.
