# Track R verification — per-tenant HNSW count scaling (2026-05-14)

## TL;DR

Per-tenant HNSW heap cost is **flat** as tenant count scales from 100 → 500 (constant 1000 vectors/tenant × 768 dims). Measured per-tenant bytes were **identical to six significant figures** (3,463,306 → 3,463,305 = ratio 1.000000). **Decision 2's Option A bet (per-tenant HNSW in OSS) holds empirically.** The `count_scale_1000` scenario was started but exceeded this session's interactive budget; the test scenario is committed and will run under `GRAPHDB_BENCH_LARGE=1` in a follow-up.

## Context

The `NEXT_STEPS_2026-05-15.md` § "What's NOT yet verified in production" called out three gaps; this doc closes the first axis (count scaling) of the first gap:

> Per-tenant HNSW memory bench at realistic tenant counts. Currently unit-tested at small scale; never measured at 100/500/1000 tenants. Outcome either validates the Option A bet (no further action) OR surfaces the enterprise-plugin filtered-HNSW work as the next track.

PR #195 already measured per-tenant *cost* at the documented Option A scale (100 tenants × 10k vectors × 768 dims = 3.46 GB heap, +8% delta vs the 3.2 GB spike estimate). What that PR did **not** measure is whether per-tenant bytes stay flat as tenant count grows. This is the load-bearing question: if per-tenant cost grows non-trivially with N, Option A breaks down for SaaS customers with hundreds-to-thousands of tenants and the enterprise filtered-HNSW plugin becomes the necessary path.

## Methodology

Extended `pkg/storage/vector_index_memory_test.go::TestVectorIndex_PerTenantMemoryFootprint` with three count-scaling scenarios that hold per-tenant size constant and sweep tenant count. Reused `measureVectorIndexHeapDelta` from PR #195 unchanged.

