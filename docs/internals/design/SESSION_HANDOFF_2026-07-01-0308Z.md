# Session handoff — 2026-07-01 03:08 UTC

**Date**: 2026-07-01 (single long session, 5 PRs merged + 1 closed; **completes the v1.1 track** and seeds a v2.0 thread)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-07-01-0234Z.md` (written mid-session after the first 3 PRs; the session continued past it).

## TL;DR

**v1.1 "Validate & observe" is done — all four tasks merged, and decision B-1 is answered: full-graph enumeration is NOT a coi-screen hot path.** The v1.2 (mmap-default) gate is now satisfied. A bonus experiment validated the snapshot ship-and-serve hydration primitive (~7.4ms map at ICIJ scale), seeding a v2.0 "snapshot-based replica hydration" thread.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #440 | property-based/fuzzed JSON↔mmap oracle | raw-byte fingerprint + all-12-type parity + native fuzz target; teeth-checked |
| #441 | re-enable HTTP-handler fuzz targets | `pkg/query` `.disabled` was stale cruft (live twin existed) — deleted; only `pkg/api` genuinely re-enabled |
| #442 | OpenTelemetry tracing + SLO/SLI docs | query spans already existed but no provider was configured; wired provider (off by default) + HTTP root-span middleware + `docs/OBSERVABILITY.md` |
| #444 | coi-screen mmap validation | **answered B-1 = NO**; mmap reopen ~1370× cheaper at ~937K; real cost is the label-bucket resolve scan, not enumeration → DoD Levers 2–3 deprioritized |
| #445 | snapshot ship-and-serve hydration gate + design spike | shipped-snapshot hydration is position-independent, ~7.4ms map at ICIJ scale, byte-identical → v2.0 cluster-bootstrap seed |
| ~~#443~~ | ~~session handoff (0234Z)~~ | **closed as redundant** — its files landed on main via #444 (coi branch was inadvertently stacked on the handoff branch); content was byte-identical |

## Current state

- **`origin/main` HEAD**: `9792569` (#445). *(This closeout PR — roadmap reconciliation + this handoff — is separate and pending.)*
- **Open PRs**: the closeout PR only (roadmap + handoff). No code PRs open.
- **Open branches**: `main` (+ a stale local `main-prerebase-backup`, unrelated, safe to delete).
- **Uncommitted changes**: none.
- **Test/lint**: all merged PRs CI-green (golangci-lint, macOS test, go.mod-tidy). `go vet` + `gofmt` clean.

## What's next

**v1.1's gates are cleared, so the headline perf win is unblocked:**

1. **v1.2.0 — mmap by default** (recommended next). The gate (property-based oracle #440 + real-consumer validation #444) is satisfied. Flip `UseMmapSnapshot` to opt-out, add deploy-ordering docs. Also do the small **`cmd/import-icij` mmap opt-in** (surfaced by #444; unblocks the coi-screen consumer runbook). Size S–M, risk medium (default-behavior change).

2. **v2.0 snapshot-based replica hydration** (design thread, seeded by #445). The primitive is validated; the next de-risk is the **delta-tail/freshness** gap (a snapshot is point-in-time — a replica needs the writes since). See `SPIKE_SNAPSHOT_HYDRATION_2026-07-01.md`.

New follow-ups surfaced this session (now in the roadmap):
- `cmd/import-icij` mmap opt-in (v1.2 bullet).
- A name/property index on the label bucket is the real coi read-path win if latency ever matters (v1.5 redirect) — not DoD Levers 2–3.

## Stale assumptions to retire

- **coord tasks still show `pending`.** The v1.1 tasks (`v1.1-mmap-oracle-property-based`, `-reenable-fuzz-tests`, `-otel-tracing`, `-coi-screen-validation`) are all **done** but the coord daemon was **not reachable this session**, so they weren't marked. Mark them done when coord is up (`coord on-merge`/`coord claim`+`release`). coord-claim was again **not** exercised end-to-end (the standing ask from the 0234Z prompt still stands).
- **`ROADMAP_post_1.0.md`** — updated in this closeout PR: v1.1 ✅, v1.2 gate satisfied, v1.5 reframed (enumeration not hot → ecosystem work), v2.0 hydration thread added. If reading a cached copy, use the updated one.
- **v1.5 "Scale the read path" is no longer a DoD-Levers refactor.** #444 showed enumeration isn't a coi hot path; Levers 2–3 optimize a path the validated consumer doesn't use.

## Open questions for the user

- **v1.2 vs v2.0-hydration next?** Recommend v1.2 mmap-default (now unblocked, headline single-node win) before the v2.0 hydration thread.
- **OTLP TLS** (from #442): insecure-gRPC-on-explicit-endpoint is the current default — acceptable, or add a TLS toggle before pointing at a non-local collector?

## Next-session prompt (paste-ready)

```
v1.1 is fully done (B-1 answered: enumeration is NOT a coi hot path; mmap-default gate
satisfied). Pick up v1.2.0 — mmap by default: flip StorageConfig.UseMmapSnapshot to
opt-out (JSON stays available), add deploy-ordering/index-build operability docs, and
give cmd/import-icij an mmap opt-in (honor GRAPHDB_STORAGE_MODE or add --mmap; also
unblocks the coi-screen consumer runbook). Keep the #440 property-based oracle as the
correctness gate. Pre-flight: build with `go build ./pkg/... ./cmd/...` (NOT `./...` —
gitignored enterprise-plugins/ breaks it locally; CI is fine). First housekeeping: mark
the four v1.1 coord tasks done once the coord daemon is reachable (it was down this
session). End via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/ROADMAP_post_1.0.md` (v1.1 ✅ / v1.2 sections) + `docs/internals/design/SPIKE_COI_SCREEN_VALIDATION_2026-07-01.md` for the B-1 evidence.
3. `CLAUDE.md` § "Orient first" is auto-loaded.
4. **Local build gotcha:** the gitignored `enterprise-plugins/` breaks `go mod tidy` / `go build ./...` locally (CI unaffected). Build `./pkg/... ./cmd/...`; tidy a tracked-only copy. (Also in memory: `enterprise-plugins-breaks-local-gomod`.)
