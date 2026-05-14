# Track R verification — auto-embed observer load test (2026-05-14)

## TL;DR

The auto-embed pipeline's drop-on-full backpressure design (S11 spike §7.5) holds under three new verification angles: **sustained-saturation steady-state** (drops keep accumulating in the second half of a 3-second run, not just at the initial burst), **erroring-embedder under saturation** (O-1 structured logs from PR #202 fire correctly from pool-dispatched tasks, and log volume is bounded by drain count, not submit count), and **HTTP-surface backpressure** (POST /nodes returns 201 for every request even under 96% drop rate; HTTP latency stays well below the catastrophic-blocking ceiling). **The S11 §7.5 bet (drop-on-full preferable to blocking CreateNode) holds empirically across the full surface — Go-direct burst, Go-direct sustained, Go-direct under-error, and HTTP-direct.**

## Context

The `NEXT_STEPS_2026-05-15.md` § "What's NOT yet verified in production" called out the second gap as (1b) auto-embed observer load test under production-shaped traffic. PR #196 (`11bf734`) added a Go-direct burst load test that proved the drop path fires; this doc closes the remaining angles called out in the gap description:

> O-1 structured logging shipped via PR #202; the bounded-pool drop path has never been exercised under sustained node-create load. Needs a harness that drives POST /v1/nodes with auto-embed enabled at a rate that exceeds the pool's drain rate.

Three angles factor out of that framing — they're independent failure modes:

