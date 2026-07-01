# Session handoff — 2026-07-01 02:34 UTC

**Date**: 2026-07-01 (single session, 3 PRs merged — the v1.1 "Validate & observe" track, minus the coi-screen corpus run)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Three of the four v1.1 coord-seed tasks are done: the JSON↔mmap equivalence oracle is now property-based/fuzzed, the api handler fuzz targets are re-enabled, and OpenTelemetry tracing (off by default) + SLO/SLI docs shipped. The only v1.1 task left is the **coi-screen ~814K ICIJ corpus validation** — the one that answers decision **B-1** and completes the gate for **v1.2 (mmap-default)**.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #440 | `test(storage): property-based/fuzzed JSON<->mmap equivalence oracle` | Sharpened the fingerprint to raw `Data` bytes (was lossy `v.String()`); added `TestMmapReopen_ValueTypeParity` (all 12 `ValueType`s, was String+Int only) + `FuzzMmapReopenParity` native fuzz target. 90s fuzz / ~64k execs found no mmap≠JSON divergence. Teeth-checked (1-bit mmap corruption caught). |
| #441 | `test(api): re-enable HTTP-handler fuzz targets; drop obsolete .disabled cruft` | **Discovery:** `pkg/query/fuzz_test.go` was already ACTIVE — its `.disabled` twin was stale cruft (deleted). Only `pkg/api` genuinely needed re-enabling. Body-JSON targets call handlers directly with tenant ctx; path/header targets use the raw mux. Teeth-checked. |
| #442 | `feat(observability): OpenTelemetry tracing + SLO/SLI docs (v1.1)` | The query layer already emitted spans with no provider configured — this makes them observable. New `pkg/tracing` (OTLP+stdout, off by default), `tracingMiddleware`, `cmd/server` wiring, `docs/OBSERVABILITY.md`. Design spec at `docs/superpowers/specs/2026-07-01-otel-tracing-design.md`. |

## Current state

- **`origin/main` HEAD**: `356a7e8` (#442).
- **Open PRs**: none (before this handoff PR).
- **Open branches**: `main` only (plus a stale local `main-prerebase-backup` unrelated to this session — safe to delete).
- **Uncommitted changes**: none.
- **Test/lint state**: `pkg/tracing`, `pkg/api`, `pkg/storage`, `pkg/query` green; `go vet` + `gofmt` clean; CI golangci-lint + macOS test + go.mod-tidy all green on the merged PRs.

## What's next

Remaining v1.1 (`ROADMAP_post_1.0.md` v1.1.0 "Validate & observe" + `coord/seed/graphdb.json`):

1. **`v1.1-coi-screen-validation`** — exercise mmap end-to-end on a synthetic ~814K ICIJ corpus. Local tooling exists: `scripts/gen-icij-synth.py`, `cmd/import-icij`, `bin/import-icij`, `ICIJ_BENCHMARK_RESULTS.md`. This is the single highest-leverage remaining item — it empirically answers **B-1** (is full-graph `GetAllNodesForTenant`-on-reopen a hot path?) and provides the real-consumer validation that is the stated precondition for **v1.2 mmap-default** and **v1.5 DoD Levers 2–3**.

After v1.1 closes: **v1.2.0 — mmap by default** (flip `UseMmapSnapshot` default to opt-out), gated on v1.1.

New gaps surfaced this session (not yet on the planning doc):
- OTLP exporter uses **insecure gRPC** when an explicit endpoint is set (documented in `OBSERVABILITY.md`). A future task could add a TLS toggle for direct-endpoint production use.
- Storage-op spans are explicitly out of scope of #442 (only HTTP + query spans exist). Candidate follow-up if tracing adoption wants storage-level visibility.

## Stale assumptions to retire

- **`coord/seed/graphdb.json`** lists `v1.1-mmap-oracle-property-based`, `v1.1-reenable-fuzz-tests`, `v1.1-otel-tracing` as `"status": "pending"`. All three are now **done** (#440, #441, #442). Next session should mark them done in coord (via `coord on-merge`/`coord claim`+`release`) — the seed file is declarative initial state, so update coord, not necessarily the seed.
- **`docs/ROADMAP_post_1.0.md`** v1.1.0 section lists four deliverables (coi-screen, oracle-harden, OTel, fuzz-reenable). Three are done; only coi-screen remains. Update the roadmap's v1.1 status.
- **The planning docs implied BOTH `pkg/query` and `pkg/api` fuzz tests needed re-enabling.** Not true — `pkg/query/fuzz_test.go` has been active for months; only the `.disabled` cruft needed removing (done #441). Only `pkg/api` was genuinely disabled.
- **`docs/internals/design/NEXT_SESSION_PROMPT.md`** previously pointed at the ask-#1 Stage-2 stack (long merged). This handoff overwrites it.

## Open questions for the user

- **B-1**: is full-graph enumeration-on-reopen a real consumer hot path? Unresolved — the coi-screen run is designed to answer it. Gates v1.2/v1.5.
- **OTLP TLS**: is insecure-gRPC-on-explicit-endpoint acceptable for the intended tracing deployments, or should a TLS toggle be added before anyone points it at a non-local collector?

## Next-session prompt (paste-ready)

```
Pick up v1.1-coi-screen-validation: exercise mmap mode end-to-end on a synthetic
~814K ICIJ corpus (scripts/gen-icij-synth.py + cmd/import-icij) to empirically
answer decision B-1 (is full-graph GetAllNodesForTenant-on-reopen a hot path?)
and provide the real-consumer validation gating v1.2 mmap-default. While running
it, VALIDATE the new observability shipped this session — enable tracing
(GRAPHDB_TRACING_ENABLED=true, OTEL_TRACES_EXPORTER=console) and confirm query
spans nest under request spans on a real workload; sanity-check the SLI PromQL in
docs/OBSERVABILITY.md against /metrics. Pre-flight: `go build ./pkg/... ./cmd/...`
(NOT `./...` — the gitignored enterprise-plugins/ breaks it; see §"go mod tidy"
below). Then mark v1.1-mmap-oracle-property-based / -reenable-fuzz-tests /
-otel-tracing done in coord + update ROADMAP_post_1.0.md v1.1 status. End via the
session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/ROADMAP_post_1.0.md` (v1.1 section) and `docs/NEXT_STEPS_2026-06-18.md`.
3. `CLAUDE.md` § "Orient first" is auto-loaded.
4. **go mod tidy / build gotcha**: the local working tree has a gitignored `enterprise-plugins/` dir (stale `cluso-graphdb` module alias + un-vendored aws-sdk) that breaks `go mod tidy` and `go build ./...` **locally** while CI (public checkout, no enterprise-plugins) is unaffected. To tidy correctly, tidy a tracked-only copy: `rsync -a --exclude=.git --exclude=enterprise-plugins ./ <tmp>/ && cd <tmp> && go mod tidy`, then copy `go.mod`/`go.sum` back. Build with `./pkg/... ./cmd/...`, not `./...`.
5. If picking up coi-screen: read `docs/internals/design/ICIJ_OFFSHORE_LEAKS_BENCHMARK.md` + `ICIJ_BENCHMARK_RESULTS.md`.
