# Perf Roadmap: SIMD distance kernels + Go 1.26 adoption — 2026-06-01

**Track**: Performance (proposed letter **P** — reconcile against the live
`NEXT_STEPS_<DATE>.md` before treating the letter as load-bearing; the repo has
hit A/C-style semantic collisions before).

**Slot**: This fills the "new audit — performance under realistic SaaS load"
candidate already named in `NEXT_STEPS_2026-05-15.md` § Critical-path option (C).
It is not a manufactured track; it is evidence-grounded (see § Context).

**Status**: Roadmap approved via brainstorming 2026-06-01. No code landed under
this track yet. Predecessor evidence: PR on branch `perf/hnsw-search-pooling`
(value-typed HNSW heap, 212 → ~37 allocs/op) surfaced the next bottleneck.

---

## Context & evidence

This track is grounded in profiling done while closing the HNSW heap-allocation
work, not in the (repeatedly unreliable) `AUDIT_performance_2026-05-06.md`
estimates:

- **The search hot loop is now allocation-light** (`perf/hnsw-search-pooling`:
  `BenchmarkHNSWSearch` 212 → ~37 allocs/op, stable across `-count=6`). With
  per-item heap boxing gone, the remaining per-query cost is **compute**, and
  `HNSWIndex.distance` fires per neighbor candidate (≈ef × layers per `Search`).
- **`pkg/vector/distance.go` is entirely scalar float32 reduction loops** —
  `CosineSimilarity` (3 accumulators), `EuclideanDistance`, `DotProduct`,
  `Normalize`, `Magnitude`. No assembly, no intrinsics anywhere in the repo
  (zero `.s` files; no `//go:build amd64/arm64` precedent except a `!race`
  test tag).
- **`CosineSimilarity` recomputes `‖query‖` per comparison** even though the
  query is constant within a `Search` — an algebraic win available before any
  SIMD.
- **LSA already quantizes docs to int8** (`lsa.go:quantizeFloat32`,
  `lsaQuantScale`); `TopKByVector` (line ~675) scans the corpus with a scalar
  `sim += qvec[j] * float32(x)` — a future int8-SIMD target with the
  groundwork already laid.
- **Toolchain**: `go.mod` declares `go 1.25.3`; the installed toolchain is
  `go1.26.3`. CI spans **amd64** (ubuntu: lint, benchmark, release, docker) and
  **arm64** (macOS test matrix, per Track H). Any kernel needs both arch paths
  plus a scalar fallback.