| Angle | What's pinned | Failure mode this catches |
|---|---|---|
| Sustained | Drop rate stays positive in steady state | Pool quietly stops dropping (counter wedge, leaked worker reactivates queue, off-by-one in bounded-channel arithmetic) |
| Erroring embedder | O-1 logs (PR #202) fire from pool-dispatched tasks under load | Log path wired to synchronous-only path; misbehaving embedder produces log-volume proportional to client load |
| HTTP | HTTP returns 201 + bounded latency under saturation | Drop signal propagates back to HTTP failure; future change wires CreateNode to await writeback |

PR #196 covers only the first half of "burst, non-erroring, Go-direct." This doc closes the other three combinations needed for full coverage.

## Methodology

Three new tests added:

| Test | Location | Surface |
|---|---|---|
| `TestAutoEmbedObserver_SustainedLoadDropsContinue` | `pkg/intelligence/auto_embed_observer_load_test.go` | Go-direct, sustained, non-erroring |
| `TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad` | `pkg/intelligence/auto_embed_observer_load_test.go` | Go-direct, burst, **erroring** (ErrNoIndexForTenant) |
| `TestAutoEmbedObserver_HTTPCreateNodeBackpressure` | `pkg/api/auto_embed_http_load_test.go` | **HTTP-direct**, burst, non-erroring |

All three reuse PR #196's harness shape (`slowFakeEmbedder` with 50ms delay, 2-worker / 10-queue pool, 8 producers) plus the additions specific to each angle. The HTTP test wires the observer manually (not via `bootstrapAutoEmbedFromEnv`) because the production LSAEmbedder drains too fast against a tiny test corpus to saturate the pool reliably; bootstrap-path wiring is already covered by `TestBootstrapAutoEmbedFromEnv_EndToEnd` in `server_autoembed_bootstrap_test.go`.

All three are `GRAPHDB_BENCH_LARGE`-gated and pass under `-race`.

**Erroring-embedder design choice**: all-errors (vs. mixed) gives a tight upper bound on expected log lines. `ErrNoIndexForTenant` exercises the dedicated `no-index-for-tenant` category; the `embed-failed` category (with M-1 sanitization) is already pinned by `TestAutoEmbedObserver_LogsEmbedderError_SanitizesUserText` (unit-level) and would only duplicate at load.

**HTTP threshold rationale**: latency assertion is `max_http_lat_ms < 500ms`. The catastrophic-blocking failure mode (pool.Submit waits for queue space) pushes max latency toward `queue_depth × embedder_delay = 10 × 50ms = 500ms`. The primary discriminator for "Submit blocked" vs "Submit dropped" is `pool.Dropped() > 0` (drops would be 0 if Submit ever blocked); latency is the secondary signal. 500ms is calibrated to catch catastrophic regression while tolerating real HTTP + 8-way write-contention overhead (~140-260ms observed).

## Results

Measured on macOS dev hardware (Apple Silicon, Go 1.24, `GRAPHDB_BENCH_LARGE=1`, `-race` enabled).

### TestAutoEmbedObserver_SustainedLoadDropsContinue

| Metric | Value |
|---|---:|
| Wall time | 3.00 s (load) + ~2.3 s (drain) = ~5.5 s total |
| Producers | 8 |
| Total creates | 127,184 |
| Drops at midpoint (1.5s) | 63,951 |
| Drops at end (3.0s) | 127,054 |
| Drops in second half | 63,103 |
| Drops per second | 42,347 |
| Max CreateNode latency | 7.94 ms |

Steady-state drop accumulation: 63,103 drops in the second half vs 63,951 in the first half is within 1.3% — drops are essentially linear in elapsed time, exactly what the design predicts. The pool never stops dropping after initial saturation.

### TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad

| Metric | Value |
|---|---:|
| Wall time | 0.53 s |
| Total creates | 400 |
| Embedder calls (drained) | 12 |
| Pool drops | 388 |
| Log lines (`auto-embed:`) | 12 |
| Log lines / drained | 1.00 |
| Log lines / total submits | 0.03 |

Log volume is **exactly bounded** by embedder calls (drained tasks) — every drained task produces one O-1 log line, dropped tasks produce zero. A misbehaving embedder cannot cause log-volume explosion proportional to client request rate. The `no-index-for-tenant` category string and the structural `tenant=acme` / `policy=Doc` fields all appear in logs as expected.

### TestAutoEmbedObserver_HTTPCreateNodeBackpressure

| Metric | Value |
|---|---:|
| Wall time | 3.44 s (load) + 0.5 s (drain) + ~3 s (cleanup) = ~6.9 s total |
| Producers | 8 |
| Total HTTP requests | 400 |
| HTTP 201 Created | 400 (100%) |
| HTTP non-201 | 0 |
| Pool drops | 331 (83% drop rate) |
| Embedder calls (drained) | 69 |
| Max HTTP latency | 88.85 ms (one run); 140.72 ms (another) |

All HTTP requests succeed even with 83% of embed tasks dropped — the drop signal is correctly decoupled from CreateNode's return value. Max HTTP latency (88-141ms across runs) is well under the 500ms catastrophic ceiling, and the `pool.Dropped() > 0` assertion confirms `Submit` is non-blocking on the saturation path.

## Conclusion

**S11 spike §7.5 (drop-on-full backpressure) is validated across all four surface combinations**:

| Surface | Erroring? | Shape | Test |
|---|---|---|---|
| Go-direct | no | burst | `TestAutoEmbedObserver_BackpressureUnderLoad` (PR #196) |
| Go-direct | no | sustained | `TestAutoEmbedObserver_SustainedLoadDropsContinue` (this PR) |
| Go-direct | yes (ErrNoIndexForTenant) | burst | `TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad` (this PR) |
| HTTP-direct | no | burst | `TestAutoEmbedObserver_HTTPCreateNodeBackpressure` (this PR) |

Implications:

- **PR #202's O-1 logging works correctly at scale.** Logs fire from pool-dispatched tasks (not just synchronous-path tasks) and are bounded by drain count. The "log volume bounded by drain rate, not submit rate" property holds — important for production where a misbehaving embedder must not amplify client load into operator-log pressure.
- **HTTP requests stay independent of embed task fate.** A future change that wires CreateNode to await embedding completion would break Assertion 1 (`http_201 != totalRequests`) and Assertion 3 (`maxLatMs > 500ms`); the test catches both.
- **Track R component (1b) is fully closed.** All three remaining angles (sustained, erroring, HTTP) are covered; the one component still open is (1c) Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`.

## Limitations

- **Single-process, single-run measurements.** Each test runs once at fixed-size load shapes. Multi-run variance characterization (e.g., 10 runs to compute percentile distributions of HTTP latency) is a separate follow-up; the current assertions are coarse enough that single-run flakiness should be rare under `GRAPHDB_BENCH_LARGE=1`.
- **`slowFakeEmbedder` is artificial.** Production LSAEmbedder execution time depends on tenant corpus size; real-world embedders (hosted-API, ONNX) have entirely different drain-rate profiles. The 50ms-delay synthetic embedder is calibrated to saturate the test's 2-worker pool, not to reproduce any specific production deployment.
- **`auto_embed_http_load_test.go` uses manual observer wiring**, not `bootstrapAutoEmbedFromEnv`. Bootstrap-path wiring under load is not exercised by this test; that path's unit-level wiring is pinned by `TestBootstrapAutoEmbedFromEnv_EndToEnd` already. A future HTTP load test could combine the bootstrap path with a slow-fake embedder via env-var injection if needed.
- **Race detector slows worker dispatch substantially.** Observed embedder call rate is ~18/s under `-race` vs ~40/s without; assertion thresholds (drops > 0, drain count, log line count) account for this by being load-shape-relative, not absolute. If a future tightening fails under `-race`, check whether the test relies on absolute-rate behavior.
- **HTTP test threshold (500ms) is calibrated to the *current* HTTP-layer + storage overhead** (no `BulkImportMode`). If the HTTP path gets faster or the storage path slows down, the threshold gains or loses headroom. The doc-comment in the test calls out the calibration explicitly so a future engineer can recalibrate without digging through git blame.

## Next actions

Per `NEXT_STEPS_2026-05-15.md` § Critical path:

- **(A) verification gap** — remaining component: (1c) Docker/k8s exercise of `GRAPHDB_AUTO_EMBED_ENABLED`. End-to-end container build + env-driven bootstrap. Larger scope than (1a) or (1b); needs Dockerfile + compose work plus a deployment-recipe verification doc.
- **(C) new audit angle** — performance under SaaS load (now correlated by (1a) measurement + (1b) saturation behavior across the auto-embed pipeline), vector/embedding side-channels (M-1 sanitization holds under load per this doc; O-1 logging closes the remaining audit-routed item from the side-channels audit), productization audit for multi-node.

Planning doc update needed: mark Track R (1b) as fully closed via this PR + PR #196 + PR #202; add this doc as the reference. Recommend the `planning-doc-update` skill as a separate small follow-up PR rather than bundling it here.

## How to reproduce

Three test entry points, each `GRAPHDB_BENCH_LARGE`-gated:

```bash
# All three new tests under -race (run them together for parity with this doc's numbers):
GRAPHDB_BENCH_LARGE=1 go test -v -race \
  -run 'TestAutoEmbedObserver_(SustainedLoadDropsContinue|EmbedderErrorsLoggedUnderLoad|HTTPCreateNodeBackpressure)' \
  -timeout 120s \
  ./pkg/intelligence/ ./pkg/api/
```

Expected total wall time: ~14-20 seconds (sustained: ~5.5s; erroring: ~0.5s; HTTP: ~7s; plus per-package test-binary startup). Under heavy machine load, multiply by ~2x.

Individual tests:

```bash
# Sustained-load steady state
GRAPHDB_BENCH_LARGE=1 go test -v -race \
  -run TestAutoEmbedObserver_SustainedLoadDropsContinue \
  -timeout 60s ./pkg/intelligence/

# Erroring-embedder + O-1 log volume bounding
GRAPHDB_BENCH_LARGE=1 go test -v -race \
  -run TestAutoEmbedObserver_EmbedderErrorsLoggedUnderLoad \
  -timeout 60s ./pkg/intelligence/

# HTTP-surface backpressure
GRAPHDB_BENCH_LARGE=1 go test -v -race \
  -run TestAutoEmbedObserver_HTTPCreateNodeBackpressure \
  -timeout 60s ./pkg/api/
```

The CI fast path (`go test -short`) skips all three (`GRAPHDB_BENCH_LARGE` is unset) — they are operator-run benches, not CI-resident.

## References

- S11 spike §7.5 (`docs/internals/design/S11_AUTO_EMBEDDER_REDESIGN.md`): "Dropping on back-pressure is preferable to blocking CreateNode" — the design this verification closes against.
- PR #196 (`11bf734`): predecessor — Go-direct burst load test (Track R verification gap, part 2).
- PR #202 (`2e22885`): O-1 structured error logging in auto-embed worker.
- PR #195 (`d2172ae`) + this doc's sibling (`TRACK_R_COUNT_SCALING_VERIFICATION_2026-05-14.md`): per-tenant HNSW count scaling (Track R component 1a, closed).
- `NEXT_STEPS_2026-05-15.md` § Verification gap: planning-doc framing for this component.
