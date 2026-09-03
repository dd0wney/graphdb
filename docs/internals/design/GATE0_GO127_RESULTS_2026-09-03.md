# Gate 0 — Go 1.27 adoption results (2026-09-03)

Successor to `GATE0_GO126_RESULTS_2026-06-01.md`. Same shape: A/B benchstat,
SIMD smoke, exit checklist, discovered consequences.

## Why bump now

1. **Local and CI ran different toolchains.** `go.mod` said `toolchain go1.26.4`
   and `GOTOOLCHAIN=auto` picks a local toolchain whenever it is *newer* than the
   pin. A dev box with go1.27.0 installed therefore ran every local `go test` on
   1.27.0 while CI ran 1.26.4. Local green and CI green were two different
   results.
2. **The archsimd smoke test did not compile on 1.27.** Exactly the R1 risk that
   `PERF_SIMD_ROADMAP_2026-06-01.md` named ("re-validate on every Go minor
   bump"). Latent while CI was pinned; live the moment anyone bumped.
3. **1.27 carries things this repo wants**: size-specialised allocation for
   objects under 80 bytes (up to 30% cheaper — the size class of node and edge
   records), the goroutine-leak profile promoted to GA (49 goroutine launch
   sites, LSM workers), and `archsimd` extended to arm64 Neon (removes the
   "amd64-only" reason the roadmap gave for shelving Phase 1b; the
   memory-bound reason still stands).

## Method

A/B: `go1.26.4` (baseline — the CI pin) vs `go1.27.0`. `go.mod` bumped
`1.26.0` → `1.27.0` between captures. Benchmarks unchanged. benchstat for deltas.

Machine: linux/amd64, 32 hardware threads. **Not comparable in absolute terms to
the 1.26 gate**, which measured darwin/arm64.

Primary signal: `pkg/vector BenchmarkHNSWSearch` (`-count 6`).
Storage best-effort (`-count 6`): `BenchmarkGetNode_Uniform_PureReads_4`,
`BenchmarkGetEdge_Uniform_PureReads_4`, `BenchmarkGetNode_Sequential`.

Contention note: the storage runs (both toolchains) executed while the HNSW
baseline was running on the same box, so the storage A and B saw the same
background load. The HNSW after-run executed alone.

## What changed

| Surface | Before | After |
|---|---|---|
| `go.mod` | `go 1.26.0` + `toolchain go1.26.4` | `go 1.27.0` (toolchain line dropped — it equalled the floor) |
| `test.yml` ×6, `lint.yml` ×2, `benchmark.yml`, `release.yml`, `fuzz.yml` | `go-version: '1.26.4'` | `'1.27.0'` |
| `go-client.yml` | `go-version: "1.26"` | `"1.27"` (`clients/go/go.mod` stays `go 1.23` — that is the SDK consumers' floor, not ours) |
| `lint.yml` golangci-lint | `v2.11.4` | `v2.13.2` — v2.13.0 is the first release with go1.27 support (golangci-lint #6642); a golangci-lint built with an older Go cannot typecheck a `go 1.27.0` module. `config verify` passes with v2.13.2 built on go1.27.0. |
| `Dockerfile` | `golang:1.26-alpine` | `golang:1.27-alpine` (tag published 2026-09-02) |
| `pkg/vector/distance_simd_smoke_amd64.go` | `LoadFloat32x8Slice`, `.StoreSlice` | `LoadFloat32x8`, `.Store` |
| `docs/CI_CD.md` | matrix described as Go 1.23–1.25 × Ubuntu+macOS (stale since PR #181) | one leg: 1.27.0 + macOS |
| `docs/PRODUCTION_QUICKSTART.md` | "Go 1.23+ if building from source" (already wrong at the 1.26 floor) | "Go 1.27+" |
| `docs/TLS_CONFIGURATION.md` example Dockerfile | `golang:1.21-alpine` | `golang:1.27-alpine` |
| `Dockerfile.license-server` (found by review — the first sweep grepped `Dockerfile` and `go-version` only) | `golang:1.26-alpine` | `golang:1.27-alpine` |
| `fuzz.yml` pin comment | "go.mod's toolchain directive" (a line the bump removed) | "the go.mod floor (go 1.27.0)" |
| `packer/README-CUSTOM-IMAGE.md`, `SYNTOPICA-STAGING-DEPLOYMENT.md` | "Go 1.21+", and a `go1.21.5` tarball | "Go 1.27+", `go1.27.0` tarball |

## SIMD smoke — red, then green

Go 1.27 revised the amd64 `archsimd` API: the slice-taking loader and store
took the bare names, and the `*[8]float32` forms became `...Array`.

Red, before the rename (`go1.27.0`, `GOEXPERIMENT=simd`):

```
pkg/vector/distance_simd_smoke_amd64.go:13:17: undefined: archsimd.LoadFloat32x8Slice
pkg/vector/distance_simd_smoke_amd64.go:14:17: undefined: archsimd.LoadFloat32x8Slice
```

and, after the loader rename alone:

```
pkg/vector/distance_simd_smoke_amd64.go:16:13: va.Add(vb).StoreSlice undefined (type archsimd.Float32x8 has no field or method StoreSlice)
```

Positive control: the same command on `go1.26.4` passed before the rename.

Green, after both renames (`go1.27.0`, `GOEXPERIMENT=simd`, real linux/amd64 —
no Docker detour needed this time):

```
--- PASS: TestSIMDSmokeAddMatchesScalar (0.00s)
```

Without the experiment the fallback file is selected and the test SKIPs, as
designed. Floor proof: `GOTOOLCHAIN=go1.26.4 go build ./pkg/vector/` now
refuses with `go: go.mod requires go >= 1.27.0`.

## Benchmarks

### benchstat: go1.26.4 → go1.27.0, `pkg/vector BenchmarkHNSWSearch`

```
goos: linux
goarch: amd64
pkg: github.com/dd0wney/graphdb/pkg/vector
cpu: AMD Ryzen 9 5950X 16-Core Processor            
              │ bench_old_hnsw.txt │       bench_new_hnsw.txt        │
              │       sec/op       │    sec/op     vs base           │
HNSWSearch-32         905.6µ ± 64%   848.0µ ± ∞ ¹  ~ (p=0.126 n=6+5)
¹ need >= 6 samples for confidence interval at level 0.95

              │ bench_old_hnsw.txt │        bench_new_hnsw.txt        │
              │        B/op        │     B/op       vs base           │
HNSWSearch-32         10.51Ki ± 1%   10.52Ki ± ∞ ¹  ~ (p=0.900 n=6+5)
¹ need >= 6 samples for confidence interval at level 0.95

              │ bench_old_hnsw.txt │       bench_new_hnsw.txt       │
              │     allocs/op      │  allocs/op   vs base           │
HNSWSearch-32          19.00 ± 11%   19.00 ± ∞ ¹  ~ (p=1.000 n=6+5)
¹ need >= 6 samples for confidence interval at level 0.95
```

Raw samples, µs/op — baseline go1.26.4: 953, 1268, 840, 1483, 859, 852;
after go1.27.0: 848, 854, 938, 826, 835 (the sixth after-sample had not finished
when the box's shutdown deadline forced this document to close; `n=6+5`).

### benchstat: go1.26.4 → go1.27.0, `pkg/storage` reads (best-effort)

```
goos: linux
goarch: amd64
pkg: github.com/dd0wney/graphdb/pkg/storage
cpu: AMD Ryzen 9 5950X 16-Core Processor            
                               │ bench_old_storage.txt │        bench_new_storage.txt        │
                               │        sec/op         │    sec/op      vs base              │
GetEdge_Uniform_PureReads_4-32           126.4n ±   2%   131.2n ±  76%       ~ (p=0.065 n=6)
GetNode_Uniform_PureReads_4-32           166.9n ± 213%   173.0n ± 120%       ~ (p=0.937 n=6)
GetNode_Sequential-32                    608.7n ±  69%   641.4n ±  23%       ~ (p=0.240 n=6)
geomean                                  234.2n          244.2n         +4.28%

                               │ bench_old_storage.txt │       bench_new_storage.txt        │
                               │         B/op          │    B/op     vs base                │
GetEdge_Uniform_PureReads_4-32              544.0 ± 0%   544.0 ± 0%       ~ (p=1.000 n=6) ¹
GetNode_Uniform_PureReads_4-32              560.0 ± 0%   560.0 ± 0%       ~ (p=1.000 n=6) ¹
GetNode_Sequential-32                       561.0 ± 0%   561.0 ± 0%       ~ (p=1.000 n=6) ¹
geomean                                     554.9        554.9       +0.00%
¹ all samples are equal

                               │ bench_old_storage.txt │       bench_new_storage.txt        │
                               │       allocs/op       │ allocs/op   vs base                │
GetEdge_Uniform_PureReads_4-32              3.000 ± 0%   3.000 ± 0%       ~ (p=1.000 n=6) ¹
GetNode_Uniform_PureReads_4-32              4.000 ± 0%   4.000 ± 0%       ~ (p=1.000 n=6) ¹
GetNode_Sequential-32                       4.000 ± 0%   4.000 ± 0%       ~ (p=1.000 n=6) ¹
geomean                                     3.634        3.634       +0.00%
¹ all samples are equal
```

Both toolchains ran these while the HNSW baseline (single-threaded, 10k × 768
inserts per run) occupied another core. The ±213% / ±120% spread on
`GetNode_Uniform_PureReads_4` is that contention, not the code; the
comparison is A-vs-B under the same load, and benchstat calls all three `~`.
`B/op` and `allocs/op` are identical to the sample on both toolchains.

### Verdict (honest)

**Within noise on both toolchains, on every metric.** benchstat reports `~` for
sec/op (p=0.126), B/op (p=0.900) and allocs/op (p=1.000). No regression; no
measurable win.

Read the baseline's ±64% honestly: two of its six runs (1268 µs and 1483 µs)
overlapped the full test suite, golangci-lint and govulncheck, which ran on
the same box during the baseline. The other four baseline samples (840–953 µs)
sit exactly where the five after-samples sit (826–938 µs). The after-run
executed alone. A load confound that only inflates the *baseline* could
manufacture a false win for 1.27; it did not — benchstat still calls the
comparison `~`, and the unloaded samples of A and B are indistinguishable.

This is the same shape as the 1.26 gate's Green Tea result: HNSW search has
~19 allocs/op and ~10.5 KiB/op left, so an allocator improvement (1.27's
size-specialised small-object paths) has little to bite on here. The gain
1.27 offers this repo is correctness of tooling (one toolchain everywhere,
eight stdlib advisories closed, a compiling archsimd smoke), not this number.

## Security — govulncheck under go1.27.0

The 1.26.4 pin existed to close GO-2026-5037 and the `ReadMIMEHeader` advisory
(security audit H-9). Result under 1.27.0:

Same `govulncheck ./...`, two clean worktrees (no gitignored `enterprise-plugins`
directory, which breaks package loading): one at unpatched `main` (a980270)
under `GOTOOLCHAIN=go1.26.4`, one with this branch applied under go1.27.0. One
variable changed: the toolchain.

| Reachable from graphdb code | go1.26.4, `main` | go1.27.0, this branch |
|---|---|---|
| Standard-library advisories | **8** — GO-2026-6218 `net/url`, 6091 `html/template`, 6090 `crypto/tls`, 6089 `net/http`, 6088 `encoding/xml`, 5972 `encoding/asn1`, 5856 `crypto/tls`, 5026 `net/http` — every one "fixed in go1.26.5 / go1.26.6" | **0** |
| Dependency advisories | 4 | 4 — the identical set |
| "Your code is affected by …" | 12 | 4 |

Two consequences:

1. **The 1.26.4 pin was already stale.** go1.26.6 had shipped eight stdlib
   fixes the pin did not carry. The H-9 rationale (pin the toolchain to close
   reachable stdlib advisories) was right; the pinned number had simply aged.
   1.27.0 carries all eight. A toolchain pin is a claim with an expiry date —
   worth a scheduled govulncheck, which `fuzz.yml`'s nightly shape could host.
2. **Four dependency advisories are reachable on both toolchains and are
   untouched by this PR** (deliberately — a dependency bump is its own change
   with its own review):
   - GO-2026-5004 `github.com/jackc/pgx/v5` v5.7.6 → v5.9.2 — SQL injection via
     placeholder confusion; reachable from `pkg/licensing` `PGStore.ListLicenses`.
   - GO-2026-5158 `go.opentelemetry.io/otel` v1.43.0 → v1.44.0 — reachable from
     `pkg/api` `tracingMiddleware` baggage extraction.
   - GO-2026-6061 `google.golang.org/grpc` v1.80.0 → v1.82.1 (indirect, via the
     OTLP gRPC exporter).
   - GO-2026-5970 `golang.org/x/text` v0.35.0 → v0.39.0 (indirect).

   Follow-up: one `go get` PR for the four, with govulncheck's "affected by 0"
   as its acceptance line.

## Gate exit checklist

- [x] `go.mod` on `1.27.0`; `go build` + `go vet` over `.`, `pkg`, `cmd`, `examples`
  clean on go1.27.0; the new default `stdversion` vet check reports nothing.
- [x] `go test -short` — 57 packages `ok`, exit 0; `pkg/vector` green under `-race`.
- [x] golangci-lint v2.13.2 (built with go1.27.0) — `0 issues`; `config verify` OK.
- [x] `gofmt -s -l ./pkg ./cmd` empty; `make contract-guard` OK (14 contracts, 21
  guarding tests); `go mod tidy -diff` empty and `go mod verify` OK in a clean tree.
- [x] SIMD smoke: red on 1.27 before the rename (two errors, quoted above), green
  after, on real linux/amd64; SKIP without the experiment; 1.26.4 refuses the module.
- [x] govulncheck A/B recorded — 8 reachable stdlib advisories on 1.26.4, 0 on 1.27.0;
  4 dependency advisories unchanged (follow-up).
- [x] Pre/post benchstat recorded — storage reads `~` on all three; HNSW verdict above.
- [ ] **CI green on the PR — NOT push-validated at the time of writing.** The box
  that produced this document was scheduled to shut down at 21:27 AEST on
  2026-09-03, minutes after the PR opened. First action next session: read the
  checks on the PR (`gh pr view --json statusCheckRollup`), especially `lint`
  (new golangci-lint pin), `simd-smoke`, and the `go-client` workflow.

## Discovered consequence — the local golangci-lint

The dev box's `golangci-lint` (v2.12.2, built with go1.26.3) now refuses the
module before any analyser runs:

```
Error: can't load config: the Go language version (go1.26) used to build golangci-lint is lower than the targeted Go version (1.27.0)
```

`make lint-local` dies the same way (exit 2). Until this bump `CLAUDE.md`
described that binary as "partly usable — nine of eleven linters"; that
description is now false on any box that has not upgraded. `CLAUDE.md` is
corrected in this PR.

Fix, either form:

```
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...   # CI's exact linter, no install
```

v2.13.2 built with go1.27.0 typechecks the whole module, so all eleven CI
linters run locally — govet and staticcheck included. On this branch: `0 issues`.
The "nine of eleven" ceiling in `scripts/lint-local.sh` was a symptom of the
build-Go-vs-target-Go gap, not a property of the tool. What still holds the
script to nine is the config it generates from `.golangci.yml`, which removes
govet and staticcheck from the `linters.enable` block; widening that is a
separate, small change.
