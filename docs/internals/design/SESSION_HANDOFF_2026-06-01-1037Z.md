# Session handoff — 2026-06-01 10:37 UTC

**Date**: 2026-06-01 (single long session, ~14 commits direct-to-`main`, no PRs — see §3 note)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

A performance investigation that started as "add SIMD to vector distance" and ended by **disproving its own premise**: the float32 cosine kernel is memory-bandwidth-bound on both arm64 and amd64, so SIMD buys ~3–5%. The evidence pivoted the track to **int8 quantization** (cut bytes, not math): ~3.6× arm64 / ~1.5× amd64 at negligible recall cost. Shipped to `main`: the value-typed HNSW heap (212→~37 allocs/op), Go 1.26 adoption (Gate 0 complete, CI-validated), and the SIMD→int8 evidence arc recorded in the roadmap. Next track (not started): **int8-quantized HNSW**.

## What's done this session

**Landed directly on `main` (local-merge + push, not via PRs — user's explicit choice this session).** Commits oldest-first:

| Commit | What | Notes |
|---|---|---|
| `af7dd81` | Value-typed HNSW `priorityQueue` (heap) | **212 → ~37 allocs/op (−82%)**, deterministic. Latency neutral (claim retracted after single-run noise). Replaced `container/heap` `any`-boxing. |
| `906a70a`/`4cd0a5f`/`68c0ea6` | SIMD perf roadmap + Gate-0 plan (+ archsimd-reality correction) | Brainstormed→planned. Corrected mid-stream when `go doc` revealed archsimd is experimental/amd64-only. |
| `536de38` | Throwaway archsimd smoke kernel (amd64) | Build-tag isolated (`amd64 && goexperiment.simd`); passed on real amd64 via Docker + CI. |
| `7e044a8`/`db52b74`/`ad1b7af` | Go 1.26 baseline → bump → post-bump deltas | **Green Tea GC within noise** on this workload/arch (honest, recorded). go.mod `1.25.3`→`1.26.0`. |
| `8419fa3` | CI: 1.26 across 9 pins (4 workflows) + smoke job | Dropped 1.23–1.25 matrix (1.26.0 floor). **Validated green on GitHub** (macOS matrix + smoke + coverage + build). |
| `f24e6e9` | **fix(docker): builder image 1.25→1.26-alpine** | The 1.26 bump broke Docker Publish (Dockerfile pin missed in the workflow sweep). Now green. |
| `7f4ae9e` | Roadmap outcome banner — SIMD shelved, pivot to int8 | Captures the full evidence arc (see §6). |

**Throwaway probe branches (deleted, findings captured in roadmap + §5 here):** `perf/hnsw-search-pooling`, `perf/phase1a-query-norm-hoist` (query-norm hoist — shelved, within-noise), `spike/int8-quantization` (int8 spike — the winning lever).

## Current state

- **`origin/main` HEAD**: `7f4ae9e`. 14 commits ahead of the prior handoff baseline (`7e28031`).
- **CI on `main`**: green except **Deploy Documentation to GitHub Pages** (failing *before* this session — pre-existing, not ours) and the Benchmarks comment-step (known-tolerated, `CLAUDE.md` § Known infra). Docker Publish **fixed** (`f24e6e9`). Tests/Lint/Coverage/Build/smoke validated green.
- **`go.mod` is now `go 1.26.0`** — 1.26 is the build floor. All CI workflows + both Dockerfiles bumped.
- **Open branches**: `feat/expose-label-mutation` (user's WIP, see below), `feat/expose-property-indexes-and-uniqueness`, an old `docs/session-handoff-2026-05-16-2337Z`. No perf branches (cleaned up).
- **Uncommitted**: `enterprise_tests_framework.go` is modified on `feat/expose-label-mutation` — that's the **user's pre-existing WIP**, stashed/restored repeatedly this session, NOT session output. Untouched.
- **Test/lint**: `pkg/vector` green incl. `-race` (concurrent-search + heap); `go vet` + `golangci-lint` clean on touched code. Full `pkg/storage` suite is slow (a `DiskBacked_CacheMiss` bench hangs ~20min — use `-run`/`-bench` filters).
- **Open PRs #240/#241** are the user's feature work (property-indexes, label-mutation); #238/#239 are stale May-20 handoff PRs.

## What's next

**The pivot target — int8-quantized HNSW (new track, not started).** The roadmap banner (`PERF_SIMD_ROADMAP_2026-06-01.md`, top) has the full rationale. Concretely:

- Store `int8` vectors + a `float32` scale + a `float32` norm per `hnswNode` (4× less memory **and** bandwidth in the search/insert distance loops — directly attacks the measured bottleneck).
- Portable scalar int8 dot kernel (gets the full ~3.6× on arm64; ~1.5× on amd64).
- Optional `archsimd` AVX512-VNNI int8 dot on amd64 (where scalar int8 is compute-bound) to push amd64 toward the memory-bound ~4×. This is SIMD's *correct* home — int8, not float32.
- Recall budget: the spike measured mean distance error 0.00016 (max 0.00075) @768 — validate end-to-end recall on a real index before committing.

**The throwaway spike kernel + recall harness (branch deleted — reproduce from here):**

```go
func quantizeInt8(v []float32) (q []int8, scale float32) {
    var maxAbs float32
    for _, x := range v { if a := float32(math.Abs(float64(x))); a > maxAbs { maxAbs = a } }
    if maxAbs == 0 { return make([]int8, len(v)), 0 }
    scale = maxAbs / 127.0
    q = make([]int8, len(v))
    for i, x := range v { q[i] = int8(math.Round(float64(x / scale))) }
    return q, scale
}
// dot in int32 over int8 lanes; float32 norms stored per node; ~3.6x arm64 / ~1.5x amd64
func cosineDistanceInt8(qa, qb []int8, scaleA, scaleB, normA, normB float32) float32 {
    if normA == 0 || normB == 0 { return 1.0 }
    var dot int32
    for i := range qa { dot += int32(qa[i]) * int32(qb[i]) }
    return 1.0 - (float32(dot)*scaleA*scaleB)/(normA*normB)
}
```

**Other open items (unchanged from prior planning):** `parallel_aggregation` is dead code (no callers) — candidate for deletion. `NEXT_STEPS_2026-05-15.md` option (C) "performance under realistic SaaS load" is now **consumed** by this track.

## Stale assumptions to retire

- **`PERF_SIMD_ROADMAP_2026-06-01.md` body (lines ~17 onward)** is the *pre-evidence* plan ("commit to Go 1.26 intrinsics", "Phase 1b SIMD float32 kernel", "Tier-1 latency-critical payload"). **Superseded by the OUTCOME banner at the top of the same file.** Read the banner; treat the body as decision-trail only.
- **The premise "SIMD will speed up vector distance"** is **false for float32** on this codebase: the cosine kernel is memory-bandwidth-bound (both arches; removing ⅔ of arithmetic → ~3–5%). SIMD-float32 is shelved. Do not re-propose it.
- **Auto-memory `project_goolang_target_workload` ("graphdb is the goolang SIMD dogfooding target")**: the vector *distance* kernel is the wrong dogfooding site for float32 SIMD (memory-bound). The valid SIMD opportunity is **int8 dot on amd64 (VNNI)**. Adjust the dogfooding framing accordingly.
- **`reference_graphdb_embedding_search_api` / any note implying HNSW search is compute-bound**: it's memory-bound at realistic embedding dims (768/1536). The lever is fewer bytes (quantization), not faster math.
- **`NEXT_STEPS_2026-05-15.md` § Critical-path option (C)** ("commission a new perf audit") — **done**: this session is that audit. The next planning doc should mark it consumed and point at the int8-HNSW track.
- **"Go 1.26 first-class SIMD is stable"** (implied by the original roadmap): it's `simd/archsimd`, **`GOEXPERIMENT=simd`-gated, amd64-only, outside the Go 1 compatibility promise**. Verified against `go1.26.3`. Any archsimd use needs the env flag threaded through build/CI/release and a scalar fallback.

## Open questions for the user

- **int8-HNSW recall budget**: the spike's 0.00016 mean distance error is on random vectors; real embeddings cluster differently. Is a small recall hit acceptable for 1.5–3.6× + 4× memory savings, or does this need a recall-preserving variant (e.g., int8 for candidate gen + float32 re-rank of top-k)?
- **Per-tenant memory**: int8 vectors are 4× smaller — this also improves the per-tenant HNSW footprint (the F4/Track-R concern). Worth folding the int8 work into that capacity story explicitly?
- **archsimd appetite**: shipping the amd64 int8 path means a production DB builds behind `GOEXPERIMENT=simd`. Acceptable, or keep amd64 on the portable scalar (~1.5×) and skip the experimental dependency?

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

## How to use this handoff

1. Read this first.
2. Read the OUTCOME banner at the top of `docs/internals/design/PERF_SIMD_ROADMAP_2026-06-01.md` (not the body).
3. `CLAUDE.md` § "Orient first" is auto-loaded.
4. If starting int8-HNSW: read `pkg/vector/distance.go`, `pkg/vector/hnsw_types.go` (the `hnswNode` struct), and `GATE0_GO126_RESULTS_2026-06-01.md` (the empirical numbers + repro).
