# Session handoff — 2026-10-08 02:50 UTC

**Date**: 2026-10-08 (single session; 10 PRs merged, 2 open; driven largely by a new embedding consumer, mapper)
**Outgoing model**: Claude Opus 5.5
**Delegation**:
- 4 × Explore (sonnet), general-database gap sweep (storage, REST/clients, Cypher, ops). Used after verification; three claims were wrong and were corrected before use (CheckInvariants does support mmap; the Go client never retries POST; REST batch partial success is contract CC7/CC20, not a defect).
- 6 × reviewer (sonnet): #641, #644, #646 at low effort; #645 and #650 at high effort; one read-only review of mapper's embedded backend. Every report was re-checked in code before acting; the #645 and #650 reviews each produced real changes (Stat-error refusal, WAL-behind-snapshot refusal; effective-tenant membership key, batch commit recheck, batch nil guard).
- 1 × security (opus), encryption-at-rest review for mapper, with probes in a separate worktree. Used; its two key claims (rules.json plaintext, envelope test cannot fail) re-checked in code.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

A new consumer, `oit-cyber/mapper` (embeds `pkg/storage`; session `mapper-6b`), drove a gap review that found and fixed seven data-correctness defects: Cypher answers that were wrong with no error, bulk-mode data loss and resurrection, a mmap format limit that lost long records, decoder panics, a nil-map panic that hung the store, and a readiness probe that hid a poisoned WAL. v1.6.0 is approved and waits only for #650.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #640 | chore(coord): seed seven tasks from the mapper embedding review | node upsert (storage/REST/Go client), 409 `conflicting_node_id`, MERGE errors, data-dir flock, indexed unique create |
| #641 | fix(query): use a property index only where it cannot change the rows | An index on `pid` made `MATCH (a:P)-[:R]->(b:Q) WHERE a.pid=1` return `b=null`. Oracle test (rows with vs without index) + plan test (the oracle passes if the index is never used). New `storage.PropertyIndexType`. |
| #642 | chore(coord): seed seventeen tasks from the general gap sweep | 6 Cypher silent results, 3 hidden ops failures, 6 missing primitives, WAL default re-measure, bulk group commit |
| #643 | chore(coord): seed the bulk-mode refusal of an unreplayed WAL | |
| #644 | fix(query): make Cypher DELETE delete edges and refuse what it cannot delete | `DELETE r` was a no-op reporting affected=1; `DELETE r, a` and a node in several rows failed with "not found" after deleting. |
| #645 | fix(storage): refuse a bulk-mode open that would drop or re-apply WAL entries | Two defects: bulk open ignored an unreplayed WAL (acknowledged write lost, ID reused, CheckInvariants clean); bulk Close recorded boundary 0, so covered entries re-applied and a deleted node came back. |
| #646 | fix(query): make Cypher SET and REMOVE write edge properties | Also fixed a storage panic: patching a node/edge created with nil properties ("assignment to entry in nil map"); the node path held a shard lock. |
| #647 | fix(api): report not-ready when storage is closed or its WAL is poisoned | New `GraphStorage.Health()`; `/health/ready` used to return 200 over a poisoned WAL. |
| #648 | chore(coord): seed three tasks from mapper's storage findings | decoder length checks, lock panic safety, sub-second timestamps; plus (second commit) mmap width refusal and mapper consumer contracts |
| #649 | fix(storage): refuse a damaged value instead of panicking in its decoder | `AsInt/AsFloat/AsBool/AsTimestamp` panicked on short data; `AsStringArray` allocated 64 GiB from `FF FF FF FF`. |

