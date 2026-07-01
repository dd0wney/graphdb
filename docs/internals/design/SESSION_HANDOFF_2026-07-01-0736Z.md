# Session handoff — 2026-07-01 07:36 UTC

**Date**: 2026-07-01 (short session picking up from `SESSION_HANDOFF_2026-07-01-0354Z.md`; 1 PR merged + 1 docs PR open)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-07-01-0354Z.md` (which closed out v1.1 + v1.2 and queued v1.3).

## TL;DR

**v1.3.0 "Deploy anywhere" part-1 shipped**: a single-node Helm chart + a thin Terraform
module landed via **#450**, live-verified on a real kind cluster. v1.3 is now 🟡 **partial** —
the **Go-native client** and the **`gofmt` CI gate** (the other two v1.3 deliverables) are
still unstarted. One release chore blocks a working default install: publish a
`dd0wney/graphdb:1.2.0` image.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #450 | feat(v1.3): Helm chart + Terraform module (Deploy anywhere) | **Merged.** Additive/packaging-only (one Dockerfile uid pin). Live-verified on kind (default + TLS + encryption). The live runs caught **6 runtime bugs static `helm template` could not**: command-vs-args (image uses CMD, no ENTRYPOINT), mandatory `JWT_SECRET`, TLS probe scheme, encryption key-dir under ro-rootfs, plus terraform fmt/naming + image/appVersion mismatch. |
| #451 | docs(planning): reconcile v1.3 (Helm+Terraform done) | **OPEN** — single-file roadmap reconciliation. Awaiting merge sign-off. |

Full artifacts: design `docs/superpowers/specs/2026-07-01-helm-terraform-design.md`, plan
`docs/superpowers/plans/2026-07-01-helm-terraform.md`.

## Current state

- **`origin/main` HEAD**: `93da312` (#450).
- **Open PRs**: **#451** (roadmap reconcile, docs-only, green, awaiting merge).
- **Open branches**: `docs/v1.3-roadmap-reconcile` (#451's branch); plus a stale local
  `main-prerebase-backup` (unrelated, safe to delete).
- **Uncommitted changes**: none (working tree clean on `main`).
- **Test/lint**: #450 merged with all 12 CI checks green (`mergeStateStatus=CLEAN`), including
  the new `helm lint + template` and `terraform fmt + validate` jobs. No Go changed, so the Go
  suite/lint were unaffected.

## What's next

Ranked queue (from `docs/ROADMAP_post_1.0.md`, updated by #451):

1. **Finish v1.3** — the two remaining deliverables, each its own spec→plan→implement cycle:
   - **Go-native client** (`clients/go`) — rounds out the existing Python (`clients/python`)
     + TS (`workers/graphdb-client`). The Python client is the reference for surface parity.
   - **`gofmt` CI lint gate** — trivial; a separate small cycle.
2. **v1.4.0 — Finish the API surface** (GraphQL index-level pagination, F3 compliance HTTP
   API, SDK parity). No gate.
3. **v2.0 snapshot-based replica hydration** (design thread from #445) — the delta-tail/
   freshness gap is the next de-risk. See `SPIKE_SNAPSHOT_HYDRATION_2026-07-01.md`.

### New gaps surfaced this session (for the next planning checkpoint)

- **Publish `dd0wney/graphdb:1.2.0` image** (release chore). The chart default is
  `image.tag=<appVersion>=1.2.0`, but Docker Hub only has `1.0.0`/`latest`/`sha-*`. Until a
  `1.2.0` image exists, a default `helm install` won't pull. Not code — a release-pipeline step.
- **Chart follow-ups deferred (acceptable):** `persistence.enabled=false` (ephemeral) is
  render-verified only; `values.schema.json` is deliberately partial (no `additionalProperties:
  false`); ServiceMonitor has no CRD-capability guard (opt-in gated). Chart publishing
  (OCI/ghcr) is a documented later add.

## Stale assumptions to retire

- **`docs/ROADMAP_post_1.0.md` v1.3.0 section** (was lines 83-88): "Helm chart + Terraform
  module" was unmarked → now ✅ done via #450 (Helm+Terraform), with Go client + gofmt gate
  still open. **#451 already applies this correction** — merge it and the roadmap is current.
- **Prior handoff's next-session prompt** (`SESSION_HANDOFF_2026-07-01-0354Z.md`, "Pick up
  v1.3.0 …") is now **done for the packaging half**; the live pickup is the Go client + gofmt.
- **coord tasks still `pending`.** The v1.1/v1.2/v1.3 coord tasks remain unmarked because the
  **coord daemon (`:8090`) has been down since 2026-07-01** (still down this session). Mark
  them when it's reachable. coord-claim remains un-exercised end-to-end (standing ask).

## Open questions for the user

- **Merge #451?** (docs-only roadmap reconcile; green). It's the last loose end from this session.
- **Who/when publishes `dd0wney/graphdb:1.2.0`?** This is the one real blocker to an
  out-of-the-box `helm install`. Options: cut a v1.2.0 release/tag, or (interim) default the
  chart to `latest`. Recommended: publish the 1.2.0 image to match the pinned default.

## Next-session prompt (paste-ready)

```
v1.3 part-1 (Helm chart + Terraform module) shipped in #450, live-verified on kind. Pick up
the rest of v1.3: the first-party Go-native client (clients/go — mirror the surface of the
existing clients/python; TS lives in workers/graphdb-client) and the trivial gofmt CI gate.
Brainstorm the Go client's API surface first (spec→plan→implement per client). Pre-flight:
build with `go build ./pkg/... ./cmd/...` (NOT ./... — gitignored enterprise-plugins/ breaks
it locally; CI is fine). Housekeeping: merge open PR #451 (roadmap reconcile) if not already;
mark v1.1/v1.2/v1.3 coord tasks done once the coord daemon (:8090) is reachable; someone must
publish dd0wney/graphdb:1.2.0 for the chart's default install to pull. End via session-handoff.
```

## How to use this handoff

1. Read this first.
2. Then `docs/ROADMAP_post_1.0.md` (v1.3 🟡 section) — after #451 merges it reflects current state.
3. `CLAUDE.md` § "Orient first" is auto-loaded.
4. If building the Go client: read `clients/python/src/graphdb_client/` (resources/ + client.py)
   for the surface to mirror, and `pkg/api/` for the server contract.
