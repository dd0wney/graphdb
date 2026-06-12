# Session handoff — 2026-06-10 06:47 UTC

**Date**: 2026-06-10 (one very long session: CI hygiene → full security re-audit → Waves 1+2+executable-Wave-3 of the backlog; ~19 PRs merged. Supersedes the earlier 04:56Z handoff, which was closed.)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Commissioned a full-surface **security re-audit** (`AUDIT_security_2026-06-10.md` — no live cross-tenant exposure; 11 High / 16 Medium / 10 Low) and shipped **all of it that's executable without a design decision**: Wave 1 (server hardening), Wave 2 (client release), and Wave 3's no-decision items. **All 11 Highs + all targeted Mediums are addressed** (M-7 #390 was the last). What remains is design-gated: the **M-1 decision** (Option A/C — spike doc #391 awaiting it; gates H-3), then M-14 (snapshot-format bump) and M-15 (cross-repo), + cutting a versioned client release. Also cleared two long-standing CI gaps (benchmark 403, never-deployed docs site). `main` clean at `4f78fd4`.

## What's done this session

**Phase 0 — CI hygiene + audit setup**

| PR | What |
|---|---|
| #365 | merged the stranded prior session handoff |
| #368 | Node-24 action migration (beat the 2026-06-16 deadline); **fixed the benchmark comment-step 403** that made every PR `UNSTABLE`; pinned gosec off `@master`; restored lint-job Go cache; added `.github/dependabot.yml` (→ open PR #370) |
| #369 | retired the "UNSTABLE is normal" guidance in `CLAUDE.md` + ci-status-triage skill + memory |
| #371 | the audit doc itself (6 parallel specialist passes + gosec/govulncheck) |

Also: **GitHub Pages enabled** (`build_type=workflow`) → docs site deploys green for the first time (was 50/50 failures), live at https://dd0wney.github.io/graphdb/.

**Wave 1 — server hardening (all 6 server Highs + 5 Mediums)**

| PR | Findings |
|---|---|
| #372 | H-1 suspended-tenant (`GetActive`) + M-5 tenant-ID validation/log-injection |
| #373 | H-7 HNSW caps + H-8 `/traverse` node cap |
| #374 | H-9 toolchain → go1.26.4 (govulncheck 2→0) |
| #375 | H-2 at-rest perms 0600/0700 + H-4 WAL record-size cap |
| #376 | M-3 GraphQL depth + M-4 pre-auth body cap + M-16 audit-log caps |
| #377 | M-6 mint-token default → viewer |
| #378 | M-13 SDK `CreatedAPIKey.key` `repr=False` |

**Wave 2 — client release**

| PR | Findings |
|---|---|
| #379 | Python SDK: H-10 path-traversal `quote_segment` (experimentally-confirmed `../admin`), M-10 cache namespace, M-11 429-not-on-POST, M-12 `trust_env=False`, L-9/L-10 |
| #380 | TS Workers client: H-11 cache identity namespace, M-11 idempotent-only retries, L-9 `redirect:'manual'` |

**Wave 3 — executable items (no design decision needed)**

| PR | Findings |
|---|---|
| #383 | M-8 OIDC: user-mgmt handlers via composite validator + `nbf` |
| #384 | M-2 tenant-delete fails on LSA-snapshot cleanup failure (remanence) |
| #385 | H-6 `context.Context` threaded through betweenness/edge-betweenness/scc/triangles/node-similarity-all (interface change, Option A approved) + deadline→408 |
| #386 | L-5 Cypher sanitizer SQL false-positives + L-7 float→int corruption near MaxInt64; **all 10 Lows dispositioned** in the audit doc |
| #387 | H-5 rate limiting **activated** (was never wired → both limiters nil → no protection incl. auth brute-force) + general on-by-default with `RATE_LIMIT_ENABLED=false` opt-out |
| #390 | **M-7** token revocation via per-user generation counter (JWT `gen` claim; bumped on revoke/password/role change — role-change bump also closes **AUTH-7**) |
| #391 | **M-1 design doc** (`DESIGN_m1_wal_remanence_2026-06-10.md`) — spike output, awaiting an A/C decision (see Open questions) |

| #381, #388, #392 | planning-doc Track S record (Waves 1-2, Wave 3, then M-7/M-1) |

Every fix PR is RED-against-pre-fix pinned. Full `pkg/api` / `pkg/storage` / `pkg/auth` / `pkg/algorithms` / SDK / TS suites green per PR; golangci-lint 0 issues; gofmt clean.

## Current state

- `origin/main` HEAD: **`4f78fd4`** (#392). M-7 (#390), the M-1 design doc (#391), #388/#392 planning, and #370 (dependabot) all merged after the initial audit waves.
- **Open PRs:** just **this handoff PR**. (#370 was the third-party-actions bump #368 deliberately left untouched — docker/*, golangci-lint-action, setup-uv, goreleaser, dockerhub-description — NOT redundant with #368; merged green. Its goreleaser/docker bumps are only exercised by an actual release, so the next release is their real test.)
- Open branches: `main` + this-handoff branch.
- Uncommitted changes: none (`.claude/scheduled_tasks.lock` is untracked session noise).
- **CI normal-state: green-is-green now** (the benchmark 403 is fixed). Any PR failure is net-new. The benchmark *job* still runs ~26 min and comments on same-repo PRs.

## What's next

Per `docs/NEXT_STEPS_2026-06-03.md` § Track S (updated by #388). Track S remains active; the executable work is done. Remaining, all in priority order:

1. **Decide M-1 (Option A or C)** — the `DESIGN_m1_wal_remanence_2026-06-10.md` doc (#391) is decision-ready. **A** = a `TruncateUpTo(lsn)` checkpoint across all 3 WAL backends (real durability-layer change, no data loss); **C** = document the window + defer to `Close()` (interim). This is the top blocker — it also gates H-3.
2. **Cut a versioned client release** bundling #379/#380. Needs a **semver judgment** (the TS M-11 retry change is arguably breaking at 1.x → 2.0.0, or 1.1.0-with-prominent-changelog) and the **PyPI-publish decision** (workflow armed, needs trusted-publishing). Python SDK is at `0.1.0`, TS at `1.0.0`.
3. **Remaining Wave 3 design-gated items (after the M-1 decision):**
   - **H-3** WAL payload encryption — entangled with M-1 + M-14 (encryption-toggle orphans plaintext WAL entries → needs M-1 compaction + an M-14 marker). Sequence after M-1.
   - **M-14** snapshot magic-header + version — snapshot-format version-bump discipline (decision).
   - **M-15** enterprise `.so` plugin hash/signature verification — cross-repo with graphdb-enterprise (coordination).
4. Off-Track-S (carried): GraphQL index-level pagination (offset→ID cursor migration), ctx-passing → auto-embedding (R2.5a), batched-WAL default sweep.

**Done since this handoff was first drafted:** M-7 (#390) — token revocation, also closed AUTH-7. **All 11 Highs + all targeted Mediums are now shipped.**

## Stale assumptions to retire

Already applied this session (so the next agent doesn't re-flag):
- `CLAUDE.md` § Known infra patterns + ci-status-triage skill row — benchmark-`UNSTABLE` pattern struck (closed #368); green-is-green. Current.
- `NEXT_STEPS_2026-06-03.md` — Track S added (#381) + Wave 3 progress (#388) + M-7/M-1 (#392). Current.
- Memory `project_ci_red_state_tolerated`, `project_security_audit_2026_06_10`, `MEMORY.md` — all refreshed to current state.
- `AUDIT_security_2026-06-10.md` — § L-tier disposition added (#386); the findings list itself is the as-audited record (not annotated per-PR — the planning doc carries done-state).

Nothing else known-stale.

## Open questions for the user

1. **M-1: Option A or C?** The `DESIGN_m1_wal_remanence_2026-06-10.md` doc (#391) requests this — A = the `TruncateUpTo` durability-layer change; C = document-window interim. Top blocker; gates H-3. Every other remaining Wave 3 item waits behind a similar decision.
2. **Cut the client release?** Needs the semver call (TS 1.x retry-behavior change) + the PyPI-publish decision. Prepared but not executed (version numbers are a public contract).

## Next-session prompt (paste-ready)

`main` is clean at `4f78fd4`; the 2026-06-10 security audit shipped **all 11 Highs + all targeted Mediums** (M-7 #390 was the last). L-tier dispositioned. Track S active. The remaining Wave 3 items are all decision-gated:
1. **Decide M-1 — Option A or C** (read `DESIGN_m1_wal_remanence_2026-06-10.md`): A = `TruncateUpTo(lsn)` checkpoint across the 3 WAL backends; C = document-window interim. Implement the chosen path (gates H-3); OR
2. **Cut the versioned client release** (#379/#380) — decide semver (TS retry change is arguably breaking at 1.x) + PyPI publish; OR
3. **M-14** (snapshot format-header bump) / **M-15** (plugin verification, cross-repo with graphdb-enterprise).
The audit doc's "consolidated confirmed-clean" + "L-tier disposition" sections are the start points — don't re-derive them. End the session via the `session-handoff` skill.

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-06-03.md` § Track S + `docs/internals/design/AUDIT_security_2026-06-10.md` (§ confirmed-clean, § Wave-3 findings, § L-tier disposition).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If cutting the release: `clients/python/pyproject.toml`, `workers/graphdb-client/package.json`, `.github/workflows/python-sdk-publish.yml`. If a Wave 3 spike: the finding's file:line in the audit doc.