| Parameter | Value |
|---|---|
| Vectors per tenant | 1,000 (held constant across tenants) |
| Dimensions | 768 (matches Option A's documented embedding size) |
| HNSW `m` | 16 (production default per `pkg/api/handlers_vectors.go`) |
| HNSW `efConstruction` | 200 (production default) |
| Distance metric | `vector.MetricCosine` (production default) |
| Vectors | zero-filled (PR #195 caveat: raw byte cost is identical to real vectors; graph edge density may differ) |
| Measurement | `runtime.GC()` × 2 + `ReadMemStats.HeapAlloc` delta (Go-runtime canonical idiom) |

Per-tenant size of 1,000 vectors (vs the documented Option A's 10k) is a 10× compression chosen so the 1000-tenant scenario fits in dev-machine RAM budget. Total *vector count* at the largest scenario (1M vectors) matches PR #195's spike_estimate (100 × 10k = 1M). For the count-scaling question, what matters is whether per-tenant bytes stay constant across N — the *absolute* per-tenant size is a separate axis already validated by PR #195.

**Threshold for "Option A validated"**: per-tenant bytes at N > 100 must be **≤ 1.5× the 100-tenant baseline**. The assertion is encoded in the test (`count_scale_linearity` subtest); CI enforces it under `GRAPHDB_BENCH_LARGE=1`. 1.5× is generous; a follow-up can tighten once multi-run variance data exists.

## Results

Measured on macOS dev machine (Apple Silicon, Go 1.24), single run, `GRAPHDB_BENCH_LARGE=1`:

| Scenario | Tenants | Vectors/tenant | Dims | Heap bytes | Per-tenant bytes | Per-vector bytes | Ratio vs 100-baseline |
|---|---:|---:|---:|---:|---:|---:|---:|
| count_scale_100 | 100 | 1,000 | 768 | 346,330,640 | 3,463,306 | 3,463 | 1.000 |
| count_scale_500 | 500 | 1,000 | 768 | 1,731,652,864 | 3,463,305 | 3,463 | 1.000 |
| count_scale_1000 | 1,000 | 1,000 | 768 | *(pending — see Limitations)* | | | |

**Wall times observed**: 100T ≈ 78 s, 500T ≈ 13 min. The 1000T scenario was started but exceeded the session's interactive budget; the test continues running in the background as of this commit.

### Comparison to PR #195's spike_estimate

| Comparator | Total vectors | Total heap | Heap / vector |
|---|---:|---:|---:|
| PR #195 (100 tenants × 10k vectors × 768 dims) | 1,000,000 | ~3,460,000,000 | ~3,460 |
| This run, 500-tenant point (500 × 1k × 768) | 500,000 | 1,731,652,864 | 3,463 |

Per-vector bytes are nearly identical (3,460 vs 3,463 — within 0.1%) despite 10× difference in vectors-per-tenant. This means the per-tenant *container* overhead (`HNSWIndex` struct, inner property map, outer tenant map slot) is **negligible** relative to the per-vector cost — confirming the load-bearing assumption behind Option A.

## Conclusion

**Option A is validated on the count-scaling axis** at the tested scale (100 → 500 tenants). The 100→500 ratio is identical to six significant figures, far below the 1.5× threshold. The OSS tier's "per-tenant HNSW for low-hundreds-tenants" assumption holds empirically; extending to low-thousands appears safe based on the flatness observed in the 100→500 sweep, but the 1000-tenant data point is pending and should be confirmed in a follow-up.

Implications:

- **No new tracks surface from this measurement.** The enterprise filtered-HNSW plugin remains a *premium-tier* offering for customers with thousand-of-tenants-at-10k-vectors-each profiles where consolidated indexing is the better fit. It is not a *correctness* prerequisite for the OSS tier.
- **Decision 2 holds without modification.** Tier-bifurcated resolution (OSS = Option A; enterprise plugin = Option B) is the right shape.
- **Track R is partially closed on the (1a) axis.** 100 + 500 measured; 1000 pending. Two of the three verification components remain open: (1b) auto-embed observer load test, (1c) Docker/k8s exercise.

## Limitations

- **Single-run measurement, two data points captured here.** The 1000-tenant scenario was started but the wall time exceeded the session's interactive budget. The test code includes all three scenarios; running `GRAPHDB_BENCH_LARGE=1 go test ... ./pkg/storage/` (≥ 45 min wall) captures all three. A follow-up commit will append the 1000-tenant number once the in-flight run completes.
- **Go-heap only, not RSS.** `HeapAlloc` is what the index *allocates*; operator-visible RSS can be higher due to fragmentation and GC pacing. A future extension can capture RSS via `/proc/self/status` or `ps`.
- **Zero-filled vectors.** Raw byte cost is identical (HNSW stores vectors verbatim); graph edge density may differ for real embeddings. Each `m`-friends list is capped at 2m, so the upper bound is fixed.
- **Single-run, no variance characterization.** Per-run variance for HNSW heap measurements is ~5-10% per PR #195's documentation. The 100→500 ratio of 1.000000 is well within any plausible variance band, so the conclusion is robust. Multi-run variance characterization is a separate follow-up.
- **One process, no IPC/replication overhead.** This measures the index itself, not full-system memory under a sharded write path. The cluster code (`pkg/cluster/`) and snapshot persistence add their own footprints not captured here.

## Next actions

Per `NEXT_STEPS_2026-05-15.md` § Critical path:

- **(A) verification gap** — remaining components: (1a) the count_scale_1000 data point; (1b) auto-embed observer load test under production-shaped traffic; (1c) Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`. Either (1b) or (1c) is a valid next-session pick.
- **Append the count_scale_1000 number to this doc** once the in-flight bench completes. Single-line edit to the Results table.
- **(C) new audit angle** — performance under SaaS load (now partly correlated by this measurement), vector/embedding side-channels, productization audit for multi-node.

Planning doc update needed: mark Track R (1a) as partially closed (100+500 confirmed, 1000 pending); add this doc as the reference. Recommend the `planning-doc-update` skill as a small follow-up PR rather than bundling it here.

## How to reproduce

```bash
GRAPHDB_BENCH_LARGE=1 go test -v \
  -run 'TestVectorIndex_PerTenantMemoryFootprint/count_scale' \
  -timeout 1800s \
  ./pkg/storage/
```

Expected wall time: ≥ 45 min on Apple Silicon for all three count_scale scenarios (count_scale_100 ≈ 78 s, count_scale_500 ≈ 13 min, count_scale_1000 estimated ≈ 25-30 min). Per-run variance: ~5-10%. If per-tenant bytes drift more than 1.5× from the 100-tenant baseline on a future run, the `count_scale_linearity` subtest will fail with a structured error.
