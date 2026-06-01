# Gate 0 — Go 1.26 Adoption + SIMD De-Risk Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adopt Go 1.26 (banking the transparent Green Tea GC / stack-allocation wins) and prove the experimental `simd/archsimd` intrinsics path works end-to-end on amd64, before any Tier-1 distance-kernel work commits to it.

**Architecture:** Three independent deliverables. (1) Measure the free runtime wins with a clean go1.25.3-vs-go1.26.3 A/B. (2) Bump `go.mod`. (3) A throwaway SIMD smoke kernel that isolates the experimental package behind a `goexperiment.simd` build tag so the default build stays green, with a differential test run on amd64. All `simd/archsimd` facts below were verified empirically against the installed `go1.26.3` on 2026-06-01.

**Tech Stack:** Go 1.26 (`go1.26.3`), `simd/archsimd` (experimental, `GOEXPERIMENT=simd`, **amd64-only**), `golang.org/x/perf/cmd/benchstat`, downloadable `go1.25.3` toolchain for the baseline.

**Empirically-verified facts this plan rests on:**
- `simd/archsimd` exists only under `GOEXPERIMENT=simd`; its own doc says *"It currently supports AMD64."* No arm64/NEON.
- Real API (confirmed via source): `archsimd.LoadFloat32x8Slice([]float32) Float32x8`, methods `.Add/.Mul/.Sub(Float32x8) Float32x8`, `.StoreSlice([]float32)`, CPU checks `archsimd.X86.AVX2()/.AVX512()/.FMA()`.
- `GOEXPERIMENT=simd` sets the `goexperiment.simd` build-constraint tag. A file tagged `//go:build amd64 && goexperiment.simd` compiles *only* with the experiment on; a complementary `//go:build !amd64 || !goexperiment.simd` fallback keeps default builds (amd64-without-experiment AND arm64) green. All three configs were build-checked.
- This dev box is `darwin/arm64`, so SIMD code **cannot run locally** — only cross-compile-check. Execution happens on linux/amd64 (Docker `--platform linux/amd64` locally, or CI's ubuntu/amd64 job).

---

## File Structure

- `go.mod` — bump the `go` directive (Task 2).
- `docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md` — Create: records the A/B benchmark deltas + smoke result (the Gate-0 exit evidence). (Tasks 1, 3, 4.)
- `pkg/vector/distance_simd_smoke_amd64.go` — Create: throwaway SIMD kernel, `goexperiment.simd`-gated. Deleted when Phase 1b lands the real kernel. (Task 4.)
- `pkg/vector/distance_simd_smoke_fallback.go` — Create: complementary fallback so default builds compile. (Task 4.)
- `pkg/vector/distance_simd_smoke_test.go` — Create: differential test (SIMD == scalar). (Task 4.)
- `.github/workflows/test.yml` — Modify: add a `GOEXPERIMENT=simd` amd64 smoke-test job. (Task 5.)

---

### Task 1: Capture pre-bump benchmark baseline (go1.25.3 runtime)

**Files:**
- Create: `docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md`

- [ ] **Step 1: Install the pinned baseline toolchain**

Run:
```bash
go install golang.org/dl/go1.25.3@latest
go1.25.3 download
go1.25.3 version   # expect: go version go1.25.3 <os>/<arch>
```

- [ ] **Step 2: Install benchstat**

Run:
```bash
go install golang.org/x/perf/cmd/benchstat@latest
```

- [ ] **Step 3: Capture the baseline (Green Tea GC OFF — genuine 1.25 runtime)**

`go.mod` is still `go 1.25.3` at this point, so the go1.25.3 toolchain builds it. Run from repo root:
```bash
go1.25.3 test ./pkg/vector/ -run='^$' -bench=BenchmarkHNSWSearch -benchmem -count=8 -timeout=300s | tee /tmp/baseline_vector.txt
go1.25.3 test ./pkg/storage/ -run='^$' -bench=. -benchmem -count=8 -timeout=600s | tee /tmp/baseline_storage.txt
```
Expected: each line like `BenchmarkHNSWSearch-8   ...   NN allocs/op`. Keep both files.

- [ ] **Step 4: Record the baseline in the results doc**

Create `docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md` with this skeleton (paste the actual numbers from Step 3):
```markdown
# Gate 0 — Go 1.26 adoption results (2026-06-01)

## Method
A/B: `go1.25.3` (baseline, Green Tea GC off) vs `go1.26.3` (Green Tea GC default on),
`go.mod` bumped 1.25.3 -> 1.26.0 between captures. Benchmarks unchanged. -count=8, benchstat.

## Baseline (go1.25.3)
<paste /tmp/baseline_vector.txt and /tmp/baseline_storage.txt summary lines>

## After (go1.26.3) — filled in Task 3

## SIMD smoke result — filled in Task 4
```

- [ ] **Step 5: Commit**

```bash
git add docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md
git commit -m "perf(gate0): record pre-bump (go1.25.3) benchmark baseline"
```

---

### Task 2: Bump go.mod to Go 1.26

**Files:**
- Modify: `go.mod` (the `go` directive, currently `go 1.25.3`)

- [ ] **Step 1: Bump the directive**

Edit `go.mod`: change `go 1.25.3` to `go 1.26.0`. Do not add a `toolchain` line (let it resolve to the installed `go1.26.3`).

- [ ] **Step 2: Verify the whole module builds on 1.26**

Run:
```bash
go build ./...
```
Expected: no output, exit 0.

- [ ] **Step 3: Verify tests still pass (changed-risk packages)**

Run:
```bash
go test ./pkg/vector/ -count=1
go test ./pkg/storage/ -short -count=1 -timeout=300s
go vet ./pkg/vector/ ./pkg/storage/
```
Expected: `ok` for both test runs; vet silent.

- [ ] **Step 4: Commit**

```bash
git add go.mod
git commit -m "chore(go): bump module to Go 1.26.0"
```

---

### Task 3: Capture post-bump benchmarks and record the free-win deltas

**Files:**
- Modify: `docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md`

- [ ] **Step 1: Capture with the 1.26.3 toolchain (Green Tea GC default ON)**

Run (default `go` is now go1.26.3):
```bash
go test ./pkg/vector/ -run='^$' -bench=BenchmarkHNSWSearch -benchmem -count=8 -timeout=300s | tee /tmp/after_vector.txt
go test ./pkg/storage/ -run='^$' -bench=. -benchmem -count=8 -timeout=600s | tee /tmp/after_storage.txt
```

- [ ] **Step 2: Compute the deltas with benchstat**

Run:
```bash
benchstat /tmp/baseline_vector.txt /tmp/after_vector.txt
benchstat /tmp/baseline_storage.txt /tmp/after_storage.txt
```
Expected: a table with `sec/op`, `B/op`, `allocs/op` old-vs-new columns and % deltas. (The win may be small or noisy — record it honestly either way; do NOT claim a win that benchstat marks `~`.)

- [ ] **Step 3: Paste the benchstat output into the results doc**

Fill the `## After (go1.26.3)` section of `GATE0_GO126_RESULTS_2026-06-01.md` with the benchstat tables. Add a one-line honest verdict (e.g., "Green Tea GC: −X% sec/op on HNSWSearch, B/op flat" or "within noise").

- [ ] **Step 4: Commit**

```bash
git add docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md
git commit -m "perf(gate0): record post-1.26 benchmark deltas (Green Tea GC)"
```

---

### Task 4: SIMD smoke kernel — prove archsimd works on amd64

This is a **throwaway** kernel whose only job is to de-risk the `simd/archsimd` path. TDD order: write the differential test, watch it fail (kernel missing), implement, watch it pass.

**Files:**
- Create: `pkg/vector/distance_simd_smoke_amd64.go`
- Create: `pkg/vector/distance_simd_smoke_fallback.go`
- Test: `pkg/vector/distance_simd_smoke_test.go`

- [ ] **Step 1: Write the failing differential test**

Create `pkg/vector/distance_simd_smoke_test.go`:
```go
package vector

import "testing"

// TestSIMDSmokeAddMatchesScalar de-risks the simd/archsimd path: an 8-wide
// float32 add via the experimental intrinsics must equal the scalar result.
// On non-amd64 or without GOEXPERIMENT=simd, simdSmokeSupported is false and
// the test is skipped (the kernel only exists on amd64 under the experiment).
func TestSIMDSmokeAddMatchesScalar(t *testing.T) {
	if !simdSmokeSupported {
		t.Skip("simd/archsimd unavailable: needs amd64 + GOEXPERIMENT=simd")
	}
	a := []float32{1, 2, 3, 4, 5, 6, 7, 8}
	b := []float32{8, 7, 6, 5, 4, 3, 2, 1}

	got := simdSmokeAdd8(a, b)

	for i := range a {
		want := a[i] + b[i]
		if got[i] != want {
			t.Fatalf("lane %d: simd=%v scalar=%v", i, got[i], want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (symbols undefined)**

Run:
```bash
go test ./pkg/vector/ -run TestSIMDSmokeAddMatchesScalar 2>&1 | head
```
Expected: build failure — `undefined: simdSmokeSupported` / `undefined: simdSmokeAdd8`.

- [ ] **Step 3: Write the amd64+experiment SIMD kernel**

Create `pkg/vector/distance_simd_smoke_amd64.go`:
```go
//go:build amd64 && goexperiment.simd

package vector

import "simd/archsimd"

// simdSmokeSupported reports that the real archsimd kernel is compiled in.
const simdSmokeSupported = true

// simdSmokeAdd8 adds two 8-element float32 slices using a single AVX2 vector
// add. Throwaway: deleted when Phase 1b lands the real distance kernel.
func simdSmokeAdd8(a, b []float32) [8]float32 {
	va := archsimd.LoadFloat32x8Slice(a)
	vb := archsimd.LoadFloat32x8Slice(b)
	var out [8]float32
	va.Add(vb).StoreSlice(out[:])
	return out
}
```

- [ ] **Step 4: Write the fallback so default builds stay green**

Create `pkg/vector/distance_simd_smoke_fallback.go`:
```go
//go:build !amd64 || !goexperiment.simd

package vector

// simdSmokeSupported is false when archsimd is unavailable (non-amd64, or
// amd64 without GOEXPERIMENT=simd). The smoke test skips in that case.
const simdSmokeSupported = false

// simdSmokeAdd8 is never called when simdSmokeSupported is false; it exists
// only so the package compiles on every target.
func simdSmokeAdd8(a, b []float32) [8]float32 {
	var out [8]float32
	for i := range out {
		out[i] = a[i] + b[i]
	}
	return out
}
```

- [ ] **Step 5: Verify the DEFAULT build stays green on both arches (no experiment)**

Run:
```bash
go build ./pkg/vector/                                  # this arm64 box
GOOS=linux GOARCH=amd64 go build ./pkg/vector/          # amd64 without experiment
```
Expected: both exit 0. The fallback compiles; `archsimd` is NOT imported in default builds.

- [ ] **Step 6: Verify the EXPERIMENT build compiles the real kernel (cross-compile, amd64)**

Run from this arm64 box:
```bash
GOOS=linux GOARCH=amd64 GOEXPERIMENT=simd go build ./pkg/vector/
```
Expected: exit 0 (compiles the archsimd file). It cannot be *run* here — see Step 7.

- [ ] **Step 7: Run the smoke test on linux/amd64 (Docker, Rosetta/qemu on M1)**

Run:
```bash
docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src golang:1.26 \
  env GOEXPERIMENT=simd go test ./pkg/vector/ -run TestSIMDSmokeAddMatchesScalar -v
```
Expected: `--- PASS: TestSIMDSmokeAddMatchesScalar`. (If Docker is unavailable locally, this is also covered by CI in Task 5 — that is the canonical gate.)

- [ ] **Step 8: Record the smoke result and commit**

Fill the `## SIMD smoke result` section of `GATE0_GO126_RESULTS_2026-06-01.md` with the pass output + the exact env (`amd64`, `GOEXPERIMENT=simd`, `go1.26.3`). Then:
```bash
git add pkg/vector/distance_simd_smoke_amd64.go pkg/vector/distance_simd_smoke_fallback.go pkg/vector/distance_simd_smoke_test.go docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md
git commit -m "perf(gate0): add throwaway archsimd smoke kernel (amd64) + differential test"
```

---

### Task 5: Wire the smoke test into CI (amd64 + GOEXPERIMENT=simd)

The canonical Gate-0 proof must be reproducible in CI, not just local Docker. Add an amd64 job that runs the smoke test with the experiment on.

**Files:**
- Modify: `.github/workflows/test.yml`

- [ ] **Step 1: Read the current workflow to match its style**

Run:
```bash
sed -n '1,60p' .github/workflows/test.yml
```
Note the existing job names, the `actions/setup-go` version, and how `go-version` is set, so the new job matches conventions.

- [ ] **Step 2: Add the SIMD smoke job**

Append this job under `jobs:` in `.github/workflows/test.yml` (align `go-version` to the value the other jobs use; `ubuntu-latest` is amd64):
```yaml
  simd-smoke:
    name: SIMD smoke (amd64, GOEXPERIMENT=simd)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.x'
      - name: Run archsimd smoke test
        env:
          GOEXPERIMENT: simd
        run: go test ./pkg/vector/ -run TestSIMDSmokeAddMatchesScalar -v
```

- [ ] **Step 3: Validate the workflow YAML locally**

Run:
```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/test.yml')); print('YAML OK')"
```
Expected: `YAML OK`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "ci(gate0): run archsimd smoke test on amd64 with GOEXPERIMENT=simd"
```

- [ ] **Step 5: Gate-0 exit checklist (record in the results doc, then commit)**

Confirm and tick in `GATE0_GO126_RESULTS_2026-06-01.md`:
- [ ] `go.mod` on 1.26.0; `go build ./...` + `pkg/vector` / `pkg/storage` tests green.
- [ ] Pre/post benchstat deltas recorded (honest verdict, even if "within noise").
- [ ] Smoke test PASS on amd64 (local Docker and/or the new CI job green).
- [ ] Default build green on arm64 AND amd64-without-experiment (archsimd not imported).

```bash
git add docs/internals/design/GATE0_GO126_RESULTS_2026-06-01.md
git commit -m "perf(gate0): record Gate-0 exit checklist"
```

---

## Self-Review

**Spec coverage** (against `PERF_SIMD_ROADMAP_2026-06-01.md` § Gate 0):
- "Bump go.mod 1.25.3 → 1.26" → Task 2. ✓
- "Measure free wins before any SIMD" → Tasks 1 + 3 (clean go1.25.3-vs-go1.26.3 A/B). ✓
- "De-risk: minimal simd kernel compiles + runs on amd64" → Task 4 (arm64 dropped vs the roadmap's original "both arches" — corrected by the 2026-06-01 empirical finding that archsimd is amd64-only; the roadmap doc must be amended to match). ✓
- "Exit gate: CI green + free-win numbers + smoke passes" → Task 5 Step 5. ✓

**Known correction the roadmap needs** (flagged, not silently dropped): the roadmap's Gate-0 exit gate says "both amd64 and arm64" and calls archsimd "first-class SIMD." Reality: amd64-only, experimental, `GOEXPERIMENT`-gated, not under the Go 1 compatibility promise. Amend `PERF_SIMD_ROADMAP_2026-06-01.md` (Gate 0 exit gate, R1/R2, mechanism note) in the same branch before this plan executes.

**Placeholder scan:** none — every code block is real and was compile-checked on go1.26.3; the only "<paste>" markers are deliberate slots for empirical numbers the engineer captures at run time.

**Type consistency:** `simdSmokeSupported` (const) and `simdSmokeAdd8(a, b []float32) [8]float32` have identical signatures across the amd64 and fallback files and the test. `archsimd.LoadFloat32x8Slice` / `.Add` / `.StoreSlice` match the verified API.
