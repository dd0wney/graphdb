# int8-quantized HNSW — design

**Date**: 2026-06-01
**Status**: Approved (brainstorming complete) — ready for implementation plan
**Track**: Performance (the int8 pivot from the shelved SIMD-float32 track; see
`docs/internals/design/PERF_SIMD_ROADMAP_2026-06-01.md` OUTCOME banner)
**Predecessor evidence**: `GATE0_GO126_RESULTS_2026-06-01.md`,
`SESSION_HANDOFF_2026-06-01-1037Z.md`

---

## 1. Goal & success criteria

Cut the **memory bandwidth** consumed by the HNSW distance hot loop — the
empirically-measured bottleneck — by storing each indexed vector as `int8`
(plus a `float32` scale and a `float32` norm) instead of `[]float32`. SIMD on
float32 was shelved because the float32 cosine kernel is memory-bandwidth-bound
(~3–5% from removing ⅔ of the arithmetic); the lever is **fewer bytes**, not
faster math.

**Success criteria:**

- **Throughput**: ~3.6× search/insert on arm64 @768 dims; ~1.5× on amd64 with
  the scalar path (amd64 is compute-bound at int8, addressed by the SIMD path).
- **Memory**: 4× smaller per-vector footprint in the in-memory index on all
  arches.
- **Recall (the gate)**: recall loss on **real embeddings** small enough to
  accept. The spike measured mean distance error 0.00016 (max 0.00075) over
  2000 trials @768 on *random* vectors; this must be re-confirmed on clustered
  real embeddings before the change is committed (§6).
- **Correctness**: the amd64 SIMD kernel produces bit-identical dot products to
  the portable scalar kernel on all inputs (§7).

**Non-goals (this phase):** int8 candidate-gen + float32 re-rank (documented
fallback only); true VNNI `VPDPBUSD` (not present in the toolchain — see §5);
any on-disk format change (none required — see §2).

---

## 2. Storage representation

`hnswNode` (in `pkg/vector/hnsw_types.go`) changes from a single `float32`
slice to the quantized triple:

```go
type hnswNode struct {
    id      uint64
    qvec    []int8   // quantized vector (symmetric, scale = maxAbs/127)
    scale   float32  // dequantization factor: original ≈ qvec[i] * scale
    norm    float32  // L2 norm of the ORIGINAL float32 vector
    level   int
    friends [][]uint64
}
```

**`norm` is the L2 norm of the original float32 vector**, not a norm
re-derived from the dequantized int8 values. It is more accurate (it does not
accumulate quantization error) and makes the distance computation mildly
*asymmetric* in a recall-favourable direction.

**No on-disk format change.** The HNSW index is in-memory only and is rebuilt
from float32 source vectors on startup (`pkg/storage/persistence_replay.go`,
`index_operations.go` call `idx.Insert(id, []float32)`). The persistent-index
API (`NewPersistentHNSWIndex`/`SaveMetadata`) referenced in
`btree_storage.go:557` no longer exists in tree. Consequences:

- float32 remains the **source of truth**; int8 is a lossy in-RAM projection.
- A bad recall result is fully recoverable — re-rank (Phase 2) can fetch
  float32 from storage; rollback is a code revert, not a data migration.
- The snapshot-format-stability rule in `CLAUDE.md` does **not** apply here.

---

## 3. Quantization

A single helper, used at exactly two call sites:

```go
// quantizeInt8 quantizes v with a symmetric per-vector scale (maxAbs/127),
// returning the int8 vector, the scale, and the L2 norm of the ORIGINAL v.
// A zero vector returns (zeros, 0, 0).
func quantizeInt8(v []float32) (q []int8, scale, norm float32)
```

- **Insert** (`hnsw.go` `Insert`): quantize the incoming vector once; store
  `{qvec, scale, norm}` on the node. The `len(vector) != h.dimensions` guard
  is unchanged.
- **Search** (`hnsw.go` `Search`): quantize the query **once** at entry into a
  small value carrier:

  ```go
  type quantizedVec struct {
      q     []int8
      scale float32
      norm  float32
  }
  ```

  Thread `quantizedVec` (by value or pointer) through `searchLayer` /
  `searchLayerKNN` instead of `query []float32`. Quantization is **not** done
  per comparison — that is what converts the byte-reduction into a real
  speedup.

All affected signatures (`searchLayer`, `searchLayerKNN`, the internal
`distance` method) are **package-private**, so there is **no external API
breakage**. `Insert(id, []float32)` and `Search([]float32, k, ef)` keep their
public float32 signatures.

---

## 4. Distance metrics — all three derived from one int8 dot

The inner loop computes one integer dot product:

```
dot = Σ qa[i] * qb[i]   // int32 accumulation over int8 lanes
```

Each supported metric is a cheap scalar tail over `{dot, scaleA, scaleB,
normA, normB}` (the index is metric-configurable per
`CreateVectorIndex`; cosine is the default but euclidean and dot_product are
selectable, so all three must work):

| Metric        | Distance |
|---------------|----------|
| Cosine        | `1 − (dot·sA·sB) / (normA·normB)` |
| DotProduct    | `−(dot·sA·sB)` (negated so "closer" = smaller, matching current `Distance`) |
| Euclidean     | `√( max(0, normA² + normB² − 2·dot·sA·sB) )` |

- The euclidean **`max(0, …)` clamp is load-bearing**: exact float32 norms
  combined with an *approximate* int8 dot can produce a slightly negative
  radicand when `a ≈ b`, which would yield `NaN` and corrupt the search.
- **Zero-vector guard** (`normA == 0 || normB == 0`) is preserved from the
  current `CosineSimilarity` / distance code: cosine and dot_product return the
  same sentinels they do today; euclidean falls through to the norm-only term.

