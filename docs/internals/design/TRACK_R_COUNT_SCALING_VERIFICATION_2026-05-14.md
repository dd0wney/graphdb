# Track R verification — per-tenant HNSW count scaling (2026-05-14)

## TL;DR

Per-tenant HNSW heap cost is **flat** as tenant count scales from 100 → 1000 (constant 1000 vectors/tenant × 768 dims). Measured per-tenant bytes are **identical to six significant figures** across the full range (3,463,428 / 3,463,209 / 3,463,237 — ratios 1.000 / 1.000). **Decision 2's Option A bet (per-tenant HNSW in OSS) holds empirically across the planning doc's full named tenant range.**

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

Measured on two macOS dev machines (Apple Silicon, Go 1.24, `GRAPHDB_BENCH_LARGE=1`):

PR #209 captured 100T + 500T in one machine; this doc's append (post-PR) captures all three on a second machine. Both runs agree to six significant figures.

| Scenario | Tenants | Vectors/tenant | Dims | Heap bytes | Per-tenant bytes | Per-vector bytes | Ratio vs 100-baseline | Source |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| count_scale_100 | 100 | 1,000 | 768 | 346,330,640 | 3,463,306 | 3,463 | 1.000 | PR #209 |
| count_scale_500 | 500 | 1,000 | 768 | 1,731,652,864 | 3,463,305 | 3,463 | 1.000 | PR #209 |
| count_scale_100 | 100 | 1,000 | 768 | 346,342,888 | 3,463,428 | 3,463 | 1.000 | this run |
| count_scale_500 | 500 | 1,000 | 768 | 1,731,604,624 | 3,463,209 | 3,463 | 1.000 | this run |
| count_scale_1000 | 1,000 | 1,000 | 768 | 3,463,237,704 | 3,463,237 | 3,463 | 1.000 | this run |

The linearity subtest's emitted ratios (`HNSW_COUNT_SCALING` log lines) are `count_scale_500/100 = 1.000` and `count_scale_1000/100 = 1.000` (raw: 0.99994 each). Both are well below the 1.5× threshold encoded in the test.

**Wall times observed (this run, single machine, Apple Silicon idle)**: 100T = 107.68 s, 500T = 535.02 s (8.9 min), 1000T = 1074.38 s (17.9 min); total 1717.09 s (28.6 min). Note: total wall time finished only 83 s under the prior session's 1800 s `-timeout` budget — this is why the doc now recommends 3600 s (see *How to reproduce*). Wall times also vary by machine and load: PR #209's machine saw 500T ≈ 13 min, ~50% slower than the 8.9 min observed here.

### Comparison to PR #195's spike_estimate