**Mechanism decision (committed, with eyes open)**: Go 1.26's `simd/archsimd`
intrinsics, not hand assembly or a third-party lib. Rationale: dogfoods the
latest Go (and the user's `goolang` SIMD goal), keeps the kernel in-tree.

**Empirically established 2026-06-01** (verified against installed `go1.26.3`,
not assumed): `simd/archsimd` is **experimental**, exists *only* under
`GOEXPERIMENT=simd`, is **amd64-only** (its own doc: "It currently supports
AMD64"), and is **not** covered by the Go 1 compatibility promise. This is a
deliberate, accepted trade-off, not "first-class stable SIMD." Consequences,
baked into the gates below:
- **arm64 (incl. Apple Silicon / Graviton) gets the scalar path only** — SIMD
  benefits the amd64 Docker/prod target. Scalar is arm64's only path, not a
  "fallback."
- **`GOEXPERIMENT=simd` must be threaded through any build/CI/release** that
  compiles the SIMD files. SIMD files are isolated behind a
  `//go:build amd64 && goexperiment.simd` tag (+ a complementary fallback) so
  the **default** build stays green on every target — verified.
- **Measurement happens on amd64**, not the arm64 dev box (which always runs
  scalar). CI's `ubuntu-latest` (amd64) is the measurement + smoke surface.

---

## Gate 0 — Go 1.26 adoption *(prerequisite; also banks free wins)*

Everything downstream needs 1.26. This gate also delivers transparent wins and
de-risks the SIMD bet cheaply.

1. **Bump** `go.mod` `go 1.25.3 → 1.26`; confirm the ubuntu + macOS CI matrix
   resolves and stays green on 1.26.
2. **Measure the free wins first.** Re-run `pkg/storage` + `pkg/vector`
   benchmarks pre/post-bump and record the Green Tea GC + stack-allocation
   deltas **before any SIMD code exists**. This isolates "what the bump bought"
   from "what the kernel bought" (same evidence-discipline as the heap work).
3. **De-risk the mechanism.** Land a throwaway minimal `archsimd` kernel (an
   8-wide float32 add) gated by `//go:build amd64 && goexperiment.simd`, with a
   differential test (SIMD == scalar). It compiles on amd64 under the experiment
   and **runs on amd64** (Docker `--platform linux/amd64` locally, or CI). The
   default build stays green on arm64 and on amd64-without-experiment. *(This
   step already done as a spike: the kernel + the three-way build isolation are
   verified — see the Gate-0 plan,
   `docs/superpowers/plans/2026-06-01-gate0-go126-simd-adoption.md`.)*

**Exit gate**: 1.26 green in CI + free-win numbers recorded + smoke test passes
on amd64 (CI `ubuntu-latest`) + default build green on arm64 and
amd64-without-experiment. *(Not "both arches run SIMD" — archsimd is amd64-only;
arm64 is verified to compile+run the scalar path.)*

---

## Phase 1 — Tier-1 distance kernel *(latency-critical payload)*

Strictly sequential; each step is independently shippable, so a stall banks the
prior win.

- **1a — Algebraic win, no SIMD.** Add `CosineDistanceWithQueryNorm(a, b, normA
  float32)` hoisting `‖query‖` out of the per-neighbor loop; store `‖v‖` per
  `hnswNode` at insert time. Ship and measure independently — a real win and a
  clean fallback if SIMD stalls. (See Open Q on snapshot format.)
- **1b — SIMD kernels.** Vectorize dot / L2 / norm in `distance.go` via
  `archsimd` (`Float32x8` AVX2; consider `Float32x16` AVX-512 guarded by
  `archsimd.X86.AVX512()`), behind build-tagged files:
  `distance_simd_amd64.go` (`//go:build amd64 && goexperiment.simd`) and
  `distance_scalar.go` (`//go:build !amd64 || !goexperiment.simd`, the existing
  scalar code — which is what arm64 and every non-experiment build use). This
  establishes the repo's first arch/experiment build-tag convention — document
  it. Note: the dot product needs a horizontal lane-sum (store the 8 lanes,
  scalar-reduce, or shuffle-add) — `archsimd` exposes no single reduce-add.
- **1c — Wire & verify.** Route `HNSWIndex.distance` and
  `pkg/queryutil/wire.go` through the new path. Differential test: the SIMD and
  scalar kernels must agree within float epsilon on randomized inputs.

---

## Phase 2 — Tier-2 LSA int8 dot *(specified, deferred)*

SIMD int8 dot product for `lsa.go:TopKByVector`. Quantization already exists, so
the win is the inner product (int8-SIMD / VNNI on amd64, NEON `sdot` on arm64).
**Gated on Phase 1** proving the dispatch + fallback pattern. Extra design
surface: the inner product is mixed int8 (doc) × float32 (query) — quantize the
query to fixed-point or dequantize in-register; decide at spec time.

---

## Tier 3 & NUMA — explicitly *not* a code kernel

- **Tier 3 (batch kernels)** — LSA SVD (`lsa.go` build-time), PageRank /
  `node_similarity` (`pkg/algorithms`). Throughput, not latency; sparse /
  gather-bound. **Opportunistic only**; pick up if index-build or analytics
  throughput becomes a stated complaint.
- **NUMA** — Go exposes no NUMA primitives; cgo affinity-pinning fights the
  scheduler (anti-pattern). The three levers are:
  - **(a) Go 1.26 Green Tea NUMA-aware GC** — free, delivered by **Gate 0**.
  - **(b) Deployment** — `numactl --interleave`, one process per socket.
  - **(c) Architecture** — process-per-socket sharding, which **is the existing
    `pkg/cluster` track** (the "no sharded write path" gap). For an in-memory
    graph DB, NUMA scaling and horizontal scaling are the same problem.

  **Conclusion**: this roadmap delivers (a) via Gate 0 and points (c) at the
  cluster track. It designs **no** NUMA code. Hand-rolled affinity is out of
  scope and discouraged.

---

## Per-tier acceptance — hybrid profile-gate + minimum-bar

Applied to **Phase 1b** and **Phase 2** (1a is a plain algebraic win, measured
normally):

1. **Profile-gate (entry).** A `-cpuprofile` of `BenchmarkHNSWSearch` (plus a
   brute-force search bench) must show `distance()` among the top cycle
   consumers. If profiling says distance is **not** the hotspot, the tier is
   **not entered** — write that down (the repo's "no silent caps" idiom), don't
   silently skip. This is the check flagged-but-never-run during the heap work;
   the roadmap makes it the literal gate.
2. **Minimum-bar (exit).** Ship only if **both** hold:
   - ≥ **2×** on the isolated kernel microbenchmark, **and**
   - a measurable end-to-end search-latency win — median over `-count ≥ 6`,
     beyond run-to-run noise. (Lesson from the heap commit, where a single-run
     "−8% latency" claim had to be retracted; allocs/op was the durable number.)

   If the kernel speeds up but end-to-end latency doesn't move, **revert** and
   record why (Amdahl: distance wasn't a large enough cycle slice).
3. **Correctness.** Differential SIMD-vs-scalar agreement within float epsilon,
   and the existing recall tests (`TestHNSWAccuracy`, `TestHNSWSearchTopK`) stay
   green.

---

## Risks & open questions

- **R1 — `archsimd` is experimental + unstable (CONFIRMED, not a risk-of).**
  It is `GOEXPERIMENT=simd`-gated and explicitly outside the Go 1 compatibility
  promise, so the API can change or break in 1.27+. *Mitigation*: SIMD code is
  isolated behind build tags with a scalar path always present; a Go upgrade
  that breaks `archsimd` degrades to scalar, never to broken. Re-validate on
  every Go minor bump. **Accept that a production DB ships behind a GOEXPERIMENT
  build flag** — this was a deliberate, eyes-open choice (see § Mechanism).
- **R2 — amd64-only (CONFIRMED).** No arm64/NEON in `archsimd`. arm64 deploys
  (Apple Silicon, Graviton) get zero SIMD benefit and run scalar. *Mitigation*:
  none needed for correctness (scalar is correct); but the **value claim is
  amd64-only** — say so wherever the win is reported. If arm64 perf becomes a
  priority, that reopens the mechanism choice (portable lib or NEON asm).
- **R3 — Amdahl risk.** Distance may be a smaller cycle slice than assumed once
  the heap allocations are gone. *Mitigation*: the profile-gate is exactly this
  guard; the minimum-bar's end-to-end clause prevents shipping a kernel win that
  doesn't move real latency.
- **Open Q — snapshot format.** Phase 1a's per-node `‖v‖`: persist it (snapshot
  version bump — see CLAUDE.md "snapshot format stability") or recompute on
  load? Recompute-on-load avoids a format change; persisting saves load-time
  work. Flagged for the implementer.

---

## How to use this document

1. **Gate 0 is the entry point** and is independently valuable (free GC/stack
   wins + de-risk) even if Phases 1–2 never ship.
2. **Reconcile the track letter** against the current `NEXT_STEPS_<DATE>.md`
   before treating "Track P" as canonical.
3. **Each phase is its own PR** in the repo's atomic-commit idiom; the
   acceptance gate's numbers go in the final commit of each phase.
4. **Revisit trigger**: if the Gate-0 profile-gate shows distance is *not* the
   hotspot, this track stops at Gate 0 (banked free wins) and the next perf
   investigation re-profiles to find the real hotspot.
