# Session handoff — 2026-06-11 23:55 UTC

**Date**: 2026-06-11 (single session, "finish Track S": 4 PRs merged, 2 in flight, 2 client releases)
**Outgoing model**: Claude Fable 5
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Track S (security) is **closed end-to-end**: M-14, M-1 (Option A, user-approved), M-15 OSS-side, the M-7 revoke endpoint, and H-3 all merged (#395–#399), planning doc reconciled (#400); first client releases cut (`python-sdk/v0.1.0`, `ts-client/v1.0.0`). Along the way M-1's tests surfaced and fixed two serious pre-existing bugs: the compressed WAL had **zero crash durability**, and `Snapshot()` was marshal-unsafe under concurrent writers.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #395 | M-14 snapshot envelope + construction-time encryption | Also fixed: encrypted snapshots could NEVER load at restart (`SetEncryption` ran after the constructor's load). `StorageConfig` gains `EncryptionEngine`/`KeyManager`; new `DefaultStorageConfig`. |
| #396 | M-1 WAL checkpoint compaction (Option A, all 3 backends) | `TruncateUpTo(lsn)` + `CompactWAL` + tenant-delete wiring. Surfaced + fixed: (a) compressed WAL never written by single-op paths NOR replayed (zero durability); (b) snapshot referenced live maps, marshal-after-RUnlock raced writers (mapEncoder panic); (c) `Transaction.Commit` apply-then-append window closed via `txWALBarrier`. |
| #397 | M-15 OSS-side plugin SHA-256 manifest verification | Fail-closed pre-`plugin.Open`; `GRAPHDB_PLUGIN_ALLOW_UNVERIFIED=true` opt-out. Enterprise manifest tooling still open (cross-repo). |
| #398 | M-7 follow-up: `POST /api/users/{id}/revoke-tokens` | Admin surface over #390's generation counter. |
| #399 | H-3 WAL-payload encryption | Sealed via the snapshot engine (`GWE1` marker) at all 4 append dispatchers; legacy plaintext replays + one-time `CompactWAL` purge on encryption toggle; ciphertext-without-engine fails loud. |
| #400 | Planning doc: Track S closed | `NEXT_STEPS_2026-06-03.md` reconciled — closes the skill loop. |
| — | Releases: `python-sdk/v0.1.0`, `ts-client/v1.0.0` | First tagged releases (tags + GH releases only, user-decided; no PyPI/npm). First-release framing dissolved the TS semver question. |

## Current state

- `origin/main` HEAD: `6423188` (#400). All of this session's PRs are merged.
- **Open PRs (this session):** **#401** — this handoff (the user's merge is the session-end signal).
- **Open PRs (PRIOR session, need disposition — see Open questions):** **#393** M-1 *Option C* interim (`pkg/compliance` GDPR Art-17 control), **#394** its planning-doc note, **#389** session handoff 2026-06-10.
- Local branches: this session's merged branches already cleaned (`--delete-branch`); remaining locals all back still-open PRs (`fix/m1-gdpr-erasure-interim`, `docs/m1-option-c-shipped`, `docs/session-handoff-2026-06-10-0647Z`, this handoff's branch) — clean after disposition.
- Uncommitted: none (`.claude/scheduled_tasks.lock` untracked, pre-existing).
- **Git stash note:** a pre-existing stash was accidentally popped mid-session and immediately re-saved intact as `stash@{0}` ("restored: pre-existing stash accidentally popped during M-1 rebase"); the gemini-bulk stash is untouched at `stash@{1}`.
- Tests: full `pkg/storage` (~120–170s), `pkg/api`, `pkg/wal`, `pkg/auth`, `pkg/plugins` suites green; race-detector clean on the new WAL/compact/encryption surfaces; `golangci-lint run ./...` 0 issues. Note: the storage suite now exceeds CLAUDE.md's `-timeout 90s` guidance — use 300s.

## What's next

1. **Disposition #393/#394/#389** (see Open questions).
2. **No critical path is forced.** Candidates per `NEXT_STEPS_2026-06-03.md` § How-to-use item 6: productization/operability wave (recommended — never had a wave; onboarding docs, single-node framing, FTS/LSA bootstrap policy), real-corpus coi-screen run (Milestone-1-proper; also answers the persist-HNSW question), GraphQL index-level pagination, batched-WAL default sweep, CI hygiene (`cmd/...` test allowlist, gofmt lint gap).
3. **Track S tail (small):** M-15 enterprise manifest-generation tooling (cross-repo, graphdb-enterprise); PyPI-publish decision.

### Gaps surfaced, not yet on the planning doc
- Benchmark M-1's snapshot deep-copy cost at scale (the perf reviewer suggested a `Snapshot()` duration bench at 10K/100K/500K nodes) — snapshot is rare-path, so this is monitoring, not a regression fix.
- CLAUDE.md's `go test ./pkg/<area>/ -short -timeout 90s` guidance is stale for `pkg/storage` (suite runs ~120–170s).

## Stale assumptions to retire

- **`NEXT_STEPS_2026-06-03.md` §§ Track S / How-to-use item 6** — "M-1 awaiting A/C decision … Remaining: H-3, M-14, M-15" → all shipped (#395–#399). **Fixed by #400** (merge it).
- **Memory `project_security_audit_2026_06_10`** ("3-wave backlog … design-required") → Track S is closed; only the enterprise-side M-15 tooling + PyPI decision remain.
- **CLAUDE.md § Common workflows** — `-timeout 90s` for package tests → `pkg/storage` needs ~300s now.
- **Any claim the compressed WAL backend is durable** — it never was until #396; if a consumer note assumed `EnableCompression` was production-usable pre-#396, it was wrong.
- **`DESIGN_m1_wal_remanence_2026-06-10.md`** "Decision requested" → resolved: Option A, all 3 backends, shipped #396.

## Open questions for the user

1. **#393 / #394 (prior session's M-1 Option C):** Option A (#396) supersedes the interim posture — erasure is now immediate, so #393's "honest remanence-window" compliance text is factually outdated. Options: (a) close both as superseded; (b) rework #393's `pkg/compliance` GDPR Art-17 control to assert the NEW immediate-erasure posture (the control itself may still be worth having — only its wording is stale). Recommend (b) as a small follow-up, closing #394 outright.
2. **#389 (2026-06-10 session handoff PR):** still open; merge it as historical record (it predates this one) or close.
3. **PyPI publish** for the Python SDK — still open (release shipped as tags-only per your call).

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (same content).

```
Read docs/internals/design/SESSION_HANDOFF_2026-06-11-2355Z.md first.
1. Resolve the handoff's Open Questions: disposition #393/#394 (M-1 Option-C interim, superseded by #396's Option A — recommend reworking #393's pkg/compliance control to the new immediate-erasure posture, closing #394) and #389 (old handoff PR); then branch-cleanup.
2. No critical path is forced — recommend opening the productization/operability wave (onboarding docs + single-node framing), with the real-corpus coi-screen run scheduled early as the likeliest source of new evidence.
3. Track S tail when convenient: M-15 enterprise-side manifest tooling (cross-repo), PyPI-publish decision.
End via the session-handoff skill.
```
