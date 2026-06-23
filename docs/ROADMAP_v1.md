# Roadmap: path to v1.0 (graphdb)

**Status:** proposal — first roadmap doc for the project. Defines what v1.0 means, the gating work, and a suggested version cut line.
**Current release:** `v1.0.0` (2026-06-23, GA, GPG-signed). Releases are tag-driven (`.goreleaser.yml`; version injected via `-X main.Version`, no hardcoded version to bump).
**Companion:** `docs/NEXT_STEPS_2026-06-18.md` is the live planning checkpoint (§B–G outstanding inventory). This doc is the GA-specific view layered on top of it.

> **Why this exists.** Before this doc there was no v0.7.0, no v1.0, and no 1.0-blocker list anywhere in the repo — only a `README` note that horizontal scale is "multi-quarter." Defining GA is itself the first prerequisite. This is that definition.

---

## What "1.0" means for graphdb

graphdb is a **single-node** graph database (`cmd/server` is the production REST server). A defensible 1.0 is:

1. **Stable surfaces** — a written commitment that the on-disk formats and the REST/GraphQL API won't break without a major version bump.
2. **Production-hardened** — durability, graceful lifecycle, and multi-tenant safety hold under real conditions.
3. **Operable** — an operator can back up, restore, observe, and deploy without enterprise plugins.
4. **Honestly scoped** — clustering/HA is **explicitly out of scope for 1.0** (single-node by design), not implied by dead code.

Much of the substrate is already GA-grade (see "Already GA-ready"). The gap to 1.0 is a small, concrete hardening set — not a rewrite.

---

## Hard blockers (must clear before any 1.0 tag)

Each is grounded in current code. **Status: the v0.7.0 hardening track (B1/B2/B3/B6) shipped in #427 (2026-06-19); B4 shipped in the v0.8.0 backup track; B5a was already done (M-14); B5b — the stability policy — is now written ([`STABILITY_POLICY.md`](./STABILITY_POLICY.md)). all v1.0 gates are closed — stability policy (#434), CHANGELOG backfill (#433), onboarding (#435) — and **v1.0.0 GA was tagged 2026-06-23**.**

