# Session handoff — 2026-06-10 04:56 UTC

**Date**: 2026-06-10 (single long session — CI hygiene, then a full security re-audit + Waves 1–2 of its backlog; 14 PRs merged)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Commissioned and executed a full-surface **security re-audit** (`AUDIT_security_2026-06-10.md`, verdict: no live cross-tenant exposure; 11 High / 16 Medium / 10 Low) and shipped **Waves 1 (server hardening) and 2 (client release)** of its backlog — every server-side High + both client Highs + all targeted Mediums, each TDD-pinned. Also cleared two long-standing CI gaps (the benchmark comment-step 403 and the never-deployed docs site). `main` is clean at `82a10e4`. Remaining audit work is **Wave 3 (design-required, each needs a `/spike`)** + an **L-tier cleanup** + cutting a **versioned client release**.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #365 | (prior) session handoff 2026-06-08 | Merged at session start — was stranded open. |
| #368 | ci: Node-24 action migration + scope workflow permissions | Beat GitHub's 2026-06-16 Node-20 force-migration. **Fixed the benchmark comment-step 403** (no `permissions:` block) that made every PR `UNSTABLE` — green now means green. Also: gosec pinned off `@master`, lint-job Go cache restored, **added `.github/dependabot.yml`** (monthly grouped actions) — already produced **open PR #370**. |
| #369 | docs: retire benchmark-comment-step tolerated-failure pattern | `CLAUDE.md` + `ci-status-triage` skill row updated; memory `project_ci_red_state_tolerated` refreshed. |
| #371 | docs(audit): security re-audit 2026-06-10 | The audit doc itself. Six parallel specialist passes + gosec/govulncheck. Consolidated confirmed-clean list is the start point for the next audit. |
| #372 | fix(api): enforce tenant status + validate tenant-ID override | **H-1** `withTenant` used `Get` (status-blind) → suspended tenants kept access; now `GetActive`. **M-5** tenant-ID override validated via new `tenant.ValidateTenantID` (closes CRLF log-injection + arbitrary keys). |
| #373 | fix(api): cap HNSW params + traversal result size | **H-7** `maxM`/`maxEfConstruction`/`maxEf`. **H-8** `MaxTraversalNodes` (a `var` so tests hit the cap on a 30-node graph, not 10k) + `X-Truncated`. |
| #374 | fix(build): pin toolchain to go1.26.4 | **H-9** govulncheck 2 reachable stdlib vulns → 0. go.mod `toolchain` directive + workflows pinned `1.26.4` (setup-go v6 `GOTOOLCHAIN=local`). |
| #375 | fix(storage): owner-only at-rest files + WAL record cap | **H-2** 0600/0700 across wal/storage/search/lsm — **the audit assumed `pkg/storage` was already 0600; it was 0644/0755** (corrected). **H-4** `maxWALRecordSize` 64 MiB before alloc → no 4 GiB restart-loop OOM. |
| #376 | fix(api): GraphQL depth limit, body cap, audit-log caps | **M-3** `ValidateQueryDepth` wired (existed, never called). **M-4** new `bodyLimitMiddleware` (64 KiB on `/auth/*` which input-validation skips). **M-16** audit-log `limit`/export caps. |
| #377 | fix(admin): mint-token defaults to viewer | **M-6** `--role` defaulted to admin; now viewer + admin-mint warning + usage-text fix. |
| #378 | fix(sdk): CreatedAPIKey.key repr=False | **M-13** one-time key was in the dataclass repr → logs/tracebacks. |
| #379 | fix(sdk): Python path encoding + cache/retry/proxy hardening | **H-10** `quote_segment` at all 20 sync+async path sites (the experimentally-confirmed `../admin` traversal). **M-10** cache namespace, **M-11** 429-not-on-POST, **M-12** `trust_env=False`, **L-9/L-10**. |
| #380 | fix(ts-client): cache namespace + idempotent retries | **H-11** Worker-global KV cache had no identity prefix. **M-11** idempotent-only retries. **L-9** `redirect:'manual'`. Note: no CI covers this package — verified locally (tsc + 52 tests + build). |
| #381 | docs(planning): record Track S | Single-file planning-doc update (the Track S section). |

Every fix PR is RED-against-pre-fix pinned. Full `pkg/api` (~111s) / `pkg/storage` (~441s) / SDK (173/2-skip) / TS (52) suites green; golangci-lint 0 issues; gofmt clean.

## Current state