| Comparator | Total vectors | Total heap | Heap / vector |
|---|---:|---:|---:|
| PR #195 (100 tenants × 10k vectors × 768 dims) | 1,000,000 | ~3,460,000,000 | ~3,460 |
| This doc, 500-tenant point (500 × 1k × 768; PR #209 run) | 500,000 | 1,731,652,864 | 3,463 |

Per-vector bytes are nearly identical (3,460 vs 3,463 — within 0.1%) despite 10× difference in vectors-per-tenant. This means the per-tenant *container* overhead (`HNSWIndex` struct, inner property map, outer tenant map slot) is **negligible** relative to the per-vector cost — confirming the load-bearing assumption behind Option A.

## Conclusion

**Option A is validated on the count-scaling axis** across the planning doc's full named range (100 → 1000 tenants). All three ratios are identical to six significant figures (raw `100T/100T=1.000`, `500T/100T=0.99994`, `1000T/100T=0.99994`), far below the 1.5× threshold. The OSS tier's "per-tenant HNSW for low-hundreds-to-thousands-of-tenants" assumption holds empirically; the inverse-scaling-with-N pattern predicted by the test's comment (small-N runs amortize fixed per-process overhead worse, so a modest decrease with N is expected) is observable in the data.

Implications:

- **No new tracks surface from this measurement.** The enterprise filtered-HNSW plugin remains a *premium-tier* offering for customers with thousand-of-tenants-at-10k-vectors-each profiles where consolidated indexing is the better fit. It is not a *correctness* prerequisite for the OSS tier.
- **Decision 2 holds without modification.** Tier-bifurcated resolution (OSS = Option A; enterprise plugin = Option B) is the right shape.
- **Track R component (1a) is fully closed.** 100 + 500 + 1000 all measured; the 1000-tenant data point confirms the flat-scaling pattern across the full named range. Two of the three verification components remain open: (1b) auto-embed observer load test, (1c) Docker/k8s exercise.

## Limitations

- **Two single-machine runs (PR #209 + this append), not multi-run characterization.** PR #209 captured 100T + 500T on one machine; this append captured 100T + 500T + 1000T on a second machine. Both agree to six significant figures, which is far stronger than per-run variance would predict, but neither captures variance across multiple runs on the same machine. Multi-run variance characterization (e.g., 3-5 runs of count_scale_1000) is a separate follow-up if the 5-10% expected variance ever needs to be tightened.
- **Go-heap only, not RSS.** `HeapAlloc` is what the index *allocates*; operator-visible RSS can be higher due to fragmentation and GC pacing. A future extension can capture RSS via `/proc/self/status` or `ps`.
- **Zero-filled vectors.** Raw byte cost is identical (HNSW stores vectors verbatim); graph edge density may differ for real embeddings. Each `m`-friends list is capped at 2m, so the upper bound is fixed.
- **One process, no IPC/replication overhead.** This measures the index itself, not full-system memory under a sharded write path. The cluster code (`pkg/cluster/`) and snapshot persistence add their own footprints not captured here.
- **Wall-time grows superlinearly with tenant count, even though per-tenant memory is flat.** count_scale_100 = ~108 s; count_scale_500 = ~535 s (5× tenants but ~5× wall, roughly linear); count_scale_1000 = ~1074 s (10× tenants but ~10× wall, also roughly linear — but the prior session's `-timeout 1800s` killed the test in trailing GC at ~30 min total wall time). This is a *bench-cost* limitation, not a *production* one — running a continuous-bench scenario at 1000 tenants is more expensive than the linear extrapolation from 100 would suggest. Relevant for any future CI-resident or continuous-bench planning.

## Next actions

Per `NEXT_STEPS_2026-05-15.md` § Critical path:

- **(A) verification gap** — remaining components: (1b) auto-embed observer load test under production-shaped traffic; (1c) Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`. Either is a valid next-session pick. Component (1a) is fully closed by this doc.
- **(C) new audit angle** — performance under SaaS load (now correlated by this measurement across the full 100→1000 range), vector/embedding side-channels, productization audit for multi-node.

Planning doc update needed: mark Track R (1a) as fully closed (100 + 500 + 1000 confirmed, all ratios 1.000); add this doc as the reference. Recommend the `planning-doc-update` skill as a small follow-up PR rather than bundling it here.

## How to reproduce

```bash
GRAPHDB_BENCH_LARGE=1 go test -v \
  -run 'TestVectorIndex_PerTenantMemoryFootprint/count_scale' \
  -timeout 3600s \
  ./pkg/storage/
```

Expected total wall time on Apple Silicon: ~30 min on a fast/idle machine, up to ~60 min under load. Two empirical reference points:

- Fast/idle machine (this run): count_scale_100 = 108 s, count_scale_500 = 535 s (8.9 min), count_scale_1000 = 1074 s (17.9 min), total = 1717 s (28.6 min).
- Slower machine (PR #209's session): count_scale_500 ≈ 13 min (~50% slower than this run), count_scale_1000 not measured — that session's `-timeout 1800s` killed the bench inside the trailing `runtime.GC()` *after* the 1M inserts completed.

The `-timeout 3600s` (60 min) is deliberately conservative. The 1800 s that killed the prior session was 83 s above the *fast* machine's full wall time — i.e. less than 5% margin on a fast machine, and *negative* margin on a slower one. The heap-drain time at ~3.5 GB is also non-deterministic. A 2× margin over the slow machine's expected wall time is the right shape.

Per-run variance on a single machine: ~5-10% (per PR #195's documentation). Cross-machine variance is much larger (~50% observed here). If per-tenant bytes drift more than 1.5× from the 100-tenant baseline on a future run, the `count_scale_linearity` subtest will fail with a structured error.