### B1 — `DELETE /nodes` is a cross-tenant data-destruction hole — ✅ DONE (#427)
`/nodes` is registered `requireAuth(withTenant(...))` but **not** admin-gated (`pkg/api/server.go:55`), and its DELETE handler (`pkg/api/handlers_nodes.go:24-62`) calls `graph.DeleteAllNodes()`, which is **tenant-blind** — no tenant parameter, clears every shard and every tenant index (`pkg/storage/node_operations.go:848`). Any authenticated tenant can wipe all tenants' data.
**Fix:** admin-gate the route and/or add a tenant-scoped `DeleteAllNodesForTenant`. (Distinct from the mmap-correctness fix in #423, which addressed reopen behavior, not tenant scoping.)
**✅ Done (#427):** added `DeleteAllNodesForTenant`; the handler now deletes only the caller's tenant via the per-node cascade (inherits WAL + mmap tombstones; other tenants untouched). Concurrent-delete race handled.

### B2 — Graceful shutdown neither drains nor stops the listener — ✅ DONE (#427)
On SIGTERM (`cmd/server/main.go:499-516`) the handler builds a 30s timeout context, then **blocks on `<-ctx.Done()`** — sleeping the full 30s — before `graph.Close()` + `os.Exit(0)`. There is **no `http.Server.Shutdown()`**; the listener is never stopped and in-flight requests are not drained.
**Fix:** call `server.Shutdown(ctx)` to stop accepting and drain, then `graph.Close()`; exit as soon as draining completes rather than always waiting 30s.
**✅ Done (#427):** `http.Server` promoted to an `atomic.Pointer` field + `Shutdown(ctx)`; `main.go` drains then closes, treating `http.ErrServerClosed` as a clean exit. `-race` clean.

### B3 — BatchedWAL is not the production default — ✅ DONE (#427, measure-then-decide)
`DefaultStorageConfig` sets `EnableBatching: false` (`pkg/storage/storage.go:23`), and `cmd/server` uses it unmodified → every write does its own `fsync` (serialized writes, p99 latency floor). `BatchedWAL` (group-commit, correct all-or-nothing) exists and is tested but is never enabled in the binary. This is the open **PERF HIGH-3** / §D "batched-WAL sweep."
**Fix:** run the `FlushInterval` latency-vs-throughput sweep, then either flip the default or **document the per-write-fsync durability guarantee explicitly** as the 1.0 contract.
**✅ Done (#427) — the data inverted the assumption:** the sweep measured batched WAL **13× slower** than per-write fsync on fast NVMe (~10.8µs/op vs batched-1ms ~135µs/op; batching is flush-interval-bound at low writer counts and only wins when fsync is expensive). **Decision: do NOT flip** — kept per-write fsync as the default and documented it + when to opt into batching at the config site. Benchmark retained as evidence.

### B4 — No hot backup/restore in OSS — ✅ DONE (this track, v0.8.0)
OSS backup was a cold `tar` of a stopped volume (`docs/DEPLOYMENT_GUIDE.md`); the `BackupPlugin` interface (`pkg/plugins/interface.go`) was only implemented by the enterprise `r2-backup` plugin. There was no `/backup` or hot-snapshot endpoint in `pkg/api/server.go`.
**✅ Done (v0.8.0 track):** `POST /admin/backup` streams a snapshot-consistent `.tar.gz` archive (snapshot + wal/ + auth/ + lsa/ + manifest.json) without stopping the server. See `docs/BACKUP_RESTORE.md` for the operator runbook and restore procedure.

### B5 — No written API/format stability commitment; JSON snapshot header (split into B5a/B5b)

**B5a — JSON snapshot versioned header — ✅ ALREADY DONE (M-14, pre-v0.8.0)**
The earlier survey described the JSON snapshot as headerless, with detection relying on a `data[0] != '{'` heuristic (audit finding **M-14**). That was correct at the time of the audit, but the fix has since landed: `pkg/storage/snapshot_envelope.go` adds a `GSNP` magic + version uint32 + flags byte envelope around every JSON payload, replacing the first-byte heuristic entirely. B5a was already implemented; the earlier survey mistook a comment in the code for live code.

**B5b — Written API/format stability policy — ✅ DONE**
The REST/GraphQL API had no written stability policy; the `CONSUMER CONTRACT:` tests (`docs/CONSUMER_CONTRACTS.md`) were the only de-facto guard.
**Done:** [`docs/STABILITY_POLICY.md`](./STABILITY_POLICY.md) — covered surfaces (REST/GraphQL, on-disk snapshot/WAL/backup formats, CLI), what "breaking" means, SemVer + format-version rules, deprecation policy, and the consumer-contract tests as enforcement. Takes effect at v1.0.0.

### B6 — Cluster dead code must be scoped out, not shipped silently — ✅ DONE (#427)
`pkg/cluster/` (~2,800 LOC) implements Raft-style election/membership but is **not wired** — `pkg/cluster/doc.go` says it has no replication append path and nothing outside its own tests imports it; `cmd/server`/`pkg/api` import none of it. Shipping it implies HA that doesn't exist.
**Fix:** declare 1.0 **single-node only**; mark the package `EXPERIMENTAL` (or exclude from the server binary). No new clustering work is a 1.0 requirement.
**✅ Done (#427):** `doc.go` header → `EXPERIMENTAL — NOT WIRED — single-node only`; README + CAPABILITIES state single-node; added a `go/parser` import-guard test that fails if `cmd/server`/`pkg/api` ever import `pkg/cluster` (or a sub-package).

---

## Already GA-ready (no work required)

| Area | Evidence |
|---|---|
| Observability | Prometheus `/metrics`, `/health` + `/health/ready` + `/health/live`, JSON structured logging (`pkg/metrics`, `pkg/logging`, `pkg/api/server.go`) |
| TLS / mTLS | `pkg/tls/` — full config, client auth, min-version, dev auto-cert |
| Encryption at rest | AES-256-GCM + PBKDF2 + key rotation (`pkg/encryption/`) |
| Rate limiting | On by default; stricter auth limiter always on (`pkg/api/middleware_ratelimit.go`) |
| Tenant isolation (reads/queries) | `withTenant` on `/nodes`, `/edges`, `/traverse`, `/query`, `/graphql`, `/algorithms`, `/vector-*`, `/search` (`pkg/api/server.go`) |
| Crash recovery | WAL replay + extensive tests (`pkg/storage/crash_recovery_test.go`, `integration_wal_batched_test.go`) |
| Packaging | Multi-stage non-root Dockerfile + healthcheck; compose files; env-var config; `docs/DEPLOYMENT_GUIDE.md` |

---

## Lower-priority 1.0 items (do, but not release-blocking on their own)

- **Docs/onboarding (§D Wave 2)** + **CHANGELOG gap**: `CHANGELOG.md` jumps `[0.6.0] → [0.3.0]` — the v0.4.0/v0.4.1/v0.5.0 entries and comparison links are missing. A credible GA needs complete, honest release notes + customer-facing onboarding.
- **A5 `withTenant` DSL-search residual** (`pkg/search/tenant_indexes.go:20`): tenant-aware DSL search not fully scoped. Single-tenant deployments unaffected; fix for multi-tenant GA.

## Explicitly deferred past 1.0 (NOT blockers)

First-party client SDKs beyond TS/Python (Java/Rust); Helm/Terraform/k8s manifests; OpenTelemetry tracing; data-platform connectors (Kafka/ETL/lakehouse/BI); the mmap-default decision (B-1/B-2 in NEXT_STEPS — mmap is off by default); the architecture refactors (NEXT_STEPS §F). These are §G commercial/productization growth, not GA gates for a single-node engine.

---

## Suggested version cut line

Independent tracks; order front-loads correctness.

| Release | Theme | Contents |
|---|---|---|
| **v0.7.0** ✅ | Production hardening | B1 (tenant-safe delete), B2 (graceful shutdown drain), B3 (WAL default decision), B6 (scope cluster experimental). **Shipped #427 (2026-06-19); tag pending.** |
| **v0.8.0** ✅ | Operability | B4 (hot backup/restore endpoint) ✅ DONE (this track); ~~B5a~~ already done pre-v0.8.0 (GSNP envelope, M-14 closed). |
| **v1.0.0** ✅ | Stable | B5b ✅ (`docs/STABILITY_POLICY.md`, #434), CHANGELOG ✅ (#433), onboarding ✅ (#435). **Single-node GA declared and tagged `v1.0.0` (2026-06-23).** |

Roughly **4 hardening PRs + 2 operability features + a stability-policy doc**. Reachable in a few focused tracks because the security/observability/crypto substrate is already solid.

## How a release is cut (reference)

Push a `vX.Y.Z` tag → `.github/workflows/release.yml` runs `goreleaser release --clean` → builds `graphdb-server`/`graphdb-cli`/`graphdb-tui` for linux/darwin amd64+arm64 (Windows dropped, #421) + GitHub release + Docker image. A pre-release component (`v0.7.0-rc.1`) publishes as a GitHub pre-release (`prerelease: auto`). No source edit is needed to bump the version.

## How to use this document

1. v1.0 scope and blockers live here; the live per-track queue lives in `docs/NEXT_STEPS_2026-06-18.md`.
2. As a blocker closes, mark it here (with the PR/SHA) and reconcile NEXT_STEPS via the `planning-doc-update` skill.
3. The blocker file:line references were verified against `main` at `3885945` (2026-06-18) — re-verify before acting, per the repo's "trust the code" rule.