Coord tasks released `done` with their PRs: `v1.4-cypher-index-lookup-drops-expansion` (#641), `v1.4-cypher-delete-edge-noop` (#644), `v1.4-bulk-mode-refuses-unreplayed-wal` (#645), `v1.4-cypher-set-edge-noop` (#646), `v1.4-readiness-reflects-storage` (#647), `v1.4-value-decoders-check-length` (#649).

## Current state

- `origin/main` HEAD: `ba6b083` (#649).
- Open PRs:
  - **#650** `fix(storage): refuse a record the mmap snapshot cannot store` — task `v1.4-mmap-record-width-refusal` (claimed). User approved merge on green and inclusion in v1.6.0. Release its task with `coord release graphdb:v1.4-mmap-record-width-refusal --pr 650` after merge.
  - **#651** `chore(coord): seed three tasks from the encryption-at-rest review` — seed record; tasks already applied to coord.
- Local branches: `chore/seed-encryption-findings` (#651), `v1.4/v1.4-mmap-record-width-refusal` (#650), and `feat/ulysses-consumer-contracts`, which is checked out in a separate worktree (`../graphdb-ulysses-contracts`, 46d9c8b) that is not from this session. Leave it alone.
- Uncommitted changes: none.
- Gates on the last code PR: `make test-local` (54 packages ok), full `pkg/storage` suite, `golangci-lint` v2.13.2 (0 issues), `gofmt -s`, `make contract-guard` all clean.
- **v1.6.0 is approved but not cut.** The changelog section is drafted (session scratchpad, not in the repo). Steps: release PR (CHANGELOG `[1.6.0]`, README and `docs/ROADMAP_post_1.0.md` pointers, `deployments/helm/graphdb/Chart.yaml` 0.1.3 / appVersion 1.6.0), merge, then `git tag -a v1.6.0` and push; the tag triggers the signed Release and Docker workflows. The v1.5.0 tag is annotated, not GPG-signed; CI signs the artifacts.

## What's next

1. **Finish v1.6.0** (above) if this session did not. Then tell `mapper-6b` to pin v1.6.0.
2. **Cypher silent results still open** (all seeded, all `v1.4-`): `cypher-multiple-match-clauses` (a second MATCH or a comma pattern is dropped; parser keeps one slot per clause), `cypher-implicit-grouping` (`RETURN n.city, count(n)` gives one row per node with count null), `cypher-delete-without-detach` (needs a user decision, see §7).
3. **`v1.4-lock-panic-safety`**: write paths that release a lock without `defer` turn any panic into a hung store (the Cypher executor and net/http recover panics). #646 and #650 removed two panic sources, not the pattern.
4. **WAL throughput**: `v1.4-wal-default-remeasure` (the default's justification was measured on tmpfs, see §6) and `v1.4-bulk-create-group-commit` with `v1.4-storage-batch-atomic-commit`.
5. **mapper asks**: `v1.4-storage-upsert-node` → `v1.4-rest-upsert-node` → `v1.4-go-client-upsert-node`; `v1.4-rest-conflict-node-id`; `v1.4-datadir-flock`; `v1.4-unique-create-property-index` (size it with a graphdb benchmark, not mapper's numbers — see the insight on the task); `v1.4-mapper-consumer-contracts`.
6. **Encryption** (#651): `v1.4-encryption-no-plaintext-test` first (it is the acceptance check for `v1.4-encrypt-uniqueness-rules-file`), and `v1.4-encryption-envelope-test-cannot-fail`.
7. **General primitives** from the sweep: `v1.4-rest-edge-upsert`, `v1.4-node-label-mutation`, `v1.4-optimistic-concurrency` (design first), `v1.4-rest-schema-introspection`, `v1.4-rest-property-index-routes`, `v1.4-rest-idempotency-key`, `v1.4-tenant-quota-enforcement`, `v1.4-subsecond-timestamps` (format design first).

### Gaps surfaced but not seeded

- `WITH` binds `nil` for an undefined variable (`pkg/query/executor.go:200` at 5988924), so `WITH x AS y DELETE y` with a misspelled `x` still writes nothing with no error.
- Float NaN and -0 may compare differently in a property index and in the scan (raised in the #641 review, not checked).
- `encryption.Engine` has no method that zeroes the key, and `GraphStorage.Close` does not clear it.

## Stale assumptions to retire

- **`pkg/storage/storage.go:24-33` (DefaultStorageConfig comment)**: "per-write fsync … the fastest on local/NVMe storage (~11µs)". Those numbers reproduce only on tmpfs: `b.TempDir()` is `/tmp`, which is tmpfs on the dev box. Same binary, only TMPDIR changed, 32 writers: tmpfs 8 µs vs batched-1ms 138 µs; LUKS ext4 19,520 vs 2,794; plain btrfs NVMe 36,004 vs 4,996. Task `v1.4-wal-default-remeasure` owns the fix; do not change the default before it reports.
- **`CLAUDE.md` § Build, test, lint**: "pkg/storage needs -timeout 300s (suite runs ~120-170s as of 2026-06)". The full suite ran in 43-44 s five times today. The timeout is harmless; the duration is stale.
- **Auto-memory `coord-graphdb-claim-release-explicitly`**: "since #583 a `<track>/<id>` branch claims via the hook". This clone has no `.git/hooks/post-checkout`, so a `v1.4/<id>` branch claimed nothing; every claim today was by hand (`coord claim graphdb:<id>`).
- **Coord task `v1.4-cypher-merge-update-errors`**: its title says MERGE discards update errors and returns a stale graph. True of `pkg/query/physical_ops_mutate.go:344,377`, but that code is reached only through `planner.go`, which nothing outside itself calls. Production runs `MergeStep` (`executor_steps.go`), which returns every error. The user kept the task as is; read it as "dead-code hygiene", not a live defect.
- **Coord task `v1.4-unique-create-property-index`**: its title says to wait for mapper's spike numbers. Superseded by insight `insight-1791416587-049514`: size it with a graphdb benchmark. mapper's per-node numbers measured the 10 ms WAL flush timer, not the scan.
- **`docs/CAPABILITIES_2026-05-10.md`** lists `query` as mature with no Cypher gaps. Six silent-result defects were found today; three are fixed (#641, #644, #646).
- **`pkg/storage/mmap_snapshot_format.go`** carries uint16 lengths with no documented limit. After #650 the limit is enforced at write (`ErrRecordTooWide`, `record_width.go`); anyone documenting the format should cite 65,535 for every count and length and for the membership key.

## Open questions for the user

1. **`v1.4-cypher-delete-without-detach`**: refuse a plain `DELETE` of a node that has edges (openCypher/Neo4j), or document graphdb's silent detach? A refusal can break a consumer; check `docs/CONSUMER_CONTRACTS.md` first.
2. **Unseeded gaps** in §5 (`WITH` nil binding, float NaN/-0 in an index, key zeroing): seed any of them?

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (same text).

## How to use this handoff

1. Read this first, then `docs/NEXT_STEPS_2026-06-18.md` for the older queue (much of today's work is in coord only: `coord status graphdb`).
2. Before trusting a coord task title written today, check for an insight on it: `coord insight list --task graphdb:<id>`.
3. Message `mapper-6b` (ListAgents) before a change to anything in its six depended-on behaviours (`v1.4-mapper-consumer-contracts`).