The metric is selected once per index (stored on `HNSWIndex.metric`), so the
branch is hoisted out of the per-comparison path where practical.

---

## 5. Kernels — two implementations behind one internal call

Mirrors the existing `distance_simd_smoke_{amd64,fallback}.go` build-tag
pattern already in `pkg/vector`.

- **`distance_int8.go`** — portable **scalar** `dotInt8(a, b []int8) int32`.
  The correctness reference and the universal fallback. Compiles on every
  arch/configuration.
- **`distance_int8_amd64.go`** (`//go:build amd64 && goexperiment.simd`) —
  **SIMD** dot via **widen `int8 → int16` then `DotProductPairs` (`VPMADDWD`)
  → int32 reduce**, using `simd/archsimd`. Tail elements (length not a multiple
  of the lane width) handled by a scalar remainder loop.
- **`distance_int8_fallback.go`** (`//go:build !(amd64 && goexperiment.simd)`)
  — routes the SIMD entry point to `dotInt8` (scalar).

**Why widen→VPMADDWD and not VNNI or VPMADDUBSW** (verified against
`go1.26.3`, `GOEXPERIMENT=simd`, `GOARCH=amd64`):

- True VNNI `VPDPBUSD` (int8→int32 4-way accumulate) is **not exposed** by
  `simd/archsimd`. `X86.AVX512VNNI()` exposes only the **feature-detection
  bit**, not the operation (checked: no 8-bit-receiver→int32 method, no
  free-function dot, no accumulator-form method on `Int32x16` taking 8-bit
  *vector* args).
- `DotProductPairsSaturated` (`VPMADDUBSW`, uint8×int8→int16) **saturates**:
  `255·127 + 255·127 = 64770 > 32767`. A general DB kernel cannot assume benign
  data, and one saturating pair silently corrupts a distance → nondeterministic
  recall loss. Rejected on **correctness**, not perf.
- `DotProductPairs` (`VPMADDWD`, int16×int16→int32) is **safe**: with int8
  values widened to int16, max product 16129, pair-sum 32258, fits int32 with
  headroom. ~half the lane throughput of hypothetical VNNI plus widening
  overhead, but a real win over amd64 scalar.

**The amd64 SIMD win is real but unverifiable on the arm64 dev box** —
`archsimd` is amd64-only and Rosetta/local-Docker distorts the compute/memory
balance. SIMD perf is validated on real amd64 CI (§8); correctness is validated
everywhere (§7).

---

## 6. Recall harness — the Phase-1 go/no-go gate

`recall_int8_test.go` (test + optional bench):

1. Load a corpus of **real embeddings** (not random — real embeddings cluster,
   which is the case the spike did not measure). Source: an existing fixture if
   one ships in-repo, else a small generated-but-clustered set; the plan picks
   the concrete source.
2. Build two indexes from the same vectors: one current-float32, one int8.
3. For a query set, measure **recall@k** = |int8 top-k ∩ float32 top-k| / k,
   plus mean/max distance error.
4. **Gate**: int8 is committed only if recall@k meets an agreed threshold
   (proposed: recall@10 ≥ 0.98 on the chosen corpus — confirm in the plan).
   If it fails, the fallback is **int8 candidate-gen + float32 re-rank**
   (over-query in int8, re-rank top-k with float32 fetched from storage) — a
   Phase 2 design, not built in this phase.

---

## 7. Testing

- **Differential kernel test**: `dotInt8` (scalar) == SIMD `dotInt8` on random
  inputs and edge cases — all-`127`, all-`-128`, zeros, alternating signs,
  lengths spanning lane boundaries and tails. This is the primary guard against
  the saturation/sign bug class.
- **Quantize round-trip**: error bounds on `quantizeInt8`; scale/norm
  correctness; zero-vector path.
- **Metric correctness**: each metric's int8 result within tolerance of the
  float32 formula; euclidean negative-radicand (`a≈b`) exercises the clamp;
  zero-vector sentinels match current behaviour.
- **Existing suite**: `pkg/vector` tests + `-race` (concurrent search + heap)
  stay green.
- **Benchmarks**: `BenchmarkHNSWSearch` and insert, int8 vs float32,
  `-count≥6`; report throughput + allocs/op.

---

## 8. CI / build

- `GOEXPERIMENT=simd` threaded through the amd64 SIMD build+test path (the
  existing archsimd smoke job already establishes the mechanism).
- **SIMD perf validated on a real amd64 runner**, not local Docker/Rosetta.
- The **differential correctness test runs on every arch/config** (it does not
  require the SIMD build — it compares scalar-vs-SIMD only where SIMD is
  compiled in; elsewhere it validates scalar against the float32 reference).
- `go vet` + `golangci-lint run ./...` clean at CI's surface; any `//nolint:`
  carries a reason (repo policy).

---

## 9. Noted benefit (not a deliverable)

int8 vectors are 4× smaller, directly reducing the **per-tenant HNSW memory
footprint** — the F4 / Track-R capacity concern. Worth one sentence in the
capacity story when that work is next touched; not built or measured here.

---

## 10. Approaches considered and rejected

- **Cosine-only int8, other metrics keep float32** — rejected: mixed
  float32/int8 storage in one struct is ugly, and all three metrics derive
  cleanly from the same `{int8 dot, scale, norm}` fields (§4).
- **Quantize-on-the-fly per comparison** (don't store int8) — rejected: no
  memory win and adds per-comparison cost.
- **Pure int8 with no recall gate** — rejected: skips the "measure on real
  embeddings" step the evidence trail explicitly calls for (§6).
- **amd64 VPMADDUBSW max-throughput path** — rejected for Phase 1: saturation
  is a correctness risk; mitigating it by narrowing the quantization range
  trades recall for speed. Revisit only if VPMADDWD proves insufficient.
