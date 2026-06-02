# Track R verification — auto-embed deployment exercise, component (1c) (2026-06-02)

## TL;DR

The in-server LSA auto-embed path was exercised in a real **container deployment** (`GRAPHDB_AUTO_EMBED_ENABLED=true`), not just unit tests, and proven **searchable end-to-end with correct nearest-neighbour ranking**. A `POST /nodes` create fires the `AutoEmbedObserver`, which computes an LSA vector, writes it back as a `TypeVector` via `UpdateNodeForTenant`, and the writeback lands in the per-tenant HNSW — so a subsequent `/vector-search` returns the right cluster on top. This **closes component (1c)**, the last verification gap in Track R: the track is now verified empirically across its full surface, not only structurally. The assertion is **ranking, not non-empty results** — the deliberate counter to the failure mode that hid #243 (the vector tests asserted "N results with valid IDs," never that the neighbours were the *nearest* ones).

## Context

`NEXT_STEPS_2026-05-15.md` § "What's NOT yet verified in production" lists three components of the Track R verification gap. (1a) per-tenant memory and (1b) backpressure are discharged (PRs #195/#209/#212 and #196/#202/#215). Component (1c) — *"a Docker / k8s deployment that exercises `GRAPHDB_AUTO_EMBED_ENABLED=true` in production-shaped traffic doesn't exist"* — remained, and was nominated as the next session's first task.

The same planning doc's § Reconciliation 2026-06-02 records why (1c) is *newly meaningful*: before #243 (HNSW recall 0.0→1.0) and #246 (REST `TypeFloatArray` ingest), a `GRAPHDB_AUTO_EMBED_ENABLED=true` run would have produced vectors that couldn't be ranked correctly, so (1c) would have validated only "the observer fired." With both fixes on `main`, the whole path works, so (1c) validates a real searchable result.

## Scope — what this exercises, precisely

This exercises the **in-server LSA auto-embed → HNSW → `/vector-search` deployment wiring, end-to-end, asserting correctly-*ordered* nearest neighbours.**

Two scope boundaries matter, and overstating either would recreate the very failure mode this arc closes (asserting a property at a regime that doesn't exercise it):

1. **It confirms ordering, not recall-at-scale.** The corpus is 6 searchable vectors — a trivially-navigable HNSW graph. #243's headline was recall "0.0 → 1.0 *at scale*"; small graphs did not exhibit that collapse. Of #243's three bugs, the heap-inversion and k-farthest ones corrupt *ordering* and the ranking assertion here would catch them at any scale; the recall-at-scale collapse needs scale this corpus is below. So **(1c) does not re-exercise #243's recall-at-scale property — that remains owned by #243's own `recall@10` test.** What (1c) adds is that the *deployment wiring* (env bootstrap → observer → writeback → HNSW insert → search) returns correctly-ordered results end-to-end, which no unit test covers.

2. **It does not exercise #246 (`TypeFloatArray` REST ingest).** The two write paths are distinct:

| Path | Who writes the vector | Property type written | Fix it depends on |
|---|---|---|---|
| In-server auto-embed (this doc) | `AutoEmbedObserver` writeback | `TypeVector` (`storage.VectorValue`) | (ordering correctness; recall-at-scale owned by #243's own test) |
| External neural ingest (already validated in `understand-graphdb`) | REST client `POST /nodes` | `TypeFloatArray` (JSON number array) | #246 (ingest) **+** #243 |

The observer writes `VectorValue` directly (`pkg/intelligence/auto_embed_observer.go:260`), so it bypasses the `TypeFloatArray` branch #246 added. Inflating this to "(1c) validates #246" would be inaccurate — #246's path is the *external* neural one, validated separately.

## The prerequisite chain (the real test surface)

The deployment exercise is valuable precisely because the in-server LSA path has ordering constraints that unit tests don't surface. Three were pinned by reading the code, then confirmed by running:

1. **Vector index must exist *before* any auto-embed writeback.** `VectorIndex.CreateIndexForTenant` (`pkg/storage/vector_index.go:80`) creates an **empty** HNSW — it does **not** backfill existing nodes. And `UpdateNodeVectorIndexes` (`pkg/storage/vector_operations.go:191`) silently `continue`s when no index exists for the property. So a writeback that fires before the index is created lands on the node but never enters HNSW → unsearchable. The harness creates the `embedding` vector index first.

2. **The LSA index must exist (with vocabulary) before traffic embeds.** `LSAEmbedder` reads the per-tenant LSA index built from *existing* nodes. On a fresh DB it's empty, so the observer drops early creates with `ErrNoIndexForTenant` (by design — no panic, no writeback). The harness therefore **seeds a training corpus first, builds the LSA index over it, then drives the traffic** that actually gets embedded. The observer holds the live `TenantLSAIndexes` registry, so a post-boot LSA build is picked up without restart.

3. **LSA is vocabulary-bound.** It's term co-occurrence, so the corpus must be **lexically** (not merely semantically) separable for ranking to be meaningful, and `vocab (T) ≥ Dims` or `BuildLSAIndex` errors (`pkg/search/lsa.go:307`). The harness uses two lexically-distinct clusters and `dims=8`, `min_doc_freq=1`.

Order, end to end: `create vector index → seed training corpus → build LSA → drive traffic → poll HNSW → query within a cluster → assert ranking`.

## Methodology

Artifacts (both committed):

- **`scripts/verify-track-r-1c-autoembed.sh`** — the driver. Two modes:
  - default: assert against `$BASE_URL` (used for the local-binary smoke).
  - `--docker`: build + `up` the compose deployment, assert against it, tear down on exit.
- **`docker-compose.track-r-1c.yml`** — runs the graphdb image (the real `Dockerfile`) with the auto-embed env vars, mapped to host port 8088.

Corpus: two lexically-distinct clusters — *ocean* (ocean, marine, whale, coral, reef, tide, current, plankton, seabed, fish) and *finance* (bank, mortgage, loan, credit, interest, deposit, capital, debt, equity, ledger, invoice). 8 training docs + 3 traffic docs per cluster. Query vectors are produced by `POST /v1/embeddings` (the *same* per-tenant LSA index the observer uses), so a query *string* maps into the same latent space as the auto-embedded doc vectors.

Assertion (per cluster): the in-cluster query's **#1 result is its own cluster**, and **≥2 of the top-3** are its own cluster. Ranking, not score thresholds (see § Arch-dependent scores).

## Results

Both runs passed all checks. LSA index: 16 docs, 8 dims. All 6 traffic vectors indexed in HNSW after the async pool drained (well under the 30s poll budget).

**Container deployment (`--docker`, amd64 image):**

```
ocean-query  : 17→0.992 ocean | 18→0.815 ocean | 19→0.415 ocean | 21→0.150 finance | 22→0.131 finance | 20→0.127 finance
finance-query: 21→0.899 finance| 20→0.732 finance| 22→0.377 finance| 17→0.175 ocean | 19→0.017 ocean | 18→-0.064 ocean
```

Both queries put their own cluster in all of the top 3, with a large margin (≈0.42–0.99 in-cluster vs ≈−0.06–0.18 off-cluster). The full pipeline ran inside the container: `POST /nodes` → async observer → LSA embed → `TypeVector` writeback → HNSW insert → `/vector-search`.

## Arch-dependent scores (and why the assertion targets ranking)

The native-arm64 local smoke gave off-cluster cosine scores that were **negative** (e.g. ocean-query finance docs ≈ −0.07); the amd64 container (under emulation on the arm64 host) gave the same docs ≈ **+0.13**. The randomized Halko SVD inside `BuildLSAIndex` is deterministic *given a fixed architecture* (`Seed:42`), but float/SIMD rounding differs between native arm64 and emulated amd64 → slightly different latent vectors → different absolute cosine scores. **The ranking is invariant** across both (top-3 all-correct-cluster, #1 correct). Asserting `score > 0.5` would have been architecture-fragile; asserting cluster *ordering* is robust. This is the same lesson as #243 at one remove: test the property that matters (are these the nearest neighbours?), not an incidental magnitude.

## What worked / what didn't

**Worked:**
- The env-driven bootstrap (`bootstrapAutoEmbedFromEnv`) wired the observer cleanly in-container; startup log confirmed `✅ Bootstrapped auto-embed observer (label="Doc", source="body", target="embedding", workers=4, queue=256)`.
- Post-boot LSA build (built *after* the empty-corpus boot) was picked up by the already-registered observer with no restart — the live-registry design holds in a real deployment.
- Async writeback drained fast; all traffic vectors were searchable within the poll budget.
- Ranking was crisply discriminated and stable across local and container runs.

**Didn't / gotchas (none are graphdb bugs):**
- `cmd/graphdb` is a **demo binary**, not the server — the server is `cmd/server` (`/app/cluso-server`, what the `Dockerfile` builds). Easy to trip on.
- macOS ships **bash 3.2** — no `declare -A`; the driver avoids associative arrays for portability.
- The Docker build logs `fatal: not a git repository` during `go build` (the `git describe` in `-ldflags` fails because `.git` is dockerignored). Harmless — `main.Version` just falls back to empty; not a (1c) concern but noted so a future reader doesn't chase it.
- The bootstrapping order is unforgiving by design: create the vector index and build the LSA index **before** the traffic you expect to be searchable, or the writeback silently no-ops (no index) / drops (no LSA). This is correct fail-soft behaviour, but an operator deploying auto-embed must know the order. Worth surfacing in customer-facing deployment docs (a productization-doc gap, not a code gap).

## How to reproduce

```bash
# One-command containerized exercise (build + up + assert + teardown):
scripts/verify-track-r-1c-autoembed.sh --docker

# Or against an already-running deployment:
docker compose -f docker-compose.track-r-1c.yml up -d --build
BASE_URL=http://localhost:8088 scripts/verify-track-r-1c-autoembed.sh
docker compose -f docker-compose.track-r-1c.yml down -v
```

## Consequence for the planning doc

Component (1c) is **discharged**. The Track R verification gap (1a + 1b + 1c) is now fully closed — Track R is verified empirically across its full surface. With all three components discharged, the planning doc's default-next shifts from (A) verification-gap to **(C) commission a new audit** (per `NEXT_STEPS_2026-05-15.md` § Decision 9). The one operational follow-up surfaced here is a **customer-facing deployment-ordering note** for auto-embed (create indexes before traffic) — a productization-doc item, not a code change.
