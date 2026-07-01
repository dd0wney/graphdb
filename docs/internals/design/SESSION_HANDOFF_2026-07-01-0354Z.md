# Session handoff — 2026-07-01 03:54 UTC

**Date**: 2026-07-01 (one long session, 8 PRs merged + 1 closed; **completed both v1.1 and v1.2**)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-07-01-0308Z.md` (which covered only through v1.1). The 0234Z + 0308Z handoffs are now historical.

## TL;DR

**v1.1 "Validate & observe" AND v1.2 "mmap by default" are both fully done.** mmap-backed lazy reopen is now the default snapshot mode (~1370× cheaper reopen than JSON at ICIJ scale, backward-compatible, `GRAPHDB_STORAGE_MODE=json` opt-out). Decision B-1 was answered (enumeration is not a coi hot path). The next unblocked track is **v1.3.0 "Deploy anywhere."**

## What's done this session

| PR | Title | Track |
|---|---|---|
| #440 | property-based/fuzzed JSON↔mmap oracle | v1.1 |
| #441 | re-enable HTTP-handler fuzz targets | v1.1 |
| #442 | OpenTelemetry tracing + SLO/SLI docs | v1.1 |
| #444 | coi-screen mmap validation (**answered B-1**) | v1.1 |
| #445 | snapshot ship-and-serve hydration gate + design spike | v1.1 bonus → v2.0 seed |
| #446 | v1.1 close-out (roadmap + handoff) | v1.1 |
| #447 | **make mmap-backed lazy reopen the default** | v1.2 |
| #448 | import-icij mmap opt-in | v1.2 |
| ~~#443~~ | ~~mid-session handoff~~ | **closed** (redundant — absorbed into #444 via a branch-stack) |

## Current state

- **`origin/main` HEAD**: `ba17080` (#448). *(This v1.2 close-out PR — roadmap + this handoff — is separate and pending.)*
- **Open PRs**: the close-out PR only. No code PRs open.
- **Open branches**: `main` (+ stale local `main-prerebase-backup`, unrelated, safe to delete).
- **Uncommitted changes**: none.
- **Test/lint**: full `pkg/... cmd/...` suite green under the flipped mmap default (48 pkgs); all merged PRs CI-green (golangci-lint, macOS test, go.mod-tidy); race-clean on the reopen path.

## What's next

1. **v1.3.0 — Deploy anywhere** (recommended; no gate). **Helm chart + Terraform module** (the #1 "can't deploy on k8s" gap) + first-party **Go-native client** (rounds out Python + TS). CI: `gofmt` lint gate. Independent of the perf spine.

2. **v2.0 snapshot-based replica hydration** (design thread from #445). The primitive is validated (~7.4ms position-independent hydration); the next de-risk is the **delta-tail/freshness** gap. See `SPIKE_SNAPSHOT_HYDRATION_2026-07-01.md`.

Carry-forward / small follow-ups (in the roadmap):
- **CI `cmd/...` test-allowlist expansion** — the one v1.2 bullet not done (separable).
- A **name/property index on the label bucket** is the real coi read-path win if latency ever matters (from #444), not DoD Levers 2–3.

## Stale assumptions to retire

- **coord tasks show `pending`.** All four v1.1 tasks AND both v1.2 items are **done**, but the coord daemon was **not reachable this entire session**, so nothing was marked. Mark the v1.1 + v1.2 tasks done when coord is up. coord-claim was again not exercised end-to-end (standing ask).
- **`ROADMAP_post_1.0.md`** — updated in this close-out PR: v1.2 ✅ (both #447 + #448). v1.1 was marked done in #446. Use the updated copy.
- **mmap is no longer opt-in.** Any doc/memory implying "mmap is off by default / experimental" is stale as of #447 — it is the default; JSON is the opt-out. Encryption + disk-backed-edges still auto-fall-back to JSON (unchanged).
- **`cmd/import-icij` is no longer JSON-only** (#448) — it defaults to mmap with a `--storage-mode`/`GRAPHDB_STORAGE_MODE` opt-out.

## Open questions for the user

- **v1.3 vs v2.0-hydration next?** Recommend v1.3 (Deploy anywhere — the #1 adoption gap, no gate) before the v2.0 hydration thread.
- **OTLP TLS** (from #442, still open): insecure-gRPC-on-explicit-endpoint is the current default — acceptable, or add a TLS toggle before a non-local collector?

## Next-session prompt (paste-ready)

```
v1.1 and v1.2 are both fully done (mmap is now the default snapshot mode; B-1 answered).
Pick up v1.3.0 "Deploy anywhere" (no gate): a Helm chart + Terraform module (the #1
"can't deploy on k8s" gap) and a first-party Go-native client (rounds out Python + TS).
Brainstorm scope first (this is greenfield packaging work with real forks — chart shape,
values surface, client API surface). Pre-flight: build with `go build ./pkg/... ./cmd/...`
(NOT `./...` — gitignored enterprise-plugins/ breaks it locally; CI is fine). First
housekeeping: mark the four v1.1 + two v1.2 coord tasks done once the coord daemon is
reachable (it was down all of 2026-07-01). End via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/ROADMAP_post_1.0.md` (v1.1 ✅ / v1.2 ✅ / v1.3 sections).
3. `CLAUDE.md` § "Orient first" is auto-loaded.
4. **Local build gotcha:** gitignored `enterprise-plugins/` breaks `go mod tidy` / `go build ./...` locally (CI unaffected). Build `./pkg/... ./cmd/...`; tidy a tracked-only copy. (Also in memory: `enterprise-plugins-breaks-local-gomod`.)
