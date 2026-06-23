# API & Format Stability Policy

This document is GraphDB's compatibility promise: which surfaces are stable,
what counts as a breaking change, and how changes are versioned. It takes effect
at **v1.0.0**. Before 1.0, anything may change between minor versions.

GraphDB follows [Semantic Versioning](https://semver.org/): given `MAJOR.MINOR.PATCH`,

- **MAJOR** — may contain breaking changes to a covered surface (below).
- **MINOR** — adds functionality in a backward-compatible way.
- **PATCH** — backward-compatible bug fixes only.

## Covered surfaces (stable from 1.0)

A breaking change to any of these requires a **major** version bump.

| Surface | What's covered |
|---|---|
| **REST API** | Request/response shapes, status codes, auth semantics, and tenant-scoping behavior of documented endpoints under `pkg/api`. |
| **GraphQL API** | The published schema: types, fields, arguments, and their nullability/semantics. |
| **On-disk JSON snapshot** | `snapshot.json` under its `GSNP` envelope (magic + version + flags). |
| **On-disk mmap snapshot** | `snapshot.mmap` (`GMNP`, version 4) — even though the *mode* is off by default, the *format* is versioned and load-bearing. |
| **Write-ahead log** | WAL record framing and replay semantics. |
| **Backup archive** | The `.tar.gz` layout and `manifest.json` schema (`manifest_version`) produced by `POST /admin/backup` and consumed by `graphdb-admin backup verify`/`restore`. |
| **CLI** | Documented `graphdb-server` flags/env vars and `graphdb-admin` subcommands and their exit codes. |

## What counts as a breaking change

- Removing or renaming an endpoint, field, flag, subcommand, or env var.
- Changing a response/record shape, type, or nullability in a way an existing consumer can observe.
- Changing default behavior in a way that alters results for an unchanged request.
- Reading an existing on-disk file in a way that produces different data, or writing a format an older same-major version cannot read, **without** a format version bump.

Additive changes (new optional fields, new endpoints, new opt-in flags) are **not** breaking and ship in minor releases.

## On-disk formats

Every on-disk format carries an explicit version (`GSNP` / `GMNP` / WAL / `manifest_version`). The rules:

- **Never** change a format's bytes without bumping its version — the snapshot and backup files are customer-data-equivalent.
- A given MAJOR line reads every format version it has ever written (backward read compatibility), migrating forward on write.
- The JSON↔mmap public-interface equivalence oracle (`pkg/storage/mmap_reopen_test.go`) gates the two snapshot paths against silent divergence.

## Not covered (no stability guarantee)

- **Internal Go packages.** Importing GraphDB as a library is not a supported, versioned API; only the binaries, REST/GraphQL APIs, and on-disk formats are.
- **`pkg/cluster` / replication.** Marked EXPERIMENTAL and not wired into the server (single-node by design for 1.0).
- **Benchmark/demo binaries** (`cmd/benchmark*`, `cmd/graphdb` demo) and anything documented as experimental or preview.
- Exact performance numbers, log lines, and metric label *values* (metric *names* follow Prometheus conventions but are not part of the SemVer contract).

## Deprecation

When a covered surface must change incompatibly, the old behavior is deprecated for at least one **minor** release before removal in the next **major** — documented in the [CHANGELOG](../CHANGELOG.md) under a `Deprecated` heading.

## How this is enforced

The `CONSUMER CONTRACT:`-tagged tests (catalogued in [`docs/CONSUMER_CONTRACTS.md`](CONSUMER_CONTRACTS.md); find them with `grep -rn "CONSUMER CONTRACT:" pkg/`) pin the behaviors downstream consumers depend on. A change that breaks one is a breaking change by definition: it either gets reverted or rides a major bump with the contract updated.
