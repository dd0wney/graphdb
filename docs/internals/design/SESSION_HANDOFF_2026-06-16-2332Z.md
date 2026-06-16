# Session handoff — 2026-06-16 23:32 UTC

**Date**: 2026-06-16 (single session; repo cleanup + CI repair, then the full graphdb-ask-#1 reopen arc spike → format → productionized Stage 1)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

graphdb ask #1 ("make reopen of a large persisted store cheap") is **shipped through Stage 1**: a flag-gated, mmap-backed lazy-reopen storage mode is on `main`, taking reopen of a 937k-node/1.3M-edge store from **14.4s → 2.9s (5.0×, 0.18× rebuild)**. It is **off by default** (`GRAPHDB_STORAGE_MODE=mmap`); the JSON path is unchanged. Stage 2 (persist/lazy the derived indexes → reopen ~0) is the documented next step.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #405 | docs: session handoff — 2026-06-12 01:55 UTC | Pre-existing stale handoff PR; merged during the "clean up the project" task (it was mergeable+clean). |
| #407 | fix: repair main CI — legacy-snapshot decrypt flake + gofmt gate | Two causes: gofmt regression in `input_validation.go` (broke Lint + the Tests format-gate) and a **real flaky bug** — the legacy headerless snapshot loader guessed encrypted-vs-plaintext from the first byte; an AES-GCM nonce starting with `{`/`[` (~0.8%) mis-fired. Replaced with an AEAD authenticated-decrypt probe. |
| #408 | spike(storage): reopen-cost investigation + Stage-1 mmap/lazy prototype | Reproduced reopen≈rebuild (0.90×). Decisive finding: reopen is **allocation-bound** (75% of `loadFromDisk` is `json.Unmarshal`; of that, 78% is allocating 25.3M live objects, GC negligible). Prototype mmap reader: open ~0 / 7 allocs vs 10.5s. |
| #409 | feat(storage): production mmap snapshot format (Stage 1, Phase 0) | Promoted prototype → production format: header v2 + CRC over structural sections, metadata blob (property/vector indexes, stats, nextIDs, sticky keys), copy-on-read, field-scan decoders, gap-tolerant directory. No wiring. |
| #410 | feat(storage): productionize mmap lazy reopen (Stage 1, Phase 1) | Wired the mmap mode through the full `Storage` interface: `loadFromDiskMmap` (lazy values, eager index field-scan), shard maps as a copy-on-write overlay + sharded tombstones, `Snapshot`/`Close` write/merge `.mmap` + `munmap`. Routed every read + write path (node/edge CRUD, cascade, upsert, unique, enumerations, **WAL replay**, transaction, batch). Server flag added. |

Also (no PR): closed stale PRs #406 (its `feat/batch-create` content had been bundled into `main` via an accidental #407 base-drift — verified identical, closed cleanly), #239, #238; deleted their local branches; left 5 closed-PR remote feature branches and `main-prerebase-backup` untouched per user.

## Current state

- **`origin/main` HEAD**: `ef84cba` (#410).
- **Open PRs**: none.
- **Open branches**: `main` + `main-prerebase-backup` (local-only safety net from a prior rebase; user chose to keep it — delete when satisfied).
- **Uncommitted changes**: none (clean tree; the earlier loose `go.mod`/`go.sum` AWS-SDK WIP + `graphdb-server-*` binaries were discarded/removed during cleanup).
- **Test/lint state**: full `pkg/storage` suite green; **race-clean** on the mmap + batch + transaction paths; `go build ./pkg/... ./cmd/...`, `go vet`, `gofmt` clean. CI on #410 was all-green except the benchmark comment-step `UNSTABLE` (the known-tolerated state per `CLAUDE.md` § "Known infra patterns").

## What's next

**Critical-path (the obvious continuation):**
1. **Stage 2 — persist/lazy the derived indexes.** The Stage-1 mmap reopen's residual ~2.9s is the eager index field-scan in `loadFromDiskMmap` (label/type, per-tenant, adjacency). Serialize those into `snapshot.mmap` (or build them lazily on first use) so open approaches ~0 — the consumer's ~0.1s dream. Design notes + the prediction (0.18× → ~0) are in `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md`.

**Off-path / parallel options:**
- **Exercise mmap mode on a real consumer** before broadening it (it's only been validated via synthetic parity + the env-gated bench). Flip `GRAPHDB_STORAGE_MODE=mmap` on a real persisted store and confirm the reopen win + interface parity end-to-end.
- **Related asks from the original brief** (lower priority): #3 incremental durability (don't force a full snapshot on `Close()`; WAL-append + threshold snapshot) and #2 bulk delete-by-predicate (`DeleteNodesByPropertyForTenant`).
- The repo's standing planning checkpoint (`docs/NEXT_STEPS_2026-05-13.md` per `CLAUDE.md`) — reconcile it; this session's work was driven by the ask-#1 brief, not that doc.

**New gaps surfaced this session (not yet on the planning doc):**
- mmap mode + at-rest encryption is unsupported (falls back to JSON; mmap can't map ciphertext). A page/segment-decrypt path would be needed to combine them.
- mmap mode + `UseDiskBackedEdges` also falls back to JSON (adjacency lives in the edge LSM, not the in-memory maps the loader rebuilds).
- The internal `checkGraphInvariants` test helper assumes `nodeShards` holds every node — structurally incompatible with the lazy representation. mmap correctness is gated by **public-interface parity vs JSON** instead (see `mmap_reopen_test.go`). If Stage 2 or later wants invariant-checking in mmap mode, the checker must be taught to build ground truth via `forEachNodeUnlocked` (mmap-aware) rather than ranging shards directly.

## Stale assumptions to retire

- **`docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md`** — the doc's framing of Stage 1 as "recommended/prototype" is now superseded by the **"Stage 1 productionized — RESULT (2026-06-17)"** section at its end (note: that section is dated 2026-06-17 but the work merged 2026-06-16 23:26 UTC; the in-doc date is cosmetic). Treat Stage 1 as **done**; the live open item is Stage 2.
- **`CLAUDE.md` § "Snapshot format stability"** currently says the on-disk snapshot is "a flat `map[uint64]*Node`/`map[uint64]*Edge`" JSON. That's still true for the **default** path, but there is now a **second** on-disk format: `snapshot.mmap` (binary, mmap-able, magic `GMNP`, version 2) written when `UseMmapSnapshot` is enabled. A future CLAUDE.md edit should mention both formats and that mmap mode is plaintext-only + in-memory-adjacency-only. (Not done here — handoff doesn't edit CLAUDE.md.)
- Any memory/notes claiming "reopen ≈ rebuild / persisting doesn't help reopen cost" → corrected: reopen via mmap mode is now **0.18× rebuild**.

## Open questions for the user

- **Should mmap mode become the default** (or be enabled for specific deployments) once Stage 2 lands, or remain opt-in? Today it's strictly opt-in via env.
- **Stage 2 now, or validate Stage 1 on a real consumer first?** Both are defensible; validating first de-risks broadening the format before more is built on it.
- `main-prerebase-backup` — safe to delete now that Stage 1 shipped? (Its original 4 commits are equivalently on `main`.)

## Next-session prompt (paste-ready)

```
Pick up graphdb ask #1 Stage 2: persist/lazy the derived indexes so mmap reopen drops
from ~2.9s (eager index field-scan) toward ~0. Read docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md
first (the "Stage 1 productionized — RESULT" section + the Stage-2 staging notes), then
the mmap implementation: pkg/storage/mmap_snapshot_{format,loader,persist}.go and the
loadFromDiskMmap index build.

Before building: decide WITH THE USER whether to do Stage 2 now or first validate Stage 1
on a real consumer (flip GRAPHDB_STORAGE_MODE=mmap on a real persisted store, confirm the
reopen win + public-interface parity). The mmap mode has only been validated via synthetic
parity + the env-gated bench (TestMmapReopen_EndToEnd, run with GRAPHDB_REOPEN_BENCH=1).

Correctness gate for any mmap change = public-interface PARITY vs JSON mode
(mmap_reopen_test.go); checkGraphInvariants does NOT work in mmap mode. Keep the mode
off-by-default and the "no-op when mmapSnap == nil" discipline so the JSON path stays
unchanged. End via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` (the full ask-#1 arc + Stage-2 plan).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If picking up Stage 2: read `pkg/storage/mmap_snapshot_loader.go` (the index build to make lazy/persisted) and `pkg/storage/mmap_reopen_test.go` (the parity harness to extend).
