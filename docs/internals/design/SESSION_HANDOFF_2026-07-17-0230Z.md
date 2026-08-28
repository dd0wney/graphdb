# Session handoff — 2026-07-17 02:30 UTC

**Date**: 2026-07-17 (single session: hardened + shipped the v1.3 Go client, 1 feature PR merged, 1 docs PR in flight)
**Outgoing model**: Claude Fable 5
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

The v1.3 Go client SDK shipped (#458): the pre-existing `v1.3/go-client` branch was test-hardened (a real goroutine-safety bug found and fixed, 18 edge tests added), review-remediated (2 more real bugs), and squash-merged. Planning-doc reconciliation is in flight as PR #459 (open, pre-authorized to merge on green).

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #458 | feat(v1.3): Go client SDK (clients/go) | Session started from the branch's existing 12 commits and added 6 more. Net-new this session: **(1) a real concurrency bug** — `transport.token` raced under `-race` and 8 concurrent 401s fired 8 refreshes; fixed with mutex-guarded token fields + staleness-checked coalescing (refresh tokens are commonly single-use). **(2) Review found 2 more real bugs**: `Raw()` hardcoded `Status: 200`, and the stale `Authorization` header was sent to `/auth/login`+`/auth/refresh`. **(3) Behavior change**: a failed refresh with no login fallback now surfaces the refresh error instead of swallowing it into a second stale-token 401. 34 tests, ~80% coverage, `-race` in CI, least-privilege workflow token. |

Open (this session's): **#459** — docs(planning): records #458 in `NEXT_STEPS_2026-06-18.md` + `ROADMAP_post_1.0.md` (v1.3 checkbox/graph/table), fixes `CLAUDE.md`'s stale doc chain (ended at 06-17; current is 06-18) and adds `clients/go/` to the repo-layout section.

## Current state

- **`origin/main` HEAD**: `d8bd162` (#458).
- **Open PRs**:
  - **#459** (this session, docs-only) — checks were pending at handoff time; the user said "merge" (squash + `--delete-branch`) once green. **Next session: verify checks and merge it first.**
  - **#457** fix(api): /query value decoding, batch per-item errors, ADMIN_PASSWORD warning — *not this session's work*; in-flight from a parallel session (`fix/api-query-batch-restore`).
  - **#453** dependabot (actions group).
- **Local branches**: `docs/planning-go-client-shipped` (#459), `fix/api-query-batch-restore` (#457), `main-prerebase-backup` (**4 commits ahead of main**, tip `fix(storage): index TypeFloatArray properties into HNSW alongside TypeVector` — provenance unknown to this session; see open questions), `main`.
- **Uncommitted changes**: none.
- **Test/lint state**: `clients/go` is race-clean, vet-clean, `golangci-lint` 0 issues, 80.4% statement coverage. Root-module code untouched this session.

## What's next

From `docs/ROADMAP_post_1.0.md` (the v1.x queue) + `NEXT_STEPS_2026-06-18.md`:

1. **Merge #459** (docs reconciliation) once green — pre-authorized.
2. **v1.3 close-out** (now 🟡 → nearly done): the **repo-wide `gofmt` lint gate** (trivial cycle — note the go-client workflow's gofmt step covers only `clients/go`; the roadmap item is the root `golangci-lint` gofmt gap, NEXT_STEPS §D "CI hygiene"), and the **⚠️ release prerequisite**: publish the `dd0wney/graphdb:1.2.0` Docker image (Docker Hub has only `1.0.0`/`latest`/`sha-*`) or default `helm install` won't pull.
3. **v1.4.0 — Finish the API surface**: GraphQL index-level pagination (resolver offset→ID-cursor), F3 compliance HTTP-API, SDK parity — the Go client now joins Python/TS in the parity set.
4. Housekeeping: resolve #457/#453 (other owners), the `main-prerebase-backup` question below, and the carried-forward item from the 07-01 handoff: **mark the v1.1/v1.2/v1.3 coord tasks done once the coord daemon (:8090) is reachable** (it wasn't, then).

### New gaps surfaced this session (not yet on a planning doc)

- `clients/go` is excluded from the root `golangci-lint` run only implicitly (separate module). If the repo-wide gofmt/lint gate lands, decide whether the client module is covered by its own workflow or folded in.
- The Go client covers current endpoints only; tenants/API-keys/security/compliance surfaces are reachable via `Raw` — candidate facets for v1.4 SDK-parity work.

## Stale assumptions to retire

All four fixes below are **contained in unmerged #459** — if #459 merges cleanly, nothing to do; if it's abandoned, redo them:

- `CLAUDE.md` § "Orient first" item 2: names `NEXT_STEPS_2026-06-17.md` and its chain ends at `06-17` → current checkpoint is **`NEXT_STEPS_2026-06-18.md`**.
- `CLAUDE.md` § "Repo layout": "`workers/graphdb-client/` … Only non-Go SDK that ships" → a first-party **Go SDK now ships** (`clients/go/`, own module; run its tests from inside the directory, not `./clients/go/` from the root module — the root module errors).
- `docs/ROADMAP_post_1.0.md` v1.3: "⬜ First-party Go-native client — not started" (+ dependency graph "Go client (pending)" + summary table) → **done, #458**.
- `docs/NEXT_STEPS_2026-06-18.md` §G: "Client SDKs (Python/Java/Rust)" → Go shipped; Python/Java/Rust remain.

Also retire from this session's own conversation record (no file): the claim that the Go client was "not tracked in any planning doc" — it was tracked in `ROADMAP_post_1.0.md`, which lives outside the `NEXT_STEPS_*` chain. **Lesson for future planning-doc updates: grep `docs/ROADMAP*.md` too, not just the latest `NEXT_STEPS`.**

## Open questions for the user

1. **`main-prerebase-backup`** is 4 commits ahead of main; its tip is a storage fix (`fix(storage): index TypeFloatArray properties into HNSW alongside TypeVector`). Is that work superseded/abandoned (delete the branch) or does it need rescuing into a PR?
2. **#457 and #453**: this session didn't touch them — merge/review them in a session that owns that context, or should the next session pick them up?

## Next-session prompt (paste-ready)

Also written to `docs/internals/design/NEXT_SESSION_PROMPT.md`.

> Read `docs/internals/design/SESSION_HANDOFF_2026-07-17-0230Z.md` first.
> 1. Check PR #459 (docs reconciliation): if checks are green, squash-merge with `--delete-branch` (pre-authorized). If red, triage per `ci-status-triage`.
> 2. Then close out v1.3 (`docs/ROADMAP_post_1.0.md`): (a) the repo-wide gofmt lint gate (root `golangci-lint` doesn't flag gofmt; the go-client workflow already gates `clients/go`); (b) publish the `dd0wney/graphdb:1.2.0` Docker image — the Helm chart's default is unpullable without it.
> 3. Ask the user about `main-prerebase-backup` (4 unmerged commits incl. a TypeFloatArray→HNSW storage fix): rescue or delete. Mark v1.1/v1.2/v1.3 coord tasks done once the coord daemon (:8090) is reachable (carried from the 07-01 handoff).
> Pre-flight: `clients/go` is its own Go module — run its tests from inside the directory. Root `go build ./...` breaks locally if `enterprise-plugins/` is present (see memory); build `./pkg/... ./cmd/...`.
> End via the session-handoff skill.

## How to use this handoff

1. Read this first.
2. Then `docs/ROADMAP_post_1.0.md` (the live v1.x queue) — **note it tracks work the `NEXT_STEPS_*` chain doesn't**.
3. Then `docs/NEXT_STEPS_2026-06-18.md` for the carried inventory.
4. If picking up SDK-parity or client work, read `clients/go/README.md` + `docs/superpowers/specs/2026-07-01-go-client-design.md`.
