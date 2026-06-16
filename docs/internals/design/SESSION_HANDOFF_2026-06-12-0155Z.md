# Session handoff — 2026-06-12 01:55 UTC

**Date**: 2026-06-12 (single session: cleared the prior session's open-PR backlog, then opened the productization/operability first wave — 5 merges + 1 close, 1 planning-doc PR in flight)
**Outgoing model**: Claude Opus 4.8 (1M context) — session began on Claude Fable 5, model swapped mid-session
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The prior session left three open PRs (two stale handoffs + an M-1 interim) needing disposition; all are now resolved. With the board clear, this session opened the **productization/operability first wave** — a documentation/release-hygiene pass that finished the long-pending `cluso` → `graphdb` rename in shipped artifacts and corrected a README + docs set that had drifted badly from the code (the documented quickstart literally couldn't authenticate). No functional code beyond binary display strings.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #389 | Session handoff — 2026-06-10 06:47 UTC | Prior-session handoff, merged as historical record (merged **before** #401 so the newer prompt wins the singleton). |
| #401 | Session handoff — 2026-06-11 23:55 UTC | Prior-session handoff; its merge was the prior session's end-signal. Rebased onto main (`-X theirs` on `NEXT_SESSION_PROMPT.md`) so the 06-11 prompt survived #389's older copy. |
| #393 | GDPR Art-17 erasure control → immediate-erasure posture (M-1 follow-up) | **Reworked before merge.** A prior session shipped an Option-C *interim* control (`StatusPartial` + WAL-remanence note, gated on a new `SystemInfo.ImmediateErasure` flag). Option A (#396) made that stale, so the control is now `StatusCompliant` unconditionally (tenant-delete purges the WAL synchronously via `CompactWAL` — structural, not config), the never-wired flag dropped, and a test pins the honesty guard (node/edge-level deletes still carry a WAL window until next compaction). TDD: rewrote the test RED first. |
| #394 | (planning note for the Option-C interim) | **Closed, not merged** — superseded by Option A; its "honest remanence-window" text is factually outdated. |
| #402 | Finish the #335 rename + make onboarding docs factually accurate | Three parts: (1) **artifact rename** `cluso-server`/`cli`/`tui` → `graphdb-*` in goreleaser + Dockerfile (build outputs, entrypoint, non-root image user) + compose + `cmd/*` display strings (`cluso>` → `graphdb>` prompt, server log, TUI title, benchmark banners); (2) **README accuracy rewrite** — every data-endpoint curl lacked auth (all routes are behind `requireAuth`), retired ZeroMQ sections (pkg/replication removed in A8.1), fixed Go examples using pre-#335 module path + nonexistent APIs; (3) **docs sweep** — `HA_QUICKSTART.md` replaced with honest single-node status doc, `PRODUCTION_QUICKSTART` Step 3 endpoint fixes. Verified with a local `docker build`. **Breaking** for download scripts pinned to `cluso-*` asset names (effective next tagged release; README has a transition note). |
| #403 | CLAUDE.md corrections | `cmd/server` is the production server, not `cmd/graphdb` (a demo); `-timeout 90s` under-buys `pkg/storage` (~120–170s now) — annotated 300s exception. |

## Current state

- **`origin/main` HEAD**: `3d29061` (#403).
- **Open PRs**: **#404** — `docs(planning): record productization first wave (#402/#403) + M-1 control rework (#393)`. Single-file planning-doc update; CI in flight (benchmark jobs ~25 min). **Intended to merge when green** — reversible docs-only change. *If still open when the next session starts, merge it.*
- **This session's handoff PR** (this doc) is the session-end signal — the user's merge closes the session.
- **Open branches**: `docs/planning-productization-wave` (backs #404) and this handoff's branch; both clean after their PRs land. 34 stale remote branches were pruned this session (`git fetch --prune`).
- **Uncommitted changes**: none (`.claude/scheduled_tasks.lock` untracked, pre-existing).
- **Test/lint state**: `go build ./...` + `go build ./cmd/...` green; `golangci-lint run ./...` 0 issues; `pkg/compliance` suite green (the only Go package touched). #402/#403 carried full CI green through merge (incl. benchmarks).

## What's next

**No critical path is forced.** Track S (security) closed 2026-06-12; the productization wave's first pass shipped. Standing-queue candidates per `NEXT_STEPS_2026-06-03.md` § How-to-use items 6–7:

1. **Real-corpus `coi-screen` run** (Milestone-1-proper) — likeliest source of new graphdb evidence; also answers the persist-HNSW question. Q3 proved the bugs on a synthetic corpus; the real ~814K ICIJ corpus run is still pending (corpus absent locally).
2. **Remaining productization items** (this wave was a first pass): single-node framing beyond the README note, FTS/LSA bootstrap-policy docs, and the CI-hygiene pair — `cmd/...` outside the CI test allowlist (so `cmd/*` tests run locally only) + `golangci-lint` not flagging `gofmt` violations.
3. **GraphQL index-level pagination** — offset→ID-cursor migration; REST shipped in #366, GraphQL resolvers still clone the full set with opaque offset cursors.
4. **Batched-WAL default sweep** — now that group commit works on all paths, should `EnableBatching` default true? Needs a FlushInterval latency-vs-throughput sweep first.
5. **Track S tail (cross-repo / decision)**: M-15 enterprise-side plugin manifest-generation tooling (graphdb-enterprise repo); the PyPI-publish decision for the Python SDK.

### Gaps surfaced this session, not yet on the planning doc

- The README's **performance claims** (330K writes/sec, 15.31 bytes/node, etc.) were **carried forward unverified** — they predate substantial storage changes (sharding, WAL encryption). A re-benchmark-and-update is a clean future productization item.
- `docs/INTEGRATION_GUIDE.md` still documents the original **Cluso trust-scoring app** as the integration consumer (intentionally kept — only its repo paths were stale). If that consumer is dead, the whole guide may be retire-able.

## Stale assumptions to retire

- **Memory `project_security_audit_2026_06_10`** — already updated this session to reflect Track S CLOSED + the #393 control rework. No further action.
- **`NEXT_STEPS_2026-06-03.md` item 6** "productization … never had a wave" → corrected by #404 to "first wave opened 2026-06-12 — see item 7." (Merge #404 to make this live.)
- **`CLAUDE.md` § Repo layout** "`graphdb` is the main server" → corrected by #403: `cmd/server` is the server, `cmd/graphdb` is a demo. (Already merged.)
- **`CLAUDE.md` § Common workflows** `-timeout 90s` for package tests → corrected by #403 with a 300s exception for `pkg/storage`. (Already merged.)
- **Any doc/README claiming ZeroMQ/Primary-Replica replication or a 3-node HA cluster** — `pkg/replication` was retired in A8.1 (#129/#130/#133). #402 swept the README and `HA_QUICKSTART.md`; if other docs still reference it, they're stale.
- **Release artifacts named `cluso-server`/`cluso-cli`/`cluso-tui`** — renamed to `graphdb-*` in #402, effective at the **next tagged release**. Releases ≤ v0.4.1 still ship the old names (README documents the transition).

## Open questions for the user

1. **README pricing tiers ($49 Pro / $299 Enterprise)** — carried forward **unchanged** by #402; only the `[your-email]` contact placeholder was fixed (→ GitHub issues). Pricing is the standing productization decision from `CAPABILITIES_2026-05-10.md`. Decide whether to keep, change, or remove these before the next tagged release.
2. **PyPI / npm publish** for the Python SDK and TS client — still open (releases shipped as git tags + GitHub releases only, per your earlier call).
3. **M-15 enterprise manifest tooling** — the OSS-side SHA-256 plugin verification shipped (#397); the manifest-*generation* half lives in graphdb-enterprise and is untouched.

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (same content).

```
Read docs/internals/design/SESSION_HANDOFF_2026-06-12-0155Z.md first.
1. If #404 (planning-doc update) is still open, confirm green and merge it; then branch-cleanup.
2. No critical path is forced. Recommended: the real-corpus coi-screen run (Milestone-1-proper) — likeliest source of new graphdb evidence, also answers persist-HNSW. Pre-flight: the ~814K ICIJ corpus is NOT local; coi-screen is a private sibling repo (../coi-screen) consuming graphdb as an embedded library. Synthetic-corpus proof already exists (Q3, #287/#288).
3. Alternatives: finish the productization wave (single-node framing, FTS/LSA bootstrap docs, cmd/... CI test allowlist + gofmt lint gap), GraphQL index-level pagination, or the batched-WAL default sweep.
4. Resolve the open questions: README pricing decision, PyPI publish, M-15 enterprise tooling.
End via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-06-03.md` (items 6–7 for the live queue).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If picking up coi-screen: read memory `project_coi_screen_tool` and `project_q3_storage_persistence_bugs` for the consumer's state and the bugs Q3 already fixed.