- `origin/main` HEAD: **`82a10e4`** (#381).
- **Open PRs: #370** — dependabot "Bump the actions group with 9 updates", auto-opened by the new dependabot config. Not yet reviewed; the actions were already bumped to current majors in #368, so #370 is likely incremental/no-op — review and merge or close.
- Open branches: `main` + this handoff branch.
- Uncommitted changes: none (`.claude/scheduled_tasks.lock` is untracked session noise).
- CI normal-state: **the routine benchmark-comment `UNSTABLE` is GONE (#368).** Green is now green — any PR failure is net-new. (The benchmark *job* still runs ~26 min; its comment posts for same-repo PRs, skips fork PRs.)
- **Infra wins:** GitHub Pages was enabled this session (`build_type=workflow`) — the docs site deploys green for the first time (was 50/50 failures). Live at https://dd0wney.github.io/graphdb/.

## What's next

Per `docs/NEXT_STEPS_2026-06-03.md` § **Track S** (added #381). Track S is the active track:

1. **Cut a versioned client release** bundling #379/#380 (Python + TS hardening). Decide the **PyPI-publish** open question (workflow armed, needs trusted-publishing).
2. **Wave 3 — design-required (each needs a `/spike` or short spec first):**
   - **H-3** encrypt WAL payloads (snapshot encryption leaves the WAL plaintext)
   - **H-5** rate limiting on by default (config-compat decision — don't silently throttle existing deployments)
   - **H-6** thread `context.Context` through algorithm inner loops (betweenness/similarity/triangles — uncancellable goroutines; ~medium blast radius)
   - **M-1/M-2** tenant-delete data remanence (snapshot+truncate after cascade; wire into `pkg/compliance`; fail-or-retry on LSA-snapshot delete)
   - **M-7** token revocation (per-user generation counter as a claim)
   - **M-8** OIDC: composite `TokenValidator` into user-mgmt handlers + `nbf` check
   - **M-14** snapshot magic-header + version (snapshot-format version-bump discipline)
   - **M-15** enterprise `.so` plugin hash/signature verification (cross-repo)
3. **L-tier (10 Lows)** — one cleanup PR or accept-risk-with-rationale. List in `AUDIT_security_2026-06-10.md` § Low.

Off-Track-S (carried, unchanged): GraphQL index-level pagination (offset→ID cursor migration), ctx-passing → auto-embedding (R2.5a), batched-WAL default sweep.

## Stale assumptions to retire

Already applied this session (so the next agent doesn't re-flag):
- `CLAUDE.md` § Known infra patterns — benchmark-comment `UNSTABLE` pattern struck (closed by #368); both historical tolerated-red patterns now closed. Current.
- `CLAUDE.md` § skills table — `ci-status-triage` row updated to "green-is-green." Current.
- Memory `project_ci_red_state_tolerated` + `MEMORY.md` line — updated to "all tolerated-red closed; any red is net-new." Current.
- Memory `project_security_audit_2026_06_10` — updated to mark Waves 1–2 done. Current.
- `NEXT_STEPS_2026-06-03.md` — Track S section added (#381). Current.

Still stale (next session may want to address):
- `docs/internals/design/AUDIT_security_2026-06-10.md` lists Wave 1/2 findings as the fix backlog but does **not** annotate which shipped — the *planning doc* (Track S) carries the PR-by-PR done-state, not the audit doc. If you want the audit doc to self-describe as "Waves 1–2 closed," add a status line at its top (optional — the planning doc is the source of truth).

## Open questions for the user

1. **Cut a client release now?** #379/#380 are merged but unreleased. A bundled Python+TS version bump (+ the PyPI-publish decision) is the natural next step.
2. **Dependabot PR #370** — review/merge or close? (Likely redundant with #368's manual bumps.)
3. **Wave 3 ordering** — which design-required item first? Highest-value candidates: H-5 (rate-limit default — small code, real config decision) or M-1/M-2 (GDPR remanence — compliance-facing).

## Next-session prompt (paste-ready)

`main` is clean at `82a10e4`; the 2026-06-10 security audit's Waves 1 (server, #372–378) and 2 (clients, #379/#380) are shipped. Track S is active (`NEXT_STEPS_2026-06-03.md`). Pick per the user:
1. **Cut a versioned client release** bundling #379/#380 + decide PyPI publish (recommended — closes Wave 2); OR
2. **Start a Wave 3 item** — `/spike` it first (each is design-gated): H-5 rate-limit-on-by-default (config-compat decision), M-1/M-2 tenant-delete remanence (pkg/compliance), H-6 ctx-threading through algorithms, M-7 revocation, M-8 OIDC, H-3 WAL encryption, M-14 snapshot header, M-15 plugin verification; OR
3. **L-tier cleanup PR** (10 Lows in `AUDIT_security_2026-06-10.md`).
First: triage dependabot **PR #370** (open). The audit doc's "consolidated confirmed-clean" section is the start point — don't re-derive it. End the session via the `session-handoff` skill.

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-06-03.md` § Track S + `docs/internals/design/AUDIT_security_2026-06-10.md` (§ confirmed-clean, § backlog).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If cutting the client release: `clients/python/pyproject.toml` + `.github/workflows/python-sdk-publish.yml`. If a Wave 3 item: the specific finding's file:line in the audit doc.
